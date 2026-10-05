package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/openv/requirements-platform/internal/mcp"
)

// REQ-91: whichever CLI drives a run, the OpenV MCP server is told which of
// its own tools the agent may call. The value is the definition's allowlist,
// reduced to the OpenV tools in it.
func TestOpenVToolAllowlist(t *testing.T) {
	cases := []struct {
		name    string
		allowed []string
		want    string
	}{
		{"wildcard", []string{"mcp__openv__*", "Read"}, "*"},
		// Claude Code's server-wide form: naming the MCP server on its own
		// grants every tool it offers. Reading it as "a tool with an empty
		// name" would set OPENV_MCP_TOOLS to "" — which openv-mcp reads as
		// NO OpenV tools, the exact opposite of what was asked for.
		{"the bare server name is the wildcard too", []string{"mcp__openv", "Read"}, "*"},
		{"bare server name alone", []string{"mcp__openv"}, "*"},
		{"named tools", []string{"mcp__openv__get_artifact", "Bash", "mcp__openv__create_link"}, "get_artifact,create_link"},
		{"no openv tools at all", []string{"Read", "Edit"}, ""},
		{"blanks are not tools", []string{"mcp__openv__get_artifact", "  "}, "get_artifact"},
		// A tool from some other MCP server is not ours to serve.
		{"another server's tools", []string{"mcp__github__list_issues"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openvToolAllowlist(tc.allowed); got != tc.want {
				t.Errorf("openvToolAllowlist(%v) = %q, want %q", tc.allowed, got, tc.want)
			}
		})
	}
}

// The variable is always set, even to the empty string: openv-mcp reads
// "set but empty" as "no OpenV tools", where unset would mean "no filter" —
// so an agent that names none must not simply leave it off.
func TestWithOpenVToolFilterAlwaysSetsTheVariable(t *testing.T) {
	spec := RunSpec{
		AllowedTools: []string{"Read"},
		MCP:          MCPServerConfig{Env: map[string]string{"OPENV_RUN_TOKEN": "t"}},
	}
	got := withOpenVToolFilter(spec)
	if _, ok := got.MCP.Env[mcp.EnvToolAllowlist]; !ok {
		t.Fatalf("%s missing from the MCP env: %v", mcp.EnvToolAllowlist, got.MCP.Env)
	}
	if got.MCP.Env[mcp.EnvToolAllowlist] != "" {
		t.Errorf("%s = %q, want empty for an agent naming no OpenV tools", mcp.EnvToolAllowlist, got.MCP.Env[mcp.EnvToolAllowlist])
	}
	if got.MCP.Env["OPENV_RUN_TOKEN"] != "t" {
		t.Error("the run token was dropped from the MCP env")
	}
	// The caller's map is not mutated: specs are passed by value and a shared
	// map would leak one run's allowlist into another's.
	if _, ok := spec.MCP.Env[mcp.EnvToolAllowlist]; ok {
		t.Error("withOpenVToolFilter mutated the caller's env map")
	}
}

// splitToolScope keeps an entry's argument scope intact, so "Bash(git *)" is
// not silently read as plain "Bash".
func TestSplitToolScope(t *testing.T) {
	cases := []struct{ entry, name, scope string }{
		{"Bash(git *)", "Bash", "git *"},
		{"Read", "Read", ""},
		{" Edit ", "Edit", ""},
		{"mcp__openv__get_artifact", "mcp__openv__get_artifact", ""},
		{"Bash(", "Bash(", ""}, // malformed: not a scope, left alone
	}
	for _, tc := range cases {
		name, scope := splitToolScope(tc.entry)
		if name != tc.name || scope != tc.scope {
			t.Errorf("splitToolScope(%q) = %q, %q; want %q, %q", tc.entry, name, scope, tc.name, tc.scope)
		}
	}
}

// The claude adapter passes the allowlist as a flag, and still hands the MCP
// server its own copy: the two layers are independent, and the server-side one
// is what holds if a CLI flag is ever mis-parsed or dropped.
func TestClaudeMCPConfigCarriesTheToolFilter(t *testing.T) {
	spec := claudeSpec()
	spec = withOpenVToolFilter(spec)

	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := writeMCPConfig(path, spec.MCP); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mcp config: %v", err)
	}
	var cfg struct {
		McpServers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("mcp config not valid JSON: %v", err)
	}
	want := "get_artifact,record_candidate_need"
	if got := cfg.McpServers["openv"].Env[mcp.EnvToolAllowlist]; got != want {
		t.Errorf("%s = %q, want %q", mcp.EnvToolAllowlist, got, want)
	}
}

// #379 bug 185: what openv-mcp serves never exceeds what the allowlist names.
// The runner writes into OPENV_MCP_TOOLS only names the MCP server reads back
// as that same one tool. Before the fix an entry naming one odd tool, such as
// the server name behind the prefix, reached the server as a wildcard and
// every OpenV tool was served; on codex, which has no allowlist of its own,
// that was the agent's whole OpenV surface.
func TestOpenVToolFilterServesOnlyNamedTools(t *testing.T) {
	served := func(t *testing.T, allowed []string) []string {
		t.Helper()
		spec := withOpenVToolFilter(RunSpec{AllowedTools: allowed})
		t.Setenv(mcp.EnvToolAllowlist, spec.MCP.Env[mcp.EnvToolAllowlist])
		var names []string
		for _, tool := range mcp.EnvFilteredTools(mcp.Tools()) {
			names = append(names, tool.Name)
		}
		return names
	}

	// Every tool of the table still passes, bare of scope or scoped, and is
	// served alone: the rule is narrow, not a new way to lose a tool.
	for _, tool := range mcp.Tools() {
		for _, entry := range []string{"mcp__openv__" + tool.Name, "mcp__openv__" + tool.Name + "(read)"} {
			if got := served(t, []string{entry, "Read"}); !slices.Equal(got, []string{tool.Name}) {
				t.Errorf("allowlist %q served %q, want just %s", entry, got, tool.Name)
			}
		}
	}

	// Each of these names no tool of the table, so nothing is served.
	for _, entry := range []string{
		"mcp__openv__mcp__openv",                    // the server name behind the prefix
		"mcp__openv__mcp__openv__*",                 // the wildcard behind the prefix twice
		"mcp__openv__mcp__openv__get_artifact",      // the prefix twice
		"mcp__openv__ *",                            // a space before the wildcard
		"mcp__openv__ get_artifact",                 // a space before a name
		"mcp__openv__get_artifact,mcp__openv",       // a comma before the server name
		"mcp__openv__get_artifact,*",                // a comma before the wildcard
		"mcp__openv__get_artifact,create_artifact",  // a comma before another tool
		"mcp__openv__get_*",                         // a glob
		"mcp__openv__get_artifact(read",             // an unclosed scope
		"mcp__openv__get_artifact mcp__openv__list", // two names in one entry
	} {
		if got := served(t, []string{entry}); len(got) > 3 {
			t.Errorf("allowlist %q served %d tools, want none", entry, len(got))
		} else if len(got) != 0 {
			t.Errorf("allowlist %q served %q, want none", entry, got)
		}
	}

	// The two wildcard spellings still serve the whole table.
	for _, entry := range []string{"mcp__openv__*", "mcp__openv"} {
		if got := served(t, []string{entry}); len(got) != len(mcp.Tools()) {
			t.Errorf("allowlist %q served %d tools, want all %d", entry, len(got), len(mcp.Tools()))
		}
	}
}
