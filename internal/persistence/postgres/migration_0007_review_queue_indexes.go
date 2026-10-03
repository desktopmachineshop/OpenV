package postgres

import "database/sql"

// 0007: review-queue read paths (issue #183). Two partial indexes over
// live rows only: suspect links touching a project, and a project's
// in_review artifacts. IF NOT EXISTS so re-applying never fails and it
// coexists with the scale-pass suspect index of the same name.
func m0007ReviewQueueIndexes(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_links_suspect
			ON links(suspect) WHERE valid_to IS NULL AND suspect
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_artifacts_project_status
			ON artifacts(project_id, status) WHERE valid_to IS NULL
		`)
	return err
}
