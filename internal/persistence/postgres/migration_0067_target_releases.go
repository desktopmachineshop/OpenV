package postgres

import "database/sql"

// 0067: target releases. A project's named, ordered milestones ("MVP",
// "V1.0") that its requirements are planned for (internal/domain/
// targetreleases). position orders them: a requirement introduced in a
// release is in every later one. An artifact's targeting is not stored
// here but in its attributes (target_release, target_release_until), so
// versions, baselines and exports carry it. The rows go with their project,
// and so with a purged workspace, through the cascading foreign key (Q17).
func m0067TargetReleases(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS target_releases (
				id UUID PRIMARY KEY,
				project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
				name VARCHAR(200) NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				target_date DATE,
				status VARCHAR(32) NOT NULL DEFAULT 'planned',
				position INTEGER NOT NULL DEFAULT 0,
				released_at TIMESTAMP,
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_target_releases_project_name ON target_releases(project_id, LOWER(name))`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_target_releases_project_position ON target_releases(project_id, position)`)
	return err
}
