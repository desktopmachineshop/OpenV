package postgres

import "database/sql"

// 0088: sizing a plan before it is staffed. A plan keeps whether a new
// task lets its effort decide its duration (effort_driven_default), and a
// resource can be a planned one: a named placeholder for someone to be
// chosen later, which is a plan_resources row of type "planned" with a
// name. It runs once, inside the transaction that records it, so no CREATE
// INDEX CONCURRENTLY. Once it has shipped it is frozen (I16): change the
// schema again with a new migration, never by editing this one.
func m0088PlanSizing(tx *sql.Tx) error {
	for _, ddl := range []string{
		`ALTER TABLE plan_settings ADD COLUMN IF NOT EXISTS effort_driven_default BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE plan_resources ADD COLUMN IF NOT EXISTS name VARCHAR(120) NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
