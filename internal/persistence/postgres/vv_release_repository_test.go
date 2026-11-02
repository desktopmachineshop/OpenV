package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/targetreleases"
)

func TestTargetReleaseRepositoryRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	project := uuid.New().String()
	seedProjects(t, db, project)
	repo := NewTargetReleaseRepository(db)
	svc := targetreleases.NewDefaultService(repo, repo.CountTargeting)

	if list, err := svc.List(project); err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty list: %v %v", list, err)
	}
	date := "2027-03-01"
	mvp, err := svc.Create(project, targetreleases.CreateRequest{Name: "MVP", TargetDate: &date, Description: "first"})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := svc.Create(project, targetreleases.CreateRequest{Name: "V1.0"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(mvp.ID)
	if err != nil || got.Name != "MVP" || got.TargetDate == nil || *got.TargetDate != date ||
		got.Description != "first" || got.Status != targetreleases.StatusPlanned || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("get: %+v %v", got, err)
	}
	// The unique index catches a name the service's check raced past.
	clash := *v1
	clash.ID = uuid.New().String()
	clash.Name = "mvp"
	if err := repo.Create(&clash); !errors.Is(err, targetreleases.ErrNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}
	released := targetreleases.StatusReleased
	if up, err := svc.Update(mvp.ID, targetreleases.UpdateRequest{Status: &released}); err != nil || up.ReleasedAt == nil {
		t.Fatalf("update: %+v %v", up, err)
	}
	if got, _ := svc.Get(mvp.ID); got.ReleasedAt == nil || got.Status != released {
		t.Fatalf("released not stored: %+v", got)
	}
	list, err := svc.Reorder(project, []string{v1.ID, mvp.ID})
	if err != nil || list[0].ID != v1.ID || list[1].Position != 1 {
		t.Fatalf("reorder: %+v %v", list, err)
	}

	// An artifact targeting the release keeps it from being deleted, and
	// only the current version counts.
	art := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO artifacts (id, project_id, type, title, body, sort_order, version, attributes)
		VALUES ($1, $2, 'requirement', 'R', '', 0, 1, $3)`, art, project, `{"target_release":"`+v1.ID+`","target_release_until":"`+mvp.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(mvp.ID); !errors.Is(err, targetreleases.ErrInUse) {
		t.Fatalf("delete targeted (until): %v", err)
	}
	if _, err := db.Exec(`UPDATE artifacts SET valid_to = NOW() WHERE id = $1`, art); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(mvp.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(mvp.ID); !errors.Is(err, targetreleases.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if _, err := svc.Get("not-a-uuid"); !errors.Is(err, targetreleases.ErrNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
	// Deleting the project takes its releases.
	if _, err := db.Exec(`DELETE FROM artifacts WHERE project_id = $1`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE id = $1`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(v1.ID); !errors.Is(err, targetreleases.ErrNotFound) {
		t.Fatalf("release outlived its project: %v", err)
	}
}
