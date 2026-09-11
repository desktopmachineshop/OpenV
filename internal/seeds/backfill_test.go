package seeds

import (
	"slices"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// registryAgentService is a fake agents.Service backed by a map, with enough
// behaviour for a whole EnsureOrgDefaults pass: SaveDefinition writes the row
// back the way the file store does, so List and GetBySlug see what the
// reconcile just wrote.
type registryAgentService struct {
	bySlug map[string]*agents.Agent
	saved  []agents.Definition
}

func newRegistry(rows ...*agents.Agent) *registryAgentService {
	s := &registryAgentService{bySlug: map[string]*agents.Agent{}}
	for _, r := range rows {
		s.bySlug[r.Slug] = r
	}
	return s
}

func (f *registryAgentService) List(orgID string) ([]*agents.Agent, error) {
	out := make([]*agents.Agent, 0, len(f.bySlug))
	for _, a := range f.bySlug {
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b *agents.Agent) int {
		switch {
		case a.Slug < b.Slug:
			return -1
		case a.Slug > b.Slug:
			return 1
		}
		return 0
	})
	return out, nil
}

func (f *registryAgentService) GetBySlug(orgID, slug string) (*agents.Agent, error) {
	return f.bySlug[slug], nil
}

func (f *registryAgentService) SaveDefinition(orgID string, def *agents.Definition) (*agents.Agent, error) {
	// The real store serializes through Validate; a definition that would not
	// survive that must not survive here either.
	if err := def.Validate(); err != nil {
		return nil, err
	}
	f.saved = append(f.saved, *def)
	agent := &agents.Agent{
		OrgID: orgID, Slug: def.Slug, Name: def.Name, Description: def.Description,
		Provider: def.Provider, Model: def.Model, Effort: def.Effort,
		AllowedTools: def.AllowedTools, WriteMode: def.WriteMode,
		RepoAccess: def.RepoAccess, MaxTurns: def.MaxTurns,
		TimeoutSeconds: def.TimeoutSeconds, Config: def.Config,
		Locked: def.Locked, SystemPrompt: def.SystemPrompt,
	}
	f.bySlug[def.Slug] = agent
	return agent, nil
}

func (f *registryAgentService) Get(id string) (*agents.Agent, error)       { return nil, nil }
func (f *registryAgentService) RawFile(orgID, slug string) (string, error) { return "", nil }
func (f *registryAgentService) SaveRawFile(orgID, slug, content string) (*agents.Agent, error) {
	return nil, nil
}
func (f *registryAgentService) Delete(orgID, slug string) error { return nil }
func (f *registryAgentService) SyncFromDisk(orgID string) error { return nil }
func (f *registryAgentService) SyncAllFromDisk() error          { return nil }

// seededTeamService reports the org as already having its default team, so
// EnsureOrgDefaults stops after the agent reconcile — the part under test.
type seededTeamService struct{ teams.Service }

func (s *seededTeamService) ListTeams(orgID, projectID string) ([]*teams.Team, error) {
	return []*teams.Team{{ID: "t1", OrgID: orgID, Name: "Default", IsDefault: true}}, nil
}

func seedDef(t *testing.T, slug string) agents.Definition {
	t.Helper()
	for _, seed := range defaultAgents() {
		if seed.def.Slug == slug {
			return seed.def
		}
	}
	t.Fatalf("no seed with slug %q", slug)
	return agents.Definition{}
}

// REQ-91 regression, through the real entry point. A seeded agent from an
// install that predates mandatory allowlists must end up with ITS OWN seed's
// list, not the generic mcp__openv__* fallback.
//
// The bug this locks down was one of ordering: the org-wide backfill ran
// first and wrote mcp__openv__* to every row that had no list, after which the
// per-seed backfill in the loop could never fire (the row was no longer
// empty). A seeded `developer` came out of startup holding OpenV tools and
// nothing else — no Read, Grep, Glob, Edit, Write or Bash(git *) — so the one
// agent that exists to work in a repository could not open a file.
func TestEnsureOrgDefaultsBackfillsSeededAgentsWithTheirOwnTools(t *testing.T) {
	want := seedDef(t, "developer")
	if len(want.AllowedTools) < 2 {
		t.Fatalf("the developer seed no longer carries vendor tools: %v", want.AllowedTools)
	}

	legacy := &agents.Agent{
		Slug:         "developer",
		Name:         "Developer",
		Provider:     "claude-code",
		RepoAccess:   true,
		Description:  want.Description,
		SystemPrompt: want.SystemPrompt,
		// The pre-REQ-91 state: no allowlist at all, which used to mean the
		// vendor CLI got every tool it has.
		AllowedTools: nil,
	}
	svc := newRegistry(legacy)

	if err := EnsureOrgDefaults("org-1", svc, &seededTeamService{}); err != nil {
		t.Fatalf("EnsureOrgDefaults: %v", err)
	}

	got, _ := svc.GetBySlug("org-1", "developer")
	if got == nil {
		t.Fatal("the developer row disappeared")
	}
	if !slices.Equal(got.AllowedTools, want.AllowedTools) {
		t.Errorf("allowed_tools = %v, want the developer seed's own list %v", got.AllowedTools, want.AllowedTools)
	}

	// An agent nobody seeded still gets the generic OpenV fallback.
	custom := &agents.Agent{Slug: "house-style", Name: "House Style", Provider: "claude-code"}
	svc2 := newRegistry(custom)
	if err := EnsureOrgDefaults("org-1", svc2, &seededTeamService{}); err != nil {
		t.Fatalf("EnsureOrgDefaults: %v", err)
	}
	got, _ = svc2.GetBySlug("org-1", "house-style")
	if !slices.Equal(got.AllowedTools, agents.DefaultAllowedTools()) {
		t.Errorf("allowed_tools = %v, want %v for an agent no seed knows", got.AllowedTools, agents.DefaultAllowedTools())
	}
}

// A locked agent is the workspace's standing answer to "can this change
// without us?". Even a narrowing backfill is a change, so it is left exactly
// as written — and stays unable to run until a person sets a list.
func TestEnsureOrgDefaultsLeavesALockedAgentWithoutTools(t *testing.T) {
	locked := &agents.Agent{
		Slug: "developer", Name: "Developer", Provider: "claude-code",
		Locked: true, AllowedTools: nil,
	}
	svc := newRegistry(locked)
	if err := EnsureOrgDefaults("org-1", svc, &seededTeamService{}); err != nil {
		t.Fatalf("EnsureOrgDefaults: %v", err)
	}
	got, _ := svc.GetBySlug("org-1", "developer")
	if len(got.AllowedTools) != 0 {
		t.Errorf("allowed_tools = %v, want none: a locked agent is never rewritten", got.AllowedTools)
	}
}
