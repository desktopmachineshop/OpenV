package agentruns

import (
	"errors"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/events"
)

// fakeRunRepo embeds the interface so only the methods the lifecycle paths
// under test touch need real implementations (anything else panics loudly).
// The conditional methods mirror the SQL semantics: they check the STORED
// status, not whatever stale copy the service holds, and report whether the
// transition was applied.
type fakeRunRepo struct {
	Repository
	runs map[string]*Run
	logs map[string][]LogEntry

	// pendingProposals is returned by CountPendingProposals;
	// applyFailedProposals by CountApplyFailedProposals.
	pendingProposals     int
	applyFailedProposals int

	// Interleaving hooks: run just before the conditional check, so tests can
	// simulate a concurrent actor winning the race.
	onCancelQueued   func()
	onUpdateTerminal func()
	onMarkRunning    func()
}

// FindByID returns a copy, like a real repository row scan, so service-side
// mutations never leak into the store without an explicit write.
func (f *fakeRunRepo) FindByID(id string) (*Run, error) {
	r, ok := f.runs[id]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

// Save stores a copy, like a real repository insert.
func (f *fakeRunRepo) Save(r *Run) error {
	cp := *r
	f.runs[r.ID] = &cp
	return nil
}

func (f *fakeRunRepo) CancelQueued(id string) (bool, error) {
	if f.onCancelQueued != nil {
		f.onCancelQueued()
	}
	r, ok := f.runs[id]
	if !ok || r.Status != StatusQueued {
		return false, nil
	}
	now := time.Now()
	r.Status = StatusCancelled
	r.CancelRequested = true
	r.FinishedAt = &now
	r.RunTokenHash = ""
	return true, nil
}

func (f *fakeRunRepo) SetCancelRequested(id string) (bool, error) {
	r, ok := f.runs[id]
	if !ok || (r.Status != StatusClaimed && r.Status != StatusRunning) {
		return false, nil
	}
	r.CancelRequested = true
	return true, nil
}

func (f *fakeRunRepo) UpdateTerminal(run *Run) (bool, error) {
	if f.onUpdateTerminal != nil {
		f.onUpdateTerminal()
	}
	stored, ok := f.runs[run.ID]
	if !ok || terminalStatuses[stored.Status] || stored.Status == StatusAwaitingApproval {
		return false, nil
	}
	cp := *run
	cp.RunTokenHash = ""
	f.runs[run.ID] = &cp
	return true, nil
}

func (f *fakeRunRepo) ReleaseClaim(runID, workerID string) (bool, error) {
	r, ok := f.runs[runID]
	if !ok || r.Status != StatusClaimed || r.WorkerID != workerID {
		return false, nil
	}
	r.Status = StatusQueued
	r.WorkerID = ""
	r.HeartbeatAt = nil
	return true, nil
}

func (f *fakeRunRepo) MarkRunning(id string, at time.Time) (bool, error) {
	if f.onMarkRunning != nil {
		f.onMarkRunning()
	}
	r, ok := f.runs[id]
	if !ok || r.Status != StatusClaimed {
		return false, nil
	}
	r.Status = StatusRunning
	r.StartedAt = &at
	r.HeartbeatAt = &at
	return true, nil
}

func (f *fakeRunRepo) Heartbeat(id string, at time.Time) (bool, error) {
	r, ok := f.runs[id]
	if !ok || (r.Status != StatusClaimed && r.Status != StatusRunning) {
		return false, nil
	}
	r.HeartbeatAt = &at
	return true, nil
}

func (f *fakeRunRepo) AppendLogs(runID string, entries []LogEntry) error {
	f.logs[runID] = append(f.logs[runID], entries...)
	return nil
}

func (f *fakeRunRepo) UpdateWorkItemID(runID, workItemID string) error {
	if r, ok := f.runs[runID]; ok {
		r.WorkItemID = &workItemID
	}
	return nil
}

func (f *fakeRunRepo) CountPendingProposals(string) (int, error) {
	return f.pendingProposals, nil
}

func (f *fakeRunRepo) CountApplyFailedProposals(string) (int, error) {
	return f.applyFailedProposals, nil
}

// FinalizeApproval mirrors the SQL: it transitions the STORED run only while
// it is still awaiting_approval, so a concurrent resolver can never
// double-finalize; reports whether it was applied.
func (f *fakeRunRepo) FinalizeApproval(runID, status string, at time.Time) (bool, error) {
	r, ok := f.runs[runID]
	if !ok || r.Status != StatusAwaitingApproval {
		return false, nil
	}
	r.Status = status
	r.FinishedAt = &at
	r.RunTokenHash = ""
	return true, nil
}

// FailStale mirrors the SQL reaper: claimed/running runs whose heartbeat
// predates cutoff (a nil heartbeat counts as stale) are failed and their ids
// returned.
func (f *fakeRunRepo) FailStale(cutoff time.Time) ([]string, error) {
	var ids []string
	for id, r := range f.runs {
		if r.Status != StatusClaimed && r.Status != StatusRunning {
			continue
		}
		if r.HeartbeatAt != nil && !r.HeartbeatAt.Before(cutoff) {
			continue
		}
		now := time.Now()
		r.Status = StatusFailed
		r.Error = "worker lost (heartbeat timeout)"
		r.ErrorClass = ErrorClassWorkerError
		r.FinishedAt = &now
		r.RunTokenHash = ""
		ids = append(ids, id)
	}
	return ids, nil
}

// fakeBus records published events so lifecycle tests can assert the run
// service emits RunFinished on every terminal transition.
type fakeBus struct {
	published []events.Event
}

func (b *fakeBus) Publish(e events.Event)       { b.published = append(b.published, e) }
func (b *fakeBus) Subscribe(func(events.Event)) {}

func (b *fakeBus) finished() []events.Event {
	var out []events.Event
	for _, e := range b.published {
		if e.EventType == events.RunFinished {
			out = append(out, e)
		}
	}
	return out
}

func newFakeServiceWithBus(bus events.Bus, runs ...*Run) (*DefaultService, *fakeRunRepo) {
	repo := &fakeRunRepo{runs: map[string]*Run{}, logs: map[string][]LogEntry{}}
	for _, r := range runs {
		repo.runs[r.ID] = r
	}
	return NewDefaultService(repo, nil, bus), repo
}

func newFakeService(runs ...*Run) (*DefaultService, *fakeRunRepo) {
	repo := &fakeRunRepo{runs: map[string]*Run{}, logs: map[string][]LogEntry{}}
	for _, r := range runs {
		repo.runs[r.ID] = r
	}
	return NewDefaultService(repo, nil, nil), repo
}

// fakeAgentCatalog answers Launch's agent lookup (Retry goes through Launch).
type fakeAgentCatalog struct {
	agents.Service
	byID map[string]*agents.Agent
}

func (f *fakeAgentCatalog) Get(id string) (*agents.Agent, error) {
	return f.byID[id], nil
}

// newRetryService wires a service whose agent catalog knows "agent-1".
func newRetryService(runs ...*Run) (*DefaultService, *fakeRunRepo) {
	repo := &fakeRunRepo{runs: map[string]*Run{}, logs: map[string][]LogEntry{}}
	for _, r := range runs {
		repo.runs[r.ID] = r
	}
	catalog := &fakeAgentCatalog{byID: map[string]*agents.Agent{
		"agent-1": {ID: "agent-1", Name: "Reviewer", Provider: "claude"},
	}}
	return NewDefaultService(repo, catalog, nil), repo
}

func strptr(s string) *string { return &s }

// TestLaunchCapturesReproducibilitySnapshot locks in issue #216: a run records
// the agent's content_hash, model and effort as they are at launch, and a later
// edit to the agent definition does not rewrite an already-launched run's
// snapshot (that is the whole point — the snapshot pins what actually executed).
func TestLaunchCapturesReproducibilitySnapshot(t *testing.T) {
	agent := &agents.Agent{
		ID:          "agent-1",
		Name:        "Reviewer",
		Provider:    "claude",
		Model:       "claude-opus-4-1",
		Effort:      "high",
		ContentHash: "hash-v1",
	}
	catalog := &fakeAgentCatalog{byID: map[string]*agents.Agent{"agent-1": agent}}
	repo := &fakeRunRepo{runs: map[string]*Run{}, logs: map[string][]LogEntry{}}
	svc := NewDefaultService(repo, catalog, nil)

	run, _, err := svc.Launch(LaunchRequest{OrgID: "org-1", AgentID: "agent-1", Prompt: "review the spec"})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if run.AgentContentHash != "hash-v1" || run.AgentModel != "claude-opus-4-1" || run.AgentEffort != "high" {
		t.Fatalf("snapshot = %q/%q/%q, want hash-v1/claude-opus-4-1/high",
			run.AgentContentHash, run.AgentModel, run.AgentEffort)
	}

	// Edit the agent after launch: a fresh launch captures the NEW identity...
	agent.Model = "claude-opus-4-2"
	agent.Effort = "max"
	agent.ContentHash = "hash-v2"

	run2, _, err := svc.Launch(LaunchRequest{OrgID: "org-1", AgentID: "agent-1", Prompt: "again"})
	if err != nil {
		t.Fatalf("Launch 2: %v", err)
	}
	if run2.AgentContentHash != "hash-v2" || run2.AgentModel != "claude-opus-4-2" || run2.AgentEffort != "max" {
		t.Errorf("second run snapshot = %q/%q/%q, want hash-v2/claude-opus-4-2/max",
			run2.AgentContentHash, run2.AgentModel, run2.AgentEffort)
	}

	// ...while the first run's stored snapshot still pins the original identity.
	stored, err := repo.FindByID(run.ID)
	if err != nil || stored == nil {
		t.Fatalf("FindByID(%s) = %v, %v", run.ID, stored, err)
	}
	if stored.AgentContentHash != "hash-v1" || stored.AgentModel != "claude-opus-4-1" || stored.AgentEffort != "high" {
		t.Errorf("first run snapshot after agent edit = %q/%q/%q, want hash-v1/claude-opus-4-1/high (must not change)",
			stored.AgentContentHash, stored.AgentModel, stored.AgentEffort)
	}
}

// TestRetryEnqueuesFreshRunWithProvenance locks in the retry contract: a NEW
// queued run copying org/agent/project/prompt, launched by the retrying user
// (not the original launcher), with retried_from_run_id set — and none of the
// source's automation/crew/delegation/kanban links or terminal state.
func TestRetryEnqueuesFreshRunWithProvenance(t *testing.T) {
	project := "proj-1"
	source := &Run{
		ID:           "r1",
		OrgID:        "org-1",
		AgentID:      "agent-1",
		ProjectID:    &project,
		AutomationID: strptr("auto-1"),
		TeamID:       strptr("team-1"),
		ParentRunID:  strptr("parent-1"),
		WorkItemID:   strptr("card-1"),
		Status:       StatusFailed,
		Priority:     PriorityChild,
		Prompt:       "review the spec",
		Error:        "boom",
		LaunchedBy:   strptr("original-user"),
	}
	svc, repo := newRetryService(source)

	run, err := svc.Retry("r1", strptr("retrying-user"))
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if run.ID == "r1" {
		t.Fatal("Retry must enqueue a NEW run, not reuse the source")
	}
	if run.Status != StatusQueued {
		t.Errorf("status = %q, want queued", run.Status)
	}
	if run.OrgID != "org-1" || run.AgentID != "agent-1" || run.Prompt != "review the spec" {
		t.Errorf("org/agent/prompt not copied: %+v", run)
	}
	if run.ProjectID == nil || *run.ProjectID != project {
		t.Errorf("project_id = %v, want %q", run.ProjectID, project)
	}
	if run.RetriedFromRunID == nil || *run.RetriedFromRunID != "r1" {
		t.Errorf("retried_from_run_id = %v, want r1", run.RetriedFromRunID)
	}
	if run.LaunchedBy == nil || *run.LaunchedBy != "retrying-user" {
		t.Errorf("launched_by = %v, want the retrying user", run.LaunchedBy)
	}
	if run.AutomationID != nil || run.TeamID != nil || run.ParentRunID != nil || run.WorkItemID != nil {
		t.Errorf("automation/crew/delegation/kanban links must not be copied: %+v", run)
	}
	if run.Priority != PriorityNormal {
		t.Errorf("priority = %d, want normal for a manual retry", run.Priority)
	}
	if run.Error != "" {
		t.Errorf("error = %q, want empty on the fresh run", run.Error)
	}
	stored := repo.runs[run.ID]
	if stored == nil || stored.Status != StatusQueued {
		t.Fatalf("retried run not persisted as queued: %+v", stored)
	}
	if repo.runs["r1"].Status != StatusFailed {
		t.Errorf("source run status mutated to %s", repo.runs["r1"].Status)
	}
}

// TestRetryOnlyTerminalFailures locks in the status gate: failed, cancelled
// and timed_out retry; queued/claimed/running/succeeded/awaiting_approval
// answer ErrNotRetryable (retrying success invites duplicate side effects).
func TestRetryOnlyTerminalFailures(t *testing.T) {
	retryable := []string{StatusFailed, StatusCancelled, StatusTimedOut}
	for _, status := range retryable {
		svc, _ := newRetryService(&Run{ID: "r1", OrgID: "org-1", AgentID: "agent-1", Status: status, Prompt: "p"})
		if _, err := svc.Retry("r1", strptr("u")); err != nil {
			t.Errorf("Retry(%s) = %v, want success", status, err)
		}
	}

	refused := []string{StatusQueued, StatusClaimed, StatusRunning, StatusSucceeded, StatusAwaitingApproval}
	for _, status := range refused {
		svc, repo := newRetryService(&Run{ID: "r1", OrgID: "org-1", AgentID: "agent-1", Status: status, Prompt: "p"})
		_, err := svc.Retry("r1", strptr("u"))
		if !errors.Is(err, ErrNotRetryable) {
			t.Errorf("Retry(%s) err = %v, want ErrNotRetryable", status, err)
		}
		if len(repo.runs) != 1 {
			t.Errorf("Retry(%s) enqueued a run despite the refusal", status)
		}
	}
}

func TestRetryUnknownRun(t *testing.T) {
	svc, _ := newRetryService()
	if _, err := svc.Retry("missing", strptr("u")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Retry(unknown) err = %v, want ErrNotFound", err)
	}
}

// findRetryOf returns the queued run the repo holds whose provenance points at
// sourceID (the auto-retry re-enqueue), or nil.
func findRetryOf(repo *fakeRunRepo, sourceID string) *Run {
	for _, r := range repo.runs {
		if r.RetriedFromRunID != nil && *r.RetriedFromRunID == sourceID {
			return r
		}
	}
	return nil
}

func runningRun(id string, attempt, max int) *Run {
	now := time.Now()
	return &Run{
		ID:           id,
		OrgID:        "org-1",
		AgentID:      "agent-1",
		Status:       StatusRunning,
		Prompt:       "do work",
		LaunchedBy:   strptr("u"),
		AttemptCount: attempt,
		MaxAttempts:  max,
		HeartbeatAt:  &now,
	}
}

// TestFinishAutoRetriesRetryableFailureUntilExhausted: a retryable terminal
// failure re-enqueues a fresh attempt (with provenance + backoff) while the
// attempt budget lasts, and stops once it is spent.
func TestFinishAutoRetriesRetryableFailureUntilExhausted(t *testing.T) {
	svc, repo := newRetryService(runningRun("r1", 1, 3))

	// Attempt 1 fails with a retryable class -> attempt 2 is enqueued.
	if _, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassWorkerError, Error: "worker died"}); err != nil {
		t.Fatalf("Finish attempt 1: %v", err)
	}
	retry2 := findRetryOf(repo, "r1")
	if retry2 == nil {
		t.Fatal("attempt 1 failure did not enqueue a retry")
	}
	if retry2.Status != StatusQueued {
		t.Errorf("retry status = %q, want queued", retry2.Status)
	}
	if retry2.AttemptCount != 2 || retry2.MaxAttempts != 3 {
		t.Errorf("retry attempt tracking = %d/%d, want 2/3", retry2.AttemptCount, retry2.MaxAttempts)
	}
	if retry2.NextAttemptAt == nil || !retry2.NextAttemptAt.After(time.Now()) {
		t.Error("retry must carry a future next_attempt_at (backoff)")
	}
	if retry2.LaunchedBy == nil || *retry2.LaunchedBy != "u" {
		t.Error("retry must preserve the launcher for personal-runner routing")
	}

	// Attempt 2 fails the same way -> attempt 3 (the last) is enqueued.
	if _, err := svc.Finish(retry2.ID, FinishRequest{Status: StatusTimedOut, ErrorClass: ErrorClassTimeout}); err != nil {
		t.Fatalf("Finish attempt 2: %v", err)
	}
	retry3 := findRetryOf(repo, retry2.ID)
	if retry3 == nil || retry3.AttemptCount != 3 {
		t.Fatalf("attempt 2 failure did not enqueue attempt 3 (got %+v)", retry3)
	}

	// Attempt 3 exhausts the budget -> no further retry.
	if _, err := svc.Finish(retry3.ID, FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassProviderUnavailable}); err != nil {
		t.Fatalf("Finish attempt 3: %v", err)
	}
	if r := findRetryOf(repo, retry3.ID); r != nil {
		t.Errorf("budget exhausted but a 4th attempt was enqueued: %+v", r)
	}
	if len(repo.runs) != 3 {
		t.Errorf("attempt chain produced %d runs, want 3", len(repo.runs))
	}
}

