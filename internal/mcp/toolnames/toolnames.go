// Package toolnames holds the names an agent's tool allowlist uses for the
// OpenV MCP server: the server's own name, the prefix of its tools and the
// environment variable that narrows them. It is a leaf that imports nothing
// of this module, so the runner can name them without linking the MCP
// server's tool table; internal/mcp keeps aliases of the three constants.
package toolnames

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
