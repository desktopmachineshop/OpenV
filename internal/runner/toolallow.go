package runner

import (
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/mcp"
)

// This file translates an agent definition's tool allowlist — which is written
// in Claude Code's vocabulary, because that is the vocabulary the platform's
// own agents were authored in — into what each vendor CLI can actually apply.
//
// The rule the platform holds to (REQ-91) is that a CLI never starts with an
// implicit "all tools" allowance. Where a CLI has no allowlist of its own, the
// allowlist is still enforced for the tools the platform owns: openv-mcp reads
// OPENV_MCP_TOOLS and serves only the tools named there, so an agent cannot
// call an OpenV tool its definition left out no matter which CLI drives it.
// The vendor's own tools are then held by the CLI's confinement — codex's
// sandbox, gemini's tools.core allowlist — rather than refused outright.

// splitToolScope separates an allowlist entry's tool name from its
// parenthesised argument scope: "Bash(git *)" -> "Bash", "git *". An entry
// with no scope returns an empty scope.
func splitToolScope(entry string) (name, scope string) {
	entry = strings.TrimSpace(entry)
	open := strings.Index(entry, "(")
	if open < 0 || !strings.HasSuffix(entry, ")") {
		return entry, ""
	}
	return strings.TrimSpace(entry[:open]), strings.TrimSpace(entry[open+1 : len(entry)-1])
}

// openvToolNames returns the OpenV MCP tools an allowlist names, bare (the
// mcp__openv__ prefix stripped), plus whether the list carried the
// mcp__openv__* wildcard.
func openvToolNames(allowed []string) (names []string, wildcard bool) {
	for _, entry := range agents.NonEmptyTools(allowed) {
		name, _ := splitToolScope(entry)
		if !strings.HasPrefix(name, mcp.ToolPrefix) {
			continue
		}
		name = strings.TrimPrefix(name, mcp.ToolPrefix)
		if name == "*" {
			wildcard = true
			continue
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names, wildcard
}

// openvToolAllowlist is the OPENV_MCP_TOOLS value for a spec: "*" when the
// definition carries the wildcard, otherwise the OpenV tools it names. An
// agent that names none gets the empty string, which openv-mcp reads as "no
// OpenV tools" rather than "no filter" — the variable being set at all is what
// switches filtering on.
func openvToolAllowlist(allowed []string) string {
	names, wildcard := openvToolNames(allowed)
	if wildcard {
		return "*"
	}
	return strings.Join(names, ",")
}

// withOpenVToolFilter returns the spec with OPENV_MCP_TOOLS added to the MCP
// server's environment, so the OpenV MCP server itself enforces the agent's
// allowlist over its own tools. Every adapter applies it, because every path
// to the MCP server's environment carries it: claude writes it into the 0600
// mcp.json, codex forwards it by name through env_vars, gemini resolves it as
// a ${VAR} reference — the same route the run token already takes.
//
// The value is not a secret, so nothing here changes where secrets may appear.
func withOpenVToolFilter(spec RunSpec) RunSpec {
	env := make(map[string]string, len(spec.MCP.Env)+1)
	for k, v := range spec.MCP.Env {
		env[k] = v
	}
	env[mcp.EnvToolAllowlist] = openvToolAllowlist(spec.AllowedTools)
	spec.MCP.Env = env
	return spec
}

// geminiBuiltinTools maps a Claude-shaped tool name to the gemini CLI's own
// built-in tool names, as listed in the gemini-cli tool reference
// (https://google-gemini.github.io/gemini-cli/docs/tools/). Where a tool has
// been renamed across CLI releases both spellings are emitted: an entry in
// tools.core that matches no registered tool simply enables nothing, so
// naming the old name alongside the new one costs nothing and keeps an agent
// working across gemini versions.
//
// A Claude tool with no gemini equivalent maps to nothing and is dropped, so
// the translation only ever narrows.
var geminiBuiltinTools = map[string][]string{
	"Read":      {"read_file"},
	"ReadFile":  {"read_file"},
	"Write":     {"write_file"},
	"Edit":      {"replace", "edit"},
	"MultiEdit": {"replace", "edit"},
	"Glob":      {"glob"},
	"Grep":      {"grep_search", "search_file_content"},
	"LS":        {"list_directory"},
	"Bash":      {"run_shell_command"},
	"WebFetch":  {"web_fetch"},
	"WebSearch": {"google_web_search"},
	"TodoWrite": {"write_todos"},
}

// geminiToolSettings translates an allowlist into the two gemini settings that
// restrict what the model may call:
//
//   - core   -> tools.core, "Restrict the set of built-in tools with an
//     allowlist". Unset means every built-in tool; an empty
//     array means none, which is what an agent that names only
//     OpenV tools gets.
//   - include -> mcpServers.openv.includeTools, "Subset of tools that should be
//     enabled for this server. When omitted all tools are enabled."
//     MCP tools are named bare there — openv is the only server in
//     the run, so no serverAlias__tool prefixing applies.
//
// includeAll reports the mcp__openv__* wildcard, for which includeTools is
// omitted rather than written empty.
//
// Note what tools.core can and cannot express: it registers a tool or does
// not. It has no per-command scoping, so "Bash(git *)" carries its scope
// through in gemini's own run_shell_command(git *) spelling — which registers
// the shell tool but does not constrain the command. What holds it is the
// approval mode: neither "default" nor "auto_edit" auto-approves a shell call,
// and a headless run cannot answer the confirmation it would raise.
func geminiToolSettings(allowed []string) (core []string, include []string, includeAll bool) {
	core = []string{}
	include = []string{}
	seenCore := map[string]bool{}
	seenInclude := map[string]bool{}

	for _, entry := range agents.NonEmptyTools(allowed) {
		name, scope := splitToolScope(entry)
		if strings.HasPrefix(name, mcp.ToolPrefix) {
			tool := strings.TrimPrefix(name, mcp.ToolPrefix)
			switch {
			case tool == "*":
				includeAll = true
			case tool != "" && !seenInclude[tool]:
				seenInclude[tool] = true
				include = append(include, tool)
			}
			continue
		}
		for _, mapped := range geminiBuiltinTools[name] {
			if scope != "" {
				mapped += "(" + scope + ")"
			}
			if !seenCore[mapped] {
				seenCore[mapped] = true
				core = append(core, mapped)
			}
		}
	}
	sort.Strings(core)
	sort.Strings(include)
	return core, include, includeAll
}
