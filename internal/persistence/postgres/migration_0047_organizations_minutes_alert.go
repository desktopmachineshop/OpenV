package postgres

import "database/sql"

// 0047: the hosted-minutes alert dedupe, the budget alert's shape
// (0029) for a second monthly allowance.
func m0047OrganizationsMinutesAlert(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS minutes_alert_month VARCHAR(7),
				ADD COLUMN IF NOT EXISTS minutes_alert_threshold INT NOT NULL DEFAULT 0
		`)
	return err
}
