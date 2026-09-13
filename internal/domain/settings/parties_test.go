package settings

import (
	"errors"
	"testing"
)

// TestValidateParties: names are trimmed, empty and repeated names refused
// (case-insensitively), the default is never stored.
func TestValidateParties(t *testing.T) {
	got, err := ValidateParties([]Party{{Name: " Acme ", Default: true}, {Name: "Landing gear supplier", Note: " gear "}})
	if err != nil || len(got) != 1 || got[0].Name != "Landing gear supplier" || got[0].Note != "gear" {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, bad := range [][]Party{{{Name: " "}}, {{Name: "A"}, {Name: "a"}}} {
		if _, err := ValidateParties(bad); !errors.Is(err, ErrInvalidParties) {
			t.Errorf("ValidateParties(%+v) = %v, want ErrInvalidParties", bad, err)
		}
	}
	many := make([]Party, MaxParties+1)
	for i := range many {
		many[i].Name = string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := ValidateParties(many); !errors.Is(err, ErrInvalidParties) {
		t.Errorf("too many parties accepted")
	}
}

// TestWithDefault: the workspace is first and marked default, keeps a note
// the project stored under the same name, and is never listed twice.
func TestWithDefault(t *testing.T) {
	got := WithDefault("Acme", []Party{{Name: "Supplier"}, {Name: "acme", Note: "us"}})
	if len(got) != 2 || !got[0].Default || got[0].Name != "Acme" || got[0].Note != "us" || got[1].Name != "Supplier" {
		t.Fatalf("got %+v", got)
	}
	if got := WithDefault("", []Party{{Name: "Supplier"}}); len(got) != 1 || got[0].Default {
		t.Fatalf("no workspace: %+v", got)
	}
}

type fakeStore struct{ project map[string]interface{} }

func (f *fakeStore) OrgSettings(string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (f *fakeStore) SetOrgSettings(string, map[string]interface{}) error    { return nil }
func (f *fakeStore) ProjectSettings(string) (map[string]interface{}, error) { return f.project, nil }
func (f *fakeStore) SetProjectSettings(_ string, s map[string]interface{}) error {
	f.project = s
	return nil
}

// TestPartiesRoundTrip: what is stored comes back, other settings survive,
// and an empty list removes the key.
func TestPartiesRoundTrip(t *testing.T) {
	store := &fakeStore{project: map[string]interface{}{"quality": map[string]interface{}{"convention": "shall"}}}
	svc := NewService(store)
	if _, err := svc.SetProjectParties("p1", []Party{{Name: "Supplier", Note: "gear"}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := svc.ProjectParties("p1")
	if err != nil || len(got) != 1 || got[0].Name != "Supplier" || got[0].Note != "gear" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, ok := store.project["quality"]; !ok {
		t.Fatalf("other settings lost: %v", store.project)
	}
	if _, err := svc.SetProjectParties("p1", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := store.project[PartiesKey]; ok {
		t.Fatalf("empty list left the key behind")
	}
}
