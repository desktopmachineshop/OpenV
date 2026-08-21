package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// RepoConnectionRepository implements the repoconns.Repository interface.
type RepoConnectionRepository struct {
	db *sql.DB
}

// NewRepoConnectionRepository creates a new repo connection repository.
func NewRepoConnectionRepository(db *sql.DB) *RepoConnectionRepository {
	return &RepoConnectionRepository{db: db}
}

// Save inserts a new repo connection.
func (r *RepoConnectionRepository) Save(c *repoconns.RepoConnection) error {
	_, err := r.db.Exec(`
		INSERT INTO repo_connections (
			id, project_id, name, remote_url, local_path, default_branch,
			credential_strategy, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		c.ID, c.ProjectID, c.Name, c.RemoteURL, c.LocalPath, c.DefaultBranch,
		c.CredentialStrategy, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// Update rewrites a repo connection's mutable columns.
func (r *RepoConnectionRepository) Update(c *repoconns.RepoConnection) error {
	_, err := r.db.Exec(`
		UPDATE repo_connections SET
			name = $2, remote_url = $3, local_path = $4, default_branch = $5,
			credential_strategy = $6, updated_at = $7
		WHERE id = $1
	`,
		c.ID, c.Name, c.RemoteURL, c.LocalPath, c.DefaultBranch,
		c.CredentialStrategy, c.UpdatedAt,
	)
	return err
}

// FindByID retrieves a repo connection, returning (nil, nil) when absent.
func (r *RepoConnectionRepository) FindByID(id string) (*repoconns.RepoConnection, error) {
	row := r.db.QueryRow(`
		SELECT id, project_id, name, remote_url, local_path, default_branch,
			credential_strategy, created_at, updated_at
		FROM repo_connections
		WHERE id = $1
	`, id)

	c := new(repoconns.RepoConnection)
	err := row.Scan(
		&c.ID, &c.ProjectID, &c.Name, &c.RemoteURL, &c.LocalPath, &c.DefaultBranch,
		&c.CredentialStrategy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListByProject returns a project's repo connections.
func (r *RepoConnectionRepository) ListByProject(projectID string) ([]*repoconns.RepoConnection, error) {
	rows, err := r.db.Query(`
		SELECT id, project_id, name, remote_url, local_path, default_branch,
			credential_strategy, created_at, updated_at
		FROM repo_connections
		WHERE project_id = $1
		ORDER BY created_at
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*repoconns.RepoConnection
	for rows.Next() {
		c := new(repoconns.RepoConnection)
		err := rows.Scan(
			&c.ID, &c.ProjectID, &c.Name, &c.RemoteURL, &c.LocalPath, &c.DefaultBranch,
			&c.CredentialStrategy, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Delete removes a repo connection.
func (r *RepoConnectionRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM repo_connections WHERE id = $1`, id)
	return err
}

// SetUserPath stores a user's local-path override for a connection; an empty
// path removes the override.
func (r *RepoConnectionRepository) SetUserPath(userID, connID, path string) error {
	if path == "" {
		_, err := r.db.Exec(
			`DELETE FROM user_repo_paths WHERE user_id = $1 AND repo_connection_id = $2`,
			userID, connID,
		)
		return err
	}
	_, err := r.db.Exec(`
		INSERT INTO user_repo_paths (user_id, repo_connection_id, local_path, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, repo_connection_id) DO UPDATE SET
			local_path = EXCLUDED.local_path,
			updated_at = NOW()
	`, userID, connID, path)
	return err
}

// UserPaths returns a user's overrides for a project's connections, keyed by
// connection id.
func (r *RepoConnectionRepository) UserPaths(userID, projectID string) (map[string]string, error) {
	rows, err := r.db.Query(`
		SELECT p.repo_connection_id, p.local_path
		FROM user_repo_paths p
		JOIN repo_connections c ON c.id = p.repo_connection_id
		WHERE p.user_id = $1 AND c.project_id = $2
	`, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]string{}
	for rows.Next() {
		var connID, path string
		if err := rows.Scan(&connID, &path); err != nil {
			return nil, err
		}
		result[connID] = path
	}
	return result, rows.Err()
}
