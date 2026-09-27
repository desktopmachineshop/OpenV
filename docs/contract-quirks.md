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
- **Pinned by, named as:** S5 *(planned)*; `writeJSONBare` (X1)
  *(planned)*. Pain points api-core-3, api-suite-org-8.
- **Pinned today:** S1 counts the raw encodes, so none is added; the header
  itself waits for S5.

## Q2. A mid-request delete answers 500

- **Where:** `internal/persistence/postgres/artifact_repository.go:165`
  returns an ad hoc `errors.New("artifact not found")` instead of
  `artifacts.ErrNotFound` (`internal/domain/artifacts/artifact.go:15`), so
  `case errors.Is(err, artifacts.ErrNotFound)` in `ChangeArtifactStatus`
  (`internal/api/handlers.go:952`) never matches and the request falls to
  `respondInternal`.
- **Pinned by, named as:** documented here; X13 keeps each repository's
  not-found convention *(planned)*. Pain point domain-requirements-11.
- **Pinned today:** nothing beyond this entry.

## Q3. Managed link edits in `PUT /artifacts/{id}` take their own path

- **Where:** `UpdateArtifact` (`internal/api/handlers.go:753`) applies
  `pending_link_adds` and removals through `processManagedLinkChanges`
  (`:2985`). Unlike `POST /links` it skips the FeatureFlowDown gate
  (`CreateLink` checks it at `:1368`), publishes no `LinkCreated` or
  `LinkDeleted` event (`CreateLink` and `DeleteLink` do, at `:1393` and
  `:1582`), silently skips an invalid add (`continue` at `:3033`–`:3079`),
  and the version note lists the *requested* links
  (`linksFromPendingAdds`, `:824`), not the ones created.
- **Pinned by, named as:** X11a *(planned)*; explicit `Policy` flags in X11b
  *(planned)*. Pain point api-requirements-3.
- **Pinned today:** nothing beyond this entry.

## Q4. `links_snapshot` and auto-version numbering

- **Where:** `UpdateArtifact` writes `links_snapshot` only when at least one
  link remains (`internal/api/handlers.go:857`), while
  `autoVersionLinkedArtifacts` (`:3117`) always writes it; the chatter note
  names auto-version N as the version read before the update plus 1
  (`:3182`).
- **Pinned by, named as:** X11a *(planned)*.
- **Pinned today:** nothing beyond this entry.

## Q5. `POST /api/v1/orgs` returns unresolved derived fields

- **Where:** `CreateOrg` (`internal/api/org_handlers.go:221`) encodes the
  workspace the domain service returns; only a read through the repository's
  `scanOrg` sets `has_logo` and resolves the release channel
  (`internal/persistence/postgres/org_repository.go:68-72`), so the create
  response says `release_channel ""` and `locked false`.
- **Pinned by, named as:** S5c *(planned)*. Pain point domain-platform-v2.
- **Pinned today:** nothing beyond this entry.

## Q6. Go and TypeScript vocabularies have drifted

- **Where:** the `refines` link rule's description differs between
  `internal/domain/links/validation.go:61` and
  `frontend/src/config/linkTypeRules.ts:52`; the event-type filters in
  `frontend/src/views/ActivityLog.tsx:9` and
  `frontend/src/views/AutomationsPage.tsx:15` are subsets of the 24 types in
  `internal/domain/events/events.go`.
- **Pinned by, named as:** S13's allowed differences *(planned)*;
  `LINK_RULE_UI_OVERRIDES` (X4) *(planned)*. Pain point fe-requirements-4.
- **Pinned today:** S6 pins the 24 event type strings
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
- **Pinned by, named as:** S5 *(planned)*; named `limitPolicy` values (X3)
  *(planned)*. Pain point persistence-v3.
- **Pinned today:** nothing beyond this entry.

## Q9. `ErrBudgetExceeded` answers 402, 400 or 500 by route

- **Where:** 402 on two launch routes, `LaunchAgentRun` and
  `DraftTestCases` (`internal/api/agent_handlers.go:351`, `:438`); 400 on
  three, `RunAutomationNow` (`:1117`), `LaunchTeamRun` (`:2196`) and
  `LaunchTestRunAgent` (`internal/api/suite_handlers.go:458`), which pass
  `err.Error()` through; 500 on delegation, `DelegateRun`
  (`internal/api/agent_handlers.go:950`).
