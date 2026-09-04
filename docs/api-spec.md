# OpenV API Specification (v1)

## Base URL

```
http://localhost:8080/api/v1
```

## Content-Type

All requests and responses use `application/json` unless noted (attachment
upload/download, connector bundle download, SSE streams).

## Authentication

Every request is authenticated by the middleware in
`internal/api/authmiddleware.go` as one of four principals. Only these paths are
open (no credentials): `/health`, `/metrics` (carries its own optional
`OPENV_METRICS_TOKEN` bearer gate), `/api/v1/auth/*`, and `/api/v1/public/*`.

### 1. Human users — session cookie

`POST /api/v1/auth/register` or `POST /api/v1/auth/login` sets the
`openv_session` cookie (HttpOnly, SameSite=Lax; `Secure` when
`SECURE_COOKIES=true`). The first user ever registered becomes the platform
admin. Optional **Google OIDC** sign-in is enabled by setting
`GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` (redirect URI:
`${PUBLIC_URL}/api/v1/auth/google/callback`); `GET /api/v1/auth/config` tells
the login page whether the button should appear.

**Active workspace (`X-Org-ID`)** — every session request runs in the context
of one organization ("workspace"). The `X-Org-ID` header selects it; the
middleware validates membership and falls back to the session's stored active
org (`POST /orgs/{id}/activate`), then to the user's personal org (created
automatically at signup). An invalid header degrades to the fallback instead
of failing the request.

### 2. Agent runs — Bearer run token

Each queued agent run is issued a single-run token (stored hashed). The
worker passes it to the agent process, which calls back with
`Authorization: Bearer <run-token>`. A run authenticates as an
editor-equivalent principal **inside its own project only**; runs with
`write_mode: proposal` have their writes diverted into the proposal queue
(HTTP 202 with a proposal receipt) instead of being applied.

### 3. Workers — Bearer org-scoped worker key

Runners (`agentd`) authenticate with `Authorization: Bearer <worker-key>`.
Keys are org-scoped rows in `worker_keys` (stored hashed):

- **Workspace keys** — minted by org admins (Settings → Worker Keys).
- **Personal runner keys** — one per member (`worker_keys.user_id` set);
  minted via `/orgs/{id}/my-runner-key` or the Agent Connector pairing flow.
  A personal key only claims runs its owner launched.
- **Session keys** — a personal key bound to a transient runner lease
  (`worker_keys.session_id` set), minted when a member leases a pool node and
  revoked when the lease ends. It routes like any personal key; it is kept
  distinct so leasing a cloud runner never rotates the member's own connector
  key.

A worker principal passes project checks only for projects belonging to its
own org.

### 3a. Runner pool nodes — Bearer `RUNNER_POOL_KEY`

Transient runner pool nodes present the deployment-wide `RUNNER_POOL_KEY`.
This principal has **no workspace and no user**: it may call the
`/api/v1/runner-pool/*` endpoints and nothing else. A node gains a workspace
identity only by being leased, and then authenticates as the lease's session
key like any other runner.

### 4. Legacy `WORKER_API_KEY`

A raw `WORKER_API_KEY` in the API server environment keeps old deployments
working: at startup it is registered as a workspace key for the bootstrap
org, and the middleware also accepts the raw env value directly (resolving it
to the bootstrap org).

## Authorization model

Enforced per-handler via `internal/api/authz.go`:

- **Platform admin** (`users.is_admin`, the first registered user) passes
  every check.
- **Org roles**: `admin` and `member` (`org_members.role`). Org admins of a
  project's org act as project owners.
- **Project roles**: `owner` > `editor` > `viewer`. A member's effective role
  is the highest of their direct grant (`project_members`) and any
  people-team grant (`project_team_access` via `org_teams`).
- **Agent runs** count as editor within their own project; **workers** pass
  for any project in their org; **run access** (viewing logs/streams) is
  granted to the launcher, then by the project ladder, then org admin for
  unscoped runs.
