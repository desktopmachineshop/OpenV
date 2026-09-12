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

> **Hosted-runner containers** that the API provisions on the Docker daemon
> (`internal/hosting/docker.go`) are created outside compose, so the
> `mem_limit` values above do not apply to them. They get their own memory and
> CPU caps from the workspace's plan limits (`runner_memory_mb`,
> `runner_cpus`; see `docs/agents.md`). Budget host memory for them separately
> if `HOSTED_RUNNERS` is enabled.

### Required configuration (.env)

Create a `.env` file next to the compose files (`docker compose` reads it
automatically; it is gitignored). Required:

```dotenv
# Database password (replaces the dev default "postgres").
POSTGRES_PASSWORD=change-me

# Public origin the frontend is served from (CORS allow-origin). The
# frontend's nginx proxies /api/ to the api service, so browsers reach the
# API on this same origin and the session cookie is first-party.
CORS_ORIGIN=https://openv.example.com

# Shared key agent workers use to authenticate with the API (replaces the
# dev default "dev-worker-key"). Generate a long random string, e.g.:
#   openssl rand -hex 32
WORKER_API_KEY=generate-a-long-random-string
```

Recommended in production (see `docker-compose.yml` for the full list):

```dotenv
FRONTEND_PORT=80

# Google sign-in + correct redirect URLs. PUBLIC_URL is where browsers reach
# the API: the frontend origin, since /api/ is proxied there.
PUBLIC_URL=https://openv.example.com
FRONTEND_URL=https://openv.example.com

# Split deployment only: browsers reach the API on its own origin instead of
# through the frontend's proxy. Baked into the frontend bundle at BUILD time
# (`up -d --build` to change). Cookies are then cross-site, which needs
# CROSS_SITE_COOKIES=true on the api service and does not work on Safari/iOS.
# REACT_APP_API_URL=https://openv.example.com:8080
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

### What "high-signal" means

Both side channels — email and web push — carry the same four notification
types, and only those:

| Type | Fires when |
|---|---|
| `run_failed` | An agent run you launched finished with an error |
| `proposal_pending` | An agent submitted a change that needs your approval |
| `review_requested` | An artifact entered the review queue (reviewers only) |
| `budget_threshold` | Workspace spend crossed 80 % or 100 % of its budget |

Chatter `@mention`s and `interview_completed` are deliberately **not** in the
set: they are frequent enough that a mailbox — never mind a buzzing pocket —
would stop being worth reading. They stay in-app, where the bell and its SSE
stream deliver them live. Either list can be overridden per deployment with
`OPENV_EMAIL_NOTIFICATION_TYPES` / `OPENV_PUSH_NOTIFICATION_TYPES`; an unset,
empty, or separators-only value falls back to the four above.

### Optional — web push notifications (REQ-109)

Web push sends the same high-signal types to a member's phone or desktop even
when OpenV is closed. As opt-in as email: with no VAPID key pair configured,
`GET /api/v1/me/push/config` answers `enabled: false`, the settings toggle
explains that the server has no keys, and nothing is ever sent.

Generate one key pair **per deployment** — the public half is what browsers
subscribe with, so rotating it invalidates every existing subscription:

```console
$ make vapid-keys          # or: go run ./cmd/openv-vapid
OPENV_VAPID_PUBLIC_KEY=BF...
OPENV_VAPID_PRIVATE_KEY=oG...
OPENV_VAPID_SUBJECT=mailto:admin@example.com
```

| Variable | Purpose |
|---|---|
| `OPENV_VAPID_PUBLIC_KEY` | Application server key handed to browsers. Unset ⇒ push off |
| `OPENV_VAPID_PRIVATE_KEY` | Signs the request to the push service. Treat as a secret; never commit it |
| `OPENV_VAPID_SUBJECT` | Operator contact, `mailto:` or `https:` only — anything else leaves push off |
| `OPENV_PUSH_NOTIFICATION_TYPES` | Comma-separated type override (default: the four high-signal types) |
| `OPENV_PUSH_ENDPOINT_HOSTS` | Comma-separated EXTRA push-service hosts accepted in a subscription endpoint (see below). Unset ⇒ the built-in list only |

Edit `OPENV_VAPID_SUBJECT` to an address you actually read: push services use
it to reach the operator about a misbehaving deployment. A half-configured or
malformed set leaves push off and says why in the boot log
(`push: web push enabled` / `push: VAPID key pair incomplete` / `push:
OPENV_VAPID_SUBJECT must be a mailto: or https: URI`).

#### Which push services a subscription may name

A subscription endpoint is a URL this server will later POST to, from inside
the deployment's network and with no authentication — so it is only accepted
when its **host** is a known push service. Endpoints are minted by browsers,
and those are these:

| Browser | Host pattern |
|---|---|
| Chrome, Chromium, Android | `fcm.googleapis.com` |
| Safari (iOS / iPadOS / macOS) | `*.push.apple.com` |
| Edge (Windows Notification Service) | `*.notify.windows.com` |
| Firefox | `push.services.mozilla.com`, `updates.push.services.mozilla.com`, `*.push.services.mozilla.com` |

`*.` matches one or more leading labels and never the bare domain. Anything
else — another host, an address literal (`https://127.0.0.1/…`,
`https://169.254.169.254/…`), a port other than 443, embedded credentials, or
plain `http` — is refused with **400**. No name is ever resolved: DNS would
only add a window in which a name that answers publicly now answers with an
internal address at send time, so the list itself is the guard.