// TestFinishDoesNotRetryNonRetryableClass: auth / agent_error / workspace
// failures stand — retrying cannot fix them.
func TestFinishDoesNotRetryNonRetryableClass(t *testing.T) {
	for _, class := range []string{ErrorClassAuth, ErrorClassAgentError, ErrorClassWorkspace} {
		svc, repo := newRetryService(runningRun("r1", 1, 3))
		if _, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: class}); err != nil {
			t.Fatalf("Finish(%s): %v", class, err)
		}
		if len(repo.runs) != 1 {
			t.Errorf("class %q enqueued a retry (%d runs), want none", class, len(repo.runs))
		}
	}
}

// TestFinishDoesNotAutoRetryOrchestratedRuns: a run carrying a parent /
// interview / automation / guided linkage is owned by that orchestrator, whose
// RunFinished hook routes the failure. Auto-retrying here would orphan the new
// run from its tree/session and let its elevated priority jump the queue only
// to have the result discarded — so orchestrated runs never auto-retry even on
// a retryable class.
func TestFinishDoesNotAutoRetryOrchestratedRuns(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*Run)
	}{
		{"child", func(r *Run) { r.ParentRunID = strptr("parent-1"); r.Priority = PriorityChild }},
		{"interview", func(r *Run) { r.InterviewSessionID = strptr("sess-1"); r.Priority = PriorityInterview }},
		{"automation", func(r *Run) { r.AutomationID = strptr("auto-1") }},
		{"guided", func(r *Run) { r.GuidedSessionID = strptr("guided-1") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runningRun("r1", 1, 3)
			tc.apply(run)
			svc, repo := newRetryService(run)
			// A retryable class that WOULD retry a top-level run.
			if _, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassWorkerError}); err != nil {
				t.Fatalf("Finish: %v", err)
			}
			if r := findRetryOf(repo, "r1"); r != nil {
				t.Errorf("orchestrated run auto-retried: %+v", r)
			}
			if len(repo.runs) != 1 {
				t.Errorf("orchestrated run produced %d runs, want 1 (no retry)", len(repo.runs))
			}
		})
	}
}

