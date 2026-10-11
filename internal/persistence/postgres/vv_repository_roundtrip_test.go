package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// The V&V repository's round trip (refactor plan X13's characterization for
// vv_repository.go, in the manner of X13a's), pinned as found: every column
// of a test run and of a test result read back through each read that scans
// them, the NULLs and the scan errors that name a column's place, what a read
// answers for a row no one has and for an empty list, and both transactions,
// AddResult's and DeleteRun's, committing what they write or, when a
// statement inside them fails, leaving nothing behind. X13 moves the run's
// and the result's columns into runColumns and resultColumns, the scans into
// scanRun and scanResult, and the two transactions into withTx; these tests,
// with test_result_history_test.go, test_run_baseline_test.go and
// malformed_parent_list_test.go, are what holds the repository still while it
// does. The helpers are in repository_roundtrip_helpers_test.go.

func rtRunSansTimes(r *vv.TestRun) vv.TestRun {
	c := *r
	c.StartedAt, c.CreatedAt, c.UpdatedAt, c.CompletedAt = time.Time{}, time.Time{}, time.Time{}, nil
	return c
}

func rtResultSansTimes(r *vv.TestResult) vv.TestResult {
	c := *r
	c.CreatedAt, c.UpdatedAt, c.ExecutedAt = time.Time{}, time.Time{}, nil
	return c
}

// rtWantOptionalTimestamp fails unless got is nil when sent is, and is
// otherwise what a TIMESTAMP column hands back for sent.
func rtWantOptionalTimestamp(t *testing.T, what string, got, sent *time.Time) {
	t.Helper()
	if (got == nil) != (sent == nil) {
		t.Errorf("%s read back as %v, want %v", what, got, sent)
		return
	}
	if got != nil {
		rtWantTimestamp(t, what, *got, *sent)
	}
}

// rtWantRun fails unless got is sent as a run reads back: every field but
// the times equal, and each time the TIMESTAMP column's wall clock of the
// time sent. sent holds BaselineDeleted as the read computes it.
func rtWantRun(t *testing.T, what string, got, sent *vv.TestRun) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtRunSansTimes(got), rtRunSansTimes(sent))
	rtWantTimestamp(t, what+" started_at", got.StartedAt, sent.StartedAt)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
	rtWantOptionalTimestamp(t, what+" completed_at", got.CompletedAt, sent.CompletedAt)
}

func rtWantRunList(t *testing.T, what string, list []*vv.TestRun, err error, want ...*vv.TestRun) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var got, wantNames []string
	for _, r := range list {
		got = append(got, r.Name)
	}
	for _, r := range want {
		wantNames = append(wantNames, r.Name)
	}
	if strings.Join(got, "|") != strings.Join(wantNames, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantNames)
		return
	}
	for i := range list {
		rtWantRun(t, what+" "+want[i].Name, list[i], want[i])
	}
}

func rtWantResult(t *testing.T, what string, got, sent *vv.TestResult) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtResultSansTimes(got), rtResultSansTimes(sent))
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
	rtWantOptionalTimestamp(t, what+" executed_at", got.ExecutedAt, sent.ExecutedAt)
}

func rtWantResultList(t *testing.T, what string, list []*vv.TestResult, err error, want ...*vv.TestResult) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var got, wantIDs []string
	for _, r := range list {
		got = append(got, r.ID)
	}
	for _, r := range want {
		wantIDs = append(wantIDs, r.ID)
	}
	if strings.Join(got, "|") != strings.Join(wantIDs, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantIDs)
		return
	}
	for i := range list {
		rtWantResult(t, fmt.Sprintf("%s result %d", what, i), list[i], want[i])
	}
}

// rtVVProject seeds a project (a test run names one that exists) and answers
// its id.
func rtVVProject(t *testing.T, repo *VVRepository) string {
	t.Helper()
	id := uuid.New().String()
	seedProjects(t, repo.db, id)
	return id
}

func rtRun(projectID, name string, at time.Time) *vv.TestRun {
	return &vv.TestRun{ID: uuid.New().String(), ProjectID: projectID, Name: name, Status: vv.RunStatusInProgress,
		StartedAt: at, CreatedAt: at, UpdatedAt: at}
}

