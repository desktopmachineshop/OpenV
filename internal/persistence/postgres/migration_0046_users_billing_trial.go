package postgres

import "database/sql"

// 0046: one free trial per buyer. A person can create workspaces
// freely, each its own billing customer, so the trial has to be keyed
// on the human rather than the workspace.
func m0046UsersBillingTrial(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS billing_trial_used_at TIMESTAMP`)
	return err
}
