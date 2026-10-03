package postgres

import "database/sql"

// 0041: figure titles (REQ-157).
//
// A figure's name was the filename it was uploaded under, which for a
// screenshot is a timestamp. attachments.title is the name a member gives
// it; attachment_versions.title records the name each version carried,
// so renaming a figure is a version like replacing its image is, with
// who and when. Empty means "not named": readers fall back to the
// uploaded filename, as before.
func m0041AttachmentTitles(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE attachments
				ADD COLUMN IF NOT EXISTS title VARCHAR(255) NOT NULL DEFAULT ''
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			ALTER TABLE attachment_versions
				ADD COLUMN IF NOT EXISTS title VARCHAR(255) NOT NULL DEFAULT ''
		`)
	return err
}
