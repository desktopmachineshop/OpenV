package postgres

import (
	"database/sql"
	"fmt"
)

// This file implements the numbered schema-migration ledger (issue #38).
//
// Every schema change after the 0001 baseline is written as a Migration and
// appended to the registry below. The runner applies each numbered migration
// exactly once, inside its own transaction, and records it in the
// schema_migrations table. The ledger row and the DDL commit together, so a
// failed migration leaves neither behind.
//
// Baseline decision: migration 0001 ("baseline") wraps the legacy idempotent
// init chain (InitSchema and the schema_*.go files) and is re-executed on
// EVERY boot, even when the ledger already records it. Rationale:
//   - Databases deployed before the ledger existed have no schema_migrations
//     table but may also be missing recent additive DDL; re-running the
//     baseline preserves today's upgrade semantics for them with no special
//     casing (fresh installs, pre-ledger upgrades, and post-ledger boots all
//     take the same code path).
//   - The cost is a few dozen Exec round-trips of IF NOT EXISTS / guarded
//     DO $$ blocks — milliseconds against a warm local Postgres.
// The baseline is frozen: do not add DDL to InitSchema or the schema_*.go
// files anymore. New schema changes go into numbered migrations (0002+).
//
// BackfillOrgs / PromoteOrgColumns intentionally stay outside the ledger as
// boot-time idempotent data-migration steps (they guard themselves and
// depend on runtime state such as the agents directory); see
// cmd/server/main.go.

// Migration is one numbered schema change.
type Migration struct {
	// Version is the migration number (0001, 0002, ...). Versions must be
	// unique and strictly ascending in the registry.
	Version int

	// Name is a short human-readable label recorded in the ledger.
	Name string

	// Run applies the migration inside a dedicated transaction. The runner
	// commits the transaction together with the ledger row, so on error the
	// whole migration rolls back and is not recorded. All new migrations
	// (0002 onward) must set Run.
	Run func(tx *sql.Tx) error

	// RunDB is the escape hatch used only by the 0001 baseline: an
	// idempotent body that manages its own statements on the raw connection
	// and is re-executed on every boot (see the baseline decision above).
	// Exactly one of Run / RunDB must be set.
	RunDB func(db *sql.DB) error
}

// migrations is the ordered registry of schema changes. Append new
// migrations at the end with the next version number; never renumber,
// reorder, or edit an entry that has shipped.
var migrations = []Migration{
	{Version: 1, Name: "baseline", RunDB: InitSchema},
	// {Version: 2, Name: "add_widgets_table", Run: func(tx *sql.Tx) error {
	//     _, err := tx.Exec(`CREATE TABLE widgets (...)`)
	//     return err
	// }},
}

// migrationLockKey is the pg_advisory_xact_lock key that serializes
// concurrent migration attempts (e.g. two API replicas booting at once).
// Arbitrary but stable: ASCII "openv" as an int64.
const migrationLockKey int64 = 0x6f70656e76

const createLedgerSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INT PRIMARY KEY,
	name TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

// Migrate brings the database to the current schema version: it creates the
// ledger table if needed, re-runs the idempotent 0001 baseline, and applies
// any unapplied numbered migrations in order, each exactly once in its own
// transaction. It is the boot-time entry point (cmd/server/main.go).
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(createLedgerSQL); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	return runMigrations(db, migrations)
}

// runMigrations applies the given registry in order. Split from Migrate so
// tests can drive a registry with extra entries.
func runMigrations(db *sql.DB, registry []Migration) error {
	// Validate the whole registry before applying anything.
	prev := 0
	for _, m := range registry {
		if m.Version <= prev {
			return fmt.Errorf("migration registry corrupt: version %04d after %04d (must be unique and ascending)", m.Version, prev)
		}
		prev = m.Version
		if (m.Run == nil) == (m.RunDB == nil) {
			return fmt.Errorf("migration %04d %s: exactly one of Run / RunDB must be set", m.Version, m.Name)
		}
	}

	for _, m := range registry {
		var err error
		if m.RunDB != nil {
			err = applyEveryBoot(db, m)
		} else {
			err = applyOnce(db, m)
		}
		if err != nil {
			return fmt.Errorf("migration %04d %s: %w", m.Version, m.Name, err)
		}
	}
	return nil
}

// applyEveryBoot runs an idempotent baseline-style migration on the raw
// connection and records it in the ledger if not already recorded.
func applyEveryBoot(db *sql.DB, m Migration) error {
	if err := m.RunDB(db); err != nil {
		return err
	}
	_, err := db.Exec(`
		INSERT INTO schema_migrations (version, name) VALUES ($1, $2)
		ON CONFLICT (version) DO NOTHING
	`, m.Version, m.Name)
	return err
}

// applyOnce runs a numbered migration exactly once: it skips versions the
// ledger already records, and otherwise applies the migration and the ledger
// insert in one transaction, serialized across concurrent processes by an
// advisory lock.
func applyOnce(db *sql.DB, m Migration) error {
	applied, err := migrationApplied(db, m.Version)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	// Serialize with other booting processes; released at commit/rollback.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, migrationLockKey); err != nil {
		return err
	}
	// Re-check under the lock: another process may have applied it between
	// the fast-path check and lock acquisition.
	var exists bool
	if err := tx.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, m.Version,
	).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	if err := m.Run(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// migrationApplied reports whether the ledger records the given version.
func migrationApplied(db *sql.DB, version int) (bool, error) {
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&exists)
	return exists, err
}
