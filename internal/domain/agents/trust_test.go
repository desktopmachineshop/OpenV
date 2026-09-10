package agents

import "testing"

// REQ-91: every agent definition must name the tools it may use. An empty
// list used to mean "the vendor CLI gets all of them", which is the hazard.
func TestValidateRequiresAllowedTools(t *testing.T) {
	base := func() *Definition {
		return &Definition{Slug: "an-agent", Name: "An Agent", Provider: "claude-code"}
	}
	for _, tools := range [][]string{nil, {}, {""}, {"   "}} {
		def := base()
		def.AllowedTools = tools
		err := def.Validate()
		if err == nil {
			t.Fatalf("Validate accepted allowed_tools %q", tools)
		}
		if err.Error() != AllowedToolsRequired {
			t.Errorf("Validate(%q) = %q, want the shared wording", tools, err)
		}
	}

	def := base()
	def.AllowedTools = []string{" mcp__openv__* ", "", "WebFetch"}
	if err := def.Validate(); err != nil {
		t.Fatalf("Validate rejected a real allowlist: %v", err)
	}
	// Blank entries are dropped and the rest trimmed, so what is stored is
	// what is passed to the CLI.
	if len(def.AllowedTools) != 2 || def.AllowedTools[0] != "mcp__openv__*" {
		t.Errorf("allowed_tools = %q, want the trimmed non-empty entries", def.AllowedTools)
	}
}

func TestUntrustedInput(t *testing.T) {
	cases := []struct {
		name  string
		agent *Agent
		want  bool
	}{
		{"nil", nil, false},
		{"openv only", &Agent{Slug: "vv-engineer", AllowedTools: []string{"mcp__openv__*"}}, false},
		{"openv tools and local file reads", &Agent{Slug: "reviewer", AllowedTools: []string{"mcp__openv__*", "Read", "Bash(git *)"}}, false},
		{"the interviewer", &Agent{Slug: InterviewerSlug, AllowedTools: []string{"mcp__openv__get_artifact"}}, true},
		{"repo access", &Agent{Slug: "developer", RepoAccess: true, AllowedTools: []string{"mcp__openv__*"}}, true},
		{"web fetch", &Agent{Slug: "a", AllowedTools: []string{"mcp__openv__*", "WebFetch"}}, true},
		{"web search, oddly cased", &Agent{Slug: "a", AllowedTools: []string{"websearch"}}, true},
		{"a foreign MCP server", &Agent{Slug: "a", AllowedTools: []string{"mcp__github__search_code"}}, true},
		{"an openv MCP tool is not foreign", &Agent{Slug: "a", AllowedTools: []string{"mcp__openv__get_context"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.UntrustedInput(); got != tc.want {
				t.Errorf("UntrustedInput() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A definition already on disk from before allowlists were mandatory still
// loads: refusing it would drop the agent out of the registry on the next
// sync. It is backfilled to the OpenV tools instead, which narrows it. Content
// somebody is saving now is refused rather than quietly fixed.
func TestParseFileAndSyncedFileDisagreeOnMissingTools(t *testing.T) {
	const legacy = "---\nslug: legacy\nname: Legacy\nprovider: claude-code\n---\nBody.\n"

	if _, err := ParseFile(legacy); err == nil {
		t.Error("ParseFile accepted content with no allowed_tools; a new save must be refused")
	}

	def, err := parseSyncedFile(legacy)
	if err != nil {
		t.Fatalf("parseSyncedFile: %v", err)
	}
	if len(def.AllowedTools) != 1 || def.AllowedTools[0] != "mcp__openv__*" {
		t.Errorf("allowed_tools = %q, want the default OpenV allowlist", def.AllowedTools)
	}
}
