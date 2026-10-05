package agentruns

import (
	"fmt"
	"time"
)

// WorkspaceBudgets reads a workspace's monthly spend budget in USD: nil when
// it has none, and an error when the workspace cannot be read or there is no
// such workspace. orgs.DefaultService is one; cmd/server passes it in, since
// this package does not import orgs.
type WorkspaceBudgets interface {
	MonthlyBudgetUSD(orgID string) (*float64, error)
}

// BudgetGuard is the over-budget soft-block that SetBudgetGuard takes: it
// refuses a launch once the workspace's month-to-date spend has reached its
// monthly budget, and lets it through, with no reason, when the workspace
// cannot be read, has no budget or one of zero or less, or its spend cannot
// be read. The spend is read only when there is a budget, from the first
// instant of the current month in UTC. Both amounts print to the cent.
func BudgetGuard(budgets WorkspaceBudgets, runs *DefaultService) func(orgID string) (bool, string) {
	return func(orgID string) (bool, string) {
		budget, err := budgets.MonthlyBudgetUSD(orgID)
		if err != nil || budget == nil || *budget <= 0 {
			return false, ""
		}
		now := time.Now().UTC()
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		spend, err := runs.MonthlySpend(orgID, monthStart)
		if err != nil || spend < *budget {
			return false, ""
		}
		return true, fmt.Sprintf("this workspace has reached its $%.2f monthly budget ($%.2f spent); new runs are blocked until next month or the budget is raised", *budget, spend)
	}
}
