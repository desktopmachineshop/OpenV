// Package mcp implements a stdio MCP server exposing OpenV tools to agent
// CLIs. The JSON-RPC 2.0 loop is hand-rolled (MCP spec 2024-11-05) to keep
// the binary dependency-free.
package mcp

import (
	"os"
	"slices"
	"strings"
)

// Tool is one MCP tool: schema plus handler. The table is exported so other
// hosts (e.g. an API loop) can reuse it.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(c *Client, args map[string]interface{}) (string, error)
}

// ServerTools is Claude Code's server-wide allowlist spelling: naming the MCP
// server on its own grants every tool that server offers. It is documented
// alongside the per-tool form, so an agent definition may carry either
// "mcp__openv" or "mcp__openv__*" to mean the whole OpenV surface, and both
// must be read the same way everywhere.
const ServerTools = "mcp__openv"

// ToolPrefix is what a vendor CLI's allowlist calls these tools: the MCP
// server is registered as "openv", so its tools are addressed as
// mcp__openv__<name>.
const ToolPrefix = ServerTools + "__"

// EnvToolAllowlist names the environment variable that narrows the tool set
// this MCP server exposes. It is the server's own half of REQ-91: a vendor CLI
// that has no per-run allowlist flag of its own (codex exec, and anything else
// that only knows how to spawn an MCP server) still cannot call an OpenV tool
// the agent definition did not name, because the tool is not there to call.
//
// The variable is read as *set or unset*, not empty or non-empty:
//
//   - unset — no filter; every tool in the table is served. This is how
//     openv-mcp behaves outside a platform run (a repository session with a
//     workspace runner key, say).
//   - "*", "mcp__openv__*" or the bare server name "mcp__openv" — the
//     wildcard spellings an agent definition may write; every tool is served.
//   - a comma-separated list — only those tools are served. Entries may be
//     bare ("get_artifact") or prefixed as a vendor CLI writes them
//     ("mcp__openv__get_artifact"); blanks are ignored.
//   - set but empty — no OpenV tool is served at all. That is deliberate: an
//     agent whose allowlist names no mcp__openv__ tool gets none, rather than
//     all of them.
const EnvToolAllowlist = "OPENV_MCP_TOOLS"

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

// readOnlyTools names every tool in Tools() that only reads: its handler
// issues GETs and changes nothing. It is the list an agent that must not write
// is granted (see the seeded interviewer in internal/seeds).
//
// A tool absent from this set is treated as a writer, so a tool added later
// grants nothing until someone deliberately lists it here. TestReadOnlyTools
// checks every name still exists in Tools().
var readOnlyTools = map[string]bool{
	"list_projects":           true,
	"list_artifacts":          true,
	"get_artifact":            true,
	"get_project_map":         true,
	"get_context":             true,
	"get_project_tree":        true,
	"search_artifacts":        true,
	"list_links_for_artifact": true,
	"list_baselines":          true,
	"get_baseline":            true,
	"get_quality_rules":       true,
	"get_quality_findings":    true,
	"get_vv_coverage":         true,
	"get_vv_gaps":             true,
	"list_work_items":         true,
	"get_work_item":           true,
	"get_work_item_history":   true,
}

// ReadOnly reports whether a tool only reads project data.
func ReadOnly(name string) bool { return readOnlyTools[strings.TrimPrefix(name, ToolPrefix)] }

// ReadOnlyToolNames returns the allowlist entries — prefixed as a vendor CLI
// wants them — for every read-only OpenV tool, in the table's own order so the
// result is stable.
func ReadOnlyToolNames() []string {
	var out []string
	for _, t := range Tools() {
		if readOnlyTools[t.Name] {
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
