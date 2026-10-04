package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// ProjectInfoRepository implements exports.ProjectRepository
type ProjectInfoRepository struct {
	db *sql.DB
}

// NewProjectInfoRepository creates a new project info repository
func NewProjectInfoRepository(db *sql.DB) *ProjectInfoRepository {
	return &ProjectInfoRepository{db: db}
}

// FindByID retrieves project information by ID: projects.ErrNotFound for an
// id no project has, a malformed one included (#379 bug 88).
func (r *ProjectInfoRepository) FindByID(id string) (*exports.ProjectInfo, error) {
	project := &exports.ProjectInfo{}

	query := `
		SELECT id, name, COALESCE(description, '')
		FROM projects
		WHERE id = $1
	`

	err := r.db.QueryRow(query, id).Scan(
		&project.ID,
		&project.Name,
		&project.Description,
	)

	if err != nil {
		if noRow(err) {
			return nil, projects.ErrNotFound
		}
		return nil, err
	}

	return project, nil
}