A self-hosted push service (a Mozilla autopush of your own, a UnifiedPush
distributor) is added with `OPENV_PUSH_ENDPOINT_HOSTS`, which **extends** the
built-in list rather than replacing it:

```bash
OPENV_PUSH_ENDPOINT_HOSTS=push.example.internal,*.push.corp.example
```

Entries are hosts, exact or leading-wildcard, and are read per request, so a
change takes effect on restart of the process that serves the API. An address
literal is refused even when listed — name the service.

Deep links in the notification are **same-origin paths** (`/projects/…`): the
service worker follows a tap with `WindowClient.navigate`, which refuses a
cross-origin URL, so unlike the emails they do not use `FRONTEND_URL`.
Members turn push on **per device**, under Settings → Notifications → *Push
notifications on this device*: the browser asks for permission, subscribes,
and the subscription is stored in `push_subscriptions` (one row per device,
unique on the endpoint). The same switch withdraws it. The per-user opt-in
lives in `users.push_notifications` and defaults **off** — unlike email, push
only exists once someone has granted a browser permission.

Sends happen off the request path, on a fixed pool of 8 workers fed by a
1024-deep queue, and each request to a push service times out after 10s. They
are best-effort: a push service answering 404 or 410 means the subscription is
gone for good and the row is deleted; any other failure stamps `failed_at`
and keeps it, which the next successful send clears. If a push service is slow
enough to fill the queue, further notifications are **dropped** rather than
allowed to back up into the request path, and each drop logs
`push: dispatch queue full; notification not pushed` with a running total —
in-app and SSE delivery are unaffected.

Browser support is the usual caveat: on iOS and iPadOS, push works only for
an app **installed to the Home Screen** (Safari 16.4+). The settings toggle
says so when the browser cannot do it.

### Email verification for sign-ups

Setting `OPENV_SMTP_HOST` also switches on **email verification** for password
accounts (SEC-15 / REQ-95). The boot log says which state applies:

- `email verification: required` — SMTP is configured and
  `OPENV_EMAIL_VERIFICATION` is not `off`. A new password account is signed in
  but meets a *Check your inbox* page until the emailed link is clicked; every
  other API call from that session answers
  `403 {"error":"email not verified","code":"email_unverified"}`. The page
  offers resend, a corrected address (applied only when its link is
  confirmed) and sign-out. Links are valid 24 hours and work once; they point
  at `FRONTEND_URL/verify-email`.
- `email verification: disabled (no OPENV_SMTP_HOST)` — accounts are verified
  at sign-up and nothing is enforced (the default for a self-hosted stack,
  dev, CI).