- **Crew writes**: project-pinned crews need project editor; workspace-wide
  crews need org admin. Automations follow the same split.

## Route inventory

Generated from the router registrations in `internal/api/*.go`
(`RegisterRoutes` and the `register*Routes` helpers) as of commit `2d6cd72`.
Auth column: `open` (no credentials) · `user` (any session) ·
`viewer`/`editor`/`owner` (project role ladder; agent runs count as editor in
their own project, workers pass within their org) · `org member`/`org admin`
(workspace role) · `worker` (worker key) · `run` (run token) ·
`token` (public one-time/invite token).

### Health

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/health` | Liveness check (`{"status":"ok"}`) | open |

### Auth & users

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/auth/register` | Create password account (first user becomes admin) and log in | open |
| POST | `/api/v1/auth/login` | Password login, sets session cookie | open |
| POST | `/api/v1/auth/logout` | End session, clear cookie | open |
| GET | `/api/v1/auth/me` | Current user profile | user |
| GET | `/api/v1/auth/config` | Which sign-in methods are enabled (Google) | open |
| GET | `/api/v1/auth/google` | Start Google OIDC flow | open |
| GET | `/api/v1/auth/google/callback` | OIDC callback, creates/logs in user | open |
| GET | `/api/v1/users` | List users (for member pickers) | user |
| GET | `/api/v1/projects/{id}/members` | List project members | viewer |
| POST | `/api/v1/projects/{id}/members` | Add member with role | owner |
| PUT | `/api/v1/projects/{id}/members/{userId}` | Change member role | owner |
| DELETE | `/api/v1/projects/{id}/members/{userId}` | Remove member | owner |

