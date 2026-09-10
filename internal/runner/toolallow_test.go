package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
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
