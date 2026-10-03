package postgres

import "database/sql"

// 0038: requirement flow-down between projects (REQ-144, REQ-147). A
// project may name a parent project in the same workspace; the link
// between a child requirement and the parent requirement it refines is
// an ordinary link, so nothing changes on the links table. The owner of
// an artifact lives in its attributes ("owner"); the expression index
// makes the list filter and the owner-filtered download cheap.
func m0038ProjectHierarchyAndOwner(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			ALTER TABLE projects
				ADD COLUMN IF NOT EXISTS parent_project_id UUID REFERENCES projects(id) ON DELETE SET NULL
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_projects_parent ON projects(parent_project_id) WHERE parent_project_id IS NOT NULL`); err != nil {
		return err
	}
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_artifacts_owner ON artifacts(project_id, (attributes->>'owner')) WHERE valid_to IS NULL`)
	return err
}
