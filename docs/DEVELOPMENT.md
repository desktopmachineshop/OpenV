# Development Guide

## Toolchain

The project targets **Go 1.25** (`go.mod`) and **Node 24** (frontend, Vite).
The standard development toolchain is **Docker only** — neither Go nor Node
needs to be installed on the host. Every build target in the `Makefile` runs
inside `golang:1.25`, and the frontend image builds on `node:24-alpine`.

If you do have a local Go 1.25+ / Node 24+ install, the commands below work
directly on the host too, but Docker is the supported path.

## Running the stack

```bash
make up          # docker compose up -d  (Postgres, API, frontend)
make down        # stop the stack
make build       # rebuild the images
```

- **Frontend**: http://localhost:3000
- **API**: http://localhost:8080
- **Postgres**: localhost:5432 (postgres/postgres, db `openv`)

The API migrates its schema on startup (boot calls
`postgres.MigrateAndBackfill` in
`internal/persistence/postgres/migrations.go`, which runs `postgres.Migrate`
plus the idempotent org backfill), so there is no separate migration step. See
"Adding a schema migration" below.

## Backend development

Compile-check or build via the Go container:

```bash
# Compile everything
docker run --rm -v "$(pwd):/app" -w /app golang:1.25 go build ./...

# Run the test suite (also available as `make test`)
docker run --rm -v "$(pwd):/app" -w /app golang:1.25 go test ./...
```

To run a changed API server, rebuild the image and restart the service:

```bash
docker compose build api && docker compose up -d api
```

Key environment variables (see `cmd/server/main.go` and
`docker-compose.yml`): `DATABASE_URL` (or `DB_HOST`/`DB_PORT`/`DB_USER`/
`DB_PASSWORD`/`DB_NAME`), `PORT`, `UPLOADS_DIR`, `OPENV_DATA_DIR`,
`AGENTS_DIR`, `WORKER_API_KEY` (legacy bootstrap worker key),
`GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET`/`PUBLIC_URL` (Google sign-in),
`FRONTEND_URL`, `CORS_ORIGIN`, `SECURE_COOKIES`, and the hosted-runner
settings (`HOSTED_RUNNERS`, `RUNNER_IMAGE`, `RUNNER_NETWORK`,
`RUNNER_API_URL`, `CONNECTOR_DIST_DIR`).

## Adding a schema migration

Schema changes are numbered migrations tracked in the `schema_migrations`
ledger (`version`, `name`, `applied_at`). At boot, `postgres.Migrate`:

1. creates the ledger table if missing,
2. re-runs the idempotent **0001 baseline** — the frozen legacy init chain
   (`InitSchema` + `schema_users/suite/agents/orgs.go`) — which keeps
   pre-ledger databases upgradeable and costs only milliseconds, then
3. applies any unapplied numbered migrations in order, each **exactly once,
   in its own transaction**, recording it in the ledger. A failed migration
   rolls back completely and is not recorded; concurrent boots are
   serialized by an advisory lock.

To add a schema change, do **not** touch `InitSchema` or the `schema_*.go`
files (the baseline is frozen). Instead append an entry to the `migrations`
registry in `internal/persistence/postgres/migrations.go` with the next
version number:

```go
{Version: 2, Name: "add_widgets_table", Run: func(tx *sql.Tx) error {
    _, err := tx.Exec(`CREATE TABLE widgets (
        id UUID PRIMARY KEY,
        created_at TIMESTAMP NOT NULL DEFAULT NOW()
    )`)
    return err
}},
```

Rules: never renumber, reorder, or edit a migration that has shipped —
follow up with a new one. Plain DDL is fine (no `IF NOT EXISTS` guards
needed; the ledger guarantees single execution). Avoid statements that
cannot run inside a transaction (e.g. `CREATE INDEX CONCURRENTLY`).

`BackfillOrgs`/`PromoteOrgColumns` remain boot-time idempotent data-migration
steps outside the ledger (they guard themselves and touch the agents
directory on disk).

## Frontend development

The frontend is a **Vite** app in `frontend/`. It installs with a plain
`npm ci` — the `--legacy-peer-deps` that used to be required everywhere was
a workaround for Create React App peer-requiring TypeScript 4 against this
project's TypeScript 5, and went with it.

```bash
docker run --rm -v "$(pwd)/frontend:/app" -w /app node:24 npm ci

docker run --rm -v "$(pwd)/frontend:/app" -w /app -p 3000:3000 \
  -e REACT_APP_API_URL=http://localhost:8080 node:24 npm start
```

The `REACT_APP_` prefix is unchanged: `vite.config.ts` sets `envPrefix` to it
so the Dockerfiles, compose files and deployment docs kept working across the
move. Values are read through `import.meta.env` rather than `process.env`,
which Vite does not shim in the browser.

