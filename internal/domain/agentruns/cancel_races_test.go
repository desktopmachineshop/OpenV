package agentruns

import (
	"reflect"
	"testing"
	"time"
)

// A cancel lands whatever a race does to the run around it (#379 bugs
// 165 to 167): a release, a claim, a worker's finish or the reaper acting
// between a read of the run and a write to it never loses a cancel someone
// requested.

// A cancel is written as the run's status asks when the write lands, not as
// the service read it: a run its worker handed back meanwhile is back in
// the queue, and ends cancelled as a queued run does; a run a worker
// claimed meanwhile has its cancel requested; a run that finished meanwhile
// stays as it finished. Before the fix the cancel of a run read as claimed
// matched only a claimed or running run, so after a release it matched
// nothing, the run answered queued, and the next claim started it again
// (#379 bug 165).
func TestRequestCancelLandsWhateverTheRunBecameMeanwhile(t *testing.T) {
	for _, c := range []struct {
		name      string
		read      string
		race      func(r *Run)
		want      string
		requested bool
		announced []string
	}{
		{
			name: "a claimed run its worker released",
			read: StatusClaimed,
			race: func(r *Run) {
				r.Status, r.WorkerID, r.HeartbeatAt, r.RunTokenHash = StatusQueued, "", nil, ""
			},
			want: StatusCancelled, requested: true, announced: []string{"r1 cancelled"},
		},
		{
			name: "a running run its worker released",
			read: StatusRunning,
			race: func(r *Run) {
				r.Status, r.WorkerID, r.HeartbeatAt, r.RunTokenHash = StatusQueued, "", nil, ""
			},
			want: StatusCancelled, requested: true, announced: []string{"r1 cancelled"},
		},
		{
			name: "a queued run a worker claimed",
			read: StatusQueued,
			race: func(r *Run) {
				now := time.Now()
				r.Status, r.WorkerID, r.HeartbeatAt = StatusClaimed, "w-2", &now
			},
			want: StatusClaimed, requested: true, announced: []string{"r1 claimed"},
		},
		{
			name: "a running run that finished",
			read: StatusRunning,
			race: func(r *Run) {
				now := time.Now()
				r.Status, r.FinishedAt, r.RunTokenHash = StatusSucceeded, &now, ""
			},
			want: StatusSucceeded, requested: false, announced: nil,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			now := time.Now()
			bus := &fakeBus{}
			svc, repo := newFakeServiceWithBus(bus, &Run{ID: "r1", OrgID: "org-1", Status: c.read, WorkerID: "w-1", HeartbeatAt: &now, RunTokenHash: "hash"})
			if c.read == StatusQueued {
				repo.runs["r1"].WorkerID, repo.runs["r1"].HeartbeatAt = "", nil
			}
			rec := &statusRecorder{}
			svc.AddSubscriber(rec)
			repo.onRequestCancel = func() { c.race(repo.runs["r1"]) }

			run, err := svc.RequestCancel("r1")
			if err != nil {
				t.Fatalf("RequestCancel: %v", err)
			}
			stored := repo.runs["r1"]
			if stored.Status != c.want || stored.CancelRequested != c.requested {
				t.Errorf("stored run: %s, cancel requested %v; want %s, %v", stored.Status, stored.CancelRequested, c.want, c.requested)
			}
			if run.Status != stored.Status || run.CancelRequested != stored.CancelRequested {
				t.Errorf("RequestCancel answered %s, cancel requested %v; want the run as stored", run.Status, run.CancelRequested)
			}
			if c.want == StatusCancelled && (stored.FinishedAt == nil || stored.RunTokenHash != "") {
				t.Errorf("the cancelled run: finished at %v, token %q; want finished, token revoked", stored.FinishedAt, stored.RunTokenHash)
			}
			if !reflect.DeepEqual(rec.announced, c.announced) {
				t.Errorf("announced %q, want %q", rec.announced, c.announced)
			}
			// A cancel publishes no RunFinished: a cancelled queued run never
			// had a worker to finish it, and a live one finishes when its
			// worker reports it cancelled.
			if finished := bus.finished(); len(finished) != 0 {
				t.Errorf("RequestCancel published %+v, want no RunFinished", finished)
			}
		})
	}
}

// A cancel requested while the run's worker reports it finished is kept:
// the run ends as its worker reported it, its cancel still requested, and
// a retryable failure is not retried, as no run asked to stop is (#379 bug
// 147). Before the fix the finish wrote back the flag it had read, false,
// over the cancel, and the auto-retry launched the run again (#379 bug
// 166).
func TestFinishKeepsACancelRequestedAsTheRunFinished(t *testing.T) {
	svc, repo := newRetryService(runningRun("r1", 1, 3))
	repo.onUpdateTerminal = func() { repo.runs["r1"].CancelRequested = true }

	run, err := svc.Finish("r1", FinishRequest{Status: StatusFailed, ErrorClass: ErrorClassWorkerError, Error: "killed"})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if stored := repo.runs["r1"]; stored.Status != StatusFailed || !stored.CancelRequested {
		t.Errorf("stored run: %s, cancel requested %v; want failed, still requested", stored.Status, stored.CancelRequested)
	}
	if !run.CancelRequested {
		t.Error("Finish answered the run with its cancel not requested, want requested")
	}
	if r := findRetryOf(repo, "r1"); r != nil {
		t.Errorf("a run asked to stop as it finished was retried: %+v", r)
	}
}
