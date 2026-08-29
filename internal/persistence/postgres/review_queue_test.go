package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// TestFindSuspectByProject exercises the review-queue suspect-link query
// (issue #183): it returns the live suspect links touching the project on
// either endpoint, enriched with each endpoint's title/type, and excludes
// trusted links and links belonging to other projects.
func TestFindSuspectByProject(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	artRepo := NewArtifactRepository(db)
	linkRepo := NewLinkRepository(db)

	projectA := uuid.New().String()
	projectB := uuid.New().String()

	mkArtifact := func(projectID, title, typ string) *artifacts.Artifact {
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
			ProjectID: projectID,
			Type:      typ,
			Title:     title,
		})
		if err := artRepo.Save(a); err != nil {
			t.Fatalf("save artifact: %v", err)
		}
		return a
	}

	req := mkArtifact(projectA, "REQ-1", "requirement")
	tc := mkArtifact(projectA, "TC-1", "test-case")
	other := mkArtifact(projectB, "OTHER", "requirement")

	mkLink := func(from, to string) *links.Link {
		l := links.NewLink(links.CreateLinkRequest{FromID: from, ToID: to, Type: "verifies"})
		if err := linkRepo.Save(l); err != nil {
			t.Fatalf("save link: %v", err)
		}
		return l
	}

	suspectLink := mkLink(req.ID, tc.ID)      // both endpoints in project A
	trustedLink := mkLink(req.ID, tc.ID)      // stays trusted -> excluded
	foreignLink := mkLink(other.ID, other.ID) // project B only -> excluded

	if err := linkRepo.SetSuspect(suspectLink.ID, true); err != nil {
		t.Fatalf("SetSuspect: %v", err)
	}
	if err := linkRepo.SetSuspect(foreignLink.ID, true); err != nil {
		t.Fatalf("SetSuspect (foreign): %v", err)
	}
	_ = trustedLink

	got, err := linkRepo.FindSuspectByProject(projectA)
	if err != nil {
		t.Fatalf("FindSuspectByProject: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d suspect links, want 1: %+v", len(got), got)
	}
	sl := got[0]
	if sl.ID != suspectLink.ID {
		t.Errorf("id = %q, want %q", sl.ID, suspectLink.ID)
	}
	if sl.FromTitle != "REQ-1" || sl.FromType != "requirement" {
		t.Errorf("from enrichment = %q/%q, want REQ-1/requirement", sl.FromTitle, sl.FromType)
	}
	if sl.ToTitle != "TC-1" || sl.ToType != "test-case" {
		t.Errorf("to enrichment = %q/%q, want TC-1/test-case", sl.ToTitle, sl.ToType)
	}
}

// TestFindSuspectByProjectMatchesEitherEndpoint: a suspect link is in a
// project's queue when EITHER endpoint belongs to the project, even if the
// other endpoint lives in a different project.
func TestFindSuspectByProjectMatchesEitherEndpoint(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	artRepo := NewArtifactRepository(db)
	linkRepo := NewLinkRepository(db)

	projectA := uuid.New().String()
	projectB := uuid.New().String()

	inA := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: projectA, Type: "requirement", Title: "A"})
	inB := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: projectB, Type: "design-item", Title: "B"})
	for _, a := range []*artifacts.Artifact{inA, inB} {
		if err := artRepo.Save(a); err != nil {
			t.Fatalf("save artifact: %v", err)
		}
	}

	// Link goes B -> A (to endpoint is in project A).
	l := links.NewLink(links.CreateLinkRequest{FromID: inB.ID, ToID: inA.ID, Type: "satisfies"})
	if err := linkRepo.Save(l); err != nil {
		t.Fatalf("save link: %v", err)
	}
	if err := linkRepo.SetSuspect(l.ID, true); err != nil {
		t.Fatalf("SetSuspect: %v", err)
	}

	got, err := linkRepo.FindSuspectByProject(projectA)
	if err != nil {
		t.Fatalf("FindSuspectByProject: %v", err)
	}
	if len(got) != 1 || got[0].ID != l.ID {
		t.Fatalf("project A queue = %+v, want the cross-project link %s", got, l.ID)
	}
}

// TestFindByProjectAndStatus verifies the in-review artifact query (issue
// #183): it returns only the project's current artifacts in the requested
// status, and never historical versions or other projects' rows.
func TestFindByProjectAndStatus(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewArtifactRepository(db)
	svc := artifacts.NewDefaultService(repo)

	projectA := uuid.New().String()
	projectB := uuid.New().String()

	mkInReview := func(projectID, title string) *artifacts.Artifact {
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: projectID, Type: "requirement", Title: title})
		if err := repo.Save(a); err != nil {
			t.Fatalf("save artifact: %v", err)
		}
		// draft -> in_review through the real state machine.
		if _, err := svc.ChangeStatus(a.ID, artifacts.StatusInReview); err != nil {
			t.Fatalf("ChangeStatus: %v", err)
		}
		return a
	}

	reviewA := mkInReview(projectA, "A in review")
	mkInReview(projectB, "B in review") // other project -> excluded

	// A draft in project A -> excluded by status.
	draft := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: projectA, Type: "requirement", Title: "A draft"})
	if err := repo.Save(draft); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	got, err := repo.FindByProjectAndStatus(projectA, artifacts.StatusInReview)
	if err != nil {
		t.Fatalf("FindByProjectAndStatus: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d in-review artifacts, want 1: %+v", len(got), got)
	}
	if got[0].ID != reviewA.ID {
		t.Errorf("id = %q, want %q", got[0].ID, reviewA.ID)
	}
	if got[0].Status != artifacts.StatusInReview {
		t.Errorf("status = %q, want %q", got[0].Status, artifacts.StatusInReview)
	}
}
