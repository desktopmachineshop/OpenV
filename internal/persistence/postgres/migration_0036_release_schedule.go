package postgres

import "database/sql"

// 0036: the stable release a workspace has turned on and its upgrade
// window (REQ-137, REQ-138), and a member's own early switch to the
// next stable release. stable_release stays empty until the first
// stable is cut and turns on; upgrade_day 0 means "at the cut".
func m0036ReleaseSchedule(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS stable_release TEXT NOT NULL DEFAULT '',
				ADD COLUMN IF NOT EXISTS upgrade_day INT NOT NULL DEFAULT 0,
				ADD COLUMN IF NOT EXISTS upgrade_hour INT NOT NULL DEFAULT 0,
				ADD COLUMN IF NOT EXISTS upgrade_timezone TEXT NOT NULL DEFAULT ''
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			ALTER TABLE org_members
				ADD COLUMN IF NOT EXISTS preview_next_stable BOOLEAN NOT NULL DEFAULT FALSE
		`)
	return err
}
