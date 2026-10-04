package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openv/requirements-platform/internal/domain/vv"
)

// VVRepository implements the vv.Repository interface
type VVRepository struct {
	db *sql.DB
}

// NewVVRepository creates a new V&V repository
func NewVVRepository(db *sql.DB) *VVRepository {
	return &VVRepository{db: db}
}

// SaveRun inserts a new test run
func (r *VVRepository) SaveRun(run *vv.TestRun) error {
	query := `
		INSERT INTO test_runs (id, project_id, name, description, baseline_id, status, started_at, completed_at, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := r.db.Exec(
		query,
		run.ID,
		run.ProjectID,
		run.Name,
		run.Description,
		run.BaselineID,
		run.Status,
		run.StartedAt,
		run.CompletedAt,
		run.CreatedBy,
		run.CreatedAt,
		run.UpdatedAt,
	)

	return err
}

// UpdateRun updates an existing test run
func (r *VVRepository) UpdateRun(run *vv.TestRun) error {
	query := `
		UPDATE test_runs
		SET name = $2, description = $3, baseline_id = $4, status = $5, completed_at = $6, updated_at = $7
		WHERE id = $1
	`

	_, err := r.db.Exec(
		query,
		run.ID,
		run.Name,
		run.Description,
		run.BaselineID,
		run.Status,
		run.CompletedAt,
		run.UpdatedAt,
	)

	return err
}

// runBaselineDeleted is the select-list column behind vv.TestRun's
// BaselineDeleted: a run names a baseline its project no longer has. A run
// keeps a deleted baseline's id as history (REQ-13), since baseline_id has no
// foreign key, and every read marks it here rather than a stored flag. It asks
// the run's own project alone, as the create does (REQ-6), so a run never
// tells whether another project's baseline exists.
const runBaselineDeleted = `test_runs.baseline_id IS NOT NULL AND NOT EXISTS (
			SELECT 1 FROM baselines b WHERE b.id = test_runs.baseline_id AND b.project_id = test_runs.project_id)`

// FindRunByID retrieves a test run by ID
func (r *VVRepository) FindRunByID(id string) (*vv.TestRun, error) {
	query := `
		SELECT id, project_id, name, description, baseline_id, ` + runBaselineDeleted + `,
			status, started_at, completed_at, created_by, created_at, updated_at
		FROM test_runs
		WHERE id = $1
	`

	run := &vv.TestRun{}
	err := r.db.QueryRow(query, id).Scan(
		&run.ID,
		&run.ProjectID,
		&run.Name,
		&run.Description,
		&run.BaselineID,
		&run.BaselineDeleted,
		&run.Status,
		&run.StartedAt,
		&run.CompletedAt,
		&run.CreatedBy,
		&run.CreatedAt,
		&run.UpdatedAt,
	)

	if err != nil {
		if noRow(err) {
			return nil, vv.ErrRunNotFound
		}
		return nil, err
	}

	return run, nil
}

// ListRunsByProject retrieves all test runs for a project, newest first
func (r *VVRepository) ListRunsByProject(projectID string) ([]*vv.TestRun, error) {
	query := `
		SELECT id, project_id, name, description, baseline_id, ` + runBaselineDeleted + `,
			status, started_at, completed_at, created_by, created_at, updated_at
		FROM test_runs
		WHERE project_id = $1
		ORDER BY started_at DESC
	`

	rows, err := r.db.Query(query, projectID)
	if malformedID(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []*vv.TestRun
	for rows.Next() {
		run := new(vv.TestRun)
		err := rows.Scan(
			&run.ID,
			&run.ProjectID,
			&run.Name,
			&run.Description,
			&run.BaselineID,
			&run.BaselineDeleted,
			&run.Status,
			&run.StartedAt,
			&run.CompletedAt,
			&run.CreatedBy,
			&run.CreatedAt,
			&run.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	return runs, rows.Err()
}

// lockRun locks a run's row for the rest of tx and returns its status, so
// that recording a result, closing the run and deleting it take turns: a
// result cannot land in a run a concurrent close has closed, nor a delete
// remove a run a concurrent recording has just given a result.
func lockRun(tx *sql.Tx, runID string) (string, error) {
	var status string
	err := tx.QueryRow(`SELECT status FROM test_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&status)
	if noRow(err) {
		return "", vv.ErrRunNotFound
	}
	return status, err
}

// DeleteRun removes a test run that holds no result. A run that holds any is
// refused with vv.ErrRunHasResults: its results are the record REQ-13 keeps,
// so it is closed rather than deleted (their ON DELETE CASCADE now serves
// only the workspace purge, which deletes the runs themselves). The refusal
// asks for a run in progress to be completed or aborted instead, and says a
// closed one is already so, since it can be neither.
func (r *VVRepository) DeleteRun(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	status, err := lockRun(tx, id)
	if err != nil {
		return err
	}
	var holdsResults bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM test_results WHERE run_id = $1)`, id).Scan(&holdsResults); err != nil {
		return err
	}
	if holdsResults && status == vv.RunStatusInProgress {
		return fmt.Errorf("%w: complete or abort it instead of deleting it", vv.ErrRunHasResults)
	}
	if holdsResults {
		return fmt.Errorf("%w: this one is already %s", vv.ErrRunHasResults, status)
	}
	if _, err := tx.Exec(`DELETE FROM test_runs WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// AddResult stores a new result. It never overwrites one (migration 0049):