- **Pinned by, named as:** S5d *(planned)*; `launchErrs402`,
  `launchErrs400` and `launchErrsDelegate` (X3) *(planned)*. Pain point
  api-suite-org-7.
- **Pinned today:** nothing beyond this entry.

## Q10. `RunFinished` is published twice, or never

- **Where:** a proposal-mode run publishes `RunFinished` when it finishes
  into `awaiting_approval` (`internal/domain/agentruns/agentruns.go:953`)
  and again when its proposals are resolved (`FinalizeIfResolved`,
  `:1178`); a cancel of a queued run (`RequestCancel`, `:1069-1080`)
  notifies the status change but never publishes it.
- **Pinned by, named as:** S5d events *(planned)*.
- **Pinned today:** nothing beyond this entry. The plan's non-goals keep the
  double publish (services-3).

## Q11. `NewHandler` rewires billing after `billing.Start`

- **Where:** `cmd/server/main.go:793` starts the billing service's
  goroutines, then `api.NewHandler` (`:797`) calls `SetSeatCounter` and
  `DefaultReturnURL` on it (`internal/api/handlers.go:372-377`; the plan's
  I17 row cites `:373-379` at `d11dee8`).
- **Pinned by, named as:** S4a `boot_steps.txt` (statement order) and the
  S4b billing profile's boot log *(planned)*; X12 keeps the point
  *(planned)*. Pain point boot-v1.
- **Pinned today:** S4a's `TestBootSteps` (`cmd/server/boot_steps_test.go`)
  fails if `(*internal/billing.Service).Start` moves past
  `inline internal/api.NewHandler` and its `SetSeatCounter` and
  `DefaultReturnURL` in `cmd/server/testdata/boot_steps.txt`; the billing-on
  boot log waits for S4b.

## Q12. `FRONTEND_URL` has two fallback chains; reports read raw `UPLOADS_DIR`

- **Where:** `FRONTEND_URL`, then `PUBLIC_URL`, then
  `http://localhost:3000` for email links and the handler
  (`cmd/server/main.go:539`, `:825`); `FRONTEND_URL`, then
  `http://localhost:3000` for the Google and OIDC sign-in configurations
  (`:739`, `:761`). The report service reads `UPLOADS_DIR` with
  `os.Getenv` (`internal/domain/reports/report.go:733`), not the server's
  `./uploads` default (`cmd/server/main.go:139`).
- **Pinned by, named as:** X10 keeps distinct fields *(planned)*. Pain point
  boot-3.
- **Pinned today:** S1's env-read ratchet counts the direct read in
  `internal/domain/reports`.

## Q13. Only `POST /api/v1/projects` enforces the project maximum

- **Where:** `checkProjectCount` (`internal/api/limits.go:326`) has one
  caller, `CreateProject` (`internal/api/handlers.go:1618`); the other
  project-creation paths do not count.
- **Pinned by, named as:** S5e's over-plan pass under the S4 tiers-on
  profile *(planned)*. Pain point api-requirements-v1.
- **Pinned today:** nothing beyond this entry.

## Q14. Some list endpoints encode `null` for an empty list

- **Where:** a repository that builds its result with `var result []*T`
  returns `nil` for no rows, and the handler encodes it as is: for example
  `GET /api/v1/agents` (`internal/persistence/postgres/agent_repository.go:125`,
  `internal/api/agent_handlers.go:163`) answers `null` for a workspace with no
  agents, while the project repository's lists start from an empty slice
  (`project_repository.go:62`) and answer `[]`.
- **Pinned by, named as:** S5a *(planned)*. Pain point api-suite-org-v6.
- **Pinned today:** nothing beyond this entry.

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
- **Pinned by, named as:** S5c's shared-bucket probe, which exercises the
  real call sites *(planned)*. Pain point api-core-v2.
- **Pinned today:** nothing beyond this entry.

## Q19. Three error-message conventions

- **Where:** one site answers `"Invalid request body"` with a capital I
  (`internal/api/handlers.go:2582`); 124 `writeJSONError` calls in
  `internal/api` pass `err.Error()` through (the same count as at
  `d11dee8`); 58 sites map any error to 404 (the plan's count at `d11dee8`,
  not re-counted here).
- **Pinned by, named as:** S5 *(planned)*; `decodeJSONMsg` (X1)
  *(planned)*; the other call sites stay untouched.
- **Pinned today:** S1's `invalid_request_body_literals` ratchet counts the
  lowercase literal (106).

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
