package links_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/links"
)

func strPtr(s string) *string { return &s }

// TestUpdateLinkChangesOnlyTheFieldsSent is the domain half of #642: a
// field left out of the request keeps its stored value and a field present
// replaces it, an empty map clearing the attributes, and a type sent empty
// is refused before anything is written.
func TestUpdateLinkChangesOnlyTheFieldsSent(t *testing.T) {
	newService := func() (*links.DefaultService, *avLinkRepo) {
		repo := newAVLinkRepo()
		repo.saved["link-1"] = &links.Link{ID: "link-1", Type: "verifies", Version: 1,
			Attributes: map[string]interface{}{"method": "test"}}
		return links.NewDefaultService(repo), repo
	}

	t.Run("only attributes keeps the type", func(t *testing.T) {
		svc, repo := newService()
		got, err := svc.UpdateLink("link-1", links.UpdateLinkRequest{Attributes: map[string]interface{}{"method": "demo"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != "verifies" || repo.saved["link-1"].Type != "verifies" {
			t.Errorf("type = %q (stored %q), want verifies kept", got.Type, repo.saved["link-1"].Type)
		}
		if want := map[string]interface{}{"method": "demo"}; !reflect.DeepEqual(got.Attributes, want) {
			t.Errorf("attributes = %v, want %v", got.Attributes, want)
		}
		if got.Version != 2 {
			t.Errorf("version = %d, want 2", got.Version)
		}
	})

	t.Run("only a type keeps the attributes", func(t *testing.T) {
		svc, repo := newService()
		got, err := svc.UpdateLink("link-1", links.UpdateLinkRequest{Type: strPtr("relates-to")})
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != "relates-to" || repo.saved["link-1"].Type != "relates-to" {
			t.Errorf("type = %q (stored %q), want relates-to", got.Type, repo.saved["link-1"].Type)
		}
		if want := map[string]interface{}{"method": "test"}; !reflect.DeepEqual(repo.saved["link-1"].Attributes, want) {
			t.Errorf("stored attributes = %v, want %v kept", repo.saved["link-1"].Attributes, want)
		}
	})

	t.Run("an empty attributes map clears them", func(t *testing.T) {
		svc, repo := newService()
		if _, err := svc.UpdateLink("link-1", links.UpdateLinkRequest{Attributes: map[string]interface{}{}}); err != nil {
			t.Fatal(err)
		}
		if got := repo.saved["link-1"].Attributes; len(got) != 0 {
			t.Errorf("stored attributes = %v, want none", got)
		}
	})

	t.Run("an empty type is refused and nothing is written", func(t *testing.T) {
		svc, repo := newService()
		_, err := svc.UpdateLink("link-1", links.UpdateLinkRequest{Type: strPtr(""), Attributes: map[string]interface{}{"method": "demo"}})
		if !errors.Is(err, links.ErrEmptyType) {
			t.Fatalf("error = %v, want ErrEmptyType", err)
		}
		got := repo.saved["link-1"]
		if got.Type != "verifies" || got.Version != 1 || got.Attributes["method"] != "test" {
			t.Errorf("stored link = %+v, want it unchanged", got)
		}
	})
}
