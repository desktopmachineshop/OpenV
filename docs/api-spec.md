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

A worker principal passes project checks only for projects belonging to its
own org.

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
| GET | `/api/v1/orgs` | List the caller's orgs | user |
| POST | `/api/v1/orgs` | Create a company workspace | user |
| GET | `/api/v1/orgs/{id}` | Workspace details | org member |
| PUT | `/api/v1/orgs/{id}` | Update name/settings/limits/`monthly_budget_usd` | org admin |
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

### Workers, runner keys, connector, hosted runner

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

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/artifacts` | Create artifact | editor |
| GET | `/api/v1/artifacts` | List artifacts (`?project_id=&type=`) | viewer |
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
| GET | `/api/v1/attachments/{id}/download` | Download the file | viewer |
| DELETE | `/api/v1/attachments/{id}` | Delete attachment | editor |
| GET | `/api/v1/artifacts/{artifactID}/attachments` | List an artifact's attachments | viewer |
| POST | `/api/v1/chatter` | Comment on an artifact | editor |
| GET | `/api/v1/chatter` | List an artifact's activity feed | viewer |

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

### Guided sessions (requirements wizard + copilot chat)

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/guided-sessions` | Start a wizard session | editor |
| GET | `/api/v1/guided-sessions` | List sessions (`?project_id=`) | viewer |
| GET | `/api/v1/guided-sessions/{id}` | Session state | viewer |
| PUT | `/api/v1/guided-sessions/{id}/step` | Save a step's answers | editor |
| POST | `/api/v1/guided-sessions/{id}/drafts` | Materialize draft artifacts | editor |
| POST | `/api/v1/guided-sessions/{id}/commit` | Commit session (drafts become real) | editor |
| POST | `/api/v1/guided-sessions/{id}/abandon` | Abandon session | editor |
| GET | `/api/v1/guided-sessions/{id}/messages` | Copilot chat history | viewer |
| POST | `/api/v1/guided-sessions/{id}/messages` | Send a chat message (launches a copilot turn) | editor |
| POST | `/api/v1/guided-sessions/{id}/chat/kickoff` | First copilot greeting | editor |
| POST | `/api/v1/guided-sessions/{id}/chat/nudge` | Context nudge after step change | editor |
| GET | `/api/v1/guided-sessions/{id}/chat/stream` | SSE stream of copilot replies | viewer |

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

## Error responses

Errors are plain-text (`http.Error`) or `{"error": "..."}` JSON depending on
handler, with conventional status codes: `400` validation, `401` missing/bad
credentials, `403` insufficient role, `404` not found, `202` proposal-mode
write diverted for review, `500` server error.
