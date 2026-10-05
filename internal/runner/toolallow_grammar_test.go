package runner

import (
	"os"
	"slices"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/mcp"
)

// Refactor step P4a pins the runner's reader of a tool allowlist,
// openvToolNames, and the OPENV_MCP_TOOLS value built from it, exactly as
// they read today, quirks included. This is the second of two grammars for
// the same spellings; the first is mcp.FilterTools
// (internal/mcp/toolfilter_grammar_test.go). Here an entry is an OpenV tool
// only with the mcp__openv__ prefix or as the bare server name, a "(scope)"
// is stripped, and a duplicate is dropped. Since #379 bug 185 a name is kept
// only when openv-mcp reads it back as that same one tool: lower-case
// letters, digits and underscores, and neither the server name nor holding
// the prefix. Merging the two grammars would change which OpenV tools an
// agent gets (REQ-91); TestAllowlistReadersAreTwoGrammars shows where they
// part.

func TestOpenVToolNamesGrammar(t *testing.T) {
	cases := []struct {
		name     string
		allowed  []string
		names    []string
		wildcard bool
	}{
		// Bare: an entry without the prefix is not an OpenV tool here.
		{"bare name is ignored", []string{"get_artifact"}, nil, false},
		{"bare wildcard is ignored", []string{"*"}, nil, false},
		{"vendor tools are ignored", []string{"Read", "Bash(git *)", "WebFetch"}, nil, false},

		// Prefixed: the prefix is stripped once, case-sensitively, and the
		// rest is kept only as a tool-name token (bug 185).
		{"prefixed name", []string{"mcp__openv__get_artifact"}, []string{"get_artifact"}, false},
		{"names keep the allowlist's order", []string{"mcp__openv__create_link", "Read", "mcp__openv__get_artifact"}, []string{"create_link", "get_artifact"}, false},
		{"an unknown name passes through", []string{"mcp__openv__no_such_tool"}, []string{"no_such_tool"}, false},
		{"the prefix twice names nothing (bug 185)", []string{"mcp__openv__mcp__openv__get_artifact"}, nil, false},
		{"the prefix inside the name names nothing (bug 185)", []string{"mcp__openv__get_mcp__openv__artifact"}, nil, false},
		{"digits are part of a name", []string{"mcp__openv__tool_2"}, []string{"tool_2"}, false},
		{"capitals name nothing (bug 185)", []string{"mcp__openv__Get_Artifact"}, nil, false},
		{"the prefix is case-sensitive", []string{"MCP__OPENV__get_artifact"}, nil, false},
		{"the prefix alone names nothing", []string{"mcp__openv__"}, nil, false},
		{"space after the prefix names nothing (bug 185)", []string{"mcp__openv__ get_artifact"}, nil, false},
		{"another server's tool", []string{"mcp__github__list_issues"}, nil, false},
		{"a server named like openv", []string{"mcp__openvpn__connect"}, nil, false},

		// Wildcard: only the prefixed "*"; it does not stop the names.
		{"prefixed wildcard", []string{"mcp__openv__*"}, nil, true},
		{"the wildcard keeps the names around it", []string{"mcp__openv__get_artifact", "mcp__openv__*", "mcp__openv__create_link"}, []string{"get_artifact", "create_link"}, true},
		{"a glob names nothing (bug 185)", []string{"mcp__openv__get_*"}, nil, false},
		{"space after the prefix spoils the wildcard and names nothing (bug 185)", []string{"mcp__openv__ *"}, nil, false},
		{"the wildcard behind the prefix twice names nothing (bug 185)", []string{"mcp__openv__mcp__openv__*"}, nil, false},
		{"another server's wildcard", []string{"mcp__github__*"}, nil, false},

		// Server name: "mcp__openv" on its own is the wildcard.
		{"server name", []string{"mcp__openv"}, nil, true},
		{"padded server name", []string{"  mcp__openv  "}, nil, true},
		{"server name behind the prefix names nothing (bug 185)", []string{"mcp__openv__mcp__openv"}, nil, false},
		{"a server named like openv is not the server", []string{"mcp__openvpn"}, nil, false},

		// Scoped: "(scope)" is stripped, on any spelling.
		{"scope is stripped", []string{"mcp__openv__get_artifact(read)"}, []string{"get_artifact"}, false},
		{"scope with spaces", []string{" mcp__openv__get_artifact ( read ) "}, []string{"get_artifact"}, false},
		{"scoped wildcard", []string{"mcp__openv__*(read)"}, nil, true},
		{"scoped server name", []string{"mcp__openv(read)"}, nil, true},
		{"an unclosed scope names nothing (bug 185)", []string{"mcp__openv__get_artifact(read"}, nil, false},
		{"a scope starts at the first parenthesis", []string{"mcp__openv__get_artifact(a)(b)"}, []string{"get_artifact"}, false},

		// Blank: entries are trimmed, and blank ones skipped.
		{"blank entries are skipped", []string{"", "  ", "mcp__openv__get_artifact", "\t"}, []string{"get_artifact"}, false},
		{"padded entries are trimmed", []string{" mcp__openv__get_artifact\n"}, []string{"get_artifact"}, false},
		{"only blanks", []string{"", " "}, nil, false},

		// Duplicate: dropped, the first mention keeping its place.
		{"duplicates are dropped", []string{"mcp__openv__create_link", "mcp__openv__get_artifact", "mcp__openv__create_link"}, []string{"create_link", "get_artifact"}, false},
		{"a scoped duplicate is a duplicate", []string{"mcp__openv__get_artifact", "mcp__openv__get_artifact(read)"}, []string{"get_artifact"}, false},
		{"duplicate wildcards", []string{"mcp__openv", "mcp__openv__*", "mcp__openv"}, nil, true},

		// Empty: no entries name nothing, nil or not.
		{"nil", nil, nil, false},
		{"empty", []string{}, nil, false},

		// A comma is not a separator here, and a name holding one is no
		// tool name (bug 185).
		{"a comma inside one entry names nothing (bug 185)", []string{"mcp__openv__get_artifact,create_link"}, nil, false},
		{"a comma before the server name names nothing (bug 185)", []string{"mcp__openv__get_artifact,mcp__openv"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names, wildcard := openvToolNames(tc.allowed)
			if !slices.Equal(names, tc.names) || wildcard != tc.wildcard {
				t.Errorf("openvToolNames(%q) = %q, %v; want %q, %v", tc.allowed, names, wildcard, tc.names, tc.wildcard)
			}
		})
	}
}