// TestFinishAutoRetriesTopLevelRun: a plain user-launched run (no orchestration
// linkage) with a retryable class still auto-retries — the guard added for
// orchestrated runs must not suppress the ordinary case.
func TestFinishAutoRetriesTopLevelRun(t *testing.T) {
	svc, repo := newRetryService(runningRun("r1", 1, 3))
	if _, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassWorkerError}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if findRetryOf(repo, "r1") == nil {
		t.Error("top-level run with a retryable class did not auto-retry")
	}
}

// TestFinishRespectsAutoRetryOptOut: with auto-retry disabled, even a
// retryable failure simply stands.
func TestFinishRespectsAutoRetryOptOut(t *testing.T) {
	svc, repo := newRetryService(runningRun("r1", 1, 3))
	svc.SetRetryPolicy(3, false)
	if _, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassWorkerError}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if len(repo.runs) != 1 {
		t.Errorf("auto-retry disabled but %d runs exist, want 1", len(repo.runs))
	}
}

// TestFailStaleAutoRetries: a run whose worker went silent is a worker_error
// (retryable), so the reaper's failure re-enqueues a fresh attempt.
func TestFailStaleAutoRetries(t *testing.T) {
	stale := runningRun("r1", 1, 3)
	old := time.Now().Add(-time.Hour)
	stale.HeartbeatAt = &old
	svc, repo := newRetryService(stale)

	ids, err := svc.FailStale(2 * time.Minute)
	if err != nil {
		t.Fatalf("FailStale: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("FailStale returned %d ids, want 1", len(ids))
	}
	retry := findRetryOf(repo, "r1")
	if retry == nil || retry.AttemptCount != 2 || retry.Status != StatusQueued {
		t.Fatalf("reaper failure did not enqueue a retry (got %+v)", retry)
	}
}

func TestRequestCancelQueuedCancelsAtomically(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusQueued, RunTokenHash: "hash"})

	run, err := svc.RequestCancel("r1")
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if run.Status != StatusCancelled {
		t.Errorf("returned status = %q, want cancelled", run.Status)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusCancelled || !stored.CancelRequested || stored.FinishedAt == nil {
		t.Errorf("stored run not cancelled cleanly: %+v", stored)
	}
	if stored.RunTokenHash != "" {
		t.Error("cancelling a queued run must revoke its token")
	}
}

