package postgres

import "database/sql"

// 0086: a relationship between tasks of two projects. It is kept apart
// from plan_dependencies, whose rows belong to one project: the driven
// task's plan reads the driving task's dates as given, and neither plan
// lists the other's task. It goes with either task. It runs once, inside
// the transaction that records it. Once it has shipped it is frozen (I16).
func m0086PlanExternalLinks(tx *sql.Tx) error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS plan_external_links (
			work_item_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			depends_on_id UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
			kind VARCHAR(2) NOT NULL DEFAULT 'FS',
			lag_days INTEGER NOT NULL DEFAULT 0,
			lag_unit VARCHAR(2) NOT NULL DEFAULT 'd',
			PRIMARY KEY (work_item_id, depends_on_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plan_external_links_depends_on ON plan_external_links(depends_on_id)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
