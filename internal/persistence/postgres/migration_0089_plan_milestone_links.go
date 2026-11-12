package postgres

import "database/sql"

// 0089: a milestone can wait for tasks, and take its date from them. A
// stored milestone keeps the ids of the tasks it waits for (waits_for) and
// how its date is set (date_mode): "fixed", the date typed, or "plan", the
// day the last of those tasks finishes. It runs once, inside the transaction
// that records it, so no CREATE INDEX CONCURRENTLY. Once it has shipped it
// is frozen (I16): change the schema again with a new migration, never by
// editing this one.
func m0089PlanMilestoneLinks(tx *sql.Tx) error {
	for _, ddl := range []string{
		`ALTER TABLE plan_milestones ADD COLUMN IF NOT EXISTS date_mode VARCHAR(8) NOT NULL DEFAULT 'fixed'`,
		`ALTER TABLE plan_milestones ADD COLUMN IF NOT EXISTS waits_for JSONB NOT NULL DEFAULT '[]'`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
