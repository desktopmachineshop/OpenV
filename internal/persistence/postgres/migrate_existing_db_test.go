package postgres

import (
	"testing"
)

// TestMigrateOverAPreExistingWorkItemsTable reproduces a production outage.
//
// The baseline schema is all CREATE ... IF NOT EXISTS, so against a database
// that already has a table, the CREATE TABLE is a no-op and any column added
// to that statement is NOT applied — while an index naming the new column
// still runs, and fails with "column ... does not exist". The baseline runs
// before the numbered migration that would have added the column, so the
// server cannot boot: every deploy crash-loops on a database that already
// has data, while a fresh one is fine. That is exactly the shape of failure
// a test on a fresh database cannot see, which is why this one starts by
// making the database old.
//
// Postgres-gated (OPENV_TEST_DATABASE_URL).
func TestMigrateOverAPreExistingWorkItemsTable(t *testing.T) {
	db := testDB(t)

	// A work_items table as it stood before source_chatter_id existed.
	if _, err := db.Exec(`
		CREATE TABLE work_items (
			id UUID PRIMARY KEY,
			project_id UUID NOT NULL,
			title VARCHAR(512) NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			board_column VARCHAR(32) NOT NULL DEFAULT 'backlog',
			sort_order INT NOT NULL DEFAULT 0,
			assignee_type VARCHAR(32) NOT NULL DEFAULT 'user',
			assignee_id UUID,
			agent_run_id UUID,
			artifact_ids JSONB NOT NULL DEFAULT '[]',
			due_date TIMESTAMP,
			created_by UUID,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		t.Fatalf("seed the pre-existing table: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate over an existing database: %v", err)
	}

	// The column and its index must both be there afterwards, whichever
	// statement put them there.
	var hasColumn bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'work_items' AND column_name = 'source_chatter_id'
		)
	`).Scan(&hasColumn); err != nil {
		t.Fatalf("check the column: %v", err)
	}
	if !hasColumn {
		t.Error("work_items.source_chatter_id missing after migrating an existing database")
	}

	var hasIndex bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'work_items' AND indexname = 'idx_work_items_source_chatter'
		)
	`).Scan(&hasIndex); err != nil {
		t.Fatalf("check the index: %v", err)
	}
	if !hasIndex {
		t.Error("idx_work_items_source_chatter missing after migrating an existing database")
	}

	// Migrating again must be a no-op, not a second failure.
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate is not idempotent: %v", err)
	}
}
