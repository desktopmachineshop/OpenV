package postgres

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// ledgerRows returns the schema_migrations ledger ordered by version.
func ledgerRows(t *testing.T, db *sql.DB) map[int]struct {
	name      string
	appliedAt time.Time
} {
	t.Helper()
	rows, err := db.Query(`SELECT version, name, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	defer rows.Close()
	out := map[int]struct {
		name      string
		appliedAt time.Time
	}{}
	for rows.Next() {
		var v int
		var entry struct {
			name      string
			appliedAt time.Time
		}
		if err := rows.Scan(&v, &entry.name, &entry.appliedAt); err != nil {
			t.Fatal(err)
		}
		out[v] = entry
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, table).Scan(&exists)
	if err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	return exists
}

// TestMigrateFreshDatabase: a fresh database gets the full baseline schema
// plus a ledger recording migration 0001.
func TestMigrateFreshDatabase(t *testing.T) {
	db := testDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{"artifacts", "projects", "users", "agents", "organizations", "schema_migrations"} {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %s after Migrate", table)
		}
	}

	ledger := ledgerRows(t, db)
	if len(ledger) != 1 {
		t.Fatalf("expected exactly 1 ledger row, got %d: %v", len(ledger), ledger)
	}
	if ledger[1].name != "baseline" {
		t.Fatalf("expected version 1 named baseline, got %+v", ledger)
	}
}

// TestMigrateSecondBootNoop: a second Migrate on the same database succeeds
// and does not add or rewrite ledger rows.
func TestMigrateSecondBootNoop(t *testing.T) {
	db := testDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	first := ledgerRows(t, db)

	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	second := ledgerRows(t, db)

	if len(second) != len(first) {
		t.Fatalf("second boot changed ledger size: %d -> %d", len(first), len(second))
	}
	if !second[1].appliedAt.Equal(first[1].appliedAt) {
		t.Errorf("second boot rewrote baseline applied_at: %v -> %v", first[1].appliedAt, second[1].appliedAt)
	}
}

// TestMigratePreLedgerDatabase: a database initialized before the ledger
// existed (bare InitSchema, no schema_migrations table) is adopted cleanly —
// Migrate records the baseline without error.
func TestMigratePreLedgerDatabase(t *testing.T) {
	db := testDB(t)

	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema (pre-ledger simulation): %v", err)
	}
	if tableExists(t, db, "schema_migrations") {
		t.Fatal("precondition failed: ledger should not exist yet")
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate on pre-ledger DB: %v", err)
	}
	ledger := ledgerRows(t, db)
	if len(ledger) != 1 || ledger[1].name != "baseline" {
		t.Fatalf("expected adopted baseline row, got %v", ledger)
	}
}

// TestNumberedMigrationRunsExactlyOnce: a migration appended to the registry
// runs once, is recorded, and is skipped on subsequent boots.
func TestNumberedMigrationRunsExactlyOnce(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	runs := 0
	registry := append(append([]Migration{}, migrations...), Migration{
		Version: 9999,
		Name:    "test_fake_table",
		Run: func(tx *sql.Tx) error {
			runs++
			_, err := tx.Exec(`CREATE TABLE migration_fake_9999 (id INT PRIMARY KEY)`)
			return err
		},
	})

	for boot := 1; boot <= 2; boot++ {
		if err := runMigrations(db, registry); err != nil {
			t.Fatalf("boot %d: %v", boot, err)
		}
	}

	if runs != 1 {
		t.Errorf("expected migration body to run exactly once, ran %d times", runs)
	}
	if !tableExists(t, db, "migration_fake_9999") {
		t.Error("expected migration_fake_9999 table to exist")
	}
	ledger := ledgerRows(t, db)
	if ledger[9999].name != "test_fake_table" {
		t.Fatalf("expected ledger row for 9999, got %v", ledger)
	}
}

// TestFailedMigrationRollsBack: a migration that errors mid-way leaves
// neither its DDL nor a ledger row behind, and a fixed retry applies it.
func TestFailedMigrationRollsBack(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	boom := errors.New("boom")
	failing := append(append([]Migration{}, migrations...), Migration{
		Version: 9999,
		Name:    "test_failing",
		Run: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE migration_fake_9999 (id INT PRIMARY KEY)`); err != nil {
				return err
			}
			return boom // fail after DDL: everything must roll back
		},
	})

	err := runMigrations(db, failing)
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom error, got %v", err)
	}
	if tableExists(t, db, "migration_fake_9999") {
		t.Error("failed migration leaked its table; transaction did not roll back")
	}
	if _, ok := ledgerRows(t, db)[9999]; ok {
		t.Error("failed migration was recorded in the ledger")
	}

	// Retry with a fixed body applies cleanly.
	fixed := append(append([]Migration{}, migrations...), Migration{
		Version: 9999,
		Name:    "test_fixed",
		Run: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE migration_fake_9999 (id INT PRIMARY KEY)`)
			return err
		},
	})
	if err := runMigrations(db, fixed); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if !tableExists(t, db, "migration_fake_9999") {
		t.Error("expected table after fixed retry")
	}
}

// TestRegistryValidation: duplicate or descending versions and malformed
// entries are rejected before anything runs.
func TestRegistryValidation(t *testing.T) {
	db := testDB(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	noop := func(tx *sql.Tx) error { return nil }

	if err := runMigrations(db, []Migration{
		{Version: 2, Name: "a", Run: noop},
		{Version: 2, Name: "b", Run: noop},
	}); err == nil {
		t.Error("expected error for duplicate versions")
	}

	if err := runMigrations(db, []Migration{
		{Version: 3, Name: "a", Run: noop},
		{Version: 2, Name: "b", Run: noop},
	}); err == nil {
		t.Error("expected error for descending versions")
	}

	if err := runMigrations(db, []Migration{
		{Version: 2, Name: "neither"},
	}); err == nil {
		t.Error("expected error when neither Run nor RunDB is set")
	}
}
