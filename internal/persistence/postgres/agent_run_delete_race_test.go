package postgres

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// The race of #379 bug 169, a sibling of bug 165: a project's delete cancels
// its queued runs and asks its claimed and running ones to stop in separate
// writes, each matching on the run's status. A worker handing a claimed run
// back between them moved it to the queue after the queued write had run,
// and the live write then found it queued: the run escaped the delete,
// queued with no project, for the next claim to start. The release holds
// the run's row here and the delete queues behind it, so the release
// commits first. Postgres-gated (OPENV_TEST_DATABASE_URL).
func TestADeleteRacingAReleaseCancelsTheRun(t *testing.T) {
	f := newClaimFixture(t)
	projectID := uuid.New().String()
	f.exec(t, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Doomed')`, projectID, f.orgID)
	id := f.queueRun(t, runSpec{projectID: &projectID})
	if claimed, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, 0, false); err != nil || claimed == nil || claimed.ID != id {
		t.Fatalf("Claim = %v, %v, want the run", claimed, err)
	}
	f.exec(t, `UPDATE agent_runs SET run_token_hash = 'hash' WHERE id = $1`, id)

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
	type deleteAnswer struct {
		removed *projects.Removed
		err     error
	}
	deleted := make(chan deleteAnswer, 1)
	go func() {
		removed, err := NewProjectRepository(f.db).Delete(projectID)
		deleted <- deleteAnswer{removed, err}
	}()
	acrAwaitLockWaiters(t, f.db, 2)
	if err := hold.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-released; err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	d := <-deleted
	if d.err != nil {
		t.Fatalf("Delete: %v", d.err)
	}

	run := f.mustFind(t, id)
	if run.Status != agentruns.StatusCancelled || !run.CancelRequested || run.FinishedAt == nil || f.tokenHash(t, id) != "" {
		t.Errorf("the run after the release and the delete: %s, cancel requested %v, finished at %v, token %q; want cancelled, requested, finished, revoked",
			run.Status, run.CancelRequested, run.FinishedAt, f.tokenHash(t, id))
	}
	found := false
	for _, c := range d.removed.CancelledRuns {
		found = found || c == id
	}
	if !found {
		t.Errorf("Delete answered the cancelled runs %v, want the run the release moved to the queue", d.removed.CancelledRuns)
	}
	if next, err := f.repo.Claim("w-2", f.orgID, "", claudeOnly, 0, false); err != nil || next != nil {
		t.Errorf("a claim after the delete took %v (%v), want nothing", next, err)
	}
}
