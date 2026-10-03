package postgres

import "database/sql"

// 0010: drop the redundant idx_links_active partial UNIQUE index (issue
// #190/#191). links.id is the PRIMARY KEY, so it is already globally unique
// and at most one row per id can be active — the partial unique index
// enforced nothing the PK did not. Fresh databases no longer create it (see
// InitSchema); this drops it on databases that already have it.
func m0010DropRedundantIdxLinksActive(tx *sql.Tx) error {
	_, err := tx.Exec(`DROP INDEX IF EXISTS idx_links_active`)
	return err
}