- `email verification: disabled (OPENV_EMAIL_VERIFICATION=off)` — the
  operator's switch, immediate, no data change.

Accounts created through Google or OIDC are verified by the provider and never
meet the page. **Every password account that existed before the feature is
unverified**, including the first admin: the moment verification becomes
required they meet the page on their next request (a live session included,
not only at sign-in) and verify with one click on *Resend email*. To turn it
on without that step, grandfather them first:

```sql
UPDATE users SET email_verified = TRUE, email_verified_at = NOW()
WHERE auth_provider = 'password' AND NOT email_verified;
```

The same statement scoped to one email unblocks a single account whose mail
cannot be delivered. Resend and change-of-address are throttled per account
(`OPENV_VERIFY_RESEND_BURST`, default 3; `OPENV_VERIFY_RESEND_REFILL_PER_HOUR`,
default 6). Worker keys, run tokens and the runner pool key never meet the
gate: only browser sessions do.

### Who may create an account

`OPENV_REGISTRATION` decides whether the deployment has a public sign-up door
(REQ-95). The boot log always says which state is in force.

| Variable             | Default | Meaning                                                            |
| -------------------- | ------- | ------------------------------------------------------------------ |
| `OPENV_REGISTRATION` | `open`  | `open`: anyone may sign up. `closed`: only an invitation link and SSO |

With `OPENV_REGISTRATION=closed` there are exactly two doors:

- an **invitation link**: `POST /api/v1/auth/register` carrying an
  `invite_token` that is live and was issued to **the address being
  registered**;
- **single sign-on**, which never meets the policy at all — the identity
  provider is doing the admitting.

Everything else answers
`403 {"error":"registration is closed","code":"registration_closed"}`. A
pending invitation for the address, *without* its link, is deliberately not a
door: answering differently for an invited address would turn the sign-up
form into an oracle for who the admins have invited, and would let whoever
learns an invited address register it first and sit on it, so the real
invitee finds their address taken. The refusal is byte-for-byte the same for
an invited address as for a stranger.

The login page reads the policy (`GET /api/v1/auth/policy`) and hides its
*Create a new account* button, showing *Registration is closed; ask a
workspace admin for an invitation* instead. Registration stays throttled per
client address either way (`OPENV_REGISTER_IP_BURST`, `_REFILL_PER_HOUR`).

**Registering an address never grants the membership; only proof of owning
it does.** The two are deliberately separate. Anyone can type an address into
a sign-up form, and a membership is a credential into somebody's workspace.
An invitation converts in exactly three ways, and in no others:

- the sign-up carries the link's token (`invite_token`) **and** registers the
  invited address — which also marks that address verified, since the token
  was mailed there and nowhere else, so an invitee on a closed,
  verification-required deployment is not walled behind a second mail. The
  answer's `invitation` field says what the token did (`accepted`,
  `already_member`, `email_mismatch`, `invalid`), so a link that granted
  nothing is never a silent nothing;
- a signed-in account whose own address **is** the invited one posts the
  token to `POST /api/v1/auth/invitations/accept`; a session on any other
  address is refused with
  `403 {"code":"invitation_email_mismatch"}` — this is the path for someone
  who already had an account, or who registered without the link. A
  successful accept marks that address verified too, for the same reason the
  sign-up path does: the link reached the mailbox, and it is this account's
  own address;
- an identity provider signs the person in and asserts `email_verified` for
  the address; an unverified or absent claim is refused outright and grants
  nothing.

Confirming an email-verification link grants **nothing**. It used to take up
every invitation waiting for the address, and that was a way in: the
change-of-address flow (`POST /auth/verify-email/change`) lets any account
have a verification mail sent to an address of its choosing, so "verified"
proves the account can read that mailbox's link, not that the account is the
person an admin invited. Someone who signed up without the link opens the
link afterwards, signed in, instead.

A deployment with no SMTP therefore has one path only: pass the invitation
link to the person, since nothing else can prove the address.

