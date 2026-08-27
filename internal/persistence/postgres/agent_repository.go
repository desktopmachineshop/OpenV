package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

// AgentRepository implements agents.Repository.
type AgentRepository struct {
	db *sql.DB
}

// NewAgentRepository creates a new agent repository.
func NewAgentRepository(db *sql.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

const agentColumns = `id, COALESCE(org_id::text, ''), slug, name, description, provider, model, effort, allowed_tools, write_mode, repo_access, max_turns, timeout_seconds, config, system_prompt, file_path, content_hash, synced_at, created_at, updated_at`

func scanAgent(row interface{ Scan(...interface{}) error }) (*agents.Agent, error) {
	a := new(agents.Agent)
	var tools, config []byte
	var syncedAt sql.NullTime
	err := row.Scan(&a.ID, &a.OrgID, &a.Slug, &a.Name, &a.Description, &a.Provider, &a.Model, &a.Effort, &tools, &a.WriteMode, &a.RepoAccess, &a.MaxTurns, &a.TimeoutSeconds, &config, &a.SystemPrompt, &a.FilePath, &a.ContentHash, &syncedAt, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if syncedAt.Valid {
		t := syncedAt.Time
		a.SyncedAt = &t
	}
	if err := json.Unmarshal(tools, &a.AllowedTools); err != nil || a.AllowedTools == nil {
		a.AllowedTools = []string{}
	}
	if err := json.Unmarshal(config, &a.Config); err != nil || a.Config == nil {
		a.Config = map[string]interface{}{}
	}
	return a, nil
}

func agentJSONFields(a *agents.Agent) ([]byte, []byte, error) {
	tools := a.AllowedTools
	if tools == nil {
		tools = []string{}
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return nil, nil, err
	}
	config := a.Config
	if config == nil {
		config = map[string]interface{}{}
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, nil, err
	}
	return toolsJSON, configJSON, nil
}

// Save inserts an agent registry row.
func (r *AgentRepository) Save(a *agents.Agent) error {
	toolsJSON, configJSON, err := agentJSONFields(a)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO agents (id, org_id, slug, name, description, provider, model, effort, allowed_tools, write_mode, repo_access, max_turns, timeout_seconds, config, system_prompt, file_path, content_hash, synced_at, created_at, updated_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`, a.ID, a.OrgID, a.Slug, a.Name, a.Description, a.Provider, a.Model, a.Effort, toolsJSON, a.WriteMode, a.RepoAccess, a.MaxTurns, a.TimeoutSeconds, configJSON, a.SystemPrompt, a.FilePath, a.ContentHash, a.SyncedAt, a.CreatedAt, a.UpdatedAt)
	return err
}

// Update rewrites an agent registry row.
func (r *AgentRepository) Update(a *agents.Agent) error {
	toolsJSON, configJSON, err := agentJSONFields(a)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		UPDATE agents SET name = $2, description = $3, provider = $4, model = $5, effort = $6, allowed_tools = $7, write_mode = $8, repo_access = $9, max_turns = $10, timeout_seconds = $11, config = $12, system_prompt = $13, file_path = $14, content_hash = $15, synced_at = $16, updated_at = $17
		WHERE id = $1
	`, a.ID, a.Name, a.Description, a.Provider, a.Model, a.Effort, toolsJSON, a.WriteMode, a.RepoAccess, a.MaxTurns, a.TimeoutSeconds, configJSON, a.SystemPrompt, a.FilePath, a.ContentHash, a.SyncedAt, a.UpdatedAt)
	return err
}

// FindByID returns an agent by id, or nil.
func (r *AgentRepository) FindByID(id string) (*agents.Agent, error) {
	a, err := scanAgent(r.db.QueryRow(`SELECT `+agentColumns+` FROM agents WHERE id = $1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// FindBySlug returns an agent by slug within an org, or nil.
func (r *AgentRepository) FindBySlug(orgID, slug string) (*agents.Agent, error) {
	a, err := scanAgent(r.db.QueryRow(`SELECT `+agentColumns+` FROM agents WHERE org_id = NULLIF($1, '')::uuid AND slug = $2`, orgID, slug))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return a, err
}

// List returns an org's agents ordered by name.
func (r *AgentRepository) List(orgID string) ([]*agents.Agent, error) {
	rows, err := r.db.Query(`SELECT `+agentColumns+` FROM agents WHERE org_id = NULLIF($1, '')::uuid ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*agents.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// Delete removes an agent registry row.
func (r *AgentRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM agents WHERE id = $1`, id)
	return err
}
