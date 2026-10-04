package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// TestLinkFindAllReadsValidFrom: the project-wide link list, which every
// export, baseline and report is built from, reads each link's valid_from
// as stored, not the zero time (#379 bug 69).
func TestLinkFindAllReadsValidFrom(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	artifactRepo := NewArtifactRepository(db)
	linkRepo := NewLinkRepository(db)

	project := uuid.New().String()
	var ids []string
	for _, title := range []string{"Requirement", "Test case"} {
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: "requirement", Title: title})
		if err := artifactRepo.Save(a); err != nil {
			t.Fatalf("save artifact: %v", err)
		}
		ids = append(ids, a.ID)
	}

	validFrom := time.Date(2025, 6, 2, 8, 30, 0, 0, time.UTC)
	link := links.NewLink(links.CreateLinkRequest{FromID: ids[1], ToID: ids[0], Type: "verifies"})
	link.ValidFrom = validFrom
	if err := linkRepo.Save(link); err != nil {
		t.Fatalf("save link: %v", err)
	}

	all, err := linkRepo.FindAll(project)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("FindAll = %d links, want 1", len(all))
	}
	if !all[0].ValidFrom.Equal(validFrom) {
		t.Errorf("FindAll valid_from = %v, want %v as stored", all[0].ValidFrom, validFrom)
	}
	if all[0].ValidTo != nil {
		t.Errorf("FindAll valid_to = %v, want none for a live link", *all[0].ValidTo)
	}
}
