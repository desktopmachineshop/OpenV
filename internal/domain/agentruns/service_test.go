package agentruns

import (
	"errors"
	"testing"
	"time"
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