func TestRequestCancelLosesRaceToClaim(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusQueued})
	// A worker claims the run between the service's read and its conditional
	// cancel: the claim must survive, and the cancel becomes cooperative.
	repo.onCancelQueued = func() {
		now := time.Now()
		repo.runs["r1"].Status = StatusClaimed
		repo.runs["r1"].WorkerID = "w-1"
		repo.runs["r1"].HeartbeatAt = &now
	}

	run, err := svc.RequestCancel("r1")
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusClaimed {
		t.Errorf("stored status = %q, want the concurrent claim preserved", stored.Status)
	}
	if stored.WorkerID != "w-1" {
		t.Errorf("worker_id = %q, want w-1 preserved (was stomped before the fix)", stored.WorkerID)
	}
	if !stored.CancelRequested {
		t.Error("cancel_requested flag should be set on the claimed run")
	}
	if run.Status != StatusClaimed || !run.CancelRequested {
		t.Errorf("returned run = %+v, want claimed with cancel_requested", run)
	}
}

func TestRequestCancelRunningSetsFlagOnly(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusRunning, WorkerID: "w-1"})

	run, err := svc.RequestCancel("r1")
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusRunning || stored.WorkerID != "w-1" {
		t.Errorf("running run's state must be untouched: %+v", stored)
	}
	if !stored.CancelRequested || !run.CancelRequested {
		t.Error("cancel_requested flag should be set")
	}
}

