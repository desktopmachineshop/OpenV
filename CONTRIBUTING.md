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
| T tooling | CI workflows, the `Makefile`, Dockerfile build commands, lint config, PR and issue templates, docs (`*.md`, `docs/**`), `.git-blame-ignore-revs`, and files under `internal/tools/`, `internal/archtest/`, `internal/contract/`, `scripts/`, `frontend/scripts/` and `frontend/src/generated/` | nothing else changed; any other `frontend/src` file fails until S12b's base-vs-head build proves the output identical |
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
   `refactor_guard.py`, 20 entries) that is modified or deleted, a rename
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
   `frontend/src/arch/**`, the boundary rules in `frontend/eslint.config.js`,
   the move proofs, the migration generator that writes M10
   (`internal/tools/liftmigrations/**`, S14d), M11a's split proof
   `internal/tools/splittools` (S14e), F1's generator
   `frontend/scripts/tsdeclmove.mjs` with its tests (S14f) and this guard)
   may be modified or deleted only in a
   class C or T commit that modifies or deletes no golden. The plan asks such
   an edit to be green against the production code of its parent; since a C
   or T commit cannot change production code, that is what the pull
   request's own CI shows, and the guard does not re-run tests per commit.
   In a commit of any class, an entry of `internal/archtest/ratchets.json`
   raised or added fails (apart from class D's new-package entries, and the
   key of a new rule added by a class T commit that also changes the archtest
   rules), and so does an entry added to S12's lint allowlists
   (`COMPONENTS_IMPORTING_VIEWS`, `EVENT_SOURCE_SITES`) or a raised
   `CEILING` in `frontend/src/arch/errorChains.test.ts`. Lowering or removing
   any of those is allowed in any class and is not a guard-code edit.
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
`X2B_CALL_SHAPE_CHANGES`, the one named exception (plan §8.3), in its class
E commit; until then every `route_guards.txt` change is an authorization
change.

The job judges a pull request with the **base's** copy of
`refactor_guard.py` (its *Script self-test* step tests the pull request's
own copy), so a pull request's edits to the lists and rules take effect only
once it merges: a pull request that drops a guard-code or protected-path
entry is still judged by that entry. A step that needs a new exception, such
as S12b's class T rule for TypeScript the build erases, lands it in an
earlier pull request. `X2B_CALL_SHAPE_CHANGES` is the one list the job reads
from the pull request, since X2b fills it in its own. `make check` does the
same with the merge base's copy.

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
- MCP and worker wire (S7): `UPDATE_GOLDEN=1 go test ./internal/mcp ./internal/runner -run <Test>`
- env vars and command lines (S8): `UPDATE_GOLDEN=1 go test ./internal/archtest -count=1 -run '^TestEnvInventory$'`
  for the inventory, `internal/archtest/testdata/env_vars.txt`;
  `UPDATE_GOLDEN=1 go test -count=1 -run '^(TestEnvInventory|TestEnvParse)$' ./internal/archtest ./cmd/agentd ./cmd/openv-mcp ./cmd/server ./internal/api ./internal/billing ./internal/domain/users ./internal/hosting ./internal/notify`
  for the parse table beside it, `env_parse.txt`, which each package's
  `TestEnvParse` writes its sections of (a failure prints the command for
  its own package); and `UPDATE_GOLDEN=1 go test -count=1 -run '^TestCLI$' ./cmd/agentd ./cmd/openv-connector ./cmd/openv-mcp ./cmd/openv-vapid`
  for the command lines under `cmd/<command>/testdata/cli/`
- frontend snapshots (S12): `cd frontend && npx vitest run src/arch -u`

The refactor tools' own goldens are not on the golden list: they change
with the tool, not with the product. `liftmigrations` (S14d) pins what it
makes of its fixture under `internal/tools/liftmigrations/testdata/want/`;
regenerate with
`UPDATE_GOLDEN=1 go test ./internal/tools/liftmigrations -count=1 -run '^(TestLiftFixture|TestSpecFixture)$'`;
`tsdeclmove` (S14f) pins what it makes of its fixture under
`frontend/scripts/testdata/tsdeclmove/want/`; regenerate with
`cd frontend && UPDATE_GOLDEN=1 node --test scripts/tsdeclmove.test.mjs`.
Either in a pull request without the refactor labels, since a refactor pull
request may add files under `testdata/` but not change them.

For `UPDATE_GOLDEN` only the value `1` regenerates; any other value
compares (`UPDATE_ROUTES` regenerates with any non-empty value).
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
