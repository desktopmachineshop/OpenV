package postgres

import "database/sql"

// 0082: what scheduling a plan automatically needs. A plan says whether its
// tasks are placed by hand or by the platform, the day it starts, whether
// it is scheduled back from a finish date, and whether each chain of tasks
// has a critical path of its own. A task can override the plan's mode and
// carry a date constraint (start no earlier than, must finish on, ...). It
// runs once, inside the transaction that records it. Once it has shipped it
// is frozen (I16).
func m0082PlanSchedulingEngine(tx *sql.Tx) error {
	for _, ddl := range []string{
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS scheduling_mode VARCHAR(8) NOT NULL DEFAULT 'manual'`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS project_start DATE`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS project_finish DATE`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS schedule_from VARCHAR(8) NOT NULL DEFAULT 'start'`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS multiple_critical_paths BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS mode VARCHAR(8) NOT NULL DEFAULT ''`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS constraint_type VARCHAR(8) NOT NULL DEFAULT 'asap'`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS constraint_date DATE`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
