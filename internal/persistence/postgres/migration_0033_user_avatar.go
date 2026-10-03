package postgres

import "database/sql"

// 0033: uploaded profile picture. Same shape as the workspace logo: the
// image lives under the uploads directory (avatars/<user id>.<ext>) and
// the row records where and what MIME type it is. While a picture is
// stored, avatar_url points at the API endpoint that serves it and an
// identity provider's picture no longer overwrites it at sign-in.
func m0033UserAvatar(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE users
				ADD COLUMN IF NOT EXISTS avatar_path TEXT NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS avatar_mime TEXT NOT NULL DEFAULT ''
		`)
	return err
}
