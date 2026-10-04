package postgres

import "database/sql"

// 0056: the record of the tasks a boot runs once per database (#379
// question 48). A row names a task that has run to an outcome on this
// database, so no later boot, of this process or another, runs it again
// (RunBootTaskOnce). The first is the sweep of the stored files no row
// names, which the deletes and purges before #379's bugs 136 and 143 left
// in the uploads directory.
func m0056BootTasks(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE boot_tasks (
			name TEXT PRIMARY KEY,
			ran_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			outcome TEXT NOT NULL DEFAULT ''
		)`)
	return err
}
