package postgres

import "database/sql"

// 0062: connected apps (REQ-44). A member attaches an AI client to a
// workspace over the remote MCP endpoint with OAuth 2.1: the client
// registers itself (oauth_clients, no secret), the member consents for one
// workspace (connected_apps, one live grant per member, client and
// workspace), the client spends a ten-minute code (oauth_codes) for an
// hour-long access token and a thirty-day refresh token that is replaced on
// use (oauth_tokens). Codes and tokens are stored as SHA-256 hashes only. A
// grant goes with its member, its workspace or its client, and its codes and
// tokens go with it; a revoked grant stays as the record of the consent.
func m0062ConnectedApps(tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS oauth_clients (
			id UUID PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			redirect_uris TEXT[] NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS connected_apps (
			id UUID PRIMARY KEY,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			client_id UUID NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
			scope VARCHAR(100) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_used_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_connected_apps_live
			ON connected_apps(user_id, org_id, client_id) WHERE revoked_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_connected_apps_org ON connected_apps(org_id)`,
		`CREATE TABLE IF NOT EXISTS oauth_codes (
			code_hash VARCHAR(64) PRIMARY KEY,
			grant_id UUID NOT NULL REFERENCES connected_apps(id) ON DELETE CASCADE,
			client_id UUID NOT NULL,
			redirect_uri TEXT NOT NULL,
			code_challenge VARCHAR(64) NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_codes_grant ON oauth_codes(grant_id)`,
		`CREATE TABLE IF NOT EXISTS oauth_tokens (
			id UUID PRIMARY KEY,
			grant_id UUID NOT NULL REFERENCES connected_apps(id) ON DELETE CASCADE,
			access_hash VARCHAR(64) NOT NULL UNIQUE,
			refresh_hash VARCHAR(64) NOT NULL UNIQUE,
			access_expires_at TIMESTAMPTZ NOT NULL,
			refresh_expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_tokens_grant ON oauth_tokens(grant_id)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
