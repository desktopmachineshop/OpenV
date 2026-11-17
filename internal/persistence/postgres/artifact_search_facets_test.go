package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// TestArtifactSearchFacets: the chip search bar's facets (issue #722) narrow
// the search by type, review status and owner (a substring, any case), with
// the ref predicate's parameter in between, a search of facets alone finds
// rows, and Facets carries each hit's status and owner back.
func TestArtifactSearchFacets(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewArtifactRepository(db)

	proj := uuid.New().String()
	seedProjects(t, db, proj)
	save := func(typ, title, status, owner string) *artifacts.Artifact {
		t.Helper()
		attrs := map[string]interface{}{}
		if owner != "" {
			attrs["owner"] = owner
		}
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: proj, Type: typ, Title: title, Body: "spindle", Attributes: attrs})
		if status != "" {
			a.Status = status
		}
		if err := repo.Save(a); err != nil {
			t.Fatalf("save %q: %v", title, err)
		}
		return a
	}
	daves := save("requirement", "Spindle speed", "in_review", "Dave Smith")
	save("requirement", "Spindle torque", "draft", "sam@example.com")
	tc := save("test-case", "Spindle test", "", "")

	ids := func(hits []*artifacts.SearchHit) map[string]bool {
		out := map[string]bool{}
		for _, h := range hits {
			out[h.ArtifactID] = true
		}
		return out
	}
	for _, matchRefs := range []bool{false, true} {
		got, err := repo.SearchInProjects([]string{proj}, "spindle", 20, artifacts.SearchOptions{
			MatchRefs: matchRefs, Types: []string{"requirement"}, Statuses: []string{"in_review"}, Owners: []string{"DAV"}, Facets: true,
		})
		if err != nil {
			t.Fatalf("faceted search (refs %v): %v", matchRefs, err)
		}
		if len(got) != 1 || got[0].ArtifactID != daves.ID {
			t.Fatalf("faceted search (refs %v) = %+v, want Dave's requirement alone", matchRefs, got)
		}
		if got[0].Status != "in_review" || got[0].Owner != "Dave Smith" {
			t.Errorf("facets carried = %q / %q", got[0].Status, got[0].Owner)
		}
	}

	got, err := repo.SearchInProjects([]string{proj}, "", 20, artifacts.SearchOptions{Types: []string{"test-case"}})
	if err != nil {
		t.Fatal(err)
	}
	if h := ids(got); len(h) != 1 || !h[tc.ID] {
		t.Fatalf("a search of facets alone = %+v, want the test case", got)
	}
	if got[0].Status != "" {
		t.Error("status carried without Facets")
	}

	got, _ = repo.SearchInProjects([]string{proj}, "spindle", 20, artifacts.SearchOptions{Statuses: []string{"draft"}})
	if len(got) != 2 {
		t.Fatalf("status draft (the column, else draft) = %d hits, want 2", len(got))
	}
}