// The OPENV_MCP_TOOLS value is "*" for a wildcard, whatever names came with
// it, and otherwise the names joined with commas. withOpenVToolFilter always
// sets it: an allowlist naming no OpenV tool sets it to the empty string,
// which openv-mcp reads as no tools, where leaving it unset would mean all.
func TestOpenVToolAllowlistValue(t *testing.T) {
	const env = "OPENV_MCP_TOOLS"
	cases := []struct {
		name    string
		allowed []string
		want    string
	}{
		{"nil sets it empty", nil, ""},
		{"empty sets it empty", []string{}, ""},
		{"blanks set it empty", []string{"", " "}, ""},
		{"no OpenV tool sets it empty", []string{"Read", "get_artifact", "*"}, ""},
		{"names, in the allowlist's order", []string{"mcp__openv__create_link", "mcp__openv__get_artifact(read)"}, "create_link,get_artifact"},
		{"the wildcard drops the names", []string{"mcp__openv__get_artifact", "mcp__openv__*"}, "*"},
		{"the server name drops the names", []string{"mcp__openv", "mcp__openv__get_artifact"}, "*"},
		{"duplicates once", []string{"mcp__openv__get_artifact", "mcp__openv__get_artifact"}, "get_artifact"},
		{"odd names are left out (bug 185)", []string{"mcp__openv__ get_artifact", "mcp__openv__mcp__openv"}, ""},
		{"only the well-formed name is written (bug 185)", []string{"mcp__openv__get_artifact,mcp__openv", "mcp__openv__get_*", "mcp__openv__create_link"}, "create_link"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openvToolAllowlist(tc.allowed); got != tc.want {
				t.Errorf("openvToolAllowlist(%q) = %q, want %q", tc.allowed, got, tc.want)
			}
			spec := withOpenVToolFilter(RunSpec{AllowedTools: tc.allowed})
			got, set := spec.MCP.Env[env]
			if !set || got != tc.want {
				t.Errorf("withOpenVToolFilter(%q): %s = %q (set %v), want %q, set", tc.allowed, env, got, set, tc.want)
			}
		})
	}
}

