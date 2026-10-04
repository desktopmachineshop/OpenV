package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// ProviderSettingRepository implements the providers.Repository interface.
type ProviderSettingRepository struct {
	db *sql.DB
}

// NewProviderSettingRepository creates a new provider setting repository.
func NewProviderSettingRepository(db *sql.DB) *ProviderSettingRepository {
	return &ProviderSettingRepository{db: db}
}

// Upsert inserts or updates the setting row for a provider, and sets p.ID to
// the stored row's id: an update keeps the id the row was inserted with.
func (r *ProviderSettingRepository) Upsert(p *providers.ProviderSetting) error {
	lastDetected := p.LastDetected
	if lastDetected == nil {
		lastDetected = map[string]interface{}{}
	}
	payload, err := json.Marshal(lastDetected)
	if err != nil {
		return err
	}
	return r.db.QueryRow(`
		INSERT INTO provider_settings (
			id, org_id, provider, auth_mode, api_key_env, default_model, enabled, last_detected, updated_at
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (org_id, provider) DO UPDATE SET
			auth_mode = EXCLUDED.auth_mode,
			api_key_env = EXCLUDED.api_key_env,
			default_model = EXCLUDED.default_model,
			enabled = EXCLUDED.enabled,
			last_detected = EXCLUDED.last_detected,
			updated_at = EXCLUDED.updated_at
		RETURNING id
	`,
		p.ID, p.OrgID, p.Provider, p.AuthMode, p.APIKeyEnv, p.DefaultModel, p.Enabled, payload, p.UpdatedAt,
	).Scan(&p.ID)
}

func scanProviderSetting(scan func(dest ...interface{}) error) (*providers.ProviderSetting, error) {
	p := new(providers.ProviderSetting)
	var lastDetected []byte
	err := scan(
		&p.ID, &p.OrgID, &p.Provider, &p.AuthMode, &p.APIKeyEnv, &p.DefaultModel, &p.Enabled,
		&lastDetected, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.LastDetected = map[string]interface{}{}
	if len(lastDetected) > 0 {
		if err := json.Unmarshal(lastDetected, &p.LastDetected); err != nil {
			p.LastDetected = map[string]interface{}{}
		}
	}
	return p, nil
}

// FindByProvider retrieves an org's setting for a provider, returning
// (nil, nil) when absent.
func (r *ProviderSettingRepository) FindByProvider(orgID, provider string) (*providers.ProviderSetting, error) {
	row := r.db.QueryRow(`
		SELECT id, COALESCE(org_id::text, ''), provider, auth_mode, api_key_env, default_model, enabled, last_detected, updated_at
		FROM provider_settings
		WHERE org_id = NULLIF($1, '')::uuid AND provider = $2
	`, orgID, provider)

	p, err := scanProviderSetting(row.Scan)
	if noRow(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// List returns an org's stored provider settings.
func (r *ProviderSettingRepository) List(orgID string) ([]*providers.ProviderSetting, error) {
	rows, err := r.db.Query(`
		SELECT id, COALESCE(org_id::text, ''), provider, auth_mode, api_key_env, default_model, enabled, last_detected, updated_at
		FROM provider_settings
		WHERE org_id = NULLIF($1, '')::uuid
		ORDER BY provider
	`, orgID)
	if malformedID(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*providers.ProviderSetting
	for rows.Next() {
		p, err := scanProviderSetting(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
