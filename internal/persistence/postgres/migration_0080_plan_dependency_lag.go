package postgres

import "database/sql"

// 0080: a relationship between two plan tasks can carry an offset.
// lag_days is the working days the driven task keeps after what the
// relationship's type asks for (negative: before), so "start to start, 5
// days later" is kind SS with lag_days 5. Existing relationships have none.
// It runs once, inside the transaction that records it. Once it has shipped
// it is frozen (I16).
func m0080PlanDependencyLag(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE plan_dependencies ADD COLUMN IF NOT EXISTS lag_days INTEGER NOT NULL DEFAULT 0`)
	return err
}
