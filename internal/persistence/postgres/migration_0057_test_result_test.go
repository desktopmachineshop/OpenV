package postgres

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// attachmentsBefore57 is a database as it stood before migration 0057 on a
// deployment: every earlier migration applied, and attachments.test_result_id
// with its index, as the 0001 baseline used to add them, with one figure.
func attachmentsBefore57(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := testDB(t)
	var before []Migration
	for _, m := range migrations {
		if m.Version < 57 {
			before = append(before, m)
		}
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0056: %v", err)
	}
	rtSeed(t, db, `DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'attachments' AND column_name = 'test_result_id') THEN
			ALTER TABLE attachments ADD COLUMN test_result_id UUID;
		END IF;
	END $$`)
	rtSeed(t, db, `CREATE INDEX IF NOT EXISTS idx_attachments_test_result_id ON attachments(test_result_id)`)
	artifact := uuid.New().String()
	rtSeed(t, db, `INSERT INTO attachments (id, artifact_id, filename, mime_type, file_path, file_size) VALUES ($1, $2, 'f.png', 'image/png', '/u/f.png', 1)`,
		uuid.New().String(), artifact)
	return db, artifact
}

// columnPresent reports whether table has column.
func columnPresent(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var present bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2)`, table, column).Scan(&present); err != nil {
		t.Fatal(err)
	}
	return present
}

// Migration 0057 drops attachments.test_result_id, which nothing writes,
// with its index, when every row holds NULL in it (#379 bug 150, question
// 49), and the boots after it do not put it back: the 0001 baseline, which
// runs before the numbered migrations on every boot, added it whenever it
// was missing. A figure's row stays as it was.
func TestMigration57DropsTheUnusedTestResultColumn(t *testing.T) {
	db, artifact := attachmentsBefore57(t)
	for boot := 1; boot <= 2; boot++ {
		if err := Migrate(db); err != nil {
			t.Fatalf("boot %d: migrate: %v", boot, err)
		}
		if columnPresent(t, db, "attachments", "test_result_id") {
			t.Errorf("after boot %d attachments still has test_result_id", boot)
		}
		var index sql.NullString
		if err := db.QueryRow(`SELECT to_regclass('idx_attachments_test_result_id')::text`).Scan(&index); err != nil {
			t.Fatal(err)
		}
		if index.Valid {
			t.Errorf("after boot %d the index idx_attachments_test_result_id is still there", boot)
		}
	}
	if n := countRows(t, db, "attachments", "artifact_id", artifact); n != 1 {
		t.Errorf("%d figure rows after the migration, want the one there was", n)
	}
	var nullable string
	if err := db.QueryRow(`SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'attachments' AND column_name = 'artifact_id'`).Scan(&nullable); err != nil || nullable != "YES" {
		t.Errorf("attachments.artifact_id is_nullable = %q, %v; want YES, as before", nullable, err)
	}
}

// A row that names a test result was written outside OpenV, and dropping
// the column would lose what it says: migration 0057 fails, naming how many
// rows hold one and what to do, and leaves the column, the row and the
// ledger as they were (#379 question 49).
func TestMigration57RefusesToDropATestResultARowNames(t *testing.T) {
	db, artifact := attachmentsBefore57(t)
	result := uuid.New().String()
	rtSeed(t, db, `UPDATE attachments SET test_result_id = $2 WHERE artifact_id = $1`, artifact, result)

	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "1 attachments rows hold a test_result_id") {
		t.Fatalf("migrate with a row naming a test result = %v, want the refusal naming the one row", err)
	}
	if !columnPresent(t, db, "attachments", "test_result_id") {
		t.Fatal("the refused migration dropped attachments.test_result_id")
	}
	if n := countRows(t, db, "attachments", "test_result_id", result); n != 1 {
		t.Errorf("%d rows name the test result after the refusal, want 1", n)
	}
	if applied, err := migrationApplied(db, 57); err != nil || applied {
		t.Errorf("the ledger records migration 0057 after its refusal: %v, %v", applied, err)
	}

	rtSeed(t, db, `UPDATE attachments SET test_result_id = NULL`)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate once the row is cleared: %v", err)
	}
	if columnPresent(t, db, "attachments", "test_result_id") {
		t.Error("attachments still has test_result_id once the row is cleared and the server has booted again")
	}
}
