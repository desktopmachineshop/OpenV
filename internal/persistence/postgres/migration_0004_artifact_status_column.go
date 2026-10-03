package postgres

import "database/sql"

// 0004: promote artifact review status to a real column (issue #127).
// Every row — current and historical versions alike — gets a status,
// backfilled from the legacy Attributes["status"] where it holds a
// recognizable value ("in-review", the issue's spelling, normalizes to
// in_review) and defaulting to 'draft' otherwise. Attributes["status"]
// stays as a deprecated read-compat mirror that the domain layer
// refreshes on every write; it is never read for authorization again.
// No new index: this PR ships no status-filtered queries (ModuleView
// filters are an explicit follow-up).
func m0004ArtifactStatusColumn(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE artifacts
			ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT 'draft'
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			UPDATE artifacts
			SET status = CASE attributes->>'status'
				WHEN 'in-review' THEN 'in_review'
				ELSE attributes->>'status'
			END
			WHERE attributes->>'status' IN
				('draft', 'in_review', 'in-review', 'approved', 'superseded')
		`)
	return err
}