Closing registration on an existing deployment changes nothing for accounts
that already exist — nobody is signed out, and every workspace keeps its
members.

### Workspace invitations

Workspace admins invite by email under *Workspace settings → Members*. An
address that already has an account **whose owner has proved it** joins the
workspace immediately; an address that does not — no account at all, or, on a
deployment that requires email verification, an account that has not verified
that address — gets an invitation, which is what makes a closed deployment
usable. The unverified case matters: until somebody has read the mailbox,
nothing ties that account to the address an admin typed, so the membership
waits for the link rather than being granted on a claim. Using the link both
joins the workspace and marks the address verified, so the invitee is not
asked to prove the same thing twice:

- Invitations are valid **7 days** and can be accepted once. The link is
  `${FRONTEND_URL}/login?invite=<token>`, and the token is stored only as a
  SHA-256 hash — the same contract as runner keys, because the link *is* a
  credential into the workspace.
- With SMTP configured the link is emailed, off the request path — the
  response's `emailed` says the send was queued, not that it landed, so no
  admin waits on a slow relay and a failure is logged rather than shown.
  **Without SMTP the invitation still exists**: the API returns the link once
  when it is created and the Members tab shows it for the admin to pass on.
- Following the link signed out opens sign-up with the address prefilled and
  carries the token through whichever way the person continues — creating the
  account, or signing in to one they already had (the token is posted only
  when the address that signed in is the invited one). Following it **signed
  in** shows the invitation with a *Join* button rather than accepting it on
  arrival, and when the session is some other address it says which address
  to sign in as and offers to sign out. Signing up for the invited address
  *without* the link joins nothing at all: the link is the only way in.
- An invitation never changes a role somebody already has. An admin who
  follows a later "member" link stays an admin (the invitation is still
  spent), so the last admin of a workspace cannot be demoted this way.
- Re-inviting an address replaces its previous invitation, whether that one
  was still live or had expired; only the newest link ever works. The
  invitation keeps its id across the replacement, so a list an admin already
  has open still revokes the right row. Re-inviting an **unchanged**
  invitation (same role, still valid) within an hour of its link actually
  being delivered is not re-sent — the person has one in their inbox — but an
  invitation whose send failed, or was never attempted, is minted and sent
  again on the next click.
- Admins can see and revoke pending invitations on the same tab. Revoking
  stops the link working immediately. Expired invitations are swept by the
  same background reaper that sweeps sessions.

### Session lifetime

A session ends two ways (REQ-99): an absolute deadline measured from sign-in,
and an idle deadline measured from its last request. Both are enforced on
every authenticated request and swept from the database in the background.
Both are **caps, not targets** — an operator may shorten them, never lengthen
them past the defaults.

| Variable                | Default (and ceiling) | Meaning                              |
| ----------------------- | --------------------- | ------------------------------------ |
| `OPENV_SESSION_MAX_AGE` | `720h` (30 days)      | Absolute lifetime from sign-in       |
| `OPENV_SESSION_IDLE`    | `168h` (7 days)       | How long a session may go unused     |

Values are Go durations (`720h`, `12h`, `45m` — note `30d` is *not* a Go
duration). A value above the ceiling is clamped, and anything unparseable or
non-positive falls back to the default; each of those decisions logs a line at
boot. The session cookie's own expiry tracks `OPENV_SESSION_MAX_AGE`, so
shortening it also shortens how long a browser keeps the cookie. A live
session records its last activity at most once a minute, so shortening the
idle window does not multiply database writes.

Changing either value applies to sessions that already exist, not just new
ones: shortening the absolute lifetime signs out sessions that are already
older than the new value on their next request.

**Password changes** (`PUT /api/v1/me/password`, Settings → Change password)
delete every other session of the account, keeping only the browser that made
the change. Accounts created through Google or OIDC have no password to change
and get `409 no_password`.

If a required variable is missing, `docker compose ... up`/`config` fails with
an error naming the variable rather than starting with dev defaults.

Notes:

