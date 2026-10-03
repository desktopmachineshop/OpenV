package postgres

import "database/sql"

// 0035: the release channel a company workspace's admin chose (REQ-136).
// Empty means the plan's default (orgs.ChannelForPlan); personal-tier
// plans ignore the column because they always run nightly.
func m0035OrgReleaseChannel(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS release_channel TEXT NOT NULL DEFAULT ''
		`)
	return err
}
