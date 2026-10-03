package postgres

import "database/sql"

// 0031: workspace logo. The image itself lives under the uploads
// directory (org-logos/<org id>.<ext>); the row records where and what
// MIME type it is, so the API can serve it and reports can embed it on
// their cover page. Empty means no logo has been uploaded.
func m0031OrgLogo(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS logo_path TEXT NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS logo_mime TEXT NOT NULL DEFAULT ''
		`)
	return err
}
