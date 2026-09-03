package seeds

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

// TestDefaultAgentsSeedsTestCaseAuthor locks in that the test-case-author agent
// (issue #218) is part of the per-org default seed set, and that it is a valid,
// proposal-mode definition with the OpenV tools it needs — so every org that
// runs EnsureOrgDefaults gets an agent the "Draft test cases" action can launch.
func TestDefaultAgentsSeedsTestCaseAuthor(t *testing.T) {
	var found *agents.Definition
	for _, seed := range defaultAgents() {
		if seed.def.Slug == TestCaseAuthorSlug {
			def := seed.def
			found = &def
			// It is a standalone specialist, not a node on the default team.
			if seed.label != "" {
				t.Errorf("test-case-author should not be on the default team, got label %q", seed.label)
			}
			break
		}
	}
	if found == nil {
		t.Fatalf("defaultAgents() is missing an agent with slug %q", TestCaseAuthorSlug)
	}

	// Validate() applies the same defaults the file store would, and rejects a
	// malformed definition — so passing it proves the seed is loadable.
	if err := found.Validate(); err != nil {
		t.Fatalf("seeded test-case-author definition is invalid: %v", err)
	}

	if found.WriteMode != agents.WriteModeProposal {
		t.Errorf("write_mode = %q, want %q (writes must be reviewed)", found.WriteMode, agents.WriteModeProposal)
	}
	if found.Provider == "" {
		t.Error("provider must be set")
	}
	if found.Name == "" {
		t.Error("name must be set")
	}

	hasOpenVTools := false
	for _, tool := range found.AllowedTools {
		if tool == "mcp__openv__*" {
			hasOpenVTools = true
		}
	}
	if !hasOpenVTools {
		t.Errorf("allowed_tools = %v, want it to include the OpenV MCP tools (mcp__openv__*)", found.AllowedTools)
	}
}

// --- the seeded-agent rename ---

// fakeAgentService is enough of agents.Service for the rename: it holds one
// org's agents by slug and records what was written.
type fakeAgentService struct {
	bySlug map[string]*agents.Agent
	saved  []agents.Definition
	err    error
}

func (f *fakeAgentService) GetBySlug(orgID, slug string) (*agents.Agent, error) {
	return f.bySlug[slug], nil
}

func (f *fakeAgentService) SaveDefinition(orgID string, def *agents.Definition) (*agents.Agent, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.saved = append(f.saved, *def)
	return &agents.Agent{OrgID: orgID, Slug: def.Slug, Name: def.Name}, nil
}

func (f *fakeAgentService) List(orgID string) ([]*agents.Agent, error) { return nil, nil }
func (f *fakeAgentService) Get(id string) (*agents.Agent, error)       { return nil, nil }
func (f *fakeAgentService) RawFile(orgID, slug string) (string, error) { return "", nil }
func (f *fakeAgentService) SaveRawFile(orgID, slug, content string) (*agents.Agent, error) {
	return nil, nil
}
func (f *fakeAgentService) Delete(orgID, slug string) error { return nil }
func (f *fakeAgentService) SyncFromDisk(orgID string) error { return nil }
func (f *fakeAgentService) SyncAllFromDisk() error          { return nil }

// currentCopilotSeed is the seed as it ships today — what an untouched
// workspace should catch up to.
func currentCopilotSeed(t *testing.T) agents.Definition {
	t.Helper()
	for _, seed := range defaultAgents() {
		if seed.def.Slug == "requirements-copilot" {
			return seed.def
		}
	}
	t.Fatal("the requirements-copilot seed is gone; the rename has nothing to adopt")
	return agents.Definition{}
}

