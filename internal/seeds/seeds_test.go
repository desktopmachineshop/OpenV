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
