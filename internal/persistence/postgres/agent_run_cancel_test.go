package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// A cancel stays a cancel (#379 bugs 148 and 151): a run whose cancel was
// requested is never handed to a worker again, by a release, a claim or a
// fresh token. Postgres-gated (OPENV_TEST_DATABASE_URL).

// A run whose cancel was requested, handed back by the worker holding it,
// ends cancelled, as its worker reporting it cancelled would have ended it:
// its token and its half-written answer go, and its worker stays on it, as
// on any finished run. No claim takes it after. Before the fix the release
// put it back in the queue, its cancel still requested, for the next claim
// to start again (#379 bug 148).
func TestReleaseClaimEndsACancelRequestedRunCancelled(t *testing.T) {
	f := newClaimFixture(t)
	for _, status := range []string{agentruns.StatusClaimed, agentruns.StatusRunning} {
		t.Run(status, func(t *testing.T) {
			id := f.queueRun(t, runSpec{})
			f.setRunState(t, id, status, "held-"+status)
			f.exec(t, `UPDATE agent_runs SET worker_id = 'w-1', claimed_by = $2, heartbeat_at = NOW() WHERE id = $1`, id, f.userA)
			if applied, err := f.repo.UpdatePartialText(id, "Half an answ"); err != nil || !applied {
				t.Fatalf("seed partial text = %v, %v", applied, err)
			}
			if applied, err := f.repo.RequestCancel(id); err != nil || !applied {
				t.Fatalf("RequestCancel = %v, %v", applied, err)
			}

			ok, err := f.repo.ReleaseClaim(id, "w-1")
			if err != nil || !ok {
				t.Fatalf("ReleaseClaim = %v, %v, want applied", ok, err)
			}
			run := f.mustFind(t, id)
			if run.Status != agentruns.StatusCancelled || !run.CancelRequested || run.FinishedAt == nil {
				t.Errorf("released run: %s, cancel requested %v, finished at %v; want cancelled, requested, finished",
					run.Status, run.CancelRequested, run.FinishedAt)
			}
			if run.WorkerID != "w-1" || run.ClaimedBy == nil || *run.ClaimedBy != f.userA {
				t.Errorf("released run's worker %q, claimed by %v; want w-1 and its runner's user kept", run.WorkerID, run.ClaimedBy)
			}
			if run.PartialText != "" || f.tokenHash(t, id) != "" {
				t.Errorf("released run kept partial text %q, token %q; want both gone", run.PartialText, f.tokenHash(t, id))
			}
			if next, err := f.repo.Claim("w-2", f.orgID, "", claudeOnly, 0, false); err != nil || next != nil {
				t.Errorf("a claim after the release took %v (%v), want nothing", next, err)
			}
		})
	}
}

// A queued run whose cancel was requested is never claimed: the claim takes
// the next run instead, and leaves it queued. The old ReleaseClaim left such
// runs in the queue (#379 bug 148), and the next claim started a run its
// launcher had asked to stop.
func TestClaimNeverTakesACancelRequestedRun(t *testing.T) {
	f := newClaimFixture(t)
	stopped := f.queueRun(t, runSpec{age: time.Hour, launchedBy: &f.userA})
	f.exec(t, `UPDATE agent_runs SET cancel_requested = TRUE WHERE id = $1`, stopped)
	wanted := f.queueRun(t, runSpec{launchedBy: &f.userA})

	for _, worker := range []struct{ name, user string }{{"a workspace key", ""}, {"the launcher's personal key", f.userA}} {
		got, err := f.repo.Claim("w-1", f.orgID, worker.user, claudeOnly, 0, false)
		if err != nil {
			t.Fatalf("%s: Claim: %v", worker.name, err)
		}
		if worker.user == "" {
			if got == nil || got.ID != wanted {
				t.Fatalf("%s claimed %v, want the run whose cancel was not requested, %s", worker.name, got, wanted)
			}
			continue
		}
		if got != nil {
			t.Errorf("%s claimed %s, want nothing: the only queued run left has its cancel requested", worker.name, got.ID)
		}
	}
	if got := f.status(t, stopped); got != agentruns.StatusQueued {
		t.Errorf("the run whose cancel was requested is %s, want it left queued", got)
	}
}

