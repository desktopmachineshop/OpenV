package postgres

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMonthlySpend locks in the month-scoped spend SQL (issue #186): it sums
// cost_usd for the org's runs created at/after monthStart, counts NULL costs
// as zero, and never leaks another org's spend or a prior month's runs.
func TestMonthlySpend(t *testing.T) {
	f := newClaimFixture(t)

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	// A run safely inside the current month (start + a day, or start itself on
	// the 1st) and one safely in the previous month.
	thisMonth := monthStart.AddDate(0, 0, 0).Add(12 * time.Hour)
	if now.Day() == 1 {
		thisMonth = monthStart.Add(1 * time.Hour)
	}
	lastMonth := monthStart.AddDate(0, 0, -5)

	f.seedUsageRun(t, f.agentID, thisMonth, 100, 40, ptr(1.50), "succeeded")
	f.seedUsageRun(t, f.agentID, thisMonth, 50, 10, ptr(0.75), "succeeded")
	// NULL cost (a live/not-yet-costed run) counts as zero, not an error.
	f.seedUsageRun(t, f.agentID, thisMonth, 10, 5, nil, "running")
	// Previous month must not count.
	f.seedUsageRun(t, f.agentID, lastMonth, 900, 300, ptr(9.99), "succeeded")

	// Another org's current-month run must be invisible.
	otherOrg := uuid.New().String()
	otherAgent := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Other', 'other-budget-org')`, otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'worker', 'Worker', 'claude')`, otherAgent, otherOrg); err != nil {
		t.Fatal(err)
	}
	foreign := f.seedUsageRunForOrg(t, otherOrg, otherAgent, thisMonth, 1, 1, ptr(42.0), "succeeded")
	_ = foreign

	spend, err := f.repo.MonthlySpend(f.orgID, monthStart)
	if err != nil {
		t.Fatalf("MonthlySpend: %v", err)
	}
	if math.Abs(spend-2.25) > 1e-9 {
		t.Fatalf("spend = %v, want 2.25 (1.50 + 0.75, NULL as zero, prior month and other org excluded)", spend)
	}

	// An org with no runs this month spends nothing (not an error).
	empty, err := f.repo.MonthlySpend(uuid.New().String(), monthStart)
	if err != nil {
		t.Fatalf("MonthlySpend(empty): %v", err)
	}
	if empty != 0 {
		t.Errorf("empty spend = %v, want 0", empty)
	}
}

// seedUsageRunForOrg is seedUsageRun for an arbitrary org (cross-tenant test).
func (f *claimFixture) seedUsageRunForOrg(t *testing.T, orgID, agentID string, createdAt time.Time, tokensIn, tokensOut int64, cost *float64, status string) string {
	t.Helper()
	id := uuid.New().String()
	if _, err := f.db.Exec(`
		INSERT INTO agent_runs (id, org_id, agent_id, status, prompt, tokens_in, tokens_out, cost_usd, artifacts_touched, created_at)
		VALUES ($1, $2, $3, $4, 'work', $5, $6, $7, '[]'::jsonb, $8)
	`, id, orgID, agentID, status, tokensIn, tokensOut, cost, createdAt); err != nil {
		t.Fatalf("seed cross-org run: %v", err)
	}
	return id
}

// TestBudgetAlertClaimAndSetBudget locks in the org-repo budget surface: the
// alert claim fires exactly once per threshold per month (higher thresholds
// escalate, a new month resets), and SetBudget round-trips through FindOrgByID
// (including clearing back to NULL).
func TestBudgetAlertClaimAndSetBudget(t *testing.T) {
	f := newClaimFixture(t)
	repo := NewOrgRepository(f.db)

	// SetBudget round-trips, and nil clears it.
	if err := repo.SetBudget(f.orgID, ptr(120.0)); err != nil {
		t.Fatalf("SetBudget: %v", err)
	}
	org, err := repo.FindOrgByID(f.orgID)
	if err != nil {
		t.Fatalf("FindOrgByID: %v", err)
	}
	if org.MonthlyBudgetUSD == nil || math.Abs(*org.MonthlyBudgetUSD-120.0) > 1e-9 {
		t.Fatalf("budget = %v, want 120", org.MonthlyBudgetUSD)
	}
	if err := repo.SetBudget(f.orgID, nil); err != nil {
		t.Fatalf("SetBudget(nil): %v", err)
	}
	if org, _ = repo.FindOrgByID(f.orgID); org.MonthlyBudgetUSD != nil {
		t.Fatalf("budget after clear = %v, want nil", org.MonthlyBudgetUSD)
	}

	// Alert claim dedupe.
	claim := func(month string, threshold int) bool {
		won, err := repo.ClaimBudgetAlert(f.orgID, month, threshold)
		if err != nil {
			t.Fatalf("ClaimBudgetAlert: %v", err)
		}
		return won
	}

	if !claim("2026-08", 80) {
		t.Fatal("first 80% claim should win")
	}
	if claim("2026-08", 80) {
		t.Fatal("second 80% claim in the same month must not win")
	}
	if !claim("2026-08", 100) {
		t.Fatal("100% claim escalates and should win")
	}
	if claim("2026-08", 100) {
		t.Fatal("second 100% claim must not win")
	}
	if claim("2026-08", 80) {
		t.Fatal("a lower threshold after 100% must not win")
	}
	if !claim("2026-09", 80) {
		t.Fatal("a new month resets and the 80% claim should win again")
	}
}
