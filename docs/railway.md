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
# (scheme + host, no trailing slash, no path). Browsers reach the API through
# the frontend's /api/ proxy (section 3), so PUBLIC_URL — the base for OAuth
# redirect URIs and links — is the frontend origin too. The API's own domain
# stays for connectors, agent runners and MCP clients (OPENV_API_URL).
CORS_ORIGIN=https://<your-frontend-domain>.up.railway.app
PUBLIC_URL=https://<your-frontend-domain>.up.railway.app
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

# Railway terminates TLS, so the session cookie can be Secure. Leave
# CROSS_SITE_COOKIES unset: with the frontend proxying /api/ the cookie is
# first-party (SameSite=Lax). Set CROSS_SITE_COOKIES=true only for the legacy
# split setup where the app is built with REACT_APP_API_URL and calls the
# API's own *.up.railway.app domain — a different SITE (up.railway.app is on
# the Public Suffix List), so the cookie must be SameSite=None + Partitioned,
# which Safari and iOS refuse: the app cannot sign in on an iPhone that way.
SECURE_COOKIES=true

# Railway's edge terminates TLS and forwards the client address in
# X-Forwarded-For; without this the per-address throttles on sign-in,
# registration and the public interview routes key on the edge instead.
OPENV_TRUST_PROXY=1

# /metrics is open to anyone otherwise. openssl rand -hex 32
OPENV_METRICS_TOKEN=<long random string>
```

Optional — Google sign-in (see `docker-compose.yml` for details):

```dotenv
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
# Authorized redirect URI in the Google console (through the frontend's
# /api/ proxy, so the sign-in cookies land on the frontend origin):
#   https://<your-frontend-domain>.up.railway.app/api/v1/auth/google/callback
```

Optional — SMTP email notifications: the `OPENV_SMTP_*` variables from
[operations.md](operations.md) work unchanged.

Setting them also turns on **email verification for sign-ups**, for every
password account including the maintainer's own (the accounts that predate
the feature are unverified). Roll it out in two steps: add the `OPENV_SMTP_*`
variables together with `OPENV_EMAIL_VERIFICATION=off`, confirm a
notification email arrives, then remove the `off`. From that deploy each
password account meets the *Check your inbox* page on its next request and
verifies with one click on *Resend email*. Re-adding `off` switches it back
off at once. With no SMTP variables nothing is enforced.

Optional — **registration policy and session lifetime** (see
[operations.md](operations.md)):

```dotenv
# Close the public sign-up door. New accounts then arrive only through a
# workspace invitation LINK or the configured SSO provider.
OPENV_REGISTRATION=closed

