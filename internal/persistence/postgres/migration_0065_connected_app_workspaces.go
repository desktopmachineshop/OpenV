package postgres

import "database/sql"

// 0065: a connected app reaches more than one workspace. An AI client such
// as Claude holds one connection to a server address, so a member who works
// in two workspaces needs one grant that covers both, and needs to change
// which ones it covers without connecting again. connected_app_workspaces
// is the set; connected_apps.org_id stays the workspace the grant was first
// made for (the key one live grant per member, client and workspace hangs
// on), and every existing grant gets its own workspace as its set. A row
// goes with its grant or its workspace.
func m0065ConnectedAppWorkspaces(tx *sql.Tx) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS connected_app_workspaces (
			grant_id UUID NOT NULL REFERENCES connected_apps(id) ON DELETE CASCADE,
			org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
			PRIMARY KEY (grant_id, org_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_connected_app_workspaces_org ON connected_app_workspaces(org_id)`,
		`INSERT INTO connected_app_workspaces (grant_id, org_id)
			SELECT id, org_id FROM connected_apps
			ON CONFLICT DO NOTHING`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