func TestRequestCancelTerminalIsNoop(t *testing.T) {
	for _, status := range []string{StatusSucceeded, StatusFailed, StatusCancelled, StatusTimedOut, StatusAwaitingApproval} {
		svc, repo := newFakeService(&Run{ID: "r1", Status: status})
		run, err := svc.RequestCancel("r1")
		if err != nil {
			t.Fatalf("RequestCancel(%s): %v", status, err)
		}
		if run.Status != status || run.CancelRequested {
			t.Errorf("terminal run %s was mutated: %+v", status, run)
		}
		if repo.runs["r1"].CancelRequested {
			t.Errorf("stored %s run gained a cancel flag", status)
		}
	}
}

func TestFinishWritesTerminalStateAndRevokesToken(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusRunning, WorkerID: "w-1", RunTokenHash: "hash"})

	run, err := svc.Finish("r1", FinishRequest{Status: StatusSucceeded, FinalText: "done"})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if run.Status != StatusSucceeded || run.RunTokenHash != "" {
		t.Errorf("returned run = %+v, want succeeded with token revoked", run)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusSucceeded || stored.FinishedAt == nil {
		t.Errorf("stored run = %+v, want succeeded", stored)
	}
	if stored.RunTokenHash != "" {
		t.Error("finishing a run must revoke its token")
	}
}

