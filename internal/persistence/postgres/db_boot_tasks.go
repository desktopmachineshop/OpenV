// The tasks a boot runs once per database (migration 0056's boot_tasks):
// data work that must follow the migrations but is not a migration, such as
// the sweep of the stored files no row names (#379 question 48), whose file
// I/O is the caller's.

package postgres

import (
	"database/sql"
	"fmt"
)

// RunBootTaskOnce runs task unless this database records that a task named
// name has run, and records it, with the outcome task answers, once it has.
// It holds the boot advisory lock throughout (withBootLock), so of two
// processes booting at once one runs the task and the other, waiting on
// the lock, then finds it recorded and skips it. A task that fails is not
// recorded, so the next boot runs it again; its error is returned, as is a
// failure to read or write the record. ran says whether task ran.
func RunBootTaskOnce(db *sql.DB, name string, task func() (outcome string, err error)) (ran bool, err error) {
	err = withBootLock(db, func() error {
		var done bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM boot_tasks WHERE name = $1)`, name).Scan(&done); err != nil {
			return fmt.Errorf("failed to read the record of boot task %s: %w", name, err)
		}
		if done {
			return nil
		}
		ran = true
		outcome, err := task()
		if err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO boot_tasks (name, outcome) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`,
			name, outcome); err != nil {
			return fmt.Errorf("failed to record boot task %s: %w", name, err)
		}
		return nil
	})
	return ran, err
}
