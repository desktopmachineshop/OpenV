package postgres

import "database/sql"

// 0068: saved searches (issue #722). A member's named set of search-bar
// chips, stored per user so it follows them between devices, per workspace,
// and per scope: the key a page's search bar stores them under. value is the
// bar's state as the frontend wrote it (internal/domain/savedsearches), so a
// page can change what its chips hold without a migration. shared lists one
// for every member of the workspace; only its owner replaces or deletes it.
// A name is one search per member, workspace and scope: saving under it
// again replaces the value. The rows go with their member and with a purged
// workspace through the cascading foreign keys (Q17).
func m0068SavedSearches(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS saved_searches (
				id UUID PRIMARY KEY,
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
				scope VARCHAR(64) NOT NULL,
				name VARCHAR(120) NOT NULL,
				value JSONB NOT NULL DEFAULT '{}'::jsonb,
				shared BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_searches_owner_name ON saved_searches(user_id, org_id, scope, name)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_saved_searches_org_scope ON saved_searches(org_id, scope)`)
	return err
}
