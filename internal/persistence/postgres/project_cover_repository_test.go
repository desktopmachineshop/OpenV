package postgres

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// A cover photo's life in project_covers: none at first, stored, replaced
// in place, stamped for only the projects that have one, removed (twice
// without error), and taken with its project when the project is deleted.
// Malformed and unknown ids answer as a project with no cover.
func TestProjectCoverRepositoryRoundTrip(t *testing.T) {
	db := rtDB(t)
	orgID := uuid.New().String()
	rtSeedOrg(t, db, orgID)
	projectRepo := NewProjectRepository(db)
	repo := NewProjectCoverRepository(db)
	mk := func(name string) string {
		p := projects.NewProject(projects.CreateProjectRequest{Name: name})
		p.OrgID = orgID
		if err := projectRepo.Create(p); err != nil {
			t.Fatalf("create project: %v", err)
		}
		return p.ID
	}
	a, b := mk("With a cover"), mk("Without one")

	for _, id := range []string{a, "not-a-uuid", uuid.New().String()} {
		if c, err := repo.GetCover(id); c != nil || err != projects.ErrNoCover {
			t.Errorf("GetCover(%q) before any: %v, %v; want nil and ErrNoCover", id, c, err)
		}
	}
	if _, err := repo.SetCover(uuid.New().String(), "image/png", []byte("x")); err != projects.ErrNotFound {
		t.Errorf("SetCover on an id no project has: %v, want ErrNotFound", err)
	}

	first, err := repo.SetCover(a, "image/png", []byte("png bytes"))
	if err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	second, err := repo.SetCover(a, "image/webp", []byte("webp bytes"))
	if err != nil {
		t.Fatalf("SetCover again: %v", err)
	}
	if !second.After(first) || second.Location() != time.UTC {
		t.Errorf("replaced at %v after %v; want later, in UTC", second, first)
	}
	c, err := repo.GetCover(a)
	if err != nil || c.Mime != "image/webp" || !bytes.Equal(c.Data, []byte("webp bytes")) || !c.UpdatedAt.Equal(second) {
		t.Fatalf("GetCover after replace: %+v, %v; want the WebP stored at %v", c, err, second)
	}

	stamps, err := repo.CoverStamps([]string{a, b, "not-a-uuid"})
	if err != nil || len(stamps) != 1 || !stamps[a].Equal(second) {
		t.Errorf("CoverStamps: %v, %v; want only %s at %v", stamps, err, a, second)
	}
	if stamps, err := repo.CoverStamps(nil); err != nil || len(stamps) != 0 {
		t.Errorf("CoverStamps(nil): %v, %v; want an empty map", stamps, err)
	}

	for i := 0; i < 2; i++ {
		if err := repo.DeleteCover(a); err != nil {
			t.Fatalf("DeleteCover %d: %v", i+1, err)
		}
	}
	if _, err := repo.GetCover(a); err != projects.ErrNoCover {
		t.Errorf("GetCover after delete: %v, want ErrNoCover", err)
	}

	if _, err := repo.SetCover(b, "image/png", []byte("png")); err != nil {
		t.Fatalf("SetCover b: %v", err)
	}
	if _, err := projectRepo.Delete(b); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_covers WHERE project_id = $1`, b).Scan(&left); err != nil || left != 0 {
		t.Errorf("covers left after the project's delete: %d, %v; want 0", left, err)
	}
}
