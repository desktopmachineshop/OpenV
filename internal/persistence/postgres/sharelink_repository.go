package postgres

import (
	"database/sql"
	"errors"
	"time"

	"github.com/openv/requirements-platform/internal/domain/sharelinks"
)

// ShareLinkRepository implements sharelinks.Repository (migration 0039).
type ShareLinkRepository struct{ db *sql.DB }

// NewShareLinkRepository creates a repository.
func NewShareLinkRepository(db *sql.DB) *ShareLinkRepository { return &ShareLinkRepository{db: db} }

const shareLinkColumns = `id, project_id, role, label, COALESCE(created_by::text, ''), created_at, expires_at, revoked_at`

func scanShareLink(row interface{ Scan(...interface{}) error }) (*sharelinks.Link, error) {
	l := &sharelinks.Link{}
	var createdBy string
	var expires, revoked sql.NullTime
	if err := row.Scan(&l.ID, &l.ProjectID, &l.Role, &l.Label, &createdBy, &l.CreatedAt, &expires, &revoked); err != nil {
		return nil, err
	}
	if createdBy != "" {
		l.CreatedBy = &createdBy
	}
	if expires.Valid {
		t := expires.Time
		l.ExpiresAt = &t
	}
	if revoked.Valid {
		t := revoked.Time
		l.RevokedAt = &t
	}
	return l, nil
}

// Create inserts a link with its token hash.
func (r *ShareLinkRepository) Create(link *sharelinks.Link, tokenHash string) error {
	createdBy := ""
	if link.CreatedBy != nil {
		createdBy = *link.CreatedBy
	}
	_, err := r.db.Exec(`INSERT INTO project_share_links (id, project_id, token_hash, role, label, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8)`,
		link.ID, link.ProjectID, tokenHash, link.Role, link.Label, createdBy, link.CreatedAt, link.ExpiresAt)
	return err
}

// Get reads one link by id.
func (r *ShareLinkRepository) Get(id string) (*sharelinks.Link, error) {
	l, err := scanShareLink(r.db.QueryRow(`SELECT `+shareLinkColumns+` FROM project_share_links WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sharelinks.ErrNotFound
	}
	return l, err
}

// ListByProject lists a project's links, newest first, revoked ones included
// so an owner sees what was handed out.
func (r *ShareLinkRepository) ListByProject(projectID string) ([]*sharelinks.Link, error) {
	rows, err := r.db.Query(`SELECT `+shareLinkColumns+` FROM project_share_links WHERE project_id = $1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*sharelinks.Link{}
	for rows.Next() {
		l, err := scanShareLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// FindByTokenHash reads the link a token hash names.
func (r *ShareLinkRepository) FindByTokenHash(hash string) (*sharelinks.Link, error) {
	l, err := scanShareLink(r.db.QueryRow(`SELECT `+shareLinkColumns+` FROM project_share_links WHERE token_hash = $1`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sharelinks.ErrNotFound
	}
	return l, err
}

// Revoke stamps the link revoked; a second revoke is a no-op.
func (r *ShareLinkRepository) Revoke(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE project_share_links SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, id, at)
	return err
}
