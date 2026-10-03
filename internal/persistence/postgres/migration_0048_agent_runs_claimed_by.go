package postgres

import "database/sql"

// 0048: the member whose personal runner key claimed a run (NULL for a
// workspace key), stamped by the claim and cleared by a release. The
// runner reads a claimed run's repository connections with the run's
// own token, and each member's local checkout path is their own, so the
// token's read needs to know whose machine the run is on.
func m0048AgentRunsClaimedBy(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS claimed_by UUID`)
	return err
}
