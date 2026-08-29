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

	// 0007: review-queue read paths (issue #183). Two partial indexes over
	// live rows only: suspect links touching a project, and a project's
	// in_review artifacts. IF NOT EXISTS so re-applying never fails and it
	// coexists with the scale-pass suspect index of the same name.
	{Version: 7, Name: "review_queue_indexes", Run: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_links_suspect
			ON links(suspect) WHERE valid_to IS NULL AND suspect
		`); err != nil {
			return err
		}
		_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifacts_project_status
			ON artifacts(project_id, status) WHERE valid_to IS NULL
		`)
		return err
	}},

	// 0008: hot-path indexes (issue #182, the data-volume pass). Every index
	// serves a query that currently seq-scans or scans a broader index than it
	// needs at scale; all are IF NOT EXISTS so the migration is a no-op on any
	// database that already grew them by hand.
	{Version: 8, Name: "hot_path_indexes", Run: func(tx *sql.Tx) error {
		stmts := []string{
			`CREATE INDEX IF NOT EXISTS idx_artifacts_project_active
				ON artifacts(project_id) WHERE valid_to IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_agent_runs_automation
				ON agent_runs(automation_id, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_agent_runs_queued_claim
				ON agent_runs(org_id, priority DESC, created_at) WHERE status = 'queued'`,
			`CREATE INDEX IF NOT EXISTS idx_agent_team_edges_from
				ON agent_team_edges(from_node_id, edge_type)`,
			`CREATE INDEX IF NOT EXISTS idx_agent_proposals_project
				ON agent_proposals(project_id)`,
			`CREATE INDEX IF NOT EXISTS idx_agent_runs_launched_by
				ON agent_runs(launched_by)`,
			`CREATE INDEX IF NOT EXISTS idx_interview_sessions_invite
				ON interview_sessions(invite_id)`,
			`CREATE INDEX IF NOT EXISTS idx_notifications_user_unread_created
				ON notifications(user_id, created_at DESC) WHERE NOT read`,
			`CREATE INDEX IF NOT EXISTS idx_links_from_active
				ON links(from_id) WHERE valid_to IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_links_to_active
				ON links(to_id) WHERE valid_to IS NULL`,
			`CREATE INDEX IF NOT EXISTS idx_agent_team_nodes_agent
				ON agent_team_nodes(agent_id)`,
		}
		for _, st := range stmts {
			if _, err := tx.Exec(st); err != nil {
				return err
			}
		}
		return nil
	}},

	// 0009: trigram search index for cross-project artifact search (issue
	// #182). gin_trgm_ops makes the ILIKE predicate index-assisted. CREATE
	// EXTENSION needs privilege some managed roles lack, so we probe and, when
	// it is absent and uncreatable, roll back to a savepoint and skip the
	// indexes (search keeps working via the seq-scan fallback) rather than
	// bricking boot.
	{Version: 9, Name: "artifact_trgm_search", Run: func(tx *sql.Tx) error {
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

	// 0010: drop the redundant idx_links_active partial UNIQUE index (issue
	// #190/#191). links.id is the PRIMARY KEY, so it is already globally unique
	// and at most one row per id can be active — the partial unique index
	// enforced nothing the PK did not. Fresh databases no longer create it (see
	// InitSchema); this drops it on databases that already have it.
	{Version: 10, Name: "drop_redundant_idx_links_active", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP INDEX IF EXISTS idx_links_active`)
		return err
	}},

	// 0011: workspace spend budgets and threshold alerts (issue #186). A
	// nullable monthly_budget_usd (NULL = no budget, warn-only default) plus the
	// two dedupe columns the budget-alert subscriber claims atomically:
	// budget_alert_month (YYYY-MM of the last alert) and budget_alert_threshold
	// (the highest percent threshold — 80 or 100 — already alerted that month).
	{Version: 11, Name: "org_monthly_budget", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS monthly_budget_usd NUMERIC,
				ADD COLUMN IF NOT EXISTS budget_alert_month VARCHAR(7),
				ADD COLUMN IF NOT EXISTS budget_alert_threshold INT NOT NULL DEFAULT 0
		`)
		return err
	}},

	// 0012: structured failure taxonomy + bounded auto-retry (issue #184).
	//   - error_class buckets a terminal failure (provider_unavailable | auth |
	//     workspace | timeout | agent_error | worker_error; empty for a run that
	//     succeeded or was cancelled).
	//   - attempt_count / max_attempts bound a run's auto-retry chain. Existing
	//     rows backfill to attempt 1 of 1 (max_attempts DEFAULT 1) so history is
	//     never retroactively retried; new runs carry the service's cap.
	//   - next_attempt_at gates a backed-off retry's claim eligibility (NULL =
	//     claimable immediately); the queued-claim path honours it.
	{Version: 12, Name: "agent_run_failure_taxonomy", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			ALTER TABLE agent_runs
				ADD COLUMN IF NOT EXISTS error_class VARCHAR(32) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 1,
				ADD COLUMN IF NOT EXISTS max_attempts INT NOT NULL DEFAULT 1,
				ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMP
		`)
		return err
	}},

	// 0013: per-user email-notification opt-out (issue #187). A single
	// boolean gate on the users row: TRUE (the default) means the user is
	// willing to receive email for the higher-signal notification types when
	// the server has SMTP configured; FALSE opts out entirely. Email is
	// strictly opt-in infrastructure — with no SMTP configured the column has
	// no effect — so defaulting existing rows to TRUE changes nothing until an
	// operator turns SMTP on.
	{Version: 13, Name: "users_email_notifications", Run: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			ALTER TABLE users
				ADD COLUMN IF NOT EXISTS email_notifications BOOLEAN NOT NULL DEFAULT TRUE
		`)
		return err
	}},

	// 0015: org- and project-configurable typed attribute definitions
	// (issue #219, phase-4 substrate). A definition names an extra typed
	// field an org wants on its artifacts; values keep living in the
	// existing artifacts.attributes JSONB, so this is a vocabulary +
	// validator with NO data migration. Scope is exactly one of org_id
	// (org-wide) or project_id (project-scoped override / project-only
	// field); enum_values holds the allowed set for enum types.
	//
	// NOTE: 0014 is intentionally skipped here — it is claimed by the
	// concurrent run-reproducibility branch. The registry only requires
	// unique, ascending versions, so the two branches merge cleanly in
	// either order. If numbering shifts on merge, bump this entry to the
	// next free number (the registry fails fast on a collision at boot).
	{Version: 15, Name: "attribute_definitions", Run: func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS attribute_definitions (
				id UUID PRIMARY KEY,
				org_id UUID,
				project_id UUID,
				key VARCHAR(64) NOT NULL,
				label TEXT NOT NULL DEFAULT '',
				data_type VARCHAR(16) NOT NULL,
				enum_values JSONB NOT NULL DEFAULT '[]',
				applies_to_type VARCHAR(64) NOT NULL DEFAULT '',
				required BOOLEAN NOT NULL DEFAULT FALSE,
				sort_order INT NOT NULL DEFAULT 0,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`); err != nil {
			return err
		}
		// Read paths: the effective-set resolver lists org-wide definitions
		// (project_id NULL) and a project's own.
		if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_attribute_definitions_org
			ON attribute_definitions(org_id) WHERE project_id IS NULL
		`); err != nil {
			return err
		}
		_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_attribute_definitions_project
			ON attribute_definitions(project_id) WHERE project_id IS NOT NULL
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
