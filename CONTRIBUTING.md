# Contributing to OpenV

Thanks for considering a contribution — issues, docs, and code are all
welcome.

## Ground rules

- Open an issue before large changes so the approach can be agreed first.
- Match the surrounding code's style; run the checks CI runs
  (`go vet`, `gofmt`, backend tests, `tsc` + frontend build — see
  `.github/workflows/ci.yml`) before opening a PR.
- Keep PRs focused: one change per PR.

## Release notes

`RELEASE_NOTES.md` at the repository root is read by the people who use
OpenV, so every pull request adds at least one bullet under `## Unreleased`
saying what they will notice: what they can now do, what looks different,
what they no longer have to do. Write for a workspace member, not a
developer; the implementation belongs in the pull request. CI refuses a
pull request that adds no bullet; a change nobody can see (CI, refactors,
internal docs) carries the `no-release-notes` label instead. Adding or
removing that label re-runs the check, so it can go on after the pull
request is opened.

Every bullet goes under one of three group headings, because that is how a
reader tells them apart:

```markdown
## Unreleased

### New features

- Export a traceability matrix to Excel from the V&V tab.

### Maintenance updates

- Large baselines load faster.

### Bug fixes

- Member avatars keep their shape beside a long name on a phone.
```

`### Maintenance updates` is for something a member can still notice — it is
faster, clearer, better documented — not for work with no visible effect;
that is what the `no-release-notes` label is for. A bullet under no group is
refused.

The group also decides the version. Promotion cuts the Unreleased bullets
into a new section headed by a semantic version and the date: anything under
*New features* makes it a minor release, maintenance and fixes alone make it
a patch, and a major release is asked for explicitly when the workflow is
run. The app announces that version to every account and shows it under
What's new (see `docs/railway.md`, "Release pipeline").

The group also decides who sees the change when (`docs/release-policy.md`).
Maintenance updates and bug fixes reach every workspace with the release
that carries them. So does a new feature on the nightly channel, but a
stable-channel workspace sees it only once the monthly stable release it
has turned on is that release or a later one. A new feature therefore
registers a key in `internal/domain/release/features.go` with the version
it ships in (`scripts/release_notes.py next` prints it) and gates its code
path and UI on that key (server: `featureEnabled`; client: `useFeature`).

## Refactor PRs

The refactor programme ([`docs/plans/codebase-refactor.md`](docs/plans/codebase-refactor.md))
changes nothing a person or a program can observe, and a CI job, *Refactor
guard* (`.github/workflows/refactor-guard.yml`, checks in
`scripts/refactor/refactor_guard.py`), checks that mechanically. Read the
plan's §4 and §8 before a refactor step; this section is the checklist.

### Labels

| Label | Meaning |
|---|---|
| `refactor` | A refactor step. Every refactor pull request carries it and `no-release-notes`, plus one label per class its commits declare. |
| `refactor:move` | Has class A commits. |
| `refactor:test` | Has class C commits. |
| `refactor:tooling` | Has class T commits. |
| `refactor:script` | Has class R commits. |
| `behavior-change` | Applied by the maintainer only, to a pull request that deliberately changes a golden without a release note (a test-only pull request extending one). Never together with a `refactor` label. |

A pull request is a refactor for the guard when it carries `refactor` or any
`refactor:*` label.

### Classes and trailers

Each commit of a refactor pull request declares one verification class in a
trailer, in the last paragraph of its message with the other trailers. A step
with two classes lands as one commit per class, and class E is never mixed
with another class in one pull request.

```text
refactor(api): M6 move the artifact handlers to their own files

Refactor-Class: A
Signed-off-by: Your Name <you@example.com>
```