### Organizations (workspaces)

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/orgs` | List the caller's orgs (`?deleted=true` lists their soft-deleted ones) | user |
| POST | `/api/v1/orgs` | Create a company workspace | user |
| GET | `/api/v1/orgs/{id}` | Workspace details | org member |
| PUT | `/api/v1/orgs/{id}` | Update name/settings/limits/`monthly_budget_usd` | org admin |
| DELETE | `/api/v1/orgs/{id}` | Soft-delete a company workspace: hidden and locked immediately, restorable for 30 days, then hard-deleted with all its data by a daily purge. Personal workspaces are refused. | org admin |
| POST | `/api/v1/orgs/{id}/restore` | Restore a soft-deleted workspace within the grace period | org admin (of the deleted org) |
| POST | `/api/v1/orgs/{id}/activate` | Set the session's active workspace | org member |
| GET | `/api/v1/orgs/{id}/members` | List workspace members | org member |
| POST | `/api/v1/orgs/{id}/members` | Add member | org admin |
| PUT | `/api/v1/orgs/{id}/members/{userId}` | Change org role | org admin |
| DELETE | `/api/v1/orgs/{id}/members/{userId}` | Remove member (self-removal = leave, allowed for members) | org admin / self |
| GET | `/api/v1/orgs/{id}/teams` | List people-teams | org member |
| POST | `/api/v1/orgs/{id}/teams` | Create people-team | org admin |
| PUT | `/api/v1/org-teams/{id}` | Rename/edit team | org admin |
| DELETE | `/api/v1/org-teams/{id}` | Delete team | org admin |
| POST | `/api/v1/org-teams/{id}/members/{userId}` | Add user to team | org admin |
| DELETE | `/api/v1/org-teams/{id}/members/{userId}` | Remove user from team | org admin |
| GET | `/api/v1/projects/{id}/team-access` | List team grants on a project | viewer |
| PUT | `/api/v1/projects/{id}/team-access` | Grant/update a team's project role | owner |
| DELETE | `/api/v1/projects/{id}/team-access/{teamId}` | Revoke a team grant | owner |
| GET | `/api/v1/orgs/{id}/quality-rules` | Workspace requirement quality rules (house style every project inherits) | org member |
| PUT | `/api/v1/orgs/{id}/quality-rules` | Set the house style; an empty body clears it back to the platform defaults | org admin |

### Workers, runner keys, connector, hosted and transient runners

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/orgs/{id}/worker-keys` | List workspace worker keys | org admin |
| POST | `/api/v1/orgs/{id}/worker-keys` | Mint a workspace key (value returned once) | org admin |
| DELETE | `/api/v1/orgs/{id}/worker-keys/{keyId}` | Revoke a key | org admin |
| GET | `/api/v1/orgs/{id}/my-runner-key` | My personal runner key status | org member |
| POST | `/api/v1/orgs/{id}/my-runner-key` | Mint/rotate my personal runner key | org member |
| DELETE | `/api/v1/orgs/{id}/my-runner-key` | Revoke my personal runner key | org member |
| POST | `/api/v1/orgs/{id}/connector-pairing` | Mint a one-time connector pairing code (10-min TTL) | org member |
| POST | `/api/v1/public/connector/pair` | Exchange pairing code for a personal runner key | token |
| GET/HEAD | `/api/v1/public/connector/download` | Download the Agent Connector bundle (`?os=windows\|linux`) | open |
| GET | `/api/v1/orgs/{id}/hosted-runner` | Hosted runner status | org admin |
| POST | `/api/v1/orgs/{id}/hosted-runner` | Provision the workspace's hosted runner container | org admin |
| POST | `/api/v1/orgs/{id}/hosted-runner/start` | Start the container | org admin |
| POST | `/api/v1/orgs/{id}/hosted-runner/stop` | Stop the container | org admin |
| DELETE | `/api/v1/orgs/{id}/hosted-runner` | Delete (optionally `?purge=true` removes the volume) | org admin |
| GET | `/api/v1/orgs/{id}/worker-status` | Live runner presence / queue depth | org member |
| GET | `/api/v1/orgs/{id}/runner-session` | My transient runner lease (with deadline and pool occupancy) | org member |
| POST | `/api/v1/orgs/{id}/runner-session` | Lease a cloud runner (409-free: an existing lease is returned; 503 when the pool is full) | org member |
| POST | `/api/v1/orgs/{id}/runner-session/extend` | Reset my lease's clocks (capped at 8h from its start) | org member |
| DELETE | `/api/v1/orgs/{id}/runner-session` | End my lease now (the node is wiped) | org member |
| GET | `/api/v1/orgs/{id}/runner-pool` | Pool occupancy and the workspace's live leases | org admin |
| POST | `/api/v1/runner-pool/nodes` | Register a pool node | pool key |
| POST | `/api/v1/runner-pool/nodes/{id}/heartbeat` | Heartbeat; returns the node's lease (credential once) | pool key |
| POST | `/api/v1/runner-pool/nodes/{id}/release` | Report a lease wiped; return to the idle pool | pool key |

