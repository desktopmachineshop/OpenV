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

// cols is the column list every read shares. The description column takes
// NULL, which no write here stores; it reads as "" rather than failing the
// scan, and with it every list the row falls in (#379 bug 87).
const cols = "SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at"

// NewProjectRepository creates a new project repository
func NewProjectRepository(db *sql.DB) projects.Repository {
	return &ProjectRepository{db: db}
}

// Create inserts a new project
func (r *ProjectRepository) Create(project *projects.Project) error {
	query := `
		INSERT INTO projects (id, org_id, name, description, agent_auth, parent_project_id, created_at, updated_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8)
	`
	_, err := r.db.Exec(query, project.ID, project.OrgID, project.Name, project.Description, project.AgentAuth, project.ParentProjectID, project.CreatedAt, project.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}
	return nil
}

// GetByID retrieves a project by ID: projects.ErrNotFound for an id no
// project has, a malformed one included (#379 bug 88).
func (r *ProjectRepository) GetByID(id string) (*projects.Project, error) {
	query := `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at FROM projects WHERE id = $1`
	row := r.db.QueryRow(query, id)

	project := &projects.Project{}
	err := row.Scan(&project.ID, &project.OrgID, &project.Name, &project.Description, &project.AgentAuth, &project.ParentProjectID, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		if noRow(err) {
			return nil, projects.ErrNotFound
		}
		return nil, fmt.Errorf("failed to retrieve project: %w", err)
	}

	return project, nil
}

