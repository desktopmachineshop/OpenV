package postgres

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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

func indexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'public' AND indexname = $1
		)`, index).Scan(&exists)
	if err != nil {
		t.Fatalf("check index %s: %v", index, err)
	}
	return exists
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
	if len(ledger) != len(migrations) {
		t.Fatalf("expected %d ledger rows, got %d: %v", len(migrations), len(ledger), ledger)
	}
	if ledger[1].name != "baseline" {
		t.Fatalf("expected version 1 named baseline, got %+v", ledger)
	}
	if ledger[2].name != "unique_personal_org_per_user" {
		t.Fatalf("expected version 2 named unique_personal_org_per_user, got %+v", ledger)
	}
	if !indexExists(t, db, "idx_organizations_personal_owner") {
		t.Error("expected idx_organizations_personal_owner after Migrate")
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
	if len(ledger) != len(migrations) || ledger[1].name != "baseline" {
		t.Fatalf("expected adopted baseline row plus numbered migrations, got %v", ledger)
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

// TestMigrateConcurrentBoots: several processes booting against the same
// fresh database at once (issue #144). Without the session-level boot lock
// the concurrent baseline re-runs race Postgres catalog uniqueness
// (CREATE TABLE IF NOT EXISTS is not concurrency-safe) and fail sporadically;
// with it every boot serializes and succeeds.
func TestMigrateConcurrentBoots(t *testing.T) {
	db := testDB(t)

	const boots = 4
	errs := make([]error, boots)
	var wg sync.WaitGroup
	for i := 0; i < boots; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Migrate(db)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent boot %d: %v", i, err)
		}
	}
	ledger := ledgerRows(t, db)
	if len(ledger) != len(migrations) {
		t.Fatalf("ledger rows = %d, want %d: %v", len(ledger), len(migrations), ledger)
	}
}

// TestMigrateAndBackfillConcurrentBoots: the full boot sequence — migrations
// plus org backfill — under concurrency. The backfill's personal-org
// check-then-insert must not mint duplicate personal orgs when two processes
// boot a legacy database at the same time.
func TestMigrateAndBackfillConcurrentBoots(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	f := seedLegacy(t, db)

	const boots = 3
	errs := make([]error, boots)
	var wg sync.WaitGroup
	for i := 0; i < boots; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = MigrateAndBackfill(db, f.agentsDir)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent boot %d: %v", i, err)
		}
	}
	// Two seeded users, exactly one personal org each.
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM organizations WHERE org_type = 'personal'`); got != 2 {
		t.Errorf("personal orgs after concurrent boots = %d, want 2", got)
	}
	if got := scalarInt(t, db, `SELECT COUNT(*) FROM org_members`); got != 2 {
		t.Errorf("org memberships after concurrent boots = %d, want 2", got)
	}
}

// TestPersonalOrgUniqueIndex: the 0002 partial unique index rejects a second
// personal org for the same user while leaving company orgs and NULL
// creators unconstrained.
func TestPersonalOrgUniqueIndex(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)

	insertOrg := func(orgType string, createdBy interface{}) error {
		id := uuid.New().String()
		_, err := db.Exec(`
			INSERT INTO organizations (id, name, slug, org_type, created_by)
			VALUES ($1, 'Org', $2, $3, $4)
		`, id, "org-"+id[:8], orgType, createdBy)
		return err
	}

	user := uuid.New().String()
	if err := insertOrg("personal", user); err != nil {
		t.Fatalf("first personal org: %v", err)
	}
	if err := insertOrg("personal", user); err == nil {
		t.Error("second personal org for the same user was allowed")
	} else if !strings.Contains(err.Error(), "idx_organizations_personal_owner") {
		t.Errorf("expected unique-index violation, got: %v", err)
	}

	// Company orgs are not constrained by the partial index.
	if err := insertOrg("company", user); err != nil {
		t.Errorf("first company org: %v", err)
	}
	if err := insertOrg("company", user); err != nil {
		t.Errorf("second company org for the same creator: %v", err)
	}

	// NULL created_by rows are distinct to the index.
	if err := insertOrg("personal", nil); err != nil {
		t.Errorf("personal org with NULL creator: %v", err)
	}
	if err := insertOrg("personal", nil); err != nil {
		t.Errorf("second personal org with NULL creator: %v", err)
	}
}

// TestPersonalOrgUniqueMigrationGuardsExistingDuplicates: a pre-ledger
// database that already holds duplicate personal orgs (the very race 0002
// closes) fails migration 0002 with an actionable error instead of an opaque
// CREATE INDEX failure, stays unapplied for retry, and migrates cleanly once
// the duplicates are resolved.
func TestPersonalOrgUniqueMigrationGuardsExistingDuplicates(t *testing.T) {
	db := testDB(t)
	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema (pre-ledger simulation): %v", err)
	}

	user := uuid.New().String()
	dupeID := uuid.New().String()
	for i, id := range []string{uuid.New().String(), dupeID} {
		if _, err := db.Exec(`
			INSERT INTO organizations (id, name, slug, org_type, created_by)
			VALUES ($1, 'Dupe', $2, 'personal', $3)
		`, id, "dupe-"+id[:8], user); err != nil {
			t.Fatalf("seed duplicate %d: %v", i, err)
		}
	}

	err := Migrate(db)
	if err == nil {
		t.Fatal("Migrate succeeded despite duplicate personal orgs")
	}
	if !strings.Contains(err.Error(), user) {
		t.Errorf("error should name the offending user, got: %v", err)
	}
	if indexExists(t, db, "idx_organizations_personal_owner") {
		t.Error("index was created despite the failed migration")
	}
	if _, ok := ledgerRows(t, db)[2]; ok {
		t.Error("failed migration 0002 was recorded in the ledger")
	}

	// Resolve the duplicate; the next boot applies 0002 cleanly.
	if _, err := db.Exec(`DELETE FROM organizations WHERE id = $1`, dupeID); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate after resolving duplicates: %v", err)
	}
	if !indexExists(t, db, "idx_organizations_personal_owner") {
		t.Error("expected index after clean retry")
	}
}
