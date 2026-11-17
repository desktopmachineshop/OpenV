package postgres

import "database/sql"

// 0066: linked sign-in methods (issue #242's follow-up).
//
// user_identities lists the identity providers an account may sign in
// through besides the one it was created with (users.auth_provider): one
// row per account and provider, added only when the account's owner
// confirmed the link with its password. identity_link_requests holds those
// confirmations while they are pending, the same shape as password_resets:
// a hashed single-use token with an expiry, at most one live per account.
func m0066UserIdentities(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS user_identities (
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				provider VARCHAR(32) NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				PRIMARY KEY (user_id, provider)
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS identity_link_requests (
				id UUID PRIMARY KEY,
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				provider VARCHAR(32) NOT NULL,
				token_hash VARCHAR(128) NOT NULL,
				expires_at TIMESTAMP NOT NULL,
				used BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_identity_link_requests_token ON identity_link_requests(token_hash)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_identity_link_requests_user ON identity_link_requests(user_id)`)
	return err
}
