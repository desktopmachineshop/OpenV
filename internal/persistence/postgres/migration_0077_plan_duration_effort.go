package postgres

import "database/sql"

// 0077: a plan task's duration and effort. duration_days is how long the
// task runs, in calendar days counting both its first and its last: with a
// start and a due date it is what they span, and it is stored so a task
// keeps its length while it has only one date or none, and so moving one
// end moves the other. effort_days is the work in it, in person-days, which
// may be more or less than the duration (three people for two days is six).
// Both are NULL until someone sets them. It runs once, inside the
// transaction that records it, so no CREATE INDEX CONCURRENTLY. Once it has
// shipped it is frozen (I16).
func m0077PlanDurationEffort(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE plan_tasks
		ADD COLUMN IF NOT EXISTS duration_days INTEGER,
		ADD COLUMN IF NOT EXISTS effort_days NUMERIC(9,2)`)
	return err
}
