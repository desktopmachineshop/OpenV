# internal/api: the HTTP layer

One Go package and one `Handler` type serve every `/api/v1` route on the one
gorilla/mux router that `cmd/server/http.go` builds. Handlers decode, guard,
call a domain service and write the answer; they never import
`internal/persistence` (K7). Plan §7.2 (`docs/plans/codebase-refactor.md`)
has the history. `go run ./internal/tools/areas which <path>` names a
file's area.

## Areas

Every area but frontend-shell and tooling has files here, matched by these
globs of `docs/areas.json`:

| Area | Globs here |
|---|---|
| requirements-core | `artifact_*.go`, `attribute_*.go`, `chatter_*.go`, `embedding_*.go`, `link_*.go`, `managed_link_edits*.go`, `meta_*.go`, `product_profile_*.go`, `project_handlers*.go`, `quality_*.go`, `reference_party_*.go`, `review_*.go`, `search_*.go`, `suite_*.go`, `work_item_*.go` |
| verification | `evidence_*.go`, `vv_*.go` |
| documents | `attachment_*.go`, `baseline_*.go`, `download_*.go`, `project_io_*.go`, `project_snapshot*.go`, `template_*.go` |
| tenancy-identity | `auth*.go`, `avatar_*.go`, `cookies*.go`, `default_workspace_*.go`, `email_verification_*.go`, `invitation_*.go`, `limits*.go`, `oidc_*.go`, `org_*.go`, `password*.go`, `project_team_access_*.go`, `registration_policy*.go` |
| agent-suite | `agent_*.go`, `ai_map*.go`, `automation_*.go`, `crew_*.go`, `guided_*.go`, `interview_*.go`, `proposal_*.go`, `provider_*.go`, `public_interview_*.go`, `repo_connection_*.go` |
| runner-fleet | `connector_*.go`, `hosted_runner_*.go`, `runner_*.go`, `worker_*.go` |
| events-notifications | `domain_event_*.go`, `event_names*.go`, `notification_*.go`, `publish*.go`, `push_*.go`, `sse*.go` |
| community | `share_*.go`, `shared_product_*.go` |
| billing | `billing_*.go` |
| platform-http | `admin_*.go`, `compression*.go`, `feature_*.go`, `handlers.go`, `health_*.go`, `httperr*.go`, `middleware_*.go`, `ratelimit*.go`, `release_*.go`, `requestlog*.go`, `respond*.go`, `routes*.go`, `security_headers*.go`, `testdata/**`, `*.md` |

A new file whose name no glob matches needs one in `docs/areas.json` (K15).

## Map

| Glob | What it holds |
|---|---|
| `handlers.go` | `HandlerDeps` (every service the layer uses), `Handler` and `NewHandler`: dependencies only |
| `routes.go` | `RegisterRoutes`, the ordered list of registrar calls; the order is the contract (I2) |
| `*_handlers.go` | one area each (K1): its handlers and its registrars, `register<Area>Routes`, the only place this package registers routes |
| `respond.go`, `httperr.go`, `authz.go`, `publish.go`, `cookies.go`, `middleware_*.go` | the K3 homes: JSON out (`respondJSON`), the error envelope and writers, every `require*` guard, domain event publishing, the session and sign-in cookies, middleware |
| `authmiddleware.go`, `compression.go`, `requestlog.go`, `security_headers.go` | the middleware `cmd/server/http.go` chains (I6) |
| `ratelimit.go` | the rate limiters handlers spend; some buckets are shared across endpoints (quirk Q18) |
| `limits.go` | plan limits, the read-only gate and `alwaysWritable`, its exemption wrapper |
| `sse.go` | `SSEHub` and `ServeStream`: the event streams and their keys (I9) |
| `event_names.go` | the actor and entity names the activity log shows beside each domain event |
| other non-test files | area helpers that are not handlers, such as `ai_map.go`, `managed_link_edits.go`, `proposal_appliers.go`, `attachment_safety.go` |
| `testkit_test.go` | `newTestHandler` and the shared fakes (K6) |
| `route_*_test.go` | S2: the route inventory, binding, overlaps and guards |
| `sse_*_test.go`, `event_payload_*_test.go` | S6: the SSE contract and the event payload Go types |
| `formats_*_test.go`, `proposal_payloads_test.go` | S9: export, report, download and proposal payload goldens |
| `testdata/*.txt` | `routes.txt`, `route_handlers.txt`, `route_overlaps.txt`, `route_guards.txt`, `event_payload_types.txt` |
| `testdata/formats/*.txt`, `testdata/proposal_payloads/**` | the S9 goldens |