### Projects, baselines, templates

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/projects` | Create project in active workspace (creator becomes owner) | user |
| GET | `/api/v1/projects` | List projects the caller can access | user |
| GET | `/api/v1/projects/{id}` | Project details | viewer |
| PUT | `/api/v1/projects/{id}` | Update project | editor |
| DELETE | `/api/v1/projects/{id}` | Delete project | owner |
| GET | `/api/v1/projects/{id}/export` | Export project JSON | viewer |
| POST | `/api/v1/projects/import` | Import a project export (JSON or ReqIF) | user |
| GET | `/api/v1/projects/{id}/report` | Generate PDF report | viewer |
| POST | `/api/v1/projects/{id}/baselines` | Snapshot a baseline | editor |
| GET | `/api/v1/projects/{id}/baselines` | List baselines | viewer |
| GET | `/api/v1/baselines/{id}` | Baseline contents | viewer |
| DELETE | `/api/v1/baselines/{id}` | Delete baseline | owner |
| GET | `/api/v1/templates` | List templates (global + workspace) | user |
| POST | `/api/v1/templates` | Save a project as a template | editor |
| POST | `/api/v1/templates/{id}/projects` | Create project from template | user |

**Export/import caveats** (`internal/domain/exports/export.go`):

- The `?format=` on export accepts `json` (default), `csv`, and `reqif` (OMG
  ReqIF 1.x, read by DOORS/Polarion). **`excel` is a stub** — the service
  returns `ErrUnsupportedFormat` ("excel export not yet implemented"), so the
  API rejects it.
- **ReqIF import** (`internal/domain/exports/reqif_import.go`): `POST
  /api/v1/projects/import` accepts a ReqIF document as well as JSON. ReqIF is
  selected by `?format=reqif`, an XML/ReqIF `Content-Type`, or sniffed from a
  `<REQ-IF` root; anything else is treated as JSON. A malformed ReqIF (or an
  enum attribute whose value is not among its datatype's declared values) is a
  **400**. Imported artifacts are remapped to fresh ids at version 1, with
  parent hierarchy, links, status, and attributes reconstructed. Bodies are
  carried as XHTML-typed values so hard line breaks survive the round trip.
- Exports include **attachment metadata only**, not the file bytes.
  Consequently attachments are **dropped on import** (JSON and ReqIF alike) — a
  re-imported project has its artifacts, links, and product profile, but no
  attachment files.

### Artifacts, links, attachments, chatter

Every artifact carries two identifiers, and they answer different questions:

- **`ref`** (`REQ-12`) is the stable address. It is minted server-side on
  create from a per-project, per-prefix counter, is unique among a project's
  live artifacts, stays constant across versions, and is **never reissued** —
  deleting `REQ-12` does not free the number, because the counter only ever
  moves forward. This is what a review comment, a test result, or an exported
  report cites.
- **`doc_number`** (`1.2`) is the section number of a heading, derived from
  its position in the tree. Reordering the document changes it, which is why
  it is never a citation. It is computed, never stored, and served only when
  a list request passes `doc_numbers=1` — that flag makes the handler read the
  whole project, since a page or a type filter is only a slice of the tree and
  cannot be numbered on its own. Only headings get one.


| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/artifacts` | Create artifact | editor |
| GET | `/api/v1/artifacts` | List artifacts (`?project_id=&type=&doc_numbers=1`) | viewer |
| GET | `/api/v1/artifacts/{id}` | Get artifact (current version) | viewer |
| PUT | `/api/v1/artifacts/{id}` | Update (creates a new temporal version) | editor |
| DELETE | `/api/v1/artifacts/{id}` | Soft-delete (history retained) | editor |
| GET | `/api/v1/artifacts/{id}/versions` | Version history | viewer |
| POST | `/api/v1/artifacts/{id}/restore` | Restore an older version | editor |
| GET | `/api/v1/artifacts/{id}/links` | Links per artifact version | viewer |
| POST | `/api/v1/links` | Create traceability link | editor |
| GET | `/api/v1/links` | List links (`?project_id=`) | viewer |
| GET | `/api/v1/links/{id}` | Get link | viewer |
| PUT | `/api/v1/links/{id}` | Update link | editor |
| DELETE | `/api/v1/links/{id}` | Delete link | editor |
| POST | `/api/v1/attachments/upload` | Upload a file (multipart) to an artifact or test result | editor |
| GET | `/api/v1/attachments/{id}` | Attachment metadata | viewer |
| GET | `/api/v1/attachments/{id}/download` | Download the file (`?version=N` for a superseded one) | viewer |
| POST | `/api/v1/attachments/{id}/versions` | Replace a figure's image with a new version (multipart) | editor |
| GET | `/api/v1/attachments/{id}/versions` | A figure's version history, newest first | viewer |
| DELETE | `/api/v1/attachments/{id}` | Delete attachment | editor |
| GET | `/api/v1/artifacts/{artifactID}/attachments` | List an artifact's attachments | viewer |
| POST | `/api/v1/chatter` | Comment on an artifact | editor |
| GET | `/api/v1/chatter` | List an artifact's activity feed | viewer |

### Figures

