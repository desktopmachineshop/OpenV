package main

import "testing"

// cliName is the command cli_harness_test.go builds and runs.
const cliName = "openv-mcp"

// cliInitialize and cliToolsList are the JSON-RPC requests an MCP client
// opens with, one per line on stdin.
const (
	cliInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"s8","version":"0"}}}`
	cliToolsList  = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
)

// TestCLI snapshots openv-mcp's command line (refactor plan S8, invariant
// I14): it has no flags, so -h is the missing-credential refusal like no
// arguments; a credential with empty stdin exits cleanly; and a
// tools/list over stdin applies OPENV_MCP_TOOLS, including a set but empty
// value, which offers no tools. Listing tools calls no API: OPENV_API_URL
// points at a closed port. internal/mcp's goldens (S7) pin the tools and
// the JSON-RPC in full.
func TestCLI(t *testing.T) {
	session := cliInitialize + "\n" + cliToolsList + "\n"
	offline := []string{"OPENV_API_TOKEN=key-example", "OPENV_API_URL=http://127.0.0.1:9"}
	runCLI(t, []cliScenario{
		{name: "no_token"},
		{name: "help_no_token", args: []string{"-h"}},
		{name: "token_empty_stdin", env: []string{"OPENV_API_TOKEN=key-example"}},
		{name: "tools_allowlist", env: append(offline, "OPENV_MCP_TOOLS=list_projects"), stdin: session},
		{name: "tools_allowlist_empty", env: append(offline, "OPENV_MCP_TOOLS="), stdin: session},
	})
}
