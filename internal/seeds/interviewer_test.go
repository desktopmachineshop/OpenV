package seeds

import (
	"slices"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/mcp"
)

func currentInterviewerSeed(t *testing.T) agents.Definition {
	t.Helper()
	for _, seed := range defaultAgents() {
		if seed.def.Slug == agents.InterviewerSlug {
			return seed.def
		}
	}
	t.Fatal("the requirements-interviewer seed is gone")
	return agents.Definition{}
}

// REQ-91: the interviewer holds read-only tools plus the candidate-need
// recorder, and nothing else. It talks to strangers on public invite links, so
// a wildcard here is the whole OpenV write surface handed to whoever is typing.
func TestInterviewerSeedIsLeastPrivilege(t *testing.T) {
	def := currentInterviewerSeed(t)

	if slices.Contains(def.AllowedTools, "mcp__openv__*") {
		t.Fatal("the interviewer still holds the wildcard OpenV allowlist")
	}
	recorder := mcp.ToolPrefix + "record_candidate_need"
	if !slices.Contains(def.AllowedTools, recorder) {
		t.Errorf("allowed_tools %v is missing %s, the one write it exists to make", def.AllowedTools, recorder)
	}
	for _, tool := range def.AllowedTools {
		if tool == recorder {
			continue
		}
		if !strings.HasPrefix(tool, mcp.ToolPrefix) {
			t.Errorf("%q is not an OpenV tool; the interviewer gets no vendor tools at all", tool)
		}
		if !mcp.ReadOnly(tool) {
			t.Errorf("%q is not read-only", tool)
		}
	}
	// It is still a definition the loader accepts.
	if err := def.Validate(); err != nil {
		t.Fatalf("seeded interviewer is invalid: %v", err)
	}
	// The seeded interviewer is, by definition, an untrusted-input agent — so
	// its runs auto-approve nothing (internal/runner).
	agent := &agents.Agent{Slug: def.Slug, AllowedTools: def.AllowedTools}
	if !agent.UntrustedInput() {
		t.Error("the interviewer must be classified as untrusted input")
	}
}

// An install that never tuned the interviewer is narrowed at the next startup:
// this is the adoption path running in the direction that takes capability
// away, which is the one that matters for a security fix.
func TestInterviewerToolsNarrowedOnAnUntouchedInstall(t *testing.T) {
	want := currentInterviewerSeed(t)
	prev := previousSeedVersions[agents.InterviewerSlug]
	if len(prev) == 0 {
		t.Fatal("no previous interviewer version recorded; the narrowing can never reach an existing workspace")
	}
	if slices.Equal(prev[len(prev)-1].AllowedTools, want.AllowedTools) {
		t.Fatal("the recorded version already holds the current allowlist; it should record what came before")
	}

	existing := &agents.Agent{
		Slug:         agents.InterviewerSlug,
		Name:         "Requirements Interviewer",
		AllowedTools: prev[len(prev)-1].AllowedTools,
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{agents.InterviewerSlug: existing}}

	changed, err := adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil || !changed {
		t.Fatalf("adoptSeedDefaults() = %v, %v; want true, nil", changed, err)
	}
	if len(svc.saved) != 1 {
		t.Fatalf("wrote %d definitions, want 1", len(svc.saved))
	}
	if !slices.Equal(svc.saved[0].AllowedTools, want.AllowedTools) {
		t.Errorf("allowed_tools = %v, want %v", svc.saved[0].AllowedTools, want.AllowedTools)
	}
}

// A workspace that chose its own tools for the interviewer keeps them — the
// narrowing is an adoption, not an override.
func TestInterviewerToolsLeftAloneWhenEdited(t *testing.T) {
	want := currentInterviewerSeed(t)
	existing := &agents.Agent{
		Slug:         agents.InterviewerSlug,
		Name:         "Our Interviewer",
		AllowedTools: []string{"mcp__openv__get_artifact", "mcp__openv__record_candidate_need", "WebSearch"},
	}
	svc := &fakeAgentService{bySlug: map[string]*agents.Agent{agents.InterviewerSlug: existing}}

	changed, err := adoptSeedDefaults("org-1", existing, want, svc)
	if err != nil {
		t.Fatalf("adoptSeedDefaults() error: %v", err)
	}
	if changed || len(svc.saved) != 0 {
		t.Errorf("an edited allowlist was overwritten: changed=%v saves=%d", changed, len(svc.saved))
	}
}