func rtVVResult(runID, caseID string, at time.Time) *vv.TestResult {
	return &vv.TestResult{ID: uuid.New().String(), RunID: runID, TestCaseID: caseID, TestCaseVersion: 1,
		Status: vv.ResultPass, CreatedAt: at, UpdatedAt: at}
}

// rtWantRunUnlocked fails unless no transaction holds the run's row: it asks
// for the row's lock with NOWAIT, which Postgres refuses at once (55P03)
// where a lock is held, so a transaction left open fails the test here
// rather than making a later statement wait. It tells a refusal the
// repository makes itself (a transaction left open holds the lock) from a
// statement Postgres refuses (it aborts the transaction and drops its locks
// whether or not the repository rolls back).
func rtWantRunUnlocked(t *testing.T, repo *VVRepository, what, runID string) {
	t.Helper()
	tx, err := repo.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	if err := tx.QueryRow(`SELECT id FROM test_runs WHERE id = $1 FOR UPDATE NOWAIT`, runID).Scan(&id); err != nil {
		t.Fatalf("%s: the run's row is locked or gone: %v", what, err)
	}
}

func rtResultRows(t *testing.T, repo *VVRepository, runID string) int {
	t.Helper()
	var n int
	if err := repo.db.QueryRow(`SELECT COUNT(*) FROM test_results WHERE run_id = $1`, runID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// rtWantUnwrappedPQ fails unless err is Postgres's own refusal handed back
// as it came (not wrapped), with the SQLSTATE code.
func rtWantUnwrappedPQ(t *testing.T, what string, err error, code string) {
	t.Helper()
	pqErr, ok := err.(*pq.Error)
	if !ok || string(pqErr.Code) != code {
		t.Errorf("%s: %v (%T), want Postgres's refusal %s as it came", what, err, err, code)
	}
}

// Every column SaveRun writes reads back through FindRunByID and
// ListRunsByProject: the baseline (with baseline_deleted false while its
// project has it), the description, status, created_by and the four
// TIMESTAMP stamps (the wall clock sent, its offset dropped, to the
// microsecond), the list newest-started first. A run with no baseline, no
// completion and no creator reads back nil pointers and an empty
// description. UpdateRun rewrites the name, description, baseline, status,
// completed_at and updated_at, never the project, started_at, created_by or
// created_at, and is no error for a run no row has; SaveRun of an id a run
// has is Postgres's refusal, as it came.
func TestVVRepositoryReadsBackEveryRunColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	projectID := rtVVProject(t, repo)

	baseline := &baselines.Baseline{ID: uuid.New().String(), ProjectID: projectID, Name: "Release 1",
		Snapshot: []byte(`{}`), CreatedAt: rtAt(0)}
	if err := NewBaselineRepository(db).Create(baseline); err != nil {
		t.Fatal(err)
	}
	creator, completed := uuid.New().String(), rtAt(50).In(rtCEST)
	full := &vv.TestRun{ID: uuid.New().String(), ProjectID: projectID, Name: "Noise campaign", Description: "Rig noise",
		BaselineID: &baseline.ID, Status: vv.RunStatusCompleted, StartedAt: rtAt(10).In(rtCEST), CompletedAt: &completed,
		CreatedBy: &creator, CreatedAt: rtAt(5).In(rtCEST), UpdatedAt: rtAt(51).In(rtCEST)}
	bare := rtRun(projectID, "Bare", rtAt(20))
	for _, r := range []*vv.TestRun{full, bare} {
		if err := repo.SaveRun(r); err != nil {
			t.Fatalf("SaveRun(%s): %v", r.Name, err)
		}
		got, err := repo.FindRunByID(r.ID)
		if err != nil {
			t.Fatalf("FindRunByID(%s): %v", r.Name, err)
		}
		rtWantRun(t, "FindRunByID "+r.Name, got, r)
	}
	// Newest started first, by the wall clock a +02:00 stamp keeps.
	list, err := repo.ListRunsByProject(projectID)
	rtWantRunList(t, "ListRunsByProject", list, err, full, bare)
	rtWantUnwrappedPQ(t, "SaveRun of an id a run has", repo.SaveRun(full), "23505")

	changed := *full
	other := uuid.New().String()
	changed.ProjectID, changed.StartedAt, changed.CreatedBy, changed.CreatedAt = other, rtAt(900), &other, rtAt(901)
	changed.Name, changed.Description, changed.BaselineID, changed.Status = "Renamed", "", nil, vv.RunStatusAborted
	changed.CompletedAt, changed.UpdatedAt = nil, rtAt(60).In(rtCEST)
	if err := repo.UpdateRun(&changed); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	want := changed
	want.ProjectID, want.StartedAt, want.CreatedBy, want.CreatedAt = full.ProjectID, full.StartedAt, full.CreatedBy, full.CreatedAt
	got, err := repo.FindRunByID(full.ID)
	rtWantRun(t, "FindRunByID after UpdateRun", got, &want)
	if err != nil {
		t.Errorf("FindRunByID after UpdateRun: %v", err)
	}
	if err := repo.UpdateRun(rtRun(projectID, "Ghost", rtAt(0))); err != nil {
		t.Errorf("UpdateRun of a run no row has: %v, want no error", err)
	}
}

// Every column AddResult writes reads back through FindResultByCase,
// ListResultsByRun, ListResultHistoryByRun and LatestResultPerCase: the case
// version, status, notes, evidence, executed_at, executed_by, the agent run
// and the stamps. A result stored with no evidence reads back an empty,
// non-nil list, and one with no executor nil pointers. The project's latest
// results leave out a run that is aborted.
func TestVVRepositoryReadsBackEveryResultColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	projectID := rtVVProject(t, repo)
	run := rtRun(projectID, "Run", rtAt(0))
	aborted := rtRun(projectID, "Aborted", rtAt(1))
	for _, r := range []*vv.TestRun{run, aborted} {
		if err := repo.SaveRun(r); err != nil {
			t.Fatal(err)
		}
	}

	caseFull, caseBare, caseAborted := uuid.New().String(), uuid.New().String(), uuid.New().String()
	executedAt, executedBy, agentRun := rtAt(2).In(rtCEST), uuid.New().String(), uuid.New().String()
	full := &vv.TestResult{ID: uuid.New().String(), RunID: run.ID, TestCaseID: caseFull, TestCaseVersion: 3,
		Status: vv.ResultFail, Notes: "Spindle ran 3 dB over", Evidence: []string{"att-1", "att-2"}, ExecutedAt: &executedAt,
		ExecutedBy: &executedBy, ExecutedByAgentRunID: &agentRun, CreatedAt: rtAt(1).In(rtCEST), UpdatedAt: rtAt(3).In(rtCEST)}
	bare := rtVVResult(run.ID, caseBare, rtAt(4))
	bare.Status = vv.ResultNotRun
	skipped := rtVVResult(aborted.ID, caseAborted, rtAt(5))
	for _, r := range []*vv.TestResult{full, bare, skipped} {
		if err := repo.AddResult(r); err != nil {
			t.Fatalf("AddResult: %v", err)
		}
	}
	rtSeed(t, db, `UPDATE test_runs SET status = 'aborted' WHERE id = $1`, aborted.ID)
	// What a read hands back: no evidence is an empty list.
	bareRead := *bare
	bareRead.Evidence = []string{}
	skippedRead := *skipped
	skippedRead.Evidence = []string{}

	for _, sent := range []*vv.TestResult{full, &bareRead} {
		got, err := repo.FindResultByCase(run.ID, sent.TestCaseID)
		if err != nil {
			t.Fatalf("FindResultByCase: %v", err)
		}
		rtWantResult(t, "FindResultByCase "+sent.Status, got, sent)
	}
	// Latest updated first, by the wall clock a +02:00 stamp keeps.
	list, err := repo.ListResultsByRun(run.ID)
	rtWantResultList(t, "ListResultsByRun", list, err, full, &bareRead)
	list, err = repo.ListResultHistoryByRun(run.ID)
	rtWantResultList(t, "ListResultHistoryByRun", list, err, full, &bareRead)
	list, err = repo.ListResultHistoryByRun(aborted.ID)
	rtWantResultList(t, "ListResultHistoryByRun of an aborted run", list, err, &skippedRead)

	latest, err := repo.LatestResultPerCase(projectID)
	if err != nil || len(latest) != 2 {
		t.Fatalf("LatestResultPerCase: %d results, %v; want the two cases of the run in progress", len(latest), err)
	}
	rtWantResult(t, "LatestResultPerCase's full result", latest[caseFull], full)
	rtWantResult(t, "LatestResultPerCase's bare result", latest[caseBare], &bareRead)
	if latest[caseAborted] != nil {
		t.Errorf("LatestResultPerCase holds the aborted run's result %s", rtJSON(latest[caseAborted]))
	}
}

