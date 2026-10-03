package postgres

import "database/sql"

// A restored figure version records which version it brought back, so the
// history can say so. A restore reuses the older version's stored file,
// which means file paths alone cannot tell a restore from a re-upload.
func m0044AttachmentVersionRestoredFrom(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE attachment_versions ADD COLUMN IF NOT EXISTS restored_from INT`)
	return err
}
