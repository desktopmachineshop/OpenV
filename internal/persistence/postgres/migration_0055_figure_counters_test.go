package postgres

import (
	"testing"

	"github.com/google/uuid"
)

// Migration 0055 deletes the figure counters of artifacts no row has, which
// purges before #379's bug 138 left behind, and keeps every counter of an
// artifact that has a row, a deleted artifact's included (#379 question 48).
func TestMigration55DeletesFigureCountersWithNoArtifact(t *testing.T) {
	db := testDB(t)
	var before []Migration
	for _, m := range migrations {
		if m.Version < 55 {
			before = append(before, m)
		}
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0054: %v", err)
	}
	projectID, live, deleted, purged := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	seedProjects(t, db, projectID)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title) VALUES ($1, $2, 'requirement', 'Live')`, live, projectID)
	rtSeed(t, db, `INSERT INTO artifacts (id, project_id, type, title, valid_to) VALUES ($1, $2, 'requirement', 'Deleted', NOW())`,
		deleted, projectID)
	rtSeed(t, db, `INSERT INTO attachment_figure_counters (artifact_id, next_num) VALUES ($1, 3), ($2, 2), ($3, 5)`, live, deleted, purged)

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for id, want := range map[string]int{live: 1, deleted: 1, purged: 0} {
		if n := countRows(t, db, "attachment_figure_counters", "artifact_id", id); n != want {
			t.Errorf("figure counters of artifact %s after migration 0055: %d, want %d", id, n, want)
		}
	}
}
