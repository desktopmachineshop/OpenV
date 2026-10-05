# internal/runner and internal/mcp: agent execution

Where an agent run executes. The runner (`agentd`, built from
`cmd/agentd`) claims runs from the API over the worker wire and drives a
vendor CLI; the MCP server (`openv-mcp`, built from `cmd/openv-mcp` over
`internal/mcp`) runs beside that CLI and gives it the openv tools, each a
call to the REST API. Both binaries run on operators' machines and lag the
server, so what they send is a contract. This README covers both packages;
`internal/mcp/CLAUDE.md` points here. Plan §7.5
(`docs/plans/codebase-refactor.md`) has the history.

## Areas

| Area (`docs/areas.json`) | Globs |
|---|---|
| runner-fleet | `internal/runner/**` (this file too), `cmd/agentd/**`, `cmd/openv-connector/**`, `internal/hosting/**`, `internal/workerproto/**` |
| agent-suite | the MCP core: `internal/mcp/tools.go`, `internal/mcp/client*.go`, `internal/mcp/stdio*.go`, `internal/mcp/schema*.go`, `internal/mcp/context_bundle*.go`, `internal/mcp/tools_delegation*.go`, `internal/mcp/tools_interviews*.go`, `internal/mcp/testdata/**`, `internal/mcp/toolnames/**`, `internal/mcp/*.md`; `cmd/openv-mcp/**`; `internal/seeds/seeds*.go` |
| requirements-core | `internal/mcp/tools_artifacts*.go`, `internal/mcp/tools_comments*.go`, `internal/mcp/tools_links*.go`, `internal/mcp/tools_projects*.go`, `internal/mcp/tools_quality*.go`, `internal/mcp/tools_reviews*.go`, `internal/mcp/tools_workitems*.go` |
| verification | `internal/mcp/tools_testruns*.go`, `internal/mcp/tools_vv*.go` |
| documents | `internal/mcp/tools_baselines*.go` |

## Map

| Glob | What it holds |
|---|---|
| `worker.go` | `Worker`: the claim loop, its two slot pools, a run from claim to finish or release |
| `client.go` | `Client`: every request of the worker wire (claim, start, logs, finish, release, sign-ins, pool) |
| `internal/workerproto/workerproto.go` | the worker wire's bodies, a types-only leaf (K7) the API and the runner both use; `agentruns.FinishRequest` and `LogEntry` and this package's `RunAuth`, `PoolNode` and `PoolAssignment` are aliases of its types |
| `adapter.go`, `claudecode.go`, `codexcli.go`, `geminicli.go`, `antigravity.go` | the provider adapters, one per vendor CLI, and detection |
| `classify.go`, `process.go` | the failure taxonomy (where a run ended, its class, whether it retries); the CLI process and its watchdog |
| `toolallow.go`, `childenv.go` | the agent's tool allowlist translated for each vendor CLI; the environment a CLI starts with |
| `login*.go`, `pty_*.go` | the CLI sign-in broker and its pseudo-terminal relay |
| `pool.go`, `workspace.go` | a cloud runner pool node and its leases; the run's directory and repository checkout |
| `wire_*_test.go`, `testdata/wire/*.json` | S7: every `Client` request and decode, byte for byte |
| `run_*_test.go`, `signin_claim_test.go`, `pool_lease_test.go`, `fakeapi_test.go`, `testdata/run_failures/*.txt` | S15a: outcomes and classes, slots, sign-in to claim, pool leases, against a stand-in API and CLIs |
| `internal/mcp/tools.go` | `Tool` with its `ReadOnly` flag, `Tools()` (the area constructors in order), `ReadOnlyToolNames()`, the `OPENV_MCP_TOOLS` filter |
| `internal/mcp/tools_*.go` | one area's tools each, returned by one constructor (K10) |
| `internal/mcp/client.go`, `internal/mcp/stdio.go`, `internal/mcp/schema.go` | the REST client the tools call (run-token auth), the JSON-RPC loop, schema helpers |
| `internal/mcp/*golden_test.go`, `internal/mcp/testdata/*` | S7: the tool table with each tool's requests, and the JSON-RPC exchange |
| `cmd/agentd/**`, `cmd/openv-mcp/**`, `cmd/openv-connector/**` | the binaries, with their CLI snapshots in `cmd/*/testdata/cli/**` (S8) |
| `internal/hosting/**` | hosted runners: containers on the API's Docker daemon |
| `internal/seeds/seeds.go` | the seeded agents; the interviewer is granted `ReadOnlyToolNames()` |

## Invariants (plan §3) that bind here