func TestFinishRefusesSecondTerminalReport(t *testing.T) {
	svc, _ := newFakeService(&Run{ID: "r1", Status: StatusRunning})
	if _, err := svc.Finish("r1", FinishRequest{Status: StatusSucceeded}); err != nil {
		t.Fatalf("first Finish: %v", err)
	}
	_, err := svc.Finish("r1", FinishRequest{Status: StatusFailed})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second Finish err = %v, want ErrInvalidTransition", err)
	}
}

func TestFinishLosesRaceToReaper(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusRunning})
	// The stale reaper fails the run between the service's read and its
	// conditional terminal write: the reaper's verdict must stand.
	repo.onUpdateTerminal = func() {
		repo.runs["r1"].Status = StatusFailed
		repo.runs["r1"].Error = "worker lost (heartbeat timeout)"
	}

	_, err := svc.Finish("r1", FinishRequest{Status: StatusSucceeded})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Finish err = %v, want ErrInvalidTransition", err)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusFailed || stored.Error == "" {
		t.Errorf("reaper's terminal state was overwritten: %+v", stored)
	}
}

func TestFinishWithPendingProposalsAwaitsApproval(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusRunning, RunTokenHash: "hash"})
	repo.pendingProposals = 2

	run, err := svc.Finish("r1", FinishRequest{Status: StatusSucceeded})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if run.Status != StatusAwaitingApproval {
		t.Errorf("status = %q, want awaiting_approval", run.Status)
	}
	if repo.runs["r1"].RunTokenHash != "" {
		t.Error("awaiting_approval run must still have its token revoked")
	}
}

func TestReleaseClaimReturnsRunToQueue(t *testing.T) {
	now := time.Now()
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusClaimed, WorkerID: "w-1", HeartbeatAt: &now})

	if err := svc.ReleaseClaim("r1", "w-1"); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusQueued || stored.WorkerID != "" || stored.HeartbeatAt != nil {
		t.Errorf("stored run = %+v, want back in queue with no worker", stored)
	}
}

func TestReleaseClaimIsConditional(t *testing.T) {
	cases := []struct {
		name string
		run  *Run
	}{
		{"different worker", &Run{ID: "r1", Status: StatusClaimed, WorkerID: "w-other"}},
		{"already running", &Run{ID: "r1", Status: StatusRunning, WorkerID: "w-1"}},
		{"already terminal", &Run{ID: "r1", Status: StatusFailed, WorkerID: "w-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := *tc.run
			svc, repo := newFakeService(tc.run)
			if err := svc.ReleaseClaim("r1", "w-1"); err != nil {
				t.Fatalf("ReleaseClaim: %v", err)
			}
			stored := repo.runs["r1"]
			if stored.Status != before.Status || stored.WorkerID != before.WorkerID {
				t.Errorf("run was mutated: %+v, want %+v", stored, before)
			}
		})
	}
}

func TestMarkRunningTransitionsClaimedRun(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusClaimed, WorkerID: "w-1"})

	if err := svc.MarkRunning("r1"); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusRunning || stored.StartedAt == nil || stored.HeartbeatAt == nil {
		t.Errorf("stored run = %+v, want running with started_at/heartbeat_at set", stored)
	}
}

func TestMarkRunningRefusesNonClaimedRun(t *testing.T) {
	for _, status := range []string{StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled, StatusTimedOut, StatusAwaitingApproval} {
		svc, repo := newFakeService(&Run{ID: "r1", Status: status})
		err := svc.MarkRunning("r1")
		if !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("MarkRunning(%s) err = %v, want ErrInvalidTransition", status, err)
		}
		if repo.runs["r1"].Status != status {
			t.Errorf("stored %s run was mutated to %s", status, repo.runs["r1"].Status)
		}
	}
}

func TestMarkRunningLosesRaceToReaper(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusClaimed, WorkerID: "w-1"})
	// The stale reaper fails the run between the worker's start report and the
	// conditional write: the failure must stand instead of being resurrected
	// to running.
	repo.onMarkRunning = func() {
		repo.runs["r1"].Status = StatusFailed
		repo.runs["r1"].Error = "worker lost (heartbeat timeout)"
	}

	err := svc.MarkRunning("r1")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("MarkRunning err = %v, want ErrInvalidTransition", err)
	}
	stored := repo.runs["r1"]
	if stored.Status != StatusFailed || stored.Error == "" {
		t.Errorf("reaper's terminal state was overwritten: %+v", stored)
	}
}

