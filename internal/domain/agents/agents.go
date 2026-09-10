package agents

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// Write modes.
const (
	WriteModeProposal = "proposal"
	WriteModeDirect   = "direct"
)

// Reasoning effort levels ("" = provider default). Providers that support
// fewer tiers map the higher ones down; providers with no effort control
// ignore the setting.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// ValidEffort reports whether v is an allowed effort value.
func ValidEffort(v string) bool {
	if v == "" {
		return true
	}
	for _, e := range EffortLevels {
		if v == e {
			return true
		}
	}
	return false
}

// InterviewerSlug is the seeded interviewer agent's slug. Its prompt carries
// a stakeholder's own words — text authored outside the workspace — so it is
// treated as an untrusted-input agent wherever that matters (UntrustedInput).
//
// It is a backstop, not the rule: what makes an interview turn untrusted is
// that it *is* an interview turn (agentruns.Run.UntrustedOrigin), whichever
// agent the interview was bound to.
const InterviewerSlug = "requirements-interviewer"

// openvServerTools is Claude Code's server-wide allowlist spelling for the
// OpenV MCP server: naming the server on its own grants every tool it offers.
// It means exactly what openvToolPrefix+"*" means, so both forms have to be
// recognised as OpenV — not as some other MCP server (see mcp.ServerTools,
// which this deliberately restates rather than importing: the domain package
// stays free of the transport packages).
const (
	openvServerTools = "mcp__openv"
	openvToolPrefix  = openvServerTools + "__"
)

// DefaultAllowedTools is the narrowest allowlist that still lets an agent do
// OpenV work, and what a legacy definition carrying no allowlist at all is
// backfilled to. It is never a widening: before allowlists were mandatory, an
// empty list meant the vendor CLI started with *every* tool it has.
func DefaultAllowedTools() []string { return []string{openvToolPrefix + "*"} }

// AllowedToolsRequired is the single wording for a definition that carries no
// tool allowlist. Validate returns it (so the API answers 400 with it) and the
// runner echoes it when it refuses to launch such an agent.
const AllowedToolsRequired = "agent definition requires allowed_tools: every agent must name the tools its vendor CLI may use (e.g. mcp__openv__*), because a CLI started with no allowlist runs with all of them"

// ErrNotFound is returned when an agent doesn't exist.
var ErrNotFound = errors.New("agent not found")

// ErrSlugExists is returned when creating an agent whose slug is already
// taken in the org. The database's unique index on (org_id, slug) is the
// authority, so a lost check-then-create race still surfaces as this error.
var ErrSlugExists = errors.New("an agent with this slug already exists")

// slugPattern matches the slug convention used by seeded agents
// (e.g. "requirements-copilot"): lowercase letters, digits and hyphens,
// starting with a letter or digit. Slugs become file names on disk, so
// this also keeps path separators and dot segments out.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// maxSlugLength caps slug size to keep file names sane.
const maxSlugLength = 128