An image attached to an artifact is a **figure**, and carries a reference of
its own — `REQ-17-FIG-1` — built from the artifact's stable reference and a
per-artifact counter.

- The number is minted once and **never reissued**: the counter only moves
  forward, so deleting a figure does not free its number, and two concurrent
  uploads cannot be handed the same one. A partial unique index on
  `figure_ref` backstops the counter.
- The stored name is the figure's (`REQ-17-FIG-1.png`), and a download is
  served under it, so saving an image lands a file named for what the document
  calls it. The name the uploader's file had is kept as `original_filename`,
  and the on-disk path stays UUID-unique: the uploads directory is flat across
  projects, figure references are unique only within one, and each version
  needs a file of its own.
- An artifact with no stable reference yields no figure reference rather than a
  bare `FIG-1` that would collide once the artifact got one.

Uploading a **new version** keeps the figure's reference and supersedes its
file. Because the artifact now shows something different, that upload also
takes the artifact to a new version and writes a note to its feed
("Figure REQ-17-FIG-1 updated from version 1 to 2 — …"). Superseded versions
stay retrievable through `?version=N`. The artifact's new version is an
attribute-free update: it does not demote an approved artifact or mark its
links suspect, because nothing the artifact *says* changed.

### Review queue

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/review-queue` | Suspect links + `in_review` artifacts awaiting review | viewer |

### Notifications

Per-user, in-app (plus optional email — see `docs/operations.md`). SSE stream
pushes new items live.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/notifications` | List the caller's notifications (`?unread=true&limit=`) | user |
| POST | `/api/v1/notifications/read` | Mark specific notifications read | user |
| POST | `/api/v1/notifications/read-all` | Mark all read | user |
| GET | `/api/v1/notifications/stream` | SSE stream of new notifications | user |
| GET | `/api/v1/me/notification-prefs` | Get email-notification opt-out | user |
| PUT | `/api/v1/me/notification-prefs` | Update email-notification opt-out | user |

### Meta

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/meta/artifact-types` | Artifact type catalog | user |
| GET | `/api/v1/meta/link-types` | Link type catalog (see `docs/link-type-rules.md`) | user |

### Shared demo products (community pool)

The joke products the new-project wizard rolls for testing. This is the only
route group in OpenV that is deliberately cross-tenant: every workspace reads
and writes the same pool, so it grows as people share what their agents
invent. Consequently the gates are tighter than the role ladder alone:

- **Reading** needs a session, so the pool is never anonymous or crawlable.
- **Publishing and reporting** need a signed-in *person* — an agent run token
  or a worker key is refused. A product a member's agent invents is published
  automatically by their browser, under their session, so the pool grows on
  its own while every row stays attributable to an account and counts against
  that workspace's daily cap. Nothing reviews an entry before other tenants
  see it: reporting and admin deletion are what cover that.
- Entries are sanitized server-side to inert single-line text (no line
  breaks, backticks, angle brackets, links, or the `openv-suggestion`
  marker), capped per field, deduplicated by normalized name, rate-limited
  per workspace per day (`OPENV_SHARED_PRODUCT_DAILY_LIMIT`, default 20) and
  capped in total (`OPENV_SHARED_PRODUCT_POOL_LIMIT`, default 5000).
- Responses carry no author identity. `created_by_org` / `created_by_user`
  are stored for rate limiting and takedown only.
- Reports are per person: `ReportsToHide` (3) *distinct* reporters hide an
  entry pending review; one account clicking repeatedly changes nothing.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/shared-products` | List the visible community pool | user |
| POST | `/api/v1/shared-products` | Share a product with every workspace | org member (session only) |
| POST | `/api/v1/shared-products/{id}/report` | Flag an entry for review | user (session only) |
| DELETE | `/api/v1/shared-products/{id}` | Remove an entry outright | platform admin |

