package postgres

import "database/sql"

// 0014: run reproducibility snapshot (issue #216, phase-4 foundation).
// A run records the agent identity it was launched with — the agent's
// content_hash (a SHA-256 of the whole markdown definition, pinning the
// exact prompt+config), model, and reasoning effort — captured once at
// launch (agentruns.Launch), never retro-filled (existing rows read '').

func m0014AgentRunReproducibilitySnapshot(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE agent_runs
				ADD COLUMN IF NOT EXISTS agent_content_hash VARCHAR NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS agent_model VARCHAR NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS agent_effort VARCHAR NOT NULL DEFAULT ''
		`)
	return err
}
