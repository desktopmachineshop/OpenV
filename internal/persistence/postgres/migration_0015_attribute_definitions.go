package postgres

import "database/sql"

// 0015: org- and project-configurable typed attribute definitions (issue
// #219, phase-4 substrate). A definition names an extra typed field an org
// wants on its artifacts; values keep living in the existing
// artifacts.attributes JSONB, so this is a vocabulary + validator with NO
// data migration. Scope is exactly one of org_id (org-wide) or project_id
// (project-scoped override); enum_values holds the allowed set for enums.
func m0015AttributeDefinitions(tx *sql.Tx) error {
	if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS attribute_definitions (
				id UUID PRIMARY KEY,
				org_id UUID,
				project_id UUID,
				key VARCHAR(64) NOT NULL,
				label TEXT NOT NULL DEFAULT '',
				data_type VARCHAR(16) NOT NULL,
				enum_values JSONB NOT NULL DEFAULT '[]',
				applies_to_type VARCHAR(64) NOT NULL DEFAULT '',
				required BOOLEAN NOT NULL DEFAULT FALSE,
				sort_order INT NOT NULL DEFAULT 0,
				created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_attribute_definitions_org
			ON attribute_definitions(org_id) WHERE project_id IS NULL
		`); err != nil {
		return err
	}
	_, err := tx.Exec(`
			CREATE INDEX IF NOT EXISTS idx_attribute_definitions_project
			ON attribute_definitions(project_id) WHERE project_id IS NOT NULL
		`)
	return err
}
