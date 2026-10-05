// Package mcp implements a stdio MCP server exposing OpenV tools to agent
// CLIs. The JSON-RPC 2.0 loop is hand-rolled (MCP spec 2024-11-05) to keep
// the binary dependency-free.
package mcp

import (
	"os"
	"slices"
	"strings"

	"github.com/openv/requirements-platform/internal/mcp/toolnames"
)

// Tool is one MCP tool: schema plus handler. The table is exported so other
// hosts (e.g. an API loop) can reuse it.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(c *Client, args map[string]interface{}) (string, error)
	// ReadOnly marks a tool that only reads: its handler issues GETs and
	// changes nothing. The read-only tools are the list an agent that must not
	// write is granted (see the seeded interviewer in internal/seeds).
	//
	// A tool that leaves it false is treated as a writer, so a tool added later
	// grants nothing until someone deliberately marks it where it is defined.
	// tools/list serves only the name, description and input schema, so the
	// flag never reaches the wire.
	ReadOnly bool
}

// ServerTools is toolnames.ServerTools, the server-wide allowlist spelling
// "mcp__openv", kept here as an alias.
const ServerTools = toolnames.ServerTools

// ToolPrefix is toolnames.ToolPrefix, the "mcp__openv__" a vendor CLI's
// allowlist puts before each of these tools, kept here as an alias.
const ToolPrefix = toolnames.ToolPrefix

// EnvToolAllowlist is toolnames.EnvToolAllowlist, the variable that narrows
// the tools this server exposes (see EnvFilteredTools), kept here as an alias.
const EnvToolAllowlist = toolnames.EnvToolAllowlist

// toolWildcard is the "every tool" entry, accepted bare or prefixed.
const toolWildcard = "*"

// FilterTools returns the subset of tools the allowlist admits. A nil allow
// slice is not "allow nothing" — callers that mean "no filter" pass the whole
// table back themselves; see EnvFilteredTools.
func FilterTools(tools []Tool, allow []string) []Tool {
	want := make(map[string]bool, len(allow))
	for _, name := range allow {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name == ServerTools {
			// The server-wide form: every tool this server offers.
			return tools
		}
		name = strings.TrimPrefix(name, ToolPrefix)
		if name == toolWildcard {
			return tools
		}
		want[name] = true
	}
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if want[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// EnvFilteredTools applies EnvToolAllowlist to the tool table. With the
// variable unset the table is returned unchanged.
func EnvFilteredTools(tools []Tool) []Tool {
	raw, ok := os.LookupEnv(EnvToolAllowlist)
	if !ok {
		return tools
	}
	return FilterTools(tools, strings.Split(raw, ","))
}

// ReadOnly reports whether a tool only reads project data: whether Tools()
// holds a tool of that name, bare or prefixed, marked ReadOnly.
func ReadOnly(name string) bool {
	name = strings.TrimPrefix(name, ToolPrefix)
	for _, t := range Tools() {
		if t.Name == name {
			return t.ReadOnly
		}
	}
	return false
}

// ReadOnlyToolNames returns the allowlist entries — prefixed as a vendor CLI
// wants them — for every read-only OpenV tool, in the table's own order so the
// result is stable.
func ReadOnlyToolNames() []string {
	var out []string
	for _, t := range Tools() {
		if t.ReadOnly {
			out = append(out, ToolPrefix+t.Name)
		}
	}
	return out
}

// Tools returns the OpenV tool table.
func Tools() []Tool {
	return slices.Concat(
		projectTools(),
		artifactReadTools(),
		artifactOverviewTools(),
		artifactSearchTools(),
		artifactCreateTools(),
		artifactUpdateTools(),
		linkTools(),
		commentTools(),
		baselineTools(),
		reviewTools(),
		testRunTools(),
		qualityTools(),
		vvTools(),
		workItemListTools(),
		workItemTools(),
		interviewTools(),
		delegationTools(),
	)
}
