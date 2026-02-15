package postgres

import (
	"database/sql"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/exports"
)

// ProjectInfoRepository implements exports.ProjectRepository
type ProjectInfoRepository struct {
	db *sql.DB
}

// NewProjectInfoRepository creates a new project info repository
func NewProjectInfoRepository(db *sql.DB) *ProjectInfoRepository {
	return &ProjectInfoRepository{db: db}
}

// FindByID retrieves project information by ID
func (r *ProjectInfoRepository) FindByID(id string) (*exports.ProjectInfo, error) {
	project := &exports.ProjectInfo{}

	query := `
		SELECT id, name, description
		FROM projects
		WHERE id = $1
	`

	err := r.db.QueryRow(query, id).Scan(
		&project.ID,
		&project.Name,
		&project.Description,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("project not found")
		}
		return nil, err
	}

	return project, nil
}
