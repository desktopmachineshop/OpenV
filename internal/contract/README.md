# internal/contract: the generated cross-language contract

Refactor plan step X4a (`docs/plans/codebase-refactor.md`, §6.7). A
generator, written as Go tests, reads the vocabularies the frontend uses from
their Go catalogues and writes two checked-in files with the same values:

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
  give now. Regenerate both after a deliberate change:
  `UPDATE_CONTRACTS=1 go test ./internal/contract/...` (only `1`
  regenerates; any other value compares).
- `TestContractAgreesWithS13` holds every vocabulary that
  `contracts/vocab.json` also lists equal to it, value for value and in
  order, so the two goldens cannot drift apart; the one vocabulary it does
  not list (`sse_events`) names its own golden in `fromS6`.

Both generated files are on the refactor guard's golden list (entry
`X4a, X5` in `scripts/refactor/refactor_guard.py`): changing them is a
behavior change that needs a release note, and a refactor pull request may
not.

## Who reads it

Nothing yet. Refactor plan X4b moves the frontend's hand-written copies onto
`contract.ts` (`useFeature(key: FeatureKey)`, typed event filters, SSE and
error-code constants); X5 adds `generated/wire.ts` from the same generator.
The TypeScript names avoid every name S13's `vocabParity.test.ts` locates a
hand-written copy by (`EVENT_TYPES`, `PLANS`, `GAP_LABELS`,
`ArtifactStatus`, ...), since a second declaration of one would leave that
reader unable to tell which is the copy.
