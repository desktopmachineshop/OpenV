package postgres

import "database/sql"

// 0012: structured failure taxonomy + bounded auto-retry (issue #184).
//   - error_class buckets a terminal failure (provider_unavailable | auth |
//     workspace | timeout | agent_error | worker_error; empty for a run that
//     succeeded or was cancelled).
//   - attempt_count / max_attempts bound a run's auto-retry chain. Existing
//     rows backfill to attempt 1 of 1 (max_attempts DEFAULT 1) so history is
//     never retroactively retried; new runs carry the service's cap.
//   - next_attempt_at gates a backed-off retry's claim eligibility (NULL =
//     claimable immediately); the queued-claim path honours it.
func m0012AgentRunFailureTaxonomy(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE agent_runs
				ADD COLUMN IF NOT EXISTS error_class VARCHAR(32) NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 1,
				ADD COLUMN IF NOT EXISTS max_attempts INT NOT NULL DEFAULT 1,
				ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMP
		`)
	return err
}
