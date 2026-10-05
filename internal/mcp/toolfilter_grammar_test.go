package mcp

import (
	"os"
	"slices"
	"testing"
)

// Refactor step P4a pins the MCP server's reader of a tool allowlist,
// FilterTools and EnvFilteredTools, exactly as it reads today, quirks
// included. It is one of two grammars for the same spellings: the runner's
// openvToolNames (internal/runner/toolallow_grammar_test.go) reads them
// differently, and merging the two would change which OpenV tools an agent
// gets (REQ-91). Where a row below looks surprising, it is today's answer,
// not a proposal.
//
// The pins use a fixed stand-in table rather than Tools(), so adding a tool
// moves none of them, and literal spellings rather than the constants, so
// the constants' values are pinned as well.

// grammarTable is the stand-in tool table, in its own order.
func grammarTable() []Tool {
	return []Tool{{Name: "list_projects"}, {Name: "get_artifact"}, {Name: "create_link"}}
}

// grammarAll is every name of grammarTable, in table order.
var grammarAll = []string{"list_projects", "get_artifact", "create_link"}

// The allowlist spellings the readers key on. P4b moves the constants to
// internal/mcp/toolnames behind aliases; their values must not move.
func TestAllowlistConstants(t *testing.T) {
	for _, c := range []struct{ name, got, want string }{
		{"ServerTools", ServerTools, "mcp__openv"},
		{"ToolPrefix", ToolPrefix, "mcp__openv__"},
		{"EnvToolAllowlist", EnvToolAllowlist, "OPENV_MCP_TOOLS"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestFilterToolsGrammar(t *testing.T) {
	cases := []struct {
		name  string
		allow []string
		want  []string
	}{
		// Bare: a name with no prefix is an OpenV tool name.
		{"bare name", []string{"get_artifact"}, []string{"get_artifact"}},
		{"bare names come back in table order", []string{"create_link", "list_projects"}, []string{"list_projects", "create_link"}},
		{"bare unknown name", []string{"delete_everything"}, nil},
		{"bare names are case-sensitive", []string{"Get_Artifact"}, nil},
		{"vendor tools match nothing", []string{"Read", "Bash", "WebFetch"}, nil},

		// Prefixed: the prefix is stripped once, case-sensitively.
		{"prefixed name", []string{"mcp__openv__get_artifact"}, []string{"get_artifact"}},
		{"bare and prefixed mixed", []string{"mcp__openv__create_link", "get_artifact"}, []string{"get_artifact", "create_link"}},
		{"the prefix is stripped once", []string{"mcp__openv__mcp__openv__get_artifact"}, nil},
		{"the prefix is case-sensitive", []string{"MCP__OPENV__get_artifact"}, nil},
		{"the prefix alone", []string{"mcp__openv__"}, nil},
		{"space after the prefix is kept", []string{"mcp__openv__ get_artifact"}, nil},
		{"another server's tool of the same name", []string{"mcp__github__get_artifact"}, nil},

		// Wildcard: "*", bare or prefixed, serves the whole table.
		{"bare wildcard", []string{"*"}, grammarAll},
		{"prefixed wildcard", []string{"mcp__openv__*"}, grammarAll},
		{"padded wildcard", []string{" * "}, grammarAll},
		{"wildcard after a name", []string{"get_artifact", "*"}, grammarAll},
		{"wildcard among vendor tools", []string{"Read", "mcp__openv__*"}, grammarAll},
		{"space after the prefix spoils the wildcard", []string{"mcp__openv__ *"}, nil},
		{"a glob is not a wildcard", []string{"get_*"}, nil},
		{"another server's wildcard", []string{"mcp__github__*"}, nil},

		// Server name: "mcp__openv" on its own serves the whole table.
		{"server name", []string{"mcp__openv"}, grammarAll},
		{"padded server name", []string{"  mcp__openv  "}, grammarAll},
		{"server name after a name", []string{"get_artifact", "mcp__openv"}, grammarAll},
		{"server name behind the prefix", []string{"mcp__openv__mcp__openv"}, nil},
		{"a server named like openv", []string{"mcp__openvpn"}, nil},

		// Scoped: a "(scope)" is not stripped, so the entry matches no tool.
		{"scoped prefixed name", []string{"mcp__openv__get_artifact(read)"}, nil},
		{"scoped bare name", []string{"get_artifact(read)"}, nil},
		{"scoped wildcard", []string{"mcp__openv__*(read)"}, nil},
		{"scoped server name", []string{"mcp__openv(read)"}, nil},

		// Blank: entries are trimmed, and blank ones skipped.
		{"blank entries are skipped", []string{"", " ", "get_artifact", "\t"}, []string{"get_artifact"}},
		{"padded entries are trimmed", []string{" get_artifact ", "\tmcp__openv__create_link\n"}, []string{"get_artifact", "create_link"}},
		{"only blanks serve nothing", []string{"", "  "}, nil},

		// Duplicate: naming a tool twice, in either spelling, serves it once.
		{"duplicates", []string{"get_artifact", "mcp__openv__get_artifact", "get_artifact"}, []string{"get_artifact"}},
		{"duplicate wildcards", []string{"*", "mcp__openv", "*"}, grammarAll},

		// Empty: no entries serve nothing; FilterTools cannot tell nil from
		// empty (EnvFilteredTools is where unset differs from set but empty).
		{"nil serves nothing", nil, nil},
		{"empty serves nothing", []string{}, nil},

		// A comma is not a separator here: EnvFilteredTools splits first.
		{"a comma inside one entry", []string{"get_artifact,create_link"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolNames(FilterTools(grammarTable(), tc.allow))
			if !slices.Equal(got, tc.want) {
				t.Errorf("FilterTools(%q) = %q, want %q", tc.allow, got, tc.want)
			}
		})
	}
}

// OPENV_MCP_TOOLS is read as set or unset: unset serves everything, set but
// empty serves nothing. A set value is split on commas and read by
// FilterTools.
func TestEnvFilteredToolsGrammar(t *testing.T) {
	const env = "OPENV_MCP_TOOLS"
	cases := []struct {
		name  string
		set   bool
		value string
		want  []string
	}{
		{"unset serves the whole table", false, "", grammarAll},
		{"set but empty serves nothing", true, "", nil},
		{"set to a blank serves nothing", true, " ", nil},
		{"set to commas serves nothing", true, " , ,", nil},
		{"bare wildcard", true, "*", grammarAll},
		{"prefixed wildcard", true, "mcp__openv__*", grammarAll},
		{"server name", true, "mcp__openv", grammarAll},
		{"a list, bare and prefixed", true, "create_link,mcp__openv__get_artifact", []string{"get_artifact", "create_link"}},
		{"a list with spaces and blanks", true, " get_artifact , ,create_link ", []string{"get_artifact", "create_link"}},
		{"a wildcard anywhere in the list", true, "get_artifact,*", grammarAll},
		{"duplicates", true, "get_artifact,get_artifact", []string{"get_artifact"}},
		{"scoped", true, "mcp__openv__get_artifact(read)", nil},
		{"a comma inside a scope splits the entry", true, "mcp__openv__get_artifact(a,b)", nil},
		{"a semicolon is not a separator", true, "get_artifact;create_link", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(env, tc.value) // restores the original value afterwards
			if !tc.set {
				os.Unsetenv(env)
			}
			got := toolNames(EnvFilteredTools(grammarTable()))
			if !slices.Equal(got, tc.want) {
				t.Errorf("%s set=%v %q: served %q, want %q", env, tc.set, tc.value, got, tc.want)
			}
		})
	}
}