// GetAll retrieves all projects
func (r *ProjectRepository) GetAll() ([]*projects.Project, error) {
	query := `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at FROM projects ORDER BY created_at DESC`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query projects: %w", err)
	}
	defer rows.Close()

	projectList := make([]*projects.Project, 0)
	for rows.Next() {
		project := &projects.Project{}
		err := rows.Scan(&project.ID, &project.OrgID, &project.Name, &project.Description, &project.AgentAuth, &project.ParentProjectID, &project.CreatedAt, &project.UpdatedAt)
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

// ListByOrg retrieves the projects in one org. It fails closed: an empty
// orgID becomes org_id = NULL, which matches no rows, so a caller without a
// resolved active workspace gets an empty list instead of every tenant's
// projects. Mirrors EventRepository.List's org predicate. A malformed orgID
// is a workspace no row has, which lists [] (#379 bug 88).
func (r *ProjectRepository) ListByOrg(orgID string) ([]*projects.Project, error) {
	query := `SELECT id, COALESCE(org_id::text, ''), name, COALESCE(description, ''), agent_auth, COALESCE(parent_project_id::text, ''), created_at, updated_at
		FROM projects WHERE org_id = NULLIF($1, '')::uuid ORDER BY created_at DESC`
	rows, err := r.db.Query(query, orgID)
	if malformedID(err) {
		return make([]*projects.Project, 0), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query projects: %w", err)
	}
	defer rows.Close()

	projectList := make([]*projects.Project, 0)
	for rows.Next() {
		project := &projects.Project{}
		err := rows.Scan(&project.ID, &project.OrgID, &project.Name, &project.Description, &project.AgentAuth, &project.ParentProjectID, &project.CreatedAt, &project.UpdatedAt)
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

// ListChildren returns the projects filed under one parent, oldest first so
// a settings page lists them in the order they were attached. A malformed id
// is a parent no row has, which lists [] (#379 bug 88).
func (r *ProjectRepository) ListChildren(id string) ([]*projects.Project, error) {
	query := cols + ` FROM projects WHERE parent_project_id = NULLIF($1, '')::uuid ORDER BY created_at ASC`
	rows, err := r.db.Query(query, id)
	if malformedID(err) {
		return make([]*projects.Project, 0), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query child projects: %w", err)
	}
	defer rows.Close()

	projectList := make([]*projects.Project, 0)
	for rows.Next() {
		project := &projects.Project{}
		if err := rows.Scan(&project.ID, &project.OrgID, &project.Name, &project.Description, &project.AgentAuth, &project.ParentProjectID, &project.CreatedAt, &project.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan project: %w", err)
		}
		projectList = append(projectList, project)
	}
	return projectList, rows.Err()
}

// Update updates an existing project: projects.ErrNotFound for an id no
// project has, a malformed one included (#379 bug 88: Postgres's refusal of
// a malformed id was handed back).
func (r *ProjectRepository) Update(project *projects.Project) error {
	if !isUUID(project.ID) {
		return projects.ErrNotFound
	}
	query := `
		UPDATE projects
		SET name = $1, description = $2, agent_auth = $3, parent_project_id = NULLIF($4, '')::uuid, updated_at = $5
		WHERE id = $6
	`
	result, err := r.db.Exec(query, project.Name, project.Description, project.AgentAuth, project.ParentProjectID, project.UpdatedAt, project.ID)
	if err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return projects.ErrNotFound
	}

	return nil
}

// Delete deletes a project and everything that belongs to it alone, in one
// transaction (#379 bug 86): every version of its artifacts, with their
// embeddings, chatter, attachments (the rows; the stored files stay where
// they are), figure counters and links in either direction, a link to or
// from another project's artifact included, with the links' version
// records; then the project row, whose foreign keys take its reference
// counters, work items, crews, test runs, interviews, guided sessions,
// attribute definitions, automations, agent proposals, baselines, evidence
// bundles, product profile, repository connections, share links,
// memberships and team grants with it (migration 0053). Its agent runs stay,
// for the workspace's usage, with no project, and its child projects stay,
// detached to the top level. The artifacts' rows are keyed by the artifact
// id, which no foreign key can reference (an artifact's key is its id and
// version), so they are deleted here, before the project. Locking the
// project row first makes a concurrent write that references the project
// wait for the delete and then fail on the foreign key. An id no project
// has, a malformed one included, is projects.ErrNotFound (#379 bug 88).
func (r *ProjectRepository) Delete(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	defer tx.Rollback()

	var locked string
	if err := tx.QueryRow(`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, id).Scan(&locked); err != nil {
		if noRow(err) {
			return projects.ErrNotFound
		}
		return fmt.Errorf("failed to delete project: %w", err)
	}
	// artifact_embeddings exists only where the vector migration ran.
	var embeddings sql.NullString
	if err := tx.QueryRow(`SELECT to_regclass('artifact_embeddings')::text`).Scan(&embeddings); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	stmts := deleteProjectStatements
	if embeddings.Valid {
		stmts = append([]string{`DELETE FROM artifact_embeddings WHERE artifact_id IN (` + projectArtifacts + `)`}, stmts...)
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt, id); err != nil {
			return fmt.Errorf("failed to delete project: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}

// projectArtifacts selects the ids of a project's artifacts, the project id
// being $1, for deleteProjectStatements.
const projectArtifacts = `SELECT DISTINCT id FROM artifacts WHERE project_id = $1`

// deleteProjectStatements is Delete's list, sent in this order after the
// artifact_embeddings statement, each with the project id as $1: the rows
// keyed by the project's artifacts, then the project, which the foreign keys
// of migration 0053 and earlier carry to the rest.
var deleteProjectStatements = []string{
	`DELETE FROM chatter WHERE artifact_id IN (` + projectArtifacts + `)`,
	`DELETE FROM attachments WHERE artifact_id IN (` + projectArtifacts + `)`, // versions cascade
	`DELETE FROM attachment_figure_counters WHERE artifact_id IN (` + projectArtifacts + `)`,
	`DELETE FROM link_artifacts WHERE artifact_id IN (` + projectArtifacts + `)
		OR link_id IN (SELECT id FROM links WHERE from_id IN (` + projectArtifacts + `) OR to_id IN (` + projectArtifacts + `))`,
	`DELETE FROM links WHERE from_id IN (` + projectArtifacts + `) OR to_id IN (` + projectArtifacts + `)`,
	`DELETE FROM projects WHERE id = $1`,
}