// The same allowlists through the readers, side by side, over a stand-in
// table of three tools. served is what openv-mcp serves today: the runner's
// grammar builds OPENV_MCP_TOOLS and the MCP server's grammar reads it.
// direct is what mcp.FilterTools serves when handed the allowlist itself,
// that is, the MCP grammar alone. Rows where the two differ are why the
// readers are not merged (REQ-91). outside is agents.ToolsReachOutside, the
// third reader, which only asks whether a tool reaches past the workspace.
//
// The last rows were round trips where the runner's name, read again by the
// MCP grammar, became a wildcard, so an entry naming one odd tool got every
// OpenV tool. Since #379 bug 185 the runner writes only names the MCP
// grammar reads back as that same tool, so they serve nothing.
func TestAllowlistReadersAreTwoGrammars(t *testing.T) {
	const env = "OPENV_MCP_TOOLS"
	table := []mcp.Tool{{Name: "list_projects"}, {Name: "get_artifact"}, {Name: "create_link"}}
	all := []string{"list_projects", "get_artifact", "create_link"}
	cases := []struct {
		name    string
		allowed []string
		served  []string
		direct  []string
		outside bool
	}{
		{"bare name", []string{"get_artifact"}, nil, []string{"get_artifact"}, false},
		{"bare wildcard", []string{"*"}, nil, all, false},
		{"prefixed name", []string{"mcp__openv__get_artifact"}, []string{"get_artifact"}, []string{"get_artifact"}, false},
		{"prefixed wildcard", []string{"mcp__openv__*"}, all, all, false},
		{"server name", []string{"mcp__openv"}, all, all, false},
		{"scoped name", []string{"mcp__openv__get_artifact(read)"}, []string{"get_artifact"}, nil, false},
		{"scoped server name", []string{"mcp__openv(read)"}, all, nil, false},
		{"blank", []string{"", " "}, nil, nil, false},
		{"duplicates", []string{"mcp__openv__create_link", "mcp__openv__get_artifact", "mcp__openv__create_link"}, []string{"get_artifact", "create_link"}, []string{"get_artifact", "create_link"}, false},
		{"set but empty", nil, nil, nil, false},
		{"a vendor shell and a name", []string{"Bash(git *)", "mcp__openv__get_artifact"}, []string{"get_artifact"}, []string{"get_artifact"}, true},
		{"another server's tool", []string{"mcp__github__list_issues"}, nil, nil, true},
		{"the prefix twice (bug 185)", []string{"mcp__openv__mcp__openv__get_artifact"}, nil, nil, false},
		{"space after the prefix (bug 185)", []string{"mcp__openv__ get_artifact"}, nil, nil, false},
		// Round trips that widened to every tool before bug 185.
		{"server name behind the prefix (bug 185)", []string{"mcp__openv__mcp__openv"}, nil, nil, false},
		{"wildcard behind the prefix twice (bug 185)", []string{"mcp__openv__mcp__openv__*"}, nil, nil, false},
		{"space before the wildcard (bug 185)", []string{"mcp__openv__ *"}, nil, nil, false},
		{"a comma before the server name (bug 185)", []string{"mcp__openv__get_artifact,mcp__openv"}, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(env, openvToolAllowlist(tc.allowed))
			served := toolTableNames(mcp.EnvFilteredTools(table))
			if !slices.Equal(served, tc.served) {
				t.Errorf("served for %q = %q, want %q", tc.allowed, served, tc.served)
			}
			direct := toolTableNames(mcp.FilterTools(table, tc.allowed))
			if !slices.Equal(direct, tc.direct) {
				t.Errorf("mcp.FilterTools(%q) = %q, want %q", tc.allowed, direct, tc.direct)
			}
			if got := agents.ToolsReachOutside(tc.allowed); got != tc.outside {
				t.Errorf("agents.ToolsReachOutside(%q) = %v, want %v", tc.allowed, got, tc.outside)
			}
		})
	}
	// Unset is the one value the runner never writes; it would serve all.
	t.Run("unset serves everything", func(t *testing.T) {
		t.Setenv(env, "")
		os.Unsetenv(env)
		if got := toolTableNames(mcp.EnvFilteredTools(table)); !slices.Equal(got, all) {
			t.Errorf("served with %s unset = %q, want %q", env, got, all)
		}
	})
}

// toolTableNames lists the names of an MCP tool table, in its order.
func toolTableNames(tools []mcp.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}
