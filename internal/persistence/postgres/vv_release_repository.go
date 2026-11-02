package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
	"github.com/openv/requirements-platform/internal/domain/targetreleases"
)

// TargetReleaseRepository implements targetreleases.Repository.
type TargetReleaseRepository struct {
	db *sql.DB
}

// NewTargetReleaseRepository creates the repository.
func NewTargetReleaseRepository(db *sql.DB) *TargetReleaseRepository {
	return &TargetReleaseRepository{db: db}
}

const targetReleaseColumns = `id, project_id, name, description, to_char(target_date, 'YYYY-MM-DD'), status, position, released_at, created_at, updated_at`

func scanTargetRelease(s rowScanner) (*targetreleases.Release, error) {
	r := &targetreleases.Release{}
	var date sql.NullString
	var released sql.NullTime
	if err := s.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Description, &date, &r.Status, &r.Position,
		&released, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	if date.Valid {
		r.TargetDate = &date.String
	}
	if released.Valid {
		t := released.Time.UTC()
		r.ReleasedAt = &t
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r, nil
}

func utcPtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// nameConflict maps the unique (project, name) index to ErrNameTaken, for
// the race two creates of one name run.
func nameConflict(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return targetreleases.ErrNameTaken
	}
	return err
}

// Create inserts a release.
func (r *TargetReleaseRepository) Create(rel *targetreleases.Release) error {
	_, err := r.db.Exec(`
		INSERT INTO target_releases (id, project_id, name, description, target_date, status, position, released_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rel.ID, rel.ProjectID, rel.Name, rel.Description, nullableString(rel.TargetDate), rel.Status,
		rel.Position, utcPtr(rel.ReleasedAt), rel.CreatedAt.UTC(), rel.UpdatedAt.UTC())
	return nameConflict(err)
}

// Update writes a release's editable fields.
func (r *TargetReleaseRepository) Update(rel *targetreleases.Release) error {
	res, err := r.db.Exec(`
		UPDATE target_releases
		SET name = $2, description = $3, target_date = $4, status = $5, released_at = $6, updated_at = $7
		WHERE id = $1`,
		rel.ID, rel.Name, rel.Description, nullableString(rel.TargetDate), rel.Status,
		utcPtr(rel.ReleasedAt), rel.UpdatedAt.UTC())
	if err != nil {
		return nameConflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return targetreleases.ErrNotFound
	}
	return nil
}

// Delete removes a release.
func (r *TargetReleaseRepository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM target_releases WHERE id = $1`, id)
	if malformedID(err) {
		return targetreleases.ErrNotFound
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return targetreleases.ErrNotFound
	}
	return nil
}

// Get returns one release, ErrNotFound when there is none.
func (r *TargetReleaseRepository) Get(id string) (*targetreleases.Release, error) {
	rel, err := scanTargetRelease(r.db.QueryRow(`SELECT `+targetReleaseColumns+` FROM target_releases WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) || malformedID(err) {
		return nil, targetreleases.ErrNotFound
	}
	return rel, err
}

// ListByProject returns the project's releases by position, then creation.
func (r *TargetReleaseRepository) ListByProject(projectID string) ([]*targetreleases.Release, error) {
	rows, err := r.db.Query(`SELECT `+targetReleaseColumns+` FROM target_releases
		WHERE project_id = $1 ORDER BY position, created_at, id`, projectID)
	if malformedID(err) {
		return []*targetreleases.Release{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*targetreleases.Release{}
	for rows.Next() {
		rel, err := scanTargetRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

// SetPositions renumbers the project's releases in the order of ids.
func (r *TargetReleaseRepository) SetPositions(projectID string, ids []string) error {
	return withTx(context.Background(), r.db, nil, func(tx *sql.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(`UPDATE target_releases SET position = $3 WHERE id = $1 AND project_id = $2`,
				id, projectID, i); err != nil {
				return err
			}
		}
		return nil
	})
}

// CountTargeting counts the project's current artifacts whose targeting
// attributes name the release: the targetreleases.TargetCounter.
func (r *TargetReleaseRepository) CountTargeting(projectID, releaseID string) (int, error) {
	var n int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM artifacts
		WHERE project_id = $1 AND valid_to IS NULL
		  AND (attributes->>'`+targetreleases.AttrFrom+`' = $2 OR attributes->>'`+targetreleases.AttrUntil+`' = $2)`,
		projectID, releaseID).Scan(&n)
	if malformedID(err) {
		return 0, nil
	}
	return n, err
}
