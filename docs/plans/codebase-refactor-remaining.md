# Codebase refactor: what is left, and how it is run

Status: 10 October 2026, against `master` at `b8cfe59`. This file is the
execution plan for the rest of
[the refactor plan](codebase-refactor.md). That plan stays the source of
truth for **what** each step does: its rows in §6.7, the invariants in §3,
and the review rules in §8.3. This file says **what is left**, **in what
order**, and **which model builds each step**. Nothing here changes a
step's scope, guard or verification class.

## 1. Where the programme stands

| Phase | State |
|---|---|
| 0 Safety net | Done. S16 is just in time: S16a, b, c, i, j and k are merged; S16d–S16h wait for F8. |
| 1 Same-package moves | Done (M1–M15, F1–F7, N1–N3, D1). F8 is rolling. |
| 2 Leaf packages | Done (P1–P4; P5 dropped). |
| 3 One mechanism each | X2–X9, X12 and X15 done. X10, X11 and X14 part done. X1, X13, X16, X17, X18 and D2 not started. X19 dropped. |
| 4 | Dropped. |

What is left, measured on `master`:

| Step | Left | Measured |
|---|---|---|
| **X10b** | Switch the boot stages to `internal/config` | X10a (#568) and the X10b pin (#570) are merged; `cmd/server` still has 9 direct environment reads |
| **X11c** | Build the appliers in `wire_agents.go`; `SetAppliers` stays at today's statement | X11a (#526) and X11b (#569) merged; add S5b's guided tour to its guard column |
| **X14c** | The 3 snapshot sites left by X14b: reports, V&V and downloads | Needs a guard exception for `internal/domain/reports → internal/domain/snapshot` first |
| **X1** | Response and decode helpers, and the `scripts/refactor/httpio` codemod | Not started; ratchets `raw_json_encodes` 248, `invalid_request_body_literals` 106; no codemod yet |
| **X13** | Persistence kit, then columns const and scanner per repository | Not started; 20 repositories without a columns const, 3 of them with hand-rolled transactions (artifact, project, V&V) |
| **X16a, X16b** | ModuleView hooks | Prep merged (#528); `ModuleView.tsx` is 1,650 lines, target a shell of 400 or fewer |
| **X17** | Page registry `src/pages.ts` | Not started |
| **X18a–X18c** | Wizard descriptors and typed answers | Not started; `GuidedWizard.tsx` is 1,635 lines, target 400 or fewer |
| **D2** | `docs/architecture.md` after Phase 3 | Waits for X13, X15 (done) and X17 |

Open items from the tracking issue (#379) that are not plan steps:

- **H1** Test hygiene: `TestNewHandlerNamesAMalformedPerRequestSettingAtBoot`
  fails under `go test -count=2`.
- **Bug 226** A huge `*_mb` workspace limit wraps when turned into bytes.
  Its fix was recommended by the agent, not yet decided by the maintainer.

Not scheduled: F8 and its S16d–S16h pins run only when a feature next
touches one of those views. S17, P5, X19 and Phase 4 stay dropped.

## 2. How it runs: one orchestrator, one worker at a time

The orchestrator is the Claude Code session the maintainer talks to. It
writes no production code. For each step it:

1. **Briefs.** Writes a short brief: the step's row from §6.7 verbatim, its
   guard, the invariant rows it names, the area `CLAUDE.md` to follow, the
   branch name, the labels and the verification class of each commit.
   Workers do not read the whole refactor plan or the tracking issue; the
   brief carries what they need.
2. **Delegates.** Starts **one** worker on the model the queue in §3 names.
   No two workers run at once.
3. **Reviews.** Reads the diff as §8.3 says for the class: for class E a
   skeptical full read, a check that the characterization PR is on
   `master` and untouched, and a check that no golden changed outside a
   named exception. Fix rounds go back to the **same** worker, so it keeps
   its context and nothing is re-read.
4. **Merges.** Every PR, class E included, merges once CI is green on its
   head and the orchestrator's review has nothing open: the maintainer
   delegated merging on 10 October 2026, in place of reviewing each class E
   batch. The maintainer can still review any PR before it merges, and any
   PR they comment on waits for them. Merging never includes *Promote to
   release*.
5. **Records.** After each merge, one Haiku worker ticks the tracking
   issue, adds window moves to `.git-blame-ignore-revs`, and records OpenV
   evidence and a baseline (§8.5 of the refactor plan).

A worker that fails review twice on the same point is replaced by a fresh
worker one model up (Haiku → Sonnet → Opus), given the review notes.

**Choosing the model.** The model is set by how much judgement the step
needs, not by its size:

- **Opus** where behavior could shift without a test noticing at first
  glance: semantic class E extractions in boot wiring, new tools that must
  match code exactly, and large React decompositions.
- **Sonnet** where a merged precedent shows the pattern and goldens catch a
  slip: a second instance of a done extraction, mid-sized class E moves,
  bug fixes with a decided design, and docs.
- **Haiku** where a tool or a template does the work: running the codemod
  on one more area, a repository with no transactions under 300 lines, a
  guard exception copied from an existing one, and bookkeeping.

**Order.** The queue in §3 runs top to bottom, one step at a time: a step
starts only after the one before it has merged.

## 3. The queue

One PR per sub-ID (maintainer decision, 10 October 2026): X1 is the
codemod plus 8 area PRs, and X13 is 20 repository PRs.

### Wave 0: unblock CI

| # | Step | Class | Model | Why that model | Depends |
|---|---|---|---|---|---|
| 0 | **Go 1.26.9** and `golang.org/x/net` v0.60.0 | maintenance | Sonnet | A minor Go upgrade needs the whole suite read for behavior changes; the *Vulnerability scan* job fails on every PR until it lands (10 standard-library advisories with no fix in Go 1.25) | – |

### Wave 1: finish what is in flight

| # | Step | Class | Model | Why that model | Depends |
|---|---|---|---|---|---|
| 1 | **H1** `-count=2` test fix | test only | Sonnet | Finding the leaked state needs some diagnosis | – |
| 2 | **X10b** Stages read config through `internal/config` | E (+ `BOOT_STEPS_CHANGES`) | Opus | Each read keeps its condition and statement; fatal checks keep their boot positions | X10a, X10b pin (merged) |
| 3 | **X11c** Appliers built in `wire_agents.go` | E | Opus | Boot order and `boot_steps.txt` must not move | X11b (merged) |
| 4 | **X14c guard** `X14C_IMPORT_EDGES` | T | Haiku | Copy of `X14B_IMPORT_EDGES` with one edge | – |
| 5 | **X14c** Reports, V&V and downloads load through `snapshot.Load` | E | Sonnet | Same conversion as X14b's 4 sites | 4 |

### Wave 2: start the two long backend tails

| # | Step | Class | Model | Why that model | Depends |
|---|---|---|---|---|---|
| 6 | **X1 tool** `scripts/refactor/httpio` with its test | T | Opus | Matches exact statement sequences through `go/ast`, never reorders, reports what it does not recognise | – |
| 7 | **X1a** Helpers in `respond.go`, first area | E | Sonnet | Helper bodies are specified exactly; this proves the tool on one area | 6 |
| 8 | **X13a** `pgkit.go`, `nulls.go`, `withTx`; artifact repository | E | Opus | Sets the pattern 19 PRs copy; `withTx` must keep each transaction's rollback behavior | – |

### Wave 3: frontend

ModuleView is a hot file (§6.9): X16a and X16b each post their spec on the
tracking issue first and merge only while no other open PR changes it.

| # | Step | Class | Model | Why that model | Depends |
|---|---|---|---|---|---|
| 9 | **X16a** ModuleView data and baseline hooks; S16a re-run first | E · window | Opus | 1,650-line component; the API call order and storage keys must not move | X15a, S16a, prep #528 |
| 10 | **X17** `pages.ts` registry, `navSections` derived | E | Sonnet | Parity tests pin it; App.tsx JSX untouched | – |
| 11 | **X18a** Step descriptors and `WizardAnswers` serializer | E | Opus | Encodes the wizard's quirks as data; any miss changes stored answers | S16b, F5 |

### Wave 4: the tails, then the second halves

| # | Step | Class | Model | Depends |
|---|---|---|---|---|
| 12 | **X1b–X1h** codemod over the other 7 areas, one area per PR (7 PRs) | E | Haiku; Sonnet for any area where the tool reports more than a few unrecognised sites | 7 merged |
| 13 | **X13** project, V&V and link repositories (3 PRs) | E | Sonnet (transactions, large files) | 8 merged |
| 14 | **X13** interview, workitem, guided, embedding repositories (4 PRs) | E | Sonnet (large files) | 8 merged |
| 15 | **X13** the 12 small repositories, one per PR (12 PRs) | E | Haiku | 8 merged |
| 16 | **X16b** column resize, sibling ordering and panel mode hooks | E · window | Opus | 9 merged |
| 17 | **X18b** `useGuidedSession` | E | Sonnet | 11 merged |

### Wave 5: close out

| # | Step | Class | Model | Depends |
|---|---|---|---|---|
| 18 | **X18c** Step components into `views/guidedWizard/`, shell of 400 lines or fewer | E | Sonnet | 17 merged |
| 19 | **D2** `docs/architecture.md` | T (docs) | Sonnet | X13 and X17 merged |
| 20 | **Bug 226** Size limits that do not fit in bytes | fix | Sonnet | Maintainer's decision on the fix |
| 21 | **Stop point 3** Baseline, the last tracking-issue update | – | Haiku | All of the above |

That is about 43 PRs: 1 in wave 0, 8 in waves 1 and 2, 3 in wave 3, 28 in
wave 4 and 3 in wave 5, plus 2 blame follow-ups for the ModuleView
windows.

## 4. What only the maintainer can do

- Review any PR before it merges, if wanted: merging is delegated (§2).
- Decide bug 226's fix, which the tracking issue records as the agent's
  recommendation.
- Make the *Refactor guard* job required, and protect `master` with
  "require branches to be up to date" (S14b).
- Re-approve TC-56 in OpenV and review REQ-91's new sentence.
- Record the stop point 3 decision on the tracking issue. #379 was closed on
  3 October but is still where the checklist lives; reopen it or say where
  the record goes instead.
- Promotion. No step here promotes; merging to `master` ends a step.
