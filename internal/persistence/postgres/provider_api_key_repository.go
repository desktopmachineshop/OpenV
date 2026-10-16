package postgres

import (
	"database/sql"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// ProviderAPIKeyRepository implements providers.KeyRepository: a
// workspace's pasted provider API keys, stored sealed.
type ProviderAPIKeyRepository struct {
	db *sql.DB
}

// NewProviderAPIKeyRepository creates the pasted-key repository.
func NewProviderAPIKeyRepository(db *sql.DB) *ProviderAPIKeyRepository {
	return &ProviderAPIKeyRepository{db: db}
}

const providerAPIKeyColumns = `COALESCE(org_id::text, ''), key_env, sealed, last4, COALESCE(updated_by::text, ''), updated_at`

func scanProviderAPIKey(row interface{ Scan(...any) error }) (*providers.StoredKey, error) {
	k := &providers.StoredKey{}
	if err := row.Scan(&k.OrgID, &k.KeyEnv, &k.Sealed, &k.Last4, &k.UpdatedBy, &k.UpdatedAt); err != nil {
		return nil, err
	}
	return k, nil
}

// UpsertKey stores the key for its workspace and variable, replacing one
// already there.
func (r *ProviderAPIKeyRepository) UpsertKey(k *providers.StoredKey) error {
	_, err := r.db.Exec(`
		INSERT INTO provider_api_keys (org_id, key_env, sealed, last4, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid, $6)
		ON CONFLICT (org_id, key_env) DO UPDATE SET
			sealed = EXCLUDED.sealed,
			last4 = EXCLUDED.last4,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at
	`, k.OrgID, k.KeyEnv, k.Sealed, k.Last4, k.UpdatedBy, k.UpdatedAt)
	return err
}

// FindKey returns the workspace's key for a variable, or (nil, nil).
func (r *ProviderAPIKeyRepository) FindKey(orgID, keyEnv string) (*providers.StoredKey, error) {
	k, err := scanProviderAPIKey(r.db.QueryRow(`SELECT `+providerAPIKeyColumns+`
		FROM provider_api_keys WHERE org_id = $1 AND key_env = $2`, orgID, keyEnv))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// ListKeys returns the workspace's keys in variable order.
func (r *ProviderAPIKeyRepository) ListKeys(orgID string) ([]*providers.StoredKey, error) {
	rows, err := r.db.Query(`SELECT `+providerAPIKeyColumns+`
		FROM provider_api_keys WHERE org_id = $1 ORDER BY key_env`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*providers.StoredKey{}
	for rows.Next() {
		k, err := scanProviderAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteKey removes the workspace's key for a variable, reporting whether
// there was one.
func (r *ProviderAPIKeyRepository) DeleteKey(orgID, keyEnv string) (bool, error) {
	res, err := r.db.Exec(`DELETE FROM provider_api_keys WHERE org_id = $1 AND key_env = $2`, orgID, keyEnv)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
