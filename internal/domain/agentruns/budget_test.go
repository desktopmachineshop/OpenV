package agentruns

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// The budget guard's own tests, against the WorkspaceBudgets port and a run
// repository's MonthlySpend. cmd/server's TestBudgetGuardRule (refactor step
// X7a) is its characterization, through the orgs service and the boot's
// call.

// budgetOf answers a workspace's monthly budget and records the workspaces
// asked about.
type budgetOf struct {
	budget *float64
	err    error
	asked  []string
}

func (b *budgetOf) MonthlyBudgetUSD(orgID string) (*float64, error) {
	b.asked = append(b.asked, orgID)
	return b.budget, b.err
}

// spendRepo answers MonthlySpend and records the month start each call reads
// from.
type spendRepo struct {
	Repository
	spend float64
	err   error
	from  []time.Time
}

func (r *spendRepo) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	if orgID != "org-1" {
		return 0, errors.New("spend read for another workspace")
	}
	r.from = append(r.from, monthStart)
	return r.spend, r.err
}

func TestBudgetGuard(t *testing.T) {
	usd := func(v float64) *float64 { return &v }
	const tail = "; new runs are blocked until next month or the budget is raised"
	for _, c := range []struct {
		name      string
		budget    *float64
		budgetErr error
		spend     float64
		spendErr  error
		spendRead bool
		blocked   bool
		reason    string
	}{
		{name: "the workspace cannot be read", budgetErr: errors.New("not found"), spend: 100},
		{name: "no budget", spend: 100},
		{name: "a budget of zero", budget: usd(0), spend: 100},
		{name: "a negative budget", budget: usd(-1), spend: 100},
		{name: "under the budget", budget: usd(25), spend: 24.99, spendRead: true},
		{name: "exactly the budget", budget: usd(25), spend: 25, spendRead: true, blocked: true,
			reason: "this workspace has reached its $25.00 monthly budget ($25.00 spent)" + tail},
		{name: "over the budget", budget: usd(10), spend: 1234.5, spendRead: true, blocked: true,
			reason: "this workspace has reached its $10.00 monthly budget ($1234.50 spent)" + tail},
		{name: "the spend cannot be read", budget: usd(25), spend: 30, spendErr: errors.New("timeout"), spendRead: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			budgets := &budgetOf{budget: c.budget, err: c.budgetErr}
			repo := &spendRepo{spend: c.spend, err: c.spendErr}
			guard := BudgetGuard(budgets, NewDefaultService(repo, nil, nil))

			now := time.Now().UTC()
			blocked, reason := guard("org-1")

			if blocked != c.blocked || reason != c.reason {
				t.Errorf("guard = %v, %q; want %v, %q", blocked, reason, c.blocked, c.reason)
			}
			if want := []string{"org-1"}; !reflect.DeepEqual(budgets.asked, want) {
				t.Errorf("budgets read for %q, want %q", budgets.asked, want)
			}
			if !c.spendRead {
				if len(repo.from) != 0 {
					t.Errorf("the spend was read from %v; want it left alone", repo.from)
				}
				return
			}
			first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			if len(repo.from) != 1 || !repo.from[0].Equal(first) || repo.from[0].Location() != time.UTC {
				t.Errorf("the spend was read from %v, want once from %v", repo.from, first)
			}
		})
	}
}

// A guard that blocks refuses the launch with ErrBudgetExceeded and its
// reason, before the agent is looked up.
func TestBudgetGuardRefusesTheLaunch(t *testing.T) {
	budget := 5.0
	svc, repo := newRetryService()
	repo.Repository = &spendRepo{spend: 5}
	svc.SetBudgetGuard(BudgetGuard(&budgetOf{budget: &budget}, svc))

	_, _, err := svc.Launch(LaunchRequest{OrgID: "org-1", AgentID: "agent-1", Prompt: "go"})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("Launch = %v, want ErrBudgetExceeded", err)
	}
	const want = "workspace monthly budget exceeded: this workspace has reached its $5.00 monthly budget ($5.00 spent); " +
		"new runs are blocked until next month or the budget is raised"
	if err.Error() != want {
		t.Errorf("Launch error %q, want %q", err, want)
	}
	if len(repo.runs) != 0 {
		t.Errorf("a refused launch stored %d runs", len(repo.runs))
	}
}