// UpdateTokenHash gives a token only to a run a worker holds whose cancel
// has not been requested. A cancel requested, a finish, a release and a
// delete's revocation each keep the run's token revoked; the claim
// handshake's reissue used to write over all of them (#379 bug 151).
func TestUpdateTokenHashKeepsARevokedRunRevoked(t *testing.T) {
	f := newClaimFixture(t)
	for _, c := range []struct {
		name, status string
		cancel, want bool
	}{
		{"a claimed run", agentruns.StatusClaimed, false, true},
		{"a running run", agentruns.StatusRunning, false, true},
		{"a claimed run asked to stop", agentruns.StatusClaimed, true, false},
		{"a running run asked to stop", agentruns.StatusRunning, true, false},
		{"a queued run", agentruns.StatusQueued, false, false},
		{"a cancelled run", agentruns.StatusCancelled, true, false},
		{"a failed run", agentruns.StatusFailed, false, false},
		{"a run awaiting approval", agentruns.StatusAwaitingApproval, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			id := f.queueRun(t, runSpec{})
			f.setRunState(t, id, c.status, "")
			f.exec(t, `UPDATE agent_runs SET cancel_requested = $2 WHERE id = $1`, id, c.cancel)
			applied, err := f.repo.UpdateTokenHash(id, "fresh-"+id)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if c.want {
				want = "fresh-" + id
			}
			if applied != c.want || f.tokenHash(t, id) != want {
				t.Errorf("UpdateTokenHash applied %v, token %q; want %v, %q", applied, f.tokenHash(t, id), c.want, want)
			}
		})
	}
}