// REQ-91 backfill: an agent from an install that predates mandatory allowlists
// is given one so it can still run — without a schema migration, and only ever
// narrowing (an empty list used to mean "every tool the CLI has").
func TestBackfillAllowedTools(t *testing.T) {
	t.Run("fills an empty list", func(t *testing.T) {
		existing := &agents.Agent{Slug: "legacy", Name: "Legacy", Provider: "claude-code"}
		svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"legacy": existing}}
		if err := backfillAllowedTools("org-1", existing, nil, svc); err != nil {
			t.Fatalf("backfillAllowedTools: %v", err)
		}
		if len(svc.saved) != 1 {
			t.Fatalf("wrote %d definitions, want 1", len(svc.saved))
		}
		if !slices.Equal(svc.saved[0].AllowedTools, agents.DefaultAllowedTools()) {
			t.Errorf("allowed_tools = %v, want %v", svc.saved[0].AllowedTools, agents.DefaultAllowedTools())
		}
		// Everything else about the agent is written back untouched.
		if svc.saved[0].Name != "Legacy" || svc.saved[0].Provider != "claude-code" {
			t.Errorf("backfill disturbed fields it does not own: %+v", svc.saved[0])
		}
	})

	t.Run("leaves an agent that has tools alone", func(t *testing.T) {
		existing := &agents.Agent{Slug: "fine", AllowedTools: []string{"mcp__openv__get_artifact"}}
		svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"fine": existing}}
		if err := backfillAllowedTools("org-1", existing, agents.DefaultAllowedTools(), svc); err != nil {
			t.Fatalf("backfillAllowedTools: %v", err)
		}
		if len(svc.saved) != 0 {
			t.Errorf("rewrote an agent that already had an allowlist: %+v", svc.saved)
		}
	})

	t.Run("never touches a locked agent", func(t *testing.T) {
		existing := &agents.Agent{Slug: "pinned", Locked: true}
		svc := &fakeAgentService{bySlug: map[string]*agents.Agent{"pinned": existing}}
		if err := backfillAllowedTools("org-1", existing, agents.DefaultAllowedTools(), svc); err != nil {
			t.Fatalf("backfillAllowedTools: %v", err)
		}
		if len(svc.saved) != 0 {
			t.Errorf("a locked agent was written: %+v", svc.saved)
		}
	})

	t.Run("prefers the seed's own tools", func(t *testing.T) {
		existing := &agents.Agent{Slug: agents.InterviewerSlug}
		svc := &fakeAgentService{bySlug: map[string]*agents.Agent{agents.InterviewerSlug: existing}}
		want := currentInterviewerSeed(t).AllowedTools
		if err := backfillAllowedTools("org-1", existing, want, svc); err != nil {
			t.Fatalf("backfillAllowedTools: %v", err)
		}
		if len(svc.saved) != 1 || !slices.Equal(svc.saved[0].AllowedTools, want) {
			t.Errorf("backfill did not use the seed's allowlist: %+v", svc.saved)
		}
	})
}

// Every seeded agent names its tools, or new workspaces would be provisioned
// with agents that cannot run.
func TestEverySeedNamesItsTools(t *testing.T) {
	for _, seed := range defaultAgents() {
		if len(agents.NonEmptyTools(seed.def.AllowedTools)) == 0 {
			t.Errorf("seeded agent %q has no allowed_tools", seed.def.Slug)
		}
		def := seed.def
		if err := def.Validate(); err != nil {
			t.Errorf("seeded agent %q is invalid: %v", seed.def.Slug, err)
		}
	}
}
