package savedsearches

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeRepo struct {
	rows []*SavedSearch
}

func (f *fakeRepo) Upsert(s *SavedSearch) error {
	for i, r := range f.rows {
		if r.UserID == s.UserID && r.OrgID == s.OrgID && r.Scope == s.Scope && r.Name == s.Name {
			s.ID, s.CreatedAt = r.ID, r.CreatedAt
			f.rows[i] = s
			return nil
		}
	}
	f.rows = append(f.rows, s)
	return nil
}

func (f *fakeRepo) ListVisible(userID, orgID, scope string) ([]*SavedSearch, error) {
	var out []*SavedSearch
	for _, r := range f.rows {
		if r.OrgID == orgID && r.Scope == scope && (r.UserID == userID || r.Shared) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) CountForUser(userID, orgID, scope string) (int, error) {
	n := 0
	for _, r := range f.rows {
		if r.UserID == userID && r.OrgID == orgID && r.Scope == scope {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) DeleteForUser(userID, orgID, id string) (int64, error) {
	for i, r := range f.rows {
		if r.ID == id && r.UserID == userID && r.OrgID == orgID {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func TestNewValidates(t *testing.T) {
	obj := json.RawMessage(`{"chips":[],"text":""}`)
	for _, tc := range []struct {
		name  string
		scope string
		title string
		value json.RawMessage
		want  error
	}{
		{"ok", "requirements", "Release 1.4", obj, nil},
		{"blank scope", " ", "x", obj, ErrScope},
		{"scope with a space", "my page", "x", obj, ErrScope},
		{"scope too long", strings.Repeat("a", 65), "x", obj, ErrScope},
		{"blank name", "requirements", "  ", obj, ErrName},
		{"name too long", "requirements", strings.Repeat("é", 121), obj, ErrName},
		{"array value", "requirements", "x", json.RawMessage(`[1]`), ErrValue},
		{"null value", "requirements", "x", json.RawMessage(`null`), ErrValue},
		{"no value", "requirements", "x", nil, ErrValue},
		{"huge value", "requirements", "x", json.RawMessage(`{"t":"` + strings.Repeat("a", MaxValueBytes) + `"}`), ErrValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := New("u1", "o1", tc.scope, tc.title, tc.value, false)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err == nil && (s.ID == "" || s.Name != strings.TrimSpace(tc.title)) {
				t.Fatalf("built %+v", s)
			}
		})
	}
}

func TestSaveReplacesByNameAndCapsNewNames(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewDefaultService(repo)
	obj := json.RawMessage(`{}`)
	first, _ := New("u1", "o1", "requirements", "Mine", obj, false)
	if err := svc.Save(first); err != nil {
		t.Fatal(err)
	}
	again, _ := New("u1", "o1", "requirements", "Mine", json.RawMessage(`{"text":"x"}`), true)
	if err := svc.Save(again); err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || len(repo.rows) != 1 {
		t.Fatalf("a save under the same name should replace: id %s vs %s, %d rows", again.ID, first.ID, len(repo.rows))
	}

	for i := len(repo.rows); i < MaxPerScope; i++ {
		s, _ := New("u1", "o1", "requirements", "n"+strings.Repeat("x", i), obj, false)
		if err := svc.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	over, _ := New("u1", "o1", "requirements", "one too many", obj, false)
	if err := svc.Save(over); !errors.Is(err, ErrTooMany) {
		t.Fatalf("err = %v, want ErrTooMany", err)
	}
	replace, _ := New("u1", "o1", "requirements", "Mine", obj, false)
	if err := svc.Save(replace); err != nil {
		t.Fatalf("replacing at the cap: %v", err)
	}
	other, _ := New("u1", "o1", "global-search", "elsewhere", obj, false)
	if err := svc.Save(other); err != nil {
		t.Fatalf("another scope is not capped by this one: %v", err)
	}
}

func TestDeleteOnlyTheOwnersOwn(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewDefaultService(repo)
	s, _ := New("u1", "o1", "requirements", "Shared", json.RawMessage(`{}`), true)
	_ = svc.Save(s)
	if n, _ := svc.Delete("u2", "o1", s.ID); n != 0 {
		t.Fatal("another member deleted a shared search")
	}
	list, _ := svc.List("u2", "o1", "requirements")
	if len(list) != 1 {
		t.Fatalf("a shared search is listed for the workspace: %d", len(list))
	}
	if n, _ := svc.Delete("u1", "o1", s.ID); n != 1 {
		t.Fatal("the owner could not delete their search")
	}
}
