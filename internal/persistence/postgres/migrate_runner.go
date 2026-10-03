// The migration runner (refactor plan M10; the declmove spec came from
// internal/tools/liftmigrations): the boot lock, the ledger, Migrate and
// MigrateAndBackfill, the runner that applies the registry in
// migrations.go, and the extension reconcile. The stored-data freeze
// hashes every declaration here (testdata/freeze/every_boot.txt).

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// migrationLockKey is the pg_advisory_xact_lock key that serializes
// concurrent migration attempts (e.g. two API replicas booting at once).
// Arbitrary but stable: ASCII "openv" as an int64.
const migrationLockKey int64 = 0x6f70656e76

// bootLockKey is the session-level advisory lock key that serializes the
// ENTIRE boot sequence — ledger creation, the re-run baseline, numbered
// migrations, and the org backfill (see withBootLock). Two processes booting
// at once otherwise race the non-transactional parts: concurrent
// CREATE TABLE IF NOT EXISTS can fail on catalog uniqueness, and the
// backfill's check-then-insert can mint duplicate personal orgs.
//
// The key MUST differ from migrationLockKey: session- and transaction-level
// advisory locks share one lock space, so if applyOnce requested the same
// key the boot holds at session level (on a different pooled connection), it
// would deadlock against itself.
const bootLockKey int64 = 0x6f70656e7601 // "openv" + 0x01

// withBootLock takes bootLockKey as a session-level advisory lock on a
// dedicated connection, runs fn (whose statements may use any pooled
// connection — the lock serializes processes, not statements), and unlocks.
// pg_advisory_lock blocks until the lock is free, so concurrent booters
// simply queue.
func withBootLock(db *sql.DB, fn func() error) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire boot-lock connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, bootLockKey); err != nil {
		return fmt.Errorf("failed to take boot advisory lock: %w", err)
	}
	defer func() {
		// Best effort: closing the connection also releases session locks.
		_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, bootLockKey)
	}()
	return fn()
}

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
// transaction. The whole sequence runs under the boot advisory lock so
// concurrent booting processes serialize instead of racing the
// non-transactional baseline.
func Migrate(db *sql.DB) error {
	return withBootLock(db, func() error { return migrateLocked(db) })
}

// MigrateAndBackfill is the boot-time entry point (cmd/server/main.go): the
// schema migration plus the idempotent org backfill, all under one hold of
// the boot advisory lock so a concurrently booting process cannot interleave
// with any part of the sequence (issue #144: BackfillOrgs's personal-org
// check-then-insert raced under concurrent boots).
func MigrateAndBackfill(db *sql.DB, agentsDir string) error {
	return withBootLock(db, func() error {
		if err := migrateLocked(db); err != nil {
			return err
		}
		if err := BackfillOrgs(db, agentsDir); err != nil {
			return fmt.Errorf("org backfill: %w", err)
		}
		return nil
	})
}

// migrateLocked is Migrate's body; callers hold the boot advisory lock.
func migrateLocked(db *sql.DB) error {
	if _, err := db.Exec(createLedgerSQL); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	if err := runMigrations(db, migrations); err != nil {
		return err
	}
	// After the ledger is up to date, repair anything the extension-guarded
	// migrations skipped because their extension was absent at migration time
	// (issue #241). Runs under the same boot advisory lock as the migrations.
	return reconcileGuardedExtensions(db)
}

// reconcileGuardedExtensions repairs the objects that the extension-guarded
// migrations (0009 pg_trgm, 0016 vector) skip when their extension is absent at
// migration time. Because those migrations still record themselves in the
// ledger when they skip the guarded DDL (boot must never brick on a managed
// Postgres that forbids CREATE EXTENSION), simply enabling the extension later
// never re-runs them — so the trigram indexes / embeddings table would never
// get created. This boot-time reconcile closes that gap: once the extension is
// present it creates the missing objects idempotently (IF NOT EXISTS, still
// guarded by an extension probe), and it is a clean no-op while the extension
// is still absent.
//
// It is deliberately best-effort and never fails boot: an extension probe
// error is surfaced (it signals a broken connection the migrations would also
// have hit), but a failure to create a guarded object is logged and swallowed,
// leaving search degraded exactly as it was before rather than bricking boot.
func reconcileGuardedExtensions(db *sql.DB) error {
	if err := reconcileTrgmIndexes(db); err != nil {
		return err
	}
	return reconcileVectorEmbeddings(db)
}

// extensionPresent reports whether the named extension is installed.
func extensionPresent(db *sql.DB, name string) (bool, error) {
	var present bool
	err := db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, name,
	).Scan(&present)
	return present, err
}

// reconcileTrgmIndexes creates the migration-0009 trigram indexes when pg_trgm
// is present but they are missing (e.g. the extension was enabled after 0009
// skipped its DDL). No-op when pg_trgm is absent.
func reconcileTrgmIndexes(db *sql.DB) error {
	present, err := extensionPresent(db, "pg_trgm")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_artifacts_title_trgm
			ON artifacts USING gin (title gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_body_trgm
			ON artifacts USING gin (body gin_trgm_ops)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			slog.Warn("reconcile: pg_trgm is present but creating a trigram search index failed; cross-project search stays on the sequential-scan fallback",
				slog.Any("error", err))
		}
	}
	return nil
}

// reconcileVectorEmbeddings creates the migration-0016 artifact_embeddings
// table and its HNSW index when the vector extension is present but they are
// missing (e.g. the extension was enabled after 0016 skipped its DDL). No-op
// when the vector extension is absent.
func reconcileVectorEmbeddings(db *sql.DB) error {
	present, err := extensionPresent(db, "vector")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if _, err := db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS artifact_embeddings (
			artifact_id UUID PRIMARY KEY,
			artifact_version INT NOT NULL,
			embedding vector(%d) NOT NULL,
			model VARCHAR NOT NULL,
			content_hash VARCHAR NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, embeddingDimensions)); err != nil {
		slog.Warn("reconcile: vector extension is present but creating artifact_embeddings failed; semantic search stays unavailable",
			slog.Any("error", err))
		return nil
	}
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_artifact_embeddings_hnsw
		ON artifact_embeddings USING hnsw (embedding vector_cosine_ops)
	`); err != nil {
		slog.Warn("reconcile: created artifact_embeddings but building its HNSW index failed; semantic search stays unavailable",
			slog.Any("error", err))
	}
	return nil
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
