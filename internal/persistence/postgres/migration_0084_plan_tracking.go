package postgres

import "database/sql"

// 0084: tracking a plan against what was planned. A baseline is a snapshot
// of every task's dates, duration, effort and cost; a task records when it
// actually started and finished, a percentage complete typed by hand in
// place of the computed one, and what it has actually cost; a plan has a
// status date, the day its progress is reported as of, and the baseline it
// is compared with. It runs once, inside the transaction that records it.
// Once it has shipped it is frozen (I16).
func m0084PlanTracking(tx *sql.Tx) error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS plan_baselines (
			id UUID PRIMARY KEY,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			tasks JSONB NOT NULL DEFAULT '[]',
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_baselines_project ON plan_baselines(project_id)`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS actual_start DATE`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS actual_finish DATE`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS percent_complete INTEGER`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS actual_cost NUMERIC(14,2)`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS status_date DATE`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS active_baseline_id UUID`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
