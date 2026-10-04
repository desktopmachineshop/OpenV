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
	// Both stale runs were mid-answer when their worker went away.
	for _, id := range []string{staleClaimed, staleRunning} {
		if applied, err := f.repo.UpdatePartialText(id, "Your vision statement is va"); err != nil || !applied {
			t.Fatalf("seed partial text on %s = %v, %v", id, applied, err)
		}
	}

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
		// A failed run has no reply coming: the half-written answer must go
		// with it, or the run detail panel renders "Output so far" under a
		// live cursor on a terminal run.
		if run.PartialText != "" {
			t.Errorf("run %s kept partial_text %q after being reaped", id, run.PartialText)
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

// TestAppendNoteNumbersAfterTheLastEntry: a note the server keeps on a run
// is written at the end of its log, numbered after the worker's last entry
// (or 1 on an empty log), reads back with the log, and is refused for a run
// no row has.
func TestAppendNoteNumbersAfterTheLastEntry(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	empty := f.queueRun(t, runSpec{})
	if err := f.repo.AppendLogs(id, []agentruns.LogEntry{
		{Seq: 1, Kind: "text", Payload: map[string]interface{}{"text": "hello"}},
		{Seq: 7, Kind: "text", Payload: map[string]interface{}{"text": "bye"}},
	}); err != nil {
		t.Fatalf("AppendLogs: %v", err)
	}

	note, err := f.repo.AppendNote(id, agentruns.LogEntry{Kind: agentruns.LogMarker,
		Payload: map[string]interface{}{"marker": "handoff_refused", "message": "refused"}})
	if err != nil {
		t.Fatalf("AppendNote: %v", err)
	}
	if note.Seq != 8 || note.RunID != id || note.CreatedAt.IsZero() {
		t.Errorf("note = %+v, want seq 8 of run %s with a time", note, id)
	}
	second, err := f.repo.AppendNote(id, agentruns.LogEntry{Kind: agentruns.LogMarker, Payload: map[string]interface{}{"marker": "x"}})
	if err != nil || second.Seq != 9 {
		t.Errorf("second note = %+v, %v, want seq 9", second, err)
	}
	logs, err := f.repo.ListLogs(id, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].Seq != 8 || logs[0].Kind != agentruns.LogMarker || logs[0].Payload["message"] != "refused" {
		t.Errorf("log after 7 = %+v, want the two notes, the first saying refused", logs)
	}

	if first, err := f.repo.AppendNote(empty, agentruns.LogEntry{Kind: agentruns.LogMarker, Payload: map[string]interface{}{}}); err != nil || first.Seq != 1 {
		t.Errorf("a note on an empty log = %+v, %v, want seq 1", first, err)
	}
	if _, err := f.repo.AppendNote(uuid.New().String(), agentruns.LogEntry{Kind: agentruns.LogMarker}); err != agentruns.ErrNotFound {
		t.Errorf("a note on a run no row has = %v, want ErrNotFound", err)
	}
}

func TestUpdateTokenHashRotation(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "old-hash")

	if applied, err := f.repo.UpdateTokenHash(id, "new-hash"); err != nil || !applied {
		t.Fatalf("UpdateTokenHash = %v, %v, want applied", applied, err)
	}
	if got, err := f.repo.FindByTokenHash("old-hash"); err != nil || got != nil {
		t.Errorf("old hash still authenticates: %v, %v", got, err)
	}
	got, err := f.repo.FindByTokenHash("new-hash")
	if err != nil || got == nil || got.ID != id {
		t.Errorf("new hash lookup = %v, %v, want run %s", got, err, id)
	}
}

