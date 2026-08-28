package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// setRunState mutates queue/lifecycle columns directly so tests can place a
// run in any state without going through the repository under test.
func (f *claimFixture) setRunState(t *testing.T, runID, status, tokenHash string) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE agent_runs SET status = $2, run_token_hash = $3 WHERE id = $1`, runID, status, tokenHash); err != nil {
		t.Fatal(err)
	}
}

func (f *claimFixture) tokenHash(t *testing.T, runID string) string {
	t.Helper()
	var hash string
	if err := f.db.QueryRow(`SELECT run_token_hash FROM agent_runs WHERE id = $1`, runID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return hash
}

func (f *claimFixture) mustFind(t *testing.T, runID string) *agentruns.Run {
	t.Helper()
	run, err := f.repo.FindByID(runID)
	if err != nil || run == nil {
		t.Fatalf("FindByID(%s) = %v, %v", runID, run, err)
	}
	return run
}

func ptr[T any](v T) *T { return &v }

func TestUpdateRewritesMutableFieldsAndKeepsToken(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "keep-me")

	started := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	finished := started.Add(30 * time.Second)
	heartbeat := finished.Add(-5 * time.Second)

	run := f.mustFind(t, id)
	run.Status = agentruns.StatusRunning
	run.CancelRequested = true
	run.WorkerID = "w-9"
	run.HeartbeatAt = &heartbeat
	run.StartedAt = &started
	run.FinishedAt = &finished
	run.ExitCode = ptr(3)
	run.FinalText = "done"
	run.Error = "warning: flaky"
	run.TokensIn = 111
	run.TokensOut = 222
	run.CostUSD = ptr(1.25)
	run.ArtifactsTouched = []map[string]interface{}{{"id": "a1", "op": "update"}}

	if err := f.repo.Update(run); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := f.mustFind(t, id)
	if got.Status != agentruns.StatusRunning || !got.CancelRequested || got.WorkerID != "w-9" {
		t.Errorf("status/cancel/worker = %s/%v/%q", got.Status, got.CancelRequested, got.WorkerID)
	}
	if got.HeartbeatAt == nil || !got.HeartbeatAt.Equal(heartbeat) {
		t.Errorf("heartbeat = %v, want %v", got.HeartbeatAt, heartbeat)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(started) || got.FinishedAt == nil || !got.FinishedAt.Equal(finished) {
		t.Errorf("started/finished = %v/%v, want %v/%v", got.StartedAt, got.FinishedAt, started, finished)
	}
	if got.ExitCode == nil || *got.ExitCode != 3 {
		t.Errorf("exit code = %v, want 3", got.ExitCode)
	}
	if got.FinalText != "done" || got.Error != "warning: flaky" {
		t.Errorf("final/error = %q/%q", got.FinalText, got.Error)
	}
	if got.TokensIn != 111 || got.TokensOut != 222 || got.CostUSD == nil || *got.CostUSD != 1.25 {
		t.Errorf("usage = %d/%d/%v", got.TokensIn, got.TokensOut, got.CostUSD)
	}
	if len(got.ArtifactsTouched) != 1 || got.ArtifactsTouched[0]["id"] != "a1" {
		t.Errorf("artifacts touched = %v", got.ArtifactsTouched)
	}
	// Update must not revoke the run token (that is UpdateTerminal's job)
	// and must not touch identity/queue fields.
	if hash := f.tokenHash(t, id); hash != "keep-me" {
		t.Errorf("token hash after Update = %q, want keep-me", hash)
	}
	if got.Prompt != "do work" {
		t.Errorf("prompt = %q, want untouched", got.Prompt)
	}
}

func TestFindByTokenHash(t *testing.T) {
	f := newClaimFixture(t)

	t.Run("live statuses authenticate", func(t *testing.T) {
		for _, status := range []string{agentruns.StatusQueued, agentruns.StatusClaimed, agentruns.StatusRunning} {
			id := f.queueRun(t, runSpec{})
			hash := "live-" + status
			f.setRunState(t, id, status, hash)
			got, err := f.repo.FindByTokenHash(hash)
			if err != nil {
				t.Fatalf("%s: %v", status, err)
			}
			if got == nil || got.ID != id {
				t.Errorf("FindByTokenHash(%s run) = %v, want run %s", status, got, id)
			}
		}
	})

	t.Run("terminal statuses are refused even with the hash intact", func(t *testing.T) {
		for _, status := range []string{
			agentruns.StatusSucceeded, agentruns.StatusFailed, agentruns.StatusCancelled,
			agentruns.StatusTimedOut, agentruns.StatusAwaitingApproval,
		} {
			id := f.queueRun(t, runSpec{})
			hash := "dead-" + status
			f.setRunState(t, id, status, hash)
			got, err := f.repo.FindByTokenHash(hash)
			if err != nil {
				t.Fatalf("%s: %v", status, err)
			}
			if got != nil {
				t.Errorf("terminal %s run still authenticates by token", status)
			}
		}
	})

	t.Run("empty hash never matches revoked-token rows", func(t *testing.T) {
		id := f.queueRun(t, runSpec{})
		f.setRunState(t, id, agentruns.StatusRunning, "")
		got, err := f.repo.FindByTokenHash("")
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("FindByTokenHash(\"\") = run %s, want nil", got.ID)
		}
	})

	t.Run("unknown hash is nil without error", func(t *testing.T) {
		got, err := f.repo.FindByTokenHash("no-such-token")
		if err != nil || got != nil {
			t.Errorf("FindByTokenHash(unknown) = %v, %v, want nil, nil", got, err)
		}
	})
}

func TestFailStale(t *testing.T) {
	f := newClaimFixture(t)
	cutoff := time.Now().UTC()
	stale := cutoff.Add(-10 * time.Minute)
	fresh := cutoff.Add(10 * time.Minute)

	setHeartbeat := func(id string, at time.Time) {
		if _, err := f.db.Exec(`UPDATE agent_runs SET heartbeat_at = $2 WHERE id = $1`, id, at); err != nil {
			t.Fatal(err)
		}
	}

	staleClaimed := f.queueRun(t, runSpec{})
	f.setRunState(t, staleClaimed, agentruns.StatusClaimed, "tok-claimed")
	setHeartbeat(staleClaimed, stale)

	staleRunning := f.queueRun(t, runSpec{})
	f.setRunState(t, staleRunning, agentruns.StatusRunning, "tok-running")
	setHeartbeat(staleRunning, stale)

	freshRunning := f.queueRun(t, runSpec{})
	f.setRunState(t, freshRunning, agentruns.StatusRunning, "tok-fresh")
	setHeartbeat(freshRunning, fresh)

	queuedNoHeartbeat := f.queueRun(t, runSpec{}) // heartbeat NULL: never stale
	staleSucceeded := f.queueRun(t, runSpec{})    // terminal: out of scope
	f.setRunState(t, staleSucceeded, agentruns.StatusSucceeded, "tok-done")
	setHeartbeat(staleSucceeded, stale)

	ids, err := f.repo.FailStale(cutoff)
	if err != nil {
		t.Fatalf("FailStale: %v", err)
	}
	failed := map[string]bool{}
	for _, id := range ids {
		failed[id] = true
	}
	if len(ids) != 2 || !failed[staleClaimed] || !failed[staleRunning] {
		t.Fatalf("FailStale ids = %v, want exactly {%s, %s}", ids, staleClaimed, staleRunning)
	}

	for _, id := range []string{staleClaimed, staleRunning} {
		run := f.mustFind(t, id)
		if run.Status != agentruns.StatusFailed {
			t.Errorf("run %s status = %s, want failed", id, run.Status)
		}
		if run.Error != "worker lost (heartbeat timeout)" {
			t.Errorf("run %s error = %q", id, run.Error)
		}
		if run.FinishedAt == nil {
			t.Errorf("run %s finished_at not set", id)
		}
		if hash := f.tokenHash(t, id); hash != "" {
			t.Errorf("run %s token hash = %q, want revoked", id, hash)
		}
	}

	if got := f.status(t, freshRunning); got != agentruns.StatusRunning {
		t.Errorf("fresh running run = %s, want untouched", got)
	}
	if got := f.status(t, queuedNoHeartbeat); got != agentruns.StatusQueued {
		t.Errorf("queued run = %s, want untouched", got)
	}
	if got := f.status(t, staleSucceeded); got != agentruns.StatusSucceeded {
		t.Errorf("succeeded run = %s, want untouched", got)
	}
	if hash := f.tokenHash(t, freshRunning); hash != "tok-fresh" {
		t.Errorf("fresh run token = %q, want kept", hash)
	}

	// A second sweep finds nothing new.
	ids, err = f.repo.FailStale(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("second FailStale = %v, want empty", ids)
	}
}

func TestAppendLogsAndListLogs(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})

	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	before := time.Now().UTC().Add(-2 * time.Second)
	err := f.repo.AppendLogs(id, []agentruns.LogEntry{
		{Seq: 1, Kind: "stdout", Payload: map[string]interface{}{"text": "hello"}, CreatedAt: base},
		{Seq: 2, Kind: "tool", Payload: map[string]interface{}{"name": "list_projects", "ok": true}, CreatedAt: base.Add(time.Second)},
		{Seq: 3, Kind: "stdout", Payload: map[string]interface{}{"text": "bye"}}, // zero CreatedAt: stamped now
	})
	if err != nil {
		t.Fatalf("AppendLogs: %v", err)
	}

	logs, err := f.repo.ListLogs(id, 0)
	if err != nil {
		t.Fatalf("ListLogs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("got %d logs, want 3", len(logs))
	}
	for i, want := range []int{1, 2, 3} {
		if logs[i].Seq != want || logs[i].RunID != id {
			t.Errorf("log %d = seq %d run %s", i, logs[i].Seq, logs[i].RunID)
		}
	}
	if logs[0].Payload["text"] != "hello" || logs[1].Payload["ok"] != true || logs[1].Kind != "tool" {
		t.Errorf("payloads did not round-trip: %v", logs)
	}
	if !logs[0].CreatedAt.Equal(base) {
		t.Errorf("log 1 created_at = %v, want %v", logs[0].CreatedAt, base)
	}
	if logs[2].CreatedAt.Before(before) {
		t.Errorf("zero created_at should be stamped at insert time, got %v", logs[2].CreatedAt)
	}

	// Re-sending an existing seq is a no-op (at-least-once delivery), and
	// the rest of the batch still lands.
	err = f.repo.AppendLogs(id, []agentruns.LogEntry{
		{Seq: 2, Kind: "stdout", Payload: map[string]interface{}{"text": "overwrite attempt"}},
		{Seq: 4, Kind: "stdout", Payload: map[string]interface{}{"text": "new"}},
	})
	if err != nil {
		t.Fatalf("AppendLogs (dup): %v", err)
	}
	logs, err = f.repo.ListLogs(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 4 {
		t.Fatalf("got %d logs after dup batch, want 4", len(logs))
	}
	if logs[1].Kind != "tool" || logs[1].Payload["name"] != "list_projects" {
		t.Errorf("duplicate seq overwrote the original entry: %v", logs[1])
	}

	// afterSeq pagination.
	tail, err := f.repo.ListLogs(id, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 2 || tail[0].Seq != 3 || tail[1].Seq != 4 {
		t.Errorf("ListLogs(after 2) = %v, want seqs 3,4", tail)
	}

	// Other runs' logs are invisible.
	other := f.queueRun(t, runSpec{})
	otherLogs, err := f.repo.ListLogs(other, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherLogs) != 0 {
		t.Errorf("other run sees %d logs, want 0", len(otherLogs))
	}
}

func TestUpdateTokenHashRotation(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "old-hash")

	if err := f.repo.UpdateTokenHash(id, "new-hash"); err != nil {
		t.Fatalf("UpdateTokenHash: %v", err)
	}
	if got, err := f.repo.FindByTokenHash("old-hash"); err != nil || got != nil {
		t.Errorf("old hash still authenticates: %v, %v", got, err)
	}
	got, err := f.repo.FindByTokenHash("new-hash")
	if err != nil || got == nil || got.ID != id {
		t.Errorf("new hash lookup = %v, %v, want run %s", got, err, id)
	}
}

func TestCancelQueuedOnlyWhileQueued(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusQueued, "tok")

	ok, err := f.repo.CancelQueued(id)
	if err != nil || !ok {
		t.Fatalf("CancelQueued = %v, %v, want applied", ok, err)
	}
	run := f.mustFind(t, id)
	if run.Status != agentruns.StatusCancelled || !run.CancelRequested || run.FinishedAt == nil {
		t.Errorf("cancelled run = %s cancel %v finished %v", run.Status, run.CancelRequested, run.FinishedAt)
	}
	if hash := f.tokenHash(t, id); hash != "" {
		t.Errorf("token hash = %q, want revoked on cancel", hash)
	}

	// Idempotence: already cancelled -> not applied.
	ok, err = f.repo.CancelQueued(id)
	if err != nil || ok {
		t.Errorf("second CancelQueued = %v, %v, want false", ok, err)
	}

	// A claimed run must not be cancellable through the queued path.
	claimed := f.queueRun(t, runSpec{})
	f.setRunState(t, claimed, agentruns.StatusClaimed, "tok2")
	ok, err = f.repo.CancelQueued(claimed)
	if err != nil || ok {
		t.Fatalf("CancelQueued(claimed) = %v, %v, want false", ok, err)
	}
	if got := f.status(t, claimed); got != agentruns.StatusClaimed {
		t.Errorf("claimed run = %s, want untouched", got)
	}
	if hash := f.tokenHash(t, claimed); hash != "tok2" {
		t.Errorf("claimed run token = %q, want kept", hash)
	}
}

func TestSetCancelRequestedOnlyForActiveRuns(t *testing.T) {
	f := newClaimFixture(t)
	cases := []struct {
		status string
		want   bool
	}{
		{agentruns.StatusClaimed, true},
		{agentruns.StatusRunning, true},
		{agentruns.StatusQueued, false},
		{agentruns.StatusSucceeded, false},
		{agentruns.StatusFailed, false},
	}
	for _, tc := range cases {
		id := f.queueRun(t, runSpec{})
		f.setRunState(t, id, tc.status, "tok")
		ok, err := f.repo.SetCancelRequested(id)
		if err != nil {
			t.Fatalf("%s: %v", tc.status, err)
		}
		if ok != tc.want {
			t.Errorf("SetCancelRequested(%s) = %v, want %v", tc.status, ok, tc.want)
		}
		run := f.mustFind(t, id)
		if run.CancelRequested != tc.want {
			t.Errorf("%s: cancel_requested = %v, want %v", tc.status, run.CancelRequested, tc.want)
		}
	}
}

func TestUpdateTerminalGuardsTerminalStates(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "tok")

	run := f.mustFind(t, id)
	finished := time.Now().UTC().Truncate(time.Millisecond)
	run.Status = agentruns.StatusSucceeded
	run.FinishedAt = &finished
	run.FinalText = "all done"
	run.ExitCode = ptr(0)
	run.ArtifactsTouched = []map[string]interface{}{{"id": "a1"}}

	ok, err := f.repo.UpdateTerminal(run)
	if err != nil || !ok {
		t.Fatalf("UpdateTerminal = %v, %v, want applied", ok, err)
	}
	got := f.mustFind(t, id)
	if got.Status != agentruns.StatusSucceeded || got.FinalText != "all done" {
		t.Errorf("terminal run = %s %q", got.Status, got.FinalText)
	}
	if hash := f.tokenHash(t, id); hash != "" {
		t.Errorf("token hash = %q, want revoked at finish", hash)
	}

	// Once terminal, a second (late/duplicate) terminal write is refused
	// and changes nothing.
	run.Status = agentruns.StatusFailed
	run.FinalText = "late overwrite"
	ok, err = f.repo.UpdateTerminal(run)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("UpdateTerminal applied on an already-terminal run")
	}
	got = f.mustFind(t, id)
	if got.Status != agentruns.StatusSucceeded || got.FinalText != "all done" {
		t.Errorf("terminal run after refused write = %s %q, want unchanged", got.Status, got.FinalText)
	}
}

func TestReleaseClaimRequiresOwningWorker(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	claimed, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, 0, false)
	if err != nil || claimed == nil || claimed.ID != id {
		t.Fatalf("claim = %v, %v", claimed, err)
	}

	// The wrong worker cannot release it.
	ok, err := f.repo.ReleaseClaim(id, "w-2")
	if err != nil || ok {
		t.Fatalf("ReleaseClaim(wrong worker) = %v, %v, want false", ok, err)
	}
	if got := f.status(t, id); got != agentruns.StatusClaimed {
		t.Errorf("run = %s, want still claimed", got)
	}

	// The owning worker returns it to the queue.
	ok, err = f.repo.ReleaseClaim(id, "w-1")
	if err != nil || !ok {
		t.Fatalf("ReleaseClaim = %v, %v, want applied", ok, err)
	}
	run := f.mustFind(t, id)
	if run.Status != agentruns.StatusQueued || run.WorkerID != "" || run.HeartbeatAt != nil {
		t.Errorf("released run = %s/%q/%v, want queued with no worker or heartbeat", run.Status, run.WorkerID, run.HeartbeatAt)
	}

	// A running run is past the release window.
	f.setRunState(t, id, agentruns.StatusRunning, "")
	if _, err := f.db.Exec(`UPDATE agent_runs SET worker_id = 'w-1' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	ok, err = f.repo.ReleaseClaim(id, "w-1")
	if err != nil || ok {
		t.Errorf("ReleaseClaim(running) = %v, %v, want false", ok, err)
	}
}

func TestHeartbeatRefreshesLiveness(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "tok")

	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := f.repo.Heartbeat(id, at); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	run := f.mustFind(t, id)
	if run.HeartbeatAt == nil || !run.HeartbeatAt.Equal(at) {
		t.Errorf("heartbeat = %v, want %v", run.HeartbeatAt, at)
	}

	// A fresh heartbeat keeps the run out of FailStale's sweep.
	ids, err := f.repo.FailStale(at.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("FailStale after heartbeat = %v, want empty", ids)
	}

	// Unknown run: no-op, no error.
	if err := f.repo.Heartbeat(uuid.New().String(), at); err != nil {
		t.Errorf("Heartbeat(unknown) = %v, want nil", err)
	}
}
