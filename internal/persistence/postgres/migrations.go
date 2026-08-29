package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
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
// depend on runtime state such as the agents directory), but they run under
// the same boot advisory lock via MigrateAndBackfill so concurrent boots
// cannot interleave with them.

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

	// 0002: at most one personal organization per user. Personal orgs are
	// always created with created_by = the owning user (signup's
	// EnsurePersonalOrg and the boot backfill both do), so a partial unique
	// index on created_by closes the check-then-insert races in both paths.
	// NULL created_by rows (possible on hand-edited data) are not
	// constrained — Postgres treats NULLs as distinct — which is the safe
	// direction. Existing duplicates would make CREATE INDEX fail with an
	// opaque error, so the migration checks first and fails with an
	// actionable message; the transaction rolls back and the ledger stays
	// unapplied, so a fixed database retries cleanly on the next boot.
	{Version: 2, Name: "unique_personal_org_per_user", Run: func(tx *sql.Tx) error {
		rows, err := tx.Query(`
			SELECT created_by::text FROM organizations
			WHERE org_type = 'personal' AND created_by IS NOT NULL
			GROUP BY created_by HAVING COUNT(*) > 1
		`)
		if err != nil {
			return err
		}
		var dupes []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				rows.Close()
				return err
			}
			dupes = append(dupes, userID)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(dupes) > 0 {
			return fmt.Errorf(
				"cannot enforce one personal organization per user: user(s) %s own multiple personal organizations; merge or delete the duplicates, then restart",
				strings.Join(dupes, ", "))
		}
		_, err = tx.Exec(`
			CREATE UNIQUE INDEX idx_organizations_personal_owner
			ON organizations(created_by) WHERE org_type = 'personal'
		`)
		return err
	}},

	// 0003: run-retry provenance (issue #133). A retried run is a brand-new
	// queued run; retried_from_run_id records which terminal run it was
	// re-enqueued from. parent_run_id deliberately stays untouched — it
	// carries delegation semantics (run tree, child priority), and a retry
	// is a sibling of its source, not a child. Plain UUID, no FK: keep the
	// pointer even if the source run is ever purged.
	{Version: 3, Name: "agent_runs_retried_from", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN retried_from_run_id UUID`)
		return err
	}},

	// 0004: promote artifact review status to a real column (issue #127).
	// Every row — current and historical versions alike — gets a status,
	// backfilled from the legacy Attributes["status"] where it holds a
	// recognizable value ("in-review", the issue's spelling, normalizes to
	// in_review) and defaulting to 'draft' otherwise. Attributes["status"]
	// stays as a deprecated read-compat mirror that the domain layer
	// refreshes on every write; it is never read for authorization again.
	// No new index: this PR ships no status-filtered queries (ModuleView
	// filters are an explicit follow-up).
	{Version: 4, Name: "artifact_status_column", Run: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			ALTER TABLE artifacts
			ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT 'draft'
		`); err != nil {
			return err
		}
		_, err := tx.Exec(`
			UPDATE artifacts
			SET status = CASE attributes->>'status'
				WHEN 'in-review' THEN 'in_review'
				ELSE attributes->>'status'
			END
			WHERE attributes->>'status' IN
				('draft', 'in_review', 'in-review', 'approved', 'superseded')
		`)
		return err
	}},

	// 0005: suspect links (issue #131). When an artifact's content changes,
	// the links touching it can no longer be trusted to still describe a
	// valid relationship, so they are flagged suspect until a human either
	// confirms each link explicitly or the artifact is approved again
	// (review implies reconfirmation). Existing rows backfill to FALSE:
	// pre-feature links were never invalidated by a tracked content change,
	// so treating them as trusted is the only defensible default.
	{Version: 5, Name: "links_suspect", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			ALTER TABLE links
			ADD COLUMN suspect BOOLEAN NOT NULL DEFAULT FALSE
		`)
		return err
	}},

	// 0006: in-app notifications (issue #132). One row per recipient per
	// event; entity_ref is an opaque jsonb pointer the frontend uses to
	// navigate ({"kind":"run","run_id":...}). The partial index serves the
	// two hot queries (bell badge count, unread-first listing) without
	// paying for read rows. NOTE: 0005 is deliberately skipped here — it is
	// being claimed by the concurrent suspect-links branch; the registry
	// only requires ascending, unique versions, so both branches merge
	// cleanly in either order.
	{Version: 6, Name: "notifications", Run: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			CREATE TABLE notifications (
				id UUID PRIMARY KEY,
				org_id UUID,
				user_id UUID NOT NULL,
				type VARCHAR(64) NOT NULL,
				title TEXT NOT NULL,
				body TEXT NOT NULL DEFAULT '',
				entity_ref JSONB NOT NULL DEFAULT '{}'::jsonb,
				read BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`); err != nil {
			return err
		}
		_, err := tx.Exec(`
			CREATE INDEX idx_notifications_user_created
			ON notifications(user_id, created_at DESC)
		`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`
			CREATE INDEX idx_notifications_user_unread
			ON notifications(user_id) WHERE NOT read
		`)
		return err
	}},

	// 0007: hot-path indexes (issue #182, the data-volume pass). Every index
	// serves a query that currently seq-scans or scans a broader index than it
	// needs at scale; all are IF NOT EXISTS so the migration is a no-op on any
	// database that already grew them by hand. Verified against the baseline
	// schema (db.go / schema_*.go) so none duplicates an existing index.
	{Version: 7, Name: "hot_path_indexes", Run: func(tx *sql.Tx) error {
		stmts := []string{
			// Every "current artifacts in a project" read (export, baseline,
			// diff, report, VV, module views) filters project_id + valid_to IS
			// NULL; the plain idx_artifacts_project_id can't skip the tombstoned
			// versions this partial index excludes outright.
			`CREATE INDEX IF NOT EXISTS idx_artifacts_project_active
				ON artifacts(project_id) WHERE valid_to IS NULL`,

			// CountRunsSince (per-automation rate limiting) filters
			// automation_id + created_at; no index covered automation_id before.
			`CREATE INDEX IF NOT EXISTS idx_agent_runs_automation
				ON agent_runs(automation_id, created_at)`,

			// The claim hot path selects the best queued run per org ordered by
			// priority DESC, created_at. A partial index over just the queued
			// rows keeps the scan proportional to the queue depth, not the whole
			// (mostly terminal) run history.
			`CREATE INDEX IF NOT EXISTS idx_agent_runs_queued_claim
				ON agent_runs(org_id, priority DESC, created_at) WHERE status = 'queued'`,

			// Team-graph edge lookups walk out of a node by edge_type; only a
			// team-scoped index existed.
			`CREATE INDEX IF NOT EXISTS idx_agent_team_edges_from
				ON agent_team_edges(from_node_id, edge_type)`,

			// Proposal listings filter by project_id alone; the existing
			// (status, project_id) index can't serve a project-only predicate.
			`CREATE INDEX IF NOT EXISTS idx_agent_proposals_project
				ON agent_proposals(project_id)`,

			// "Runs I launched" listings (ListFilter.LaunchedBy) had no index.
			`CREATE INDEX IF NOT EXISTS idx_agent_runs_launched_by
				ON agent_runs(launched_by)`,

			// Interview sessions are looked up by the invite that created them.
			`CREATE INDEX IF NOT EXISTS idx_interview_sessions_invite
				ON interview_sessions(invite_id)`,

			// The unread-first notification listing orders by created_at DESC
			// within a user's unread rows; the badge-count-only partial index
			// (idx_notifications_user_unread) lacks the ordering column.
			`CREATE INDEX IF NOT EXISTS idx_notifications_user_unread_created
				ON notifications(user_id, created_at DESC) WHERE NOT read`,

			// Link traversal from/to a current artifact filters valid_to IS
			// NULL; the plain from_id/to_id indexes include every historical
			// link version.
			`CREATE INDEX IF NOT EXISTS idx_links_from_active
				ON links(from_id) WHERE valid_to IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_links_to_active
				ON links(to_id) WHERE valid_to IS NULL`,

			// FK-backing index for the team-node -> agent reference (cheap; the
			// user_id column the issue also mentions does not exist on this
			// table, so only agent_id is added).
			`CREATE INDEX IF NOT EXISTS idx_agent_team_nodes_agent
				ON agent_team_nodes(agent_id)`,
		}
		for _, s := range stmts {
			if _, err := tx.Exec(s); err != nil {
				return err
			}
		}
		return nil
	}},

	// 0008: trigram search index for cross-project artifact search (issue
	// #182). SearchInProjects runs `title ILIKE '%q%' OR body ILIKE '%q%'`,
	// which seq-scans without trigram support. A pg_trgm GIN index over each
	// column makes the ILIKE predicate index-assisted (gin_trgm_ops supports
	// ILIKE directly, so the existing query needs no rewrite).
	//
	// CREATE EXTENSION needs elevated privilege that some managed Postgres
	// roles lack. Rather than brick boot, we probe for the extension and, when
	// it is absent and we cannot create it, roll back to a savepoint and skip
	// the indexes — the migration still records as applied (search keeps
	// working via the seq-scan fallback). A managed instance that later grants
	// the extension would need a follow-up migration to add the indexes.
	{Version: 8, Name: "artifact_trgm_search", Run: func(tx *sql.Tx) error {
		var haveTrgm bool
		if err := tx.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`,
		).Scan(&haveTrgm); err != nil {
			return err
		}
		if !haveTrgm {
			if _, err := tx.Exec(`SAVEPOINT trgm_ext`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
				// Almost always insufficient_privilege on managed PG. Undo the
				// aborted statement so the transaction (and the ledger insert)
				// can still commit, and leave search on its seq-scan fallback.
				if _, rbErr := tx.Exec(`ROLLBACK TO SAVEPOINT trgm_ext`); rbErr != nil {
					return rbErr
				}
				slog.Warn("pg_trgm extension unavailable; skipping artifact trigram search indexes (cross-project search falls back to a sequential scan)",
					slog.Any("error", err))
				return nil
			}
			if _, err := tx.Exec(`RELEASE SAVEPOINT trgm_ext`); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifacts_title_trgm
			ON artifacts USING gin (title gin_trgm_ops)
		`); err != nil {
			return err
		}
		_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifacts_body_trgm
			ON artifacts USING gin (body gin_trgm_ops)
		`)
		return err
	}},
}

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
