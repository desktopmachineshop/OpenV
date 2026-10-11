package postgres

import (
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/vv"
)

// X13's literal equality for the V&V repository: every statement it builds
// from testRunColumns, resultColumns and prefixedResultColumns is the literal
// it replaced, byte for byte (spacing, line breaks and tabs included), and
// AddResult and DeleteRun, now on withTx, begin, commit and roll back exactly
// where they did. The statements are read off the wire by the recorder in
// pgkit_test.go, so the test needs no server and checks what each method
// sends, not a copy of it.

// The statements X13 assembles, as the base (7c44002) wrote them, byte for
// byte: each is the literal the method held, the two reads of a run having
// held runBaselineDeleted between the halves of theirs.
const (
	// oldRunBaselineDeleted, the const of that name
	oldRunBaselineDeleted = `test_runs.baseline_id IS NOT NULL AND NOT EXISTS (
			SELECT 1 FROM baselines b WHERE b.id = test_runs.baseline_id AND b.project_id = test_runs.project_id)`
	// oldTestRunColumns, the list both reads of a run repeated
	oldTestRunColumns = `id, project_id, name, description, baseline_id, ` + oldRunBaselineDeleted + `,
			status, started_at, completed_at, created_by, created_at, updated_at`
	// oldFindRunByIDSelect, FindRunByID
	oldFindRunByIDSelect = `
		SELECT id, project_id, name, description, baseline_id, ` + oldRunBaselineDeleted + `,
			status, started_at, completed_at, created_by, created_at, updated_at
		FROM test_runs
		WHERE id = $1
	`
	// oldListRunsByProjectSelect, ListRunsByProject
	oldListRunsByProjectSelect = `
		SELECT id, project_id, name, description, baseline_id, ` + oldRunBaselineDeleted + `,
			status, started_at, completed_at, created_by, created_at, updated_at
		FROM test_runs
		WHERE project_id = $1
		ORDER BY started_at DESC
	`
	// oldResultColumns, the list five statements repeated on one line
	oldResultColumns = `id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at`
	// oldFindResultByCaseSelect, FindResultByCase, whose list breaks after evidence
	oldFindResultByCaseSelect = `
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence,
		       executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM test_results
		WHERE run_id = $1 AND test_case_id = $2
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	// oldListResultsByRunSelect, ListResultsByRun
	oldListResultsByRunSelect = `
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM (
			SELECT DISTINCT ON (test_case_id) *
			FROM test_results
			WHERE run_id = $1
			ORDER BY test_case_id, updated_at DESC, id DESC
		) current
		ORDER BY updated_at DESC
	`
	// oldListResultHistoryByRunSelect, ListResultHistoryByRun
	oldListResultHistoryByRunSelect = `
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM test_results
		WHERE run_id = $1
		ORDER BY updated_at DESC, id DESC
	`
	// oldLatestResultPerCaseSelect, LatestResultPerCase, its list qualified by tr
	oldLatestResultPerCaseSelect = `
		SELECT DISTINCT ON (tr.test_case_id)
			tr.id, tr.run_id, tr.test_case_id, tr.test_case_version, tr.status, tr.notes, tr.evidence, tr.executed_at, tr.executed_by, tr.executed_by_agent_run_id, tr.created_at, tr.updated_at
		FROM test_results tr
		JOIN test_runs runs ON runs.id = tr.run_id
		WHERE runs.project_id = $1 AND runs.status IN ('in-progress', 'completed')
		ORDER BY tr.test_case_id, tr.executed_at DESC NULLS LAST
	`
	// oldAddResultInsert, AddResult
	oldAddResultInsert = `
		INSERT INTO test_results (id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
)

// The statements around them that X13 does not change, also the base's:
// lockRun's, AddResult's look for the result it supersedes and its move of
// the citations, and DeleteRun's check for results and its delete.
const (
	oldLockRun    = `SELECT status FROM test_runs WHERE id = $1 FOR UPDATE`
	oldSuperseded = `
		SELECT id, updated_at, updated_at >= $3::timestamp FROM test_results
		WHERE run_id = $1 AND test_case_id = $2
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`
	oldMoveCitations = `UPDATE evidence_citations SET test_result_id = $1 WHERE test_result_id = $2`
	oldHoldsResults  = `SELECT EXISTS (SELECT 1 FROM test_results WHERE run_id = $1)`
	oldDeleteRun     = `DELETE FROM test_runs WHERE id = $1`
)

func recordedResult() *vv.TestResult {
	now := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	return &vv.TestResult{ID: uuid.New().String(), RunID: uuid.New().String(), TestCaseID: uuid.New().String(),
		TestCaseVersion: 1, Status: vv.ResultPass, CreatedAt: now, UpdatedAt: now}
}

// answerRunStatus makes the recorder answer lockRun with a run of the status.
func answerRunStatus(rec *sqlRecorder, status string) {
	rec.answers = []recAnswer{{contains: "SELECT status FROM test_runs", values: []driver.Value{status}}}
}

// The columns are the old lists, and the qualified list is the old one with
// tr. before each column.
func TestVVColumnsAreTheOldLists(t *testing.T) {
	if testRunColumns != oldTestRunColumns {
		t.Errorf("testRunColumns =\n  %q\nwant\n  %q", testRunColumns, oldTestRunColumns)
	}
	if runBaselineDeleted != oldRunBaselineDeleted {
		t.Errorf("runBaselineDeleted =\n  %q\nwant\n  %q", runBaselineDeleted, oldRunBaselineDeleted)
	}
	if resultColumns != oldResultColumns {
		t.Errorf("resultColumns =\n  %q\nwant\n  %q", resultColumns, oldResultColumns)
	}
	want := "tr." + strings.ReplaceAll(oldResultColumns, ", ", ", tr.")
	if got := prefixedResultColumns("tr"); got != want {
		t.Errorf("prefixedResultColumns(\"tr\") =\n  %q\nwant\n  %q", got, want)
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: FindRunByID vv.ErrRunNotFound, FindResultByCase nothing, each list
// nil and the latest results an empty map.
func TestVVReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewVVRepository(db)
	id := uuid.New().String()

	if got, err := repo.FindRunByID(id); got != nil || !errors.Is(err, vv.ErrRunNotFound) {
		t.Errorf("FindRunByID with no row: %v, %v; want vv.ErrRunNotFound", got, err)
	}
	wantEvents(t, "FindRunByID", rec.take(), "QUERY "+oldFindRunByIDSelect)
	if got, err := repo.ListRunsByProject(id); got != nil || err != nil {
		t.Errorf("ListRunsByProject with no rows: %#v, %v; want nil and no error", got, err)
	}
	wantEvents(t, "ListRunsByProject", rec.take(), "QUERY "+oldListRunsByProjectSelect)
	if got, err := repo.FindResultByCase(id, id); got != nil || err != nil {
		t.Errorf("FindResultByCase with no row: %v, %v; want nothing", got, err)
	}
	wantEvents(t, "FindResultByCase", rec.take(), "QUERY "+oldFindResultByCaseSelect)

	for _, r := range []struct {
		name  string
		read  func() ([]*vv.TestResult, error)
		query string
	}{
		{"ListResultsByRun", func() ([]*vv.TestResult, error) { return repo.ListResultsByRun(id) }, oldListResultsByRunSelect},
		{"ListResultHistoryByRun", func() ([]*vv.TestResult, error) { return repo.ListResultHistoryByRun(id) }, oldListResultHistoryByRunSelect},
	} {
		if list, err := r.read(); list != nil || err != nil {
			t.Errorf("%s with no rows: %#v, %v; want nil and no error", r.name, list, err)
		}
		wantEvents(t, r.name, rec.take(), "QUERY "+r.query)
	}
	if latest, err := repo.LatestResultPerCase(id); latest == nil || len(latest) != 0 || err != nil {
		t.Errorf("LatestResultPerCase with no rows: %#v, %v; want an empty map", latest, err)
	}
	wantEvents(t, "LatestResultPerCase", rec.take(), "QUERY "+oldLatestResultPerCaseSelect)
}

// AddResult begins with no options and no deadline, locks the run, looks for
// the result it supersedes, inserts, moves the citations when there is one,
// and commits; any statement that fails, or a run that is closed or missing,
// rolls it back, and a failed begin or commit is answered as it came.
func TestVVAddResultSendsItsOldLiteralsInOneTransaction(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewVVRepository(db)

	answerRunStatus(rec, vv.RunStatusInProgress)
	if err := repo.AddResult(recordedResult()); err != nil {
		t.Fatalf("AddResult: %v", err)
	}
	wantEvents(t, "AddResult", rec.take(), beginDefault, "QUERY "+oldLockRun, "QUERY "+oldSuperseded,
		"EXEC "+oldAddResultInsert, "COMMIT")

	// A result it supersedes, and the new one is newer: its citations move.
	rec.answers = append(rec.answers, recAnswer{contains: "updated_at >= $3::timestamp",
		values: []driver.Value{uuid.New().String(), time.Date(2026, 3, 14, 9, 0, 0, 0, time.UTC), false}})
	if err := repo.AddResult(recordedResult()); err != nil {
		t.Fatalf("AddResult recording again: %v", err)
	}
	wantEvents(t, "AddResult recording again", rec.take(), beginDefault, "QUERY "+oldLockRun, "QUERY "+oldSuperseded,
		"EXEC "+oldAddResultInsert, "EXEC "+oldMoveCitations, "COMMIT")

	for _, c := range []struct {
		what, failOn string
		events       []string
	}{
		{"the look for the result it supersedes", "updated_at >= $3::timestamp",
			[]string{beginDefault, "QUERY " + oldLockRun, "QUERY " + oldSuperseded, "ROLLBACK"}},
		{"the insert", "INSERT INTO test_results",
			[]string{beginDefault, "QUERY " + oldLockRun, "QUERY " + oldSuperseded, "EXEC " + oldAddResultInsert, "ROLLBACK"}},
		{"the move of the citations", "UPDATE evidence_citations",
			[]string{beginDefault, "QUERY " + oldLockRun, "QUERY " + oldSuperseded, "EXEC " + oldAddResultInsert,
				"EXEC " + oldMoveCitations, "ROLLBACK"}},
		{"the lock", "SELECT status FROM test_runs", []string{beginDefault, "QUERY " + oldLockRun, "ROLLBACK"}},
	} {
		rec.failOn = c.failOn
		if err := repo.AddResult(recordedResult()); err != errRecordedStatement {
			t.Errorf("AddResult whose %s fails: %v, want its error as it came", c.what, err)
		}
		wantEvents(t, "AddResult whose "+c.what+" fails", rec.take(), c.events...)
	}
	rec.failOn = ""

	answerRunStatus(rec, vv.RunStatusCompleted)
	if err := repo.AddResult(recordedResult()); !errors.Is(err, vv.ErrRunClosed) ||
		err.Error() != "this test run is completed; only in-progress runs accept new results" {
		t.Errorf("AddResult into a completed run: %v, want ErrRunClosed as the rule words it", err)
	}
	wantEvents(t, "AddResult into a completed run", rec.take(), beginDefault, "QUERY "+oldLockRun, "ROLLBACK")

	rec.answers = nil
	if err := repo.AddResult(recordedResult()); err != vv.ErrRunNotFound {
		t.Errorf("AddResult into a run no row has: %v, want vv.ErrRunNotFound", err)
	}
	wantEvents(t, "AddResult into a run no row has", rec.take(), beginDefault, "QUERY "+oldLockRun, "ROLLBACK")

	answerRunStatus(rec, vv.RunStatusInProgress)
	rec.failCommit = true
	if err := repo.AddResult(recordedResult()); err != errRecordedCommit {
		t.Errorf("AddResult whose commit fails: %v, want the commit's error as it came", err)
	}
	wantEvents(t, "AddResult whose commit fails", rec.take(), beginDefault, "QUERY "+oldLockRun, "QUERY "+oldSuperseded,
		"EXEC "+oldAddResultInsert, "COMMIT")
	rec.failCommit = false

	rec.failBegin = true
	if err := repo.AddResult(recordedResult()); err != errRecordedBegin {
		t.Errorf("AddResult whose begin fails: %v, want the begin's error as it came", err)
	}
	wantEvents(t, "AddResult whose begin fails", rec.take(), beginDefault)
}

// DeleteRun begins as AddResult does, locks the run, asks whether it holds a
// result, deletes it and commits; a run that holds a result or that no row
// has, or a statement that fails, rolls it back, and a failed begin or commit
// is answered as it came.
func TestVVDeleteRunSendsItsOldLiteralsInOneTransaction(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewVVRepository(db)
	const id = "run"
	holds := func(held bool) {
		rec.answers = append(rec.answers, recAnswer{contains: "SELECT EXISTS", values: []driver.Value{held}})
	}

	answerRunStatus(rec, vv.RunStatusInProgress)
	holds(false)
	if err := repo.DeleteRun(id); err != nil {
		t.Fatalf("DeleteRun: %v", err)
	}
	wantEvents(t, "DeleteRun", rec.take(), beginDefault, "QUERY "+oldLockRun, "QUERY "+oldHoldsResults,
		"EXEC "+oldDeleteRun, "COMMIT")

	for _, status := range []string{vv.RunStatusInProgress, vv.RunStatusCompleted} {
		answerRunStatus(rec, status)
		holds(true)
		want := "a test run that holds results is kept: complete or abort it instead of deleting it"
		if status != vv.RunStatusInProgress {
			want = "a test run that holds results is kept: this one is already " + status
		}
		if err := repo.DeleteRun(id); !errors.Is(err, vv.ErrRunHasResults) || err.Error() != want {
			t.Errorf("DeleteRun of a %s run that holds results: %v, want %q", status, err, want)
		}
		wantEvents(t, "DeleteRun of a "+status+" run that holds results", rec.take(),
			beginDefault, "QUERY "+oldLockRun, "QUERY "+oldHoldsResults, "ROLLBACK")
	}

	answerRunStatus(rec, vv.RunStatusInProgress)
	holds(false)
	for _, c := range []struct {
		what, failOn string
		events       []string
	}{
		{"the lock", "SELECT status FROM test_runs", []string{beginDefault, "QUERY " + oldLockRun, "ROLLBACK"}},
		{"the check for results", "SELECT EXISTS", []string{beginDefault, "QUERY " + oldLockRun, "QUERY " + oldHoldsResults, "ROLLBACK"}},
		{"the delete", "DELETE FROM test_runs", []string{beginDefault, "QUERY " + oldLockRun, "QUERY " + oldHoldsResults,
			"EXEC " + oldDeleteRun, "ROLLBACK"}},
	} {
		rec.failOn = c.failOn
		if err := repo.DeleteRun(id); err != errRecordedStatement {
			t.Errorf("DeleteRun whose %s fails: %v, want its error as it came", c.what, err)
		}
		wantEvents(t, "DeleteRun whose "+c.what+" fails", rec.take(), c.events...)
	}
	rec.failOn = ""

	rec.answers = nil
	if err := repo.DeleteRun(id); err != vv.ErrRunNotFound {
		t.Errorf("DeleteRun of a run no row has: %v, want vv.ErrRunNotFound", err)
	}
	wantEvents(t, "DeleteRun of a run no row has", rec.take(), beginDefault, "QUERY "+oldLockRun, "ROLLBACK")

	answerRunStatus(rec, vv.RunStatusInProgress)
	holds(false)
	rec.failCommit = true
	if err := repo.DeleteRun(id); err != errRecordedCommit {
		t.Errorf("DeleteRun whose commit fails: %v, want the commit's error as it came", err)
	}
	wantEvents(t, "DeleteRun whose commit fails", rec.take(), beginDefault, "QUERY "+oldLockRun, "QUERY "+oldHoldsResults,
		"EXEC "+oldDeleteRun, "COMMIT")
	rec.failCommit = false

	rec.failBegin = true
	if err := repo.DeleteRun(id); err != errRecordedBegin {
		t.Errorf("DeleteRun whose begin fails: %v, want the begin's error as it came", err)
	}
	wantEvents(t, "DeleteRun whose begin fails", rec.take(), beginDefault)
}
