# Development Guide

## Toolchain

The project targets **Go 1.25** (`go.mod`) and **Node 20** (frontend,
Create React App). The standard development toolchain is **Docker only** —
neither Go nor Node needs to be installed on the host. Every build target in
the `Makefile` runs inside `golang:1.25`, and the frontend image builds on
`node:20-alpine`.

If you do have a local Go 1.25+ / Node 20+ install, the commands below work
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

The frontend is a CRA app in `frontend/`. Its dependencies have peer-dep
conflicts under npm's default resolver, so **`--legacy-peer-deps` is
required** (the `frontend/Dockerfile` already does this):

```bash
docker run --rm -v "$(pwd)/frontend:/app" -w /app node:20 \
  npm install --legacy-peer-deps

docker run --rm -v "$(pwd)/frontend:/app" -w /app -p 3000:3000 \
  -e REACT_APP_API_URL=http://localhost:8080 node:20 npm start
```

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
enables the integration tests), a frontend `npm ci` + `tsc` + build, Docker
image builds, a Playwright smoke journey against the composed stack, and the
vulnerability scan below. Still run `make test` locally before pushing.

### The vulnerability gate

The **Vulnerability scan** job (`vuln`) is a supply-chain gate on every pull
request, required by REQ-98. It runs two scanners:

- `govulncheck ./cmd/... ./internal/...` over the server and worker code.
  govulncheck is call-graph aware, so it reports only advisories the binaries
  can actually reach and stays quiet about a vulnerable package that nothing
  calls. It exits non-zero as soon as one is reachable.
- `npm audit --omit=dev --audit-level=high` in `frontend/`. `--omit=dev`
  keeps the gate on code that reaches a browser; a build-time-only advisory
  in the `react-scripts` tree does not fail a PR. `e2e/` is not audited — it
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
