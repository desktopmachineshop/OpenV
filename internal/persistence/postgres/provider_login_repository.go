package postgres

import (
	"database/sql"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// ProviderLoginRepository implements providers.LoginRepository.
type ProviderLoginRepository struct {
	db *sql.DB
}

// NewProviderLoginRepository creates a new login request repository.
func NewProviderLoginRepository(db *sql.DB) *ProviderLoginRepository {
	return &ProviderLoginRepository{db: db}
}

const loginColumns = `id, COALESCE(org_id::text, ''), provider, target, status, auth_url, code, detail, requested_by, created_at, updated_at`

func scanLogin(row interface{ Scan(...interface{}) error }) (*providers.LoginRequest, error) {
	l := new(providers.LoginRequest)
	var requestedBy sql.NullString
	err := row.Scan(&l.ID, &l.OrgID, &l.Provider, &l.Target, &l.Status, &l.AuthURL, &l.Code, &l.Detail, &requestedBy, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if requestedBy.Valid {
		v := requestedBy.String
		l.RequestedBy = &v
	}
	return l, nil
}

// SaveLogin inserts a login request.
func (r *ProviderLoginRepository) SaveLogin(l *providers.LoginRequest) error {
	_, err := r.db.Exec(`
		INSERT INTO provider_logins (id, org_id, provider, target, status, auth_url, code, detail, requested_by, created_at, updated_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, l.ID, l.OrgID, l.Provider, l.Target, l.Status, l.AuthURL, l.Code, l.Detail, l.RequestedBy, l.CreatedAt, l.UpdatedAt)
	return err
}

// UpdateLogin rewrites a login request's mutable fields.
func (r *ProviderLoginRepository) UpdateLogin(l *providers.LoginRequest) error {
	_, err := r.db.Exec(`
		UPDATE provider_logins SET status = $2, auth_url = $3, code = $4, detail = $5, updated_at = $6
		WHERE id = $1
	`, l.ID, l.Status, l.AuthURL, l.Code, l.Detail, l.UpdatedAt)
	return err
}

// FindLoginByID returns a login request, or nil.
func (r *ProviderLoginRepository) FindLoginByID(id string) (*providers.LoginRequest, error) {
	l, err := scanLogin(r.db.QueryRow(`SELECT `+loginColumns+` FROM provider_logins WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

// FindActiveLogin returns the most recent in-flight request for a provider,
// or nil. User-targeted requests are scoped to the requesting user;
// workspace-targeted requests are org-wide.
func (r *ProviderLoginRepository) FindActiveLogin(orgID, provider, target string, userID *string) (*providers.LoginRequest, error) {
	query := `
		SELECT ` + loginColumns + ` FROM provider_logins
		WHERE org_id = NULLIF($1, '')::uuid AND provider = $2 AND target = $3
		  AND status IN ('pending', 'claimed', 'url_ready', 'awaiting_code')`
	args := []interface{}{orgID, provider, target}
	if target == providers.LoginTargetUser {
		uid := ""
		if userID != nil {
			uid = *userID
		}
		query += ` AND requested_by = NULLIF($4, '')::uuid`
		args = append(args, uid)
	}
	query += ` ORDER BY created_at DESC LIMIT 1`
	l, err := scanLogin(r.db.QueryRow(query, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

// ClaimPendingLogin atomically claims the org's oldest pending request the
// worker may execute: workspace-targeted requests for any worker, plus
// user-targeted requests belonging to the personal runner's owner.
func (r *ProviderLoginRepository) ClaimPendingLogin(orgID, workerUserID string) (*providers.LoginRequest, error) {
	row := r.db.QueryRow(`
		UPDATE provider_logins SET status = 'claimed', updated_at = NOW()
		WHERE id = (
			SELECT id FROM provider_logins
			WHERE org_id = NULLIF($1, '')::uuid AND status = 'pending'
			  AND (
				target = 'workspace'
				OR (target = 'user' AND requested_by = NULLIF($2, '')::uuid)
			  )
			ORDER BY created_at LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+loginColumns, orgID, workerUserID)
	l, err := scanLogin(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}
