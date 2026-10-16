package postgres

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
)

// ConnectedAppRepository implements connectedapps.Repository over
// oauth_clients, connected_apps, oauth_codes and oauth_tokens (migration
// 0062).
type ConnectedAppRepository struct {
	db *sql.DB
}

// NewConnectedAppRepository creates the repository.
func NewConnectedAppRepository(db *sql.DB) *ConnectedAppRepository {
	return &ConnectedAppRepository{db: db}
}

// CreateClient records a registered client.
func (r *ConnectedAppRepository) CreateClient(c *connectedapps.Client) error {
	_, err := r.db.Exec(`
		INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES ($1, $2, $3, $4)
	`, c.ID, c.Name, pq.Array(c.RedirectURIs), c.CreatedAt)
	return err
}

// GetClient returns a client, nil when none has the id.
func (r *ConnectedAppRepository) GetClient(id string) (*connectedapps.Client, error) {
	c := new(connectedapps.Client)
	err := r.db.QueryRow(`
		SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = $1
	`, id).Scan(&c.ID, &c.Name, pq.Array(&c.RedirectURIs), &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

const connectedAppColumns = `g.id, g.user_id, g.org_id, o.name, g.client_id, c.name, g.scope, g.created_at, g.last_used_at, g.revoked_at`

const connectedAppFrom = `connected_apps g
	JOIN oauth_clients c ON c.id = g.client_id
	JOIN organizations o ON o.id = g.org_id`

func scanConnectedApp(row interface{ Scan(...interface{}) error }) (*connectedapps.Grant, error) {
	g := new(connectedapps.Grant)
	var lastUsed, revoked sql.NullTime
	if err := row.Scan(&g.ID, &g.UserID, &g.OrgID, &g.OrgName, &g.ClientID, &g.ClientName, &g.Scope, &g.CreatedAt, &lastUsed, &revoked); err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		t := lastUsed.Time
		g.LastUsedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time
		g.RevokedAt = &t
	}
	return g, nil
}

// SaveGrant records consent: the live grant for the member, client and
// workspace takes the scope, or a new one is made. The partial unique index
// keeps one live grant per triple when two consents race.
func (r *ConnectedAppRepository) SaveGrant(userID, orgID, clientID, scope string, now time.Time) (*connectedapps.Grant, error) {
	var id string
	err := r.db.QueryRow(`
		INSERT INTO connected_apps (id, user_id, org_id, client_id, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (user_id, org_id, client_id) WHERE revoked_at IS NULL
		DO UPDATE SET scope = EXCLUDED.scope, updated_at = EXCLUDED.updated_at
		RETURNING id
	`, uuid.NewString(), userID, orgID, clientID, scope, now).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetGrant(id)
}

// GetGrant returns a live grant, nil when it does not exist or was revoked.
func (r *ConnectedAppRepository) GetGrant(id string) (*connectedapps.Grant, error) {
	g, err := scanConnectedApp(r.db.QueryRow(`SELECT `+connectedAppColumns+` FROM `+connectedAppFrom+`
		WHERE g.id = $1 AND g.revoked_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// ListGrants returns the member's live grants, newest first.
func (r *ConnectedAppRepository) ListGrants(userID string) ([]connectedapps.Grant, error) {
	rows, err := r.db.Query(`SELECT `+connectedAppColumns+` FROM `+connectedAppFrom+`
		WHERE g.user_id = $1 AND g.revoked_at IS NULL
		ORDER BY g.created_at DESC, g.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []connectedapps.Grant{}
	for rows.Next() {
		g, err := scanConnectedApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// RevokeGrant marks the grant revoked and deletes its codes and tokens, in
// one transaction so no token outlives the revoke.
func (r *ConnectedAppRepository) RevokeGrant(id string, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE connected_apps SET revoked_at = $2, updated_at = $2 WHERE id = $1 AND revoked_at IS NULL`, id, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM oauth_tokens WHERE grant_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM oauth_codes WHERE grant_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateCode stores an authorization code's hash.
func (r *ConnectedAppRepository) CreateCode(c *connectedapps.Code) error {
	_, err := r.db.Exec(`
		INSERT INTO oauth_codes (code_hash, grant_id, client_id, redirect_uri, code_challenge, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, c.Hash, c.GrantID, c.ClientID, c.RedirectURI, c.CodeChallenge, c.ExpiresAt)
	return err
}

// ConsumeCode deletes a code and returns it, nil when none has the hash: a
// code is spent by its first use, right or wrong. Codes left to expire are
// cleared on the way.
func (r *ConnectedAppRepository) ConsumeCode(hash string) (*connectedapps.Code, error) {
	c := new(connectedapps.Code)
	err := r.db.QueryRow(`
		DELETE FROM oauth_codes WHERE code_hash = $1
		RETURNING code_hash, grant_id, client_id, redirect_uri, code_challenge, expires_at
	`, hash).Scan(&c.Hash, &c.GrantID, &c.ClientID, &c.RedirectURI, &c.CodeChallenge, &c.ExpiresAt)
	if _, cerr := r.db.Exec(`DELETE FROM oauth_codes WHERE expires_at < NOW() - INTERVAL '1 day'`); cerr != nil && err == nil {
		return nil, cerr
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CreateToken stores a token pair's hashes.
func (r *ConnectedAppRepository) CreateToken(t *connectedapps.Token) error {
	_, err := r.db.Exec(`
		INSERT INTO oauth_tokens (id, grant_id, access_hash, refresh_hash, access_expires_at, refresh_expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, t.ID, t.GrantID, t.AccessHash, t.RefreshHash, t.AccessExpiresAt, t.RefreshExpiresAt, t.CreatedAt)
	return err
}

// GrantForAccess returns the live grant of an unexpired access token and
// notes the grant was used, at most once a minute.
func (r *ConnectedAppRepository) GrantForAccess(hash string, now time.Time) (*connectedapps.Grant, error) {
	g, err := scanConnectedApp(r.db.QueryRow(`SELECT `+connectedAppColumns+` FROM `+connectedAppFrom+`
		JOIN oauth_tokens t ON t.grant_id = g.id
		WHERE t.access_hash = $1 AND t.access_expires_at > $2 AND g.revoked_at IS NULL`, hash, now))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := r.db.Exec(`
		UPDATE connected_apps SET last_used_at = $2
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < $2::timestamptz - INTERVAL '1 minute')
	`, g.ID, now); err != nil {
		return nil, err
	}
	return g, nil
}

// ConsumeRefresh deletes the pair whose unexpired refresh token has the hash
// and returns it, nil when none. Pairs whose refresh token has expired are
// cleared on the way.
func (r *ConnectedAppRepository) ConsumeRefresh(hash string, now time.Time) (*connectedapps.Token, error) {
	t := new(connectedapps.Token)
	err := r.db.QueryRow(`
		DELETE FROM oauth_tokens WHERE refresh_hash = $1 AND refresh_expires_at > $2
		RETURNING id, grant_id, access_hash, refresh_hash, access_expires_at, refresh_expires_at, created_at
	`, hash, now).Scan(&t.ID, &t.GrantID, &t.AccessHash, &t.RefreshHash, &t.AccessExpiresAt, &t.RefreshExpiresAt, &t.CreatedAt)
	if _, cerr := r.db.Exec(`DELETE FROM oauth_tokens WHERE refresh_expires_at < $1`, now); cerr != nil && err == nil {
		return nil, cerr
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteToken deletes the pair one of whose tokens has the hash, when it
// belongs to a grant of clientID.
func (r *ConnectedAppRepository) DeleteToken(hash, clientID string) error {
	if _, err := uuid.Parse(clientID); err != nil {
		return nil
	}
	_, err := r.db.Exec(`
		DELETE FROM oauth_tokens t USING connected_apps g
		WHERE t.grant_id = g.id AND g.client_id = $2 AND (t.access_hash = $1 OR t.refresh_hash = $1)
	`, hash, clientID)
	return err
}
