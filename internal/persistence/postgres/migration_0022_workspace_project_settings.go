package postgres

import "database/sql"

// 0022: workspace and project settings (requirement quality rule sets).
// One JSONB bag per level rather than a column per preference: the rules
// resolve defaults -> workspace -> project, and each level stores only the
// keys it actually overrides. Empty settings everywhere means the platform
// defaults, so existing workspaces keep the ISO/IEC/IEEE 29148 "shall"
// convention the linter has always applied.
func m0022WorkspaceProjectSettings(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE organizations ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}'
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			ALTER TABLE projects ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}'
		`)
	return err
}
