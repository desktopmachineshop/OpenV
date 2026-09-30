# Contract quirks

Behavior that looks like a bug but is part of what OpenV's API, UI and
stored data promise today. The refactor plan
([`docs/plans/codebase-refactor.md`](plans/codebase-refactor.md) §3.1) pins
each one and keeps it: a refactor preserves a quirk, gives it an honest name
where code expresses it, and never fixes it on the way. Fixing a quirk is a
separate pull request with a release note and its goldens regenerated
deliberately (plan §9.2), never labelled `refactor`.

Each entry gives:

- **Where**: the code that produces the quirk, checked against `master` at
  `863b470` (the plan's own references are at `d11dee8`; where a reference
  has moved, the entry says so);
- **Pinned by, named as**: copied from the plan's §3.1 table, with the
  analysis's pain-point id; a step not yet merged is marked *(planned)*;
- **Pinned today**: what already fails if the quirk changes.

Plan steps: S2 route goldens, S3 stored-data freeze, S4 boot harness, S5a–S5e
API tour and authorization matrix, S6 SSE and event contract, S10
notifications, S12 frontend shell guards, S13 vocabulary parity; X-steps are
the Phase 3 consolidations that give quirks their names.

## Q1. About 158 JSON responses set no `Content-Type`

- **Where:** handlers that call `json.NewEncoder(w).Encode` without setting
  the header first, for example `ListAgents`
  (`internal/api/agent_handlers.go:163`) and `CreateOrg`
  (`internal/api/org_handlers.go:254`); `internal/api` has 249 raw encodes
  (S1's `raw_json_encodes` ratchet). Below 1,400 bytes the server then
  answers `text/plain`, and a gzipped body carries no type.
  `ContentTypeMiddleware` (`internal/api/handlers.go:1594`) sets no type
  despite its name.
- **Pinned by, named as:** S5 (S5a for the requirements core, S5b for V&V
  and the suite, S5c for identity and the workspace, S5d for agents and the
  worker wire, S5e per identity); `writeJSONBare` (X1) *(planned)*. Pain
  points api-core-3, api-suite-org-8.
- **Pinned today:** S1 counts the raw encodes, so none is added. Every step
  of the S5a tour records whether its answer, and the gzip variant of each
  GET, carries a `Content-Type`, so a handler that starts or stops setting
  one changes a golden. Of the core's routes, the two static catalogues are
  bare encodes: `GET /api/v1/meta/artifact-types` (1,060 bytes) answers
  `text/plain` with or without gzip, and `GET /api/v1/meta/link-types`
  (1,902 bytes) answers `text/plain` plain and no `Content-Type` gzipped
  (steps 1 and 2 of `cmd/server/testdata/tour/s5a/artifacts_attributes.json`).
  The S5b tour pins the V&V and suite routes the same way: 41 of its slice's
  74 routes answer their 2xx with a bare encode (the JSON answers of the
  test-run and result, work-item, guided-session and interview routes, the
  product profile, and V&V coverage, matrix, gaps and impact), and nine of
  them show no `Content-Type` once gzipped, among them a run's results and a
  project's interviews (step 28 of `cmd/server/testdata/tour/s5b/test_runs_results.json`,
  step 17 of `cmd/server/testdata/tour/s5b/interviews.json`); the evidence,
  quality, quality-rule, parties and shared-products routes set
  `application/json`. The S5c tour does the same for identity and the
  workspace: 47 of its slice's 107 routes answer a 2xx with a bare encode,
  among them `POST /api/v1/orgs` (step 1 of
  `cmd/server/testdata/tour/s5c/workspaces_logo.json`), sign-in and
  registration, `/auth/me`, the members, invitations, teams, worker and
  runner keys, runner sessions and the avatar upload, and three show no
  `Content-Type` once gzipped: the workspace list, a workspace's
  invitations and its teams (steps 5 and 99 of `workspaces_logo.json`,
  step 19 of `invitations_closed.json`, steps 36 and 54 of
  `members_teams.json`). The S5d tour pins the rest, agents and the worker
  wire: 68 of its slice's 84 routes answer a 2xx with a bare encode, among
  them the worker's claim with its `auth` object (step 15 of
  `cmd/server/testdata/tour/s5d/worker_wire.json`), every launch and run
  read, the agents, automations, crews and their `/teams` aliases,
  proposals, the events route, provider settings and sign-ins, repository
  connections and the runner pool's nodes; the crew export alone sets
  `application/json` (step 80 of `crews_teams.json`), and a proposal-mode
  run's write through S5a's routes answers its 202 receipt with
  `application/json` too (step 12 of `proposals_events.json`). Eleven show no
  `Content-Type` once gzipped: the agent, run, automation, crew and `/teams`
  lists, a crew's graph, the crew templates, a run and its tree, the events
  and the provider settings (step 1 of `agents_automations.json`, step 101
  of `worker_wire.json`, steps 1 and 3 of `crews_teams.json`, step 28 of
  `orchestration_budget.json`, steps 32 and 54 of `proposals_events.json`,
  step 1 of `providers_repos_pool.json`). The S5e matrices record the same
  per route and identity, without the gzip variant: each cell of a 2xx
  names its kind, `json` for a typed answer and `text` for a bare encode
  that net/http sniffed, so a route that gains or loses its type for one
  identity changes a cell (111 `text` cells of
  `cmd/server/testdata/tour/s5e/phantom_matrix.json`, 233 of
  `real_id_reads.json`); no refusal of either matrix is a bare encode.

