package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// TestArtifactSearchByRef covers finding an artifact by its stable ref.
// Someone who types "REQ-30" is naming one artifact, so the exact match has to
// come first — ahead of a longer ref that merely contains the query, and ahead
// of another artifact that cites it in its body.
func TestArtifactSearchByRef(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewArtifactRepository(db)

	proj := uuid.New().String()

	// Refs are supplied rather than minted so REQ-3 and REQ-30 both exist
	// without creating twenty-seven artifacts in between; Save keeps a
	// caller-supplied ref (see ensureRef).
	save := func(ref, title, body string) *artifacts.Artifact {
		t.Helper()
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID: proj,
			Type:      "requirement",
			Title:     title,
			Body:      body,
		})
		a.Ref = ref
		if err := repo.Save(a); err != nil {
			t.Fatalf("save %s: %v", ref, err)
		}
		return a
	}

	three := save("REQ-3", "Seal integrity", "the seal shall hold to 6 bar")
	thirty := save("REQ-30", "Noise limit", "sound pressure stays below 60 dB")
	citing := save("REQ-77", "Acoustics plan", "verifies REQ-30 on the test rig")

	scope := []string{proj}
	// Searching by ref is gated on release.FeatureSearchByRef; these are the
	// options a workspace that has it sends.
	refSearch := artifacts.SearchOptions{MatchRefs: true}

	t.Run("finds an artifact by its ref, whatever the case", func(t *testing.T) {
		for _, query := range []string{"REQ-30", "req-30", "  Req-30  "} {
			hits, err := repo.SearchInProjects(scope, query, 20, refSearch)
			if err != nil {
				t.Fatalf("SearchInProjects(%q): %v", query, err)
			}
			if len(hits) == 0 {
				t.Fatalf("SearchInProjects(%q) found nothing", query)
			}
			if hits[0].ArtifactID != thirty.ID {
				t.Errorf("SearchInProjects(%q) first hit = %s (%s), want REQ-30",
					query, hits[0].Ref, hits[0].Title)
			}
		}
	})

	t.Run("ranks the exact ref above one that merely contains it", func(t *testing.T) {
		// "REQ-3" is a substring of "REQ-30", so both match. The one that *is*
		// REQ-3 has to win, or typing a low-numbered ref becomes useless as
		// soon as a higher-numbered one exists.
		hits, err := repo.SearchInProjects(scope, "REQ-3", 20, refSearch)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) == 0 {
			t.Fatal("SearchInProjects(REQ-3) found nothing")
		}
		if hits[0].ArtifactID != three.ID {
			t.Errorf("first hit = %s (%s), want REQ-3", hits[0].Ref, hits[0].Title)
		}
	})

	t.Run("ranks the ref itself above an artifact citing it", func(t *testing.T) {
		hits, err := repo.SearchInProjects(scope, "REQ-30", 20, refSearch)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		var ids []string
		for _, h := range hits {
			ids = append(ids, h.Ref)
		}
		if len(hits) < 2 {
			t.Fatalf("got %v, want both REQ-30 and the artifact citing it", ids)
		}
		if hits[0].ArtifactID != thirty.ID || hits[1].ArtifactID != citing.ID {
			t.Errorf("order = %v, want REQ-30 before REQ-77 (which only cites it)", ids)
		}
	})

	t.Run("carries the ref back on every hit", func(t *testing.T) {
		hits, err := repo.SearchInProjects(scope, "seal", 20, refSearch)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 1 {
			t.Fatalf("got %d hits, want 1", len(hits))
		}
		if hits[0].Ref != "REQ-3" {
			t.Errorf("Ref = %q, want REQ-3 — the list needs it to show what was found", hits[0].Ref)
		}
	})

	t.Run("without the feature, search is what it was before", func(t *testing.T) {
		// A workspace whose channel has not reached the release that shipped
		// searching by ref must see the old behaviour exactly: REQ-30 finds
		// the artifact that mentions REQ-30 in its body, not REQ-30 itself,
		// and no hit carries a ref.
		hits, err := repo.SearchInProjects(scope, "REQ-30", 20, artifacts.SearchOptions{})
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 1 || hits[0].ArtifactID != citing.ID {
			t.Fatalf("got %d hits, want only the artifact citing REQ-30 in its body", len(hits))
		}
		if hits[0].Ref != "" {
			t.Errorf("Ref = %q, want empty — refs come with the feature", hits[0].Ref)
		}
	})

	t.Run("a ref nobody has matches nothing", func(t *testing.T) {
		hits, err := repo.SearchInProjects(scope, "REQ-9999", 20, refSearch)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("got %+v, want none", hits)
		}
	})
}