- **I11 MCP.** The tools' names, order, descriptions and input schemas;
  which are read-only and the order of `ReadOnlyToolNames()`, which is
  stored data; every request each tool sends; the JSON-RPC encoding.
- **I12 worker wire.** The claim body's keys (`agent`, `auth`, `run`,
  `run_token`), with `auth` always an object and no `omitempty`; release
  sends `{"worker_id"}`; deployed runners still send the legacy log body.
- **I13, I14.** `agentd`'s 12 flags, `openv-mcp`, the `openv-connector`
  subcommands; env parsing by `internal/envparse`'s rule, apart from the
  runner's per-run reads S8 exempts. `OPENV_MCP_TOOLS` set but empty means
  no tools.
- **REQ-91.** The allowlist readers in `internal/mcp` and here are two
  grammars; do not merge them.
- **K7 client binaries.** The domain packages `cmd/agentd` and
  `cmd/openv-mcp` link may only shrink: every type they link is wire
  contract.

## Recipes

**Add an MCP tool.**
1. Append a `Tool` to its area's constructor in `internal/mcp/tools_*.go`,
   with `ReadOnly: true` only if its handler issues nothing but GETs. A new
   area file gets a constructor appended to `Tools()` and a glob in
   `docs/areas.json`; never reorder the table.
2. Give it recording arguments in `toolGoldenArgs`
   (`internal/mcp/tools_golden_test.go`) that set every schema property.
3. Regenerate S7's golden; every request must be a route of
   `internal/api/testdata/routes.txt`:
   `UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run TestMCPToolsGolden`.
Scaffold: `go run ./internal/tools/scaffold -area <area> mcp-tool <name>`, `<area>` as in `tools_<area>.go`

**Change the worker wire.** Server side in
`internal/api/worker_protocol_handlers.go`, runner side in `client.go`,
the bodies both send in `internal/workerproto` (a body that replaced a map
declares its fields in the map's sorted key order, R10); keep accepting
what older runners send. Add cases to `wireGoldenCases`
(`wire_cases_test.go`), then regenerate:
`UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run TestRunnerWireGolden`,
and the S5d tour's `TestTourS5dWorkerWire` with a database
(`cmd/server/README.md`).

**Change how a run ends** (a message, a finish site, a retried class, the
CLI's arguments): edit `classify.go` or the adapter, add the row or
scenario the taxonomy test asks for, and regenerate:
`UPDATE_GOLDEN=1 go test ./internal/runner -count=1 -run '^(TestRunFailureClassesGolden|TestRunFailureTaxonomyGolden)$'`.

**Add an agentd flag or message.** Edit `cmd/agentd/main.go`, then
regenerate the CLI snapshots:
`UPDATE_GOLDEN=1 go test -count=1 -run '^TestCLI$' ./cmd/agentd ./cmd/openv-connector ./cmd/openv-mcp ./cmd/openv-vapid`.
A new env var also regenerates S8's inventory (`cmd/server/README.md`).

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S7** `TestMCPToolsGolden`, `TestMCPToolsGoldenArgsCoverSchemas`,
  `TestMCPJSONRPCGolden` and the read-only tests: `internal/mcp/testdata/`
  (I11). `go test ./internal/mcp -count=1`
- **S7** `TestRunnerWireGolden` and
  `TestRunnerWireGoldenCoversEveryClientMethod`: `testdata/wire/` (I12).
  `go test ./internal/runner -count=1 -run '^TestRunnerWireGolden'`
- **S15a** `TestRunFailureClassesGolden`, `TestRunFailureTaxonomyGolden`,
  and the slot, sign-in and lease tests: `testdata/run_failures/`.
  `go test ./internal/runner -count=1` (about 80 s; `-short` skips the
  tests that wait on the worker's ticks). CI's backend job runs them under
  the race detector, which alone catches a lost lock on what the runner's
  goroutines share, such as the providers a sign-in adds and the claim loop
  reads (#379 bug 125): `go test -race -count=1 ./internal/runner/...`
- **S8** `TestCLI` and `TestEnvParse` in the binaries: `cmd/*/testdata/cli/**`
  and the parse table (I13, I14).
  `go test -count=1 -run '^(TestCLI|TestEnvParse)$' ./cmd/agentd ./cmd/openv-mcp ./cmd/openv-connector`
- **S5d** `TestTourS5dWorkerWire`: what the server answers the runner;
  see `cmd/server/README.md`.
- **S1** archtest, client binaries and import edges:
  `go test ./internal/archtest -run 'TestArchitecture/(client|import)'`

A golden here changes only in a pull request that changes behavior (R3).
