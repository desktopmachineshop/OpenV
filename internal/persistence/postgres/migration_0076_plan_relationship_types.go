package postgres

import "database/sql"

// 0076: the type of a relationship between two plan tasks. Migration 0075's
// plan_dependencies held finish-to-start pairs only; a plan also needs
// start-to-start (two tasks begin together) and finish-to-finish (they end
// together), the three a Gantt names FS, SS and FF. Every pair stored before
// this was finish-to-start, which the default keeps. A pair still has one
// row, so two tasks have one relationship; a task may have any number of
// them, to different tasks. It runs once, inside the transaction that
// records it, so no CREATE INDEX CONCURRENTLY. Once it has shipped it is
// frozen (I16).
func m0076PlanRelationshipTypes(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE plan_dependencies ADD COLUMN IF NOT EXISTS kind VARCHAR(2) NOT NULL DEFAULT 'FS'`)
	return err
}
