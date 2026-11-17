package postgres

import "database/sql"

// 0090: a project baseline records the plan with the definition. A plan
// baseline can belong to a project baseline (baseline_id), which then owns
// it: deleting the project baseline deletes its plan with it. It also
// keeps the plan's figures and its milestones as they stood (moment), so
// two baselines can be compared for progress as well as for what changed.
// It runs once, inside the transaction that records it, so no CREATE INDEX
// CONCURRENTLY. Once it has shipped it is frozen (I16): change the schema
// again with a new migration, never by editing this one.
func m0090JoinedBaselines(tx *sql.Tx) error {
	for _, ddl := range []string{
		`ALTER TABLE plan_baselines ADD COLUMN IF NOT EXISTS baseline_id UUID REFERENCES baselines(id) ON DELETE CASCADE`,
		`ALTER TABLE plan_baselines ADD COLUMN IF NOT EXISTS moment JSONB`,
		`CREATE INDEX IF NOT EXISTS idx_plan_baselines_baseline ON plan_baselines(baseline_id)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
