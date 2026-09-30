package store

import "database/sql"

// initSchema is the baseline, re-run on every boot.
func initSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS widgets_base (id INT)`)
	return err
}
