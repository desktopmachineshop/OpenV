package postgres

import "database/sql"

// 0078: a plan's settings. working_days says which days of the week count
// when a task's duration and float are counted, as a bit per day (Monday is
// 1, Tuesday 2, ... Sunday 64), so a plan can leave weekends out. A project
// with no row works Monday to Friday (31). The row goes with its project,
// and so with a purged workspace, through the cascading foreign key (Q17).
// It runs once, inside the transaction that records it, so no CREATE INDEX
// CONCURRENTLY. Once it has shipped it is frozen (I16).
func m0078PlanSettings(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS plan_settings (
			project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			working_days SMALLINT NOT NULL DEFAULT 31,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`)
	return err
}