// The evidence column takes NULL, JSON null and a JSON value that is no list
// of strings in a test that lets it, and each read says what it makes of
// them: NULL reads as an empty list, JSON null as a nil one, and an object
// fails the read with the decoder's error as it came.
func TestVVRepositoryReadsOddEvidence(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	rtSeed(t, db, `ALTER TABLE test_results ALTER COLUMN evidence DROP NOT NULL`)

	for _, c := range []struct {
		name, evidence string
		want           []string
		err            string
	}{
		{"NULL", `NULL`, []string{}, ""},
		{"JSON null", `'null'::jsonb`, nil, ""},
		{"an empty list", `'[]'::jsonb`, []string{}, ""},
		{"an object", `'{}'::jsonb`, nil, "json: cannot unmarshal object into Go value of type []string"},
	} {
		t.Run(c.name, func(t *testing.T) {
			projectID := rtVVProject(t, repo)
			run := rtRun(projectID, "Run", rtAt(0))
			if err := repo.SaveRun(run); err != nil {
				t.Fatal(err)
			}
			caseID, id := uuid.New().String(), uuid.New().String()
			rtSeed(t, db, `INSERT INTO test_results (id, run_id, test_case_id, evidence) VALUES ($1, $2, $3, `+c.evidence+`)`,
				id, run.ID, caseID)

			reads := map[string]func() ([]*vv.TestResult, error){
				"FindResultByCase": func() ([]*vv.TestResult, error) {
					r, err := repo.FindResultByCase(run.ID, caseID)
					if r == nil {
						return nil, err
					}
					return []*vv.TestResult{r}, err
				},
				"ListResultsByRun":       func() ([]*vv.TestResult, error) { return repo.ListResultsByRun(run.ID) },
				"ListResultHistoryByRun": func() ([]*vv.TestResult, error) { return repo.ListResultHistoryByRun(run.ID) },
				"LatestResultPerCase": func() ([]*vv.TestResult, error) {
					m, err := repo.LatestResultPerCase(projectID)
					var list []*vv.TestResult
					for _, r := range m {
						list = append(list, r)
					}
					return list, err
				},
			}
			for name, read := range reads {
				list, err := read()
				if c.err != "" {
					if list != nil || err == nil || err.Error() != c.err {
						t.Errorf("%s over %s: %s, %v; want nothing and %q", name, c.name, rtJSON(list), err, c.err)
					}
					continue
				}
				if err != nil || len(list) != 1 || list[0].ID != id {
					t.Errorf("%s over %s: %s, %v; want the result", name, c.name, rtJSON(list), err)
					continue
				}
				if !equalStrings(list[0].Evidence, c.want) || (list[0].Evidence == nil) != (c.want == nil) {
					t.Errorf("%s over %s read evidence %#v, want %#v", name, c.name, list[0].Evidence, c.want)
				}
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A column the schema forbids NULL, here one a test has let take it, fails
// the read that scans it as the driver says, unwrapped, naming the column and
// its place among the twelve scanned (a run: name 2, description 3, status 6,
// started_at 7, created_at 10, updated_at 11; a result: test_case_id 2,
// test_case_version 3, status 4, notes 5, created_at 10, updated_at 11), and
// answers no run, result or list.
func TestVVRepositoryScanErrorsNameTheColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	const (
		str  = `converting NULL to string is unsupported`
		num  = `converting NULL to int is unsupported`
		when = `unsupported Scan, storing driver.Value type <nil> into type *time.Time`
	)
	for _, c := range []string{"name", "description", "status", "started_at", "created_at", "updated_at"} {
		rtSeed(t, db, `ALTER TABLE test_runs ALTER COLUMN `+c+` DROP NOT NULL`)
	}
	for _, c := range []string{"test_case_id", "test_case_version", "status", "notes", "created_at", "updated_at"} {
		rtSeed(t, db, `ALTER TABLE test_results ALTER COLUMN `+c+` DROP NOT NULL`)
	}

	for _, c := range []struct {
		column string
		index  int
		cause  string
	}{
		{"name", 2, str}, {"description", 3, str}, {"status", 6, str}, {"started_at", 7, when}, {"created_at", 10, when}, {"updated_at", 11, when},
	} {
		projectID := rtVVProject(t, repo)
		id := uuid.New().String()
		values := map[string]string{"name": "'Broken'", "description": "''", "status": "'in-progress'", "started_at": "NOW()",
			"created_at": "NOW()", "updated_at": "NOW()"}
		values[c.column] = "NULL"
		rtSeed(t, db, fmt.Sprintf(`INSERT INTO test_runs (id, project_id, name, description, status, started_at, created_at, updated_at)
			VALUES ($1, $2, %s, %s, %s, %s, %s, %s)`, values["name"], values["description"], values["status"],
			values["started_at"], values["created_at"], values["updated_at"]), id, projectID)
		want := fmt.Sprintf(`sql: Scan error on column index %d, name "%s": %s`, c.index, c.column, c.cause)
		if got, err := repo.FindRunByID(id); got != nil || err == nil || err.Error() != want {
			t.Errorf("FindRunByID of a run with a NULL %s: %v, %v; want %q", c.column, got, err, want)
		}
		if got, err := repo.ListRunsByProject(projectID); got != nil || err == nil || err.Error() != want {
			t.Errorf("ListRunsByProject over a run with a NULL %s: %v, %v; want %q", c.column, got, err, want)
		}
	}

	for _, c := range []struct {
		column string
		index  int
		cause  string
	}{
		{"test_case_id", 2, str}, {"test_case_version", 3, num}, {"status", 4, str}, {"notes", 5, str}, {"created_at", 10, when}, {"updated_at", 11, when},
	} {
		projectID := rtVVProject(t, repo)
		run := rtRun(projectID, "Run", rtAt(0))
		if err := repo.SaveRun(run); err != nil {
			t.Fatal(err)
		}
		caseID := uuid.New().String()
		values := map[string]string{"test_case_id": "$3", "test_case_version": "1", "status": "'pass'", "notes": "''",
			"created_at": "NOW()", "updated_at": "NOW()"}
		values[c.column] = "NULL"
		args := []interface{}{uuid.New().String(), run.ID, caseID}
		if c.column == "test_case_id" {
			args = args[:2]
		}
		rtSeed(t, db, fmt.Sprintf(`INSERT INTO test_results (id, run_id, test_case_id, test_case_version, status, notes, created_at, updated_at)
			VALUES ($1, $2, %s, %s, %s, %s, %s, %s)`, values["test_case_id"], values["test_case_version"], values["status"],
			values["notes"], values["created_at"], values["updated_at"]), args...)
		want := fmt.Sprintf(`sql: Scan error on column index %d, name "%s": %s`, c.index, c.column, c.cause)
		// A result with no case is read by the lists alone: no case id finds it.
		if c.column != "test_case_id" {
			if got, err := repo.FindResultByCase(run.ID, caseID); got != nil || err == nil || err.Error() != want {
				t.Errorf("FindResultByCase of a result with a NULL %s: %v, %v; want %q", c.column, got, err, want)
			}
		}
		for name, read := range map[string]func() (interface{}, error){
			"ListResultsByRun":       func() (interface{}, error) { l, err := repo.ListResultsByRun(run.ID); return l == nil, err },
			"ListResultHistoryByRun": func() (interface{}, error) { l, err := repo.ListResultHistoryByRun(run.ID); return l == nil, err },
			"LatestResultPerCase":    func() (interface{}, error) { m, err := repo.LatestResultPerCase(projectID); return m == nil, err },
		} {
			if isNil, err := read(); isNil != true || err == nil || err.Error() != want {
				t.Errorf("%s over a result with a NULL %s: nil %v, %v; want nil and %q", name, c.column, isNil, err, want)
			}
		}
	}
}

// What a read answers for no row: FindRunByID vv.ErrRunNotFound, for an id no
// run has and for a malformed one; FindResultByCase no result and no error
// for a case with none, and Postgres's refusal of a malformed run id; each
// list nil (JSON null), but the project's latest results, which is an empty,
// non-nil map. A delete or a result for a run no row has, a malformed id
// included, is vv.ErrRunNotFound.
func TestVVRepositoryAnswersNoRow(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	projectID := rtVVProject(t, repo)
	run := rtRun(projectID, "Empty", rtAt(0))
	if err := repo.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	notFound := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, vv.ErrRunNotFound) || err.Error() != "test run not found" {
			t.Errorf("%s: %v, want vv.ErrRunNotFound", what, err)
		}
	}

	for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
		got, err := repo.FindRunByID(id)
		if got != nil {
			t.Errorf("FindRunByID(%q) read %v", id, got)
		}
		notFound(fmt.Sprintf("FindRunByID(%q)", id), err)
		notFound(fmt.Sprintf("DeleteRun(%q)", id), repo.DeleteRun(id))
		notFound(fmt.Sprintf("AddResult into run %q", id), repo.AddResult(rtVVResult(id, uuid.New().String(), rtAt(0))))
	}

	result, err := repo.FindResultByCase(run.ID, uuid.New().String())
	if result != nil || err != nil {
		t.Errorf("FindResultByCase of a case with no result: %v, %v; want nil and no error", result, err)
	}
	for _, id := range malformedIDs {
		result, err = repo.FindResultByCase(id, uuid.New().String())
		if result != nil {
			t.Errorf("FindResultByCase(%q) read %v", id, result)
		}
		rtWantRefused(t, fmt.Sprintf("FindResultByCase(%q)", id), err)
	}

	for _, id := range []string{projectID, uuid.New().String()} {
		runs, err := repo.ListRunsByProject(id)
		if id == projectID {
			if err != nil || len(runs) != 1 {
				t.Errorf("ListRunsByProject: %d runs, %v; want the one", len(runs), err)
			}
			continue
		}
		rtWantNil(t, "ListRunsByProject of a project with no run", runs, err)
	}
	for _, id := range []string{run.ID, uuid.New().String()} {
		list, err := repo.ListResultsByRun(id)
		rtWantNil(t, "ListResultsByRun of a run with no result", list, err)
		list, err = repo.ListResultHistoryByRun(id)
		rtWantNil(t, "ListResultHistoryByRun of a run with no result", list, err)
	}
	for _, id := range []string{projectID, uuid.New().String()} {
		latest, err := repo.LatestResultPerCase(id)
		if err != nil || latest == nil || len(latest) != 0 {
			t.Errorf("LatestResultPerCase of a project with no result: %#v (nil: %v), %v; want an empty, non-nil map", latest, latest == nil, err)
		}
	}
	for _, id := range malformedIDs {
		runs, err := repo.ListRunsByProject(id)
		rtWantNil(t, fmt.Sprintf("ListRunsByProject(%q)", id), runs, err)
		latest, err := repo.LatestResultPerCase(id)
		if err != nil || latest == nil || len(latest) != 0 {
			t.Errorf("LatestResultPerCase(%q): %#v, %v; want an empty, non-nil map", id, latest, err)
		}
	}
}

// AddResult commits the result with the move of the citations of the result
// it supersedes. When a statement fails after the lock, nothing is left
// behind: not the result, nor the citations' move. A refused insert (an id a
// result has) and a refused move (a trigger that refuses every update of a
// citation, standing in for a failure) are Postgres's refusal as it came. A
// closed run's refusal, which the repository makes itself, leaves the run's
// lock free.
func TestVVAddResultCommitsOrLeavesNothing(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	projectID := rtVVProject(t, repo)
	run := rtRun(projectID, "Run", rtAt(0))
	if err := repo.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	caseID := uuid.New().String()
	first := rtVVResult(run.ID, caseID, time.Now().UTC().Add(-time.Hour))
	if err := repo.AddResult(first); err != nil {
		t.Fatal(err)
	}
	svc := evidence.NewDefaultService(NewEvidenceRepository(db))
	bundle, err := svc.Create(projectID, evidence.CreateRequest{Title: "Capture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cited, err := svc.Cite(first.ID, bundle.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	citedBy := func() string {
		t.Helper()
		var id string
		if err := db.QueryRow(`SELECT test_result_id FROM evidence_citations WHERE id = $1`, cited.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	wantUnchanged := func(what string, results int, citation string) {
		t.Helper()
		if n := rtResultRows(t, repo, run.ID); n != results {
			t.Errorf("%s: the run holds %d results, want %d", what, n, results)
		}
		if got := citedBy(); got != citation {
			t.Errorf("%s: the citation sits on %s, want %s", what, got, citation)
		}
	}

	rtSeed(t, db, `CREATE FUNCTION rt_refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused by the test' USING ERRCODE = 'P0001'; END $$`)
	rtSeed(t, db, `CREATE TRIGGER rt_refuse_move BEFORE UPDATE ON evidence_citations FOR EACH STATEMENT EXECUTE FUNCTION rt_refuse()`)
	refused := rtVVResult(run.ID, caseID, time.Now().UTC())
	rtWantUnwrappedPQ(t, "AddResult whose citation move is refused", repo.AddResult(refused), "P0001")
	wantUnchanged("after the refused move", 1, first.ID)
	rtSeed(t, db, `DROP TRIGGER rt_refuse_move ON evidence_citations`)

	clash := rtVVResult(run.ID, caseID, time.Now().UTC())
	clash.ID = first.ID
	rtWantUnwrappedPQ(t, "AddResult of an id a result has", repo.AddResult(clash), "23505")
	wantUnchanged("after the refused insert", 1, first.ID)
	if got, err := repo.FindResultByCase(run.ID, caseID); err != nil || got == nil || got.ID != first.ID {
		t.Errorf("after the refusals the case's result is %v, %v; want the first", got, err)
	}

	// A closed run refuses the result with an error of the repository's own,
	// which Postgres did not raise: the transaction it left must not hold the
	// run's lock.
	rtSeed(t, db, `UPDATE test_runs SET status = 'completed' WHERE id = $1`, run.ID)
	if err := repo.AddResult(rtVVResult(run.ID, caseID, time.Now().UTC())); !errors.Is(err, vv.ErrRunClosed) {
		t.Fatalf("AddResult into a completed run: %v, want ErrRunClosed", err)
	}
	wantUnchanged("after the refusal of a closed run", 1, first.ID)
	rtWantRunUnlocked(t, repo, "after the refusal of a closed run", run.ID)
	rtSeed(t, db, `UPDATE test_runs SET status = 'in-progress' WHERE id = $1`, run.ID)

	second := rtVVResult(run.ID, caseID, time.Now().UTC())
	if err := repo.AddResult(second); err != nil {
		t.Fatalf("AddResult after the refusals: %v", err)
	}
	wantUnchanged("after the result recorded again", 2, second.ID)
}

// DeleteRun removes a run that holds no result and commits; a run that holds
// one, or that no row has, is refused without a change. When the delete
// itself is refused (a trigger that refuses every delete of a run, standing
// in for a failure) the run stays and the refusal is Postgres's, as it came.
// The refusal of a run that holds results, which the repository makes
// itself, leaves the run's lock free.
func TestVVDeleteRunCommitsOrLeavesNothing(t *testing.T) {
	db := rtDB(t)
	repo := NewVVRepository(db)
	projectID := rtVVProject(t, repo)
	empty, held := rtRun(projectID, "Empty", rtAt(0)), rtRun(projectID, "Held", rtAt(1))
	for _, r := range []*vv.TestRun{empty, held} {
		if err := repo.SaveRun(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.AddResult(rtVVResult(held.ID, uuid.New().String(), rtAt(2))); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteRun(held.ID); !errors.Is(err, vv.ErrRunHasResults) {
		t.Fatalf("DeleteRun of a run with a result: %v, want ErrRunHasResults", err)
	}
	if _, err := repo.FindRunByID(held.ID); err != nil || rtResultRows(t, repo, held.ID) != 1 {
		t.Errorf("after the refusal the run is %v (%v) with %d results, want it kept whole", held.ID, err, rtResultRows(t, repo, held.ID))
	}

	rtWantRunUnlocked(t, repo, "after the refused DeleteRun of a run with a result", held.ID)

	rtSeed(t, db, `CREATE FUNCTION rt_refuse() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'refused by the test' USING ERRCODE = 'P0001'; END $$`)
	rtSeed(t, db, `CREATE TRIGGER rt_refuse_delete BEFORE DELETE ON test_runs FOR EACH STATEMENT EXECUTE FUNCTION rt_refuse()`)
	rtWantUnwrappedPQ(t, "DeleteRun whose delete is refused", repo.DeleteRun(empty.ID), "P0001")
	if _, err := repo.FindRunByID(empty.ID); err != nil {
		t.Errorf("after the refused delete the run is gone: %v", err)
	}
	rtSeed(t, db, `DROP TRIGGER rt_refuse_delete ON test_runs`)

	if err := repo.DeleteRun(empty.ID); err != nil {
		t.Fatalf("DeleteRun of a run with no result: %v", err)
	}
	if _, err := repo.FindRunByID(empty.ID); !errors.Is(err, vv.ErrRunNotFound) {
		t.Errorf("after DeleteRun the run reads %v, want ErrRunNotFound", err)
	}
	if err := repo.AddResult(rtVVResult(held.ID, uuid.New().String(), rtAt(3))); err != nil {
		t.Errorf("AddResult into the run after the refusals: %v", err)
	}
}

// On a database that fails every statement, each method hands the failure
// back as it came, unwrapped: the reads from their query, AddResult and
// DeleteRun from the transaction's begin (AddResult after it has encoded the
// evidence). A read answers no run, result or list. It needs no server.
func TestVVRepositoryOnAFailingDatabase(t *testing.T) {
	repo := NewVVRepository(rtClosedDB(t))
	id := uuid.New().String()
	run := rtRun(id, "Closed", rtAt(0))

	rtWantClosed(t, "SaveRun", repo.SaveRun(run), "")
	rtWantClosed(t, "UpdateRun", repo.UpdateRun(run), "")
	rtWantClosed(t, "DeleteRun", repo.DeleteRun(id), "")
	rtWantClosed(t, "AddResult", repo.AddResult(rtVVResult(id, id, rtAt(0))), "")
	got, err := repo.FindRunByID(id)
	if got != nil {
		t.Errorf("FindRunByID on a failing database read %v", got)
	}
	rtWantClosed(t, "FindRunByID", err, "")
	runs, err := repo.ListRunsByProject(id)
	rtWantNil(t, "", runs, nil)
	rtWantClosed(t, "ListRunsByProject", err, "")
	result, err := repo.FindResultByCase(id, id)
	if result != nil {
		t.Errorf("FindResultByCase on a failing database read %v", result)
	}
	rtWantClosed(t, "FindResultByCase", err, "")
	results, err := repo.ListResultsByRun(id)
	rtWantNil(t, "", results, nil)
	rtWantClosed(t, "ListResultsByRun", err, "")
	results, err = repo.ListResultHistoryByRun(id)
	rtWantNil(t, "", results, nil)
	rtWantClosed(t, "ListResultHistoryByRun", err, "")
	latest, err := repo.LatestResultPerCase(id)
	if latest != nil {
		t.Errorf("LatestResultPerCase on a failing database read %v", latest)
	}
	rtWantClosed(t, "LatestResultPerCase", err, "")
}