# Shorten session lifetimes below the 720h/168h defaults (Go durations).
OPENV_SESSION_MAX_AGE=336h
OPENV_SESSION_IDLE=72h
```

**On the hosted instance, registration is open** — that is the default, and
none of these variables is set there today, so anyone who reaches the site can
create an account. To close it, set `OPENV_REGISTRATION=closed` on the **API**
service and redeploy it; the login page picks the policy up from
`GET /api/v1/auth/policy` with no frontend rebuild, since the SPA reads it at
runtime. Existing accounts and sessions are unaffected. Invite people from
*Workspace settings → Members* afterwards — with SMTP configured the
invitation is emailed, and without it the link is shown once for you to send.
The link is what admits and joins them: an invited address that never
receives it cannot sign up on a closed deployment.

Optional — web push notifications (REQ-109): generate one VAPID key pair for
the deployment (`make vapid-keys`, or `go run ./cmd/openv-vapid`) and set all
three variables on the **API** service:

```dotenv
OPENV_VAPID_PUBLIC_KEY=<public half from make vapid-keys>
OPENV_VAPID_PRIVATE_KEY=<private half — a secret; Railway stores it sealed>
# Operator contact for the push services. mailto: or https: only.
OPENV_VAPID_SUBJECT=mailto:you@example.com
```

With any of them unset, push stays off and everything else is unchanged. The
public key reaches browsers through `/api/v1/me/push/config`, so it needs no
frontend variable and no rebuild — but it is baked into every subscription a
member takes, so **rotating the pair invalidates every existing
subscription** and each device has to be turned on again.

Push also needs the app served over HTTPS on one origin with a registered
service worker, which is what section 3's frontend already does. See
[operations.md](operations.md) for what "high-signal" covers and the
per-device opt-in.

## 3. Frontend service

**Create → GitHub Repo**, pick the same repository again.

Service settings:

- **Root Directory**: `frontend`. Railway then uses
  `frontend/railway.json` (Dockerfile build from `Dockerfile.prod`).
- **Networking → Generate Domain**, target port **8080** (nginx listens
  there; it is fixed, not `PORT`-driven).

Variables:

```dotenv
# Where nginx proxies /api/ to: the API service over Railway's private
# network (http, port 8080). Read when the container starts; the name is
# re-resolved through Railway's internal DNS on every request, so API
# redeploys (which change its private address) need no frontend restart.
# Substitute your API service's actual name for `openv-api`.
API_UPSTREAM=${{openv-api.RAILWAY_PRIVATE_DOMAIN}}:8080
```

Leave `REACT_APP_API_URL` **unset** (or empty): the app then calls `/api` on
its own origin, the session cookie is first-party, and the frontend's content
security policy allows its own origin only. Setting it to the API's public
domain is the legacy split setup — see the `CROSS_SITE_COOKIES` note in
section 2 for why that cannot sign in on iOS.

Generate the frontend domain first, since the API's `CORS_ORIGIN`,
`PUBLIC_URL` and `FRONTEND_URL` all name it; then let both services deploy.

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

- `https://<api-domain>/health` returns OK, and so does
  `https://<frontend-domain>/api/v1/auth/config` (the proxy path: a 502 here
  means nginx cannot reach `API_UPSTREAM` — check the frontend's deploy log
  for the `openv: proxying /api/ to ...` line and the API service's name).
- The frontend loads and can sign in / create a project, on a phone too. A
  CORS error in the browser console means the app was built with
  `REACT_APP_API_URL` and `CORS_ORIGIN` does not exactly match the frontend
  origin.

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
  any check on the master head is failing or still running, cuts the
  release notes (below), then fast-forwards `release`, and Railway deploys
  that push.
- **Every release says what changed.** `RELEASE_NOTES.md` at the repository
  root is customer-facing: each pull request adds a bullet under
  `## Unreleased`, grouped under `### New features`, `### Maintenance
  updates` or `### Bug fixes` (the *Release notes* CI job refuses a PR that
  adds none, unless it carries the `no-release-notes` label). The promotion
  moves those bullets into a new section headed by a semantic version and
  the date — `## 0.2.0 — 2026-09-13` — committed to master before `release`
  is pushed. **The version is derived from the notes**: a release carrying
  anything under *New features* is a minor bump, one of only maintenance and
  fixes is a patch, and a major bump is the workflow's `major` input; the
  first release is `0.1.0`. Sections headed by a date alone are the releases
  from before OpenV had version numbers — read, never written, because they
  were announced under those names. A master whose notes name nothing newer
  than what `release` already carries is refused. The API embeds the file:
  `GET /api/v1/release` reports the running version and the parsed releases
  (never the file itself, which also holds what has not shipped), the first
  server to boot on a new release notifies every nightly-channel account
  (`release_published`), and open tabs poll the version and offer a reload.
  `scripts/release_notes.py` (stdlib Python) is the one implementation of
  these rules: `check`, `check-pr`, `cut`, `check-release`, `version`,
  `next`, `stable-version`, `cut-stable`.
- **Stable releases** (`docs/release-policy.md`): a stable release is one
  of the releases above, designated by a marker line under its heading —
  `Stable channel release since 2026-10-01.` — once it has served the
  nightly channel for seven days. The **Cut stable release** workflow
  (`.github/workflows/cut-stable.yml`) runs at 06:00 UTC on the first
  working day of the month: it cuts anything still under Unreleased as a
  release first, designates the newest release that has soaked and is newer
  than the current stable, commits to master and promotes. Run it by hand
  with `fix: true` to designate the newest release at once. The API's
  stable scheduler then moves each stable-channel workspace to it at that
  workspace's upgrade window, with the notes of every release since the
  previous stable merged group by group.
- **Nightly automation**: the **Nightly promotion** workflow
  (`.github/workflows/nightly-promote.yml`) runs at 03:00 UTC and is a
  no-op until staging exists (below).
- Rollback: `git push origin <known-good-sha>:release --force-with-lease`
  redeploys an earlier build (the API's schema migrations are forward-only,
  so only roll back across releases without new migrations), or use
  Railway's per-service deployment history to redeploy a previous image.

Set each Railway service's **Settings → Source → Branch** to `release`
(create the branch first: `git push origin master:release`). The
promotion workflow needs no Railway-side configuration — Railway just sees
a normal push to the connected branch.

## Staging

After the alpha, a second Railway environment named `staging` in the same
project, with its own Postgres and volume, both services connected to
`master`, and the same variables as production apart from its own
`PUBLIC_URL`, `CORS_ORIGIN`, `API_UPSTREAM` and a raised
`OPENV_REGISTER_IP_BURST` (the smoke suite registers its own users). Seed
its database from an anonymised copy; never point it at production data.
Then set the repository variable `STAGING_BASE_URL` to the staging
frontend's origin: from that moment the nightly promotion smoke-tests
master on staging every night and promotes when it passes and there is
something new under Unreleased. Enterprise customers previewing a stable
release use a staging copy of their own instance in the same way.

## Dedicated instances

A dedicated instance is this same deployment with `OPENV_DEPLOYMENT=dedicated`,
connected to a stable release rather than `release` (check out the commit
that designated it) and upgraded on the customer's date. It reads the
shared service's public release feed (`OPENV_RELEASE_FEED_URL`, default
`https://openv-production.up.railway.app/api/v1/public/release`) once a day
and warns every workspace's admins 30 and 7 days before its 90-day support
window closes, and once more when it has.

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
