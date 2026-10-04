package agentruns

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// A cancel stays a cancel (#379 bugs 147, 148 and 151): a run asked to stop
// is never started again, by a release, a fresh token or an auto-retry.

// A run asked to stop that its worker hands back ends cancelled, as its
// worker reporting it cancelled would have ended it: the subscribers hear
// it cancelled and RunFinished goes out, as for that report. A release of
// any other run puts it back in the queue and publishes nothing, as before
// (#379 bug 148).
func TestReleaseClaimOfARunAskedToStopEndsItCancelled(t *testing.T) {
	now := time.Now()
	bus := &fakeBus{}
	svc, repo := newFakeServiceWithBus(bus,
		&Run{ID: "stopping", OrgID: "org-1", Status: StatusClaimed, WorkerID: "w-1", HeartbeatAt: &now, RunTokenHash: "hash", CancelRequested: true},
		&Run{ID: "going-on", OrgID: "org-1", Status: StatusClaimed, WorkerID: "w-1", HeartbeatAt: &now, RunTokenHash: "hash"},
	)
	rec := &statusRecorder{}
	svc.AddSubscriber(rec)

	for _, id := range []string{"stopping", "going-on"} {
		if err := svc.ReleaseClaim(id, "w-1"); err != nil {
			t.Fatalf("ReleaseClaim(%s): %v", id, err)
		}
	}
	if got := repo.runs["stopping"]; got.Status != StatusCancelled || got.FinishedAt == nil {
		t.Errorf("the run asked to stop after its release: %+v, want cancelled", got)
	}
	if want := []string{"stopping cancelled", "going-on queued"}; !reflect.DeepEqual(rec.announced, want) {
		t.Errorf("announced %q, want %q", rec.announced, want)
	}
	finished := bus.finished()
	if len(finished) != 1 || finished[0].EntityID != "stopping" || finished[0].Payload["status"] != StatusCancelled {
		t.Errorf("RunFinished published %+v, want one, for the cancelled run", finished)
	}
}

// No run token is issued to a run asked to stop, or to one no worker holds:
// ReissueToken answers ErrInvalidTransition and the run keeps no token
// (#379 bug 151: the claim handshake's reissue wrote a fresh token over the
// revocation of a project's delete).
func TestReissueTokenIssuesNoneToARunAskedToStop(t *testing.T) {
	svc, repo := newFakeService(
		&Run{ID: "stopping", Status: StatusClaimed, WorkerID: "w-1", CancelRequested: true},
		&Run{ID: "done", Status: StatusCancelled, CancelRequested: true},
		&Run{ID: "held", Status: StatusClaimed, WorkerID: "w-1"},
	)
	for _, id := range []string{"stopping", "done"} {
		token, err := svc.ReissueToken(id)
		if !errors.Is(err, ErrInvalidTransition) || token != "" {
			t.Errorf("ReissueToken(%s) = %q, %v; want no token and ErrInvalidTransition", id, token, err)
		}
		if got := repo.runs[id].RunTokenHash; got != "" {
			t.Errorf("the %s run's token hash is %q, want none", id, got)
		}
	}
	token, err := svc.ReissueToken("held")
	if err != nil || token == "" || repo.runs["held"].RunTokenHash == "" {
		t.Errorf("ReissueToken(held) = %q, %v (stored %q); want a token", token, err, repo.runs["held"].RunTokenHash)
	}
}

// A run asked to stop is never retried automatically, however it ends: its
// worker reporting a retryable failure, or the reaper failing it when its
// worker went silent. A project's delete asks its live runs to stop; a
// retry of one launched it again with no project (#379 bug 147).
func TestAutoRetryNeverRelaunchesARunAskedToStop(t *testing.T) {
	t.Run("the worker reports a retryable failure", func(t *testing.T) {
		run := runningRun("r1", 1, 3)
		run.CancelRequested = true
		svc, repo := newRetryService(run)
		if _, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassWorkerError, Error: "killed"}); err != nil {
			t.Fatalf("Finish: %v", err)
		}
		if r := findRetryOf(repo, "r1"); r != nil {
			t.Errorf("a run asked to stop was retried: %+v", r)
		}
	})
	t.Run("the reaper fails it", func(t *testing.T) {
		run := runningRun("r1", 1, 3)
		run.CancelRequested = true
		old := time.Now().Add(-time.Hour)
		run.HeartbeatAt = &old
		svc, repo := newRetryService(run)
		if _, err := svc.FailStale(2 * time.Minute); err != nil {
			t.Fatalf("FailStale: %v", err)
		}
		if repo.runs["r1"].Status != StatusFailed {
			t.Fatalf("the reaper left the run %s, want failed", repo.runs["r1"].Status)
		}
		if r := findRetryOf(repo, "r1"); r != nil {
			t.Errorf("a run asked to stop was retried after the reaper failed it: %+v", r)
		}
	})
}