func TestAppendLogsRefreshesHeartbeatOnLiveRun(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusRunning})

	run, err := svc.AppendLogs("r1", []LogEntry{{RunID: "r1", Seq: 1, Kind: LogText}})
	if err != nil {
		t.Fatalf("AppendLogs: %v", err)
	}
	if run.Status != StatusRunning {
		t.Errorf("returned status = %q, want running", run.Status)
	}
	if repo.runs["r1"].HeartbeatAt == nil {
		t.Error("heartbeat_at should be refreshed for a live run")
	}
	if len(repo.logs["r1"]) != 1 {
		t.Errorf("stored %d log entries, want 1", len(repo.logs["r1"]))
	}
}

func TestAppendLogsKeepsLogsButNotHeartbeatOnTerminalRun(t *testing.T) {
	// A late log batch from a worker whose run the reaper already failed:
	// the logs are kept for the audit trail, but heartbeat_at must not be
	// refreshed — the run is not alive.
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusFailed, Error: "worker lost (heartbeat timeout)"})

	run, err := svc.AppendLogs("r1", []LogEntry{{RunID: "r1", Seq: 7, Kind: LogText}})
	if err != nil {
		t.Fatalf("AppendLogs: %v", err)
	}
	if run.Status != StatusFailed {
		t.Errorf("returned status = %q, want failed so the worker sees the verdict", run.Status)
	}
	if repo.runs["r1"].HeartbeatAt != nil {
		t.Error("heartbeat_at was refreshed on a terminal run")
	}
	if len(repo.logs["r1"]) != 1 {
		t.Errorf("stored %d log entries, want the late batch kept", len(repo.logs["r1"]))
	}
}

func TestHeartbeatIgnoresTerminalRun(t *testing.T) {
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusCancelled})

	if err := svc.Heartbeat("r1"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if repo.runs["r1"].HeartbeatAt != nil {
		t.Error("heartbeat_at was set on a terminal run")
	}
}

func TestAttachWorkItemNeverTouchesStatus(t *testing.T) {
	// AttachWorkItem must be a targeted work_item_id write: attaching a card
	// to a run that finished meanwhile must not resurrect its old status.
	svc, repo := newFakeService(&Run{ID: "r1", Status: StatusFailed, Error: "boom"})

	if err := svc.AttachWorkItem("r1", "card-1"); err != nil {
		t.Fatalf("AttachWorkItem: %v", err)
	}
	stored := repo.runs["r1"]
	if stored.WorkItemID == nil || *stored.WorkItemID != "card-1" {
		t.Errorf("work_item_id = %v, want card-1", stored.WorkItemID)
	}
	if stored.Status != StatusFailed || stored.Error != "boom" {
		t.Errorf("run state was mutated beyond work_item_id: %+v", stored)
	}
}

// TestFailStalePublishesRunFinished locks in the reaper fix: failing a
// silent worker's run must publish RunFinished (status failed) on the bus, or
// the notifier and automation triggers miss the most common failure — a
// crashed worker — entirely.
func TestFailStalePublishesRunFinished(t *testing.T) {
	bus := &fakeBus{}
	project := "proj-1"
	stale := time.Now().Add(-10 * time.Minute)
	svc, repo := newFakeServiceWithBus(bus, &Run{
		ID:          "r1",
		OrgID:       "org-1",
		AgentID:     "agent-1",
		ProjectID:   &project,
		Status:      StatusRunning,
		HeartbeatAt: &stale,
		LaunchedBy:  strptr("user-9"),
	})

	ids, err := svc.FailStale(2 * time.Minute)
	if err != nil {
		t.Fatalf("FailStale: %v", err)
	}
	if len(ids) != 1 || ids[0] != "r1" {
		t.Fatalf("reaped ids = %v, want [r1]", ids)
	}
	if repo.runs["r1"].Status != StatusFailed {
		t.Fatalf("stored status = %q, want failed", repo.runs["r1"].Status)
	}

	finished := bus.finished()
	if len(finished) != 1 {
		t.Fatalf("RunFinished events = %d, want 1", len(finished))
	}
	e := finished[0]
	if e.Payload["status"] != StatusFailed {
		t.Errorf("event status = %v, want failed", e.Payload["status"])
	}
	if e.EntityID != "r1" {
		t.Errorf("event entity = %q, want r1", e.EntityID)
	}
	if e.Payload["launched_by"] != "user-9" {
		t.Errorf("event launched_by = %v, want user-9 (so the notifier can alert the launcher)", e.Payload["launched_by"])
	}
	if e.OrgID != "org-1" {
		t.Errorf("event org = %q, want org-1", e.OrgID)
	}
}