## Q2. A mid-request delete answers 500 (resolved)

- **Resolved:** fixed under R7 by the release-noted bug-fix pull request
  for #379's decisions 10 and 15 (OpenV REQ-17), so it is no longer a quirk
  to preserve. `internal/persistence/postgres/artifact_repository.go` now
  answers an artifact no row has, a malformed id among them,
  `artifacts.ErrNotFound`, so the `errors.Is(err, artifacts.ErrNotFound)`
  branches of `ChangeArtifactStatus` (a delete landing mid-request) and of
  `UpsertTestResult` (a `test_case_id` no artifact has, and, since the
  release-noted bug-fix pull request for #379's bug 5, one of another
  project than the run's, which `vv.UpsertResult` answers as one no row
  has, before its type is read) answer 404, and
  `PUT /api/v1/artifacts/{id}` and `POST /api/v1/artifacts/{id}/restore`,
  which load the artifact before their guard, answer it 404 `artifact not
  found` and guard with that answer (`requireProjectRoleFor`), so a real
  artifact the caller cannot reach answers alike (I3).
- **Where it was:** `artifact_repository.go` returned an ad hoc
  `errors.New("artifact not found")` instead of `artifacts.ErrNotFound`
  (`internal/domain/artifacts/artifact.go:15`), so the 404 branch of
  `ChangeArtifactStatus` (`internal/api/handlers.go`) never matched and the
  request fell to `respondInternal`; the update and the restore sent any
  lookup error to `respondInternal`.
- **Pinned by, named as:** documented here, S5a–S5e for its neighbours;
  X13 keeps the artifact repository's sentinel *(planned)*. Pain point
  domain-requirements-11.
- **Pinned today:** the delete landing mid-request cannot be timed from
  outside, so no test reaches it; `internal/persistence/postgres`'s
  `TestAnArtifactNoRowHasIsErrNotFound` holds the repository to the
  sentinel for a well-formed id no artifact has and for malformed ones. The
  deterministic neighbours read 404: `PUT /api/v1/artifacts/{id}` and
  `POST /api/v1/artifacts/{id}/restore` on an id no artifact has, 404
  `artifact not found`, where the `GET`, the versions and the `DELETE`
  answer 404 too (steps 37 and 47 of
  `cmd/server/testdata/tour/s5a/artifacts_attributes.json`), and a result
  recorded for a test case id no artifact has, 404 `artifact not found`
  (step 20 of `cmd/server/testdata/tour/s5b/test_runs_results.json`), each
  of which answered 500 until that pull request. The S5e matrix pins the
  first two for every identity: with a well-formed body they answer 404
  `artifact not found` to every column the auth middleware lets through,
  the worker key and the run token among them, where `DELETE
  /api/v1/artifacts/{id}` answers 404 in the words `project not found`
  (the sections of `cmd/server/testdata/tour/s5e/phantom_matrix.json`), and
  its malformed-id pass holds `not-a-uuid`, `%FF` and `a%00b` there to the
  same 404. An id that is not a UUID answered 500 on the evidence bundle,
  file and citation routes, on a shared product's vote, report and delete,
  on a node's heartbeat and release and in the `agent_id` and `run_id`
  filters of the run and proposal lists, until the same pull request had
  the repositories read such an id as one no row has
  (`internal/persistence/postgres/ids.go`): those steps now answer as a
  well-formed id no row has, 404, 204 on the uncite, which looks nothing
  up, and an empty list for the filters (steps 17, 46, 54, 77 and 84 of
  `cmd/server/testdata/tour/s5b/evidence.json`, 32, 48 and 61 of
  `shared_products.json`, 124 and 125 of
  `cmd/server/testdata/tour/s5d/providers_repos_pool.json`, 100 of
  `worker_wire.json`, 24 of `proposals_events.json`). The S5d tour's
  well-formed neighbours answer 404, the not-found of their sibling routes:
  deleting an agent that is gone, or a slug no agent has (steps 40 and 41 of
  `agents_automations.json`), releasing a pool node no one has, as its
  heartbeat does (steps 122 and 123 of `providers_repos_pool.json`), and a
  delegation from a run whose crew node was removed, the run still naming
  the node (step 9 of `orchestration_budget.json`).

## Q3. Managed link edits in `PUT /artifacts/{id}` take their own path

- **Where:** `UpdateArtifact` (`internal/api/handlers.go:753`) applies
  `pending_link_adds` and removals through `processManagedLinkChanges`
  (`:2985`). Unlike `POST /links` it skips the FeatureFlowDown gate
  (`CreateLink` checks it at `:1368`), publishes no `LinkCreated` or
  `LinkDeleted` event (`CreateLink` and `DeleteLink` do, at `:1393` and
  `:1582`), silently skips an invalid add (`continue` at `:3033`–`:3079`),
  and the version note lists the *requested* links
  (`linksFromPendingAdds`, `:824`), not the ones created.
- **Pinned by, named as:** S5a; X11a *(planned)*; explicit `Policy` flags in
  X11b *(planned)*. Pain point api-requirements-3.
- **Pinned today:** the S5a tour, `cmd/server/testdata/tour/s5a/links_managed_edits.json`:
  one managed edit adds a valid and an invalid link and removes a link
  between two other artifacts (step 49): the invalid add is skipped without
  a word, only `artifact.updated` is published, and the removed link's
  source gets no auto-version (steps 54–56); the note lists the requested
  links, the skipped one too (step 50); a supplier's managed `refines` add
  into a project it only views is skipped, where `POST /api/v1/links` lets
  the same link through (steps 62–64). The FeatureFlowDown half is not
  pinned yet: the tour's workspaces are on the nightly channel, where every
  feature is on, so neither path refuses; it needs a stable-channel
  workspace.

