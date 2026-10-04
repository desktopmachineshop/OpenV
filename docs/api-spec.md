# OpenV API Specification (v1)

## Base URL

```
http://localhost:8080/api/v1
```

## Content-Type

All requests and responses use `application/json` unless noted (attachment
upload/download, connector bundle download, SSE streams).

## Times

Times are RFC 3339. A time sent with an offset as an evidence bundle's
`captured_at`, a work item's `due_date`, an interview invite's `expires_at`
or a share link's `expires_at` is stored as the instant it names and read
back in UTC: `2026-01-15T09:30:00+01:00` reads back `2026-01-15T08:30:00Z`,
though the create's own answer echoes it as sent, and a share link closes
at that instant. A workspace invitation's `expires_at`, which the server
sets, is stored the same way.

## Authentication

Every request is authenticated by the middleware in
`internal/api/authmiddleware.go` as one of four principals. Only these paths are
open (no credentials): `/health`, `/metrics` (carries its own optional
`OPENV_METRICS_TOKEN` bearer gate), `/api/v1/auth/*`, and `/api/v1/public/*`.

### 1. Human users — session cookie

`POST /api/v1/auth/register` or `POST /api/v1/auth/login` sets the
`openv_session` cookie (HttpOnly, SameSite=Lax; `Secure` when
`SECURE_COOKIES` is `true`, in any case, or `1`). The first user ever
registered becomes the platform admin. Optional **Google OIDC** sign-in is
enabled by setting `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` (redirect URI:
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
editor-equivalent principal **inside its own project only**: another
project's routes answer it as a project no row has (`404` `project not
found`, or the looked-up resource's own not-found), its own project's owner
routes `403` `agent runs act at most as a project editor`, and it creates no
project (`POST /projects`, `/projects/import` and `/templates/{id}/projects`
answer it `403` `agent runs cannot create projects`, REQ-42); `GET
/projects` lists it its own project alone (`[]` for a run with no project).
Runs with
`write_mode: proposal` have their writes diverted into the proposal queue
(HTTP 202 with a proposal receipt) instead of being applied, and launch no
run: every route that sets one going, or arms one, answers them `403`
`proposal-mode agent runs cannot launch agent runs` before any lookup
(`POST /agents/{slug}/runs`, `/crews/{id}/runs` and `/teams/{id}/runs`,
`/test-runs/{id}/agent-run`, `/agent-runs/{id}/retry`,
`/automations/{id}/run-now`, a guided session's chat message, kickoff and
nudge, and `/projects/{id}/interviews` and `/interviews/{id}/invites`,
since an interview names the interviewer whose run each message its
invite's participant posts launches), and `/projects/{id}/draft-test-cases`
`403` `proposal-mode agent runs cannot draft test cases` (REQ-21, REQ-75),
while `POST /agent-runs/delegate` still starts the delegates a crew's design
gives a proposal-mode crew agent. A run another run's token launches records
the launching run as its `parent_run_id`, as a delegation does, so the
launching run's `/agent-runs/{id}/tree` lists it to a reader who could open
it by itself: the tree leaves out a run below its root that its reader
could not open, such as the unscoped run a project's run launched, which
only the workspace's admins read, and every run below that one. Each claim hands
the worker a freshly minted token, and the token stops authenticating (`401`)
as soon as the run finishes, is finalised after review, or is released back to
the queue; a released run's next claim issues a new one. A run token never
reviews a proposal, its own run's or another's: approve and reject answer it
`403` (REQ-21). The runner reads a claimed run's repository connections with
the run's token (`GET /projects/{id}/repo-connections`, read only, its own
project's), whose answer carries the local paths of the member whose personal
runner key claimed the run, and none for a run a workspace key holds.

### 3. Workers — Bearer org-scoped worker key

Runners (`agentd`) authenticate with `Authorization: Bearer <worker-key>`.
Keys are org-scoped rows in `worker_keys` (stored hashed):

- **Workspace keys** — minted by org admins (Settings → Worker Keys).
- **Personal runner keys** — one per member (`worker_keys.user_id` set);
  minted via `/orgs/{id}/my-runner-key` or the Agent Connector pairing flow.
  A personal key claims the runs its owner launched and the ownerless ones
  (board, automation, delegation) its owner could see: any of the
  workspace's for a workspace admin, otherwise those in a project the owner
  holds a role in; an ownerless run with no project, which only workspace
  admins see, is left to them and to workspace keys. A run a key launches
  records no launcher, so it routes as ownerless too: one with no project
  that a member's own personal key launched is not taken by that member's
  runner unless the member is a workspace admin. The run lifecycle calls
  (start, logs, finish, release) answer a personal key `404` on any other
  run, except the one its runner claimed, which it reaches while the run is
  claimed or running, whoever can see it meanwhile: a run whose project is
  deleted has no project, and the runner of a member who is no workspace
  admin still reads its cancel and reports it cancelled.
- **Session keys** — a personal key bound to a transient runner lease
  (`worker_keys.session_id` set), minted when a member leases a pool node and
  revoked when the lease ends. It routes like any personal key; it is kept
  distinct so leasing a cloud runner never rotates the member's own connector
  key.

A worker principal passes project checks only for projects belonging to its
own org; another org's project answers it as one no row has (`404`). There a
workspace key passes every project check up to an editor's
(REQ-42's workspace-wide editor rights) and gets the project's `403` on an
owner's, as a project editor does, while a personal key (a session key among
them) is its holder acting, reads included: it passes only where its holder
would, as an admin of the workspace or with a project role that meets the
route's (REQ-16); otherwise it gets what its holder's session gets, `404`
`project not found` where the holder has no role (as for a project no row
has) and the project's `403` where the holder's role falls short, and the
project list (`GET /projects`) gives it the projects its holder's own
session lists. No worker key creates a project: `POST /projects`,
`/projects/import` and `/templates/{id}/projects` answer it `403` `runner
keys cannot create projects`, since only a person owns what it creates.

### 3a. Runner pool nodes — Bearer `RUNNER_POOL_KEY`

Transient runner pool nodes present the deployment-wide `RUNNER_POOL_KEY`.
This principal has **no workspace and no user**: it may call the
`/api/v1/runner-pool/*` endpoints and nothing else. A node gains a workspace
identity only by being leased, and then authenticates as the lease's session
key like any other runner.

### 4. Legacy `WORKER_API_KEY`

A raw `WORKER_API_KEY` in the API server environment keeps old deployments
working: at startup it is registered as a workspace key for the bootstrap
org, named `env-bootstrap`, once a personal workspace exists to hold it, and
until then the middleware accepts the raw env value directly (resolving it
to the bootstrap org). Once a key row holds the value, the row decides:
revoked on the Runners tab (`DELETE /orgs/{id}/worker-keys/{keyId}`), the
value answers `401` `invalid token`, as any revoked key does, even while the
environment still holds it. A restart with the same value leaves the key
revoked, registers nothing and logs a warning; only a new value registers a
new key. The value is used exactly as set, like every credential the server
reads: spaces or a line break around it are not trimmed, and the server
names such a key in its boot log, never printing it.

## Authorization model

Enforced per-handler via `internal/api/authz.go`:

- **Platform admin** (`users.is_admin`) passes every check, in a project
  or workspace that exists: one no row has answers `404` `project not
  found` or `workspace not found`, as a project does to a worker key. The
  first registered user has it; a platform admin grants it to others from
  the Platform admin page (`PUT /api/v1/admin/users/{id}/admin`, REQ-155).
- **Org roles**: `admin` and `member` (`org_members.role`). Org admins of a
  project's org act as project owners.
- **Project roles**: `owner` > `editor` > `reviewer` > `viewer`. A member's
  effective role is the highest of their direct grant (`project_members`)
  and any people-team grant (`project_team_access` via `org_teams`). A
  `reviewer` (REQ-150) reads everything a viewer reads and may comment
  (`POST /chatter`), and is refused every write an editor makes; it is the
  role a reviewer share link grants (`docs/sharing.md`).
- **Agent runs** count as editor within their own project, except that they
  never approve or reject a proposal, and create no project, and a
  proposal-mode run launches no run; **workers** create no project either, and pass
  for any project in their org, a workspace key as an editor, a personal key
  only where its holder would, reads included; **run access** (viewing logs/streams) is
  granted to the launcher, then by the project ladder, then org admin for
  unscoped runs.
- **Crew writes**: project-pinned crews need project editor; workspace-wide
  crews need org admin. Automations follow the same split. A crew's pin
  (`project_id` on create, import and clone) must name a project of the
  crew's workspace (`400` otherwise), and a clone is checked where the copy
  lands as a new crew is. A pinned crew launched with no `project_id` runs
  in its pinned project. Only a caller who may know of a crew launches it: a
  member, a worker key or an agent run of its workspace (an agent run a
  pinned crew only in the pinned project, the one project it reaches), or a
  caller who reaches its pinned project; anyone else, an editor of the
  target project who is no member of the crew's workspace among them, gets
  `404` `team not found`, as for a crew no row has.
- **Existence hiding** (I3, OpenV REQ-17): a resource the caller cannot
  reach at all answers exactly as one that does not exist, the same status
  and the same message. A project where the caller has no role (and is no
  admin of its workspace), a project where a personal runner key's holder has
  none, another org's project to a worker key, and another project to an agent
  run answer `404` `project not found`; a
  workspace the caller is no member of (a deleted one to its former members
  included) answers `404` `workspace not found`; a resource looked up by its
  own id (a baseline, a run, a crew, a work item...) whose project or
  workspace the caller cannot reach answers that resource's own not-found,
  as an id no row has does (`404` `baseline not found`, `agent run not
  found`, `team not found`...); what a body names in a project or
  workspace the caller cannot reach answers as what does not exist: a
  link's endpoint, a crew's pin, a flow-down parent and an interview's
  persona the `400` of one no row has (`source artifact not found`, `target
  artifact not found`, `project not found`, `parent project not found`,
  `persona artifact not found`), and a people-team granted a project and
  a work item's crew assignee the `404` `team not found`, while one the
  caller does reach in another workspace or project is refused as such
  (`400`; a crew assignee `team belongs to a different workspace`). A
  result's `test_case_id` outside its run's project answers `404`
  `artifact not found`, as one no row has, whether or not the caller
  reaches it, and before its type is read: a run verifies its own
  project's test cases. An artifact's update and restore, which look the
  artifact up before their guard, answer an artifact the caller cannot
  reach, as one no row has, `404` `artifact not found`, and a crew's
  launch, and a work item assigned to a crew, answer a caller who may not
  know of the crew (*Crew writes*) `404` `team not found`. A worker key or
  run token, which
  has no session, gets the workspace guard's `401`, but one of another
  workspace, at a route that looks its resource up first (a people-team, a
  workspace-wide attribute definition), gets that resource's not-found; and
  a run token polling a run outside its project (`GET
  /agent-runs/delegate/{id}`) gets `404` `agent run not found`. A caller who
  reaches the project or workspace but lacks the role a route needs gets
  `403`. An account's uploaded picture is read only by the account itself,
  a member of a workspace it is a member of and a platform admin; anyone
  else gets `404` `user has no uploaded picture`, as for an account with no
  picture or none at all (#379's decision 14). A public link's token is not
  hidden: whoever holds the link opens it.
- **Malformed ids**: an id that is not a UUID, in a path, a query or a body
  field that names something to look up, answers exactly as a well-formed id
  no row has: the same `404`, an empty list, or nothing changed; never a
  `500`. So does text that is not UTF-8 or holds a NUL (a path's or a
  query's `%FF` or `%00`, a body's `\u0000`). Where the lookup comes before
  the guard or the id sits in the query or the body, an id no row has
  answers `404` too: an artifact's update and restore and a result's
  `test_case_id` `artifact not found`, a download's or its options'
  `baseline_id` and a test run's `baseline_id` `baseline not found`, and a
  work item's crew assignee (`assignee_type` `team`) `team not found`. A
  body field stored as a reference without a lookup (a work item's
  person's or agent's `assignee_id`, a launch's `work_item_id`...) still
  passes the database's refusal through as a `400` (quirk Q19), and `POST
  /projects/{id}/draft-test-cases` refuses a `requirement_ids` entry that
  is not a UUID with its own `400` `requirement_ids must be valid artifact
  ids`, where a well-formed id no artifact has is launched with the rest,
  since nothing looks the ids up.

## Route inventory

Generated from the router registrations in `internal/api/*.go`
(`RegisterRoutes` and the `register*Routes` helpers) as of commit `2d6cd72`.
Auth column: `open` (no credentials) · `user` (any session) ·
`viewer`/`reviewer`/`editor`/`owner` (project role ladder; agent runs count as editor in
their own project, workers pass within their org, a workspace key up to
`editor`, a personal key only with its holder's role) · `org member`/`org admin`
(workspace role) · `worker` (worker key) · `run` (run token) ·
`token` (public one-time/invite token).

### Health

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/health` | Liveness check (`{"status":"ok"}`) | open |

### Auth & users

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/auth/register` | Create password account `{email, password, name, invite_token?}` (first user becomes admin) and log in; on a server with SMTP the account starts unverified and a verification link is emailed. `invite_token` is the token from an invite link (`/login?invite=<token>`): when it is valid **for the address being registered**, that one invitation is accepted and the membership it names is granted, **and the account's address is marked verified** — the token was mailed to that address and nowhere else, so a closed, verification-required deployment does not wall the invitee behind a second mail. Without it registration grants no membership — the invitation stays pending until its link is used. When a token was supplied the answer carries an `invitation` field saying what it did: `accepted`, `already_member`, `email_mismatch` (live link, different address), `invalid` (unknown, revoked, spent, expired — including revoked between the sign-up being allowed and the membership being claimed); the field is absent when no token was sent. The token is resolved **once** per sign-up, so a revoke can never produce an account that "passed" and then joined nothing in silence. `403 {"code":"registration_closed"}` when `OPENV_REGISTRATION=closed` and the request carries no `invite_token` issued to the address being registered (a pending invitation for the address is **not** a door: it would leak who has been invited, and let a stranger squat the address). `400 weak_password` for a password under 8 characters, as a password change and a reset answer it; an invalid or already registered address is a `400` with no code. Sign-in's refusal stays one generic `401` whatever the password, so it never tells which accounts exist | open |
| POST | `/api/v1/auth/login` | Password login, sets session cookie | open |
| POST | `/api/v1/auth/logout` | End session, clear cookie | open |
| GET | `/api/v1/auth/me` | Current user profile | user |
| GET | `/api/v1/auth/config` | Which sign-in methods are enabled (Google, OIDC), whether `email_verification_required`, whether `password_reset_email` can be offered (a mailer is configured), and the `registration` policy | open |
| GET | `/api/v1/auth/policy` | The registration policy alone, plus the server's password rule: `{"registration":"open"\|"closed","min_password_length":8}`. Clients state `min_password_length` in their password forms rather than a copy of it, so the form and the server can never disagree | open |
| POST | `/api/v1/auth/invitations/preview` | Preview an invite link `{token}` → `{email, org_name, role, expires_at}`; one `404` for every unusable link. The token travels in the body, never in the path: an invite link is a credential, and a URL is written into access logs, proxy logs, browser history and `Referer` headers. Throttled on its own bucket, not the sign-in one. Only an unusable link is `404`: a lookup that fails for any other reason answers `500`, because telling the invitee their link is invalid would send them off for a replacement that fails identically | open |
| POST | `/api/v1/auth/invitations/accept` | Join the signed-in account to the invitation's workspace `{token}` → `{org_id, org_name, role, already_member}`. Converts only when the **session's own email is the invited address**; otherwise `403 {"code":"invitation_email_mismatch"}`, whose body never names the invited address (the preview already shows it to whoever holds the link). `404` when the link is unusable. An account that is already a member keeps its role — `role` reports the role it holds, `already_member` is `true`, and the invitation is spent. A successful accept (`already_member` included) **marks the account's address verified**: the token was mailed to that address and nowhere else, and it is the session's own address, so this is the same proof `POST /auth/register` accepts from an `invite_token` | user (cookie only, JSON body) |
| POST | `/api/v1/auth/share/accept` | Take up a **reviewer share link** `{token}` → `{project_id, project_name, role}` for the signed-in account: it becomes a `reviewer` of the project, or keeps the stronger role it already holds (`role` reports what it holds afterwards). `400` for a public link (it needs no account), `404` for an unusable one, `429` on the share-link bucket. See `docs/sharing.md` | user (cookie only, JSON body) |
| PUT | `/api/v1/me/password` | Change password `{current_password, new_password}`; `204` on success and every OTHER session of the account is invalidated. `400 weak_password`, `403 password_incorrect`, `409 no_password` (SSO-only account) | user |
| POST | `/api/v1/me/avatar` | Upload the account's profile picture: multipart field `file`, PNG/JPEG/GIF/WebP whose bytes are the declared type (an image of another type is `400`), at most 2 MiB (`413` beyond). Replaces any previous picture; returns the user with `has_avatar:true` and an `avatar_url` on the API (`/api/v1/users/{id}/avatar?v=<upload time>`, relative to the API origin) that from then on outranks the identity provider's picture at sign-in | user |
| DELETE | `/api/v1/me/avatar` | Remove the uploaded picture; returns the user with `has_avatar:false` and an empty `avatar_url` (an identity provider's picture returns at the next sign-in) | user |
| GET | `/api/v1/users/{id}/avatar` | An account's uploaded picture (served as its stored type, `Content-Disposition: inline`, cached a day — the URL changes on every upload), to the account itself, a member of a workspace it is a member of, and a platform admin; `404` when none is uploaded, and the same `404` to anyone else, so the answer does not say whether the account exists or has a picture | user |
| POST | `/api/v1/auth/verify-email` | Confirm an emailed link `{token}`; returns the user (`400` invalid/expired, `409` address taken). Grants **no** workspace membership: the address it confirms is one the account asked the mail to be sent to, so it is not evidence that the account is the person an admin invited | open |
| POST | `/api/v1/auth/verify-email/resend` | Email a fresh link to the session's account (`202 {sent_to}`; `409` already verified; `502` mail failed) | user (cookie only, JSON body) |
| POST | `/api/v1/auth/verify-email/change` | Email a fresh link to a corrected address `{email}`; the account's address changes when that link is confirmed | user (cookie only, JSON body) |
| POST | `/api/v1/auth/password-reset` | Email a password reset link `{email}` (REQ-158). Answers `202 {sent_to}` for **every** well-formed address, known or not, and does the lookup and the send after answering, so neither the status nor the timing says whether an account exists; an address with no account, or an SSO-only one, is simply sent nothing. `409 {"code":"reset_email_unavailable"}` on a deployment with no mailer (server configuration, not account state); `429` per client address and per address asked for. The link is valid for one hour and works once | open |
| POST | `/api/v1/auth/password-reset/confirm` | Spend a reset link `{token, new_password}`: sets the password and ends **every** session of the account, and marks the address verified when the link was emailed (it reached that inbox). `204`; `400 weak_password` (checked before the link is spent, so a weak password does not cost it), `400 reset_invalid` (unknown, spent or expired). No session is created: the person signs in with the new password | open |
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
| PUT | `/api/v1/orgs/{id}` | Update name/settings/limits/`monthly_budget_usd`/`release_channel` (`nightly`, `stable`, or `""` for the plan's default; `400` on a plan that always runs nightly)/`upgrade_window` (`{day 1-28, hour 0-23, timezone}` or `null` for "at the cut"; `400` for bad values or a plan that cannot choose). Every part is checked before any is written, so a request refused for one part changes nothing. The workspace answers with its effective `release_channel`, `release_channel_locked`, `stable_release` and window | org admin |
| GET | `/api/v1/orgs/{id}/features` | The caller's feature gates in the workspace: `{channel, stable_release, preview, features: {key: bool}, next_stable_release?, next_stable_at?}`. Keys today: `flow-down`, `artifact-owners`, `share-links`, `assistant-project-edits` (the V&V Assistant's new-artifact, edit and move cards), `default-workspace` (choosing the workspace a sign-in lands in), `figure-titles` (renaming a figure). Gates are resolved from the channel and the stable release the workspace has turned on, or the newest stable when the caller previews it (REQ-137, REQ-138) | org member |
| PUT | `/api/v1/orgs/{id}/plan` | Move a workspace to another plan `{plan}` → the workspace (REQ-154). Plans: `single`, `business_lite`, `business`, `enterprise`, `self_host`, `open_source` (plus the legacy aliases `free`, `team`); `400` for any other name, `404` for an unknown workspace, and `409` with `code: "already_subscribed"` for a granted plan (`enterprise`, `open_source`) while the workspace holds a live subscription, which is cancelled first (REQ-168). A move from a plan that always runs nightly (`single`, `business_lite`, `self_host`, `open_source`, legacy `free`) onto one whose admins choose the channel (`business`, `enterprise`, legacy `team`) writes `nightly` as the channel override where none is set, as a checkout does, so the workspace keeps the features it uses (`release_channel: "nightly"`, unlocked); otherwise the override is kept and the effective channel follows the new plan's default where none is set. This is how the open-source tier is granted: `python3 scripts/openv/sync.py api PUT /api/v1/orgs/<id>/plan '{"plan":"open_source"}'` signed in as a platform admin (`OPENV_EMAIL`/`OPENV_PASSWORD`) | platform admin |
| PUT | `/api/v1/orgs/{id}/members/me/preview` | `{enabled}`: switch the caller's own account to the newest stable release early in this workspace; answers the caller's gates. `400` on a plan that always runs nightly; `404` `workspace not found` for an account with no membership of the workspace, as for one no row has, and `403` for a platform admin outside it, whom the guard lets by, since the preview is kept on the membership | org member |
| DELETE | `/api/v1/orgs/{id}` | Soft-delete a company workspace: hidden and locked immediately, restorable for 30 days, then hard-deleted with all its data by a daily purge, which then removes its stored files (every version of its figures, its evidence files and its logo) from the uploads directory; one that cannot be removed is logged. Personal workspaces are refused. | org admin |
| POST | `/api/v1/orgs/{id}/restore` | Restore a soft-deleted workspace within the grace period; returns the workspace as restored. `403` for a member who is not its admin, `404` `workspace not found` for an account that is no member, as for a workspace no row has | org admin (of the deleted org) |
| POST | `/api/v1/orgs/{id}/activate` | Set the session's active workspace; `404` for a workspace that does not exist | org member |
| GET | `/api/v1/orgs/{id}/members` | List workspace members | org member |
| POST | `/api/v1/orgs/{id}/members` | Add member by email. `201` with the membership when the address has an account **whose owner has proved it** and joined; `409` when it is already a member (change a role with `PUT`); `202 {invitation, link, emailed, reason?}` when it has no account — or, where the deployment requires email verification, has one that has **not** verified that address — and was invited instead, so the membership waits for somebody to read the mailbox. `400` for a personal workspace. `POST /orgs/{id}/invitations` answers the same outcomes with the same statuses and bodies | org admin |
| GET | `/api/v1/orgs/{id}/invitations` | Pending invitations to the workspace | org admin |
| POST | `/api/v1/orgs/{id}/invitations` | Bring an address `{email, role}` into the workspace, taking the same branch **and the same statuses** as `POST /members`: `201` with the membership when the address already has an account that has verified it, `409` when it is already a member, `202 {invitation, link, emailed, reason?}` when it has no account (or an unverified one, where verification is required). `link` is the one-time `${FRONTEND_URL}/login?invite=<token>` and is never retrievable again. `emailed` is `true` when SMTP is configured and the send was **queued**: the mail goes out off the request path, so no admin waits on a relay, and a failure is logged rather than reported. Re-inviting an address replaces whatever unaccepted invitation it holds — keeping its `id`, so an id a client already holds stays valid — except, on a deployment that can send mail, within an hour of an **unchanged** one (same role, still valid) whose link was **actually delivered**, which is returned as-is with `emailed:false`, a `reason`, and no `link`, so a repeated click cannot mail the same person again. An invitation whose send failed, or never happened, has nothing in anybody's inbox and is minted and sent again (without SMTP the link in the response is the delivery, so a fresh one is always minted). Throttled per inviting account (`429`) | org admin |
| DELETE | `/api/v1/orgs/{id}/invitations/{invId}` | Revoke a pending invitation (its link stops working) | org admin |
| PUT | `/api/v1/orgs/{id}/members/{userId}` | Change org role `{role}` (`admin`, `member`). `400` for any other role, for an account that is not a member, and for demoting the workspace's last admin | org admin |
| DELETE | `/api/v1/orgs/{id}/members/{userId}` | Remove member (self-removal = leave, allowed for members). `400` for the workspace's last admin and, as for a role change, for an account that is not a member, including the second of two removals of one member at once; nothing is removed and no event is published | org admin / self |
| GET | `/api/v1/orgs/{id}/teams` | List people-teams | org member |
| POST | `/api/v1/orgs/{id}/teams` | Create people-team | org admin |
| PUT | `/api/v1/org-teams/{id}` | Rename/edit team | org admin |
| DELETE | `/api/v1/org-teams/{id}` | Delete team | org admin |
| POST | `/api/v1/org-teams/{id}/members/{userId}` | Add user to team | org admin |
| DELETE | `/api/v1/org-teams/{id}/members/{userId}` | Remove user from team | org admin |
| GET | `/api/v1/projects/{id}/team-access` | List team grants on a project | viewer |
| PUT | `/api/v1/projects/{id}/team-access` | Grant/update a team's project role `{org_team_id, role}`: `400` for a team of another workspace, `404` `team not found` for one no row has or of a workspace the caller is no member of | owner |
| DELETE | `/api/v1/projects/{id}/team-access/{teamId}` | Revoke a team grant | owner |
| GET | `/api/v1/orgs/{id}/quality-rules` | Workspace requirement quality rules (house style every project inherits) | org member |
| PUT | `/api/v1/orgs/{id}/quality-rules` | Set the house style; an empty body clears it back to the platform defaults | org admin |
| GET | `/api/v1/orgs/{id}/logo` | The workspace logo image (served as its stored type, `Content-Disposition: inline`); `404` when none is set | org member |
| POST | `/api/v1/orgs/{id}/logo` | Upload the workspace logo: multipart field `file`, PNG/JPEG/GIF/WebP whose bytes are the declared type (an image of another type is `400`), at most 2 MiB (`413` beyond). Replaces any previous logo; returns the org with `has_logo`. `404` for a workspace that does not exist, before anything is stored | org admin |
| DELETE | `/api/v1/orgs/{id}/logo` | Remove the workspace logo; returns the org with `has_logo:false` | org admin |

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
| GET | `/api/v1/orgs/{id}/runner-session` | My transient runner lease (with deadline and a `pool_load` band — `green` / `amber` / `red` / `unavailable`, never a count) | org member |
| POST | `/api/v1/orgs/{id}/runner-session` | Lease a cloud runner: `201` with a new lease; the lease the caller already holds is returned with `200` and the same payload, rather than a second node or a `409`; `503` when the pool is full | org member |
| POST | `/api/v1/orgs/{id}/runner-session/extend` | Reset my lease's clocks (capped at 8h from its start) | org member |
| DELETE | `/api/v1/orgs/{id}/runner-session` | End my lease now (the node is wiped) | org member |
| GET | `/api/v1/orgs/{id}/runner-pool` | Pool occupancy and the workspace's live leases | org admin |
| POST | `/api/v1/runner-pool/nodes` | Register a pool node | pool key |
| POST | `/api/v1/runner-pool/nodes/{id}/heartbeat` | Heartbeat; returns the node's lease (credential once) | pool key |
| POST | `/api/v1/runner-pool/nodes/{id}/release` | Report a lease wiped; return to the idle pool. `404` `pool node is not registered` for a node the pool has no record of, as the heartbeat answers; the runner does not retry it, and registers again when its heartbeat gets that `404` | pool key |

### Projects, baselines, templates

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/projects` | Create project in active workspace (creator becomes owner). An agent run's token and a runner key are refused (`403`) before the body is read; then `403` `plan_read_only` on a read-only workspace and `403` `limit_reached` at its project maximum | user |
| GET | `/api/v1/projects` | List projects the caller can access (a personal runner key: those its holder can); an agent run's token, its own project alone | user |
| GET | `/api/v1/projects/{id}` | Project details | viewer |
| PUT | `/api/v1/projects/{id}` | Update project: `name`, `description`, `agent_auth`, `parent_project_id` (the project this one refines, `docs/flow-down.md`; `""` detaches; `400` for a parent in another workspace, the project itself or one of its descendants, and `400` `parent project not found` for one no row has or the caller cannot reach) | editor |
| DELETE | `/api/v1/projects/{id}` | Delete project and, in one transaction, all that belongs to it alone: every version of its artifacts with their chatter, attachment records and links (those to and from other projects' artifacts too), work items, crews, test runs, baselines, evidence, interviews, guided sessions, project attribute definitions, automations, proposals, share links, members and team grants. Its agent runs stay, with no project, for the workspace's usage; a queued one is cancelled and a claimed or running one asked to stop, as `POST /api/v1/agent-runs/{id}/cancel` does, one awaiting approval is cancelled (its proposals go with the project), and each one's run token is refused from then on. They stop naming what the delete removes (the card, guided or interview session, automation, crew and crew node of the project); what they name outside it stays. Its activity-log events stay; its child projects are detached to the top level. Once the delete has committed, the stored files of its attachments (every version) and evidence are removed from the uploads directory; one that cannot be removed is logged and does not fail the delete | owner |
| GET | `/api/v1/projects/{id}/children` | The projects filed under this one | viewer |
| GET | `/api/v1/projects/{id}/linked-artifacts` | The far end of every link crossing out of the project: `[{id, project_id, project_name, ref, type, title, status}]`, so a parent requirement a local one refines, or the child requirements refining a local one, can be named without rights on those projects | viewer |
| GET | `/api/v1/projects/{id}/parties` | The reference parties the project recognises as owners: `{parties: [{name, note, default}]}`, the workspace's own company first and marked `default` | viewer |
| PUT | `/api/v1/projects/{id}/parties` | Replace the project's own parties `{parties: [{name, note}]}`; the default is never stored; `400` for an empty or repeated name | editor |
| GET | `/api/v1/projects/{id}/export` | Export project JSON | viewer |
| POST | `/api/v1/projects/import` | Import a project export (JSON or ReqIF); `400` for a document that does not parse. An agent run's token and a runner key are refused (`403`). Counts toward the workspace's project maximum (`403` `limit_reached`) and stays allowed on a read-only workspace (REQ-177) | user |
| GET | `/api/v1/projects/{id}/report` | Legacy: PDF (`?format=pdf`) or Word (`?format=docx`), the specification with the default content but without V&V status: the route reads no test evidence, so the status the download carries by default would be wrong here. The download routes are the supported path | viewer |
| GET | `/api/v1/projects/{id}/download/options` | What a download can be narrowed to: sections, types, owners (`{owner, count}`, most artifacts first), attachment categories, fields, template presets, defaults. The fields are the attribute keys the artifacts carry: a baseline that kept the attribute definitions (REQ-5) labels a defined key as its definition did and lists the defined keys before the discovered ones, while the live project and an older baseline word each key themselves; a definition no artifact carries is not a field | viewer |
| GET | `/api/v1/projects/{id}/download/{json,csv,excel,reqif,pdf,docx}` | One download in the chosen format; see the download parameters below | viewer |
| POST | `/api/v1/projects/{id}/baselines` | Snapshot a baseline (see below for what the snapshot holds); publishes `baseline.captured` `{name}` | editor |
| GET | `/api/v1/projects/{id}/baselines` | List baselines | viewer |
| GET | `/api/v1/baselines/{id}` | Baseline contents | viewer |
| DELETE | `/api/v1/baselines/{id}` | Delete baseline; publishes `baseline.deleted` `{name}`, the name it had, so the activity log records it (REQ-5). `403` for an editor or viewer, `404` `baseline not found` for one no row has | owner |
| GET | `/api/v1/projects/{id}/share-links` | The project's share links (`docs/sharing.md`), tokens never included | owner |
| POST | `/api/v1/projects/{id}/share-links` | Mint a share link `{role: "public"\|"reviewer", label, expires_at?}` → `201` with the link, its `token` and the `url` to hand out (`${FRONTEND_URL}/share/<token>`); the token is stored hashed and is in this answer and nowhere else. `expires_at` is the instant the link closes, whatever offset it is sent with (see Times); the list answers it in UTC. `400` for another role, and for an `expires_at` that falls outside years 1 to 9999 in UTC, such as `9999-12-31T23:00:00-05:00` (`expires_at must fall between years 1 and 9999 in UTC`), which the list could not answer. It is truncated to the microsecond, as stored, before that check, so a link closes no later than sent; `403 {"code":"feature_unavailable"}` on a stable-channel workspace whose release lacks `share-links` | owner |
| DELETE | `/api/v1/share-links/{id}` | Revoke a link: it opens nothing from then on (idempotent) | owner of its project |
| GET | `/api/v1/templates` | List templates (global + workspace) | user |
| POST | `/api/v1/templates` | Save a project as a template | editor |
| POST | `/api/v1/templates/{id}/projects` | Create project from a built-in template or one of the caller's workspace; `404` `template not found` for a UUID no template has and for another workspace's template. An agent run's token and a runner key are refused (`403`); `403` `plan_read_only` on a read-only workspace and `403` `limit_reached` at its project maximum, as `POST /projects` | user |

**Baselines** carry `created_by` (the capturing account) and
`created_by_name` (its display name, resolved server-side so a client never
shows a bare id) on both the list and the create response. Both are absent
when nobody is attributable: a baseline captured before authorship was
recorded, one taken by an automation holding a workspace key, or one whose
author has since deleted their account — deleting an account nulls the column
rather than removing the baseline, because a project's history must outlive
the people in it.

`GET /api/v1/baselines/{id}` returns the snapshot itself — a whole project
export, which is close to a megabyte of JSON for a real project. The snapshot
is the JSON export (artifacts, links, the attachments' metadata, the product
profile, linked artifacts) with `attribute_definitions` beside it: the
workspace's and the project's definitions in effect when it was captured
(REQ-5), which the live JSON export leaves out. Attachment **files** stay out
of it; each attachment's record is kept — its name, type, size, figure
reference and the artifact it belongs to. A snapshot captured before
baselines kept definitions has no `attribute_definitions` and reads as it
always did: its ReqIF download types no attribute as an enumeration. The
open-source showcase publishes a baseline's snapshot without its
definitions, as a live public link carries none. It is served
gzipped to any client that offers it (see `docs/operations.md`), but a client
should still show progress while it loads rather than rendering an empty
project.

**Export/import caveats** (`internal/domain/exports/export.go`):

- The `?format=` on export accepts `json` (default), `csv`, `excel` and
  `reqif` (OMG ReqIF 1.x, read by DOORS/Polarion). Anything else is a 400.
- **Excel** (`internal/domain/exports/excel.go`) is an `.xlsx` workbook served
  as `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`: a
  `Project` cover sheet (name, description, export time, baseline when the
  snapshot came from one, artifact and link counts), one sheet per artifact
  type present named after the type ("Requirements", "Test Cases"), and a
  `Links` sheet of the traceability links by endpoint ref and title. An
  artifact sheet carries the CSV's columns with the stable `ref` and the
  derived `section` number in front. `section` is a heading's own number and,
  for every other row, the number of the heading it sits under — the same
  section the PDF nests it in — so a flat sheet still places an artifact in
  the document; a row with no heading above it leaves it empty. Refs, section
  numbers and timestamps are written as text so a spreadsheet cannot re-read
  them as numbers or dates. It is a download only — there is no Excel import.
- **ReqIF import** (`internal/domain/exports/reqif_import.go`): `POST
  /api/v1/projects/import` accepts a ReqIF document as well as JSON. ReqIF is
  selected by `?format=reqif`, an XML/ReqIF `Content-Type`, or sniffed from a
  `<REQ-IF` root; anything else is treated as JSON. A malformed ReqIF (or an
  enum attribute whose value is not among its datatype's declared values) is a
  **400**, and so is JSON that does not parse into an export (a syntax
  error, a file cut short, a value of the wrong type). Imported artifacts
  are remapped to fresh ids at version 1, with parent hierarchy, links,
  status, and attributes reconstructed. Bodies are carried as XHTML-typed
  values so hard line breaks survive the round trip.
- **Downloads** (`internal/domain/downloads`, `docs/reports.md`): every
  `/download/{format}` reads the same query. `baseline_id` picks a snapshot
  (`404` `baseline not found` for one no row has, a malformed id or another
  project's, as every route that takes a baseline answers it: the options,
  the report, the V&V reads and report, the quality report, the impact
  read, the AI map, a baseline's read, diff and delete, and the diff's
  `against`; `/export` takes none);
  `sections`, `types` (a comma-separated list of artifact types, or `all`),
  `owners` (a comma-separated list of owner names: only
  the artifacts whose `owner` attribute is one of them, plus the headings,
  so one party's share of a project can be handed over on its own — REQ-148),
  `headings=0`, `attachments` narrow it (REQ-56). A `template` (`standard`,
  `requirements-review`, `test-planning`, `vv`) sets the types when `types`
  is left out, in every format, and `types=all` keeps every type whatever
  the template keeps, as the wizard sends it once every type is ticked
  (REQ-131). The PDF and Word documents also take the template's content,
  and read `toc`, `traceability`,
  `figures`, `vv`, `results` (`0|1`) and `fields` (`all`, `none`, or a
  comma-separated list of attribute keys); an explicit parameter wins over
  the template. Each requirement's V&V status is in a document by default
  (REQ-6; `vv=0` leaves it out); the test results are not (`results=1`).
  The ReqIF download types an enum attribute as an enumeration by the
  attribute definitions in effect, from the same function the ReqIF export
  takes them from, so the two documents type alike; a baseline's is typed by
  the definitions it kept. As in the export, a value that is not in its
  enum attribute's list, such as one left behind when the list was edited,
  is left out of the file. A baseline's PDF and Word documents name its
  fields as its options do. Any attachment category turns the response into a zip
  holding the document and the files. The cover states whether the
  document is a named baseline (with its id and capture time) or the live
  project at the export time, and shows the workspace logo when one is set.
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
| POST | `/api/v1/artifacts` | Create artifact. `copied_from` (the id of an artifact the caller can read) marks a duplicate or a paste: the new artifact carries none of the source's versions or links, and its feed opens with one system note, *Copied from REQ-12 (version 3)*; an unreadable or unknown source leaves the copy without the note | editor |
| GET | `/api/v1/artifacts` | List artifacts (`?project_id=&type=&owner=&doc_numbers=1`; `owner` matches the `owner` attribute exactly) | viewer |
| GET | `/api/v1/artifacts/{id}` | Get artifact (current version) | viewer |
| PUT | `/api/v1/artifacts/{id}` | Update (creates a new temporal version); `404` `artifact not found` for an artifact no row has or the caller cannot reach | editor |
| DELETE | `/api/v1/artifacts/{id}` | Soft-delete (history retained) | editor |
| GET | `/api/v1/artifacts/{id}/versions` | Version history, newest first. A deleted artifact's stays readable: the guard asks the project of its versions, so the project's viewers read it after the delete (REQ-4); an id no version has, or one in a project the caller cannot reach, answers `404` `project not found` | viewer |
| POST | `/api/v1/artifacts/{id}/restore` | Restore an older version `{version}` as a new one. The artifact keeps its ref, as every version does (a restore that brings back a type with another prefix draws a new one, as a retype does), and the restore publishes `artifact.restored` `{artifact_type, title, version, restored_version}`. `404` `artifact version not found` for a version the artifact never had; `404` `artifact not found` as for an update | editor |
| GET | `/api/v1/artifacts/{id}/links` | Links per artifact version: `?version=N` reads that version's links, a deleted artifact's among them, as its versions are read; without it, the live links | viewer |
| POST | `/api/v1/links` | Create traceability link. A link may cross projects: the caller needs editor rights on both ends' projects, except for `refines` (the flow-down link, `docs/flow-down.md`), which needs editor rights on the source's project and viewer rights on the target's | editor |
| GET | `/api/v1/links` | List links (`?project_id=`): every link that touches the project, from either end, so a flow-down link written from a child project is seen by the parent too | viewer |
| GET | `/api/v1/links/{id}` | Get link | viewer |
| PUT | `/api/v1/links/{id}` | Update link | editor |
| PUT | `/api/v1/links/{id}/confirm` | Clear the suspect flag: an editor vouches that the trace still holds after an artifact at one end changed. Idempotent — confirming a link that is not suspect changes nothing. Refused `403` for a proposal-mode agent run rather than diverted to a proposal: this is a human sign-off, and routing it through a proposal would defeat the review the flag exists to trigger | editor |
| DELETE | `/api/v1/links/{id}` | Delete link | editor |
| POST | `/api/v1/attachments/upload` | Upload a file (multipart) to an artifact as a figure (`artifact_id`). A figure takes its artifact to a new version, as a new figure version and a rename do (see Figures). An upload whose project is deleted while the file comes in waits for the delete and answers `404` "project not found", its file removed | editor |
| GET | `/api/v1/attachments/{id}` | Attachment metadata (`title` is the name a member gave the figure, empty when none; readers fall back to `original_filename`) | viewer |
| PUT | `/api/v1/attachments/{id}` | Rename a figure: `{title}` (trimmed, up to 255 characters, `""` clears it). A change is a new figure version over the same image and a new artifact version, recorded in the notes; an unchanged title writes nothing. `403` with the remedy while the workspace's channel has not received `figure-titles` (REQ-157) | editor |
| GET | `/api/v1/attachments/{id}/download` | Download the file (`?version=N` for a superseded one) | viewer |
| POST | `/api/v1/attachments/{id}/versions` | Replace a figure's file with a new version (multipart) | editor |
| POST | `/api/v1/attachments/{id}/versions/{version}/restore` | Bring an older version's file and title back as a NEW version; nothing is deleted. 404 for a version the figure never had, 409 for the one already current | editor |
| GET | `/api/v1/attachments/{id}/versions` | A figure's version history, newest first; each entry carries the file, its `kind` and the `title` the figure had at that version, so a rename and a new file read alike | viewer |
| DELETE | `/api/v1/attachments/{id}` | Delete a figure with every version of it. Once the delete has committed, the stored file of every version is removed from the uploads directory; one that cannot be removed is logged and does not fail the delete, and a delete that fails removes no file | editor |
| GET | `/api/v1/artifacts/{artifactID}/attachments` | List an artifact's attachments | viewer |
| GET | `/api/v1/projects/{projectID}/attachments` | Every attachment in the project, in artifact order then figure number. Serves cross-artifact figure citations, which need the project's figures as one list | viewer |
| POST | `/api/v1/chatter` | Comment on an artifact (a reviewer may: that is what the role is for) | reviewer |
| GET | `/api/v1/chatter` | List an artifact's activity feed. Each entry carries `mentions` (the project members its @names resolve to) and `todo` (the work item raised from it, with its current status) — both composed at read time, neither stored | viewer |

### Figures

A file attached to an artifact is a **figure**, and carries a reference of
its own — `REQ-17-FIG-1` — built from the artifact's stable reference and a
per-artifact counter.

**What may be attached** is the catalogue in
`internal/domain/attachments/kind.go`: images (PNG, JPEG, GIF, WebP, SVG,
TIFF, BMP), PDF, and CAD (STEP, IGES, STL, 3MF, OBJ, PLY, glTF, DXF, DWG and
the common native part formats). Anything else is refused with `400`.

The **extension decides the recorded type**, not the uploaded
`Content-Type`: browsers send `application/octet-stream` for most CAD
formats. A declared image type is honoured for a file whose name says
nothing. Bytes are checked against the format's signature where it has one
(`%PDF-`, `ISO-10303-21`, `PK` …); formats with no dependable signature —
binary STL, native part files — pass on their extension, because nothing
rests on that check (see below).

Every attachment carries a **`kind`** in its JSON: `image`, `document`,
`model` or `other`, derived from the MIME type rather than stored, so a
client asks it instead of pattern-matching types of its own. `image/vnd.dxf`
and `image/vnd.dwg` are `model`: their registered types begin `image/` and
nothing can render them as pictures.

**Serving is an allowlist of one.** Only a raster image is served
`Content-Disposition: inline`. SVG, PDF, CAD and everything else is handed
over as a download under `Content-Security-Policy: sandbox; default-src
'none'; frame-ancestors 'none'`, and the API-wide `X-Frame-Options: DENY`
stands on every response. That is what makes storing a PDF or an opaque CAD
file safe without trusting its contents, and it is why the app previews those
formats by fetching the bytes with the member's session and rendering them on
its own origin rather than pointing a frame at the API.

**Only images reach a generated document.** A report embeds `kind: image`
attachments and nothing else; a download's attachment groups file non-images
under `documents`, `models` or `data` rather than `figures`.

- The number is minted once and **never reissued**: the counter only moves
  forward, so deleting a figure does not free its number, and two concurrent
  uploads cannot be handed the same one. A partial unique index on
  `figure_ref` backstops the counter.
- The stored name is the figure's (`REQ-17-FIG-1.png`, `REQ-17-FIG-2.step`),
  and a download is served under it, so saving one lands a file named for what
  the document calls it. The name the uploader's file had is kept as `original_filename`,
  and the on-disk path stays UUID-unique: the uploads directory is flat across
  projects, figure references are unique only within one, and each version
  needs a file of its own.
- An artifact with no stable reference yields no figure reference rather than a
  bare `FIG-1` that would collide once the artifact got one.

Adding a figure is an edit of the artifact that carries it: the upload takes
the artifact to a new version (REQ-4), through the same attribute-free update
as below, and writes the figure's note to its feed.

Uploading a **new version** keeps the figure's reference and supersedes its
file, and may change its format — a sketch replaced by the real drawing — so
each version carries its own `kind`. Because the artifact now shows something different, that upload also
takes the artifact to a new version and writes a note to its feed
("Figure REQ-17-FIG-1 updated from version 1 to 2 — …"). Superseded versions
stay retrievable through `?version=N`. The artifact's new version is an
attribute-free update: it does not demote an approved artifact or mark its
links suspect, because nothing the artifact *says* changed.

### Review queue

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/review-queue` | Suspect links + `in_review` artifacts awaiting review | viewer |
| POST | `/api/v1/projects/{id}/review-round` | Start a review round: every in-scope draft moves to `in_review` | editor |

A **review round** is the bulk form of the single-artifact status change: it
walks the project's current artifacts and moves each one in `draft` to
`in_review`, leaving every other status exactly as it is. Each move publishes
the same `artifact.status_changed` event and writes the same feed note it
would have one at a time, so an artifact's own history explains how it entered
review; the round itself also publishes one
`project.review_round_started` event against the project.

The round carries no stored state, which is what makes running it each cycle
do the right thing: an `approved` artifact is left approved, because approval
was of that exact content, while one whose content was edited since approval
is already back in `draft` (see the status rules above) and so is picked up.
Running it twice with nothing in between writes nothing the second time.

```json
POST /api/v1/projects/{id}/review-round
{ "types": ["requirement", "test-case"] }
```

`types` is optional and must name artifact types from the catalog; an unknown
one is a 400 and nothing is written. Omitted, the scope is **every type in the
catalog**, headings and descriptions included: a reviewer signs off the
document, and structure left out of every round would sit in draft for ever,
making a "reviewed" project one that still had unreviewed artifacts in it.
Proposal-mode agent runs are refused (403): the proposal vocabulary has no
status op, and a review-gated agent starting the review would defeat the gate.

The response reports what the run did — `moved` (the artifacts, in full) plus
`already_in_review`, `approved`, `superseded` and `out_of_scope` counts that
together account for every current artifact in the project, and `types`, the
scope actually used.

#### Deciding a review

The queue itself has no decision endpoint: approving and rejecting are the
ordinary status transitions, so a decision made from the queue and one made on
the artifact are the same write and land in the same history.

| Decision | Call |
|---|---|
| Approve | `PUT /api/v1/artifacts/{id}/status` with `{"status":"approved"}` |
| Send back | `POST /api/v1/chatter` with the reason, then `PUT …/status` with `{"status":"draft"}` |

A rejection is two calls, **in that order**. The reason is posted first: if
the note fails, nothing has been rejected and the reviewer can try again,
whereas the reverse order can leave an author with an artifact back in draft
and no word on what was wrong with it. The reason goes in as an ordinary note,
so `@name` mentions notify, `@@name` raises a to-do and `#REQ-12` cites,
exactly as they do from the notes panel.

### Notifications

Per-user, in-app (plus optional email — see `docs/operations.md`). SSE stream
pushes new items live.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/notifications` | List the caller's notifications (`?view=inbox\|flagged\|cleared&unread=true&limit=&before=<cursor>`) | user |
| POST | `/api/v1/notifications/read` | Mark specific notifications read | user |
| POST | `/api/v1/notifications/read-all` | Mark all read | user |
| POST | `/api/v1/notifications/clear` | Archive the caller's inbox into the cleared view | user |
| DELETE | `/api/v1/notifications/cleared` | Permanently delete what the caller has cleared (irreversible) | user |
| PUT | `/api/v1/notifications/{id}/flag` | Flag or unflag one of the caller's notifications | user |
| GET | `/api/v1/notifications/stream` | SSE stream of new notifications | user |
| GET | `/api/v1/me/notification-prefs` | Get the caller's email opt-out and push opt-in | user |
| PUT | `/api/v1/me/notification-prefs` | Update either preference (`email_notifications`, `push_notifications`); an absent field is left as it was | user |
| GET | `/api/v1/me/default-workspace` | The workspace the caller's sign-in lands in: `{org_id}`, `""` for the personal workspace (REQ-156) | user |
| PUT | `/api/v1/me/default-workspace` | Choose it: `{org_id}` names a workspace the caller belongs to (`404` otherwise, the same answer as an unknown one), `""` or the personal workspace's id means the personal workspace. A stable-channel workspace whose release predates the feature answers `403` with the remedy (`default-workspace` gate). The choice is re-checked against membership on every sign-in: a member who has left the workspace lands in their personal one | user |

#### Notification types

| Type | Fires on | Goes to |
|---|---|---|
| `run_failed` | An agent run finishes failed | Whoever launched it |
| `proposal_pending` | A proposal-mode agent write needs approval | Project editors and owners |
| `review_requested` | An artifact enters `in_review` | Project editors and owners |
| `interview_completed` | An interview participant finishes | Project editors and owners |
| `mention` | An `@name` in a comment | The mentioned project members |
| `budget_threshold` | Month-to-date spend crosses 80% or 100% | Workspace admins |
| `hosted_minutes` | The month's leased cloud runner minutes reach 80% or 100% of the plan's allowance | Workspace admins |
| `access_changed` | Your own workspace or project access changes | The affected member |
| `membership_changed` | Somebody joins, leaves, is invited, or changes role | Workspace admins |
| `release_published` | A server first boots on a new release (the top section of `RELEASE_NOTES.md`); `entity_ref.kind` is `release`, the title reads *OpenV version upgraded to 0.2.0* and the body carries the release's first bullets under their group headings. Or a stable release turns on for a workspace at its upgrade window, with the notes since the previous stable | Nightly: every account with a nightly-channel workspace, once per release (`release_announcements` claim). Stable: the workspace's members, once per workspace and release (`release_schedule` claim) |
| `release_scheduled` | A stable release is designated and will turn on for the workspace at its window, and again a day before it does | The workspace's admins |
| `release_support_window` | On a dedicated instance: a newer stable exists on the shared service and the 90-day support window is 30 or 7 days from closing, or has closed | Every workspace's admins on that instance |

`access_changed` and `membership_changed` are two audiences for the same
events, and are separate types because the reasons differ: one answers "what
can I do now?", the other is workspace governance. Nobody is notified about
their own action, and somebody who leaves voluntarily is not told they left
— only the admins are. An invitation to an address with no account notifies
the admins only; the invitation email is that person's notification. Taking
an invitation up notifies the admins the same way whichever door it came
through — a sign-up carrying the link (`POST /auth/register` with
`invite_token`), the link taken up signed in (`POST /auth/invitations/accept`)
or a single sign-on — and never the person who joined, whose action it was.
Project membership changes reach the affected member but **not** workspace
admins: project roles change constantly and would drown the arrivals and
departures that matter.

Both email by default (with everything else in `DefaultEmailTypes`, overridable
with `OPENV_EMAIL_NOTIFICATION_TYPES`), because an access change is exactly the
thing somebody needs to know while they are not looking at the app.

#### Where a notification leads, and what it says

The bell, the email's link and the web push's `url` open the same page for
a notification, chosen by its `entity_ref.kind`:

| `entity_ref.kind` | Opens |
|---|---|
| `run`, `proposal` | `/projects/<project_id>/agent-runs?run=<run_id>` (no `?run=` without a run id) |
| `interview` | `/projects/<project_id>/interviews` |
| `artifact` | `/projects/<project_id>/requirements` |
| `project_membership` | `/projects/<project_id>/settings?tab=members` |
| `membership` | `/org/settings?tab=members` |
| `org_usage` | `/org/settings?tab=usage` |
| `org_limits` | `/org/settings?tab=billing` |
| `release` | `/whats-new` |
| `support_window` | `/org/settings` |
| anything else | `/projects/<project_id>`, or `/projects` without a project |

The email's link is that path after `FRONTEND_URL`; the push's `url` is the
path alone. Bodies are plain text: a release's notes arrive with their
Markdown rendered as text, a review request quotes the artifact's title
exactly as written, and a support-window warning counts the days left
rounding up ("Upgrade OpenV within 2 days", "within 1 day", and "today"
with less than a day left). A push body longer than 200 characters is cut
after the last whole word or line that fits, with an ellipsis. An email's
subject outside ASCII is sent as RFC 2047 encoded words, and a line break
in any header value from data is sent as a space.

### Web push subscriptions

Per-device web push for the same high-signal types (REQ-109). Session cookie
only — run tokens and worker keys are refused — and every query is keyed on
the session's user id, so a member only ever sees or withdraws their own
devices. Off unless the server has a VAPID key pair (`docs/operations.md`).

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/me/push/config` | `{enabled, public_key}`; `enabled` is false when no VAPID keys are configured | user |
| GET | `/api/v1/me/push-subscriptions` | `{subscriptions: [...]}` — the caller's devices, never their encryption keys | user |
| POST | `/api/v1/me/push-subscriptions` | Register this device: `{endpoint, keys:{p256dh, auth}, user_agent?}` → 201. Idempotent on `endpoint`: re-posting refreshes the keys | user |
| DELETE | `/api/v1/me/push-subscriptions` | Withdraw a device: `{endpoint}` → 204 (204 too when nothing was there) | user |

`endpoint` must be an `https` URL on port 443, with no credentials, whose
host is a known push service: `fcm.googleapis.com`, `*.push.apple.com`,
`*.notify.windows.com`, `push.services.mozilla.com`,
`updates.push.services.mozilla.com` or `*.push.services.mozilla.com`, plus any
host listed in `OPENV_PUSH_ENDPOINT_HOSTS` (comma-separated, exact or
leading-wildcard, for a self-hosted push service — see `docs/operations.md`).
Anything else, including an address literal, is **400**; no name is resolved.

The 201 body is the **persisted** row, so re-posting a device already on file
answers with the same `id` and `created_at` that `GET
/api/v1/me/push-subscriptions` lists.

A push service that answers 404 or 410 deletes the subscription server-side;
any other failure stamps `failed_at` and keeps the row, which a later
successful send clears.

### Meta

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/meta/artifact-types` | Artifact type catalog | user |
| GET | `/api/v1/meta/link-types` | Link type catalog (see `docs/link-type-rules.md`) | user |

### Release

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/release` | The release this server runs, from the `RELEASE_NOTES.md` it was built with: `{version, date, notes: [...], categories: [{name, notes}], markdown, releases: [...]}` — `version` is the top section's semantic version (`0.2.0`; a date for the releases from before OpenV had version numbers; empty when the notes name none yet), `notes` its bullets flat, `categories` the same bullets grouped (`New features`, `Maintenance updates`, `Bug fixes`), `markdown` that section as written, and `releases` every release newest first in the same shape (a stable release carries `stable_since`), plus `stable` — the newest stable release `{version, since, previous, notes, categories}` with the notes of every release since the previous stable merged, or `null` — and `deployment` (`shared` or `dedicated`). The notes file itself is never served: it also holds what has not shipped yet. `Cache-Control: no-store`: open tabs poll it to notice a newer release and offer a reload | user |
| GET | `/api/v1/public/release` | The release feed dedicated instances poll: `{version, stable, stable_since}`, cacheable for five minutes | open |

### Share links and the open-source showcase

Project share links (REQ-149) and the projects an open-source workspace
publishes (REQ-151); `docs/sharing.md` explains both. Every path here is
open: a share token is the credential, so the lookups are throttled per
address on the invite-preview bucket and the token is redacted from the
request log. Every unusable link — unknown, revoked, expired — gets the
same `404`.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/public/share/{token}` | What the link shows: `{role, project: {id, name, description}, workspace, counts, snapshot?}`. A **public** link carries `snapshot`, the live project as a read-only export (artifacts, links; attachment metadata but no files), `Cache-Control: no-store`; a **reviewer** link carries the project's name only, since the holder signs in and uses the app | token |
| GET | `/api/v1/public/share/{token}/page` | The page a link unfurler reads: HTML whose Open Graph and Twitter-card tags name the project and point at the preview image, with a refresh into the app at `${FRONTEND_URL}/s/<token>` for a browser. The frontend's nginx serves `/share/<token>` from here | token |
| GET | `/api/v1/public/share/{token}/preview.png` | The 1200×630 preview card: project name, workspace, artifact counts, description; drawn on the API, cached 5 minutes | token |
| GET | `/api/v1/public/open-source/projects` | Every project of a workspace on the `open_source` plan that has a baseline: `[{project_id, name, description, workspace, baseline_id, baseline, snapshot_at, counts}]`, newest snapshot first, with the name and description the baseline recorded. A project with no baseline is not listed: what an open-source workspace publishes is its latest snapshot, never live work | open |
| GET | `/api/v1/public/open-source/projects/{id}` | That project's latest baseline in the share shape above, with `baseline` naming the snapshot and `project` carrying the name and description it recorded; the snapshot leaves out `linked_artifacts` (other projects' refs and titles). One `404` for a project that is private, unknown or unbaselined | open |
| GET | `/api/v1/public/open-source/projects/{id}/page` | Unfurl page for the project, as for a share link; the frontend serves `/open-source/p/<id>` from here and the app opens at `/open-source/<id>` | open |
| GET | `/api/v1/public/open-source/projects/{id}/preview.png` | Its preview card | open |

### Platform administration

The Platform admin page (account menu → Platform admin, REQ-155) for the
deployment's operators. Platform admins only: `401` signed out, `403` for
everybody else.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/admin/workspaces` | Every live workspace with its plan, channel and `members` count, oldest first | platform admin |
| GET | `/api/v1/admin/users` | Every account: `[{id, name, email, auth_provider, is_admin, created_at}]`, admins first | platform admin |
| POST | `/api/v1/admin/users/{id}/password-reset` | Mint a password reset link for an account and answer it **once**: `{link, expires_at}`, valid 24 hours, works once (REQ-158). Nothing is emailed: the admin passes the link on however they talk to the person, which is the support path and the only path on a deployment with no mailer. Following it verifies nothing about the mailbox. `404` unknown account; `409 no_password` for an account that signs in through an identity provider. Logged with the admin's id | platform admin |
| PUT | `/api/v1/admin/users/{id}/admin` | Grant or remove platform-admin standing `{is_admin}` → the account. `400` when an admin tries to remove their own standing or the last admin's; `404` for an unknown account | platform admin |

Plans are changed with `PUT /api/v1/orgs/{id}/plan` (above).

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
- Votes are per person too, and the same gate applies: a session user votes,
  an agent-run token or a runner key cannot. Voting is idempotent on both
  sides — hence `PUT` / `DELETE` rather than `POST`.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/shared-products` | List the visible community pool | user |
| POST | `/api/v1/shared-products` | Share a product with every workspace | org member (session only) |
| POST | `/api/v1/shared-products/{id}/report` | Flag an entry for review | user (session only) |
| PUT | `/api/v1/shared-products/{id}/vote` | Vote for an entry | user (session only) |
| DELETE | `/api/v1/shared-products/{id}/vote` | Withdraw your vote | user (session only) |
| DELETE | `/api/v1/shared-products/{id}` | Remove an entry outright | platform admin |

`GET /api/v1/shared-products` takes `?limit=` (default 200, max 500) and
`?sort=`:

| `sort` | Order | Rows |
|---|---|---|
| `recent` (default) | newest first | every visible entry |
| `top` | most votes first, then newest | only entries with at least one vote |
| `top_week` | most votes in the last 7 days first, then newest | only entries voted for in that window |

Any other `sort` is `400`. Every row carries `votes` (all time), `votes_week`
(votes inside the rolling seven-day window, counted by the database against
its own clock) and `voted` — *your* vote, so a caller with no session user
(a workspace runner key) always reads `false`.

Both vote endpoints answer `200` with `{"votes": N, "votes_week": N,
"voted": true|false}`, and `404` for an id that is unknown *or* hidden: a
hidden entry is out of every list and cannot be voted for either.

### Product profile, V&V, test runs

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/profile` | Product profile (vision, users, constraints) | viewer |
| PUT | `/api/v1/projects/{id}/profile` | Update product profile | editor |
| POST | `/api/v1/projects/{id}/test-runs` | Create test run. A `baseline_id` names one of the project's baselines: one no row has, a deleted one, a malformed id or another project's answers `404` `baseline not found`, after the project guard and the body's decode, and no run is created | editor |
| GET | `/api/v1/projects/{id}/test-runs` | List test runs. A run whose `baseline_id` names no baseline of its project, as when the baseline was deleted after the run named it (a baseline delete goes ahead, and the run keeps the id as history, REQ-13), carries `baseline_deleted: true`; the field is left out when the baseline exists or no baseline is named. It is read from the baselines on every read, as in every answer below that holds a run | viewer |
| GET | `/api/v1/test-runs/{id}` | Test run details, `baseline_deleted` as in the list | viewer |
| PUT | `/api/v1/test-runs/{id}` | Update test run: `{status}`, `completed` or `aborted`, from `in-progress`; the answer marks `baseline_deleted` as the list does. No route changes a run's `baseline_id` | editor |
| DELETE | `/api/v1/test-runs/{id}` | Delete a test run that holds no result. `409` for one that holds results, which are kept (REQ-13): `a test run that holds results is kept: complete or abort it instead of deleting it` for a run in progress, and `a test run that holds results is kept: this one is already completed` (or `aborted`) for a closed one | editor |
| POST | `/api/v1/test-runs/{id}/results` | Record a test result `{test_case_id, status, notes?, evidence?}`. Recording a case again adds a result with an id of its own rather than overwriting the earlier one, which stays in the run's history; the one recorded last is the case's current result wherever a result is read, its times always after those of the result it supersedes, an omitted `evidence` carries the current one's, and the current one's evidence citations move to it (REQ-13, REQ-121). `404` `artifact not found` for a `test_case_id` no artifact of the run's project has; `409` `this test run is completed; only in-progress runs accept new results` (or `aborted`) for a closed run | editor |
| POST | `/api/v1/test-runs/{id}/agent-run` | `{agent_slug, test_case_ids?}`: launch an agent on the run's agent-executable cases. Refused `403` for a proposal-mode agent run, and `409` for a closed run, before the body is read, as a result there is: `this test run is completed; only in-progress runs accept new results` (or `aborted`) | editor |
| GET | `/api/v1/test-runs/{id}/results` | The run's current result per test case, the latest recorded first; `?history=true` lists every result recorded in the run, those later results superseded included, newest first | viewer |
| GET | `/api/v1/test-runs/{id}/citations` | Evidence cited across the run, keyed by test result id | viewer |
| GET | `/api/v1/projects/{id}/vv/coverage` | Verification coverage summary. A requirement refined by requirements of child projects carries them as `refinements` (each with its own rollup in its project), `flow_down` (the worst of them) and, when it has no evidence of its own, takes the flow-down as its `rollup` with `via_refinements` set (REQ-146) | viewer |
| GET | `/api/v1/projects/{id}/vv/matrix` | Traceability matrix | viewer |
| GET | `/api/v1/projects/{id}/vv/gaps` | Coverage gaps | viewer |
| GET | `/api/v1/projects/{id}/vv/report` | V&V report PDF: the coverage and gaps `vv/coverage` and `vv/gaps` answer, flow-down included (REQ-146), and the test runs | viewer |

### Workspace limits

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/orgs/{id}/limits` | Every limit the workspace is subject to, with usage where countable | member |

Any member may read it: what the workspace allows is not privileged, and
hiding it only produces a surprise at the moment somebody is refused. The
response carries the plan, whether the deployment is self-hosted (which
decides the remedy offered), and one entry per limit:

```json
{
  "key": "max_members",
  "label": "Workspace members",
  "description": "How many people can be in this workspace. Pending invitations count towards it.",
  "unit": "count",
  "limit": 25,
  "unlimited": false,
  "used": 7
}
```

`unlimited` is stated rather than implied, so a client never has to know that
`0` is special. `used` is absent for limits whose usage cannot be counted.
`fixed` marks a ceiling nothing raises — no plan, no setting — so a client can
show it as a fact rather than as a warning that the workspace is full. A
**personal workspace** reports `max_members` as `{"limit": 1, "fixed": true}`
whatever its plan says: it is one person's by definition, and adding anybody to
it is refused by `POST /orgs/{id}/members` and `/invitations` with `400`, not
as a limit refusal.

**When a limit stops a call**, the answer is `403` with
`"code": "limit_reached"` and the arithmetic attached:

```json
{
  "error": "Workspace members: this workspace allows 5 and already has 5 (including invitations not yet accepted). Upgrade the workspace's plan to raise this limit, or ask a workspace admin to.",
  "code": "limit_reached",
  "limit": "max_members",
  "label": "Workspace members",
  "used": 5,
  "allowed": 5,
  "remedy": "Upgrade the workspace's plan to raise this limit, or ask a workspace admin to."
}
```

`error` is self-sufficient — a client that shows only that string still tells
the person what stopped them and what to do. `remedy` is the same sentence on
its own for clients that want to present it separately, and it differs by
deployment: a self-hosted instance names the setting to change rather than
offering a plan upgrade.

`403` rather than `402`: Payment Required would be wrong on a deployment with
no plan and nobody to pay, and one code has to serve both. Evidence uploads
are the exception, answering `413` because there the refusal is about the size
of the request.

Limits are enforced when creating a shared workspace, bringing somebody into
one (pending invitations count towards the seat total), and creating a
project. `docs/operations.md` covers how an operator sets them.


### Billing

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/public/plans` | The plans for sale with their confirmed amounts per currency | open |
| GET | `/api/v1/orgs/{id}/billing` | The workspace's billed plan, entitled plan and subscription snapshot | admin |
| POST | `/api/v1/orgs/{id}/billing/refresh` | Re-read the workspace's subscription from the provider now; `{"session_id"}` binds a just-completed checkout | admin, rate limited |
| POST | `/api/v1/orgs/{id}/billing/checkout` | `{"plan","interval","currency"}` → `{"url"}`, the provider's checkout page | admin, rate limited, channel gated |
| POST | `/api/v1/orgs/{id}/billing/change` | `{"plan","interval"}` → the state; moves the live subscription in place, prorated | admin, rate limited |
| POST | `/api/v1/orgs/{id}/billing/portal` | `{"url"}`, the provider's self-service portal (card, address, VAT number, invoices, cancel) | admin, rate limited |

Billing is an optional module (`docs/plans/billing-stripe.md`). With no
provider configured — every self-hosted deployment — the public catalogue
answers `{"billing_enabled": false, "plans": []}` and the workspace routes
answer `404` with `code: "billing_unavailable"`. The routes are registered
either way, so the surface is the same on every deployment.

The catalogue is served from the last reading the reconcile job confirmed
against the provider, never from a live call: an open endpoint cannot be
made to spend the provider's rate limit, and an outage never blanks the
pricing page. Amounts are in the currency's minor unit (pence, cents):

```json
{
  "billing_enabled": true,
  "as_of": "2026-09-21T22:00:00Z",
  "currencies": ["eur", "gbp", "usd"],
  "plans": [
    {"plan": "business", "per_seat": true,
     "intervals": {"month": {"amounts": {"gbp": 1200, "usd": 1500, "eur": 1400}, "tax_behavior": "exclusive"}}}
  ]
}
```

The workspace state carries `plan` (billed), `entitled_plan` (what the
limits resolve from — the free tier once a subscription lapses), `granted`
(a platform admin's plan, nothing to buy) and `billing` (status, interval,
seats, period end, cancel-at-period-end, grandfathered, synced-at). Provider
object ids never reach a client. A refresh reads from the provider and can
grant nothing it does not hold; a provider failure is `503` with
`code: "billing_upstream"` and `Retry-After`, the workspace left as it was.
A `session_id` the provider does not know is its answer, not a failure:
`404` *checkout not found*, with no `Retry-After`, the workspace left as it
was.
The limits response likewise carries `entitled_plan`, `plan_status` and
`grandfathered`, so a member can see there is a payment problem without
seeing anything about money. Each flag (`hosted_automation`, `teams`,
`workspace_budget`) is listed with `kind: "flag"` and `included`, and
`hosted_runner_minutes_month` with the month's leased minutes as `used`.

**Read-only over plan.** `read_only` is true, and `over_plan` names the
limits, while a workspace holds more than its plan allows (more members
than `max_members`, more projects than `max_projects`) — after a lapsed
subscription, say. Every mutating request scoped to that workspace or its
projects then answers `403` with `code: "plan_read_only"`, `over` and
`remedy`, except the sixteen writes registered `alwaysWritable`, which
answer as on a writable workspace: the writes that bring it back under
plan or out (removing a member or leaving, revoking an invitation,
deleting a project or the workspace), the billing endpoints, and import;
three revocations, which only take access away
(`DELETE /orgs/{id}/worker-keys/{keyId}`,
`DELETE /orgs/{id}/my-runner-key`, `DELETE /share-links/{id}`); three
writes that touch only the caller's own session or lease
(`POST /orgs/{id}/activate`, `PUT /orgs/{id}/members/me/preview`,
`DELETE /orgs/{id}/runner-session`); and `POST /agent-runs/{id}/cancel`,
for its launcher, the project's editors and, for a run in no project, the
workspace's admins alike. The remedy, like a
`limit_reached` one, suits the deployment: the Billing tab where billing
exists, and on a self-hosted deployment the `OPENV_LIMITS` settings to
raise, the `error` there naming the deployment's limit, not the plan's.
Reads, and export in every format, are never refused in any state. A flag
the plan does not include is refused with `403 limit_reached` naming the
flag, at: creating a hosted runner and a hosted worker's run claim
(`hosted_automation`; a claim by the member's own Agent Connector is never
gated), creating a people-team and granting a team on a project
(`teams`), and the workspace usage rollup and budget (`workspace_budget`).
A cloud-runner lease is cut to the month's remaining
`hosted_runner_minutes_month` and refused with `limit_reached` once none
is left.

A purchase is a redirect: `checkout` answers with a page of the provider's
and the browser returns to the Billing tab with `?checkout=done&session_id=`,
which the tab hands to `refresh`; the platform verifies the session belongs
to that workspace (`403 checkout_mismatch` otherwise), records the
subscription, and marks the buyer's one trial used. `checkout` answers `400
unknown_plan` for a plan, interval or currency not on sale, or a currency
other than the one the workspace's first purchase fixed; `409
already_subscribed` while a subscription is live (as a platform admin's plan
grant over it is), `409 granted_plan` where
a platform admin set the plan, and `400` for `business` on a personal
workspace. Where two admins complete two checkouts, the second is cancelled
at bind and the tab says so. `change` keeps one subscription per workspace
(`409 no_subscription` without one): `business_lite` bills a quantity of
one, `business` the workspace's seats. `portal` needs a customer record
(`409 no_customer`). Writes are limited to a burst of 5 then 20 an hour per
workspace (`OPENV_BILLING_WRITE_BURST` / `_REFILL_PER_HOUR`).
### Evidence bundles

A bundle is one physical or manual capture session — what was done, when, by
whom, under what conditions, and the files it produced. It belongs to the
project rather than to a run, because one session commonly answers several
test cases at once: a single 90-minute noise sweep across idle, half and full
load is the evidence for all three. Results **cite** a bundle; the same bundle
may be cited by any number of results, in this run and in later ones.

A bundle carries a citable reference (`EVD-1`), unique within the project and
never reissued, and may have no files at all — for an inspection or a
demonstration, a written account is the evidence.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/evidence-bundles` | List bundles, newest capture first | viewer |
| POST | `/api/v1/projects/{id}/evidence-bundles` | Record a capture session | editor |
| GET | `/api/v1/evidence-bundles/{id}` | One bundle with its files and the results citing it | viewer |
| PUT | `/api/v1/evidence-bundles/{id}` | Edit a bundle (the `ref` and project are fixed) | editor |
| DELETE | `/api/v1/evidence-bundles/{id}` | Delete a bundle, its files and its citations | editor |
| POST | `/api/v1/evidence-bundles/{id}/files` | Upload one file (multipart, streamed) | editor |
| GET | `/api/v1/evidence-files/{id}/download` | Download a file | viewer |
| DELETE | `/api/v1/evidence-files/{id}` | Delete one file | editor |
| GET | `/api/v1/test-results/{id}/citations` | The bundles a result rests on | viewer |
| POST | `/api/v1/test-results/{id}/citations` | Cite a bundle (`{"bundle_id", "note"}`) | editor |
| DELETE | `/api/v1/test-results/{id}/citations/{bundleId}` | Drop a citation; the bundle is untouched | editor |

**Uploads** are a separate path from artifact figures, and the rules differ.
Any file type is accepted; the per-file cap is `OPENV_MAX_EVIDENCE_MB`
(default 200), distinct from the figure cap `OPENV_MAX_UPLOAD_MB` (25). The
body is streamed to disk rather than buffered, and its SHA-256 is recorded at
upload.

**Downloads are never rendered.** Evidence may be any format, including ones
that carry script, so every file is served `application/octet-stream` with
`Content-Disposition: attachment`, `X-Content-Type-Options: nosniff` and a
`default-src 'none'` policy. The recorded digest is returned in
`X-Evidence-SHA256` so a downloader can check the bytes against the record.

**Storage** is capped per workspace by the `evidence_storage_mb` org limit
(free 2048, team 20480), because uploads share one volume with the rest of
the deployment. An upload that would exceed it is refused with `413` and a
message naming the current usage. Citing a bundle twice from one result is
accepted quietly rather than refused — it is the state the caller asked for —
and answers `201` with the citation already stored: its `id`, `created_at`
and `note`, which the repeat leaves as they were.

`vv/gaps` returns one list of artifact IDs per bucket:

| Bucket | Contents |
|---|---|
| `requirements_without_method` | No `verification_method` attribute set |
| `requirements_without_test_case` | Method is `test` but no test case verifies it |
| `requirements_unverified` | Method is set to any method other than `test` (`demonstration`, `analysis`, `inspection` or any other value the project uses) and it is not yet marked verified — no test case can cover these, so they would otherwise be invisible here even though the coverage rollup already counts them as `uncovered` |
| `requirements_failing` | Latest result of a verifying test case is a fail |
| `orphan_test_cases` | Test cases that verify nothing |
| `needs_without_requirement` | User needs no requirement derives from |
| `hazards_unmitigated` | Hazards no design item mitigates |

`requirements_unverified` is derived from the same rollup `vv/coverage`
computes, so the two views cannot disagree about a requirement.

### Requirement quality

Advisory linting of requirement wording. `quality-rules` names the project's
normative convention (`shall` — ISO/IEC/IEEE 29148 — or `rfc2119`) and the
severity of each rule; a project inherits its workspace's rules until it sets
its own, and an empty PUT body clears the override. Reports carry the rule set
they were produced under, since the same sentence scores differently between
conventions.

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/quality` | Lint every requirement/user need (`?baseline_id=` lints a snapshot). A citation is judged by the rule `/artifacts/{id}/quality` applies (REQ-164): one of any artifact the requirement is linked to, in either direction and in whichever project, counts as linked; the report names another project's artifact from the export's `linked_artifacts` | viewer |
| GET | `/api/v1/artifacts/{id}/quality` | Lint one artifact (400 for a type the linter does not judge) | viewer |
| GET | `/api/v1/projects/{id}/quality-rules` | Resolved rules + both levels' overrides + the editor catalog | viewer |
| PUT | `/api/v1/projects/{id}/quality-rules` | Set or clear the project's override | editor |

### Work items (kanban)

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/projects/{id}/work-items` | Create card. `source_chatter_id` raises it from a note (that note must be in the same project, else 400). `assignee_type` is `user` (the default), `agent` or `team` (a crew), else 400 `invalid assignee_type: must be user, agent or team`; a crew's `assignee_id` must name a crew the caller may know of (*Crew writes*), else 404 `team not found`, and one of the project's workspace, else 400 `team belongs to a different workspace` | editor |
| GET | `/api/v1/projects/{id}/work-items` | List board: the columns in the order the board flows in (`backlog`, `todo`, `in-progress`, `review`, `done`), then `sort_order`, then `created_at` | viewer |
| GET | `/api/v1/work-items/{id}` | Card + activity | viewer |
| PUT | `/api/v1/work-items/{id}` | Edit card. `assignee_type` and a crew's `assignee_id` are checked as on create; an omitted `assignee_type` keeps the card's, and the `assignee_id` sent is checked against it. An update replaces `assignee_id` (omitted, it clears it), and one that re-sends the card's own assignee, the same type and id, is not checked again | editor |
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
| POST | `/api/v1/guided-sessions/{id}/commit` | Commit session: each of its drafts is approved through the review states (`draft` → `in_review` → `approved`, a version and a `status-change` note per step), and the session closes. Each approval publishes the `artifact.status_changed` event a status change does, with the committing user as actor and the session id as `guided_session`; the step into review publishes none, so editors are not asked to review it. Refused `403` for a proposal-mode agent run, like a status change | editor |
| POST | `/api/v1/guided-sessions/{id}/abandon` | Abandon session | editor |
| GET | `/api/v1/guided-sessions/{id}/messages` | Assistant chat history | viewer |
| POST | `/api/v1/guided-sessions/{id}/messages` | Send a chat message (launches an assistant turn; optional `artifact_id`). Refused `403` for a proposal-mode agent run, as are kickoff and nudge | editor |
| POST | `/api/v1/guided-sessions/{id}/chat/kickoff` | First assistant greeting (optional `artifact_id`) | editor |
| POST | `/api/v1/guided-sessions/{id}/chat/nudge` | Context nudge after step change | editor |
| GET | `/api/v1/guided-sessions/{id}/chat/stream` | SSE stream of assistant replies | viewer |

`chat/nudge` takes `{step, state, event}` and answers `{status, runner_online}`.
`status` is `launched` (a turn was enqueued), `pending` (a turn is already in
flight, so this nudge was **parked** on the session — the newest parked nudge
wins, and the finishing turn launches exactly one more from it) or
`unavailable` (no turn is coming). Both `launched` and `pending` mean a reply
will arrive on the stream. If the in-flight turn finishes while the nudge is
being parked, the request takes it back and launches it itself, answering
`launched`: a nudge is never left waiting for a turn that has already gone
looking for one. Committing or abandoning a session discards any nudge still
parked on it.

The chat streams (`chat/stream` and the public interview stream) carry two
event types: `message`, one complete transcript message, and
`assistant_partial`, `{run_id, text}` — the answer **so far** while the agent
writes it, always the whole text rather than a delta, sent at most once per
500 ms per run. A client renders it as an in-progress bubble and replaces it
when the next `message` arrives.

### Interviews

| Method | Path | Purpose | Auth |
|---|---|---|---|
| POST | `/api/v1/projects/{id}/interviews` | Create interview; an optional `persona_artifact_id` is checked as `PUT /interviews/{id}/persona` checks it. Refused `403` for a proposal-mode agent run, since each participant message launches the interviewer's run | editor |
| GET | `/api/v1/projects/{id}/interviews` | List interviews | viewer |
| POST | `/api/v1/interviews/{id}/close` | Close interview | editor |
| PUT | `/api/v1/interviews/{id}/persona` | Link/unlink a persona artifact (`persona_artifact_id`: `400` for an artifact of another project or one that is no persona, `400` `persona artifact not found` for one no row has or in a project the caller cannot reach) | editor |
| POST | `/api/v1/interviews/{id}/invites` | Mint an invite link. Refused `403` for a proposal-mode agent run, as is the interview | editor |
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
| DELETE | `/api/v1/agents/{slug}` | Delete agent; `404` for a slug no agent of the workspace has, a deleted one's among them | org admin |
| GET | `/api/v1/agents/{slug}/raw` | Raw markdown (frontmatter + prompt) | user |
| PUT | `/api/v1/agents/{slug}/raw` | Save raw markdown | org admin |
| POST | `/api/v1/agents/{slug}/runs` | Launch a run of this agent: in the body's `project_id`, or, with none, in the agent's workspace, where a person must be a member (a worker key or run token of that workspace launches there), past the workspace's read-only gate either way. Refused `403` for a proposal-mode agent run | editor (project-scoped) / org member |
| POST | `/api/v1/projects/{id}/draft-test-cases` | `{requirement_ids}`: launch the seeded test-case author on them, in proposal mode. Refused `403` for a proposal-mode agent run, like a status change: a launch is no write a proposal can carry | editor |

**`allowed_tools` is required.** `POST /api/v1/agents` and
`PUT /api/v1/agents/{slug}` answer **400** when the definition carries no tool
allowlist — absent, `[]`, or only blank entries — with:

```json
{"error":"agent definition requires allowed_tools: every agent must name the tools its vendor CLI may use (e.g. mcp__openv__*), because a CLI started with no allowlist runs with all of them"}
```

`PUT /api/v1/agents/{slug}/raw` refuses the same content the same way, since
the frontmatter goes through the same validation. An empty list is not "no
tools": a vendor CLI started without an allowlist runs with every tool it has,
so the platform will not store an agent that has none, and the runner fails
such a run before launching anything (`docs/agents.md`, *Tools an agent may
use*). Definitions already on disk from before this rule are backfilled to
`mcp__openv__*` when they sync, not rejected.

A **non-empty** allowlist is never a reason to refuse, whichever provider the
agent names. Claude Code takes it verbatim (`--allowedTools`); gemini-cli has
it translated into the settings that CLI documents for restricting tools
(`tools.core`, and the openv server's `includeTools`); codex-cli, which has no
allowlist mechanism at all, is confined by its sandbox instead. In every case
the OpenV MCP server is additionally handed `OPENV_MCP_TOOLS` and serves only
the `mcp__openv__*` tools the definition names, so the allowlist is enforced
for OpenV's own tools regardless of what the vendor CLI can express.

### Agent runs

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/agent-runs` | List runs, newest first (project-scoped: viewer, the project's runs whatever workspace the caller acts in; workspace-wide: org admin, members see their own). `project=none` keeps the workspace-wide listing's runs that have no project (a whole-workspace automation's, one launched outside any project, one its project's delete left behind), for the workspace Runs page: an org admin gets them all, a member the ones they launched, as `GET /agent-runs/{id}` lets each read them. `400` for any other `project` value, or `project=none` beside `project_id` | user |
| POST | `/api/v1/agent-runs/claim` | Worker claims the next eligible queued run (a personal key: its owner's, and the ownerless ones its owner could see), never one whose cancel was requested. `204` when there is none, and when the run claimed was asked to stop before its token was issued (a project's delete, say): it is handed back, which ends it cancelled | worker |
| POST | `/api/v1/agent-runs/delegate` | Running crew agent delegates to a child agent; `404` when the run's crew node was removed after it launched | run |
| GET | `/api/v1/agent-runs/delegate/{id}` | Delegation status: `403` `not your delegated run` for another run of the caller's project, `404` `agent run not found` for a run outside it, as for one no row has | run |
| GET | `/api/v1/agent-runs/{id}` | Run details | launcher / viewer |
| GET | `/api/v1/agent-runs/{id}/tree` | Run + child-run tree, less each run below the root the caller could not open by itself and the runs below it | launcher / viewer |
| GET | `/api/v1/agent-runs/{id}/logs` | Run log entries: the worker's, then any note the server keeps on the finished run (`kind` `marker`, below) | launcher / viewer |
| POST | `/api/v1/agent-runs/{id}/logs` | Worker appends log entries (returns cancel flag) | worker |
| GET | `/api/v1/agent-runs/{id}/stream` | SSE live log stream | launcher / viewer |

The logs body is `{"entries": [...], "partial_text": "..."}`; a bare array of
entries is still accepted (older runners). `partial_text` is the assistant
answer written so far — the whole text, not a delta, capped at 64 KB — and an
empty string means "unchanged". It is stored on the run as `partial_text`,
returned with the run, cleared whenever the run stops being live — at finish
(`final_text` takes over), when the stale-run reaper fails it, and when a
worker releases it back to the queue — and broadcast as `partial` on the run's
own stream and as `assistant_partial` on any session the run belongs to.
| POST | `/api/v1/agent-runs/{id}/cancel` | Request cancellation | launcher / editor |
| POST | `/api/v1/agent-runs/{id}/retry` | Re-enqueue a failed, cancelled or timed-out run as a new one (`retried_from_run_id`); `409` otherwise. Refused `403` for a proposal-mode agent run, `401` for any other run token | launcher / editor |
| POST | `/api/v1/agent-runs/{id}/start` | Worker marks run running | worker |
| POST | `/api/v1/agent-runs/{id}/finish` | Worker reports completion of a claimed or running run; `409` for a run no worker holds (queued: never claimed, or released back) or one already finished | worker |
| POST | `/api/v1/agent-runs/{id}/release` | `{worker_id}`: the worker holding a claimed or running run hands it back to the queue (a worker shutting down); the run's token is revoked with it. A run whose cancel was requested is not queued again: it ends `cancelled`. `204` also when nothing was released | worker |

A run that finishes with proposals pending review waits in
`awaiting_approval` until its last proposal is reviewed, then succeeds, or
fails when an approved proposal could not be applied: its `error` is then
`one or more approved proposals failed to apply` and its `error_class`
`agent_error`, which is never retried automatically; a member may still
retry it (REQ-79, REQ-84). Deleting its project cancels it instead, its
proposals gone with the project. A run whose cancel was requested is never
retried automatically either, however it ends.

When a crew run succeeds (for one awaiting approval, once it is finalised),
each `hands-off-to` and `reviews` edge of its crew node starts what it leads
to: an agent's run, or, for a person, a card on the run's project board
assigned to them. That card goes only to an admin of the project's
workspace, or a member of it with a role in the project, directly or
through a people team. That is stricter than what the person's own session
opens: a platform admin who is neither, and someone who has left the
workspace but kept a role in the project, are refused too. Anyone refused
is granted nothing: no card is made, and the run keeps a note saying why,
which its card's activity repeats (`run-failed`) (REQ-23, REQ-81). The
reason names the rule that refused: a member of the workspace with no role
in the project "has no role in this project", to be given one, and anyone
outside the workspace, such as a former member who kept a role in the
project, "is no longer a member of this workspace", to be added back to it
with a role in the project; a check that fails says the person's access
"could not be checked".
When the workspace's budget refuses the agent successors
(`OPENV_BUDGET_ENFORCE`), none is launched: the run publishes
`agentrun.successors_skipped`, with `agent_id`, `team_id`, `successors` (the
skipped nodes' labels), `team_node_ids` and `reason` (the budget refusal,
which names the budget and the month's spend), and keeps a note saying the
same (REQ-76); a person's hand-off, which starts no run, is still made. A
note is an entry the server appends at the end of the finished run's log,
`kind` `marker`, whose payload carries `marker` (`handoff_refused` or
`successors_skipped`), `message`, the sentence the run panel shows, and the
detail: `team_node_id`, `user_id` and `edge_type` for a refused hand-off,
`successors`, `team_node_ids` and `reason` for skipped successors.

### Crews (agent org charts) — canonical

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/crews` | List crews | user |
| POST | `/api/v1/crews` | Create crew | editor (project-pinned) / org admin |
| GET | `/api/v1/crews/{id}` | Crew graph (nodes + edges) | org member |
| PUT | `/api/v1/crews/{id}` | Update crew | editor / org admin |
| DELETE | `/api/v1/crews/{id}` | Delete crew | editor / org admin |
| POST | `/api/v1/crews/{id}/clone` | Clone crew (the source's write guard, then the copy's: `project_id` a project of the crew's workspace the caller edits, or none for an org admin) | editor / org admin |
| POST | `/api/v1/crews/{id}/nodes` | Add node (agent or human) | editor / org admin |
| POST | `/api/v1/crews/{id}/runs` | Launch a run at the crew's entry node (in the body's `project_id`, else in a pinned crew's project); `400` for a project outside the crew's workspace; `404` `team not found` for a caller who may not know of the crew (*Crew writes*). A pin that no longer names a project of the crew's workspace counts as none, for launches and for every crew write. Refused `403` for a proposal-mode agent run | editor / org admin |
| PUT | `/api/v1/crew-nodes/{id}` | Update node | editor / org admin |
| DELETE | `/api/v1/crew-nodes/{id}` | Remove node | editor / org admin |
| POST | `/api/v1/crews/{id}/edges` | Add edge (delegates-to, hands-off-to, reviews) | editor / org admin |
| PUT | `/api/v1/crew-edges/{id}` | Update edge | editor / org admin |
| DELETE | `/api/v1/crew-edges/{id}` | Remove edge | editor / org admin |

**Deprecated aliases** (same handlers, kept for compatibility — see
`registerCrewRoutes` in `internal/api/crew_handlers.go`): `/api/v1/teams`, `/api/v1/teams/{id}`,
`/api/v1/teams/{id}/clone|nodes|runs|edges`, `/api/v1/team-nodes/{id}`,
`/api/v1/team-edges/{id}`. New integrations should use the `/crews` forms.

### Automations & proposals

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/automations` | List automations | user |
| POST | `/api/v1/automations` | Create (manual / cron / event-triggered). `created_by` is the caller, whatever the body says. `project_id` pins it to a project of the workspace (`400` "project does not belong to this workspace" otherwise); absent, null or `""`, it covers the whole workspace. A triggered automation fires only on events of its own workspace, and of its own project when pinned to one; a workspace-wide one's run takes the event's project. The target must be the workspace's: an agent of it, or a crew of it pinned to no project or to the automation's own (`400` "agent not found", "crew not found", "the crew is pinned to another project", or, for a workspace-wide one, "a crew pinned to a project cannot run an automation for the whole workspace"). Cron expressions are read in UTC | editor (project-pinned) / org admin |
| GET | `/api/v1/automations/{id}` | Details | org member |
| PUT | `/api/v1/automations/{id}` | Update; null fields are left as they are. `project_id` moves it: to a project, or with `""` to the whole workspace, which takes the write rights of where it goes too and the `workspace-automations` feature (`403` with the feature gate's message before a stable-channel workspace has it). The target is checked as on create when the scope or the target changes | editor / org admin (and of the scope it moves to) |
| DELETE | `/api/v1/automations/{id}` | Delete | editor / org admin |
| POST | `/api/v1/automations/{id}/run-now` | Launch immediately. Refused `403` for a proposal-mode agent run, `401` for any other run token | editor / org admin |
| GET | `/api/v1/proposals` | List agent proposals, newest first, at most 500: `?project_id=` (viewer), or without it the active workspace's (org admin; anyone else `400` `project_id is required`); `status`, `run_id` narrow either | viewer (project) / org admin |
| POST | `/api/v1/proposals/{id}/approve` | Apply a proposed write; a run token is refused (`403`) | editor |
| POST | `/api/v1/proposals/{id}/reject` | Reject it; a run token is refused (`403`) | editor |

### Repo connections & provider settings

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/projects/{id}/repo-connections` | List connected repositories, each with the caller's local path (a run token: its claimant's) | viewer |
| POST | `/api/v1/projects/{id}/repo-connections` | Connect a repository | owner |
| PUT | `/api/v1/repo-connections/{id}` | Update connection | owner |
| DELETE | `/api/v1/repo-connections/{id}` | Remove connection | owner |
| PUT | `/api/v1/repo-connections/{id}/my-path` | Set my machine's checkout path | viewer |
| GET | `/api/v1/provider-settings` | Provider status + model catalog per provider | user |
| PUT | `/api/v1/provider-settings` | Update provider config (auth mode, default model); answers the stored row, whose `id` an update keeps | org admin |
| POST | `/api/v1/provider-settings/detect` | Worker reports detected CLIs/logins/models, `{provider: {...}}` for each of its adapters. Every provider the server knows is recorded, whatever else the report names; one it does not know answers `400` naming it (the first by name), the known ones recorded all the same | worker |
| POST | `/api/v1/provider-logins` | Start a CLI sign-in relay (workspace: org admin; user-targeted: org member) | user |
| POST | `/api/v1/provider-logins/claim` | Worker claims a pending sign-in | worker |
| GET | `/api/v1/provider-logins/{id}` | Sign-in status (redacted) | user |
| POST | `/api/v1/provider-logins/{id}/code` | Submit the pasted authorization code (workspace: org admin; user-targeted: its requester or an org admin) | user |
| POST | `/api/v1/provider-logins/{id}/cancel` | Cancel sign-in (workspace: org admin; user-targeted: its requester or an org admin) | user |
| POST | `/api/v1/provider-logins/{id}/progress` | Worker reports flow progress; a completed or cancelled sign-in is answered as it is, unchanged | worker |
| GET | `/api/v1/provider-logins/{id}/full` | Full login record (incl. code) for the executing worker | worker |

### Events

| Method | Path | Purpose | Auth |
|---|---|---|---|
| GET | `/api/v1/events` | Domain event feed (`?project_id=` viewer, the project's events whatever workspace the caller acts in; workspace-wide: org admin) | user |

Each event carries its stored audit fields (`actor` — `user:<id>`,
`agent:<run id>`, `worker:<org>[:user:<id>]` or `system` — plus `entity_id`)
and, alongside them, the names those IDs stand for, resolved server-side for
the events the caller may see: `actor_kind`, `actor_id`, `actor_name`, and
`entity_kind` / `entity_name` (the artifact title, work item title, baseline
name, agent name, ...). A name is omitted when the row behind the ID is gone,
so clients must fall back to the raw IDs.

## Throttling

Sign-in (`POST /auth/login`), registration (`POST /auth/register`) and the
Google and OIDC start and callback routes are throttled per client address,
and sign-in additionally per account on failed attempts; the public interview
routes are throttled per invite and per address. A throttled request is
answered `429` with a JSON `error` and a `Retry-After` header in seconds.
Verification resend and change-of-address are throttled per account
(`OPENV_VERIFY_RESEND_BURST` 3, `OPENV_VERIFY_RESEND_REFILL_PER_HOUR` 6).
Asking for a password reset email (`POST /auth/password-reset`) is throttled
per client address on the sign-in bucket and per address asked for
(`OPENV_PASSWORD_RESET_BURST` 3, `OPENV_PASSWORD_RESET_REFILL_PER_HOUR` 6),
whether or not that address has an account, so the bucket cannot be read
for existence either; confirming a link draws on the sign-in bucket per
client address only, since the token itself is unguessable.
Previewing an invite link (`POST /auth/invitations/preview`) has its own
generous per-address bucket (`OPENV_INVITE_PREVIEW_BURST` 60,
`OPENV_INVITE_PREVIEW_REFILL_PER_HOUR` 240) and deliberately does **not**
draw on the sign-in one: the token is unguessable, so the limit only bounds
lookups, and opening an invite link must never cost somebody the sign-in
budget for the account they were invited to use.
Creating an invitation (`POST /orgs/{id}/invitations` and `POST
/orgs/{id}/members` for an address with no account) mails an address the
sender chose, so it is bounded per **inviting account**
(`OPENV_INVITE_BURST` 20, `OPENV_INVITE_REFILL_PER_HOUR` 60): one admin — or
one stolen admin session — cannot point the deployment's SMTP credentials at
a list, and cannot spend a colleague's budget either. Re-posting an
unchanged invitation within the hour of its link being **delivered** does not
mail anything at all (see the endpoint), so an impatient admin costs the
invitee nothing; an invitation whose send failed is re-sent instead, since
nothing reached the invitee to be spared.

Each bucket's `…_BURST` is a whole number above 0 and its
`…_REFILL_PER_HOUR` a positive, finite number (`2.5` and `1e3` included),
spaces round either ignored. Any other value keeps that bucket's default,
and the server logs one warning at boot naming the variable: `Inf`, which
used to refill a bucket at once and so switched its throttling off, now
keeps the default refill. The client address a bucket keys on follows
`OPENV_TRUSTED_PROXY_HOPS`, a whole number above 0, or the one hop
`OPENV_TRUST_PROXY` declares when it is `true` in any case or `1` (see
[operations.md](operations.md)).

While a server requires email verification (`email_verification_required` in
`GET /auth/config`), a session whose account has `email_verified: false` is
answered `403 {"error":"email not verified","code":"email_unverified"}` on
every route outside `/api/v1/auth/*`; `code` is the stable field a client
branches on. Bearer credentials are never gated. A wrong `current_password` on
`PUT /me/password` spends the account's sign-in budget, so guessing it is
throttled the same way guessing at the login form is.

## Sessions

A session ends at whichever deadline comes first: `OPENV_SESSION_MAX_AGE`
(absolute, from sign-in; default and ceiling 720h) or `OPENV_SESSION_IDLE`
(since its last request; default and ceiling 168h). Both are checked on every
authenticated request, so shortening either applies to sessions that already
exist, and a background sweep deletes the rows. An expired session is
answered like any other invalid one (`401`). A successful `PUT /me/password`
invalidates every other session of the account immediately; the caller's own
survives. A password reset (`POST /auth/password-reset/confirm`) invalidates
**every** session, including any the resetting browser held. See [operations.md](operations.md) for the variables.
JSON request bodies are capped at 32 MB. An attachment upload is capped by
the workspace's `max_upload_mb` limit instead (free 128 MB, Business Lite
512 MB, Business 1 GB), answered with `413` and the workspace's own number
when exceeded. An upload whose bytes do not match the declared image type is
refused with `400`, and an SVG attachment is always served as a download.

## Error responses

Errors are plain-text (`http.Error`) or `{"error": "..."}` JSON depending on
handler, with conventional status codes: `400` validation, `401` missing/bad
credentials, `403` insufficient role, `404` not found (also for a resource
the caller cannot reach at all, and for an id that is not a UUID: see the
authorization model), `202` proposal-mode write diverted for review, `500`
server error.