### Product profile, V&V, test runs

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/profile` | Product profile (vision, users, constraints) | viewer |
| PUT | `/api/v1/projects/{id}/profile` | Update product profile | editor |
| POST | `/api/v1/projects/{id}/test-runs` | Create test run | editor |
| GET | `/api/v1/projects/{id}/test-runs` | List test runs | viewer |
| GET | `/api/v1/test-runs/{id}` | Test run details | viewer |
| PUT | `/api/v1/test-runs/{id}` | Update test run | editor |
| DELETE | `/api/v1/test-runs/{id}` | Delete test run | editor |
| POST | `/api/v1/test-runs/{id}/results` | Record/overwrite a test result | editor |
| GET | `/api/v1/test-runs/{id}/results` | List results | viewer |
| GET | `/api/v1/projects/{id}/vv/coverage` | Verification coverage summary | viewer |
| GET | `/api/v1/projects/{id}/vv/matrix` | Traceability matrix | viewer |
| GET | `/api/v1/projects/{id}/vv/gaps` | Coverage gaps | viewer |
| GET | `/api/v1/projects/{id}/vv/report` | V&V report | viewer |

### Requirement quality

Advisory linting of requirement wording. `quality-rules` names the project's
normative convention (`shall` — ISO/IEC/IEEE 29148 — or `rfc2119`) and the
severity of each rule; a project inherits its workspace's rules until it sets
its own, and an empty PUT body clears the override. Reports carry the rule set
they were produced under, since the same sentence scores differently between
conventions.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/quality` | Lint every requirement/user need (`?baseline_id=` lints a snapshot) | viewer |
| GET | `/api/v1/artifacts/{id}/quality` | Lint one artifact (400 for a type the linter does not judge) | viewer |
| GET | `/api/v1/projects/{id}/quality-rules` | Resolved rules + both levels' overrides + the editor catalog | viewer |
| PUT | `/api/v1/projects/{id}/quality-rules` | Set or clear the project's override | editor |

### Work items (kanban)

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/projects/{id}/work-items` | Create card | editor |
| GET | `/api/v1/projects/{id}/work-items` | List board | viewer |
| GET | `/api/v1/work-items/{id}` | Card + activity | viewer |
| PUT | `/api/v1/work-items/{id}` | Edit card | editor |
| DELETE | `/api/v1/work-items/{id}` | Delete card | editor |
| POST | `/api/v1/work-items/{id}/move` | Move card (agent columns can enqueue runs) | editor |
| POST | `/api/v1/work-items/{id}/comments` | Comment on card | viewer |

### Guided sessions (requirements wizard + V&V Assistant chat)

The assistant's transcript is stored on a guided session, and one conversation
serves the whole project: the wizard writes into it, and so does the notes
panel beside any artifact. The message and kickoff endpoints therefore accept
an optional `artifact_id` — the artifact the reader has open — which is added
to that turn's prompt as fenced, untrusted content. The wizard sends none.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/guided-sessions` | Start a wizard session | editor |
| GET | `/api/v1/guided-sessions` | List sessions (`?project_id=`) | viewer |
| GET | `/api/v1/guided-sessions/{id}` | Session state | viewer |
| PUT | `/api/v1/guided-sessions/{id}/step` | Save a step's answers | editor |
| POST | `/api/v1/guided-sessions/{id}/drafts` | Materialize draft artifacts | editor |
| POST | `/api/v1/guided-sessions/{id}/commit` | Commit session (drafts become real) | editor |
| POST | `/api/v1/guided-sessions/{id}/abandon` | Abandon session | editor |
| GET | `/api/v1/guided-sessions/{id}/messages` | Assistant chat history | viewer |
| POST | `/api/v1/guided-sessions/{id}/messages` | Send a chat message (launches an assistant turn; optional `artifact_id`) | editor |
| POST | `/api/v1/guided-sessions/{id}/chat/kickoff` | First assistant greeting (optional `artifact_id`) | editor |
| POST | `/api/v1/guided-sessions/{id}/chat/nudge` | Context nudge after step change | editor |
| GET | `/api/v1/guided-sessions/{id}/chat/stream` | SSE stream of assistant replies | viewer |

