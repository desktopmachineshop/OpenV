package postgres

import "database/sql"

// 0061: a workspace admin pastes a provider API key in the app instead of
// setting it on every runner host (one row per workspace and key variable,
// such as ANTHROPIC_API_KEY). The key is kept sealed (AES-256-GCM under the
// server's OPENV_SECRETS_KEY), never in the clear; last4 is all the app
// shows of it. The rows go with their workspace; the author is forgotten
// when their account goes.
func m0061ProviderApiKeys(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS provider_api_keys (
			org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			key_env VARCHAR(64) NOT NULL,
			sealed BYTEA NOT NULL,
			last4 VARCHAR(8) NOT NULL DEFAULT '',
			updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
			updated_at TIMESTAMP NOT NULL,
			PRIMARY KEY (org_id, key_env)
		)
	`)
	return err
}
