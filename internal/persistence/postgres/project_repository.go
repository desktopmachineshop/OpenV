package postgres

import (
	"database/sql"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// ProjectRepository implements projects.Repository using PostgreSQL
type ProjectRepository struct {
	db *sql.DB
}

// NewProjectRepository creates a new project repository
func NewProjectRepository(db *sql.DB) projects.Repository {
	return &ProjectRepository{db: db}
}

// Create inserts a new project
func (r *ProjectRepository) Create(project *projects.Project) error {
	query := `
		INSERT INTO projects (id, org_id, name, description, created_at, updated_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(query, project.ID, project.OrgID, project.Name, project.Description, project.CreatedAt, project.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}
	return nil
}

// GetByID retrieves a project by ID
func (r *ProjectRepository) GetByID(id string) (*projects.Project, error) {
	query := `SELECT id, COALESCE(org_id::text, ''), name, description, created_at, updated_at FROM projects WHERE id = $1`
	row := r.db.QueryRow(query, id)

	project := &projects.Project{}
	err := row.Scan(&project.ID, &project.OrgID, &project.Name, &project.Description, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project not found")
		}
		return nil, fmt.Errorf("failed to retrieve project: %w", err)
	}

	return project, nil
}

// GetAll retrieves all projects
func (r *ProjectRepository) GetAll() ([]*projects.Project, error) {
	query := `SELECT id, COALESCE(org_id::text, ''), name, description, created_at, updated_at FROM projects ORDER BY created_at DESC`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query projects: %w", err)
	}
	defer rows.Close()

	projectList := make([]*projects.Project, 0)
	for rows.Next() {
		project := &projects.Project{}
		err := rows.Scan(&project.ID, &project.OrgID, &project.Name, &project.Description, &project.CreatedAt, &project.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projectList = append(projectList, project)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating projects: %w", err)
	}

	return projectList, nil
}

// Update updates an existing project
func (r *ProjectRepository) Update(project *projects.Project) error {
	query := `
		UPDATE projects 
		SET name = $1, description = $2, updated_at = $3
		WHERE id = $4
	`
	result, err := r.db.Exec(query, project.Name, project.Description, project.UpdatedAt, project.ID)
	if err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("project not found")
	}

	return nil
}

// Delete deletes a project
func (r *ProjectRepository) Delete(id string) error {
	query := `DELETE FROM projects WHERE id = $1`
	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("project not found")
	}

	return nil
}