## Q4. `links_snapshot` and auto-version numbering

- **Where:** `UpdateArtifact` writes `links_snapshot` only when at least one
  link remains (`internal/api/handlers.go:857`), while
  `autoVersionLinkedArtifacts` (`:3117`) always writes it; the chatter note
  names auto-version N as the version read before the update plus 1
  (`:3182`).
- **Pinned by, named as:** S5a; X11a *(planned)*.
- **Pinned today:** the S5a tour, `cmd/server/testdata/tour/s5a/links_managed_edits.json`: a
  managed removal that leaves no link carries the previous `links_snapshot`
  forward (steps 57–59), while `autoVersionLinkedArtifacts` writes an empty
  one (step 48); every auto-version note names the version read before the
  update plus 1, and the versions it names are the ones made (steps 65–67).

## Q5. `POST /api/v1/orgs` returns unresolved derived fields

- **Where:** `CreateOrg` (`internal/api/org_handlers.go:221`) encodes the
  workspace the domain service returns; only a read through the repository's
  `scanOrg` sets `has_logo` and resolves the release channel
  (`internal/persistence/postgres/org_repository.go:68-72`), so the create
  response says `release_channel ""` and `locked false`.
- **Pinned by, named as:** S5c. Pain point domain-platform-v2.
- **Pinned today:** the S5c tour. `POST /api/v1/orgs` answers
  `"release_channel":""`, `"release_channel_locked":false`,
  `"billing":{"status":""}` and the creator's `"role":"admin"`, and `GET
  /api/v1/orgs/{id}` of the same workspace answers `nightly`, `true`,
  `"none"` and no role (steps 1 and 2 of
  `cmd/server/testdata/tour/s5c/workspaces_logo.json`), on the
  single plan under the tiers (step 6 of `tiers_secure_runner_sessions.json`)
  and on a self-hosted deployment's self-host plan (steps 2 and 3 of
  `self_hosted_cross_site_sso.json`, read back in step 33).

## Q6. Go and TypeScript vocabularies have drifted

- **Where:** the `refines` link rule's description differs between
  `internal/domain/links/validation.go:61` and
  `frontend/src/config/linkTypeRules.ts:52`; the event-type filters in
  `frontend/src/views/ActivityLog.tsx:9` and
  `frontend/src/views/AutomationsPage.tsx:15` are subsets of the 26 types in
  `internal/domain/events/events.go` (24 until the R7 bug-fix pull request
  for #379's decisions on REQ-4 and REQ-5 added `artifact.restored` and
  `baseline.deleted`, which the activity log's filter offers and the
  automations page does not).
- **Pinned by, named as:** S13's allowed differences *(planned)*;
  `LINK_RULE_UI_OVERRIDES` (X4) *(planned)*. Pain point fe-requirements-4.
- **Pinned today:** S6 pins the 26 event type strings
  (`internal/domain/events/event_types_test.go`), not the TS subsets.

## Q7. Bell deep links differ from email and push links

- **Where:** `pathForNotification`
  (`frontend/src/components/NotificationBell.tsx:31`) and `notificationPath`
  (`internal/notify/email.go:253`) map the same notification to different
  routes.
- **Pinned by, named as:** S10 *(planned)*; the X4 fixture keeps separate
  `go` and `ts` expectations *(planned)*. Pain points fe-shell-v2,
  services-7.
- **Pinned today:** S12's deep-link snapshot pins the backend-built links it
  resolves (`frontend/src/arch/__snapshots__/deepLinks.txt`), not the bell's
  mapping.

## Q8. Seven `limit` parsers; events and runs reset to 100

- **Where:** the seven query parsers are in `ListArtifacts`
  (`internal/api/handlers.go:715`), `ListAgentRuns`
  (`internal/api/agent_handlers.go:458`), `ListDomainEvents` (`:2214`),
  `ListNotifications` (`internal/api/notification_handlers.go:130`),
  `GlobalSearch` (`internal/api/search_handlers.go:65`),
  `ListSharedProducts` (`internal/api/shared_product_handlers.go:64`) and
  `ListProjectInterviewSessions` (`internal/api/suite_handlers.go:1632`).
  The event and run repositories reset a limit at or below 0 or above 500 to
  100 instead of clamping it
  (`internal/persistence/postgres/event_repository.go:39-41`,
  `agent_run_repository.go:147-149`).
- **Pinned by, named as:** S5 (S5a for `ListArtifacts` and `GlobalSearch`,
  S5b for `ListSharedProducts` and `ListProjectInterviewSessions`, S5c for
  `ListNotifications`, S5d for the event and run resets); named
  `limitPolicy` values (X3) *(planned)*. Pain point persistence-v3.
- **Pinned today:** the S5a tour: `GET /api/v1/artifacts` answers limit 0
  and a limit over 1,000 with all 201 artifacts of a 201-artifact project,
  which none of the other six parsers' policies would (the largest, shared
  products', is 200 and 500), and reads a negative or non-numeric offset as
  0, with `X-Total-Count` (steps 31–34 of
  `cmd/server/testdata/tour/s5a/artifacts_attributes.json`; the exact 1,000
  would take 1,001 artifacts, and is X3a's table test's); `GET /api/v1/search` reads limit 0 as
  20 and caps 51 at 50 (steps 51 and 52 of
  `cmd/server/testdata/tour/s5a/review_chatter_search.json`). The S5b tour:
  `GET /api/v1/shared-products` reads limit 0, -3 and `abc` as its default
  of 200 and caps 501 at 500, so a pool of 201 answers 200 rows with no
  limit and all 201 with limit 501 (steps 3–7, 71 and 72 of
  `cmd/server/testdata/tour/s5b/shared_products.json`); `GET
  /api/v1/projects/{id}/interview-sessions` refuses `x` with a 400, reads no
  limit, 0 and -1 as its default of 20, and caps 500 at 100, which answers
  all 21 sessions of its project (steps 43–48 of
  `cmd/server/testdata/tour/s5b/interviews.json`; the exact 100 would take
  101 sessions, and is X3a's). The S5c tour: `GET /api/v1/notifications`
  refuses limit 0 and `abc` with a 400, `limit must be a positive integer`,
  where every other parser falls back to a default, and pages by a keyset
  cursor, `before=<time>|<id>`, with a full page carrying `next_cursor`
  (steps 9–15 of `cmd/server/testdata/tour/s5c/notifications_push.json`;
  its cap needs more notifications than the area makes, and is X3a's). The
  S5d tour, over 101 runs and their events in a workspace of their own: `GET
  /api/v1/events` and `GET /api/v1/agent-runs` answer limit 0, -1, `abc` and
  501 with 100 rows, reset rather than clamped (the events page with its
  `X-Next-Cursor`), and limit 500 with all 101 (steps 66–75 of
  `cmd/server/testdata/tour/s5d/proposals_events.json`). For the events the
  handler resets the limit the same way before the repository sees it
  (`internal/api/agent_handlers.go:2215`), so the event repository's own
  reset is not reached from outside: a change to it alone changes no golden,
  where a change to the handler's, or to the run repository's, changes
  steps 69 and 74.

## Q9. `ErrBudgetExceeded` answers 402, 400 or 500 by route

- **Where:** 402 on two launch routes, `LaunchAgentRun` and
  `DraftTestCases` (`internal/api/agent_handlers.go:351`, `:438`); 400 on
  three, `RunAutomationNow` (`:1117`), `LaunchTeamRun` (`:2196`) and
  `LaunchTestRunAgent` (`internal/api/suite_handlers.go:458`), which pass
  `err.Error()` through; 500 on delegation, `DelegateRun`
  (`internal/api/agent_handlers.go:950`).
- **Pinned by, named as:** S5d; `launchErrs402`, `launchErrs400` and
  `launchErrsDelegate` (X3) *(planned)*. Pain point api-suite-org-7.
- **Pinned today:** the S5d tour, booted with `OPENV_BUDGET_ENFORCE=true`,
  puts a workspace over its monthly budget and sends every launch path:
  the launch and the test-case draft answer 402, run-now, a crew launch and
  a test run's agent run 400, each with the guard's text, and delegation
  500 `failed to launch delegated run` (steps 43–48 of
  `cmd/server/testdata/tour/s5d/orchestration_budget.json`). Beyond this
  entry's six, a retry answers 500 `failed to retry run`
  (`RetryAgentRun`, `internal/api/agent_handlers.go:625`; step 49), and a
  crew run that finishes over budget launches none of its agent successors,
  the refusal only logged, while its hand-off to a person is still made
  (steps 50 and 51). With the budget cleared the launch, run-now, the crew
  launch and the retry pass (steps 52–55), so each refusal was the
  guard's.

## Q10. `RunFinished` is published twice, or never

- **Where:** a proposal-mode run publishes `RunFinished` when it finishes
  into `awaiting_approval` (`internal/domain/agentruns/agentruns.go:953`)
  and again when its proposals are resolved (`FinalizeIfResolved`,
  `:1178`); a cancel of a queued run (`RequestCancel`, `:1069-1080`)
  notifies the status change but never publishes it.
- **Pinned by, named as:** S5d events.
- **Pinned today:** the S5d tour records each step's events. A
  proposal-mode run publishes `agentrun.finished` when its finish leaves it
  awaiting approval (step 19 of
  `cmd/server/testdata/tour/s5d/proposals_events.json`) and again when its
  last proposal is resolved, by a bulk approval, a rejection that finalises
  it failed, a bulk rejection that finalises it succeeded and a bulk
  approval with an apply failure that finalises it failed (steps 30, 35, 50
  and 80); a queued run's cancel publishes `workitem.moved` and no
  `agentrun.finished`, where a running run's cooperative cancel publishes it
  on the worker's finish (steps 54 and 42 of `worker_wire.json`). The plan's
  non-goals keep the double publish (services-3).

## Q11. `NewHandler` rewires billing after `billing.Start`

- **Where:** `cmd/server/main.go:793` starts the billing service's
  goroutines, then `api.NewHandler` (`:797`) calls `SetSeatCounter` and
  `DefaultReturnURL` on it (`internal/api/handlers.go:372-377`; the plan's
  I17 row cites `:373-379` at `d11dee8`).
- **Pinned by, named as:** S4a `boot_steps.txt` (statement order) and the
  S4b billing profile's boot log; X12 keeps the point *(planned)*. Pain
  point boot-v1.
- **Pinned today:** S4a's `TestBootSteps` (`cmd/server/boot_steps_test.go`)
  fails if `(*internal/billing.Service).Start` moves past
  `inline internal/api.NewHandler` and its `SetSeatCounter` and
  `DefaultReturnURL` in `cmd/server/testdata/boot_steps.txt`. S4b's
  `TestBootProfiles` (`cmd/server/boot_profiles_test.go`) pins the billing
  profile's boot in `cmd/server/testdata/boot/billing.txt`: `billing
  enabled` logged between `release` and `starting server`, the reconcile
  `Start` runs at once (its three warnings among the lines from goroutines,
  awaited before the first probe), and the provider calls it made (three
  operations, three attempts each, all refused by the test's proxy). Since
  S5c the rewiring also shows in what the server sends the provider: the S5c
  tour's billing area answers as Stripe (a stand-in the recording proxy
  serves over TLS), and its checkouts carry `success_url` and `cancel_url`,
  and its portal session `return_url`, built from `FRONTEND_URL` with its
  trailing slash trimmed through `DefaultReturnURL`, and a Business
  checkout and plan change bill the seats `SetSeatCounter` counts (members
  plus pending invitations: quantity 3) (steps 22–24 and 30 of
  `cmd/server/testdata/tour/s5c/billing.json`); and the seat sync, the
  goroutine `Start` launches, pushes that count as the item's quantity after
  a pending invitation is revoked and after a member is removed, then reads
  the subscription again (steps 46 and 47 of `billing.json`, with the
  requests before them in its `stand_in_requests_outside_steps`). Those are
  the values the rewiring sets, not its order against `Start`, which stays
  `boot_steps.txt`'s alone.

## Q12. `FRONTEND_URL` has two fallback chains; reports read raw `UPLOADS_DIR`

- **Where:** `FRONTEND_URL`, then `PUBLIC_URL`, then
  `http://localhost:3000` for email links and the handler
  (`cmd/server/main.go:538`, `:824`); `FRONTEND_URL`, then
  `http://localhost:3000` for the Google and OIDC sign-in configurations
  (`:738`, `:760`). The report service reads `UPLOADS_DIR` with
  `os.Getenv` (`internal/domain/reports/report.go:735`), not the server's
  `./uploads` default (`cmd/server/main.go:141`). Both reads trim the
  value, as every setting but a credential has been read since the R7 fix
  of #379's question 15 (`internal/envparse`), so spaces round it do not
  send the two to different directories; the quirk is the missing default,
  not the spaces. The lines are as that fix left them, each one or two from
  `863b470`'s (`main.go:539`, `:825`, `:739`, `:761` and `:139`, and
  `report.go:733`).