// a result recorded again for a case the run already has one for is a row
// of its own beside the earlier ones, and the newest is the case's current
// result. The citations of the result it supersedes move to it, so the
// evidence a case's outcome rests on stays with the outcome the run shows
// (REQ-121: a citation stays intact when its result is re-recorded), as it
// did when a re-record overwrote the row a citation names. A result is
// stamped after the one it supersedes, the order the run's lock records
// them in, so its stamps may move; result carries the stamps stored.
func (r *VVRepository) AddResult(result *vv.TestResult) error {
	evidence := result.Evidence
	if evidence == nil {
		evidence = []string{}
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return err
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	status, err := lockRun(tx, result.RunID)
	if err != nil {
		return err
	}
	if err := vv.CheckAcceptsResults(status); err != nil {
		return err
	}

	// The newest result is the one this supersedes, and this one must be
	// newer still, since the reads take the newest by its stamp. The service
	// stamps a result before the lock, so one that waited for it, or one
	// stamped on an instance whose clock runs behind, can carry a stamp at
	// or before the current one's: kept, the case would answer the result
	// this supersedes while its citations moved here. Such a stamp moves to
	// a microsecond, the column's precision, after the current one's, and
	// the time executed with it when it is that same stamp. The comparison
	// is the column's own, of the values as stored.
	var superseded sql.NullString
	var current sql.NullTime
	var behind bool
	err = tx.QueryRow(`
		SELECT id, updated_at, updated_at >= $3::timestamp FROM test_results
		WHERE run_id = $1 AND test_case_id = $2
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, result.RunID, result.TestCaseID, result.UpdatedAt).Scan(&superseded, &current, &behind)
	if err != nil && !noRow(err) {
		return err
	}
	if behind {
		at := current.Time.Add(time.Microsecond)
		if result.ExecutedAt != nil && result.ExecutedAt.Equal(result.UpdatedAt) {
			result.ExecutedAt = &at
		}
		result.CreatedAt, result.UpdatedAt = at, at
	}

	if _, err := tx.Exec(`
		INSERT INTO test_results (id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		result.ID,
		result.RunID,
		result.TestCaseID,
		result.TestCaseVersion,
		result.Status,
		result.Notes,
		evidenceJSON,
		result.ExecutedAt,
		result.ExecutedBy,
		result.ExecutedByAgentRunID,
		result.CreatedAt,
		result.UpdatedAt,
	); err != nil {
		return err
	}

	if superseded.Valid {
		if _, err := tx.Exec(`UPDATE evidence_citations SET test_result_id = $1 WHERE test_result_id = $2`,
			result.ID, superseded.String); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// scanResult scans one test result row
func scanResult(rows *sql.Rows) (*vv.TestResult, error) {
	result := new(vv.TestResult)
	var evidenceJSON []byte

	err := rows.Scan(
		&result.ID,
		&result.RunID,
		&result.TestCaseID,
		&result.TestCaseVersion,
		&result.Status,
		&result.Notes,
		&evidenceJSON,
		&result.ExecutedAt,
		&result.ExecutedBy,
		&result.ExecutedByAgentRunID,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	result.Evidence = []string{}
	if len(evidenceJSON) > 0 {
		if err := json.Unmarshal(evidenceJSON, &result.Evidence); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// FindResultByCase returns the run's current result for one test case, the
// newest recorded, or (nil, nil) when nothing has been recorded for it yet.
func (r *VVRepository) FindResultByCase(runID, testCaseID string) (*vv.TestResult, error) {
	rows, err := r.db.Query(`
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence,
		       executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM test_results
		WHERE run_id = $1 AND test_case_id = $2
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, runID, testCaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanResult(rows)
}

// ListResultsByRun retrieves the run's current result per test case, the
// latest recorded first.
func (r *VVRepository) ListResultsByRun(runID string) ([]*vv.TestResult, error) {
	return r.listResults(`
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM (
			SELECT DISTINCT ON (test_case_id) *
			FROM test_results
			WHERE run_id = $1
			ORDER BY test_case_id, updated_at DESC, id DESC
		) current
		ORDER BY updated_at DESC
	`, runID)
}

// ListResultHistoryByRun retrieves every result recorded in the run, the
// ones later results superseded included, the latest recorded first.
func (r *VVRepository) ListResultHistoryByRun(runID string) ([]*vv.TestResult, error) {
	return r.listResults(`
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM test_results
		WHERE run_id = $1
		ORDER BY updated_at DESC, id DESC
	`, runID)
}

func (r *VVRepository) listResults(query string, args ...interface{}) ([]*vv.TestResult, error) {
	rows, err := r.db.Query(query, args...)
	if malformedID(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*vv.TestResult
	for rows.Next() {
		result, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, rows.Err()
}

// LatestResultPerCase returns the latest executed result per test case across
// the project's in-progress and completed runs.
func (r *VVRepository) LatestResultPerCase(projectID string) (map[string]*vv.TestResult, error) {
	query := `
		SELECT DISTINCT ON (tr.test_case_id)
			tr.id, tr.run_id, tr.test_case_id, tr.test_case_version, tr.status, tr.notes, tr.evidence, tr.executed_at, tr.executed_by, tr.executed_by_agent_run_id, tr.created_at, tr.updated_at
		FROM test_results tr
		JOIN test_runs runs ON runs.id = tr.run_id
		WHERE runs.project_id = $1 AND runs.status IN ('in-progress', 'completed')
		ORDER BY tr.test_case_id, tr.executed_at DESC NULLS LAST
	`

	rows, err := r.db.Query(query, projectID)
	if malformedID(err) {
		return map[string]*vv.TestResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	latest := make(map[string]*vv.TestResult)
	for rows.Next() {
		result, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		latest[result.TestCaseID] = result
	}

	return latest, rows.Err()
}