### Interviews

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/projects/{id}/interviews` | Create interview | editor |
| GET | `/api/v1/projects/{id}/interviews` | List interviews | viewer |
| POST | `/api/v1/interviews/{id}/close` | Close interview | editor |
| PUT | `/api/v1/interviews/{id}/persona` | Link/unlink a persona artifact | editor |
| POST | `/api/v1/interviews/{id}/invites` | Mint an invite link | editor |
| GET | `/api/v1/interviews/{id}/invites` | List invites | viewer |
| POST | `/api/v1/interview-invites/{id}/revoke` | Revoke invite | editor |
| GET | `/api/v1/interviews/{id}/sessions` | List participant sessions | viewer |
| GET | `/api/v1/interview-sessions/{id}/transcript` | Transcript | viewer |
| GET | `/api/v1/public/interviews/{token}` | Public interview intro | token |
| POST | `/api/v1/public/interviews/{token}/messages` | Participant sends a message | token |
| GET | `/api/v1/public/interviews/{token}/stream` | SSE stream of interviewer replies | token |
| POST | `/api/v1/public/interviews/{token}/finish` | Participant ends the session | token |

### Agents (definitions)

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/agents` | List workspace agents | user |
| POST | `/api/v1/agents` | Create agent (writes the markdown file) | org admin |
| POST | `/api/v1/agents/sync` | Re-sync agent files → registry | org admin |
| GET | `/api/v1/agents/{slug}` | Agent details | user |
| PUT | `/api/v1/agents/{slug}` | Update agent | org admin |
| DELETE | `/api/v1/agents/{slug}` | Delete agent | org admin |
| GET | `/api/v1/agents/{slug}/raw` | Raw markdown (frontmatter + prompt) | user |
| PUT | `/api/v1/agents/{slug}/raw` | Save raw markdown | org admin |
| POST | `/api/v1/agents/{slug}/runs` | Launch a run of this agent | editor (project-scoped) / user |

### Agent runs

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/agent-runs` | List runs (project-scoped: viewer; workspace-wide: org admin, members see their own) | user |
| POST | `/api/v1/agent-runs/claim` | Worker claims the next eligible queued run | worker |
| POST | `/api/v1/agent-runs/delegate` | Running crew agent delegates to a child agent | run |
| GET | `/api/v1/agent-runs/delegate/{id}` | Delegation status | run |
| GET | `/api/v1/agent-runs/{id}` | Run details | launcher / viewer |
| GET | `/api/v1/agent-runs/{id}/tree` | Run + child-run tree | launcher / viewer |
| GET | `/api/v1/agent-runs/{id}/logs` | Run log entries | launcher / viewer |
| POST | `/api/v1/agent-runs/{id}/logs` | Worker appends log entries (returns cancel flag) | worker |
| GET | `/api/v1/agent-runs/{id}/stream` | SSE live log stream | launcher / viewer |
| POST | `/api/v1/agent-runs/{id}/cancel` | Request cancellation | launcher / editor |
| POST | `/api/v1/agent-runs/{id}/start` | Worker marks run running | worker |
| POST | `/api/v1/agent-runs/{id}/finish` | Worker reports completion | worker |

### Crews (agent org charts) — canonical

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/crews` | List crews | user |
| POST | `/api/v1/crews` | Create crew | editor (project-pinned) / org admin |
| GET | `/api/v1/crews/{id}` | Crew graph (nodes + edges) | org member |
| PUT | `/api/v1/crews/{id}` | Update crew | editor / org admin |
| DELETE | `/api/v1/crews/{id}` | Delete crew | editor / org admin |
| POST | `/api/v1/crews/{id}/clone` | Clone crew | editor / org admin |
| POST | `/api/v1/crews/{id}/nodes` | Add node (agent or human) | editor / org admin |
| POST | `/api/v1/crews/{id}/runs` | Launch a run at the crew's entry node | editor / org admin |
| PUT | `/api/v1/crew-nodes/{id}` | Update node | editor / org admin |
| DELETE | `/api/v1/crew-nodes/{id}` | Remove node | editor / org admin |
| POST | `/api/v1/crews/{id}/edges` | Add edge (delegates-to, hands-off-to, reviews) | editor / org admin |
| PUT | `/api/v1/crew-edges/{id}` | Update edge | editor / org admin |
| DELETE | `/api/v1/crew-edges/{id}` | Remove edge | editor / org admin |

