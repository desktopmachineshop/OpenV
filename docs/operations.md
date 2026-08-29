# Operations

Running OpenV in production and keeping its data safe.

- [Production deployment (compose overlay)](#production-deployment)
- [Backup and restore](#backup-and-restore)
- [Windows notes](#windows-notes)

## Production deployment

Production runs the same stack as dev, with a second compose file layered on
top. `docker-compose.prod.yml` is an overlay — it is never used alone:

```sh
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
# or:
make prod-up
```

Compared to the dev stack, the overlay:

- builds the frontend from `frontend/Dockerfile.prod` — a static React build
  served by nginx (unprivileged, container port 8080), published on host port
  80 by default (`FRONTEND_PORT` overrides it)
- adds healthchecks: the API is probed on `GET /health` (unauthenticated, see
  `internal/api/authmiddleware.go`), the frontend on nginx's `/health`;
  the frontend waits for the API to be healthy before starting
- sets `restart: unless-stopped` on all services
- takes the Postgres password from the environment instead of the dev default
- stops publishing Postgres port 5432 on the host (backups exec into the
  container instead)
- sets memory limits via `mem_limit` (honored by plain, non-swarm
  `docker compose`): 1 GB for Postgres and the API, 256 MB for the frontend

> **Not covered yet:** hosted-runner containers that the API provisions on the
> Docker daemon (`internal/hosting/docker.go`) are created outside compose and
> currently run with **no resource caps**. Budget host memory accordingly if
> `HOSTED_RUNNERS` is enabled.

### Required configuration (.env)

Create a `.env` file next to the compose files (`docker compose` reads it
automatically; it is gitignored). Required:

```dotenv
# Database password (replaces the dev default "postgres").
POSTGRES_PASSWORD=change-me

# Public URL browsers use to reach the API. Baked into the frontend bundle
# at BUILD time - changing it requires `up -d --build`.
REACT_APP_API_URL=https://openv.example.com:8080

# Public origin the frontend is served from (CORS allow-origin).
CORS_ORIGIN=https://openv.example.com

# Shared key agent workers use to authenticate with the API (replaces the
# dev default "dev-worker-key"). Generate a long random string, e.g.:
#   openssl rand -hex 32
WORKER_API_KEY=generate-a-long-random-string
```

Recommended in production (see `docker-compose.yml` for the full list):

```dotenv
FRONTEND_PORT=80

# Google sign-in + correct redirect URLs:
PUBLIC_URL=https://openv.example.com:8080
FRONTEND_URL=https://openv.example.com
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
```

If a required variable is missing, `docker compose ... up`/`config` fails with
an error naming the variable rather than starting with dev defaults.

Notes:

- The overlay terminates plain HTTP. For TLS put a reverse proxy (Caddy,
  Traefik, nginx) in front of the frontend and API ports.
- When the API does sit behind a reverse proxy, also set `OPENV_TRUST_PROXY=1`
  on the api service so per-IP rate limiting on the public interview endpoints
  keys on the real client address from `X-Forwarded-For`/`X-Real-IP` (make
  sure the proxy overwrites those headers). Leave it unset when clients reach
  the API directly: the headers are client-supplied, and trusting them would
  let anyone dodge per-IP limits — or exhaust another client's bucket — by
  spoofing a header.
- `REACT_APP_API_URL` is a build argument: the React bundle is static, so the
  frontend image must be rebuilt when it changes.
- If you switch an existing deployment from the dev Postgres password, the
  database was already initialized with the old one — `POSTGRES_PASSWORD` on
  an existing volume does not change it. Run
  `docker exec -it openv-postgres psql -U postgres -c "ALTER USER postgres PASSWORD 'new-password';"`
  once, then start the stack with the new value.

To stop: `docker compose -f docker-compose.yml -f docker-compose.prod.yml down`
(or `make prod-down`). Data lives in named volumes and survives `down`.

## Backup and restore

Everything the stack persists lives in three named volumes: the Postgres data
directory, `openv-data` (agent definitions and other API data) and
`uploads_data` (attachments). `make backup` captures all of it in one bundle:

```sh
make backup
# -> backups/openv-backup-<YYYYMMDD-HHMMSS>.tar.gz
```

The bundle contains:

| File                  | Contents                                             |
| --------------------- | ---------------------------------------------------- |
| `openv-db.sql`        | `pg_dump --clean --if-exists` of the `openv` database |
| `openv-data.tar.gz`   | the `openv-data` volume (`/data` in the API)         |
| `uploads-data.tar.gz` | the `uploads_data` volume (`/uploads` in the API)    |

The stack must be running: the dump execs `pg_dump` inside the
`openv-postgres` container (so no client tools are needed on the host and the
dump version always matches the server), and the volume tars run in a
throwaway `alpine` container attached with `--volumes-from openv-api` (so
volume names never need to be known). The Postgres data volume itself is not
copied — the SQL dump is the portable representation of it. Backups are
consistent snapshots per component; for a strictly consistent full snapshot,
run it during a quiet period.

`backups/` is gitignored. Copy bundles somewhere off the host.

### Restore

```sh
make restore BACKUP=backups/openv-backup-20260827-120000.tar.gz
```

The bundle must be inside `backups/`. Restore is **destructive**: it stops
`openv-api`, replaces the contents of the `openv-data` and `uploads_data`
volumes, replays the SQL dump into the `openv` database (the dump's
`DROP ... IF EXISTS` statements clear existing objects first), and starts the
API again. The Postgres container must be running. Restoring a backup from an
older schema version onto a newer server is fine at the Postgres level, but
the API re-applies its schema migrations (`postgres.Migrate`) on top at
startup.

## Windows notes

The Makefile targets are written for a POSIX shell (dockerized, so the host
needs no Go, Node, or Postgres tools — only Docker and GNU make). On Windows:

- run `make` from **WSL** (recommended), or from Git Bash with a
  Windows-native GNU make on `PATH` (Git for Windows does not ship `make`)
- Git Bash's MSYS layer rewrites arguments that look like POSIX paths
  (`/tmp/x` becomes `C:/Users/.../Temp/x`), which breaks docker commands that
  reference container paths. The recipes are written to avoid this (container
  paths only appear inside `sh -c '...'` strings), but if you adapt them,
  keep that in mind — or set `MSYS_NO_PATHCONV=1`.
