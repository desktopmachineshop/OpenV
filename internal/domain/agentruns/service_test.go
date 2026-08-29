package agentruns

import (
	"errors"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agents"
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

	// pendingProposals is returned by CountPendingProposals.
	pendingProposals int

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
