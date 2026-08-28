package postgres

import (
	"database/sql"
	"encoding/json"

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

// FindRunByID retrieves a test run by ID
func (r *VVRepository) FindRunByID(id string) (*vv.TestRun, error) {
	query := `
		SELECT id, project_id, name, description, baseline_id, status, started_at, completed_at, created_by, created_at, updated_at
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
		&run.Status,
		&run.StartedAt,
		&run.CompletedAt,
		&run.CreatedBy,
		&run.CreatedAt,
		&run.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, vv.ErrRunNotFound
		}
		return nil, err
	}

	return run, nil
}

// ListRunsByProject retrieves all test runs for a project, newest first
func (r *VVRepository) ListRunsByProject(projectID string) ([]*vv.TestRun, error) {
	query := `
		SELECT id, project_id, name, description, baseline_id, status, started_at, completed_at, created_by, created_at, updated_at
		FROM test_runs
		WHERE project_id = $1
		ORDER BY started_at DESC
	`

	rows, err := r.db.Query(query, projectID)
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

// DeleteRun removes a test run; results cascade via foreign key
func (r *VVRepository) DeleteRun(id string) error {
	query := `DELETE FROM test_runs WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

// UpsertResult inserts or updates the result for (run_id, test_case_id).
// The stored row's id and created_at are scanned back onto the result.
func (r *VVRepository) UpsertResult(result *vv.TestResult) error {
	evidence := result.Evidence
	if evidence == nil {
		evidence = []string{}
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO test_results (id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (run_id, test_case_id) DO UPDATE SET
			test_case_version = EXCLUDED.test_case_version,
			status = EXCLUDED.status,
			notes = EXCLUDED.notes,
			evidence = EXCLUDED.evidence,
			executed_at = EXCLUDED.executed_at,
			executed_by = EXCLUDED.executed_by,
			executed_by_agent_run_id = EXCLUDED.executed_by_agent_run_id,
			updated_at = EXCLUDED.updated_at
		RETURNING id, created_at
	`

	return r.db.QueryRow(
		query,
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
	).Scan(&result.ID, &result.CreatedAt)
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

// ListResultsByRun retrieves all results recorded in a run
func (r *VVRepository) ListResultsByRun(runID string) ([]*vv.TestResult, error) {
	query := `
		SELECT id, run_id, test_case_id, test_case_version, status, notes, evidence, executed_at, executed_by, executed_by_agent_run_id, created_at, updated_at
		FROM test_results
		WHERE run_id = $1
		ORDER BY updated_at DESC
	`

	rows, err := r.db.Query(query, runID)
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
