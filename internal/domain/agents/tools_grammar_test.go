package agents

import "testing"

// Refactor step P4a pins the domain's reader of a tool allowlist,
// ToolsReachOutside, exactly as it reads today, quirks included. It shares
// the OpenV spellings with mcp.FilterTools and the runner's openvToolNames
// but keeps its own copies of them (openvServerTools, openvToolPrefix) and
// asks a different question: does any entry reach content nobody in the
// workspace wrote. A "(scope)" is cut at the first parenthesis, closed or
// not; the vendor names compare in any case, the mcp__ prefixes only in
// lower case.
func TestToolsReachOutsideGrammar(t *testing.T) {
	cases := []struct {
		name  string
		tools []string
		want  bool
	}{
		// Bare: vendor tool names, and an OpenV tool name without its prefix.
		{"a bare OpenV tool name", []string{"get_artifact"}, false},
		{"local file tools", []string{"Read", "Grep", "Glob", "Edit", "Write"}, false},
		{"a shell", []string{"Bash"}, true},
		{"a shell in any case", []string{"bASH"}, true},
		{"web fetch", []string{"WebFetch"}, true},
		{"web search in any case", []string{"WEBSEARCH"}, true},

		// Prefixed: everything behind the OpenV prefix is OpenV's own.
		{"an OpenV tool", []string{"mcp__openv__get_artifact"}, false},
		{"anything behind the OpenV prefix", []string{"mcp__openv__mcp__github__search_code"}, false},
		{"the OpenV prefix alone", []string{"mcp__openv__"}, false},
		{"the OpenV prefix in capitals is no MCP tool", []string{"MCP__OPENV__get_artifact"}, false},
		{"another server's tool", []string{"mcp__github__search_code"}, true},
		{"another server's tool in capitals is no MCP tool", []string{"MCP__github__search_code"}, false},
		{"the mcp__ prefix alone is another server", []string{"mcp__"}, true},

		// Wildcard.
		{"the OpenV wildcard", []string{"mcp__openv__*"}, false},
		{"a bare wildcard", []string{"*"}, false},
		{"another server's wildcard", []string{"mcp__github__*"}, true},

		// Server name.
		{"the OpenV server name", []string{"mcp__openv"}, false},
		{"the padded OpenV server name", []string{"  mcp__openv  "}, false},
		{"a server named like openv", []string{"mcp__openvpn"}, true},
		{"another server's name", []string{"mcp__github"}, true},

		// Scoped: the scope is dropped before matching.
		{"a scoped OpenV tool", []string{"mcp__openv__get_artifact(read)"}, false},
		{"the scoped OpenV server name", []string{"mcp__openv(read)"}, false},
		{"a scoped shell, colon form", []string{"Bash(git:*)"}, true},
		{"a scoped shell, space before the scope", []string{"Bash (git *)"}, true},
		{"a scoped web fetch", []string{"WebFetch(domain:example.com)"}, true},
		{"a scoped tool of another server", []string{"mcp__github__search_code(x)"}, true},
		{"an unclosed scope still ends the name", []string{"Bash(git"}, true},
		{"a scope starts at the first parenthesis", []string{"mcp__openv__get_artifact(Bash)"}, false},

		// Blank: entries are trimmed, and blank ones match nothing.
		{"blank entries", []string{"", "  ", "\t"}, false},
		{"a padded shell", []string{"  Bash  "}, true},

		// Duplicate: one entry that reaches outside is enough, wherever it is.
		{"duplicate OpenV tools", []string{"mcp__openv__get_artifact", "mcp__openv__get_artifact"}, false},
		{"duplicate shells", []string{"Bash", "Bash"}, true},
		{"one outside entry after OpenV ones", []string{"mcp__openv__*", "Read", "WebSearch"}, true},

		// Empty: nil, empty and all-blank read the same.
		{"nil", nil, false},
		{"empty", []string{}, false},

		// A comma is not a separator here.
		{"a comma inside one entry", []string{"mcp__openv__get_artifact,Bash"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolsReachOutside(tc.tools); got != tc.want {
				t.Errorf("ToolsReachOutside(%q) = %v, want %v", tc.tools, got, tc.want)
			}
		})
	}
}
