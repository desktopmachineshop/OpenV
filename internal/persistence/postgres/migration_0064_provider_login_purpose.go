package postgres

import "database/sql"

// 0064: a brokered sign-in may now be for minting the member a long-lived
// Claude subscription token on their cloud runner (`claude setup-token`),
// which the server seals as their saved token, rather than for signing the
// runner's CLI in. purpose is ” for a sign-in, as every existing row is,
// or 'token'. Only a runner that can capture the token claims a 'token'
// row, so the claim and the active-request lookup both filter on it.
func m0064ProviderLoginPurpose(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE provider_logins ADD COLUMN IF NOT EXISTS purpose VARCHAR(20) NOT NULL DEFAULT ''`)
	return err
}
