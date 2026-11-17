package postgres

import "database/sql"

// 0083: the people and agents who do a plan's work. A task can be assigned
// to several, each for a share of their time (units, a percentage); a
// project keeps each one's daily rate and capacity; a task has a fixed cost
// and can let its effort decide its duration; a plan has a currency and a
// budget. It runs once, inside the transaction that records it. Once it has
// shipped it is frozen (I16).
func m0083PlanResources(tx *sql.Tx) error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS plan_assignments (
			work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			assignee_type VARCHAR(8) NOT NULL,
			assignee_id UUID NOT NULL,
			units INTEGER NOT NULL DEFAULT 100,
			PRIMARY KEY (work_item_id, assignee_type, assignee_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_assignments_project ON plan_assignments(project_id)`,
		`CREATE TABLE IF NOT EXISTS plan_resources (
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			assignee_type VARCHAR(8) NOT NULL,
			assignee_id UUID NOT NULL,
			daily_rate NUMERIC(12,2) NOT NULL DEFAULT 0,
			capacity INTEGER NOT NULL DEFAULT 100,
			PRIMARY KEY (project_id, assignee_type, assignee_id)
		)`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS fixed_cost NUMERIC(14,2) NOT NULL DEFAULT 0`,
		`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS effort_driven BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT ''`,
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS budget NUMERIC(14,2)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
