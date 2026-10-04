# cmd/server: the composition root

The API server's `main` package. It reads the configuration, opens and
migrates the database, builds every repository and service, wires the event
bus, starts the background loops and serves the one router. Since refactor
step M4, `main()` only calls stages; plan §7.1
(`docs/plans/codebase-refactor.md`) has the history. Run
`go run ./internal/tools/areas which <path>` to see which area owns a file.

## Areas

| Area (`docs/areas.json`) | Files here |
|---|---|
| platform-http | `main.go`, `app.go`, `config.go`, `http.go`, `jobs.go`, `logging.go`, `lookups.go`, `upload_sweep.go`, `wire_config.go`, `wire_storage.go`, `wire_jobs.go`, `wire_http.go`, `testdata/**`, `*.md` |
| requirements-core | `wire_core.go`, `wire_projects.go` |
| tenancy-identity | `wire_workspace.go`, `wire_sso.go` |
| agent-suite | `wire_agents.go` |
| events-notifications | `wire_notify.go`, `wire_realtime.go` |

Test files (`*_test.go`) need no area.

## Map

| Glob | What it holds |
|---|---|
| `main.go` | `main()`: the stage calls in boot order, the deferred cleanups two stages return, then listen and graceful shutdown |
| `app.go` | the `app` struct: every value a stage builds for a later stage |
| `wire_*.go` | the stages, methods on `*app` that `main()` calls in this order: `signals`, `config` (`wire_config.go`); `connect`, `storage` (`wire_storage.go`); `core`; `workspace`, `runners` (`wire_workspace.go`); `projects`; `agents`; `realtime`; `notify`, `release` (`wire_notify.go`); `jobs`; `sso`; `billing`, `handlers`, `server` (`wire_http.go`) |
| `config.go` | the env getters `envOr`, `envInt`, `envBool`, `envSwitch` and `envSecret`, over `internal/envparse` |
| `http.go` | the one `mux.NewRouter()`, `/metrics`, the middleware chain (`buildHTTPHandler`) and `newServer` |
| `jobs.go` | the background loops: `runPurgeLoop`, `runReaper`, `reconcileHostedRunners`; `removeStoredFiles`, which removes the files a committed purge took |
| `upload_sweep.go` | `sweepUnreferencedUploads`, which stage `storage` runs once per database after the migrations unless `OPENV_UPLOAD_SWEEP=off`: the stored files no row names, and the logos and profile pictures of workspaces and accounts no row has, by the fail-safe rules at the top of the file |
| `lookups.go` | closures over the database and services that stages hand to services |
| `logging.go` | `initLogging` and `fatal` |
| `boot_*_test.go`, `harness*_test.go` | the S4 boot harness: the real binary booted per env profile |
| `tour_*_test.go` | the S5 API tour, one area per `tour_<slice>_<key>_test.go`, plus its framework |
| `env_*_test.go` | S8's parse table for this package's getters, and env edge cases |
| `testdata/boot/*.txt`, `testdata/boot_steps.txt` | S4 goldens: one per boot; the order of `main()`'s wiring |
| `testdata/tour/**` | S5 goldens: one JSON per tour area, `coverage.txt` per slice and the union |
| `testdata/stages/**` | the fixtures of `TestBootStepsFollowStages` |

## Invariants (plan §3) that bind here

- **I17, R9 boot order.** Each stage is a contiguous range of the old
  `main()`, in its order. Never regroup or reorder stages. Every `defer`
  stays in `main()`: a stage that opens something returns its cleanup
  (`signals`, `connect`). The purge runs at start and then daily, the
  reaper first ticks after 30 s, the scheduler's catch-up runs before
  listen, and `billing.Start` (stage `billing`) runs before `NewHandler`
  (stage `handlers`, quirk Q11).
- **I10 subscriber order** on the event bus: orchestration hooks
  (`realtime`), then the notifier and the budget monitor (`notify`), then
  the trigger matcher (`jobs`).
- **I13 env.** Every fatal check keeps its place relative to the migration
  in `storage`, and a variable is read only under today's condition, through
  the getters in `config.go` (K8).
