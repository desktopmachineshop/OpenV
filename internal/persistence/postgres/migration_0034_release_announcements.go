package postgres

import "database/sql"

// 0034: which releases have been announced. The API learns its release
// from the notes it was built with and, at boot, tells every account
// about a release it has not announced before. The row is the claim: an
// insert that hits the primary key lost the race to another replica (or
// an earlier boot), so one release is announced once however many
// servers start on it.
func m0034ReleaseAnnouncements(tx *sql.Tx) error {
	_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS release_announcements (
				version TEXT PRIMARY KEY,
				announced_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`)
	return err
}
