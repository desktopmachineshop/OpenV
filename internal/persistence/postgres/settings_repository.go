package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/settings"
)

// SettingsRepository implements settings.Store over the settings JSONB column
// carried by organizations and projects.
type SettingsRepository struct {
	db *sql.DB
}

// NewSettingsRepository creates the settings store.
func NewSettingsRepository(db *sql.DB) settings.Store {
	return &SettingsRepository{db: db}
}

// OrgSettings implements settings.Store.
func (r *SettingsRepository) OrgSettings(orgID string) (map[string]interface{}, error) {
	return r.get(`SELECT settings FROM organizations WHERE id = $1`, orgID, "workspace")
}

// SetOrgSettings implements settings.Store.
func (r *SettingsRepository) SetOrgSettings(orgID string, s map[string]interface{}) error {
	return r.set(`UPDATE organizations SET settings = $2, updated_at = NOW() WHERE id = $1`, orgID, s, "workspace")
}

// ProjectSettings implements settings.Store.
func (r *SettingsRepository) ProjectSettings(projectID string) (map[string]interface{}, error) {
	return r.get(`SELECT settings FROM projects WHERE id = $1`, projectID, "project")
}

// SetProjectSettings implements settings.Store.
func (r *SettingsRepository) SetProjectSettings(projectID string, s map[string]interface{}) error {
	return r.set(`UPDATE projects SET settings = $2, updated_at = NOW() WHERE id = $1`, projectID, s, "project")
}

// get reads one settings column. A missing row yields empty settings rather
// than an error: callers resolve against defaults, and the row's absence is
// already reported by whatever loaded the org or project itself.
func (r *SettingsRepository) get(query, id, level string) (map[string]interface{}, error) {
	var raw []byte
	switch err := r.db.QueryRow(query, id).Scan(&raw); {
	case err == sql.ErrNoRows:
		return map[string]interface{}{}, nil
	case err != nil:
		return nil, fmt.Errorf("failed to read %s settings: %w", level, err)
	}
	out := map[string]interface{}{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("failed to decode %s settings: %w", level, err)
	}
	return out, nil
}

func (r *SettingsRepository) set(query, id string, s map[string]interface{}, level string) error {
	if s == nil {
		s = map[string]interface{}{}
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to encode %s settings: %w", level, err)
	}
	if _, err := r.db.Exec(query, id, encoded); err != nil {
		return fmt.Errorf("failed to save %s settings: %w", level, err)
	}
	return nil
}