// TestFinalizeIfResolvedSucceedsWhenProposalsResolved is the awaiting_approval
// exit: with no pending proposals and none apply-failed, the run leaves the
// absorbing state for succeeded and publishes RunFinished.
func TestFinalizeIfResolvedSucceedsWhenProposalsResolved(t *testing.T) {
	bus := &fakeBus{}
	svc, repo := newFakeServiceWithBus(bus, &Run{ID: "r1", OrgID: "org-1", AgentID: "a1", Status: StatusAwaitingApproval, LaunchedBy: strptr("user-9")})
	repo.pendingProposals = 0
	repo.applyFailedProposals = 0

	run, err := svc.FinalizeIfResolved("r1")
	if err != nil {
		t.Fatalf("FinalizeIfResolved: %v", err)
	}
	if run.Status != StatusSucceeded {
		t.Errorf("returned status = %q, want succeeded", run.Status)
	}
	if repo.runs["r1"].Status != StatusSucceeded || repo.runs["r1"].FinishedAt == nil {
		t.Errorf("stored run = %+v, want succeeded with finished_at", repo.runs["r1"])
	}
	finished := bus.finished()
	if len(finished) != 1 || finished[0].Payload["status"] != StatusSucceeded {
		t.Fatalf("RunFinished events = %+v, want one succeeded", finished)
	}
}

// TestFinalizeIfResolvedFailsWhenApplyFailed resolves to failed when any
// approved write failed to apply.
func TestFinalizeIfResolvedFailsWhenApplyFailed(t *testing.T) {
	bus := &fakeBus{}
	svc, repo := newFakeServiceWithBus(bus, &Run{ID: "r1", OrgID: "org-1", AgentID: "a1", Status: StatusAwaitingApproval})
	repo.pendingProposals = 0
	repo.applyFailedProposals = 1

	run, err := svc.FinalizeIfResolved("r1")
	if err != nil {
		t.Fatalf("FinalizeIfResolved: %v", err)
	}
	if run.Status != StatusFailed {
		t.Errorf("returned status = %q, want failed", run.Status)
	}
	if run.Error == "" {
		t.Error("failed finalize should carry an error explaining the apply failure")
	}
	finished := bus.finished()
	if len(finished) != 1 || finished[0].Payload["status"] != StatusFailed {
		t.Fatalf("RunFinished events = %+v, want one failed", finished)
	}
}

// TestFinalizeIfResolvedNoopWhilePending leaves the run in awaiting_approval
// (and publishes nothing) while any proposal is still pending review.
func TestFinalizeIfResolvedNoopWhilePending(t *testing.T) {
	bus := &fakeBus{}
	svc, repo := newFakeServiceWithBus(bus, &Run{ID: "r1", Status: StatusAwaitingApproval})
	repo.pendingProposals = 2

	run, err := svc.FinalizeIfResolved("r1")
	if err != nil {
		t.Fatalf("FinalizeIfResolved: %v", err)
	}
	if run.Status != StatusAwaitingApproval {
		t.Errorf("status = %q, want awaiting_approval preserved", run.Status)
	}
	if repo.runs["r1"].Status != StatusAwaitingApproval {
		t.Errorf("stored status = %q, want awaiting_approval preserved", repo.runs["r1"].Status)
	}
	if len(bus.finished()) != 0 {
		t.Errorf("no RunFinished should publish while proposals are pending: %+v", bus.finished())
	}
}

// TestFinalizeIfResolvedIgnoresNonAwaitingRun is a no-op for any run that is
// not awaiting approval, so a proposal resolved against an already-terminal
// run never re-publishes or mutates it.
func TestFinalizeIfResolvedIgnoresNonAwaitingRun(t *testing.T) {
	for _, status := range []string{StatusRunning, StatusSucceeded, StatusFailed, StatusQueued} {
		bus := &fakeBus{}
		svc, repo := newFakeServiceWithBus(bus, &Run{ID: "r1", Status: status})
		repo.pendingProposals = 0

		run, err := svc.FinalizeIfResolved("r1")
		if err != nil {
			t.Fatalf("FinalizeIfResolved(%s): %v", status, err)
		}
		if run.Status != status {
			t.Errorf("status = %q, want %q untouched", run.Status, status)
		}
		if len(bus.finished()) != 0 {
			t.Errorf("FinalizeIfResolved(%s) published %+v, want nothing", status, bus.finished())
		}
	}
}

func TestTruncateRuneSafety(t *testing.T) {
	if got := Truncate("héllo", 100); got != "héllo" {
		t.Errorf("short text should pass through, got %q", got)
	}
	// "é" is 2 bytes; a 3-byte budget lands mid-rune and must back off.
	got := Truncate("aéé", 3)
	if got != "aé…" {
		t.Errorf("Truncate = %q, want %q", got, "aé…")
	}
}
