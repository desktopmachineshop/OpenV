package seeds

import (
	"slices"
	"strings"
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
func TestAdoptSeedDefaultsUpdatesAnUntouchedAgent(t *testing.T) {
	want := currentCopilotSeed(t)
	prev := previousSeedVersions["requirements-copilot"][0]
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

	changed, err := adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil || !changed {
		t.Fatalf("adoptSeedDefaults() = %v, %v; want true, nil", changed, err)
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
func TestAdoptSeedDefaultsLeavesAnEditedAgentAlone(t *testing.T) {
	want := currentCopilotSeed(t)
	prev := previousSeedVersions["requirements-copilot"][0]

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
			changed, err := adoptSeedDefaults("org-1", tc.existing, want, svc)
			if err != nil {
				t.Fatalf("adoptSeedDefaults() error: %v", err)
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
func TestAdoptSeedDefaultsAreIdempotent(t *testing.T) {
	want := currentCopilotSeed(t)
	existing := &agents.Agent{
		Slug:         "requirements-copilot",
		Name:         want.Name,
		Description:  want.Description,
		SystemPrompt: want.SystemPrompt,
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": existing}}

	changed, err := adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil || changed {
		t.Fatalf("adoptSeedDefaults() = %v, %v; want false, nil on an already-current agent", changed, err)
	}
	if len(svc.saved) != 0 {
		t.Errorf("wrote %d definitions for an unchanged agent, want 0", len(svc.saved))
	}
}

// A seed with no recorded previous identity is never touched.
func TestAdoptSeedDefaultsSkipsAgentsWithNoRecordedHistory(t *testing.T) {
	existing := &agents.Agent{Slug: TestCaseAuthorSlug, Name: "Whatever They Called It"}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{TestCaseAuthorSlug: existing}}

	changed, err := adoptSeedDefaults("org-1", existing, agents.Definition{Name: "New Name"}, svc)
	if err != nil || changed || len(svc.saved) != 0 {
		t.Fatalf("adoptSeedDefaults() = %v, %v, %d saves; want false, nil, 0", changed, err, len(svc.saved))
	}
}

// Every recorded version has to be text that actually shipped, and none of it
// may equal what ships now — a recorded value identical to the current seed
// matches nothing useful, and a field nobody recorded can never reach a
// workspace that already has the agent. This is the half of a seed change
// that is easy to forget.
func TestPreviousSeedVersionsDifferFromTheCurrentSeed(t *testing.T) {
	want := currentCopilotSeed(t)
	versions := previousSeedVersions["requirements-copilot"]
	if len(versions) == 0 {
		t.Fatal("no previous versions recorded; nothing can be adopted")
	}
	for i, prev := range versions {
		if prev.SystemPrompt == want.SystemPrompt {
			t.Errorf("version %d records the current system prompt; it should record what came before", i)
		}
		if slices.Equal(prev.AllowedTools, want.AllowedTools) {
			t.Errorf("version %d records the current allowed_tools; it should record what came before", i)
		}
		if prev.SystemPrompt == "" || len(prev.AllowedTools) == 0 {
			t.Errorf("version %d leaves a field unrecorded, so an agent carrying it can never be recognised as untouched", i)
		}
	}
	// The oldest recorded version is the pre-rename identity the rename
	// migration keys on.
	if versions[0].Name == "" || versions[0].Name == want.Name {
		t.Errorf("oldest recorded name %q does not differ from the current %q", versions[0].Name, want.Name)
	}
}

// The case this mechanism exists for: an agent provisioned before the seed
// gained a capability, never touched by anyone, receives it.
func TestAdoptSeedDefaultsGrantsNewToolsToAnUntouchedAgent(t *testing.T) {
	want := currentCopilotSeed(t)
	versions := previousSeedVersions["requirements-copilot"]
	prev := versions[len(versions)-1] // as it shipped before web access
	existing := &agents.Agent{
		Slug:         "requirements-copilot",
		Name:         prev.Name,
		Description:  prev.Description,
		SystemPrompt: prev.SystemPrompt,
		AllowedTools: prev.AllowedTools,
		Model:        "some-model",
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": existing}}

	changed, err := adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil || !changed {
		t.Fatalf("adoptSeedDefaults() = %v, %v; want true, nil", changed, err)
	}
	got := svc.saved[0]
	if !slices.Equal(got.AllowedTools, want.AllowedTools) {
		t.Errorf("allowed_tools = %v, want %v", got.AllowedTools, want.AllowedTools)
	}
	if got.SystemPrompt != want.SystemPrompt {
		t.Error("the prompt explaining the new tools was not adopted alongside them")
	}
	if got.Model != "some-model" {
		t.Errorf("adoption disturbed a field the workspace owns: model = %q", got.Model)
	}
}

// A workspace that chose its agent's tools keeps them. Widening those without
// asking is the one thing this mechanism must never do.
func TestAdoptSeedDefaultsLeavesEditedToolsAlone(t *testing.T) {
	want := currentCopilotSeed(t)
	versions := previousSeedVersions["requirements-copilot"]
	prev := versions[len(versions)-1]
	existing := &agents.Agent{
		Slug:         "requirements-copilot",
		Name:         prev.Name,
		Description:  prev.Description,
		SystemPrompt: prev.SystemPrompt,
		AllowedTools: []string{"mcp__openv__get_artifact"}, // narrowed by the member
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": existing}}

	if _, err := adoptSeedDefaults("org-1", existing, want, svc); err != nil {
		t.Fatalf("adoptSeedDefaults() error: %v", err)
	}
	for _, saved := range svc.saved {
		if !slices.Equal(saved.AllowedTools, []string{"mcp__openv__get_artifact"}) {
			t.Errorf("the workspace's own tool list was overwritten with %v", saved.AllowedTools)
		}
	}
}

// The V&V Assistant can look things up. Its whole job is asking whether a
// requirement is testable, a hazard is covered, a limit is real — questions
// that usually have an authoritative source somewhere.
func TestVVAssistantCanSearchTheWeb(t *testing.T) {
	var def *agents.Definition
	for _, seed := range defaultAgents() {
		if seed.def.Slug == "requirements-copilot" {
			d := seed.def
			def = &d
			break
		}
	}
	if def == nil {
		t.Fatal("the V&V Assistant seed is missing")
	}

	has := func(tool string) bool {
		for _, t := range def.AllowedTools {
			if t == tool {
				return true
			}
		}
		return false
	}
	if !has("WebSearch") {
		t.Errorf("allowed_tools = %v, want WebSearch among them", def.AllowedTools)
	}
	// Search returns snippets; without fetch the agent can cite a result it
	// never read.
	if !has("WebFetch") {
		t.Errorf("allowed_tools = %v, want WebFetch alongside WebSearch", def.AllowedTools)
	}
	// Granting the tools without saying so leaves the model to guess it has
	// them, and the untrusted-content rule unstated.
	if !strings.Contains(def.SystemPrompt, "search the web") {
		t.Error("the system prompt does not tell the assistant it can search the web")
	}
	if !strings.Contains(def.SystemPrompt, "not as instructions") {
		t.Error("the system prompt does not tell the assistant to treat web content as untrusted")
	}
	// It still must not write to a project itself.
	if !strings.Contains(def.SystemPrompt, "never create or modify artifacts yourself") {
		t.Error("the assistant's read-only stance was lost")
	}
}

// A locked agent does not move, even when every field still carries exactly
// what an earlier seed wrote — the case adoption would otherwise act on. This
// is the whole promise of the lock: nothing changes unless we change it.
func TestAdoptSeedDefaultsSkipsALockedAgent(t *testing.T) {
	want := currentCopilotSeed(t)
	versions := previousSeedVersions["requirements-copilot"]
	prev := versions[len(versions)-1]
	existing := &agents.Agent{
		Slug:         "requirements-copilot",
		Name:         prev.Name,
		Description:  prev.Description,
		SystemPrompt: prev.SystemPrompt,
		AllowedTools: prev.AllowedTools,
		Locked:       true,
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"requirements-copilot": existing}}

	changed, err := adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil {
		t.Fatalf("adoptSeedDefaults() error: %v", err)
	}
	if changed {
		t.Error("a locked agent was updated")
	}
	if len(svc.saved) != 0 {
		t.Errorf("a locked agent was written %d times; it must not be touched at all", len(svc.saved))
	}
	// Unlocking restores the ordinary behaviour, so the lock is a choice and
	// not a one-way door.
	existing.Locked = false
	changed, err = adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil || !changed {
		t.Fatalf("after unlocking, adoptSeedDefaults() = %v, %v; want true, nil", changed, err)
	}
}