## Invariants (plan §3) that bind here

- **I1, I2** the route set and its registration order and binding, with the
  three same-method overlaps of the agents block; one router (K2).
- **I3** 401, 403 or 404 per route and identity: a resource the caller
  cannot reach answers exactly as one no row has. Each handler keeps its own
  order of guard, lookup and decode, which decides which status wins.
- **I4, I5** status codes and body bytes (key order, trailing newline,
  `null` vs `[]`), the `{error, code}` envelope, its codes in `httperr.go`
  and every message. Quirk Q1: a bare encode that sets no `Content-Type`
  stays bare.
- **I9, I10** SSE event names and stream keys; domain event types, payload
  keys and their Go value types; the actor each publisher stamps.
- **I12** the worker wire in `worker_protocol_handlers.go`: claim body keys,
  `auth` always an object, 204 on an empty claim.
- **R8** a decode error that reaches a response names Go types: moved types
  keep their names.

## Recipes

**Add an endpoint to an area.**
1. Write the handler on `*Handler` in the area's `*_handlers.go`: guard with
   a `require*` from `authz.go`, write errors with `httperr.go`, JSON with
   `respondJSON`, and publish events through `publish.go`.
2. Add one route line to that file's registrar: the full path template and
   `.Methods(...)`. Wrap the handler in `h.alwaysWritable(...)` only if it
   must work on a read-only plan.
3. Regenerate the route goldens:
   `UPDATE_ROUTES=1 go test ./internal/api -count=1 -v -run 'TestRouteInventory|TestRouteBinding'`.
   This changes behavior, so the pull request carries a release note.
4. Test through `newTestHandler(t, func(h *Handler) { ... })` with the fakes
   in `testkit_test.go`.
5. The S5e matrix sends every route of `testdata/routes.txt`; regenerate its
   goldens with a database (`cmd/server/README.md`). The client call goes
   in `frontend/src/api/` (`frontend/src/api/README.md`).

**Add an API area.** A new `<area>_handlers.go` with its registrar, one call
in `RegisterRoutes` (its position decides which of two overlapping
templates serves a path), a glob in
`docs/areas.json` if no area claims the name, then the steps above. A new
service: one `HandlerDeps` field, plus the `Handler` field and its copy in
`NewHandler` until M14, plus one line in `cmd/server/wire_http.go` (K5).
Scaffold: `go run ./internal/tools/scaffold api-area <name>`

**Publish a new domain event.** Add the type in `internal/domain/events`
(see `internal/domain/README.md`), publish it with `h.publish` or
`h.publishOrgEvent`, then regenerate S6's payload golden:
`UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^TestEventPayloadTypes$'`.

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S2** `TestRouteInventoryIsBackwardCompatible` and `TestRouteBinding`:
  the four route goldens (I1, I2, I3).
  `go test ./internal/api -count=1 -run 'TestRouteInventory|TestRouteBinding'`
- **S6** `TestSSEContract` and `TestEventPayloadTypes`:
  `contracts/sse-events.json` and `testdata/event_payload_types.txt` (I9, I10).
  `go test -short ./internal/api -count=1 -run '^(TestSSE|TestEventPayload)'`
- **S9** `TestFormatsGolden` and `TestProposalPayloadsGolden`: the S9 goldens
  (I15, I16).
  `go test ./internal/api -count=1 -run '^(TestFormatsGolden|TestProposalPayloadsGolden)$'`
- **I3 unit tests**: what the matrix cannot send, and the read-only gate's
  DELETE side.
  `go test ./internal/api -count=1 -run '^(TestAResourceTheCallerCannotReachAnswersAsOneNoRowHas|TestAnotherWorkspacesKeyOrRunAnswersAsForAnIDNoRowHas|TestACrewLaunchAnswersWhoMayNotKnowOfItAsForACrewNoRowHas|TestAReadOnlyWorkspaceStillRevokesAccess)$'`
- **S5** the tour and the S5e matrix: every route's bytes and authorization,
  run from `cmd/server` (`cmd/server/README.md`).
- **S1** archtest: one router, HandleFunc outside registrars, raw JSON
  encodes, invalid request body literals, require helpers outside authz, K3
  helper homes, Handler literals in tests, R8 decode errors.
  `go test ./internal/archtest`
- **S12** `clientRoutes.test.ts`: every frontend call is a route of
  `testdata/routes.txt` (I23).
  `cd frontend && npx vitest run src/arch/clientRoutes.test.ts`

Each golden's failure prints its regenerate command; a refactor never runs
it (R3), and any change to `route_guards.txt` counts as an authorization
change.
