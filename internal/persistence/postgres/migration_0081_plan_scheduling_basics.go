package postgres

import "database/sql"

// 0081: what a schedule needs beyond dates and links. A task can count its
// duration in calendar days (elapsed) and carry a deadline; a relationship's
// offset has a unit (working days, elapsed days, or a percentage of the
// driving task's duration); a plan has holidays, days it does not work
// whatever the weekday, and a threshold of float at or below which a task
// is critical. It runs once, inside the transaction that records it. Once
// it has shipped it is frozen (I16).
func m0081PlanSchedulingBasics(tx *sql.Tx) error {
	for _, ddl := range []string{
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS elapsed BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS deadline DATE`,
		`ALTER TABLE plan_dependencies ADD COLUMN IF NOT EXISTS lag_unit VARCHAR(2) NOT NULL DEFAULT 'd'`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS critical_slack_days INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS plan_holidays (
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			day DATE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (project_id, day)
		)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
