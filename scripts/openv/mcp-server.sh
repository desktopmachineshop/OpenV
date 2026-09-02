#!/bin/sh
# Starts the OpenV MCP tool server for an agent session in this repository —
# the same server agentd runs beside a vendor CLI, driven by the workspace
# runner key in the environment instead of a per-run token. Claude Code and
# other MCP hosts start it from .mcp.json at the repository root.
#
# Credentials come from the environment, exactly as scripts/openv/sync.py
# takes them: OPENV_API_URL plus OPENV_API_TOKEN (a workspace runner key).
#
# The binary is built on first use and whenever its sources change; `make mcp`
# builds it ahead of time so a session starts without waiting for the compile.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
bin="$root/bin/openv-mcp"

if [ -z "${OPENV_API_TOKEN:-}" ] && [ -z "${OPENV_RUN_TOKEN:-}" ]; then
	echo "openv-mcp: set OPENV_API_TOKEN to a workspace runner key (Settings -> Runners -> Workspace keys) before starting this server" >&2
	exit 1
fi

stale=no
if [ ! -x "$bin" ]; then
	stale=yes
elif [ -n "$(find "$root/cmd/openv-mcp" "$root/internal/mcp" -name '*.go' -newer "$bin" -print 2>/dev/null | head -n 1)" ]; then
	stale=yes
fi

if [ "$stale" = yes ]; then
	if ! command -v go >/dev/null 2>&1; then
		echo "openv-mcp: no Go toolchain on PATH and no binary at $bin — build one with 'make worker-unix' on a machine that has Go" >&2
		exit 1
	fi
	echo "openv-mcp: building $bin" >&2
	(cd "$root" && go build -o "$bin" ./cmd/openv-mcp) >&2
fi

exec "$bin"
