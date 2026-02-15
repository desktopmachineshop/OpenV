package postgres

import (
	"database/sql"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/baselines"
)

// BaselineRepository implements baselines.Repository for Postgres.
type BaselineRepository struct {
	db *sql.DB
}

// NewBaselineRepository creates a new baseline repository.
func NewBaselineRepository(db *sql.DB) *BaselineRepository {
	return &BaselineRepository{db: db}
}

// Create inserts a new baseline.
func (r *BaselineRepository) Create(baseline *baselines.Baseline) error {
	query := `
		INSERT INTO baselines (id, project_id, name, snapshot, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := r.db.Exec(query,
		baseline.ID,
		baseline.ProjectID,
		baseline.Name,
		baseline.Snapshot,
		baseline.CreatedAt,
	)

	return err
}

// ListByProjectID returns baselines for a project.
func (r *BaselineRepository) ListByProjectID(projectID string) ([]*baselines.Baseline, error) {
	query := `
		SELECT id, project_id, name, created_at
		FROM baselines
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*baselines.Baseline
	for rows.Next() {
		item := &baselines.Baseline{}
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Name, &item.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, item)
	}

	return results, rows.Err()
}

// GetByID returns a baseline by ID.
func (r *BaselineRepository) GetByID(id string) (*baselines.Baseline, error) {
	query := `
		SELECT id, project_id, name, snapshot, created_at
		FROM baselines
		WHERE id = $1
	`

	baseline := &baselines.Baseline{}
	if err := r.db.QueryRow(query, id).Scan(
		&baseline.ID,
		&baseline.ProjectID,
		&baseline.Name,
		&baseline.Snapshot,
		&baseline.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("baseline not found")
		}
		return nil, err
	}

	return baseline, nil
}

// Delete removes a baseline by ID.
func (r *BaselineRepository) Delete(id string) error {
	query := `
		DELETE FROM baselines
		WHERE id = $1
	`

	_, err := r.db.Exec(query, id)
	return err
}
