package orgs

import (
	"encoding/json"
	"errors"
	"testing"
)

// The two reads the run service's policies make of a workspace:
// MonthlyBudgetUSD for the budget guard (agentruns.BudgetGuard) and
// RunnerGraceSeconds for first-refusal routing (agentruns.RoutingPolicy).
// cmd/server's TestBudgetGuardRule and TestRoutingPolicyRule (refactor step
// X7a) are their characterization.

// oneOrgRepo holds one workspace, org-1, or fails every read with err.
type oneOrgRepo struct {
	Repository
	org *Org
	err error
}

func (r oneOrgRepo) FindOrgByID(id string) (*Org, error) {
	if r.err != nil {
		return nil, r.err
	}
	if id != "org-1" || r.org == nil {
		return nil, nil
	}
	o := *r.org
	return &o, nil
}

func TestMonthlyBudgetUSD(t *testing.T) {
	budget := 25.5
	svc := NewDefaultService(oneOrgRepo{org: &Org{ID: "org-1", MonthlyBudgetUSD: &budget}})
	if got, err := svc.MonthlyBudgetUSD("org-1"); err != nil || got == nil || *got != 25.5 {
		t.Errorf("a budget of 25.5: %v, %v", got, err)
	}

	svc = NewDefaultService(oneOrgRepo{org: &Org{ID: "org-1"}})
	if got, err := svc.MonthlyBudgetUSD("org-1"); err != nil || got != nil {
		t.Errorf("no budget: %v, %v; want nil, nil", got, err)
	}
	if got, err := svc.MonthlyBudgetUSD("org-2"); !errors.Is(err, ErrNotFound) || got != nil {
		t.Errorf("no such workspace: %v, %v; want nil, ErrNotFound", got, err)
	}

	failed := errors.New("statement timeout")
	svc = NewDefaultService(oneOrgRepo{err: failed})
	if got, err := svc.MonthlyBudgetUSD("org-1"); !errors.Is(err, failed) || got != nil {
		t.Errorf("a failed read: %v, %v; want nil and the error", got, err)
	}
}

func TestRunnerGraceSeconds(t *testing.T) {
	for _, c := range []struct {
		name, limits string
		want         int
	}{
		{"a grace of 90", `{"runner_grace_seconds": 90}`, 90},
		{"truncated to whole seconds", `{"runner_grace_seconds": 90.9}`, 90},
		{"no upper bound", `{"runner_grace_seconds": 86400}`, 86400},
		{"zero is passed on", `{"runner_grace_seconds": 0}`, 0},
		{"a negative grace is passed on", `{"runner_grace_seconds": -30}`, -30},
		{"a string is no grace", `{"runner_grace_seconds": "90"}`, 0},
		{"null is no grace", `{"runner_grace_seconds": null}`, 0},
		{"no key", `{"max_projects": 7}`, 0},
		{"no limits", `{}`, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			var limits map[string]interface{}
			if err := json.Unmarshal([]byte(c.limits), &limits); err != nil {
				t.Fatal(err)
			}
			svc := NewDefaultService(oneOrgRepo{org: &Org{ID: "org-1", Limits: limits}})
			if got := svc.RunnerGraceSeconds("org-1"); got != c.want {
				t.Errorf("RunnerGraceSeconds = %d, want %d", got, c.want)
			}
		})
	}

	t.Run("an int is no grace: only a JSON number, a float64, reads", func(t *testing.T) {
		svc := NewDefaultService(oneOrgRepo{org: &Org{ID: "org-1", Limits: map[string]interface{}{"runner_grace_seconds": 90}}})
		if got := svc.RunnerGraceSeconds("org-1"); got != 0 {
			t.Errorf("RunnerGraceSeconds = %d, want 0", got)
		}
	})
	t.Run("no such workspace", func(t *testing.T) {
		if got := NewDefaultService(oneOrgRepo{}).RunnerGraceSeconds("org-2"); got != 0 {
			t.Errorf("RunnerGraceSeconds = %d, want 0", got)
		}
	})
	t.Run("the workspace cannot be read", func(t *testing.T) {
		if got := NewDefaultService(oneOrgRepo{err: errors.New("timeout")}).RunnerGraceSeconds("org-1"); got != 0 {
			t.Errorf("RunnerGraceSeconds = %d, want 0", got)
		}
	})
}
