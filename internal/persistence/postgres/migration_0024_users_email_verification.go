package postgres

import "database/sql"

// 0024: email verification for password accounts (SEC-15 / REQ-95).
//
// users.email_verified says whether the account has proved control of its
// address; email_verified_at records when. SSO accounts (google, oidc)
// are backfilled TRUE: both callbacks already refuse a provider-unverified
// email, so the identity provider has done the proving. Password accounts
// are backfilled FALSE — including every account that predates this
// migration — and are asked to verify on their next request, but ONLY on
// a deployment that has SMTP configured (users.EmailVerificationPolicy);
// with no mailer the column is inert and nothing changes for a
// self-hosted stack or CI.
//
// email_verifications holds the one-shot links, in the shape of
// connector_pairings: the raw token is never stored, only its SHA-256;
// `email` is the address the link was sent to, which becomes the
// account's address on confirm (the change-of-address flow), so an
// address is never swapped unproven.
func m0024UsersEmailVerification(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE users
				ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE,
				ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMP
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			UPDATE users
			SET email_verified = TRUE,
			    email_verified_at = COALESCE(email_verified_at, created_at)
			WHERE auth_provider IN ('google', 'oidc') AND NOT email_verified
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS email_verifications (
				id UUID PRIMARY KEY,
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				email VARCHAR(320) NOT NULL,
				token_hash VARCHAR(128) NOT NULL,
				expires_at TIMESTAMP NOT NULL,
				used BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_email_verifications_token ON email_verifications(token_hash)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_email_verifications_user ON email_verifications(user_id)`)
	return err
}
