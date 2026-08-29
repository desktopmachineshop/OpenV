package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// TestFailureTaxonomyRoundTrip persists a run carrying the failure-taxonomy
// columns (error_class, attempt_count, max_attempts, next_attempt_at) and reads
// them back through FindByID, exercising the migration + Save + scanRun path.
func TestFailureTaxonomyRoundTrip(t *testing.T) {
	f := newClaimFixture(t)
	next := time.Now().Add(90 * time.Second).UTC().Truncate(time.Millisecond)
	run := &agentruns.Run{
		ID:               uuid.New().String(),
		OrgID:            f.orgID,
		AgentID:          f.agentID,
		Status:           agentruns.StatusFailed,
		Prompt:           "do work",
		Error:            "provider overloaded",
		ErrorClass:       agentruns.ErrorClassProviderUnavailable,
		AttemptCount:     2,
		MaxAttempts:      3,
		NextAttemptAt:    &next,
		ArtifactsTouched: []map[string]interface{}{},
		CreatedAt:        time.Now(),
	}
	if err := f.repo.Save(run); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := f.mustFind(t, run.ID)
	if got.ErrorClass != agentruns.ErrorClassProviderUnavailable {
		t.Errorf("error_class = %q, want provider_unavailable", got.ErrorClass)
	}
	if got.AttemptCount != 2 || got.MaxAttempts != 3 {
		t.Errorf("attempt tracking = %d/%d, want 2/3", got.AttemptCount, got.MaxAttempts)
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.UTC().Truncate(time.Millisecond).Equal(next) {
		t.Errorf("next_attempt_at = %v, want %v", got.NextAttemptAt, next)
	}

	// UpdateTerminal must also persist error_class onto a live run.
	run2 := f.queueRun(t, runSpec{})
	f.setRunState(t, run2, agentruns.StatusRunning, "live")
	term := f.mustFind(t, run2)
	term.Status = agentruns.StatusFailed
	term.ErrorClass = agentruns.ErrorClassAgentError
	applied, err := f.repo.UpdateTerminal(term)
	if err != nil || !applied {
		t.Fatalf("UpdateTerminal = %v, %v", applied, err)
	}
	if got := f.mustFind(t, run2); got.ErrorClass != agentruns.ErrorClassAgentError {
		t.Errorf("UpdateTerminal error_class = %q, want agent_error", got.ErrorClass)
	}
}

// TestFailStaleClassifiesWorkerError: the reaper's bulk failure stamps
// error_class = worker_error so those runs are recognised as retryable.
func TestFailStaleClassifiesWorkerError(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	f.setRunState(t, id, agentruns.StatusRunning, "live")
	if _, err := f.db.Exec(`UPDATE agent_runs SET heartbeat_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	ids, err := f.repo.FailStale(time.Now().Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("FailStale: %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("FailStale returned %v, want [%s]", ids, id)
	}
	if got := f.mustFind(t, id); got.ErrorClass != agentruns.ErrorClassWorkerError {
		t.Errorf("reaper-failed run error_class = %q, want worker_error", got.ErrorClass)
	}
}

// TestClaimSkipsBackedOffRetry: a queued run whose next_attempt_at is still in
// the future is not claimable; once it elapses, it is.
func TestClaimSkipsBackedOffRetry(t *testing.T) {
	f := newClaimFixture(t)
	id := f.queueRun(t, runSpec{})
	if _, err := f.db.Exec(`UPDATE agent_runs SET next_attempt_at = NOW() + INTERVAL '1 hour' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	claim, err := f.repo.Claim("w-1", f.orgID, "", claudeOnly, agentruns.PriorityNormal, false)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claimed a backed-off run %s early", claim.ID)
	}
	// Move the backoff into the past: now claimable.
	if _, err := f.db.Exec(`UPDATE agent_runs SET next_attempt_at = NOW() - INTERVAL '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	claim, err = f.repo.Claim("w-1", f.orgID, "", claudeOnly, agentruns.PriorityNormal, false)
	if err != nil {
		t.Fatalf("Claim after backoff: %v", err)
	}
	if claim == nil || claim.ID != id {
		t.Fatalf("expected to claim %s after backoff elapsed, got %v", id, claim)
	}
}
