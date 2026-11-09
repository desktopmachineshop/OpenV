package postgres

import "database/sql"

// 0073: comment read state. When a member last read an artifact's comments
// (internal/domain/commentreads), so the Home pages' Needs you lists only
// comments newer than that, and Mark unread can move the time back. One row
// per member and artifact. artifact_id names the artifact's stable id, which
// is not a key of the versioned artifacts table, so the row cascades with its
// member, its workspace and the artifact's project instead (Q17); a row
// outliving a deleted artifact names nothing and is never read.
func m0073CommentReads(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS comment_reads (
				user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
				project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				artifact_id UUID NOT NULL,
				read_at TIMESTAMP NOT NULL,
				PRIMARY KEY (user_id, artifact_id)
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_comment_reads_org ON comment_reads(org_id)`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_comment_reads_project ON comment_reads(project_id)`)
	return err
}
