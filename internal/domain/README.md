# internal/domain: the business rules

One Go package per concept: its types, the rules that change them, the
`Repository` port that `internal/persistence/postgres` implements and the
`Service` the API and the background services call. A domain package
imports only other domain packages (K7), so it is tested without a database
or HTTP. Plan §7.3 (`docs/plans/codebase-refactor.md`) has the history.
`go run ./internal/tools/areas which <path>` names a file's area.

## Areas

Each package belongs to one area of `docs/areas.json` through its
`internal/domain/<package>/**` glob:

| Area | Packages |
|---|---|
| requirements-core | `artifacts/`, `attributes/`, `chatter/`, `embeddings/`, `links/`, `mentions/`, `products/`, `projects/`, `quality/`, `settings/`, `workitems/` |
| verification | `evidence/`, `vv/` |
| documents | `attachments/`, `baselines/`, `downloads/`, `exports/`, `reports/`, `snapshot/`, `templates/` |
| tenancy-identity | `invitations/`, `members/`, `orgs/`, `tokens/`, `users/` |
| agent-suite | `agentruns/`, `agents/`, `automations/`, `crewtemplates/`, `guided/`, `interviews/`, `proposals/`, `providers/`, `repoconns/`, `teams/` |
| runner-fleet | `hostedworkers/`, `runnersessions/`, `workerkeys/` |
| events-notifications | `events/`, `notifications/`, `pushsubs/` |
| community | `sharedproducts/`, `sharelinks/` |
| platform-http | `release/`, and this directory's `*.md` |

Billing has no domain package: it is `internal/billing` (see
`internal/notify/README.md`), and a workspace's plan and limits are
`orgs/`.

## Map

| Glob | What it holds |
|---|---|
| `*/*.go` named after the package or its entity (`artifacts/artifact.go`, `orgs/orgs.go`) | the types, `Repository`, the `Service` interface and `DefaultService`, built by `NewDefaultService` (or `NewService`) |
| `*/repository.go` | the `Repository` port where it has a file of its own (`projects/repository.go`) |
| other `*/*.go` | one concern each, such as `orgs/plans.go`, `orgs/limits.go`, `orgs/teams.go` (people-teams), `release/features.go`, `exports/reqif.go` |
| `events/events.go` | the domain event types: stored data that automations and the notifier match |
| `notifications/notifications.go` | the notification types the notifier creates |
| `release/features.go` | the feature keys that gate unreleased work by release channel |
| `*/*_test.go` | unit tests; none needs a database |
| `exports/testdata/import_fields.txt` | S9: every export field, carried or dropped by the import |

The glossary in `docs/areas.json` maps the UI's words to these names
(workspace = `orgs`, crew = `teams`, runner key = `workerkeys`).

## Invariants (plan §3) that bind here

- **K7 layering.** No import of `internal/api`, `internal/persistence` or an
  app service; a port is declared here and `cmd/server` wires it. The
  import edges are frozen in `internal/archtest/ratchets.json`.
- **I10 domain events.** Type strings, payload keys and their Go value
  types, and the actor strings are behavior: payloads stay maps, since
  automations and `internal/notify/membership.go` read the Go types.
- **I15, I16 formats and stored data.** Export, import and report formats;
  snapshot and proposal payload JSON; `links_snapshot` (quirk Q4).
- **I24 vocabularies.** Go is the source of each vocabulary the frontend
  copies (link rules, statuses, plans, error codes, gap labels, event
  types); today's drift is pinned, not fixed.
- **I25.** `release/features.go` changes only with a release note; a
  refactor never touches it.
- **R8.** A moved or aliased type keeps its name, so JSON decode errors keep
  their text.

## Recipes

**Add a domain package.**
1. Create `internal/domain/<name>/` with its types, `Repository` and
   `Service`; import only domain packages.
2. Claim it with `internal/domain/<name>/**` in its area in
   `docs/areas.json` (K15); check with
   `go run ./internal/tools/areas which internal/domain/<name>/<name>.go`.
3. Its new import edges go in `import_edges` of
   `internal/archtest/ratchets.json`, by hand, in review;
   `go test ./internal/archtest` names any it misses.
4. Implement the repository (`internal/persistence/postgres/README.md`),
   build the service in its stage (`cmd/server/README.md`) and expose it
   (`internal/api/README.md`).

**Add a domain event type.** A constant in `events/events.go` and its entry
in `pinnedEventTypes` (`events/event_types_test.go`); publish it from the
API (`internal/api/publish.go`) or a service. Then regenerate S6's payload
golden (`internal/api/README.md`) and S13's vocabulary:
`UPDATE_GOLDEN=1 go test ./internal/vocabparity -count=1 -run '^TestVocabulary$'`.
The frontend's copies follow (`frontend/src/README.md`).

**Add a field to an exported type.** Map it in `createProjectFromExport` or
`importArtifactsAndLinks` (`exports/export.go`) if an import should carry it,
then regenerate:
`UPDATE_GOLDEN=1 go test ./internal/domain/exports -count=1 -run '^TestImportFields$'`
and the S9 format goldens (`internal/api/README.md`).

**Change a vocabulary** (a link rule, status, plan, error code): regenerate
`contracts/vocab.json` with the `TestVocabulary` command above and update
the TypeScript copy; `vocabParity.test.ts` names any copy that disagrees.

A new notification type: `internal/notify/README.md`. A feature key: the
root `CLAUDE.md` ("Deployment").

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S1** archtest: import edges, K7 layering, client binaries (the domain
  packages `cmd/agentd` and `cmd/openv-mcp` link) and domain reflection.
  `go test ./internal/archtest -run 'TestArchitecture/(import|K7|client|domain)'`
- **S6** `TestEventTypesArePinned`: the event type strings (I10).
  `go test ./internal/domain/events -count=1`
- **S9** `TestImportFields`: `exports/testdata/import_fields.txt` (I15).
  `go test ./internal/domain/exports -count=1 -run '^TestImportFields$'`
- **S10** `TestEveryNotificationTypeHasAContentGolden`: each notification
  type has its golden in `internal/notify/testdata/notifications/`.
  `go test ./internal/domain/notifications -count=1`
- **S13** `TestVocabulary`: `contracts/vocab.json` from the Go catalogues
  (I24). `go test ./internal/vocabparity -count=1`
- **I25** `TestEmbeddedNotesParse`: the release notes the server embeds.
  `go test ./internal/domain/release -count=1`

Everything: `go test -count=1 ./internal/domain/... ./internal/vocabparity ./internal/archtest`.
