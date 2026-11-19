package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/projects"
)

// ProjectCoverRepository implements projects.CoverRepository: one row of
// project_covers per project that has a cover photo (migration 0091).
type ProjectCoverRepository struct {
	db *sql.DB
}

// NewProjectCoverRepository builds the cover photo repository.
func NewProjectCoverRepository(db *sql.DB) *ProjectCoverRepository {
	return &ProjectCoverRepository{db: db}
}

// GetCover answers the project's cover, projects.ErrNoCover when it has none
// or the id is no project's, a malformed one included.
func (r *ProjectCoverRepository) GetCover(projectID string) (*projects.Cover, error) {
	if !isUUID(projectID) {
		return nil, projects.ErrNoCover
	}
	c := &projects.Cover{ProjectID: projectID}
	err := r.db.QueryRow(`SELECT mime, data, updated_at FROM project_covers WHERE project_id = $1`, projectID).
		Scan(&c.Mime, &c.Data, &c.UpdatedAt)
	if noRow(err) {
		return nil, projects.ErrNoCover
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read project cover: %w", err)
	}
	c.UpdatedAt = c.UpdatedAt.UTC()
	return c, nil
}

// SetCover stores the project's cover, replacing any it had, and answers
// when; projects.ErrNotFound for an id no project has. The insert selects
// the project's row, so a project deleted meanwhile stores nothing.
func (r *ProjectCoverRepository) SetCover(projectID, mime string, data []byte) (time.Time, error) {
	if !isUUID(projectID) {
		return time.Time{}, projects.ErrNotFound
	}
	var at time.Time
	err := r.db.QueryRow(`
		INSERT INTO project_covers (project_id, mime, data, updated_at)
		SELECT id, $2, $3, NOW() FROM projects WHERE id = $1
		ON CONFLICT (project_id) DO UPDATE SET mime = EXCLUDED.mime, data = EXCLUDED.data, updated_at = EXCLUDED.updated_at
		RETURNING updated_at`, projectID, mime, data).Scan(&at)
	if noRow(err) {
		return time.Time{}, projects.ErrNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to store project cover: %w", err)
	}
	return at.UTC(), nil
}

// DeleteCover removes the project's cover; none, or a malformed id, is not
// an error.
func (r *ProjectCoverRepository) DeleteCover(projectID string) error {
	if !isUUID(projectID) {
		return nil
	}
	if _, err := r.db.Exec(`DELETE FROM project_covers WHERE project_id = $1`, projectID); err != nil {
		return fmt.Errorf("failed to remove project cover: %w", err)
	}
	return nil
}

// CoverStamps answers when each of the projects that has a cover stored
// it. Malformed ids are skipped: no project has them.
func (r *ProjectCoverRepository) CoverStamps(projectIDs []string) (map[string]time.Time, error) {
	ids := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		if isUUID(id) {
			ids = append(ids, id)
		}
	}
	out := map[string]time.Time{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(`SELECT project_id::text, updated_at FROM project_covers WHERE project_id = ANY($1::uuid[])`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("failed to read project covers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("failed to read project covers: %w", err)
		}
		out[id] = at.UTC()
	}
	return out, rows.Err()
}
