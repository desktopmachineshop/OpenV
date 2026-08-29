package postgres

import (
	"fmt"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"

	"github.com/google/uuid"
)

// TestArtifactPageQueries exercises the issue-#136 paged listing: pages in
// stable tree order partition exactly what FindByProjectID returns, the type
// filter applies to both page and count, and superseded (valid_to) versions
// stay invisible.
func TestArtifactPageQueries(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewArtifactRepository(db)

	projectID := uuid.New().String()
	otherProject := uuid.New().String()

	var rootID string
	const total = 9
	for i := 0; i < total; i++ {
		typ := "requirement"
		if i%3 == 0 {
			typ = "test-case"
		}
		req := artifacts.CreateArtifactRequest{
			ProjectID: projectID,
			Type:      typ,
			Title:     fmt.Sprintf("artifact %d", i),
			// Identical sort_order everywhere forces the created_at/id
			// tiebreakers to keep the page order stable.
			SortOrder: intPtr(1),
		}
		if rootID != "" && i%2 == 1 {
			req.ParentID = &rootID
		}
		a := artifacts.NewArtifact(req)
		if err := repo.Save(a); err != nil {
			t.Fatalf("Save(%d): %v", i, err)
		}
		if i == 0 {
			rootID = a.ID
		}
	}
	// Noise: another project's artifact and a superseded row must not count.
	if err := repo.Save(artifacts.NewArtifact(artifacts.CreateArtifactRequest{
		ProjectID: otherProject, Type: "requirement", Title: "other project", SortOrder: intPtr(1),
	})); err != nil {
		t.Fatalf("Save other-project artifact: %v", err)
	}
	superseded := artifacts.NewArtifact(artifacts.CreateArtifactRequest{
		ProjectID: projectID, Type: "requirement", Title: "old version", SortOrder: intPtr(1),
	})
	if err := repo.Save(superseded); err != nil {
		t.Fatalf("Save superseded artifact: %v", err)
	}
	if err := repo.Delete(superseded.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	count, err := repo.CountByProject(projectID, "")
	if err != nil {
		t.Fatalf("CountByProject: %v", err)
	}
	if count != total {
		t.Errorf("CountByProject = %d, want %d", count, total)
	}
	tcCount, err := repo.CountByProject(projectID, "test-case")
	if err != nil {
		t.Fatalf("CountByProject(test-case): %v", err)
	}
	if tcCount != 3 {
		t.Errorf("CountByProject(test-case) = %d, want 3", tcCount)
	}

	full, err := repo.FindByProjectID(projectID)
	if err != nil {
		t.Fatalf("FindByProjectID: %v", err)
	}
	if len(full) != total {
		t.Fatalf("FindByProjectID returned %d artifacts, want %d", len(full), total)
	}

	// Pages of 4 must partition the project without duplicates or gaps.
	var paged []*artifacts.Artifact
	for offset := 0; ; offset += 4 {
		page, err := repo.FindPageByProject(projectID, "", 4, offset)
		if err != nil {
			t.Fatalf("FindPageByProject(offset=%d): %v", offset, err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, page...)
		if offset > total {
			t.Fatal("paging did not terminate")
		}
	}
	if len(paged) != total {
		t.Fatalf("paged walk returned %d artifacts, want %d", len(paged), total)
	}
	seen := map[string]bool{}
	for _, a := range paged {
		if seen[a.ID] {
			t.Errorf("artifact %s appeared in two pages", a.ID)
		}
		seen[a.ID] = true
	}
	for _, a := range full {
		if !seen[a.ID] {
			t.Errorf("artifact %s missing from paged walk", a.ID)
		}
	}

	// Type filter pages too.
	tcPage, err := repo.FindPageByProject(projectID, "test-case", 10, 0)
	if err != nil {
		t.Fatalf("FindPageByProject(test-case): %v", err)
	}
	if len(tcPage) != 3 {
		t.Errorf("test-case page = %d rows, want 3", len(tcPage))
	}
	for _, a := range tcPage {
		if a.Type != "test-case" {
			t.Errorf("type filter leaked a %q artifact", a.Type)
		}
	}
}

func intPtr(v int) *int { return &v }
