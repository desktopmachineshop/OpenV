package main

import (
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// budgetOrgs is a workspace repository holding org-1 with a monthly budget.
type budgetOrgs struct {
	orgs.Repository
	budget float64
}

func (r budgetOrgs) FindOrgByID(id string) (*orgs.Org, error) {
	if id != "org-1" {
		return nil, nil
	}
	budget := r.budget
	return &orgs.Org{ID: id, MonthlyBudgetUSD: &budget}, nil
}

// spentRuns is a run repository whose workspace has spent spend this month.
type spentRuns struct {
	agentruns.Repository
	spend float64
}

func (r spentRuns) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	return r.spend, nil
}

// The over-budget soft-block refuses a launch once the month-to-date spend
// has reached the budget, not only once it has passed it: a workspace that
// has spent exactly its budget is blocked, and one a cent short is not
// (#379 bug 100; the S5d tour only spends $5.00 against $1.00, so a guard
// that compared with <= instead of < passed it).
func TestBudgetGuardBlocksAtExactlyTheBudget(t *testing.T) {
	for _, c := range []struct {
		name          string
		budget, spend float64
		blocked       bool
	}{
		{"a cent below the budget", 25, 24.99, false},
		{"exactly the budget", 25, 25, true},
		{"over the budget", 25, 25.01, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			guard := budgetGuard(orgs.NewDefaultService(budgetOrgs{budget: c.budget}),
				agentruns.NewDefaultService(spentRuns{spend: c.spend}, nil, nil))
			blocked, reason := guard("org-1")
			if blocked != c.blocked {
				t.Fatalf("$%.2f spent against a $%.2f budget: blocked = %v (%q), want %v", c.spend, c.budget, blocked, reason, c.blocked)
			}
			if blocked && !strings.Contains(reason, "has reached its $25.00 monthly budget") {
				t.Errorf("the refusal %q does not name the budget", reason)
			}
			if !blocked && reason != "" {
				t.Errorf("a launch let through carries a reason: %q", reason)
			}
		})
	}
}
