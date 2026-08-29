package postgres

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// seedUsageRun inserts a run with explicit usage columns, bypassing the
// queue lifecycle (usage aggregates whatever the table holds).
func (f *claimFixture) seedUsageRun(t *testing.T, agentID string, createdAt time.Time, tokensIn, tokensOut int64, cost *float64, status string) string {
	t.Helper()
	run := &agentruns.Run{
		ID:               uuid.New().String(),
		OrgID:            f.orgID,
		AgentID:          agentID,
		Status:           status,
		Prompt:           "work",
		TokensIn:         tokensIn,
		TokensOut:        tokensOut,
		CostUSD:          cost,
		ArtifactsTouched: []map[string]interface{}{},
		CreatedAt:        createdAt,
	}
	if err := f.repo.Save(run); err != nil {
		t.Fatalf("seed usage run: %v", err)
	}
	return run.ID
}

// TestUsageRollup locks in the usage SQL contract: runs are grouped by agent
// slug (largest token consumers first) and by UTC calendar day (ascending),
// scoped to the org, cut off at since, with NULL costs treated as zero.
func TestUsageRollup(t *testing.T) {
	f := newClaimFixture(t)

	day := func(daysAgo int, hour int) time.Time {
		return time.Now().UTC().AddDate(0, 0, -daysAgo).Truncate(24 * time.Hour).Add(time.Duration(hour) * time.Hour)
	}
	since := day(7, 0)

	// worker agent: two runs on the same day, one without cost (still live).
	f.seedUsageRun(t, f.agentID, day(2, 9), 100, 40, ptr(0.5), agentruns.StatusSucceeded)
	f.seedUsageRun(t, f.agentID, day(2, 15), 50, 10, nil, agentruns.StatusRunning)
	// coder agent: one bigger run on another day.
	f.seedUsageRun(t, f.repoAgent, day(1, 8), 900, 300, ptr(2.25), agentruns.StatusFailed)
	// Outside the window: must not count.
	f.seedUsageRun(t, f.agentID, day(30, 12), 5000, 5000, ptr(9.99), agentruns.StatusSucceeded)

	// Another org's runs must be invisible.
	otherOrg := uuid.New().String()
	otherAgent := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Other', 'other-org')`, otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'worker', 'Worker', 'claude')`, otherAgent, otherOrg); err != nil {
		t.Fatal(err)
	}
	foreign := &agentruns.Run{
		ID: uuid.New().String(), OrgID: otherOrg, AgentID: otherAgent, Status: agentruns.StatusSucceeded,
		Prompt: "foreign", TokensIn: 777, TokensOut: 777, ArtifactsTouched: []map[string]interface{}{}, CreatedAt: day(1, 9),
	}
	if err := f.repo.Save(foreign); err != nil {
		t.Fatal(err)
	}

	byAgent, byDay, err := f.repo.Usage(f.orgID, since)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}

	if len(byAgent) != 2 {
		t.Fatalf("byAgent = %+v, want 2 agents", byAgent)
	}
	// coder first: 1200 tokens vs worker's 200.
	if byAgent[0].AgentSlug != "coder" || byAgent[0].Runs != 1 || byAgent[0].TokensIn != 900 || byAgent[0].TokensOut != 300 {
		t.Errorf("byAgent[0] = %+v, want coder with 1 run, 900/300 tokens", byAgent[0])
	}
	if math.Abs(byAgent[0].CostUSD-2.25) > 1e-9 {
		t.Errorf("coder cost = %v, want 2.25", byAgent[0].CostUSD)
	}
	if byAgent[1].AgentSlug != "worker" || byAgent[1].Runs != 2 || byAgent[1].TokensIn != 150 || byAgent[1].TokensOut != 50 {
		t.Errorf("byAgent[1] = %+v, want worker with 2 runs, 150/50 tokens", byAgent[1])
	}
	if math.Abs(byAgent[1].CostUSD-0.5) > 1e-9 {
		t.Errorf("worker cost = %v, want 0.5 (NULL cost counted as zero)", byAgent[1].CostUSD)
	}

	if len(byDay) != 2 {
		t.Fatalf("byDay = %+v, want 2 days", byDay)
	}
	wantOlder := day(2, 0).Format("2006-01-02")
	wantNewer := day(1, 0).Format("2006-01-02")
	if byDay[0].Day != wantOlder || byDay[1].Day != wantNewer {
		t.Errorf("byDay order = [%s, %s], want ascending [%s, %s]", byDay[0].Day, byDay[1].Day, wantOlder, wantNewer)
	}
	if byDay[0].Runs != 2 || byDay[0].TokensIn != 150 || byDay[0].TokensOut != 50 {
		t.Errorf("byDay[0] = %+v, want the worker day's 2 runs and 150/50 tokens", byDay[0])
	}
	if byDay[1].Runs != 1 || byDay[1].TokensIn != 900 || byDay[1].TokensOut != 300 {
		t.Errorf("byDay[1] = %+v, want the coder day's run", byDay[1])
	}

	// An empty window aggregates to nothing (not an error).
	byAgent, byDay, err = f.repo.Usage(f.orgID, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("Usage(empty window): %v", err)
	}
	if len(byAgent) != 0 || len(byDay) != 0 {
		t.Errorf("empty window returned %+v / %+v, want nothing", byAgent, byDay)
	}
}

// TestRetriedFromRoundTrip locks in the provenance column: Save persists
// retried_from_run_id and FindByID returns it, while ordinary runs keep it
// NULL (and parent_run_id stays untouched — a retry is not a child run).
func TestRetriedFromRoundTrip(t *testing.T) {
	f := newClaimFixture(t)
	sourceID := f.queueRun(t, runSpec{})

	retry := &agentruns.Run{
		ID:               uuid.New().String(),
		OrgID:            f.orgID,
		AgentID:          f.agentID,
		RetriedFromRunID: &sourceID,
		Status:           agentruns.StatusQueued,
		Prompt:           "do work",
		ArtifactsTouched: []map[string]interface{}{},
		CreatedAt:        time.Now(),
	}
	if err := f.repo.Save(retry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := f.mustFind(t, retry.ID)
	if got.RetriedFromRunID == nil || *got.RetriedFromRunID != sourceID {
		t.Errorf("retried_from_run_id = %v, want %s", got.RetriedFromRunID, sourceID)
	}
	if got.ParentRunID != nil {
		t.Errorf("parent_run_id = %v, want NULL (retry must not create a run-tree child)", got.ParentRunID)
	}
	if children, err := f.repo.ListChildren(sourceID); err != nil || len(children) != 0 {
		t.Errorf("ListChildren(source) = %v, %v — a retry must not appear in the source's run tree", children, err)
	}

	source := f.mustFind(t, sourceID)
	if source.RetriedFromRunID != nil {
		t.Errorf("source retried_from_run_id = %v, want NULL", source.RetriedFromRunID)
	}
}
