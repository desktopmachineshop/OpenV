package postgres

import (
	"database/sql"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/templates"
)

// TemplateRepository implements templates.Repository for Postgres.
type TemplateRepository struct {
	db *sql.DB
}

// NewTemplateRepository creates a new template repository.
func NewTemplateRepository(db *sql.DB) *TemplateRepository {
	return &TemplateRepository{db: db}
}

// Create inserts a new template.
func (r *TemplateRepository) Create(template *templates.Template) error {
	query := `
		INSERT INTO templates (id, template_key, name, description, snapshot, is_default, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Exec(query,
		template.ID,
		template.Key,
		template.Name,
		template.Description,
		template.Snapshot,
		template.IsDefault,
		template.CreatedAt,
	)

	return err
}

// List returns all templates.
func (r *TemplateRepository) List() ([]*templates.Template, error) {
	query := `
		SELECT id, template_key, name, description, is_default, created_at
		FROM templates
		ORDER BY is_default DESC, created_at DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*templates.Template
	for rows.Next() {
		item := &templates.Template{}
		if err := rows.Scan(
			&item.ID,
			&item.Key,
			&item.Name,
			&item.Description,
			&item.IsDefault,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, item)
	}

	return results, rows.Err()
}

// GetByID returns a template by ID.
func (r *TemplateRepository) GetByID(id string) (*templates.Template, error) {
	query := `
		SELECT id, template_key, name, description, snapshot, is_default, created_at
		FROM templates
		WHERE id = $1
	`

	item := &templates.Template{}
	if err := r.db.QueryRow(query, id).Scan(
		&item.ID,
		&item.Key,
		&item.Name,
		&item.Description,
		&item.Snapshot,
		&item.IsDefault,
		&item.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("template not found")
		}
		return nil, err
	}

	return item, nil
}

// GetByKey returns a template by key.
func (r *TemplateRepository) GetByKey(key string) (*templates.Template, error) {
	query := `
		SELECT id, template_key, name, description, snapshot, is_default, created_at
		FROM templates
		WHERE template_key = $1
	`

	item := &templates.Template{}
	if err := r.db.QueryRow(query, key).Scan(
		&item.ID,
		&item.Key,
		&item.Name,
		&item.Description,
		&item.Snapshot,
		&item.IsDefault,
		&item.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("template not found")
		}
		return nil, err
	}

	return item, nil
}
