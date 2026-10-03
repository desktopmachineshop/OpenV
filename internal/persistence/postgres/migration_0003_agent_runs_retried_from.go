package postgres

import "database/sql"

// 0003: run-retry provenance (issue #133). A retried run is a brand-new
// queued run; retried_from_run_id records which terminal run it was
// re-enqueued from. parent_run_id deliberately stays untouched — it
// carries delegation semantics (run tree, child priority), and a retry
// is a sibling of its source, not a child. Plain UUID, no FK: keep the
// pointer even if the source run is ever purged.
func m0003AgentRunsRetriedFrom(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN retried_from_run_id UUID`)
	return err
}