Four commands make up the frontend gate, and CI runs all four:

```bash
npx tsc --noEmit    # types
npm run lint        # eslint — see frontend/eslint.config.js
npm test            # vitest, once (npm run test:watch to iterate)
npm run build       # vite build, into frontend/build/
```

`npm run lint` exists because CRA used to run eslint inside the build and
fail on warnings when `CI=true`; Vite does not, so the gate is a step of its
own.

In the composed stack the frontend container runs `npm start` itself; for
quick iteration, `docker compose build frontend && docker compose up -d
frontend` picks up changes.

## Worker binaries and connector bundles

The agent worker binaries run on the **host** (next to your vendor CLIs), but
they are cross-compiled through Docker as well:

```bash
make worker          # Windows binaries -> bin/agentd.exe, bin/openv-mcp.exe
make worker-unix     # Linux/macOS binaries -> bin/agentd, bin/openv-mcp
make worker-image    # hosted-runner image (openv-worker:latest, Dockerfile.worker)
make connector-dist  # Agent Connector download bundles -> dist/*.zip
                     # (served at /api/v1/public/connector/download)
```

See `docs/agents.md` for how the runners are used.

## Tests and CI

```bash
make test   # go test ./... inside golang:1.25
```

CI runs on GitHub Actions (`.github/workflows/ci.yml`): Go vet/test scoped
to `./cmd/... ./internal/...` with a Postgres service (`OPENV_TEST_DATABASE_URL`
enables the integration tests), the Postgres-backed tests again on
`pgvector/pgvector:pg15` so the embedding tests that need the vector
extension run too (the main job's plain `postgres:15` covers the paths
without it), a frontend `npm ci` + `tsc` + build, Docker
image builds, a Playwright smoke journey against the composed stack, and the
vulnerability scan below. Still run `make test` locally before pushing.

A new migration changes the stored-data goldens under
`internal/persistence/postgres/testdata/` (refactor plan step S3): the
schema after both boot paths, the migration freeze and the purge catalogue.
Regenerate them in the same pull request, against a Postgres that has the
vector and pg_trgm extensions (`pgvector/pgvector:pg15`, as CI's pgvector leg
runs), with
`UPDATE_GOLDEN=1 go test ./internal/persistence/postgres -count=1 -run '^(TestMigrationFreeze|TestEveryBootFreeze|TestSchemaGolden|TestPurgeCatalog)$'`.
A shipped migration is never edited: the freeze appends new migrations and
refuses to rewrite an old one, and fails if the schema goldens record a
migration it has not frozen, so the one command above is the way to
regenerate them. A new table with an `org_id`, `project_id` or `artifact_id`
column must go when its workspace is purged, through `PurgeOrg`'s list or an
`ON DELETE CASCADE` foreign key whose columns are `NOT NULL` (a nullable one
leaves the rows whose key is NULL); the list of tables a purge misses may
only shrink.