// ValidSlug reports whether s is an acceptable agent slug.
func ValidSlug(s string) bool {
	return len(s) <= maxSlugLength && slugPattern.MatchString(s)
}

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
	Effort         string                 `json:"effort"`
	AllowedTools   []string               `json:"allowed_tools"`
	WriteMode      string                 `json:"write_mode"`
	RepoAccess     bool                   `json:"repo_access"`
	MaxTurns       int                    `json:"max_turns"`
	TimeoutSeconds int                    `json:"timeout_seconds"`
	Config         map[string]interface{} `json:"config"`
	SystemPrompt   string                 `json:"system_prompt"`
	// Locked pins the agent against automatic updates: the seed adoption
	// that brings untouched agents up to a new release skips it entirely.
	// For a workspace that needs to be able to say this agent does not
	// change unless we change it.
	Locked      bool       `json:"locked"`
	FilePath    string     `json:"file_path"`
	ContentHash string     `json:"content_hash"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Definition is the editable content of an agent's markdown file:
// the YAML frontmatter fields plus the body (system prompt).
type Definition struct {
	Slug           string                 `json:"slug" yaml:"slug"`
	Name           string                 `json:"name" yaml:"name"`
	Description    string                 `json:"description" yaml:"description,omitempty"`
	Provider       string                 `json:"provider" yaml:"provider"`
	Model          string                 `json:"model" yaml:"model,omitempty"`
	Effort         string                 `json:"effort" yaml:"effort,omitempty"`
	AllowedTools   []string               `json:"allowed_tools" yaml:"allowed_tools,omitempty"`
	WriteMode      string                 `json:"write_mode" yaml:"write_mode,omitempty"`
	RepoAccess     bool                   `json:"repo_access" yaml:"repo_access,omitempty"`
	MaxTurns       int                    `json:"max_turns" yaml:"max_turns,omitempty"`
	TimeoutSeconds int                    `json:"timeout_seconds" yaml:"timeout_seconds,omitempty"`
	Config         map[string]interface{} `json:"config" yaml:"config,omitempty"`
	// Locked travels in the frontmatter so the guarantee survives a file
	// sync: an agent edited on disk stays locked without anyone having to
	// remember to re-tick it.
	Locked       bool   `json:"locked" yaml:"locked,omitempty"`
	SystemPrompt string `json:"system_prompt" yaml:"-"`
}

// Validate checks a definition for required fields and sane defaults.
func (d *Definition) Validate() error {
	return d.validate(true)
}

// validate is Validate with one knob: requireTools off admits a definition
// that names no tools. Exactly one caller turns it off — a *locked* file
// already on disk, being re-read by the sync (parseSyncedFile). See there for
// why that is the one case where an allowlist can be absent, and note what it
// buys: nothing. Such an agent is kept in the registry so it stays visible
// and editable, and it still cannot run — the worker and every adapter refuse
// an empty allowlist (REQ-91).
func (d *Definition) validate(requireTools bool) error {
	if d.Slug == "" {
		return errors.New("agent definition requires a slug")
	}
	if !ValidSlug(d.Slug) {
		return errors.New("agent slug must be lowercase letters, digits and hyphens (e.g. requirements-copilot)")
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
	// REQ-91: no allowlist, no agent. Enforced here so every write path — the
	// REST handlers, a raw markdown save, an import — refuses identically.
	// Definitions already on disk are backfilled as they sync instead
	// (parseSyncedFile), so an install predating this rule keeps working.
	d.AllowedTools = NonEmptyTools(d.AllowedTools)
	if requireTools && len(d.AllowedTools) == 0 {
		return errors.New(AllowedToolsRequired)
	}
	if !ValidEffort(d.Effort) {
		return errors.New("effort must be one of low, medium, high, xhigh, max (or empty for the provider default)")
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

// NonEmptyTools drops blank entries from a tool list, so an allowlist of
// `[""]` or `[" "]` counts as no allowlist at all.
func NonEmptyTools(tools []string) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		if s := strings.TrimSpace(t); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// UntrustedInput reports what this agent's *definition* says about whether its
// tool results can carry content authored outside the workspace — a
// repository's files, a web page. Such a run must never auto-approve file
// edits or shell commands (REQ-91, HAZ-1): everything it may do has to be on
// its allowlist, where a person put it.
//
// Three things make an agent untrusted by definition:
//
//   - it has repository access, so a cloned repo's files reach the model;
//   - it holds a tool that reads the outside world — WebFetch/WebSearch, or an
//     MCP server other than openv, whose tool results OpenV cannot vouch for;
//   - it is the seeded interviewer, kept as a backstop.
//
// This is only half the question. The other half is where the *run* came from
// — an interview turn's prompt is a stranger's transcript whatever agent is
// serving it — and that lives on the run
// (agentruns.Run.UntrustedOrigin). The worker ORs the two; neither alone is
// the answer.
func (a *Agent) UntrustedInput() bool {
	if a == nil {
		return false
	}
	return a.Slug == InterviewerSlug || a.RepoAccess || ToolsReachOutside(a.AllowedTools)
}

// ToolsReachOutside reports whether an allowlist grants a tool that pulls in
// content nobody in the workspace wrote.
func ToolsReachOutside(tools []string) bool {
	for _, t := range tools {
		name := strings.TrimSpace(t)
		// Drop a vendor argument filter ("Bash(git *)") before matching.
		if i := strings.IndexByte(name, '('); i >= 0 {
			name = strings.TrimSpace(name[:i])
		}
		switch {
		case strings.EqualFold(name, "WebFetch"), strings.EqualFold(name, "WebSearch"):
			return true
		case name == openvServerTools || strings.HasPrefix(name, openvToolPrefix):
			// OpenV's own server, in either spelling: its tool results are
			// the workspace's own data.
		case strings.HasPrefix(name, "mcp__"):
			return true
		}
	}
	return false
}
