package agents

import (
	"errors"
	"time"
)

// Write modes.
const (
	WriteModeProposal = "proposal"
	WriteModeDirect   = "direct"
)

// ErrNotFound is returned when an agent doesn't exist.
var ErrNotFound = errors.New("agent not found")

// Agent is the registry row mirroring a file-backed agent definition.
// The markdown file is the source of truth; this row exists for FK
// integrity and query speed.
type Agent struct {
	ID             string                 `json:"id"`
	OrgID          string                 `json:"org_id"`
	Slug           string                 `json:"slug"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Provider       string                 `json:"provider"`
	Model          string                 `json:"model"`
	AllowedTools   []string               `json:"allowed_tools"`
	WriteMode      string                 `json:"write_mode"`
	RepoAccess     bool                   `json:"repo_access"`
	MaxTurns       int                    `json:"max_turns"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
	Config         map[string]interface{} `json:"config"`
	SystemPrompt   string                 `json:"system_prompt"`
	FilePath       string                 `json:"file_path"`
	ContentHash    string                 `json:"content_hash"`
	SyncedAt       *time.Time             `json:"synced_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// Definition is the editable content of an agent's markdown file:
// the YAML frontmatter fields plus the body (system prompt).
type Definition struct {
	Slug           string                 `json:"slug" yaml:"slug"`
	Name           string                 `json:"name" yaml:"name"`
	Description    string                 `json:"description" yaml:"description,omitempty"`
	Provider       string                 `json:"provider" yaml:"provider"`
	Model          string                 `json:"model" yaml:"model,omitempty"`
	AllowedTools   []string               `json:"allowed_tools" yaml:"allowed_tools,omitempty"`
	WriteMode      string                 `json:"write_mode" yaml:"write_mode,omitempty"`
	RepoAccess     bool                   `json:"repo_access" yaml:"repo_access,omitempty"`
	MaxTurns       int                    `json:"max_turns" yaml:"max_turns,omitempty"`
	TimeoutSeconds int                    `json:"timeout_seconds" yaml:"timeout_seconds,omitempty"`
	Config         map[string]interface{} `json:"config" yaml:"config,omitempty"`
	SystemPrompt   string                 `json:"system_prompt" yaml:"-"`
}

// Validate checks a definition for required fields and sane defaults.
func (d *Definition) Validate() error {
	if d.Slug == "" {
		return errors.New("agent definition requires a slug")
	}
	if d.Name == "" {
		return errors.New("agent definition requires a name")
	}
	if d.Provider == "" {
		return errors.New("agent definition requires a provider")
	}
	switch d.WriteMode {
	case "":
		d.WriteMode = WriteModeProposal
	case WriteModeProposal, WriteModeDirect:
	default:
		return errors.New("write_mode must be 'proposal' or 'direct'")
	}
	if d.MaxTurns <= 0 {
		d.MaxTurns = 50
	}
	if d.TimeoutSeconds <= 0 {
		d.TimeoutSeconds = 1800
	}
	return nil
}

// Repository defines persistence for the agent registry. Slugs are unique
// per organization; ids stay globally unique.
type Repository interface {
	Save(a *Agent) error
	Update(a *Agent) error
	FindByID(id string) (*Agent, error)
	FindBySlug(orgID, slug string) (*Agent, error)
	List(orgID string) ([]*Agent, error)
	Delete(id string) error
}

// Service defines agent-definition domain logic. Definitions live as
// markdown files on disk under AGENTS_DIR/<org_id>/; all writes go through
// the file store then sync into the registry.
type Service interface {
	List(orgID string) ([]*Agent, error)
	Get(id string) (*Agent, error)
	GetBySlug(orgID, slug string) (*Agent, error)
	// SaveDefinition writes/overwrites the agent's markdown file and syncs
	// the registry row. Creates the agent if the slug is new in the org.
	SaveDefinition(orgID string, def *Definition) (*Agent, error)
	// RawFile returns the current markdown file content for a slug.
	RawFile(orgID, slug string) (string, error)
	// SaveRawFile parses, validates, writes and syncs a raw markdown file.
	// The slug in the frontmatter must match.
	SaveRawFile(orgID, slug string, content string) (*Agent, error)
	// Delete moves the file to the org's trash and removes the registry row.
	Delete(orgID, slug string) error
	// SyncFromDisk reconciles the registry with one org's agents directory.
	SyncFromDisk(orgID string) error
	// SyncAllFromDisk walks every org subdirectory and syncs each one.
	SyncAllFromDisk() error
}
