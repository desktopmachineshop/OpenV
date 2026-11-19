package postgres

import "database/sql"

// 0091: a project's cover photo, shown on its Overview and on its card in
// All projects. Unlike the workspace logo and the profile picture, the
// image is kept in the row itself, not under the uploads directory: one
// row per project, removed with the project by its foreign key, so a
// deleted or purged project leaves no file behind and no sweep has to
// find one. The image is small (the API caps it), and updated_at versions
// the URL the app fetches it from. Once it has shipped it is frozen (I16):
// change the schema again with a new migration, never by editing this one.
func m0091ProjectCovers(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS project_covers (
			project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
			mime TEXT NOT NULL,
			data BYTEA NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}
