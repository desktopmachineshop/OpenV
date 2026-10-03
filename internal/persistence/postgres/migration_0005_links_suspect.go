package postgres

import "database/sql"

// 0005: suspect links (issue #131). When an artifact's content changes,
// the links touching it can no longer be trusted to still describe a
// valid relationship, so they are flagged suspect until a human either
// confirms each link explicitly or the artifact is approved again
// (review implies reconfirmation). Existing rows backfill to FALSE:
// pre-feature links were never invalidated by a tracked content change,
// so treating them as trusted is the only defensible default.
func m0005LinksSuspect(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE links
			ADD COLUMN suspect BOOLEAN NOT NULL DEFAULT FALSE
		`)
	return err
}