// An agent nobody has edited still says what the old seed wrote, so it takes
// the new name, description and prompt.
func TestAdoptSeedRenameUpdatesAnUntouchedAgent(t *testing.T) {
	want := currentCopilotSeed(t)
	prev := previousSeedIdentity["requirements-copilot"]
	existing := &agents.Agent{
		Slug:         "requirements-copilot",
		Name:         prev.Name,
		Description:  prev.Description,
		SystemPrompt: prev.SystemPrompt,
		Provider:     "claude-code",
		Effort:       "low",
		Model:        "some-model",
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": existing}}

	changed, err := adoptSeedRename("org-1", existing, want, svc)
	if err != nil || !changed {
		t.Fatalf("adoptSeedRename() = %v, %v; want true, nil", changed, err)
	}
	if len(svc.saved) != 1 {
		t.Fatalf("wrote %d definitions, want 1", len(svc.saved))
	}
	got := svc.saved[0]
	if got.Name != want.Name {
		t.Errorf("name = %q, want %q", got.Name, want.Name)
	}
	if got.Description != want.Description || got.SystemPrompt != want.SystemPrompt {
		t.Error("description and system prompt should have been brought up to the current seed")
	}
	// Tuning the workspace owns must survive the rename.
	if got.Model != "some-model" || got.Effort != "low" || got.Slug != "requirements-copilot" {
		t.Errorf("rename disturbed fields it does not own: %+v", got)
	}
}

// The point of the migration: a workspace that renamed or retuned its agent
// keeps exactly what it wrote.
func TestAdoptSeedRenameLeavesAnEditedAgentAlone(t *testing.T) {
	want := currentCopilotSeed(t)
	prev := previousSeedIdentity["requirements-copilot"]

	cases := []struct {
		name     string
		existing *agents.Agent
		wantSave bool
	}{
		{
			name: "renamed by the workspace",
			existing: &agents.Agent{
				Slug: "requirements-copilot", Name: "Reqs Buddy",
				Description: prev.Description, SystemPrompt: prev.SystemPrompt,
			},
			// The other two fields are still the old seed's, so they update;
			// the name the member chose is what must not move.
			wantSave: true,
		},
		{
			name: "every identity field edited",
			existing: &agents.Agent{
				Slug: "requirements-copilot", Name: "Reqs Buddy",
				Description: "ours", SystemPrompt: "our prompt",
			},
			wantSave: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": tc.existing}}
			changed, err := adoptSeedRename("org-1", tc.existing, want, svc)
			if err != nil {
				t.Fatalf("adoptSeedRename() error: %v", err)
			}
			if changed != tc.wantSave {
				t.Fatalf("changed = %v, want %v", changed, tc.wantSave)
			}
			for _, saved := range svc.saved {
				if saved.Name != "Reqs Buddy" {
					t.Errorf("the workspace's own name was overwritten with %q", saved.Name)
				}
			}
		})
	}
}

// Already renamed: a restart must not rewrite the file and churn its hash.
func TestAdoptSeedRenameIsIdempotent(t *testing.T) {
	want := currentCopilotSeed(t)
	existing := &agents.Agent{
		Slug:         "requirements-copilot",
		Name:         want.Name,
		Description:  want.Description,
		SystemPrompt: want.SystemPrompt,
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": existing}}

	changed, err := adoptSeedRename("org-1", existing, want, svc)
	if err != nil || changed {
		t.Fatalf("adoptSeedRename() = %v, %v; want false, nil on an already-current agent", changed, err)
	}
	if len(svc.saved) != 0 {
		t.Errorf("wrote %d definitions for an unchanged agent, want 0", len(svc.saved))
	}
}

// A seed with no recorded previous identity is never touched.
func TestAdoptSeedRenameSkipsAgentsWithNoRecordedRename(t *testing.T) {
	existing := &agents.Agent{Slug: TestCaseAuthorSlug, Name: "Whatever They Called It"}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{TestCaseAuthorSlug: existing}}

	changed, err := adoptSeedRename("org-1", existing, agents.Definition{Name: "New Name"}, svc)
	if err != nil || changed || len(svc.saved) != 0 {
		t.Fatalf("adoptSeedRename() = %v, %v, %d saves; want false, nil, 0", changed, err, len(svc.saved))
	}
}

// The recorded "previous" text has to be the text that actually shipped, or
// the migration silently matches nothing and every workspace keeps the old
// name for ever.
func TestPreviousSeedIdentityDiffersFromTheCurrentSeed(t *testing.T) {
	want := currentCopilotSeed(t)
	prev := previousSeedIdentity["requirements-copilot"]
	if prev.Name == "" || prev.Name == want.Name {
		t.Errorf("previous name %q does not differ from the current %q", prev.Name, want.Name)
	}
	if prev.SystemPrompt == want.SystemPrompt {
		t.Error("previous system prompt is identical to the current one")
	}
}