// The race of #379 bug 151: a claim commits just before the run's project is
// deleted, and the claim's handshake then asks for the run's token. The
// delete revokes the token of every claimed run of the project and asks it
// to stop; a reissue after the delete, or one that waited on the run's row
// while the delete held it, issues no token, and the run stays revoked.
// Before the fix the reissue wrote a fresh hash over the revocation and the
// new token signed in.
func TestReissuingATokenAfterAProjectDeleteRevokedItIssuesNone(t *testing.T) {
	for _, when := range []string{"after the delete", "while the delete holds the run"} {
		t.Run(when, func(t *testing.T) {
			db := rtDB(t)
			w := pdSeedWorkspace(t, db)
			runs := NewAgentRunRepository(db)
			svc := agentruns.NewDefaultService(runs, nil, nil)
			projectID, runID := uuid.New().String(), uuid.New().String()
			rtSeed(t, db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Doomed')`, projectID, w.org)
			rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, project_id, prompt, run_token_hash) VALUES ($1, $2, $3, $4, 'Work', 'hash')`,
				runID, w.agent, w.org, projectID)
			claimed, err := runs.Claim("worker-1", w.org, "", []string{"claude"}, 0, false)
			if err != nil || claimed == nil || claimed.ID != runID {
				t.Fatalf("Claim = %v, %v, want the run", claimed, err)
			}

			var token string
			if when == "after the delete" {
				if _, err := NewProjectRepository(db).Delete(projectID); err != nil {
					t.Fatalf("Delete: %v", err)
				}
				token, err = svc.ReissueToken(runID)
			} else {
				token, err = arcReissueBehindTheDelete(t, db, svc, projectID, runID)
			}
			if !errors.Is(err, agentruns.ErrInvalidTransition) || token != "" {
				t.Errorf("ReissueToken = %q, %v; want no token, and ErrInvalidTransition", token, err)
			}
			var hash string
			var cancelRequested bool
			if err := db.QueryRow(`SELECT run_token_hash, cancel_requested FROM agent_runs WHERE id = $1`, runID).Scan(&hash, &cancelRequested); err != nil {
				t.Fatal(err)
			}
			if hash != "" || !cancelRequested {
				t.Errorf("the run after the reissue: token hash %q, cancel requested %v; want revoked and asked to stop", hash, cancelRequested)
			}
			if token != "" {
				if found, err := runs.FindByTokenHash(users.HashToken(token)); err != nil || found != nil {
					t.Errorf("the reissued token signs in as %v (%v), want refused", found, err)
				}
			}
		})
	}
}

// arcReissueBehindTheDelete revokes the project's runs in a transaction of
// its own, as the delete's first statements do (cancelProjectRuns), starts
// the reissue, waits until the reissue waits on the run's row, commits the
// revocation, and answers what the reissue answered.
func arcReissueBehindTheDelete(t *testing.T, db *sql.DB, svc *agentruns.DefaultService, projectID, runID string) (string, error) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := cancelProjectRuns(tx, projectID); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		token string
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		token, err := svc.ReissueToken(runID)
		done <- answer{token, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		select {
		case a := <-done:
			t.Fatalf("the reissue did not wait for the revocation: %q, %v", a.token, a.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the reissue never waited for the revocation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	a := <-done
	return a.token, a.err
}

// Migration 0058 on a database holding what the old ReleaseClaim left: runs
// back in the queue with their cancel requested (#379 bug 148), which a
// claim now never takes. It ends each cancelled, finished, its token
// revoked, as a cancel ends a queued run, and leaves every other run as it
// was: a queued run nobody asked to stop, a claimed or running run asked to
// stop (its worker reports it cancelled), and finished runs, a cancelled
// one included. Running it again changes nothing.
func TestMigration58CancelsTheRunsAReleaseRequeuedAfterTheirCancel(t *testing.T) {
	db := testDB(t)
	var before []Migration
	var m58 func(*sql.Tx) error
	for _, m := range migrations {
		if m.Version < 58 {
			before = append(before, m)
		}
		if m.Version == 58 {
			m58 = m.Run
		}
	}
	if m58 == nil {
		t.Fatal("no migration 0058 in the registry")
	}
	if _, err := db.Exec(createLedgerSQL); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db, before); err != nil {
		t.Fatalf("migrate to 0057: %v", err)
	}

	w := pdSeedWorkspace(t, db)
	type run struct {
		status, token  string
		cancel, ending bool
	}
	seeded := map[string]run{
		"requeued after its cancel": {"queued", "hash-requeued", true, true},
		"queued":                    {"queued", "hash-queued", false, false},
		"claimed and asked to stop": {"claimed", "", true, false},
		"running and asked to stop": {"running", "", true, false},
		"cancelled":                 {"cancelled", "", true, false},
		"succeeded":                 {"succeeded", "", false, false},
		"awaiting approval":         {"awaiting_approval", "", false, false},
		"failed after its cancel":   {"failed", "", true, false},
		"another requeued after it": {"queued", "", true, true},
	}
	ids := map[string]string{}
	for name, r := range seeded {
		ids[name] = uuid.New().String()
		rtSeed(t, db, `INSERT INTO agent_runs (id, agent_id, org_id, prompt, status, run_token_hash, cancel_requested, partial_text)
			VALUES ($1, $2, $3, 'Work', $4, $5, $6, '')`, ids[name], w.agent, w.org, r.status, r.token, r.cancel)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	read := func() map[string]string {
		t.Helper()
		got := map[string]string{}
		for name, id := range ids {
			var status, token string
			var cancel bool
			var finished sql.NullTime
			if err := db.QueryRow(`SELECT status, run_token_hash, cancel_requested, finished_at FROM agent_runs WHERE id = $1`, id).
				Scan(&status, &token, &cancel, &finished); err != nil {
				t.Fatal(err)
			}
			got[name] = fmt.Sprintf("%s token=%q cancel=%v finished=%v", status, token, cancel, finished.Valid)
			if finished.Valid {
				got[name] += " at " + finished.Time.String()
			}
		}
		return got
	}
	after := read()
	for name, r := range seeded {
		want := fmt.Sprintf("%s token=%q cancel=%v finished=false", r.status, r.token, r.cancel)
		if r.ending {
			want = `cancelled token="" cancel=true finished=true`
		}
		if got := after[name]; !strings.HasPrefix(got, want) {
			t.Errorf("the %s run after the migration: %s, want %s", name, got, want)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := m58(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("migration 0058 a second time: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if again := read(); !reflect.DeepEqual(again, after) {
		t.Errorf("migration 0058 a second time changed the runs:\n%v\nwant\n%v", again, after)
	}
}
