package postgres

import "database/sql"

// 0061: a member's own long-lived provider sign-in, today the token
// `claude setup-token` prints for a Claude subscription. A leased runner
// starts from an empty home on every lease, so without a stored token every
// lease asked for a fresh browser sign-in. One row per member and provider;
// the token is sealed (AES-256-GCM under OPENV_SECRETS_KEY) before it is
// written, and only its last four characters are kept in the clear. The
// rows belong to the member, not a workspace, and go with them.
func m0061UserProviderTokens(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS user_provider_tokens (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider VARCHAR(50) NOT NULL,
			sealed BYTEA NOT NULL,
			last4 VARCHAR(4) NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			PRIMARY KEY (user_id, provider)
		)
	`)
	return err
}
