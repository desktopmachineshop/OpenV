package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// TestArtifactSearchInProjects exercises the ILIKE search against a real
// postgres: project scoping, title-before-body ranking, wildcard escaping,
// archived-version exclusion, and the limit.
func TestArtifactSearchInProjects(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewArtifactRepository(db)

	projA := uuid.New().String()
	projB := uuid.New().String()
	projHidden := uuid.New().String()

	save := func(projectID, title, body string) *artifacts.Artifact {
		t.Helper()
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID: projectID,
			Type:      "requirement",
			Title:     title,
			Body:      body,
		})
		if err := repo.Save(a); err != nil {
			t.Fatalf("save %q: %v", title, err)
		}
		return a
	}

	titleHit := save(projA, "Login flow", "users authenticate with a password")
	bodyHit := save(projB, "Session handling", "sessions expire after login timeout")
	save(projHidden, "Login screen", "login belongs to a project outside the scope")
	save(projA, "Unrelated", "nothing to see here")
	pct := save(projA, "Discount rules", "apply a 100% rebate at checkout")

	// Archive an old version: only the current version may match.
	archived := save(projA, "Old login spec", "obsolete")
	archived.Title = "Renamed spec"
	archived.Body = "no matching words anymore"
	archived.Version++
	archived.ValidFrom = time.Now()
	archived.UpdatedAt = time.Now()
	if err := repo.Update(archived); err != nil {
		t.Fatalf("update: %v", err)
	}

	scope := []string{projA, projB}

	t.Run("scopes to the given projects with titles ranked first", func(t *testing.T) {
		hits, err := repo.SearchInProjects(scope, "login", 20)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 2 {
			t.Fatalf("got %d hits %+v, want 2 (out-of-scope and archived matches excluded)", len(hits), hits)
		}
		if hits[0].ArtifactID != titleHit.ID {
			t.Errorf("first hit = %q, want the title match %q", hits[0].Title, titleHit.Title)
		}
		if hits[1].ArtifactID != bodyHit.ID {
			t.Errorf("second hit = %q, want the body match %q", hits[1].Title, bodyHit.Title)
		}
		if hits[1].Snippet == "" || hits[1].Snippet != "sessions expire after login timeout" {
			t.Errorf("snippet = %q, want the body excerpt around the match", hits[1].Snippet)
		}
	})

	t.Run("limit caps the result set", func(t *testing.T) {
		hits, err := repo.SearchInProjects(scope, "login", 1)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 1 || hits[0].ArtifactID != titleHit.ID {
			t.Fatalf("got %+v, want just the ranked-first title match", hits)
		}
	})

	t.Run("wildcards are matched literally", func(t *testing.T) {
		hits, err := repo.SearchInProjects(scope, "100%", 20)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 1 || hits[0].ArtifactID != pct.ID {
			t.Fatalf("got %+v, want only the literal 100%% match", hits)
		}
		hits, err = repo.SearchInProjects(scope, "100%x", 20)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("got %+v, want none — %% must not act as a wildcard", hits)
		}
	})

	t.Run("empty scope returns nothing", func(t *testing.T) {
		hits, err := repo.SearchInProjects(nil, "login", 20)
		if err != nil {
			t.Fatalf("SearchInProjects: %v", err)
		}
		if len(hits) != 0 {
			t.Fatalf("got %+v, want none for an empty project scope", hits)
		}
	})
}
