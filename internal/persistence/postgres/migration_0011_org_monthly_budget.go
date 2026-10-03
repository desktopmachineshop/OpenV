package postgres

import "database/sql"

// 0011: workspace spend budgets and threshold alerts (issue #186). A
// nullable monthly_budget_usd (NULL = no budget, warn-only default) plus the
// two dedupe columns the budget-alert subscriber claims atomically:
// budget_alert_month (YYYY-MM of the last alert) and budget_alert_threshold
// (the highest percent threshold — 80 or 100 — already alerted that month).
func m0011OrgMonthlyBudget(tx *sql.Tx) error {
	_, err := tx.Exec(`
			ALTER TABLE organizations
				ADD COLUMN IF NOT EXISTS monthly_budget_usd NUMERIC,
				ADD COLUMN IF NOT EXISTS budget_alert_month VARCHAR(7),
				ADD COLUMN IF NOT EXISTS budget_alert_threshold INT NOT NULL DEFAULT 0
		`)
	return err
}