The boot harness in `cmd/server` (refactor plan steps S4a and S4b) builds
the server binary with `-cover`, boots it on a database of its own on the
server `OPENV_TEST_DATABASE_URL` names, with an environment it builds from
nothing, and probes it from outside: the boot log, the middleware answers
(security headers, CORS, the body cap, gzip, the mux's 404, 405 and 301),
`/metrics` and a SIGTERM drain, one golden per profile under
`cmd/server/testdata/boot/`. `TestBootSmoke` boots the default profile and
one with a metrics token; `TestBootProfiles` boots one profile per setting
that changes what the server does (`SECURE_COOKIES`, `CROSS_SITE_COOKIES`,
`OPENV_SELF_HOSTED`, the plan tiers, `OPENV_REGISTRATION=closed`,
`OPENV_LIMITS`, `OPENV_BUILD_SHA`, billing on, billing configured on a
self-hosted install, and a malformed grandfather date on one), adds the
workspace's effective limits and every billing route to the probes, and
points `HTTP_PROXY` and `HTTPS_PROXY` at a proxy of the test's own that
refuses and records every request, so no request leaves the machine and
the golden lists what the server tried;
`TestBootMisconfigured` boots once per fatal setting (`OPENV_LIMITS`, the
grandfather date, the billing price map, `CORS_ORIGIN`), and again where a
condition matters (`OPENV_LIMITS` with no database to reach, the price map
with billing off and on a self-hosted install), and records the exit, the
fatal message and whether the migrations ran first.
`cmd/server/testdata/boot_steps.txt` holds the order of `main()`'s wiring
(setters, subscriptions, `Start` calls, the calls whose error it checks,
goroutines, defers), read from the source. A change to what the server
logs at boot, to a middleware, to what a setting does, to which settings
are fatal, or to that order changes them; regenerate in the same pull
request with
`OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^(TestBootSmoke|TestBootProfiles|TestBootMisconfigured)$'`
(a server with or without the vector extension) and
`UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootSteps$'`.
A new environment setting that changes what the server does gets a profile
in `s4bProfiles` (`cmd/server/boot_profiles_test.go`), and a new fatal
check a boot in `misconfiguredBoots` (`boot_misconfigured_test.go`), each
with its golden; `TestBootGoldensAreClaimed` fails on a golden no boot
writes.

The API tour beside it (refactor plan steps S5a–S5e) boots the same binary
once per area, on a database of its own, with `TZ=UTC` and the recording
proxy, registers its accounts through the API and drives the area's routes:
each request's status, whether a `Content-Type` is set (also on the gzip
variant of each GET), the headers, the body as bytes with only the values
that change from run to run replaced by tokens (a time the server minted is
written with the request it was minted in, `<time@step 12>`), and the domain
events the request published (type, actor, and each payload key with its
JSON type and normalised value), one golden per area under
`cmd/server/testdata/tour/<slice>/`. S5a covers the requirements
core (projects, templates, artifacts, attribute definitions, links with the
managed edits of `PUT /artifacts/{id}`, review, chatter, search,
attachments with Range requests, baselines, share links and the public views
they open, and every export, import, report and `/download/*` format).
`coverage.txt` beside the goldens lists the routes the slice's steps reached,
marking those that answered only errors, and `/metrics` must count every
request under the route template the tour declared. A change to what one of
those routes answers changes its golden; regenerate in the same pull
request with the command the failure prints,
`OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestTourS5a<Area>$'`
for one area or `-run '^TestTour'` for all (a server with or without the
vector extension, running in UTC on the same host's clock), which also rewrites
`coverage.txt`; `UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestTourCoverage$'`
rewrites `coverage.txt` alone with no database. An area is one file,
`cmd/server/tour_<slice>_<key>_test.go`, with its golden; the areas in
`tour_s5a_*_test.go` are the worked examples, and
`TestTourGoldensAreClaimed` fails on a golden no area writes.

S5b, the second slice (`cmd/server/testdata/tour/s5b/`, areas
`tour_s5b_*_test.go`, tests `TestTourS5b<Area>`), covers V&V and the suite:
test runs and results, evidence bundles, files and citations (with the
evidence download's Range requests), V&V coverage, matrix, gaps and the V&V
report, impact analysis, requirement quality and the quality-rule sets, the
product profile and parties, work items and the board's agent launches,
guided sessions and their copilot chat, stakeholder interviews with their
public participant routes and rate limits, and the shared-products pool;
`tour_s5b_test_runs_results_test.go` is its worked example. It added to the
framework, for the slices after it: `eventStream(n)`, which reads a
`text/event-stream` answer frame by frame and then closes it
(`tour_stream_test.go`, since a stream never ends); `tourArea.env`, an
area's own server variables, listed in its golden; `unordered` on an object,
for a Go map keyed by random ids; `tour.headerPattern`, a pattern for one
response header only, such as a `Retry-After` that counts down; and a
multipart part over 4 KiB recorded by its size and digest, so that an upload
made to reach a size limit (the evidence area's 1 MB limits, set through
`tourArea.env`) does not write its megabyte into the golden.
Regenerate one area with
`OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestTourS5b<Area>$'`,
or the slice with `-run '^TestTourS5b'`.

S5c, the third slice (`cmd/server/testdata/tour/s5c/`, areas
`tour_s5c_*_test.go`, tests `TestTourS5c<Area>`), covers identity and the
workspace: registration, sign-in and sign-out, the session cookie and the
workspace a request with no `X-Org-ID` resolves to, Google and OIDC
sign-on, email verification and password reset (with the mail they send),
the password, avatar and default workspace, the platform admin's lists,
invitations with registration closed, workspaces with their logo, plan,
features, limits, members, roles and teams, a project's members and team
grants, worker keys, runner keys, connector pairing and download, hosted
runners and runner sessions, notifications and web push, and billing. Its
areas boot under S4b's profiles where those change an answer (secure and
cross-site cookies, registration closed, self-hosted, tiers on), and the
rate-limit buckets that several routes share are drained through one route
and read through another. `tour_s5c_sessions_auth_test.go` is its worked
example. It added to the framework, in `tour_accounts_test.go`,
`tour_mail_test.go` and `tour_standin_test.go`: accounts that recorded
steps make (`tour.adopt`, `tour.session`), a signed-in request with no
`X-Org-ID` (`noOrgHeader`), waits for what the server does after it answers
(`tour.await`, `awaitOutbound`, `awaitMail`), values whose length varies by
design (`elide`, `tour.patternVarying`), an area's S4b profiles, a second
server to sign up on and files of its own (`tourArea.profiles`,
`signUpWithout`, `files`), a mail catcher that records every mail in the
golden, and stand-ins for Stripe, Google and an OIDC identity provider that
the recording proxy answers itself over TLS the server trusts, so that a
request that would leave the machine is answered and recorded instead.
Regenerate one area with
`OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestTourS5c<Area>$'`,
or the slice with `-run '^TestTourS5c'`; an area with stand-ins needs a
platform where Go reads `SSL_CERT_FILE` (Linux, the BSDs) and skips on macOS,
where `make check` accepts exactly those skips (`TestTourStandIns` and the
three stand-in areas) and names them; CI runs them on Linux.

### The vulnerability gate

The **Vulnerability scan** job (`vuln`) is a supply-chain gate on every pull
request, required by REQ-98. It runs two scanners:

- `govulncheck ./cmd/... ./internal/...` over the server and worker code.
  govulncheck is call-graph aware, so it reports only advisories the binaries
  can actually reach and stays quiet about a vulnerable package that nothing
  calls. It exits non-zero as soon as one is reachable.
- `npm audit --omit=dev --audit-level=high` in `frontend/`. `--omit=dev`
  keeps the gate on code that reaches a browser; a build-time-only advisory
  in the toolchain does not fail a PR. `e2e/` is not audited — it
  declares devDependencies only, so there is nothing for `--omit=dev` to see.

Run the same two checks before pushing:

```bash
make vuln
```

Two things follow from how the scanners work. The Go toolchain comes from the
`go` directive in `go.mod`, and govulncheck attributes standard-library
advisories to whichever toolchain builds the code — so **keeping that
directive on a current Go patch release is part of passing the gate**, and a
stdlib finding is usually fixed by bumping it rather than by touching any
dependency. And a frontend advisory that lives in a transitive package is
normally closed with an entry in the `overrides` block of
`frontend/package.json` (that is what pins `fast-uri`, among others), then
`npm install --package-lock-only` to refresh the lock file.

Findings are not suppressed. There is no allow-list and no
`--ignore`/`audit-level` escape hatch beyond the documented `high` threshold:
a reachable advisory either gets fixed or the gate stays red.

### CodeQL

`.github/workflows/codeql.yml` scans **this repository's own code**, which is
the half the vulnerability gate cannot see. govulncheck and npm audit answer
"is a dependency we use known to be vulnerable"; CodeQL answers "did we write
a bug" — injection, path traversal, unsafe deserialisation, and the other
mistakes that show up as dataflow from an untrusted source to a sensitive
sink. Neither scanner substitutes for the other.

Two languages, analysed separately: `go` with build mode `autobuild` (Go is
compiled, so CodeQL needs a build to observe it — autobuild runs
`go build ./...`, and build mode `none` is not offered for Go) and
`javascript-typescript` with `none` (read from source; building the browser
bundle would only slow the scan). `fail-fast` is off so one language failing
never hides the other's findings, and each uploads under its own category so
one language's results are never read as the other's being fixed.

It runs on four triggers:

- **pull requests** into `master` — the only point where a finding is cheap.
- **pushes to `master`** — the baseline the Security tab reflects.
- **`v*` tags** — every released version is scanned as itself, so a release
  can say when it was last analysed.
- **weekly**, Mondays at 04:27 UTC. This is the trigger the other three
  cannot replace: it re-analyses unchanged code against an updated query
  pack, which is how a newly published class of bug is found in old code.
  Monthly would widen that window fourfold for no saving — Actions minutes
  are free on a public repository, which is also why CodeQL costs nothing
  here at all. A private repository would need a GitHub Code Security
  licence.

**It interacts with promotion.** The promote workflow refuses while any check
on the master head is failing or still running, so a master CodeQL run now
sits between a merge and a promotion — a few minutes, and a red CodeQL check
will hold a release until the finding is triaged. That is the intended
trade: not shipping unscanned code is worth more than a faster promotion.

CodeQL cannot be run locally the way `make vuln` can — it needs the CodeQL
CLI and uploads its results to GitHub — so the pull request itself is the
first place a change to the workflow is exercised.

The hosted-runner provisioner in `internal/hosting` talks to the Docker
daemon through `github.com/moby/moby/client` (the renamed, still-maintained
Moby engine client), which is where the fixes for GO-2026-4887 and
GO-2026-4883 ship; the retired `github.com/docker/docker` module is no longer
a dependency.

## Database inspection

```bash
docker exec -it openv-postgres psql -U postgres -d openv

\dt                 -- list tables
\d artifacts        -- inspect a table
SELECT * FROM agent_runs ORDER BY created_at DESC LIMIT 10;
```

See `docs/data-model.md` for a table-by-table overview.

## Production builds

```bash
docker build -f Dockerfile.api -t openv-api:latest .
docker build -f frontend/Dockerfile -t openv-frontend:latest frontend
docker build -f Dockerfile.worker -t openv-worker:latest .   # hosted runner
```
