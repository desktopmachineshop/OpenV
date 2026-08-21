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

const loginColumns = `id, COALESCE(org_id::text, ''), provider, status, auth_url, code, detail, requested_by, created_at, updated_at`

func scanLogin(row interface{ Scan(...interface{}) error }) (*providers.LoginRequest, error) {
	l := new(providers.LoginRequest)
	var requestedBy sql.NullString
	err := row.Scan(&l.ID, &l.OrgID, &l.Provider, &l.Status, &l.AuthURL, &l.Code, &l.Detail, &requestedBy, &l.CreatedAt, &l.UpdatedAt)
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
		INSERT INTO provider_logins (id, org_id, provider, status, auth_url, code, detail, requested_by, created_at, updated_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10)
	`, l.ID, l.OrgID, l.Provider, l.Status, l.AuthURL, l.Code, l.Detail, l.RequestedBy, l.CreatedAt, l.UpdatedAt)
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

// FindActiveLoginByProvider returns an org's most recent in-flight request
// for a provider, or nil.
func (r *ProviderLoginRepository) FindActiveLoginByProvider(orgID, provider string) (*providers.LoginRequest, error) {
	l, err := scanLogin(r.db.QueryRow(`
		SELECT `+loginColumns+` FROM provider_logins
		WHERE org_id = NULLIF($1, '')::uuid AND provider = $2
		  AND status IN ('pending', 'claimed', 'url_ready', 'awaiting_code')
		ORDER BY created_at DESC LIMIT 1
	`, orgID, provider))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

// ClaimPendingLogin atomically claims the org's oldest pending request.
func (r *ProviderLoginRepository) ClaimPendingLogin(orgID string) (*providers.LoginRequest, error) {
	row := r.db.QueryRow(`
		UPDATE provider_logins SET status = 'claimed', updated_at = NOW()
		WHERE id = (
			SELECT id FROM provider_logins
			WHERE org_id = NULLIF($1, '')::uuid AND status = 'pending'
			ORDER BY created_at LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+loginColumns, orgID)
	l, err := scanLogin(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}
