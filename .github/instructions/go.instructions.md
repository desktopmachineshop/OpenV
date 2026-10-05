---
applyTo: "cmd/**/*.go,internal/**/*.go"
---

# Go code: how to work

How to change the Go backend, as its architecture tests and goldens hold
every pull request to it. What the platform must do is not here: it is the
live OpenV project (see `CLAUDE.md`). The area guides, a `README.md` and a
`CLAUDE.md` in `cmd/server/`, `internal/api/`, `internal/domain/`,
`internal/persistence/postgres/`, `internal/runner/` and `internal/notify/`,
have the map, the recipes and the commands for each directory;
`docs/architecture.md` has the package graph. A rule marked *archtest* is a
rule of `go test ./internal/archtest`, named as its failure names it and
explained in `internal/archtest/README.md`.

## Layers and dependencies

- **K7 layering** (archtest *K7 layering*). A package under
  `internal/domain` imports only other domain packages, besides the
  standard library and third-party modules; `internal/persistence` imports
  only `internal/domain`; `internal/api` never imports
  `internal/persistence`. To reach across a layer, declare an interface in
  the package that needs it (as `artifacts` declares `LinkSuspector`) and
  let `cmd/server` wire the implementation.
- **Import edges are frozen** (archtest *import edges*). A new import
  between the module's packages is added to `import_edges` in
  `internal/archtest/ratchets.json` by hand, in review, and still passes K7.
- **Interfaces at the boundaries.** A domain package that offers a service
  exposes it as a `Service` interface and declares the `Repository` port
  that `internal/persistence/postgres` implements, so storage details stay
  behind it.
- **Dependencies are passed in.** Services are built in `cmd/server`'s
  stages (`wire_*.go`) and handed to what uses them. A new API dependency is
  one `api.HandlerDeps` field plus one line in stage `handlers`
  (`cmd/server/wire_http.go`) (K5; archtest *K5 raw HandlerDeps reads*).
- The client binaries (`cmd/agentd`, `cmd/openv-mcp`, `cmd/openv-connector`,
  `cmd/openv-vapid`) run on members' machines: the domain packages they link
  may only shrink (archtest *client binaries*).
- New Go code goes under `cmd/` or `internal/`: the Docker images copy
  those directories and a few named root files only (archtest *build
  context*).

## No hidden global state

- No `func init()` (archtest *no init functions*). Registries (migrations,
  MCP tools, routes) are explicit ordered lists.
- A package-level `var` is initialised by pure calls only: no env read,
  clock, file, network or call into the module (archtest *side-effecting
  package variables*). Build the value where it is needed, or behind
  `sync.OnceValue`.
- Add no mutable package-level state; pass a value in instead. The known
  exception is `internal/domain/orgs`'s default `DeploymentPolicy`, the
  four deployment settings boot writes through `SetSelfHosted`,
  `SetTiersEnforced`, `SetDefaultPlan` and `SetDeploymentLimits` and
  everything reads at call time (refactor step X8); a test that needs other
  settings builds a `DeploymentPolicy` of its own where the code it tests
  takes one.
- **Settings.** A binary reads its environment through its getters over
  `internal/envparse` (`cmd/server/config.go`, `cmd/agentd/main.go`), which
  trim and parse every value one way. Under `internal/`, the direct env
  reads per package may only fall, and a package not listed may make none
  (archtest *direct env reads*): take the value as a parameter. Every
  variable a program reads is in the S8 inventory (`TestEnvInventory`) and
  in `docs/env-vars.md` (`TestEnvVarsDoc`); `cmd/server/README.md`, "Add an
  env var", has the steps.

## The HTTP layer (`internal/api`)

- **One area per file** (K1). An area's handlers and its
  `register<Area>Routes` live in its `<area>_handlers.go`; routes are
  registered only there (archtest *HandleFunc outside registrars*), on the
  one router `cmd/server/http.go` builds (archtest *one router*);
  `RegisterRoutes` in `routes.go` is the ordered list of registrar calls.
  Every route but `GET /health` and `/metrics` is under `/api/v1/`.
- **One home per helper** (K3). A helper two files use lives in
  `respond.go`, `httperr.go`, `authz.go` (every `require*` guard),
  `publish.go`, `cookies.go` or a `middleware_*.go` file, never in one area
  file used by another (archtest *K3 helper homes*, *require helpers outside
  authz*).
- **JSON in and out** (K4). Answer through `respondJSON` and the
  `httperr.go` writers (`respondError`, `writeJSONError`); the counts of raw
  `json.NewEncoder` calls and `"invalid request body"` literals may only
  fall (archtest *raw JSON encodes*, *invalid request body literals*). An
  existing encode that sets no `Content-Type` stays as it is (quirk Q1,
  `docs/contract-quirks.md`).
- **Handlers stay thin.** A handler decodes, guards, calls a domain service
  and answers; a new business rule goes in its domain package. Some
  orchestration, link writes among it, still sits in handlers (refactor
  plan §7.3); don't add to it.
- **Tests** build a handler with `newTestHandler` and the shared fakes in
  `testkit_test.go`, never a `Handler{...}` literal (K6; archtest *Handler
  literals in tests*).
- **Document a new route** with a row in `docs/api-spec.md` (archtest *API
  spec drift*). A route added, removed or changed alters the route goldens,
  so it is a behavior change with a release note.

## Migrations (`internal/persistence/postgres`)

- One `migration_00NN_<name>.go` per version plus one line in the ordered
  registry in `migrations.go` (K9; `TestEachMigrationFileRegistersItsVersion`,
  `TestRegistryIsOrderedWithoutDB`).
- Never edit, reorder or renumber a migration that has shipped; follow up
  with a new one (`TestMigrationFreeze`). Add no DDL to `InitSchema` or
  `schema_*.go`. The full recipe is in that directory's `README.md`.

## Size, format and tests

- **K14.** A new file has at most 800 lines and a function at most 100; the
  grandfathered ceilings only fall (archtest *K14 file size*, *K14 function
  size*). Split by concern within the package.
- Code is `gofmt`-clean and passes `go vet` (CI's backend job).
- A domain package is tested without a database or HTTP.
- A golden under `testdata/` changes only in a pull request that changes
  behavior, regenerated with the command its failing test prints; never edit
  one by hand.
- Every entry of `internal/archtest/ratchets.json` may only fall. Never
  raise one to make a change pass: change the code.
  `UPDATE_RATCHETS=1 go test ./internal/archtest` tightens it.
- A new file needs an area: a glob in `docs/areas.json` (K15,
  `internal/tools/areas`); `go run ./internal/tools/areas which <path>`
  names the area a path falls in.

Before you finish: `make check-fast` while working, `make check` before
pushing, and the commands in the `CLAUDE.md` of the area you changed.
