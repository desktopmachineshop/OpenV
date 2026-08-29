package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/links"
)

// TestLinkDeleteSoftNoOrphan is the issue-#190(c) regression test: Delete must
// soft-delete a link (close valid_to) rather than hard-DELETE the row, so its
// link_artifacts references are never orphaned. It also locks in that Update
// will not resurrect or mutate a soft-deleted link.
func TestLinkDeleteSoftNoOrphan(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewLinkRepository(db)

	artifactID := uuid.New().String()
	link := links.NewLink(links.CreateLinkRequest{
		FromID: artifactID,
		ToID:   uuid.New().String(),
		Type:   "verifies",
	})
	if err := repo.Save(link); err != nil {
		t.Fatalf("save link: %v", err)
	}
	if err := repo.RecordLinkForArtifactVersion(link.ID, artifactID, 1); err != nil {
		t.Fatalf("record link_artifacts: %v", err)
	}

	if err := repo.Delete(link.ID); err != nil {
		t.Fatalf("delete link: %v", err)
	}

	// The link disappears from every read path (all filter valid_to IS NULL).
	if _, err := repo.FindByID(link.ID); err == nil {
		t.Error("FindByID returned a soft-deleted link; want not found")
	}

	// But the row itself survives with valid_to set (tombstone, not hard delete).
	var rowCount int
	var validToSet bool
	if err := db.QueryRow(`
		SELECT COUNT(*), BOOL_OR(valid_to IS NOT NULL) FROM links WHERE id = $1
	`, link.ID).Scan(&rowCount, &validToSet); err != nil {
		t.Fatalf("inspect links row: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("links row count = %d, want 1 (soft delete must not remove the row)", rowCount)
	}
	if !validToSet {
		t.Error("soft-deleted link has NULL valid_to; want a closed validity interval")
	}

	// The link_artifacts reference is not orphaned — it still points at a real
	// (tombstoned) links row.
	var laCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM link_artifacts la
		JOIN links l ON l.id = la.link_id
		WHERE la.link_id = $1
	`, link.ID).Scan(&laCount); err != nil {
		t.Fatalf("inspect link_artifacts: %v", err)
	}
	if laCount != 1 {
		t.Errorf("joined link_artifacts count = %d, want 1 (no orphan)", laCount)
	}

	// Update must not resurrect or mutate a soft-deleted link.
	link.Type = "satisfies"
	if err := repo.Update(link); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := repo.FindByID(link.ID); err == nil {
		t.Error("Update resurrected a soft-deleted link; want it to stay tombstoned")
	}
}
