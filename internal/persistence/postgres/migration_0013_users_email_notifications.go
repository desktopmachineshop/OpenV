package postgres

import "database/sql"

// 0013: per-user email-notification opt-out (issue #187). A single
// boolean gate on the users row: TRUE (the default) means the user is
// willing to receive email for the higher-signal notification types when
// the server has SMTP configured; FALSE opts out entirely. Email is
// strictly opt-in infrastructure — with no SMTP configured the column has
// no effect — so defaulting existing rows to TRUE changes nothing until an
// operator turns SMTP on.
func m0013UsersEmailNotifications(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE users
				ADD COLUMN IF NOT EXISTS email_notifications BOOLEAN NOT NULL DEFAULT TRUE
		`)
	return err
}