- **Pinned by, named as:** the S5c tour for the two chains; S8's env
  inventory for both reads of each variable; X10 keeps distinct fields
  *(planned)*. Pain point boot-3.
- **Pinned today:** S1's env-read ratchet counts the direct read in
  `internal/domain/reports`. The S5c tour boots a server with `PUBLIC_URL`
  set and `FRONTEND_URL` unset, and pins each site: an invitation's link
  (`main.go`'s email link base) and a share link's `url` (the handler's
  `FrontendURL`) are on `PUBLIC_URL` (the first chain), and Google's
  `redirect_uri` is on it too, while an OIDC sign-in and a Google sign-in
  land on `http://localhost:3000` (the second) (steps 4, 44, 42, 31 and 43
  of `cmd/server/testdata/tour/s5c/self_hosted_cross_site_sso.json`). The raw
  `UPLOADS_DIR` read is not pinned by a response, but
  `internal/archtest/testdata/env_vars.txt` (S8) lists it beside the
  server's `envOr` with `"./uploads"`, and `FRONTEND_URL` and `PUBLIC_URL`
  each with their constant and their computed fallback; S8's `per-request`
  exemption keeps the report's read where it is.

## Q13. Only `POST /api/v1/projects` enforces the project maximum (resolved)

- **Resolved:** fixed under R7 by its own release-noted bug-fix pull
  request (#379's question 8; OpenV REQ-176 and REQ-177), so it is no
  longer a quirk to preserve. Every project create, `POST /api/v1/projects`,
  `POST /api/v1/templates/{id}/projects` and `POST /api/v1/projects/import`,
  now asks `requireProjectCreate` (`internal/api/authz.go`), which takes the
  plan read-only gate (`requireWritable` on the caller's active workspace,
  which the import route's `alwaysWritable` passes, as REQ-177 keeps import
  open on a read-only workspace) and `checkProjectCount` before anything is
  created.
- **Where it was:** `checkProjectCount` (`internal/api/limits.go`) had one
  caller, `CreateProject` (`internal/api/handlers.go`); the template and
  import routes counted nothing, and none of the three asked the gate.
- **Pinned by, named as:** S5e's over-plan pass under the S4 tiers-on
  profile, and under the self-hosted profile with `OPENV_LIMITS`, pin the
  fix. Pain point api-requirements-v1.
- **Pinned today:** at the single plan's 200 projects, a new project, a
  template's project and an import are each refused 403 `limit_reached`
  with the Billing tab's remedy; once W is over `max_projects`, read-only, a
  new project is refused by the gate, 403 `plan_read_only`, before the count
  (steps 60–66 of `cmd/server/testdata/tour/s5e/over_plan_tiers_on.json`).
  A workspace read-only for its seats refuses a new project and a template's
  project 403 `plan_read_only` (steps 28 and 29) and still takes the
  imports, which are always writable (steps 48 and 49). On a self-hosted
  deployment at `OPENV_LIMITS`'s seven projects the three creates are
  refused `limit_reached` with the `OPENV_LIMITS` remedy, and once two
  projects past the seven are written as setup a new project and a
  template's project are refused `plan_read_only` and the imports, past the
  gate, `limit_reached` (steps 4–7, 12, 13, 18 and 19 of
  `over_plan_self_hosted.json`). `internal/api`'s
  `TestEveryProjectCreateTakesTheGateAndTheCount` covers each route at the
  maximum, over it, and on a workspace over its seats.

## Q14. Some list endpoints encode `null` for an empty list

- **Where:** a repository that builds its result with `var result []*T`
  returns `nil` for no rows, and the handler encodes it as is: for example
  `GET /api/v1/agents` (`internal/persistence/postgres/agent_repository.go:125`,
  `internal/api/agent_handlers.go:163`) answers `null` for a workspace with no
  agents, while the project repository's lists start from an empty slice
  (`project_repository.go:62`) and answer `[]`.
- **Pinned by, named as:** S5a (the requirements core), S5b (V&V and the
  suite); later slices pin their own lists. Pain point api-suite-org-v6.
- **Pinned today:** the S5a tour records each empty list as the bytes the
  server sends. `null`: an artifact's figures (`GET
  /api/v1/artifacts/{artifactID}/attachments`), its chatter, a project's
  links (`GET /api/v1/links`), a version with no `links_snapshot`, a
  project's baselines, and the empty project's export (`artifacts`, `links`
  and `attachments`, also inside a baseline's snapshot). `[]`: projects,
  children, artifacts, attribute definitions, current links, linked
  artifacts, a project's figures, share links, the open-source showcase and
  a download's selection. See the steps titled "null (Q14)" in
  `cmd/server/testdata/tour/s5a/{attachments,baselines_documents,exports_imports,links_managed_edits,review_chatter_search}.json`.
  The S5b tour does the same for its routes. `null`: a project's test runs,
  a run's results, a project's work items and a work item's activity, a
  project's guided sessions, a project's interviews with an interview's
  invites and sessions, and the transcript of an interview session with no
  message yet (the repository's `nil`, also inside the participant's
  intro). `[]` or `{}`: evidence bundles and a result's citations (the
  handlers normalise nil), a run's citations (`{}`), a guided session's
  drafts and transcript, the V&V entries and every gap list, a
  project's interview sessions (normalised), and a top-voted shared-products
  list. See the steps titled "null (Q14)" in
  `cmd/server/testdata/tour/s5b/{guided_sessions,interviews,test_runs_results,work_items}.json`,
  steps 1, 47 and 48 of `evidence.json` and step 20 of
  `vv_coverage_report.json` beside them. The S5c tour, for identity and the
  workspace, `null`: an account's deleted workspaces (`GET
  /api/v1/orgs?deleted=true`), a workspace's teams, a project's team
  grants, a workspace's worker keys, and an empty inbox or page of
  notifications (step 6 of `cmd/server/testdata/tour/s5c/workspaces_logo.json`,
  step 59 of `sessions_auth.json`, steps 29 and 81 of `members_teams.json`,
  step 1 of `runner_keys_connector.json`, steps 2, 6, 7 and 11 of
  `notifications_push.json`). The S5d tour, for agents and the worker wire,
  `null`: a workspace's agents and its sync once none is left, automations,
  a new crew's nodes and edges and a workspace's crews, proposals (a
  project's, and a workspace admin's with none in the workspace), runs, a
  project's repository connections, a workspace's worker keys, and a pool
  node's providers read back after its registration answered `[]` (steps
  42, 43, 73 and 74 of `cmd/server/testdata/tour/s5d/agents_automations.json`,
  steps 11 and 116 of `crews_teams.json`, steps 23 and 29 of
  `proposals_events.json`, steps 90 and 96 of `worker_wire.json`, steps 68,
  100, 102 and 107 of `providers_repos_pool.json`). `[]`: a run's log, the
  events before a cursor no event has, and the project list of a personal
  runner key whose member has a role in no project (steps 53 and 104 of
  `worker_wire.json`, step 61 of `proposals_events.json`). The S5e matrix counts each list its
  real-id reads answer, per identity (`[n]` or `null`): a workspace's teams,
  a project's repository connections and team grants and a project's
  proposals answer `null` to every column that reads them, and, with no
  query, a plain member's runs, none of which it launched, and the
  automations of a workspace with none (the sections "real ids, GET", "real
  ids in the query, GET" and "real ids, lists with no query" of
  `cmd/server/testdata/tour/s5e/real_id_reads.json`).

## Q15. An unknown protected path answers 401; OPTIONS answers 200 unlogged

- **Where:** `AuthMiddleware` refuses before the router sees the request
  (`internal/api/authmiddleware.go:177`), so a path no route matches answers
  401, not 404, unless it is public. `CORSMiddleware` answers every
  `OPTIONS` with 200 itself (`internal/api/security_headers.go:58-61`),
  outside the request log. The chain is built at `cmd/server/main.go:874-920`.
- **Pinned by, named as:** S4a. Pain point boot-v4.
- **Pinned today:** S4a's `TestBootSmoke` (`cmd/server/boot_smoke_test.go`):
  the `auth-before-routing` probe (401 on `GET /api/v1/no-such-route`) and
  the `preflight-allowed` and `preflight-refused` probes (200, `log (none)`)
  in `cmd/server/testdata/boot/{default,metrics_token}.txt`.

## Q16. The wizard and the notes panel build different artifact text

- **Where:** `GuidedWizard.tsx` materialises artifacts from its own section
  table and templates (`frontend/src/views/GuidedWizard.tsx:63`, bodies at
  `:781`, `:817`); the notes panel's path uses
  `frontend/src/components/wizard/suggestionDrafts.ts` (bodies at `:171-194`,
  sections at `:290`).
- **Pinned by, named as:** F5 golden strings *(planned)*, which keep both
  variants as named exports of `artifactTemplates.ts`. Pain point
  fe-suite-org-3.
- **Pinned today:** nothing beyond this entry.

## Q17. The purge list has gaps not covered by cascade

- **Where:** `PurgeOrg` (`internal/persistence/postgres/org_repository.go:580`)
  deletes a hand-maintained list of tables; `attachment_figure_counters`,
  keyed by `artifact_id` with no foreign key, keeps its rows.
- **Pinned by, named as:** S3 purge-catalog allowlist, `purgeGapAllowlist`.
  Pain point persistence-4.
- **Pinned today:** S3: `TestPurgeCatalog`
  (`internal/persistence/postgres/migration_freeze_purge_test.go:53`) and
  `testdata/purge/catalog.txt`. The allowlist may only shrink, and closing a
  gap is a release-noted change of its own.

## Q18. Rate-limit buckets are shared across endpoints

- **Where:** `authIPLimiter` (`internal/api/handlers.go:242`), the sign-in
  budget per client address, is also spent by `VerifyEmail`
  (`internal/api/email_verification_handlers.go:52`) and by password reset
  (`internal/api/password_reset_handlers.go:66`, `:121`), besides `Login`
  (`internal/api/auth_handlers.go:328`).
  Likewise `invitePreviewLimiter` (`internal/api/handlers.go:255`), the
  invitation-preview budget, is also spent by every share-link token lookup
  and share accept (`allowPublicShare`, `internal/api/share_handlers.go:199`),
  which answer its 429 with no `Retry-After`.
- **Pinned by, named as:** S5c's shared-bucket probe, which exercises the
  real call sites; S5a for the share routes. Pain point api-core-v2.
- **Pinned today:** the S5a tour drains `invitePreviewLimiter` through
  invitation previews, then the share routes answer 429 with no
  `Retry-After` (steps 46–49 of
  `cmd/server/testdata/tour/s5a/share_links_public.json`), so a share
  bucket of its own would change that golden; the S5c tour drains it the
  other way, through share-link lookups, and a preview from that address is
  refused (step 57 of `cmd/server/testdata/tour/s5c/invitations_closed.json`).
  The S5c tour drains each shared bucket through one route and reads the
  429, with its `Retry-After`, on another, from its own client address
  (`OPENV_CLIENT_IP_HEADER`): `authIPLimiter` spent by email verification
  refuses sign-in and reset confirmation, spent by reset confirmation
  refuses verification and sign-in, and spent by sign-in refuses
  confirmation and verification (steps 71–81 of
  `cmd/server/testdata/tour/s5c/sessions_auth.json`); on a server with a
  mailer, reset requests spend it too and refuse sign-in, confirmation and
  verification, and sign-ins refuse a reset request (steps 128–134 of
  `mail_password_admin.json`); without one, a reset request is refused with
  a 409 before it spends a token, from a drained address too (steps 91 and
  92 of `sessions_auth.json`). Beyond the plan's entry, the tour pins the
  other shared buckets the same way: `authAccountLimiter`, spent by failed
  sign-ins, refuses `PUT /api/v1/me/password`, and the reverse (steps 82–86
  of `sessions_auth.json`); `ssoIPLimiter`, spent by Google sign-on starts,
  refuses Google's callback (steps 87 and 88), and spent by OIDC sign-on
  starts refuses OIDC's callback (steps 45 and 46 of
  `self_hosted_cross_site_sso.json`); `inviteLimiter`, spent by an admin's
  invitations, refuses its add through `POST /api/v1/orgs/{id}/members`,
  which invites through the same call site (step 59 of
  `invitations_closed.json`); `verifyResendLimiter`, spent by
  address changes, refuses a resend (steps 25, 35 and 36 of
  `mail_password_admin.json`); and the billing write bucket, one per
  workspace, is spent by checkout, plan change and portal alike, and not by
  a refresh (steps 38–43 of `billing.json`).

## Q19. Three error-message conventions

- **Where:** one site answers `"Invalid request body"` with a capital I
  (`internal/api/handlers.go:2582`); 124 `writeJSONError` calls in
  `internal/api` pass `err.Error()` through (the same count as at
  `d11dee8`); 58 sites map any error to 404 (the plan's count at `d11dee8`,
  not re-counted here).
- **Pinned by, named as:** S5 (S5a, S5b, S5c and S5d for their routes, S5e
  per identity); `decodeJSONMsg` (X1) *(planned)*; the other call sites stay
  untouched.
- **Pinned today:** S1's `invalid_request_body_literals` ratchet counts the
  lowercase literal (106). The S5a tour pins every error message its routes
  answer, byte for byte: the capital `"Invalid request body"` of
  `RenameAttachment` (step 28 of `cmd/server/testdata/tour/s5a/attachments.json`) beside the
  lowercase one elsewhere, and `err.Error()` passed through, such as
  `encoding/xml`'s text for a truncated ReqIF import (step 46 of
  `cmd/server/testdata/tour/s5a/exports_imports.json`). The S5b tour pins
  the driver's text passed through as a 400,
  `pq: invalid input syntax for type uuid: "not-a-uuid"`, for a work
  item's `assignee_id` (a person's or an agent's: a crew's has been looked
  up since the release-noted bug-fix pull request for #379's REQ-23
  decision, and one no crew has answers `404` `team not found`, step 15),
  an interview's `guided_session_id` and a guided draft's `parent_id`
  (step 5 of `work_items.json`, step 8 of
  `interviews.json` and step 29 of `guided_sessions.json`, under
  `cmd/server/testdata/tour/s5b/`; a run's `baseline_id`, step 6 of
  `test_runs_results.json`, passed the driver's text through too until the
  fix for #379's decision on REQ-6, and now answers as a baseline no row
  has, `baseline not found`), and any lookup error mapped to 404: a
  quality report's malformed `baseline_id`, a lint of a malformed artifact
  id and a malformed run's citations (steps 17 and 27 of
  `quality_profile_parties.json`, step 68 of `evidence.json`). The S5c tour
  pins the same conventions for identity and the workspace: the driver's
  text passed through as a 400 for a worker key's name over 255 characters
  (step 6 of `cmd/server/testdata/tour/s5c/runner_keys_connector.json`;
  its step 16, a revoke of a malformed key id, passed the driver's text
  through too until the fix for #379's decision 15, and now answers as a key
  no workspace has, `worker key not found`); the domain's
  text as it is, a blank workspace name, the last admin leaving or demoted,
  and a role change for or removal of an account that is not a member, in
  `ErrNotMember`'s words, which address the caller, not the account the path
  names (step 4 of `workspaces_logo.json`, steps 20, 21, 24, 25 and 111 of
  `members_teams.json`). A lookup's error was answered as a 500 where the
  path or body names something that is not an id (the default workspace,
  the platform admin's reset link and admin standing, and a workspace's
  members), until the release-noted bug-fix pull request for #379's
  decision 15 answered such an id as one no row has: `404` (step 47 of
  `sessions_auth.json`, steps 66 and 92 of `mail_password_admin.json`,
  step 18 of `members_teams.json`); the ids a body stores as references
  without a lookup keep the driver's 400 above, since text that is not a
  UUID cannot be stored as a phantom id is. `UpdateOrg`
  still passes the service's not-found through as a 400
  (`internal/api/org_handlers.go:309`), but no step reaches it: the
  workspace guard answers a workspace no row has with its 404 first, the
  platform admin's rename of one among them (step 101 of
  `workspaces_logo.json`). The S5d tour, for agents and the worker
  wire: the driver's text passed through as a 400 for a launch on a card id
  that is not a UUID, an automation of an agent no row has (the foreign
  key's refusal) and a repeated `reviews` edge of a crew (the unique key's),
  and robfig's text for a cron that does not parse (step 9 of
  `cmd/server/testdata/tour/s5d/worker_wire.json`, steps 61, 53 and 85 of
  `agents_automations.json`, step 40 of `crews_teams.json`); the YAML
  parser's text for an agent file that does not parse (step 23 of
  `agents_automations.json`); and any lookup error answered as a 404: an
  automation, a CLI sign-in and a repository connection named by an id that
  is not a UUID (step 78 of `agents_automations.json`, steps 67 and 95 of
  `providers_repos_pool.json`), and a proposal's (step 40 of
  `proposals_events.json`). "crew not found" answers the export, where
  every other crew route answers "team not found" (step 84 of
  `crews_teams.json`). The S5e matrix pins every refusal's message per route
  and identity (`cmd/server/testdata/tour/s5e/phantom_matrix.json`); the
  platform admin, whom the guards pass only into a project or workspace
  that exists, gets their `404` for a phantom before any handler's text,
  so that no refusal of the matrix passes the driver's text through.

## Q20. Inline error chains render string bodies differently

- **Where:** inline `err.response?.data?.error || err.message` reads across
  `frontend/src` show a plain-text error body differently from
  `apiErrorMessage` (`frontend/src/api/errors.ts:71`).
- **Pinned by, named as:** `legacyErrorText` (X15) *(planned)*. Pain point
  fe-suite-org-7.
- **Pinned today:** S12's ratchet (`frontend/src/arch/errorChains.test.ts`,
  `CEILING = 53`) refuses a new inline chain.

## Q21. Four SSE reconnect policies

- **Where:** the four `EventSource` sites:
  `frontend/src/components/agents/RunDetailPanel.tsx:259` (backoff with
  `after_seq`), `frontend/src/components/wizard/GuidedChatPanel.tsx:380` and
  `frontend/src/views/InterviewChat.tsx:51` (a capped exponent forever; the
  public interview without credentials), and
  `frontend/src/components/NotificationBell.tsx:146`.
- **Pinned by, named as:** S6; named `useEventStream` policies (X15)
  *(planned)*. Pain point fe-shell-9.
- **Pinned today:** S6's stream tests
  (`frontend/src/components/agents/RunDetailPanel.test.tsx`,
  `frontend/src/views/InterviewChat.stream.test.tsx`) and S12's lint
  allowlist `EVENT_SOURCE_SITES` in `frontend/eslint.config.js`, which
  refuses a fifth site.

## Q22. Token redaction applies only to the access log

- **Where:** `redactPath` (`internal/api/requestlog.go:93`) hides interview
  and share tokens in the request log (`:63`), but the error log writes the
  raw path (`internal/api/httperr.go:89`).
- **Pinned by, named as:** untouched; the security fix is a separate pull
  request. Pain point api-core-v3.
- **Pinned today:** nothing beyond this entry.

## Adding a quirk

A refactor that finds another quirk keeps it, adds an entry here and a row
to the plan's §3.1 table in a class T commit, and pins it in a class C
commit before the code behind it moves (rule R1). A bug found during a
refactor is not a quirk: it gets its own pull request with a `### Bug fixes`
bullet (plan §8.5).
