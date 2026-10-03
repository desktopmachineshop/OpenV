package postgres

import "database/sql"

// 0027: streamed assistant answers, and nudges that wait their turn.
//
// agent_runs.partial_text holds the answer a live run has written so far
// — the whole text, refreshed from the worker's 750 ms log batches and
// cleared at finish, so a lost batch can only cost freshness, never
// corrupt the display. It is deliberately a column on the run and not a
// log row: there is exactly one current value per run, and readers want
// the latest, not the history.
//
// guided_sessions.pending_nudge parks the newest wizard nudge that
// arrived while a copilot run was in flight ({step, state, event}); the
// run's finish launches exactly one turn from it and clears it. NULL —
// the default and the cleared state — means nothing is waiting.
func m0027RunPartialTextAndPendingNudge(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS partial_text TEXT NOT NULL DEFAULT ''
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			ALTER TABLE guided_sessions ADD COLUMN IF NOT EXISTS pending_nudge JSONB
		`)
	return err
}
