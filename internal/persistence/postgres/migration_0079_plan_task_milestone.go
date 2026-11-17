package postgres

import "database/sql"

// 0079: a plan task can be assigned to a milestone. milestone_key is the
// key the plan gives the milestone: a stored milestone's id, or
// "release:<release id>" for a release's. It is text with no foreign key,
// since a release milestone has no row until its criteria are set; a key
// whose milestone has gone is read as no milestone. It runs once, inside
// the transaction that records it. Once it has shipped it is frozen (I16).
func m0079PlanTaskMilestone(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS milestone_key TEXT`)
	return err
}