- **I6 middleware order** in `buildHTTPHandler`, outermost first:
  SecurityHeaders, BodyLimit, CORS, Compression, RequestLog, metrics, Auth,
  router. **I1, I2, K2:** one router, and `/metrics` is the only route
  registered outside `RegisterRoutes`.
- **I8** cookies and HSTS follow `SECURE_COOKIES` and `CROSS_SITE_COOKIES`.
- **K5** a handler dependency is one field of `api.HandlerDeps` plus one line
  in stage `handlers` (`wire_http.go`).

## Recipes

**Wire a new service or repository.**
1. Build it in the stage that owns its concern (the Areas table above;
   repositories in `storage`). Add an `app` field only when a later stage
   reads it.
2. If the stage gains a setter, a bus subscription, a `Start`, a checked
   error, a goroutine or a defer, regenerate `testdata/boot_steps.txt`:
   `UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootSteps$'`.
3. Check the stages still flatten:
   `go run ./internal/tools/movecheck -flatten main ./cmd/server`.

**Give the API a new dependency** (K5). Add the field to `api.HandlerDeps`
(`internal/api/handlers.go`), which `api.Handler` embeds, then one line in
the `api.HandlerDeps` literal of stage `handlers`. A new API area also
needs its registrar; see `internal/api/README.md`.
Scaffold: `go run ./internal/tools/scaffold api-area <name>`

**Add an env var.** Read it with a getter from `config.go` in the stage that
uses it, under the condition it applies to. Then regenerate S8's inventory:
`UPDATE_GOLDEN=1 go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest ./cmd/server`.
A setting that changes what the server does gets a profile in
`s4bProfiles` (`boot_profiles_test.go`); a new fatal check gets a boot in
`misconfiguredBoots` (`boot_misconfigured_test.go`). Regenerate their
goldens with a database:
`OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^(TestBootSmoke|TestBootProfiles|TestBootMisconfigured)$'`.

**Add a background loop.** A named function in `jobs.go`, started with `go`
from the stage that owns it, at the statement where it must run. There is
no generic job runner (plan §7.6): first-run timing is behavior (I17).

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S4** `TestBootSteps`: `testdata/boot_steps.txt`, the wiring order of
  `main()` and its stages (I10, I17).
  `go test ./cmd/server -count=1 -run '^TestBootSteps'`
- **S4** `TestBootSmoke`, `TestBootProfiles` and `TestBootMisconfigured`:
  `testdata/boot/*.txt`, the boot log, middleware answers, `/metrics` and
  fatal exits per profile (I6, I8, I13). They need a database.
  `OPENV_TEST_DATABASE_URL=<server URL> go test ./cmd/server -count=1 -run '^TestBoot'`
- **S5** `TestTour*`: `testdata/tour/**`, every route's status, headers,
  bytes and events per area, and the S5e authorization matrix (I3, I4, I5).
  `OPENV_TEST_DATABASE_URL=<server URL> go test ./cmd/server -count=1 -run '^TestTour'`;
  without a database the coverage floor, existence hiding and matrix
  helpers still run:
  `go test ./cmd/server -count=1 -run '^(TestTourCoverage|TestTourExistenceHiding|TestTourMatrix)$'`
- `TestBuildHTTPHandlerLayerOrder`: the middleware order (I6).
  `go test ./cmd/server -count=1 -run '^TestBuildHTTPHandlerLayerOrder$'`
- **S8** `TestEnvParse` and `TestEnvInventory`: env names, defaults and
  parsing (I13).
  `go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest ./cmd/server`
- **M4's proof**: `main()` with every stage inlined, and no `defer` or early
  `return` in a stage.
  `go run ./internal/tools/movecheck -flatten main ./cmd/server`
- **S1** archtest: one router, K14 sizes, direct env reads, build by package
  path. `go test ./internal/archtest`

The boot and tour goldens change only in a pull request that changes
behavior, with a release note or the maintainer's `behavior-change` label
(R3); each failure prints the command that regenerates its golden.