**Deprecated aliases** (same handlers, kept for compatibility — see
`internal/api/agent_handlers.go`): `/api/v1/teams`, `/api/v1/teams/{id}`,
`/api/v1/teams/{id}/clone|nodes|runs|edges`, `/api/v1/team-nodes/{id}`,
`/api/v1/team-edges/{id}`. New integrations should use the `/crews` forms.

### Automations & proposals

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/automations` | List automations | user |
| POST | `/api/v1/automations` | Create (manual / cron / event-triggered) | editor (project-pinned) / org admin |
| GET | `/api/v1/automations/{id}` | Details | org member |
| PUT | `/api/v1/automations/{id}` | Update | editor / org admin |
| DELETE | `/api/v1/automations/{id}` | Delete | editor / org admin |
| POST | `/api/v1/automations/{id}/run-now` | Launch immediately | editor / org admin |
| GET | `/api/v1/proposals` | List pending agent proposals | viewer (project) / org admin |
| POST | `/api/v1/proposals/{id}/approve` | Apply a proposed write | editor |
| POST | `/api/v1/proposals/{id}/reject` | Reject it | editor |

### Repo connections & provider settings

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/repo-connections` | List connected repositories | viewer |
| POST | `/api/v1/projects/{id}/repo-connections` | Connect a repository | owner |
| PUT | `/api/v1/repo-connections/{id}` | Update connection | owner |
| DELETE | `/api/v1/repo-connections/{id}` | Remove connection | owner |
| PUT | `/api/v1/repo-connections/{id}/my-path` | Set my machine's checkout path | viewer |
| GET | `/api/v1/provider-settings` | Provider status + model catalog per provider | user |
| PUT | `/api/v1/provider-settings` | Update provider config (auth mode, default model) | org admin |
| POST | `/api/v1/provider-settings/detect` | Worker reports detected CLIs/logins/models | worker |
| POST | `/api/v1/provider-logins` | Start a CLI sign-in relay (workspace: org admin; user-targeted: org member) | user |
| POST | `/api/v1/provider-logins/claim` | Worker claims a pending sign-in | worker |
| GET | `/api/v1/provider-logins/{id}` | Sign-in status (redacted) | user |
| POST | `/api/v1/provider-logins/{id}/code` | Submit the pasted authorization code | user |
| POST | `/api/v1/provider-logins/{id}/cancel` | Cancel sign-in | user |
| POST | `/api/v1/provider-logins/{id}/progress` | Worker reports flow progress | worker |
| GET | `/api/v1/provider-logins/{id}/full` | Full login record (incl. code) for the executing worker | worker |

### Events

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/events` | Domain event feed (`?project_id=` viewer; workspace-wide: org admin) | user |

Each event carries its stored audit fields (`actor` — `user:<id>`,
`agent:<run id>`, `worker:<org>[:user:<id>]` or `system` — plus `entity_id`)
and, alongside them, the names those IDs stand for, resolved server-side for
the events the caller may see: `actor_kind`, `actor_id`, `actor_name`, and
`entity_kind` / `entity_name` (the artifact title, work item title, baseline
name, agent name, ...). A name is omitted when the row behind the ID is gone,
so clients must fall back to the raw IDs.

## Error responses

Errors are plain-text (`http.Error`) or `{"error": "..."}` JSON depending on
handler, with conventional status codes: `400` validation, `401` missing/bad
credentials, `403` insufficient role, `404` not found, `202` proposal-mode
write diverted for review, `500` server error.