- The overlay terminates plain HTTP. For TLS put a reverse proxy (Caddy,
  Traefik, nginx) in front of the frontend and API ports.
- When the API does sit behind a reverse proxy, also set `OPENV_TRUST_PROXY=1`
  on the api service so per-IP rate limiting — on the public interview
  endpoints and on sign-in, registration and SSO — keys on the real client
  address from `X-Forwarded-For`/`X-Real-IP` (make sure the proxy overwrites
  those headers). Leave it unset when clients reach
  the API directly: the headers are client-supplied, and trusting them would
  let anyone dodge per-IP limits — or exhaust another client's bucket — by
  spoofing a header.
- The frontend container proxies `/api/` to the API (`frontend/nginx.conf`;
  the upstream comes from `API_UPSTREAM`, `api:8080` in compose, and is
  re-resolved through the container's DNS so an API restart with a new
  address is picked up). Browsers therefore use one origin for both, the
  session cookie is first-party, and the frontend's content security policy
  is `'self'` only. Server-sent event streams and file downloads pass through
  unbuffered; the proxy's request-body cap matches `OPENV_MAX_BODY_MB`.
- `REACT_APP_API_URL` (split deployments only) is a build argument: the
  React bundle is static, so the frontend image must be rebuilt when it
  changes. The same value is written into the frontend's content security
  policy at build time (`frontend/security-headers.conf`), so the browser
  will only connect to that API origin.
- The API sets HSTS when `SECURE_COOKIES=true` (or `CROSS_SITE_COOKIES=true`);
  set it only once the API is reachable over TLS alone, because browsers then
  refuse plain HTTP to that host for a year.
- Credential throttling defaults (per client address unless stated) can be
  tuned with `OPENV_AUTH_IP_BURST` / `OPENV_AUTH_IP_REFILL_PER_HOUR` (30,
  120), `OPENV_AUTH_ACCOUNT_BURST` / `_REFILL_PER_HOUR` (5 failed sign-ins,
  20; per account), `OPENV_REGISTER_IP_BURST` / `_REFILL_PER_HOUR` (5, 10),
  `OPENV_SSO_IP_BURST` / `_REFILL_PER_HOUR` (20, 60) and
  `OPENV_INVITE_PREVIEW_BURST` / `_REFILL_PER_HOUR` (60, 240 — invite-link
  previews have their own generous bucket so opening an invitation never
  spends the sign-in budget) and `OPENV_INVITE_BURST` / `_REFILL_PER_HOUR`
  (20, 60 — invitations per **inviting account**: creating one mails an
  address the sender chose, so the endpoint is a relay and is bounded;
  re-posting an unchanged invitation within an hour mails nothing at all).
  Body and upload caps:
  `OPENV_MAX_BODY_MB` (32) and `OPENV_MAX_UPLOAD_MB` (25).
- **Test evidence storage.** Evidence files (the datasets behind physical and
  manual test results) have their own per-file cap, `OPENV_MAX_EVIDENCE_MB`
  (200), because the 25 MB figure cap is right for an image pasted into a
  requirement and useless for an instrument capture. They are written to
  `UPLOADS_DIR` like everything else, so they share the deployment's single
  volume: watch its free space, and cap each workspace with the
  `evidence_storage_mb` org limit (free 2048, team 20480) rather than relying
  on the per-file cap alone. One campaign's captures can otherwise fill the
  volume and take the deployment down with it. Evidence is included in the
  volume backup along with figures.
- Set `OPENV_METRICS_TOKEN` so `/metrics` needs a bearer token; without it
  anyone can read the API's request and runtime statistics.
- If you switch an existing deployment from the dev Postgres password, the
  database was already initialized with the old one — `POSTGRES_PASSWORD` on
  an existing volume does not change it. Run
  `docker exec -it openv-postgres psql -U postgres -c "ALTER USER postgres PASSWORD 'new-password';"`
  once, then start the stack with the new value.

To stop: `docker compose -f docker-compose.yml -f docker-compose.prod.yml down`
(or `make prod-down`). Data lives in named volumes and survives `down`.

