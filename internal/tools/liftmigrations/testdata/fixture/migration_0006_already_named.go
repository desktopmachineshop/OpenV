package store

import "database/sql"

// 0006: lifted before.
func m0006AlreadyNamed(tx *sql.Tx) error {
	_, err := tx.Exec(`SELECT 6`)
	return err
}