| Class | What it may change | What the guard checks per commit |
|---|---|---|
| A pure move | Go and `frontend/src` TypeScript sources, a lowered `ratchets.json`, the `declmove` spec | `go run ./internal/tools/declhash -base <parent> -head <commit> <package dirs>` reports every declaration identical; for TypeScript, `tsdeclhash --no-module` and `tsmovecheck` pass |
| B extract in place, D package move | anything but guard code | no golden changed; D may add the `import_edges` and `client_domain_deps` entries of the package it creates (edges into it, and out of it only to what its importers already imported) |
| C test only | test files (`*_test.go`, `*.test.ts(x)`, `*.spec.ts(x)`, `*.test.mjs`, `*_test.py`, `e2e/tests/**`), `frontend/src/arch/**`, `frontend/src/test/**`, and **new** files under `testdata/`, `__snapshots__/` or `contracts/` | nothing else changed |
| T tooling | CI workflows, the `Makefile`, Dockerfile build commands, lint config, PR and issue templates, docs (`*.md`, `docs/**`), `.git-blame-ignore-revs`, and files under `internal/tools/`, `internal/archtest/`, `internal/contract/`, `scripts/`, `frontend/scripts/` and `frontend/src/generated/`; also TypeScript under `frontend/src` that ships and that the production build erases (type-only code, such as X5's assertions; a test, `src/arch/**`, `src/test/**` or test data there is class C) | nothing else changed; for TypeScript under `frontend/src`, S12b's build identity (below) requires byte-identical `build/assets/*.js` and `*.css` from the base and the head, which proves what the production build emits, so code it drops (a branch only the dev server takes) is the reviewer's to read; any other `frontend/src` file (a stylesheet, say) fails |
| R scripted rewrite | what the script writes | a `Refactor-Script: <path> [args…]` trailer names a script that is already in the commit's parent (commit it, and any tool it drives, in an earlier class T commit); the job runs it in a scratch worktree of the parent and requires the result to equal the commit byte for byte |
| E semantic extraction | anything | one or more `Refactor-Characterization: <test file>` trailers name tests that are on the base and unchanged by the pull request; two reviewers, one of them the maintainer |

A merge of `master` into the branch needs no trailer when its tree is what
git's own merge of its two parents gives (`git merge-tree`), apart from files
that take `master`'s copy. A merge that carries a change of its own, such as
a conflict resolved by hand or settled with the branch's side
(`git checkout --ours`, `git merge -s ours`), which drops what `master`
changed there, is checked like a commit and needs a trailer; a move or
scripted rewrite is never resolved in a merge but regenerated on the latest
`master` (R4).

### What the Refactor guard job checks

On **every** pull request, including every label change:

1. **Goldens.** A golden on the list (`GOLDEN_LIST` in
   `refactor_guard.py`, 22 entries) that is modified or deleted, a rename
   included, needs a `RELEASE_NOTES.md` bullet under `## Unreleased` or the
   maintainer's `behavior-change` label. Adding a golden is fine. An inline
   snapshot (`toMatchInlineSnapshot`, `toThrowErrorMatchingInlineSnapshot`)
   added to a test fails: vitest goldens are file snapshots under
   `__snapshots__/`.

On a **refactor** pull request, also:

2. A modified or deleted golden fails whatever the notes say, and so does the
   `behavior-change` label. So does a modification or deletion under
   `testdata/`, `__snapshots__/`, `frontend/src/generated/` or
   `docs/exports/*.json`, or of a protected path: `RELEASE_NOTES.md`,
   `internal/domain/release/features.go`, `go.mod`, `go.sum`,
   `Dockerfile.api` (once M1 builds it by package path), the project
   templates the API image serves (`examples/**`), and the frontend image's
   files (`frontend/package.json` and `package-lock.json`, the npm
   counterpart of `go.mod` and `go.sum`; `frontend/public/**`, `index.html`,
   `docker-entrypoint.d/**`, `nginx.conf`, `security-headers.conf`,
   `openv-nginx/**`, `Dockerfile.prod`, `railway.json`, `vite.config.ts`).
   Guard code (`GUARD_CODE`: the Phase 0 guard tests, `internal/archtest/**`,
   `frontend/src/arch/**` with S12b's `cssOrder.test.ts` and
   `sizeBudget.test.ts`, S12b's
   `frontend/scripts/bundle-check.mjs` and its test, the boundary rules in
   `frontend/eslint.config.js`,
   the move proofs, M4's generator and proof `internal/tools/stageextract`
   and `internal/tools/movecheck` (S14c), the migration generator that
   writes M10 (`internal/tools/liftmigrations/**`, S14d), M11a's split proof
   `internal/tools/splittools` (S14e), F1's generator
   `frontend/scripts/tsdeclmove.mjs` with its tests (S14f), M14's generator
   `scripts/refactor/embed_deps.sh` with the `internal/tools/embeddeps`
   rewriter it drives, and this guard)
   may be modified or deleted only in a
   class C or T commit that modifies or deletes no golden. The plan asks such
   an edit to be green against the production code of its parent; since a C
   or T commit cannot change production code, that is what the pull
   request's own CI shows, and the guard does not re-run tests per commit.
   In a commit of any class, an entry of `internal/archtest/ratchets.json`
   raised or added fails (apart from class D's new-package entries, and the
   key of a new rule added by a class T commit that also changes the archtest
   rules), and so does an entry added to S12's lint allowlists
   (`COMPONENTS_IMPORTING_VIEWS`, `EVENT_SOURCE_SITES`), a raised
   `CEILING` in `frontend/src/arch/errorChains.test.ts`, or a raised budget
   or an entry raised or added among S12b's K14 size budgets in
   `frontend/src/arch/sizeBudget.test.ts` (`FILE_BUDGET`,
   `COMPONENT_BUDGET`, `OVER_1000`, `FILE_CEILINGS`, `COMPONENT_CEILINGS`).
   Lowering or removing any of those is allowed in any class and is not a
   guard-code edit. The allowlists and ceiling maps hold only entries of one
   form, a quoted key and a decimal integer (a list of quoted paths in
   `COMPONENTS_IMPORTING_VIEWS`), and `//` comments, which is all the guard
   reads: an unquoted or computed key, a spread, arithmetic or a hex number
   there fails in any class.
   **Build identity (S12b).** When the pull request changes a file under
   `frontend/src` that ships (not a test, `src/arch/**`, `src/test/**`,
   `testdata/`, a snapshot or a `.d.ts`), or adds or changes a Vite or
   PostCSS config beside `frontend/package.json` (`vite.config.*`,
   `postcss.config.*`, `.postcssrc*`), the job exports `frontend/` at the
   base and at the head, links in its `npm ci` install, runs `npm run build`
   for each, and requires every chunk's `build/assets/*.css` byte-identical,
   lazy chunks included, so a reordered stylesheet import anywhere (in
   `ModuleView`'s graph, say) fails, naming the chunk and the first bytes
   that differ. When a class T commit changes that TypeScript, every
   chunk's `build/assets/*.js` must be identical as well; the check is the
   pull request's, base against head, so such a commit shares its pull
   request only with commits that leave the build as it is. `.map` files are
   left out (they embed the sources), and a build that fails or writes no
   `build/assets` fails the check. A chunk that differs only in the hashed
   file names of the chunks it loads is listed after the change that
   renamed them. A file the other build emits with the same bytes under
   another chunk's name is no difference (the bundler names a chunk several
   modules share after one of them, so an extraction can rename it), and
   the job notes it; a file that pairs with none of the other build's is
   named as emitted by one side only.
   Something the pull request itself adds (a golden and the guard test that
   writes it, say) is not frozen until it merges, so a later commit of the
   same pull request may still refine it. A file of the base that one commit
   deletes and a later one re-adds is judged against the base: re-adding
   guard code counts as editing it unless the bytes are the base's, and a
   re-added `ratchets.json`, allowlist or ceiling may not be above the
   base's. The same holds for a single entry: one removed in one commit and
   re-added higher in a later one counts as raised. Then the class checks
   above run on every commit.
3. **Stale ratchets.** The job runs
   `UPDATE_RATCHETS=1 go test -count=1 -run '^TestArchitecture$' ./internal/archtest`;
   a refactor pull request fails if that changes `ratchets.json`, so commit
   the tightened file. Other pull requests get a warning.

The job's summary lists each commit with its class. Every failure names the
rule, the commit and the path, and says how to fix it: split the commit,
relabel, add the bullet, or ask the maintainer for `behavior-change`.

The lists live at the top of `refactor_guard.py`, each entry naming the plan
step that owns it. Entries for steps not yet merged are the patterns the plan
names, and an empty slot marks a step whose guard files the plan does not
name yet: each later S-step adds its goldens and guard files to those lists
in a class T commit of its own pull request. X2b fills
`X2B_CALL_SHAPE_CHANGES`, a named golden exception (plan §8.3), in its
class E commit; until then every `route_guards.txt` change is an
authorization change. X14b fills `X14B_IMPORT_EDGES`, a named ratchet
exception, the same way: in a class E commit, an `import_edges` entry of
`ratchets.json` that adds only edges listed there, into packages the base
has, is not a raise. `X11B_IMPORT_EDGES`, a second ratchet exception, is
filled the other way, in a class T commit ahead of X11b, with exactly the
edges X11b adds: `internal/api`'s into `internal/domain/traceability`, the
package X11b creates, and that package's own. In a class E commit, an
`import_edges` entry that adds only edges listed there, into packages the
base has or the commit creates, is not a raise; no class edits that list as
data, so X11b's commit leaves it as it is. X6, X7b, X7c and X10b each fill `BOOT_STEPS_CHANGES`,
the other named golden exception, with the `cmd/server/testdata/boot_steps.txt`
lines they replace, add or remove: a change to that file made in class E
commits that is exactly those changes, in list order, is not a golden change.

The job judges a pull request with the **base's** copy of
`refactor_guard.py` (its *Script self-test* step tests the pull request's
own copy), so a pull request's edits to the lists and rules take effect only
once it merges: a pull request that drops a guard-code or protected-path
entry is still judged by that entry. A step that needs a new exception lands
it in an earlier pull request, as S12b landed the class T rule for
TypeScript the build erases before X5 uses it. `X2B_CALL_SHAPE_CHANGES`, `X14B_IMPORT_EDGES` and
`BOOT_STEPS_CHANGES` are the lists the job reads from the pull request, since X2b, X14b and the boot-steps
steps fill them in their own; it reads `X11B_IMPORT_EDGES` there too, though X11b's own pull request leaves
it as the base has it. `make check` does the same with the merge base's copy.

### Goldens are regenerated only for a behavior change

A failing golden means the change is not a refactor: stop, and fix the code,
or drop the refactor labels and make it a release-noted behavior change. In
a behavior-changing pull request, regenerate with the command the failure
prints:

- routes: `UPDATE_ROUTES=1 go test ./internal/api -count=1 -v -run 'TestRouteInventory|TestRouteBinding'`
- stored data (S3): `UPDATE_GOLDEN=1 go test ./internal/persistence/postgres -count=1 -run '^(TestMigrationFreeze|TestEveryBootFreeze|TestSchemaGolden|TestPurgeCatalog)$'`
  (against a server with pgvector)
- boot (S4): `OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootSmoke$'`
  for the boot and middleware probes (a server with or without pgvector),
  `-run '^TestBootProfiles$'` for the same under each environment profile
  (cookies, self-hosted, tiers, registration, limits, build SHA, billing),
  `-run '^TestBootMisconfigured$'` for the boots that must refuse to start,
  and `UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootSteps$'`
  for the order of `main()`'s wiring
- API tour (S5): `OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestTourS5aAttachments$'`
  for one area (each golden under `cmd/server/testdata/tour/<slice>/` names
  its test in `"test"`, and a failure prints its command; the V&V and suite
  slice's areas run as `TestTourS5b<Area>`, such as
  `-run '^TestTourS5bEvidence$'`, the identity and workspace slice's as
  `TestTourS5c<Area>`, such as `-run '^TestTourS5cBilling$'`, the agents
  and worker wire slice's as `TestTourS5d<Area>`, such as
  `-run '^TestTourS5dWorkerWire$'`, and the authorization matrix slice's as
  `TestTourS5e<Area>`, such as `-run '^TestTourS5ePhantomMatrix$'`),
  `-run '^TestTourS5e'` for one slice, or
  `-run '^TestTour'` for every area (a server with or without pgvector); an
  area's run also rewrites its slice's `coverage.txt` and the union across
  slices, `cmd/server/testdata/tour/coverage.txt`, and
  `UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestTourCoverage$'`
  rewrites every slice's `coverage.txt` and the union alone, with no
  database. Two area commits combined conflict there: regenerate them, never
  merge them by hand
- SSE and event payloads (S6): `UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^TestSSEContract$'`
  and `-run '^TestEventPayloadTypes$'`
- Go↔TS vocabularies (S13): `UPDATE_GOLDEN=1 go test ./internal/vocabparity -count=1 -run '^TestVocabulary$'`
  for `contracts/vocab.json` (the vocabularies the frontend copies, read
  from the Go catalogues), then
  `cd frontend && UPDATE_GOLDEN=1 npx vitest run src/arch/vocabParity.test.ts`
  for `contracts/vocab-allowed-diffs.json`, the differences between those
  and their TypeScript copies that the change keeps (a fixed drift leaves
  it, a deliberate new one joins it)
- generated contract (X4a): `UPDATE_CONTRACTS=1 go test ./internal/contract/...`
  for `frontend/src/generated/contract.ts` and
  `internal/contract/testdata/contract.json`, the same vocabularies written
  from the Go catalogues as TypeScript and JSON; the vocabularies they share
  with `contracts/vocab.json` must equal it, so regenerate that first (S13,
  above)
- MCP and worker wire (S7): `UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run <Test>`
- notification content (S10): `UPDATE_GOLDEN=1 go test ./internal/notify -count=1 -run '^TestNotificationContent$'`
  for `internal/notify/testdata/notifications/<type>/` (each type's stored
  row, SSE frame, email and web push), then
  `cd frontend && npx vitest run src/components/NotificationBell.paths.test.tsx -u`
  for the bell's deep links beside them,
  `frontend/src/components/__snapshots__/NotificationBell.paths.txt`, which
  reads the Go goldens
- run failure outcomes and classes (S15a): `UPDATE_GOLDEN=1 go test ./internal/runner -count=1 -run '^(TestRunFailureClassesGolden|TestRunFailureTaxonomyGolden)$'`
  for `internal/runner/testdata/run_failures/` (what the runner reports
  for each terminal outcome of a run, and the taxonomy behind it)
- env vars and command lines (S8): `UPDATE_GOLDEN=1 go test ./internal/archtest -count=1 -run '^TestEnvInventory$'`
  for the inventory, `internal/archtest/testdata/env_vars.txt`;
  `UPDATE_GOLDEN=1 go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest ./cmd/agentd ./cmd/openv-mcp ./cmd/server ./internal/api ./internal/billing ./internal/domain/users ./internal/hosting ./internal/notify`
  for the parse table beside it, `env_parse.txt`, which each package's
  `TestEnvParse` writes its sections of (a failure prints the command for
  its own package); and `UPDATE_GOLDEN=1 go test -count=1 -run '^TestCLI$' ./cmd/agentd ./cmd/openv-connector ./cmd/openv-mcp ./cmd/openv-vapid`
  for the command lines under `cmd/<command>/testdata/cli/`
- export, report and download formats, proposal payloads and import fields
  (S9): `UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^(TestFormatsGolden|TestProposalPayloadsGolden)$'`
  for `internal/api/testdata/formats/` (every export, report and download
  the API renders, through the real handlers) and
  `internal/api/testdata/proposal_payloads/`;
  `UPDATE_GOLDEN=1 go test ./internal/domain/exports -count=1 -run '^TestImportFields$'`
  for `internal/domain/exports/testdata/import_fields.txt`; and
  `UPDATE_GOLDEN=1 OPENV_TEST_DATABASE_URL=<server URL> go test ./internal/persistence/postgres -count=1 -run '^TestExportDocsRoundTrip$'`
  for the round trip of `docs/exports/*.json` under
  `internal/persistence/postgres/testdata/formats/roundtrip/` (a server with
  or without pgvector)
- frontend snapshots (S12, and S12b's CSS cascade,
  `frontend/src/arch/__snapshots__/cssOrder.txt`): `cd frontend && npx vitest run src/arch -u`
- bundle shape (S12b, `frontend/scripts/testdata/bundle-shape.json`): `cd frontend && npm run build && UPDATE_BUNDLE_SHAPE=1 node scripts/bundle-check.mjs`

The refactor tools' own goldens are not on the golden list: they change
with the tool, not with the product. `liftmigrations` (S14d) pins what it
makes of its fixture under `internal/tools/liftmigrations/testdata/want/`;
regenerate with
`UPDATE_GOLDEN=1 go test ./internal/tools/liftmigrations -count=1 -run '^(TestLiftFixture|TestSpecFixture)$'`;
`stageextract` (S14c) pins what it makes of its fixture under
`internal/tools/stageextract/testdata/want/`; regenerate with
`UPDATE_GOLDEN=1 go test ./internal/tools/stageextract -count=1 -run '^TestFixture$'`;
`tsdeclmove` (S14f) pins what it makes of its fixture under
`frontend/scripts/testdata/tsdeclmove/want/`; regenerate with
`cd frontend && UPDATE_GOLDEN=1 node --test scripts/tsdeclmove.test.mjs`.
Each goes in a pull request without the refactor labels, since a refactor
pull request may add files under `testdata/` but not change them.

For `UPDATE_GOLDEN` and `UPDATE_BUNDLE_SHAPE` only the value `1`
regenerates; any other value compares (`UPDATE_ROUTES` regenerates with any
non-empty value).
`UPDATE_RATCHETS=1 go test ./internal/archtest` only ever tightens
`ratchets.json`, so any pull request may run it.

### Running it locally

`make check LABELS="refactor refactor:tooling no-release-notes"` runs the
guard over the commits since the merge base with `origin/master`, with those
labels, and the stale-ratchet check for a refactor. The script alone:

```sh
python3 scripts/refactor/refactor_guard.py --base "$(git merge-base origin/master HEAD)" \
  --head HEAD --label refactor --label refactor:tooling
```

After a class A or R pull request merges, a class T follow-up adds its
merged commit's SHA to `.git-blame-ignore-revs` (turn it on with
`git config blame.ignoreRevsFile .git-blame-ignore-revs`). Quirks a refactor
keeps rather than fixes are listed in
[`docs/contract-quirks.md`](docs/contract-quirks.md).

**For the maintainer:** mark *Refactor guard* as a required status check on
`master` (Settings → Branches), next to *Release notes*, and apply
`behavior-change` only to a pull request that is not a refactor.

## Licensing of contributions

OpenV is licensed under the [Elastic License 2.0](LICENSE). By contributing, you
agree that your contribution is licensed under the same terms.

All commits must carry a **Developer Certificate of Origin (DCO)**
sign-off, certifying you have the right to submit the work under the
project's license (the full text is at [developercertificate.org](https://developercertificate.org)):

```
Signed-off-by: Your Name <your.email@example.com>
```

`git commit -s` adds this automatically. PRs with unsigned commits will be
asked to rebase with sign-offs before merging.

The DCO certifies your right to submit the work; it does not transfer
copyright. If the project ever needs to offer the code under different
terms (e.g. commercial licensing for OEM embedding), it will ask
contributors for that permission at the time. Your contribution always
remains available under the Elastic License 2.0.

## Enterprise code

Any future enterprise-only code will live in a clearly separated `ee/`
directory under its own license, so the boundary between the open core and
commercial extensions stays visible in the tree. Everything outside `ee/`
is and remains under the Elastic License 2.0.
