package postgres

import "database/sql"

// 0042: password reset links (REQ-158).
//
// The same shape as email_verifications: a hashed single-use token with
// an expiry, at most one live per account. delivery says how the link
// reached its holder — emailed to the account's own address, or minted
// by a platform admin (issued_by) and handed over out of band — because
// only the first is proof of the mailbox.
func m0042PasswordResets(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS password_resets (
				id UUID PRIMARY KEY,
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				token_hash VARCHAR(128) NOT NULL,
				delivery VARCHAR(16) NOT NULL DEFAULT 'email',
				issued_by UUID,
				expires_at TIMESTAMP NOT NULL,
				used BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token_hash)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_password_resets_user ON password_resets(user_id)`)
	return err
}
