package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// An artifact's history on the database (REQ-4): a restore through the
// service keeps the ref, burning no number, and a deleted artifact's
// versions stay readable, while an id that is not a UUID has none, as an id
// no row has.
func TestAnArtifactsHistoryKeepsItsRefAndOutlivesTheArtifact(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repo := NewArtifactRepository(db)
	svc := artifacts.NewDefaultService(repo)
	projectID := uuid.New().String()
	seedProjects(t, db, projectID)

	req := newTestArtifact(projectID, artifacts.TypeRequirement, "Answer in time")
	if err := svc.CreateArtifact(req); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	title := "Answer in good time"
	if _, err := svc.UpdateArtifact(req.ID, artifacts.UpdateArtifactRequest{Title: &title}); err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}

	restored, err := svc.RestoreArtifactVersion(req.ID, 1)
	if err != nil {
		t.Fatalf("RestoreArtifactVersion: %v", err)
	}
	if restored.Ref != "REQ-1" || restored.Version != 3 || restored.Title != "Answer in time" {
		t.Errorf("restored = %s version %d %q, want REQ-1 at version 3 with version 1's title",
			restored.Ref, restored.Version, restored.Title)
	}
	current, err := repo.FindByID(req.ID)
	if err != nil || current.Ref != "REQ-1" {
		t.Fatalf("stored after the restore = %v, %v; want ref REQ-1", current, err)
	}
	next := newTestArtifact(projectID, artifacts.TypeRequirement, "Keep records")
	if err := svc.CreateArtifact(next); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if next.Ref != "REQ-2" {
		t.Errorf("the next requirement is %s, want REQ-2: the restore drew no number", next.Ref)
	}

	if err := svc.DeleteArtifact(req.ID); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}
	if _, err := repo.FindByID(req.ID); err != artifacts.ErrNotFound {
		t.Fatalf("FindByID after the delete = %v, want ErrNotFound", err)
	}
	versions, err := svc.GetArtifactVersions(req.ID)
	if err != nil {
		t.Fatalf("GetArtifactVersions after the delete: %v", err)
	}
	if len(versions) != 3 || versions[0].Version != 3 || versions[0].ValidTo == nil || versions[0].ProjectID != projectID {
		t.Errorf("a deleted artifact's versions = %d rows, want its 3 versions, the newest closed by the delete", len(versions))
	}

	for _, id := range []string{"not-a-uuid", uuid.New().String()} {
		versions, err := svc.GetArtifactVersions(id)
		if err != nil || len(versions) != 0 {
			t.Errorf("GetArtifactVersions(%q) = %d rows, %v; want none and no error", id, len(versions), err)
		}
	}
}
