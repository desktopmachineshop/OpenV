package postgres

import "database/sql"

// 0019: workspace soft delete. deleted_at marks a company workspace as
// deleted-but-restorable; membership lookups exclude such orgs, hiding and
// locking them, and a daily purge hard-deletes rows past the grace period
// (orgs.DeletionGraceDays). The partial index keeps the purge scan cheap.
func m0019OrgSoftDelete(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE organizations ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_organizations_deleted_at
			ON organizations (deleted_at) WHERE deleted_at IS NOT NULL
		`)
	return err
}
