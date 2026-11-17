package postgres

import "database/sql"

// 0087: the workspace's plan calendar. The days of the week its plans work
// move from each project to the workspace, where a row here overrides what
// a project stored; and a person's or an agent's time off, the days they
// are away whatever the plan, is kept for the workspace, since a holiday
// is the same on every project. Both go with the workspace. It runs once,
// inside the transaction that records it. Once it has shipped it is frozen
// (I16).
func m0087PlanWorkspaceCalendar(tx *sql.Tx) error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS plan_org_calendar (
			org_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
			working_days INTEGER NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS plan_time_off (
			id UUID PRIMARY KEY,
			org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			assignee_type VARCHAR(16) NOT NULL DEFAULT 'user',
			assignee_id UUID NOT NULL,
			start_date DATE NOT NULL,
			end_date DATE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_time_off_org ON plan_time_off(org_id)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