## Hosted runners: the docker socket, and how to avoid it

A hosted runner is a container the **API creates on a Docker daemon**, which
means the API needs a daemon to talk to — in the compose deployment, the host's
own, mounted in as `/var/run/docker.sock`.

> **Mounting the host's container socket into the API grants the API control
> of the host.** This is not a hardening detail; it is the whole security
> boundary. Anything that can reach that socket can start a container with the
> host's filesystem bind-mounted and run as root inside it — so a remote-code
> execution bug in the API, or an agent that talks the API into a request it
> should not make, becomes root on the machine. It also crosses every other
> tenant boundary on that daemon: the socket is not scoped to OpenV's own
> containers.

**The transient runner pool is the alternative, and it needs no socket at
all.** Pool nodes are ordinary long-lived `agentd` replicas started with a
shared `RUNNER_POOL_KEY`; they register themselves and wait to be leased, so
nothing has to create a container on demand and the API never talks to a
daemon. It is also what works on platforms that give you replicas but no socket
(Railway among them). Prefer it, and leave `HOSTED_RUNNERS=off` unless a
workspace genuinely needs an always-on API-key runner. See `docs/agents.md`,
*Enable transient runners (operators)*.

If you do enable hosted runners, keep the socket off the public path: run the
API on a host that carries nothing else, and treat access to that host as
equivalent to root.

### What a hosted runner container is created with

Every hosted runner container is created with the isolation below
(`internal/hosting/docker.go`, `hostConfigFor`) — it runs a vendor CLI over
prompts that may carry text nobody in the workspace wrote, so it is treated as
a place where something will eventually go wrong:

| Setting | Value | Why |
| --- | --- | --- |
| `CapDrop` | `ALL` | A runner is one node process as an unprivileged user; it needs no Linux capability. |
| `SecurityOpt` | `no-new-privileges:true` | Nothing inside can climb through a setuid binary. |
| `PidsLimit` | 1024 (`HOSTED_RUNNER_PIDS_LIMIT`) | A fork bomb hits a wall instead of the host. The cgroup counts **threads**, not processes, so the cap sits well above any process count you would guess from `ps`: a node CLI's thread pool, a toolchain build and a test run share the allowance. |
| `Memory` / `NanoCPUs` | the workspace's plan caps | `runner_memory_mb`, `runner_cpus`; see `docs/agents.md`. |
| `Binds` | the org's data volume only | Never the docker socket. |
| `ReadonlyRootfs` | **not set** | The vendor CLIs in the runner image write outside `/data` (npm and CLI caches, git temporaries), so a read-only root filesystem breaks runs today. Getting there means a tmpfs for each of those paths. |

`HOSTED_RUNNER_PIDS_LIMIT` on the API service overrides the cap; `0` means no
cap (docker's own convention), and an unparseable value falls back to the
default of 1024 rather than to unlimited. Raise it if runs in a workspace start
failing to spawn — the symptom is a `fork`/`EAGAIN` failure deep inside a
vendor CLI, not a message about the limit.

### The runner network

Set **`RUNNER_NETWORK`** on the API service to a docker network that reaches
**the API and the provider endpoints, and nothing else**. Without it a runner
lands on docker's default bridge, where it can reach every other container on
it — the database included — and the API logs a line saying so at each
provision.

The compose example (`docker-compose.prod.yml`) declares an `openv-runners`
network that only the API also joins:

```yaml
networks:
  openv-runners:
    name: openv-runners

services:
  api:
    networks: [default, openv-runners]   # reachable by runners
    environment:
      RUNNER_NETWORK: openv-runners
```

Postgres stays on `default` only, so a runner container has no route to it.
Note what this does and does not buy you: it removes the runner's access to
your other services, but a bridge network still has **egress to the internet**,
which is what lets the CLIs reach `api.anthropic.com` and friends. Restricting
that egress to the provider endpoints themselves is a host firewall or a proxy
job (allow the vendor API hostnames, deny the rest); docker networking alone
cannot express it.

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
