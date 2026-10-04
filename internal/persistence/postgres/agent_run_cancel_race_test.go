package postgres

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// A cancel lands whatever a race does to the run around it (#379 bugs 165
// to 167): a release, a claim, a worker's finish or the reaper writing the
// run while a cancel is under way never loses the cancel. Each race is made
// deterministic with row locks: a transaction of the test's own holds the
// run's row, the writers under test queue behind it in the order they
// start, and the test lets them through once they all wait.
// Postgres-gated (OPENV_TEST_DATABASE_URL).

// acrHoldRun locks the run's row in a transaction of its own, as a writer
// holding it would, and answers the transaction for the test to write in
// and commit.
func acrHoldRun(t *testing.T, db *sql.DB, runID string) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.Exec(`SELECT id FROM agent_runs WHERE id = $1 FOR UPDATE`, runID); err != nil {
		t.Fatal(err)
	}
	return tx
}

// acrAwaitLockWaiters waits until n sessions on the test's database wait for
// a lock: the writers started behind acrHoldRun's lock, each queued behind
// the one started before it.
func acrAwaitLockWaiters(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d sessions wait for the run's row, want %d", waiting, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// acrAnswer is what a service call started behind a held row answered.
type acrAnswer struct {
	run *agentruns.Run
	err error
}

// acrClaim queues a run and claims it for worker w-1, running when status
// says so, holding a token.
func acrClaim(t *testing.T, f *claimFixture, status string) string {
	t.Helper()
	id := f.queueRun(t, runSpec{})
	if claimed, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, 0, false); err != nil || claimed == nil || claimed.ID != id {
		t.Fatalf("Claim = %v, %v, want the run", claimed, err)
	}
	if status == agentruns.StatusRunning {
		if applied, err := f.repo.MarkRunning(id, time.Now().UTC()); err != nil || !applied {
			t.Fatalf("MarkRunning = %v, %v", applied, err)
		}
	}
	f.exec(t, `UPDATE agent_runs SET run_token_hash = 'hash' WHERE id = $1`, id)
	return id
}

// The race of #379 bug 165: a cancel reads a run as claimed or running
// while its worker hands it back. The release holds the run's row, and the
// cancel's write, waiting behind it, lands on the run the release left
// queued: the run ends cancelled, finished, its token revoked, as a cancel
// ends a queued run, and no claim takes it after. Before the fix the cancel
// wrote only to a claimed or running run, matched nothing, and the run
// answered queued, its cancel lost, for the next claim to start again.
func TestACancelRacingAReleaseEndsTheRunCancelled(t *testing.T) {
	for _, status := range []string{agentruns.StatusClaimed, agentruns.StatusRunning} {
		t.Run(status, func(t *testing.T) {
			f := newClaimFixture(t)
			svc := agentruns.NewDefaultService(f.repo, nil, nil)
			id := acrClaim(t, f, status)

			hold := acrHoldRun(t, f.db, id)
			released := make(chan error, 1)
			go func() {
				ok, err := f.repo.ReleaseClaim(id, "w-1")
				if err == nil && !ok {
					err = errors.New("nothing released")
				}
				released <- err
			}()
			acrAwaitLockWaiters(t, f.db, 1)
			cancelled := make(chan acrAnswer, 1)
			go func() {
				run, err := svc.RequestCancel(id)
				cancelled <- acrAnswer{run, err}
			}()
			acrAwaitLockWaiters(t, f.db, 2)
			if err := hold.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-released; err != nil {
				t.Fatalf("ReleaseClaim: %v", err)
			}
			a := <-cancelled
			if a.err != nil {
				t.Fatalf("RequestCancel: %v", a.err)
			}

			run := f.mustFind(t, id)
			if run.Status != agentruns.StatusCancelled || !run.CancelRequested || run.FinishedAt == nil || f.tokenHash(t, id) != "" {
				t.Errorf("the run after the release and the cancel: %s, cancel requested %v, finished at %v, token %q; want cancelled, requested, finished, revoked",
					run.Status, run.CancelRequested, run.FinishedAt, f.tokenHash(t, id))
			}
			if a.run.Status != agentruns.StatusCancelled {
				t.Errorf("RequestCancel answered %s, want cancelled", a.run.Status)
			}
			if next, err := f.repo.Claim("w-2", f.orgID, "", claudeOnly, 0, false); err != nil || next != nil {
				t.Errorf("a claim after the cancel took %v (%v), want nothing", next, err)
			}
		})
	}
}

// A cancel that reads a run as queued while a worker claims it lands on the
// claimed run as a request to stop: the claim keeps the run, its worker and
// its token, and the worker reads the cancel on its next report. The
// claim's write is made in the transaction holding the run's row (Claim
// itself skips a row another transaction holds), and the cancel waits for
// it.
func TestACancelRacingAClaimAsksTheClaimedRunToStop(t *testing.T) {
	f := newClaimFixture(t)
	svc := agentruns.NewDefaultService(f.repo, nil, nil)
	id := f.queueRun(t, runSpec{})

	hold := acrHoldRun(t, f.db, id)
	cancelled := make(chan acrAnswer, 1)
	go func() {
		run, err := svc.RequestCancel(id)
		cancelled <- acrAnswer{run, err}
	}()
	acrAwaitLockWaiters(t, f.db, 1)
	if _, err := hold.Exec(`UPDATE agent_runs SET status = 'claimed', worker_id = 'w-1', heartbeat_at = NOW(), run_token_hash = 'hash' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	a := <-cancelled
	if a.err != nil {
		t.Fatalf("RequestCancel: %v", a.err)
	}

	run := f.mustFind(t, id)
	if run.Status != agentruns.StatusClaimed || run.WorkerID != "w-1" || !run.CancelRequested || run.FinishedAt != nil || f.tokenHash(t, id) != "hash" {
		t.Errorf("the run after the claim and the cancel: %s by %q, cancel requested %v, finished at %v, token %q; want claimed by w-1, requested, unfinished, its token kept",
			run.Status, run.WorkerID, run.CancelRequested, run.FinishedAt, f.tokenHash(t, id))
	}
	if a.run.Status != agentruns.StatusClaimed || !a.run.CancelRequested {
		t.Errorf("RequestCancel answered %s, cancel requested %v; want claimed, requested", a.run.Status, a.run.CancelRequested)
	}
}
