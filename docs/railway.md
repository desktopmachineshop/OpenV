# Deploying OpenV on Railway

How to run OpenV on [Railway](https://railway.com): one Railway project
containing a Postgres database and two services built from this repository —
the Go API (`Dockerfile.api`) and the static React frontend
(`frontend/Dockerfile.prod`, nginx).

The code is already Railway-aware: the API prefers a `DATABASE_URL`
connection string when one is set (`cmd/server/main.go`) and listens on the
`PORT` Railway injects; the frontend image serves on container port 8080.
The frontend carries its build/deploy settings in `frontend/railway.json`;
the API's live on the service itself (see below). Most of the setup is wiring
environment variables.

## 1. Create the database

In your Railway project: **Create → Database → PostgreSQL**. Nothing to
configure; the service exposes `DATABASE_URL` for other services to
reference.

## 2. API service

**Create → GitHub Repo**, pick this repository.

Service settings:

- **Root Directory**: leave as `/`.
- **Build → Dockerfile Path**: `Dockerfile.api`.
- **Deploy → Healthcheck Path**: `/health`, timeout 120s; restart policy
  `ON_FAILURE`, 10 retries.

  These live on the service rather than in a root `railway.json` on purpose.
  A config file at the repository root applies to **every** service that
  builds from that root and overrides the service's own Build settings — so
  a second service built from this repo (the runner pool in section 4) would
  silently build `Dockerfile.api` instead of its own image, and neither the
  service's Dockerfile Path nor `RAILWAY_DOCKERFILE_PATH` could override it.
  `frontend/railway.json` is fine to keep: it sits under the frontend's own
  root directory, so it reaches only that service.
- **Networking → Generate Domain**, target port **8080** (any port works —
  the API reads Railway's `PORT` — but 8080 matches the default). Note the
  domain; it is your public API URL.
- **Volume** (right-click the service → Attach Volume, or Settings →
  Volumes): mount path `/data`. Railway allows one volume per service, so
  uploads are pointed inside it via `UPLOADS_DIR` below. Without a volume,
  attachments and agent definitions are lost on every deploy.

Variables (service → **Variables**):

```dotenv
# Railway's Postgres reference. lib/pq defaults to sslmode=require and the
# internal database endpoint does not terminate TLS, so append sslmode
# explicitly. Traffic stays on Railway's private network.
DATABASE_URL=${{Postgres.DATABASE_URL}}?sslmode=disable

# Persistent storage (both inside the single /data volume).
OPENV_DATA_DIR=/data
UPLOADS_DIR=/data/uploads

# Public origins. CORS_ORIGIN must be exactly the frontend's origin
# (scheme + host, no trailing slash, no path).
CORS_ORIGIN=https://<your-frontend-domain>.up.railway.app
PUBLIC_URL=https://<your-api-domain>.up.railway.app
FRONTEND_URL=https://<your-frontend-domain>.up.railway.app

# Legacy shared worker key (org-scoped keys minted in workspace settings are
# preferred, but never leave this on a guessable value if you set it).
#   openssl rand -hex 32
WORKER_API_KEY=<long random string>

# Transient runners: set this to a long random string (openssl rand -hex 32)
# and give the runner-pool service the same value. Members can then lease a
# pre-warmed cloud runner from the UI instead of installing the connector.
# Leave it unset to keep the feature off. See section 4 below.
RUNNER_POOL_KEY=<long random string>

# Railway containers have no Docker daemon: hosted runner containers cannot
# be provisioned there. Run agent workers (agentd) on your own machine
# instead, pointed at the API domain — see docs/agents.md.
HOSTED_RUNNERS=off

# The frontend and API live on two different *.up.railway.app domains, which
# browsers treat as different SITES (up.railway.app is on the Public Suffix
# List). Without these, the session cookie is SameSite=Lax and is never sent
# on the frontend's cross-site API calls — register/login appear to succeed
# but every request after them is 401. CROSS_SITE_COOKIES issues cookies with
# SameSite=None and implies Secure. Not needed when both services sit behind
# one domain.
SECURE_COOKIES=true
CROSS_SITE_COOKIES=true
```

Optional — Google sign-in (see `docker-compose.yml` for details):

```dotenv
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
# Authorized redirect URI in the Google console:
#   https://<your-api-domain>.up.railway.app/api/v1/auth/google/callback
```

Optional — SMTP email notifications: the `OPENV_SMTP_*` variables from
[operations.md](operations.md) work unchanged.

## 3. Frontend service

**Create → GitHub Repo**, pick the same repository again.

Service settings:

- **Root Directory**: `frontend`. Railway then uses
  `frontend/railway.json` (Dockerfile build from `Dockerfile.prod`).
- **Networking → Generate Domain**, target port **8080** (nginx listens
  there; it is fixed, not `PORT`-driven).

Variables:

```dotenv
# Baked into the static bundle at BUILD time (Dockerfile.prod declares the
# matching ARG, and Railway passes service variables as build args).
# Changing it requires a redeploy. No trailing slash.
REACT_APP_API_URL=https://<your-api-domain>.up.railway.app
```

Because the two domains reference each other (`CORS_ORIGIN` on the API,
`REACT_APP_API_URL` on the frontend), generate both domains first, then fill
in the variables, then let both services deploy.

## 4. Runner pool service (optional — transient runners)

Transient runners let a member lease a pre-warmed cloud runner from the UI and
sign their agents into it from the browser, with nothing to download. Railway
cannot run the hosted-runner tier (no Docker socket), but it runs a pool
perfectly well: a pool is just replicas of one service.

**Create → GitHub Repo**, pick the same repository a third time.

Service settings:

- **Root Directory**: leave as `/`, and set **Settings → Build → Dockerfile
  Path** to `Dockerfile.worker` (this service is the runner image, not the
  API). If it builds the API instead, something has reintroduced a
  `railway.json` at the repository root — see the note in section 2.
- **Networking**: none. A pool node makes outbound calls only; do not generate
  a domain.
- **Settings → Deploy → Replicas**: the number of members who can hold a cloud
  runner at once. Start at 2 and raise it when members start seeing "every
  cloud runner is in use".
- **Volume**: none. A pool node's state is meant to be thrown away, and it is
  wiped between leases anyway.

Variables:

```dotenv
# Must match the API service's RUNNER_POOL_KEY exactly.
RUNNER_POOL_KEY=${{openv-api.RUNNER_POOL_KEY}}

# The API as seen from inside the pool node. Railway's private network is
# cheaper than egressing to the public domain.
OPENV_API_URL=http://${{openv-api.RAILWAY_PRIVATE_DOMAIN}}:8080

# Per-lease HOME directories (created and deleted per lease).
RUNNER_SESSION_ROOT=/data/sessions

# Each replica gets a distinct hostname, which is the node's identity.
# Unset the image's hosted-runner default — a leased node is a member's own
# runner and does sign their CLIs in.
OPENV_HOSTED=
```

Substitute your API service's actual name for `openv-api` in the references
above. With `RUNNER_POOL_KEY` set on both services, the **Cloud runner** card
appears in each member's settings.

## 5. Verify

- `https://<api-domain>/health` returns OK.
- The frontend loads and can sign in / create a project. A CORS error in
  the browser console means `CORS_ORIGIN` does not exactly match the
  frontend origin.

## Release pipeline

Railway auto-deploys every push to the branch each service is connected to.
Connected to `master`, that means every merged PR rebuilds the live product.
To decouple shipping from merging, both services connect to the **`release`**
branch instead:

- `release` only ever fast-forwards to `master` — it carries no commits of
  its own.
- Merges to `master` run CI as usual but deploy nothing.
- **Promotions are batched and human-initiated** (nightly or weekly, at the
  maintainer's discretion): each one rebuilds both services, and build
  minutes currently outweigh real usage. Merged-but-unpromoted work sitting
  on `master` is the normal resting state; agents must not promote unasked.
- To ship: run the **Promote to release** workflow from the GitHub Actions
  tab (`.github/workflows/promote-release.yml`). It refuses to promote while
  any check on the master head is failing or still running, then
  fast-forwards `release`, and Railway deploys that push.
- Rollback: `git push origin <known-good-sha>:release --force-with-lease`
  redeploys an earlier build (the API's schema migrations are forward-only,
  so only roll back across releases without new migrations), or use
  Railway's per-service deployment history to redeploy a previous image.

Set each Railway service's **Settings → Source → Branch** to `release`
(create the branch first: `git push origin master:release`). The
promotion workflow needs no Railway-side configuration — Railway just sees
a normal push to the connected branch.

## Notes and limitations on Railway

- **Hosted runners are unavailable** (`HOSTED_RUNNERS=off`): the API
  provisions runner containers via the host Docker socket, which Railway
  does not expose. Use host-side workers (`make worker`, `agentd`) on your
  own machine with `RUNNER_API_URL` pointed at the public API domain — or
  the **transient runner pool** in section 4, which needs no Docker daemon
  and gives members a runner without installing anything.
- **Pool replicas are billed while idle.** Pre-warming is the point (a lease
  is ready in seconds), but an idle replica still costs what an idle
  container costs. Size the pool to real concurrent use, and leave
  `RUNNER_POOL_KEY` unset on deployments that do not want the feature.
- **One volume per service**: `/data` holds both agent definitions
  (`$OPENV_DATA_DIR/agents`) and uploads (`UPLOADS_DIR=/data/uploads`).
- **Connector downloads** are baked into the API image (`Dockerfile.api`
  builds a single self-contained executable per OS — agentd and openv-mcp
  embedded — into `./dist`, the `CONNECTOR_DIST_DIR` default), so the
  "Download for Windows/Linux" buttons work out of the box. macOS has no
  prebuilt download.
- **Backups**: Railway's Postgres backups cover the database. Attachment
  files live in the service volume — `make backup` from
  [operations.md](operations.md) assumes docker compose and does not apply
  here.
- **Deploys**: Railway auto-deploys on push to the connected branch; both
  services rebuild independently. The API runs schema migrations on boot,
  serialized by an advisory lock, so redeploys are safe.
