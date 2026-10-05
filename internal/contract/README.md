# internal/contract: the generated cross-language contract

Refactor plan steps X4a and X5 (`docs/plans/codebase-refactor.md`, §6.7). A
generator, written as Go tests, reads the vocabularies the frontend uses from
their Go catalogues and writes two checked-in files with the same values
(X4a), and writes the JSON shape of the Go types the API sends most as
TypeScript interfaces (X5, [below](#wirets-the-json-shapes-x5)):

- `frontend/src/generated/contract.ts`: one `export const ... as const` per
  vocabulary, with the union type derived from it (`FeatureKey`,
  `DomainEventType`, `SseEventName`, `ApiErrorCode`, `LinkType`,
  `ArtifactTypeValue`, `ArtifactStatusValue`, `WorkspacePlan`, `VvGapKey`,
  `AgentProvider`);
- `internal/contract/testdata/contract.json`: the same vocabularies as JSON.

| Vocabulary | Read from |
|---|---|
| `feature_keys` | `release.Registry`, in registry order |
| `event_types` | the const block of `events.ArtifactCreated`, in declaration order |
| `sse_events` | `contracts/sse-events.json` (S6), sorted; `TestSSEContract` in `internal/api` writes it from the Go sources |
| `error_codes` | the const block of `api.ErrCodeEmailUnverified` |
| `link_rules` | `links.GetLinkTypeRules()`, with the Go text |
| `artifact_types` | `artifacts.TypeCatalog()` |
| `artifact_statuses` | the const block of `artifacts.StatusDraft`, each with the statuses `artifacts.CanTransition` allows next |
| `plans` | the const block of `orgs.PlanSingle`, the legacy aliases included |
| `gap_labels` | the two gap tables of `internal/domain/reports`, which must be equal, keyed by `vv.GapReport`'s JSON names |
| `providers` | `providers.KnownProviders()`, in display order |

Like `internal/vocabparity` (S13), whose readers it copies, the package holds
only test files: it imports the exported catalogues and parses the rest by
declaration name, so it adds no import edge to
`internal/archtest/ratchets.json`.

## Tests

- `TestContract` fails when either file differs from what the catalogues
  give now, and `TestWire` when `wire.ts` differs from what the Go types
  write. Regenerate all three after a deliberate change:
  `UPDATE_CONTRACTS=1 go test ./internal/contract/...` (only `1`
  regenerates; any other value compares).
- `TestContractAgreesWithS13` holds every vocabulary that
  `contracts/vocab.json` also lists equal to it, value for value and in
  order, so the two goldens cannot drift apart; the one vocabulary it does
  not list (`sse_events`) names its own golden in `fromS6`.
- `TestPhoneAuditFeatureKeys` (X4b) holds the feature keys that
  `e2e/tools/phone-audit.js` turns on for its mocked workspace to
  `release.Registry`: the audit is plain JavaScript, outside the type check
  that holds the frontend's keys to `FeatureKey`.

Both generated files are on the refactor guard's golden list (entry
`X4a, X5` in `scripts/refactor/refactor_guard.py`): changing them is a
behavior change that needs a release note, and a refactor pull request may
not.

## Who reads it

`contract.ts`, since refactor plan X4b: `FeatureKey` types the key of
`useFeature` (`frontend/src/hooks/useFeature.ts`) and every `*_FEATURE`
const, in `frontend/src/features.ts` or beside the module that owns its
gate. The TypeScript names avoid every
name S13's `vocabParity.test.ts` locates a hand-written copy by
(`EVENT_TYPES`, `PLANS`, `GAP_LABELS`, `ArtifactStatus`, ...), since a
second declaration of one would leave that reader unable to tell which is
the copy. `wire.ts`: `frontend/src/api/wireCompat.ts`, by type only.

## wire.ts: the JSON shapes (X5)

`frontend/src/generated/wire.ts` has one interface per Go type, named
`Wire<Name>`, in this order (`wireTypes` in `wire_test.go`):

| Go type | Interface | Hand-written twin (`frontend/src/api/types`) |
|---|---|---|
| `artifacts.Artifact` | `WireArtifact` | `Artifact` |
| `links.Link` | `WireLink` | `Link` |
| `projects.Project` | `WireProject` | `Project` |
| `orgs.Org` | `WireOrg` | `Org` |
| `users.User` | `WireUser` | `User` |
| `agentruns.Run` | `WireRun` | `AgentRun` |
| `agents.Agent` | `WireAgent` | `AgentDef` |
| `attachments.Attachment` | `WireAttachment` | `Attachment` |
| `baselines.Baseline` | `WireBaseline` | `Baseline` |
| `notifications.Notification` | `WireNotification` | `AppNotification` |
| `events.Event` | `WireEvent` | `DomainEvent` |

A struct one of them carries that is not in the list gets its own interface
after them, `Wire<its Go name>` (today `WireBilling`, for `orgs.Billing`).

**Field rules**, as `encoding/json` writes (`jsonFields` follows its
`typeFields`): the `json` tag's name, else the Go name; `-` and unexported
fields skipped; an embedded struct without a tag name flattened, the
shallowest name winning, then the tagged one, and a tie hiding both. A key
is optional (`?`) where `omitempty` can leave it out (any kind but a
struct), under `omitzero`, or when promoted through an embedded pointer.
Types: `time.Time` and named string types `string`; numbers `number`;
`json.RawMessage` and interfaces `unknown`; a pointer its element
`| null`; a slice `T[] | null` and a map `Record<string, T> | null`, since
a nil one writes `null`, whatever the code that fills it does (a
hand-written type that relies on a non-nil one says so in
`COMPAT_EXCEPTIONS`); `[]byte` a base64 `string | null`; the `,string`
option `string`. Where `omitempty` leaves a nil pointer, slice or map out,
the key is optional and the `| null` goes. A field whose type has its own
`MarshalJSON` or `MarshalText`, an anonymous struct, or a map with
non-string, non-integer keys fails the test rather than guess.

**Marshal self-check.** For every interface, the generator marshals a
pointer to a sample of the type (every field filled, so `omitempty` leaves
nothing out; a `MarshalJSON` on either receiver runs) and requires the keys
of the JSON object to be the fields' keys plus the ones `wireAdded` declares,
each value of the JSON kind the emitted type allows. The emitted keys are
the sample's, in the order it writes them. A key a `MarshalJSON` adds must
be declared in `wireAdded` with the Go type of its value (today
`Attachment.MarshalJSON`'s `kind`, an `attachments.Kind`); an undeclared
added key, a field key the JSON lacks, or an entry the JSON does not carry
fails, naming the key. `TestWireEmitterFollowsEncodingJSON` runs the rules
over a fixture with the harder cases, and `TestWireSelfCheckRefuses` shows
each refusal.

**wireCompat.ts.** `frontend/src/api/wireCompat.ts` is types only and
imported by nothing, so the build erases it (S12b's identity holds), and
`npx tsc --noEmit -p .` checks it. For each pair it requires every key of
the hand-written interface to be a key of its twin, and the twin's value
type to be assignable to the hand-written one (`Wire[K] extends Hand[K]`:
the frontend reads what the server sends). `COMPAT_EXCEPTIONS` names each
difference today with `[excuse, reason]`: `narrowed` (the hand-written type
accepts part of what Go allows, and must still be assignable to it) or
`client-side` (a key another Go type adds, which must stay absent from the
twin). An entry no longer needed fails too.

**After changing one of these Go types:** run
`UPDATE_CONTRACTS=1 go test ./internal/contract/...`, then
`cd frontend && npx tsc --noEmit -p .`, which names each hand-written key
that no longer matches. `wire.ts` is on the refactor guard's golden list,
so a refactor pull request never changes it.
