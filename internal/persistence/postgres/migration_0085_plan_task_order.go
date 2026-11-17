package postgres

import "database/sql"

// 0085: a task's place among its sibling tasks on the plan. Until a task
// is moved its siblings are listed by start date, then by age; moving one
// numbers them all. It runs once, inside the transaction that records it.
// Once it has shipped it is frozen (I16).
func m0085PlanTaskOrder(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE plan_tasks ADD COLUMN IF NOT EXISTS sort_order INTEGER`)
	return err
}
