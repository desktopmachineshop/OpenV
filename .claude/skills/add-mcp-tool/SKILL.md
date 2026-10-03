---
name: add-mcp-tool
description: Use when adding a tool to the openv MCP server (internal/mcp, which openv-mcp serves to agent CLIs), or changing what an existing tool sends.
---

# Add an MCP tool

The recipe is in the area guide; follow it there.

- [internal/runner/README.md, Recipes](../../../internal/runner/README.md#recipes),
  "Add an MCP tool" (the guide covers `internal/mcp` too).
- A route the tool needs that the server lacks: the `add-endpoint` skill.

Scaffold (`-n` previews; it prints the steps left when it is done):

- `go run ./internal/tools/scaffold -area <area> mcp-tool <name>`, where
  `<area>` names `internal/mcp/tools_<area>.go`.

Before you finish: [internal/mcp/CLAUDE.md](../../../internal/mcp/CLAUDE.md),
then "Before you finish" in the root [CLAUDE.md](../../../CLAUDE.md).
