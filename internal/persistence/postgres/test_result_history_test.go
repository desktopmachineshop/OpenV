package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// recordAgain adds a result for the fixture's case i, as the service would
// for a re-record: a fresh id and the time it was recorded.
func (f *evidenceFixture) recordAgain(t *testing.T, i int, status string, at time.Time) *vv.TestResult {
	t.Helper()
	r := &vv.TestResult{
		ID: uuid.New().String(), RunID: f.runID, TestCaseID: f.caseIDs[i], TestCaseVersion: 1,
		Status: status, Notes: "again", ExecutedAt: &at, CreatedAt: at, UpdatedAt: at,
	}
	if err := f.vvRepo.AddResult(r); err != nil {
		t.Fatalf("record case %d again: %v", i, err)
	}
	return r
}

// Recording a case again adds a result beside the first, which stays as it
// was recorded: the run's list, its lookup by case and the project's latest
// results answer the newest, and the run's history holds both (REQ-13). The
// upsert on (run_id, test_case_id) wrote over the first.
func TestReRecordingAddsAResultAndKeepsTheFirst(t *testing.T) {
	f := newEvidenceFixture(t)
	later := time.Now().UTC().Add(time.Minute)
	again := f.recordAgain(t, 0, vv.ResultFail, later)

	current, err := f.vvRepo.ListResultsByRun(f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 3 || current[0].ID != again.ID || current[0].Status != vv.ResultFail {
		t.Fatalf("the run lists %d results, the first %+v; want its three cases, the re-recorded one first and current",
			len(current), current[0])
	}
	for _, r := range current[1:] {
		if r.ID == f.resultIDs[0] {
			t.Fatalf("the superseded result %s is still listed as current", r.ID)
		}
	}

	found, err := f.vvRepo.FindResultByCase(f.runID, f.caseIDs[0])
	if err != nil || found == nil || found.ID != again.ID {
		t.Fatalf("the case's result is %+v (%v), want the newest, %s", found, err, again.ID)
	}
	latest, err := f.vvRepo.LatestResultPerCase(f.projectID)
	if err != nil || latest[f.caseIDs[0]] == nil || latest[f.caseIDs[0]].ID != again.ID {
		t.Fatalf("the project's latest result for the case is %+v (%v), want %s", latest[f.caseIDs[0]], err, again.ID)
	}

	history, err := f.vvRepo.ListResultHistoryByRun(f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].ID != again.ID {
		t.Fatalf("the run's history holds %d results, want all four, newest (%s) first", len(history), again.ID)
	}
	var first *vv.TestResult
	for _, r := range history {
		if r.ID == f.resultIDs[0] {
			first = r
		}
	}
	if first == nil || first.Status != vv.ResultPass || first.Notes != "measured on the rig" {
		t.Fatalf("the first result is %+v in the history, want it as recorded (pass, its notes)", first)
	}
}

// Two re-records of a case can reach the store in the opposite order to the
// times the service stamped them with: one waited for a connection after it
// was stamped, or an instance whose clock runs behind stamped it. The result
// the store records last is still the case's current one, stamped after the
// result it supersedes, so the citations it takes over sit on the result the
// run shows (REQ-13, REQ-121). The store kept the service's stamp, so the case
// answered the result stamped later while its citation sat on the one
// recorded later, and a stamp equal to the current one left the choice to the
// ids.
func TestAReRecordStampedEarlierIsStillTheCurrentResult(t *testing.T) {
	f := newEvidenceFixture(t)
	svc := evidence.NewDefaultService(f.repo)
	bundle, err := svc.Create(f.projectID, evidence.CreateRequest{Title: "Rig capture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cited, err := svc.Cite(f.resultIDs[0], bundle.ID, "channel 2")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC()
	stampedLater := f.recordAgain(t, 0, vv.ResultFail, base.Add(2*time.Second))
	stampedEarlier := f.recordAgain(t, 0, vv.ResultPass, base.Add(time.Second))
	sameStamp := f.recordAgain(t, 0, vv.ResultBlocked, stampedEarlier.UpdatedAt)

	previous := stampedLater
	for _, r := range []*vv.TestResult{stampedEarlier, sameStamp} {
		if !r.UpdatedAt.After(previous.UpdatedAt) || !r.CreatedAt.Equal(r.UpdatedAt) ||
			r.ExecutedAt == nil || !r.ExecutedAt.Equal(r.UpdatedAt) {
			t.Fatalf("a result recorded after %s (updated %s) answered created %s, updated %s, executed %v; "+
				"want all three one stamp, after the result it supersedes", previous.ID, previous.UpdatedAt,
				r.CreatedAt, r.UpdatedAt, r.ExecutedAt)
		}
		previous = r
	}

	found, err := f.vvRepo.FindResultByCase(f.runID, f.caseIDs[0])
	if err != nil || found == nil || found.ID != sameStamp.ID || !found.UpdatedAt.Equal(sameStamp.UpdatedAt) {
		t.Fatalf("the case's result is %+v (%v), want the one recorded last, %s, as it answered", found, err, sameStamp.ID)
	}
	current, err := f.vvRepo.ListResultsByRun(f.runID)
	if err != nil || len(current) != 3 || current[0].ID != sameStamp.ID {
		t.Fatalf("the run lists %d results (%v), want its three cases, the one recorded last first", len(current), err)
	}
	latest, err := f.vvRepo.LatestResultPerCase(f.projectID)
	if err != nil || latest[f.caseIDs[0]] == nil || latest[f.caseIDs[0]].ID != sameStamp.ID {
		t.Fatalf("the project's latest result for the case is %+v (%v), want %s", latest[f.caseIDs[0]], err, sameStamp.ID)
	}
	byResult, err := svc.CitationsForRun(f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if got := byResult[sameStamp.ID]; len(got) != 1 || got[0].ID != cited.ID {
		t.Fatalf("the case's current result %s has %d citations, want the one cited (%s); the run's citations "+
			"are keyed %v", sameStamp.ID, len(got), cited.ID, byResult)
	}
	history, err := f.vvRepo.ListResultHistoryByRun(f.runID)
	if err != nil || len(history) != 6 || history[0].ID != sameStamp.ID || history[1].ID != stampedEarlier.ID ||
		history[2].ID != stampedLater.ID {
		t.Fatalf("the run's history holds %d results (%v), want six, the case's three re-records newest first", len(history), err)
	}
}

// A completed or aborted run takes no result, checked where the result is
// stored, under the run's lock, so a close that lands between the service's
// read of the run and the insert cannot let one in (#379 bug 5).
func TestAClosedRunTakesNoResultInTheStore(t *testing.T) {
	for _, status := range []string{vv.RunStatusCompleted, vv.RunStatusAborted} {
		t.Run(status, func(t *testing.T) {
			f := newEvidenceFixture(t)
			if _, err := f.db.Exec(`UPDATE test_runs SET status = $2 WHERE id = $1`, f.runID, status); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			r := &vv.TestResult{ID: uuid.New().String(), RunID: f.runID, TestCaseID: f.caseIDs[0],
				TestCaseVersion: 1, Status: vv.ResultPass, CreatedAt: now, UpdatedAt: now}
			err := f.vvRepo.AddResult(r)
			if !errors.Is(err, vv.ErrRunClosed) {
				t.Fatalf("a result for a run that is %s answered %v, want ErrRunClosed", status, err)
			}
			history, err := f.vvRepo.ListResultHistoryByRun(f.runID)
			if err != nil || len(history) != 3 {
				t.Fatalf("the run holds %d results (%v) after the refusal, want the fixture's 3", len(history), err)
			}
		})
	}
	f := newEvidenceFixture(t)
	now := time.Now().UTC()
	err := f.vvRepo.AddResult(&vv.TestResult{ID: uuid.New().String(), RunID: uuid.New().String(),
		TestCaseID: f.caseIDs[0], Status: vv.ResultPass, CreatedAt: now, UpdatedAt: now})
	if !errors.Is(err, vv.ErrRunNotFound) {
		t.Fatalf("a result for a run no row has answered %v, want ErrRunNotFound", err)
	}
}

// A run that holds results is kept: the delete is refused and every result
// stays (REQ-13), where the results went with the run by ON DELETE CASCADE.
// The refusal asks for a run in progress to be completed or aborted instead,
// and says a closed one is already so, where it asked that of a closed run
// too, which cannot be completed or aborted. A run with no result is still
// deleted, and one no row has is not found.
func TestARunWithResultsIsNotDeleted(t *testing.T) {
	f := newEvidenceFixture(t)
	for _, status := range []string{vv.RunStatusInProgress, vv.RunStatusCompleted, vv.RunStatusAborted} {
		if _, err := f.db.Exec(`UPDATE test_runs SET status = $2 WHERE id = $1`, f.runID, status); err != nil {
			t.Fatal(err)
		}
		err := f.vvRepo.DeleteRun(f.runID)
		if !errors.Is(err, vv.ErrRunHasResults) {
			t.Fatalf("deleting a run that is %s and holds results answered %v, want ErrRunHasResults", status, err)
		}
		want := "a test run that holds results is kept: complete or abort it instead of deleting it"
		if status != vv.RunStatusInProgress {
			want = "a test run that holds results is kept: this one is already " + status
		}
		if err.Error() != want {
			t.Fatalf("deleting a run that is %s and holds results answered %q, want %q", status, err, want)
		}
		if run, err := f.vvRepo.FindRunByID(f.runID); err != nil || run == nil || run.Status != status {
			t.Fatalf("the refused run is gone or changed: %+v, %v", run, err)
		}
		if history, err := f.vvRepo.ListResultHistoryByRun(f.runID); err != nil || len(history) != 3 {
			t.Fatalf("the refused run holds %d results (%v), want 3", len(history), err)
		}
	}

	empty := uuid.New().String()
	if _, err := f.db.Exec(`INSERT INTO test_runs (id, project_id, name, status) VALUES ($1, $2, 'Empty', 'aborted')`,
		empty, f.projectID); err != nil {
		t.Fatal(err)
	}
	if err := f.vvRepo.DeleteRun(empty); err != nil {
		t.Fatalf("deleting a run with no result: %v", err)
	}
	if _, err := f.vvRepo.FindRunByID(empty); !errors.Is(err, vv.ErrRunNotFound) {
		t.Fatalf("the empty run is still there: %v", err)
	}
	if err := f.vvRepo.DeleteRun(empty); !errors.Is(err, vv.ErrRunNotFound) {
		t.Fatalf("deleting it again answered %v, want ErrRunNotFound", err)
	}
}

// The 0001 baseline re-runs on every boot and creates
// idx_test_results_run_case UNIQUE ... IF NOT EXISTS. Migration 0049 keeps
// the name on the index it makes non-unique, so a reboot leaves it so and a
// case keeps taking results.
func TestResultHistorySurvivesAReboot(t *testing.T) {
	f := newEvidenceFixture(t)
	if err := Migrate(f.db); err != nil {
		t.Fatalf("second boot: %v", err)
	}
	var unique bool
	if err := f.db.QueryRow(`
		SELECT i.indisunique FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE c.relname = 'idx_test_results_run_case'`).Scan(&unique); err != nil {
		t.Fatalf("read idx_test_results_run_case: %v", err)
	}
	if unique {
		t.Fatal("idx_test_results_run_case is unique again after a reboot")
	}
	base := time.Now().UTC()
	f.recordAgain(t, 1, vv.ResultFail, base.Add(time.Minute))
	f.recordAgain(t, 1, vv.ResultPass, base.Add(2*time.Minute))
	if history, err := f.vvRepo.ListResultHistoryByRun(f.runID); err != nil || len(history) != 5 {
		t.Fatalf("the run holds %d results (%v), want 5", len(history), err)
	}
}
