package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/products"
)

// ProductProfileRepository implements the products.Repository interface
type ProductProfileRepository struct {
	db *sql.DB
}

// NewProductProfileRepository creates a new product profile repository
func NewProductProfileRepository(db *sql.DB) *ProductProfileRepository {
	return &ProductProfileRepository{db: db}
}

// Get retrieves a product profile by project ID; returns nil, nil when none exists
func (r *ProductProfileRepository) Get(projectID string) (*products.ProductProfile, error) {
	query := `
		SELECT project_id, vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at
		FROM product_profiles
		WHERE project_id = $1
	`

	profile := &products.ProductProfile{}
	var constraintsJSON, metricsJSON, settingsJSON []byte

	err := r.db.QueryRow(query, projectID).Scan(
		&profile.ProjectID,
		&profile.Vision,
		&profile.ProblemStatement,
		&profile.TargetUsers,
		&constraintsJSON,
		&metricsJSON,
		&settingsJSON,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	profile.Constraints = []map[string]interface{}{}
	if len(constraintsJSON) > 0 {
		if err := json.Unmarshal(constraintsJSON, &profile.Constraints); err != nil {
			return nil, err
		}
	}
	profile.SuccessMetrics = []map[string]interface{}{}
	if len(metricsJSON) > 0 {
		if err := json.Unmarshal(metricsJSON, &profile.SuccessMetrics); err != nil {
			return nil, err
		}
	}
	profile.Settings = map[string]interface{}{}
	if len(settingsJSON) > 0 {
		if err := json.Unmarshal(settingsJSON, &profile.Settings); err != nil {
			return nil, err
		}
	}

	return profile, nil
}

// Upsert inserts or updates a product profile
func (r *ProductProfileRepository) Upsert(p *products.ProductProfile) error {
	constraints := p.Constraints
	if constraints == nil {
		constraints = []map[string]interface{}{}
	}
	metrics := p.SuccessMetrics
	if metrics == nil {
		metrics = []map[string]interface{}{}
	}
	settings := p.Settings
	if settings == nil {
		settings = map[string]interface{}{}
	}

	constraintsJSON, err := json.Marshal(constraints)
	if err != nil {
		return err
	}
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO product_profiles (project_id, vision, problem_statement, target_users, constraints, success_metrics, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (project_id) DO UPDATE SET
			vision = EXCLUDED.vision,
			problem_statement = EXCLUDED.problem_statement,
			target_users = EXCLUDED.target_users,
			constraints = EXCLUDED.constraints,
			success_metrics = EXCLUDED.success_metrics,
			settings = EXCLUDED.settings,
			updated_at = EXCLUDED.updated_at
	`

	_, err = r.db.Exec(
		query,
		p.ProjectID,
		p.Vision,
		p.ProblemStatement,
		p.TargetUsers,
		constraintsJSON,
		metricsJSON,
		settingsJSON,
		p.CreatedAt,
		p.UpdatedAt,
	)

	return err
}
