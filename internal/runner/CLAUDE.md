@README.md

Before you finish a change here or in `internal/mcp`, run:
- `go test -short -count=1 ./internal/runner ./internal/mcp ./internal/seeds ./internal/hosting ./cmd/agentd ./cmd/openv-mcp ./cmd/openv-connector`
- before pushing, without `-short` and under the race detector, as CI runs it: `go test -race -count=1 ./internal/runner` (about 90 s)
- `go test ./internal/archtest` (a client binary may link no new domain package)

Don't:
- reorder the MCP tools (I11, R9); a new tool is appended in its `internal/mcp/tools_*.go`, `ReadOnly` only if it only reads (K10)
- drop or rename a worker-wire field: deployed runners lag the server (I12); the claim's `auth` keeps no `omitempty`
- merge the allowlist readers of `internal/mcp` and `internal/runner`: they are two grammars (REQ-91)
- change an agentd flag or message without regenerating `TestCLI`'s snapshots (I14)
- hand-edit `testdata/`; regenerate with the command the failing test prints
