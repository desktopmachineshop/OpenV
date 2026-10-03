package postgres

import "database/sql"

// 0039: project share links (REQ-149, REQ-150). A public link shows the
// live project read-only to whoever holds it; a reviewer link grants the
// reviewer role to a signed-in account. The token is stored hashed.
func m0039ProjectShareLinks(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS project_share_links (
				id UUID PRIMARY KEY,
				project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				token_hash VARCHAR(128) NOT NULL UNIQUE,
				role VARCHAR(16) NOT NULL,
				label TEXT NOT NULL DEFAULT '',
				created_by UUID,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				expires_at TIMESTAMP,
				revoked_at TIMESTAMP
			)
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_project_share_links_project ON project_share_links(project_id)`)
	return err
}
