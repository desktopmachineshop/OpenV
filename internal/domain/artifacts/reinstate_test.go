package artifacts

import (
	"errors"
	"testing"
	"time"
)

// An archived artifact comes back as its last version written again as the
// next, live one: ref, content and status kept, the archived version left
// closed in the history.
func TestReinstateWritesTheLastVersionAgain(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	closed := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	parent := "hdg-1"
	repo.byID = map[string]*Artifact{parent: {ID: parent}}
	repo.versions = []*Artifact{
		{ID: base.ID, ProjectID: base.ProjectID, ParentID: &parent, Type: TypeRequirement, Ref: "REQ-7", Title: "Brake within 2 m",
			Status: StatusApproved, Version: 3, ValidTo: &closed},
		{ID: base.ID, ProjectID: base.ProjectID, Type: TypeRequirement, Ref: "REQ-7", Title: "Brake", Version: 2, ValidTo: &closed},
	}
	back, err := svc.ReinstateArtifact(base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Version != 4 || back.ValidTo != nil || back.Ref != "REQ-7" || back.Title != "Brake within 2 m" ||
		back.Status != StatusApproved || back.ParentID == nil || *back.ParentID != parent {
		t.Fatalf("reinstated %+v", back)
	}
	if repo.updated != back {
		t.Fatal("the reinstated version was not stored")
	}
}

// A parent archived too is not there to sit under: the artifact comes back at
// the top level. A live artifact, or an id no version has, is refused.
func TestReinstateRefusesAndReparents(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	closed := time.Now().UTC()
	gone := "hdg-gone"
	repo.byID = map[string]*Artifact{}
	repo.versions = []*Artifact{{ID: base.ID, ParentID: &gone, Version: 2, ValidTo: &closed}}
	back, err := svc.ReinstateArtifact(base.ID)
	if err != nil || back.ParentID != nil {
		t.Fatalf("reinstated under %v, %v; want the top level", back.ParentID, err)
	}
	repo.versions = []*Artifact{{ID: base.ID, Version: 5}}
	if _, err := svc.ReinstateArtifact(base.ID); !errors.Is(err, ErrNotArchived) {
		t.Errorf("a live artifact: %v, want ErrNotArchived", err)
	}
	repo.versions = nil
	if _, err := svc.ReinstateArtifact("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("no versions: %v, want ErrNotFound", err)
	}
}
