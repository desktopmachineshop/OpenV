# Operations

Running OpenV in production and keeping its data safe.

- [Production deployment (compose overlay)](#production-deployment)
- [Railway deployment](railway.md) — managed hosting, separate guide
- [Backup and restore](#backup-and-restore)
- [Scheduled backups (opt-in sidecar)](#scheduled-backups)
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

Optional — email notifications (issue #187). Strictly opt-in: with
`OPENV_SMTP_HOST` unset the mailer is a no-op and only in-app + live (SSE)
notifications are delivered, so dev and existing deployments are unaffected.
Set these to turn on email delivery of the higher-signal notification types:

```dotenv
# SMTP transport (stdlib net/smtp; PLAIN auth when USER is set).
OPENV_SMTP_HOST=smtp.example.com
OPENV_SMTP_PORT=587                 # default 587
OPENV_SMTP_USER=notifications@example.com
OPENV_SMTP_PASSWORD=...
OPENV_SMTP_FROM=notifications@example.com   # default: OPENV_SMTP_USER

# Which notification types email (comma-separated). Default:
#   run_failed,proposal_pending,review_requested,budget_threshold
# Chatter @mentions and interview_completed are intentionally in-app only.
OPENV_EMAIL_NOTIFICATION_TYPES=run_failed,proposal_pending,review_requested,budget_threshold
```

Deep links in emails point at the app UI using `FRONTEND_URL` (falling back to
`PUBLIC_URL`), so set `FRONTEND_URL` to the externally reachable frontend base.
Delivery is best-effort: a send failure is logged and the run/notification
still succeeds. Each user can opt out under Settings → Notifications (stored as
`users.email_notifications`, default on); the opt-out only matters once SMTP is
configured.

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

The stack must be running. Both the manual `make backup` and the scheduled
sidecar (below) run the **same** recipe — `scripts/backup.sh` — so there is one
source of truth. `make backup` runs it once in a throwaway sidecar container:

```sh
docker compose -f docker-compose.yml -f docker-compose.backup.yml \
  run --rm --no-deps backup --once
```

The sidecar is the stock `postgres:15-alpine` image (already pulled for
Postgres, so it ships a version-matched `pg_dump` plus busybox `tar`/`find`)
with `scripts/backup.sh` bind-mounted in — nothing to build. The script dumps
the database over the compose network (`pg_dump -h postgres`, so no host client
tools are needed and the dump version always matches the server) and tars the
`openv-data` and `uploads_data` volumes, which it mounts read-only. The
Postgres data volume itself is not copied — the SQL dump is the portable
representation of it. The bundle is written to a hidden `.partial` name and
renamed only on success, so an interrupted run never leaves a bundle that looks
complete. Backups are consistent snapshots per component; for a strictly
consistent full snapshot, run it during a quiet period.

After writing a bundle the script prunes bundles older than
`BACKUP_RETENTION_DAYS` (default 7; set 0 to keep everything).

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

## Scheduled backups

`make backup` is a manual, one-shot snapshot. To take backups automatically on
a schedule, enable the **opt-in backup sidecar** — a small companion service
defined in `docker-compose.backup.yml`. It runs `scripts/backup.sh --loop`,
which produces exactly the bundles described above on a fixed interval and
prunes old ones. It complements the manual target rather than replacing it:
both call the same script, so bundles are interchangeable and `make restore`
restores either.

The sidecar is **not part of the default stack**. It only runs when you layer
its overlay in. Nothing changes for deployments that don't opt in.

### Enabling it

Layer `docker-compose.backup.yml` on top of your normal compose command:

```sh
# dev / plain stack
docker compose -f docker-compose.yml -f docker-compose.backup.yml up -d

# production (add it after the prod overlay)
docker compose -f docker-compose.yml -f docker-compose.prod.yml \
               -f docker-compose.backup.yml up -d --build
```

This starts a `backup` container (`openv-backup`) alongside the stack. It takes
its first backup immediately, then repeats every `BACKUP_INTERVAL_SECONDS`.
Follow it with `docker logs -f openv-backup`.

To stop just the scheduled backups, remove that one service without touching
the rest of the stack:

```sh
docker rm -f openv-backup
```

### Configuration

Put these in the same `.env` the rest of the stack reads (all optional):

```dotenv
# Seconds between automated backups (default 86400 = daily).
BACKUP_INTERVAL_SECONDS=86400

# Prune automated + manual bundles older than this many days (default 7).
# Set 0 to keep every bundle (no automatic pruning).
BACKUP_RETENTION_DAYS=7

# Where bundles land on the host. Defaults to ./backups — the same directory
# make backup and make restore use. Point it at a mounted disk to keep
# backups off the system volume, e.g. /mnt/backups/openv.
BACKUP_HOST_DIR=./backups
```

The sidecar reads `POSTGRES_PASSWORD` (the same variable the prod overlay
requires) to authenticate `pg_dump`; on the dev stack it falls back to the dev
default.

### Where backups land and retention

Automated bundles are written to `BACKUP_HOST_DIR` (default `backups/`) with
the same `openv-backup-<YYYYMMDD-HHMMSS>.tar.gz` naming as manual ones. After
each run the script deletes bundles last modified more than
`BACKUP_RETENTION_DAYS` days ago (both automated and manual bundles in that
directory). `backups/` is gitignored; the directory is still not off-host — copy
bundles (or point `BACKUP_HOST_DIR` at) durable, off-host storage.

### Restoring from an automated backup

Automated bundles are byte-for-byte the same format as manual ones, so restore
is unchanged — pick a bundle and run the existing target:

```sh
make restore BACKUP=backups/openv-backup-20260827-020000.tar.gz
```

See [Restore](#restore) above for what it does (it is destructive).

### Interval vs. exact times — the tradeoff

The sidecar uses a simple sleep-interval loop rather than a cron daemon: it is
self-contained (no extra process to supervise inside the container), robust
(one failed run is logged and retried on the next tick instead of killing the
loop), and matches the compose-first deployment — enable it with one overlay,
no host-level setup. The tradeoff is that runs are spaced by elapsed time from
container start, not pinned to wall-clock times like "02:00 daily"; the phase
resets if the container restarts. If you need backups at an exact time, leave
the sidecar off and instead drive the manual target from the host scheduler —
e.g. a cron entry or systemd timer running `make backup` (which invokes the
same `scripts/backup.sh`):

```cron
# /etc/cron.d/openv-backup — 02:00 daily, from the repo checkout
0 2 * * *  deploy  cd /opt/openv && make backup >> /var/log/openv-backup.log 2>&1
```

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