// TestRequestCancelWritesWhatTheRunsStatusAsks: a queued run is cancelled,
// finished, its token revoked; a claimed or running run is asked to stop,
// its status and token kept for its worker to report it cancelled; a
// finished run, or one awaiting approval, is left as it is.
func TestRequestCancelWritesWhatTheRunsStatusAsks(t *testing.T) {
	f := newClaimFixture(t)
	cases := []struct {
		status, want string
		written      bool
		token        string
	}{
		{agentruns.StatusQueued, agentruns.StatusCancelled, true, ""},
		{agentruns.StatusClaimed, agentruns.StatusClaimed, true, "tok"},
		{agentruns.StatusRunning, agentruns.StatusRunning, true, "tok"},
		{agentruns.StatusSucceeded, agentruns.StatusSucceeded, false, "tok"},
		{agentruns.StatusFailed, agentruns.StatusFailed, false, "tok"},
		{agentruns.StatusCancelled, agentruns.StatusCancelled, false, "tok"},
		{agentruns.StatusTimedOut, agentruns.StatusTimedOut, false, "tok"},
		{agentruns.StatusAwaitingApproval, agentruns.StatusAwaitingApproval, false, "tok"},
	}
	for _, tc := range cases {
		id := f.queueRun(t, runSpec{})
		f.setRunState(t, id, tc.status, "tok")
		ok, err := f.repo.RequestCancel(id)
		if err != nil {
			t.Fatalf("%s: %v", tc.status, err)
		}
		if ok != tc.written {
			t.Errorf("RequestCancel(%s) = %v, want %v", tc.status, ok, tc.written)
		}
		run := f.mustFind(t, id)
		if run.Status != tc.want || run.CancelRequested != tc.written {
			t.Errorf("%s: %s, cancel_requested = %v; want %s, %v", tc.status, run.Status, run.CancelRequested, tc.want, tc.written)
		}
		if hash := f.tokenHash(t, id); hash != tc.token {
			t.Errorf("%s: token hash = %q, want %q", tc.status, hash, tc.token)
		}
		if finished := run.FinishedAt != nil; finished != (tc.status == agentruns.StatusQueued) {
			t.Errorf("%s: finished at %v, want it set only on the queued run it cancelled", tc.status, run.FinishedAt)
		}
		// A second request writes the flag again on a live run, and nothing
		// on the run the first one cancelled.
		again, err := f.repo.RequestCancel(id)
		if err != nil || again != (tc.written && tc.status != agentruns.StatusQueued) {
			t.Errorf("%s: a second RequestCancel = %v, %v", tc.status, again, err)
		}
	}
	if ok, err := f.repo.RequestCancel(uuid.New().String()); err != nil || ok {
		t.Errorf("RequestCancel(no such run) = %v, %v, want false and no error", ok, err)
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

	// The worker had begun writing an answer before handing the run back.
	if applied, err := f.repo.UpdatePartialText(id, "Half an answ"); err != nil || !applied {
		t.Fatalf("seed partial text = %v, %v", applied, err)
	}

	// The wrong worker cannot release it.
	ok, err := f.repo.ReleaseClaim(id, "w-2")
	if err != nil || ok {
		t.Fatalf("ReleaseClaim(wrong worker) = %v, %v, want false", ok, err)
	}
	if got := f.status(t, id); got != agentruns.StatusClaimed {
		t.Errorf("run = %s, want still claimed", got)
	}
	if got := f.mustFind(t, id).PartialText; got != "Half an answ" {
		t.Errorf("refused release changed partial_text to %q", got)
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
	// The next worker writes its own answer: a re-queued run must not carry
	// the departing worker's half-written one.
	if run.PartialText != "" {
		t.Errorf("re-queued run kept partial_text %q", run.PartialText)
	}

	// A running run can still be released by its owning worker: this is the
	// worker-shutdown path (SIGINT releases in-flight runs back to the queue
	// rather than failing them). ReleaseClaim covers status IN ('claimed','running').
	f.setRunState(t, id, agentruns.StatusRunning, "")
	if _, err := f.db.Exec(`UPDATE agent_runs SET worker_id = 'w-1' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	// The wrong worker still cannot release it.
	if ok, err := f.repo.ReleaseClaim(id, "w-2"); err != nil || ok {
		t.Errorf("ReleaseClaim(running, wrong worker) = %v, %v, want false", ok, err)
	}
	// The owning worker returns it to the queue.
	ok, err = f.repo.ReleaseClaim(id, "w-1")
	if err != nil || !ok {
		t.Errorf("ReleaseClaim(running, owner) = %v, %v, want applied", ok, err)
	}
	if got := f.status(t, id); got != agentruns.StatusQueued {
		t.Errorf("run = %s, want queued after owner release", got)
	}
}

// TestReleaseClaimRevokesTheRunToken: a released run's token stops
// authenticating with the release, not only when the run is claimed again.
// Whoever the departing worker handed the token to (the agent it started)
// no longer acts for the run; the next claim issues a fresh token.
func TestReleaseClaimRevokesTheRunToken(t *testing.T) {
	f := newClaimFixture(t)
	for _, status := range []string{agentruns.StatusClaimed, agentruns.StatusRunning} {
		id := f.queueRun(t, runSpec{})
		hash := "held-" + status
		f.setRunState(t, id, status, hash)
		if _, err := f.db.Exec(`UPDATE agent_runs SET worker_id = 'w-1' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if got, err := f.repo.FindByTokenHash(hash); err != nil || got == nil {
			t.Fatalf("%s: the held run's token = %v, %v, want it to authenticate", status, got, err)
		}

		ok, err := f.repo.ReleaseClaim(id, "w-1")
		if err != nil || !ok {
			t.Fatalf("%s: ReleaseClaim = %v, %v, want applied", status, ok, err)
		}
		got, err := f.repo.FindByTokenHash(hash)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("%s: the released run's old token still authenticates as run %s, want it refused", status, got.ID)
		}
		if got := f.tokenHash(t, id); got != "" {
			t.Errorf("%s: released run's token hash = %q, want revoked", status, got)
		}
	}

	// A refused release (another worker's) keeps the holder's token.
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "kept")
	if _, err := f.db.Exec(`UPDATE agent_runs SET worker_id = 'w-1' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.repo.ReleaseClaim(id, "w-2"); err != nil || ok {
		t.Fatalf("ReleaseClaim(wrong worker) = %v, %v, want false", ok, err)
	}
	if got := f.tokenHash(t, id); got != "kept" {
		t.Errorf("a refused release changed the token hash to %q", got)
	}
}

// TestUpdateTerminalTakesOnlyAHeldRun: a finish writes only into a run a
// worker holds. A queued run, never claimed or released back, takes no
// result and keeps its token.
func TestUpdateTerminalTakesOnlyAHeldRun(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusQueued, "tok")

	run := f.mustFind(t, id)
	finished := time.Now().UTC()
	run.Status = agentruns.StatusSucceeded
	run.FinishedAt = &finished
	run.FinalText = "Nothing to do."
	ok, err := f.repo.UpdateTerminal(run)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("UpdateTerminal applied to a queued run")
	}
	got := f.mustFind(t, id)
	if got.Status != agentruns.StatusQueued || got.FinalText != "" || got.FinishedAt != nil {
		t.Errorf("queued run after a refused finish = %s %q %v, want unchanged", got.Status, got.FinalText, got.FinishedAt)
	}
	if hash := f.tokenHash(t, id); hash != "tok" {
		t.Errorf("token hash = %q, want kept", hash)
	}

	// A claimed run, which a worker holds before it starts, finishes.
	f.setRunState(t, id, agentruns.StatusClaimed, "tok")
	run.Status = agentruns.StatusFailed
	run.Error = "no adapter for provider claude"
	if ok, err := f.repo.UpdateTerminal(run); err != nil || !ok {
		t.Fatalf("UpdateTerminal(claimed) = %v, %v, want applied", ok, err)
	}
	if got := f.mustFind(t, id); got.Status != agentruns.StatusFailed || got.Error != run.Error {
		t.Errorf("claimed run after finish = %s %q, want failed with its error", got.Status, got.Error)
	}
}

// TestFinalizeIfResolvedStoresTheApplyFailure runs the run service over the
// repository: a run awaiting approval whose approved proposal failed to
// apply is finalised failed with the reason stored, so that the run read back
// says why, as the status it broadcast did, and with the error class
// agent_error, which is not retried (OpenV REQ-84).
func TestFinalizeIfResolvedStoresTheApplyFailure(t *testing.T) {
	f := newClaimFixture(t)
	failed := f.queueRun(t, runSpec{})
	f.setRunState(t, failed, agentruns.StatusAwaitingApproval, "")
	clean := f.queueRun(t, runSpec{})
	f.setRunState(t, clean, agentruns.StatusAwaitingApproval, "")
	project := uuid.New().String()
	seedProjects(t, f.db, project)
	for _, p := range []struct{ run, status string }{{failed, "apply_failed"}, {failed, "rejected"}, {clean, "applied"}} {
		if _, err := f.db.Exec(`INSERT INTO agent_proposals (id, run_id, project_id, op, status) VALUES ($1, $2, $3, 'create_link', $4)`,
			uuid.New().String(), p.run, project, p.status); err != nil {
			t.Fatal(err)
		}
	}
	svc := agentruns.NewDefaultService(f.repo, nil, nil)

	run, err := svc.FinalizeIfResolved(failed)
	if err != nil {
		t.Fatalf("FinalizeIfResolved: %v", err)
	}
	const reason = "one or more approved proposals failed to apply"
	if run.Status != agentruns.StatusFailed || run.Error != reason {
		t.Fatalf("finalised run = %s %q, want failed with %q", run.Status, run.Error, reason)
	}
	stored := f.mustFind(t, failed)
	if stored.Status != agentruns.StatusFailed || stored.Error != reason || stored.FinishedAt == nil {
		t.Errorf("stored run = %s %q finished %v, want failed with %q", stored.Status, stored.Error, stored.FinishedAt, reason)
	}
	if stored.ErrorClass != agentruns.ErrorClassAgentError || agentruns.IsRetryableClass(stored.ErrorClass) {
		t.Errorf("stored error class = %q, want agent_error, which is not retried", stored.ErrorClass)
	}

	// A run whose proposals all landed succeeds with no error and no class.
	if _, err := svc.FinalizeIfResolved(clean); err != nil {
		t.Fatalf("FinalizeIfResolved(clean): %v", err)
	}
	if stored := f.mustFind(t, clean); stored.Status != agentruns.StatusSucceeded || stored.Error != "" || stored.ErrorClass != "" {
		t.Errorf("stored clean run = %s %q class %q, want succeeded with no error and no class", stored.Status, stored.Error, stored.ErrorClass)
	}
}

func TestHeartbeatRefreshesLiveness(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "tok")

	at := time.Now().UTC().Truncate(time.Millisecond)
	applied, err := f.repo.Heartbeat(id, at)
	if err != nil || !applied {
		t.Fatalf("Heartbeat = %v, %v, want applied", applied, err)
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

	// Unknown run: not applied, no error.
	if applied, err := f.repo.Heartbeat(uuid.New().String(), at); err != nil || applied {
		t.Errorf("Heartbeat(unknown) = %v, %v, want false, nil", applied, err)
	}
}

// TestRunRoundTripsRoutingColumns locks in the issue-#158 fix: FindByID and
// List (which share runColumns/scanRun) return preferred_user_id and
// hosted_after, so run JSON matches the frontend's Run type and the
// AgentRunsPage "reserved for personal runner" badge can render.
func TestRunRoundTripsRoutingColumns(t *testing.T) {
	f := newClaimFixture(t)
	hostedAfter := time.Now().UTC().Add(30 * time.Second).Truncate(time.Millisecond)
	reserved := f.queueRun(t, runSpec{launchedBy: &f.userA, preferred: &f.userA, hostedAfter: hostedAfter})
	plain := f.queueRun(t, runSpec{})

	got := f.mustFind(t, reserved)
	if got.PreferredUserID == nil || *got.PreferredUserID != f.userA {
		t.Errorf("FindByID preferred_user_id = %v, want %s", got.PreferredUserID, f.userA)
	}
	if got.HostedAfter == nil || !got.HostedAfter.Equal(hostedAfter) {
		t.Errorf("FindByID hosted_after = %v, want %v", got.HostedAfter, hostedAfter)
	}

	if got := f.mustFind(t, plain); got.PreferredUserID != nil || got.HostedAfter != nil {
		t.Errorf("unreserved run routing columns = %v/%v, want nil/nil", got.PreferredUserID, got.HostedAfter)
	}

	list, err := f.repo.List(agentruns.ListFilter{OrgID: f.orgID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, run := range list {
		if run.ID != reserved {
			continue
		}
		found = true
		if run.PreferredUserID == nil || *run.PreferredUserID != f.userA {
			t.Errorf("List preferred_user_id = %v, want %s", run.PreferredUserID, f.userA)
		}
		if run.HostedAfter == nil || !run.HostedAfter.Equal(hostedAfter) {
			t.Errorf("List hosted_after = %v, want %v", run.HostedAfter, hostedAfter)
		}
	}
	if !found {
		t.Fatal("reserved run missing from List")
	}
}

// TestRunRoundTripsReproducibilitySnapshot locks in issue #216: the launch-time
// agent snapshot (content hash, model, effort) round-trips through Save and the
// shared runColumns/scanRun read path, and a run saved without a snapshot reads
// back blank (the pre-feature / migration-0014 default), never NULL.
func TestRunRoundTripsReproducibilitySnapshot(t *testing.T) {
	f := newClaimFixture(t)

	snapshotted := &agentruns.Run{
		ID:               uuid.New().String(),
		OrgID:            f.orgID,
		AgentID:          f.agentID,
		Status:           agentruns.StatusQueued,
		Prompt:           "do work",
		ArtifactsTouched: []map[string]interface{}{},
		CreatedAt:        time.Now(),
		AgentContentHash: "abc123def456",
		AgentModel:       "claude-opus-4-1",
		AgentEffort:      "high",
	}
	if err := f.repo.Save(snapshotted); err != nil {
		t.Fatalf("save snapshotted run: %v", err)
	}
	blank := f.queueRun(t, runSpec{}) // queueRun sets no snapshot fields

	got := f.mustFind(t, snapshotted.ID)
	if got.AgentContentHash != "abc123def456" || got.AgentModel != "claude-opus-4-1" || got.AgentEffort != "high" {
		t.Errorf("FindByID snapshot = %q/%q/%q, want abc123def456/claude-opus-4-1/high",
			got.AgentContentHash, got.AgentModel, got.AgentEffort)
	}
	if b := f.mustFind(t, blank); b.AgentContentHash != "" || b.AgentModel != "" || b.AgentEffort != "" {
		t.Errorf("un-snapshotted run = %q/%q/%q, want all blank", b.AgentContentHash, b.AgentModel, b.AgentEffort)
	}

	list, err := f.repo.List(agentruns.ListFilter{OrgID: f.orgID})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, run := range list {
		if run.ID != snapshotted.ID {
			continue
		}
		found = true
		if run.AgentContentHash != "abc123def456" || run.AgentModel != "claude-opus-4-1" || run.AgentEffort != "high" {
			t.Errorf("List snapshot = %q/%q/%q, want abc123def456/claude-opus-4-1/high",
				run.AgentContentHash, run.AgentModel, run.AgentEffort)
		}
	}
	if !found {
		t.Fatal("snapshotted run missing from List")
	}
}

// TestListFailsClosedOnOrg locks in the issue-#180 fix: the run listing's org
// predicate is mandatory. A run in another workspace never appears in this
// org's listing, and an empty OrgID (an unresolved active workspace) returns
// nothing rather than every tenant's runs.
func TestListFailsClosedOnOrg(t *testing.T) {
	f := newClaimFixture(t)
	mine := f.queueRun(t, runSpec{})

	// A second workspace with its own agent and queued run.
	otherOrg := uuid.New().String()
	otherAgent := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Other Org', 'other-org')`, otherOrg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'worker', 'Worker', 'claude')`, otherAgent, otherOrg); err != nil {
		t.Fatal(err)
	}
	otherRun := uuid.New().String()
	if err := f.repo.Save(&agentruns.Run{
		ID: otherRun, OrgID: otherOrg, AgentID: otherAgent,
		Status: agentruns.StatusQueued, Prompt: "elsewhere",
		ArtifactsTouched: []map[string]interface{}{}, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save other-org run: %v", err)
	}

	t.Run("org listing excludes other workspaces", func(t *testing.T) {
		list, err := f.repo.List(agentruns.ListFilter{OrgID: f.orgID})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, run := range list {
			if run.OrgID != f.orgID {
				t.Fatalf("listing for %s leaked a run from %s", f.orgID, run.OrgID)
			}
		}
		if !containsRun(list, mine) {
			t.Errorf("own-org run %s missing from listing", mine)
		}
		if containsRun(list, otherRun) {
			t.Errorf("other-org run %s leaked into listing", otherRun)
		}
	})

	t.Run("empty org returns nothing", func(t *testing.T) {
		list, err := f.repo.List(agentruns.ListFilter{OrgID: ""})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("empty-org listing returned %d runs, want 0 (fail closed)", len(list))
		}
	})
}

func containsRun(list []*agentruns.Run, id string) bool {
	for _, r := range list {
		if r.ID == id {
			return true
		}
	}
	return false
}
