package postgres

import "database/sql"

// 0037: what the release scheduler has done for each workspace and
// stable release (REQ-138, REQ-140): told the admins at the cut,
// reminded them a day before, turned the release on. Each step is a
// claim so replicas and restarts never repeat a notification.
func m0037ReleaseSchedule(tx *sql.Tx) error {
	_, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS release_schedule (
				org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
				version TEXT NOT NULL,
				announced_at TIMESTAMP,
				reminded_at TIMESTAMP,
				turned_on_at TIMESTAMP,
				PRIMARY KEY (org_id, version)
			)
		`)
	return err
}
