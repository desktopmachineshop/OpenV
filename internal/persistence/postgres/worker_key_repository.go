package postgres

import (
	"database/sql"
	"time"

	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// WorkerKeyRepository implements workerkeys.Repository.
type WorkerKeyRepository struct {
	db *sql.DB
}

// NewWorkerKeyRepository creates a new worker key repository.
func NewWorkerKeyRepository(db *sql.DB) *WorkerKeyRepository {
	return &WorkerKeyRepository{db: db}
}

const workerKeyColumns = `k.id, k.org_id, k.user_id, k.name, k.key_hash, k.created_by, k.revoked, k.last_used_at, k.created_at, k.session_id`

func scanWorkerKey(row interface{ Scan(...interface{}) error }, extra ...interface{}) (*workerkeys.Key, error) {
	k := new(workerkeys.Key)
	var userID, createdBy, sessionID sql.NullString
	var lastUsed sql.NullTime
	dest := []interface{}{&k.ID, &k.OrgID, &userID, &k.Name, &k.KeyHash, &createdBy, &k.Revoked, &lastUsed, &k.CreatedAt, &sessionID}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if sessionID.Valid {
		v := sessionID.String
		k.SessionID = &v
	}
	if userID.Valid {
		v := userID.String
		k.UserID = &v
	}
	if createdBy.Valid {
		v := createdBy.String
		k.CreatedBy = &v
	}
	if lastUsed.Valid {
		t := lastUsed.Time
		k.LastUsedAt = &t
	}
	return k, nil
}

// Save inserts a worker key.
func (r *WorkerKeyRepository) Save(k *workerkeys.Key) error {
	_, err := r.db.Exec(`
		INSERT INTO worker_keys (id, org_id, user_id, name, key_hash, created_by, revoked, created_at, session_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, k.ID, k.OrgID, k.UserID, k.Name, k.KeyHash, k.CreatedBy, k.Revoked, k.CreatedAt, k.SessionID)
	return err
}

// List returns an org's keys, newest first, with user names on personal keys.
func (r *WorkerKeyRepository) List(orgID string) ([]*workerkeys.Key, error) {
	rows, err := r.db.Query(`
		SELECT `+workerKeyColumns+`, COALESCE(u.name, '')
		FROM worker_keys k LEFT JOIN users u ON u.id = k.user_id
		WHERE k.org_id = $1 ORDER BY k.created_at DESC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*workerkeys.Key
	for rows.Next() {
		var userName string
		k, err := scanWorkerKey(rows, &userName)
		if err != nil {
			return nil, err
		}
		k.UserName = userName
		result = append(result, k)
	}
	return result, rows.Err()
}

// FindPersonal returns the user's active personal key in the org, or nil.
// Transient runner session keys are excluded: leasing a cloud runner must not
// shadow (or, through Create's rotation, revoke) the key on the member's own
// machine.
func (r *WorkerKeyRepository) FindPersonal(orgID, userID string) (*workerkeys.Key, error) {
	k, err := scanWorkerKey(r.db.QueryRow(`
		SELECT `+workerKeyColumns+` FROM worker_keys k
		WHERE k.org_id = $1 AND k.user_id = $2 AND NOT k.revoked AND k.session_id IS NULL
		ORDER BY k.created_at DESC LIMIT 1
	`, orgID, userID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// HasOnlinePersonalKey reports recent activity on any of the user's personal
// keys — their own machine's connector key or a transient runner they hold a
// lease on. Both are "their runner" for routing: a run they launch is
// reserved for whichever one is online.
func (r *WorkerKeyRepository) HasOnlinePersonalKey(orgID, userID string, since time.Time) (bool, error) {
	var count int
	err := r.db.QueryRow(`
		SELECT COUNT(*) FROM worker_keys
		WHERE org_id = $1 AND user_id = $2 AND NOT revoked AND last_used_at > $3
	`, orgID, userID, since).Scan(&count)
	return count > 0, err
}

// FindByID returns a key, or nil.
func (r *WorkerKeyRepository) FindByID(id string) (*workerkeys.Key, error) {
	k, err := scanWorkerKey(r.db.QueryRow(`SELECT `+workerKeyColumns+` FROM worker_keys k WHERE k.id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// FindByHash returns the key with the given hash, or nil.
func (r *WorkerKeyRepository) FindByHash(hash string) (*workerkeys.Key, error) {
	k, err := scanWorkerKey(r.db.QueryRow(`SELECT `+workerKeyColumns+` FROM worker_keys k WHERE k.key_hash = $1`, hash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// SavePairing stores a connector pairing code.
func (r *WorkerKeyRepository) SavePairing(id, orgID, userID, codeHash string, expiresAt time.Time) error {
	_, err := r.db.Exec(`
		INSERT INTO connector_pairings (id, org_id, user_id, code_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, orgID, userID, codeHash, expiresAt)
	return err
}

// ConsumePairing atomically uses a valid pairing code.
func (r *WorkerKeyRepository) ConsumePairing(codeHash string, now time.Time) (string, string, error) {
	var orgID, userID string
	err := r.db.QueryRow(`
		UPDATE connector_pairings SET used = TRUE
		WHERE code_hash = $1 AND NOT used AND expires_at > $2
		RETURNING org_id, user_id
	`, codeHash, now).Scan(&orgID, &userID)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return orgID, userID, nil
}

// Revoke disables a key.
func (r *WorkerKeyRepository) Revoke(id string) error {
	_, err := r.db.Exec(`UPDATE worker_keys SET revoked = TRUE WHERE id = $1`, id)
	return err
}

// Touch records key usage.
func (r *WorkerKeyRepository) Touch(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE worker_keys SET last_used_at = $2 WHERE id = $1`, id, at)
	return err
}
