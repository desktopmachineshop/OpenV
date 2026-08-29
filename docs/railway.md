# Deploying OpenV on Railway

How to run OpenV on [Railway](https://railway.com): one Railway project
containing a Postgres database and two services built from this repository —
the Go API (`Dockerfile.api`) and the static React frontend
(`frontend/Dockerfile.prod`, nginx).

The code is already Railway-aware: the API prefers a `DATABASE_URL`
connection string when one is set (`cmd/server/main.go`) and listens on the
`PORT` Railway injects; the frontend image serves on container port 8080.
`railway.json` (API) and `frontend/railway.json` (frontend) carry the
build/deploy settings, so most of the setup is wiring environment variables.

## 1. Create the database

In your Railway project: **Create → Database → PostgreSQL**. Nothing to
configure; the service exposes `DATABASE_URL` for other services to
reference.

## 2. API service

**Create → GitHub Repo**, pick this repository.

Service settings:

- **Root Directory**: leave as `/`. Railway picks up the root `railway.json`
  automatically (Dockerfile build from `Dockerfile.api`, health check on
  `/health`).
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

# Railway containers have no Docker daemon: hosted runner containers cannot
# be provisioned there. Run agent workers (agentd) on your own machine
# instead, pointed at the API domain — see docs/agents.md.
HOSTED_RUNNERS=off
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

## 4. Verify

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
  own machine with `RUNNER_API_URL` pointed at the public API domain.
- **One volume per service**: `/data` holds both agent definitions
  (`$OPENV_DATA_DIR/agents`) and uploads (`UPLOADS_DIR=/data/uploads`).
- **Connector download bundles** (`CONNECTOR_DIST_DIR`) are not built into
  the API image; the download endpoints will 404 unless you bake `dist/`
  into a custom image.
- **Backups**: Railway's Postgres backups cover the database. Attachment
  files live in the service volume — `make backup` from
  [operations.md](operations.md) assumes docker compose and does not apply
  here.
- **Deploys**: Railway auto-deploys on push to the connected branch; both
  services rebuild independently. The API runs schema migrations on boot,
  serialized by an advisory lock, so redeploys are safe.
