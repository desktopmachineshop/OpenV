package postgres

import "database/sql"

// 0020: retire the bundled "Desktop CNC Mill Example" default template.
// It is no longer in DefaultTemplates, but SeedDefaults only ever adds
// rows, so databases seeded before it was retired keep offering it in the
// template picker until it is deleted here. Scoped to the global built-in
// row (org_id IS NULL): a workspace's own saved templates are untouched.
func m0020DropCncMillDefaultTemplate(tx *sql.Tx) error {
	_, err := tx.Exec(`
			DELETE FROM templates
			WHERE template_key = 'example-cnc-mill' AND org_id IS NULL
		`)
	return err
}
