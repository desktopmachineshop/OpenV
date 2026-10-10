# scripts and .github/workflows: tooling

The scripts the repository runs outside the Go and frontend builds, and the
GitHub Actions workflows that run them with the tests. Every Python script
here is stdlib-only Python 3, so it runs on any machine or runner with no
install step. Plan §7.9 (`docs/plans/codebase-refactor.md`) and
`CONTRIBUTING.md` ("Refactor PRs") have the history and the author's
checklist; `docs/DEVELOPMENT.md` ("Tests and CI") the long form.

## Areas

Everything here is the tooling area of `docs/areas.json`, through
`scripts/**` and `.github/**`. The rest of tooling has guides of its own:

| Area | Globs | Guide |
|---|---|---|
| tooling | `scripts/**`, `.github/**`, `Makefile` | this file |
| tooling | `internal/tools/**` | `internal/tools/README.md`: the refactor proofs and generators, `areas` and `scaffold` |
| tooling | `internal/archtest/**` | `internal/archtest/README.md`: the S1 architecture rules and `ratchets.json` |
| tooling | `frontend/scripts/**`, `frontend/src/arch/**`, `frontend/eslint.config.js` | `frontend/src/README.md` |
| tooling | `docs/areas.json`, `CLAUDE.md`, `README.md`, `CONTRIBUTING.md`, `docs/DEVELOPMENT.md` | the root `CLAUDE.md` ("Where things live") |

## Map

| Glob | What it holds |
|---|---|
| `openv/sync.py` | the OpenV requirements project from any machine: `register`, `bootstrap`, `vv`, `status`, `export`, and `api` for any endpoint (root `CLAUDE.md`, `docs/requirements-maintenance.md`) |
| `openv/mcp-server.sh` | builds and starts `openv-mcp` for an agent session in this repository; `.mcp.json` runs it |
| `release_notes.py` | the release-notes rules CI and the release workflows apply: `check`, `check-pr`, `cut`, `check-release`, `version`, `next`, `stable-version`, `cut-stable` |
| `refactor/refactor_guard.py` | the Refactor guard (S14b): `GOLDEN_LIST`, `FROZEN_DATA`, `PROTECTED_PATHS`, `GUARD_CODE`, and the per-commit class checks |
| `refactor/classify_commits.py` | the layers and files each commit touches, and the hub-touch rates of plan §10 |
| `backup.sh` | the backup recipe `make backup` and the backup sidecar run |
| `**/*_test.py` | the unittest suite of each script beside it |
| `.github/workflows/ci.yml` | the jobs every pull request runs: backend, backend-pgvector, frontend, vuln, secrets, docker, e2e |
| `.github/workflows/refactor-guard.yml` | the Refactor guard, on every pull request and every label change |
| `.github/workflows/release-notes.yml` | a pull request adds a release note, or carries `no-release-notes` |
| `.github/workflows/codeql.yml` | CodeQL |
| `.github/workflows/promote-release.yml`, `.github/workflows/nightly-promote.yml`, `.github/workflows/cut-stable.yml`, `.github/workflows/staging-smoke.yml` | the release pipeline (`docs/railway.md`, `docs/release-policy.md`) |
| `.github/pull_request_template.md`, `.github/ISSUE_TEMPLATE/**`, `.github/instructions/*.md` | the PR template, issue templates, and agent instructions |

## Invariants (plan §3) that bind here

- **R1, R3 and S14b.** The guard judges a pull request with the base's
  copy of `refactor/refactor_guard.py`, so an edit to its lists or rules takes
  effect once it merges; only `X2B_CALL_SHAPE_CHANGES`,
  `X14B_IMPORT_EDGES`, `X11B_IMPORT_EDGES`, `X10A_IMPORT_EDGES`,
  `X10B_IMPORT_EDGES` and `BOOT_STEPS_CHANGES` are read from the pull request. A golden is added to `GOLDEN_LIST`, and guard code
  to `GUARD_CODE`, by the step that creates it, in a class T commit.
- **I25.** `RELEASE_NOTES.md` is parsed by `release_notes.py` and by the
  server (`internal/domain/release`); both must accept the same file.
- **Plan §7.9.** The release workflows are out of the refactor's scope:
  `promote-release.yml`, `nightly-promote.yml`, `cut-stable.yml` and
  `staging-smoke.yml`.
- **`make check` mirrors CI** (the backend, frontend, release-notes and
  Refactor guard jobs, without Docker); `make check-fast` is its subset
  of under a minute.

## Recipes

**Add a CI check.** Add the step to its job in `.github/workflows/ci.yml`
and the same command to the `check` target of the `Makefile` (and to
`check-fast` if it is fast and needs no database), so the local gate keeps
mirroring CI.

**Add a golden or a guard file to the Refactor guard.** In a class T commit
of the step that creates it: an entry in `GOLDEN_LIST` or `GUARD_CODE`
naming the step, and a case in `refactor/refactor_guard_test.py`; then
`python3 scripts/refactor/refactor_guard_test.py`.

**Call an OpenV endpoint that has no subcommand.**
`python3 scripts/openv/sync.py api METHOD PATH [JSON]` with
`OPENV_API_URL` and `OPENV_API_TOKEN` set; a call made often becomes a
subcommand.

**Change the release-notes rules.** Edit `release_notes.py` with a case in
`release_notes_test.py`, keep the Go parser in `internal/domain/release`
in step, and run both suites (Guards below).

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S14b** the guard's self-test, which the Refactor guard job runs first.
  `python3 scripts/refactor/refactor_guard_test.py`
- **S14a, S14b** the commit classifier and guard suites, as CI's backend
  job runs them.
  `python3 -m unittest scripts/refactor/classify_commits_test.py scripts/refactor/refactor_guard_test.py`
- **Release notes** the script's suite and the notes file.
  `python3 -m unittest scripts/release_notes_test.py` and
  `python3 scripts/release_notes.py check RELEASE_NOTES.md`; the server's
  side: `go test ./internal/domain/release -count=1 -run '^TestEmbeddedNotesParse$'`
- **The Refactor guard itself**, over the commits since the merge base,
  with the labels the pull request will carry:
  `make check LABELS="refactor refactor:tooling no-release-notes"`, or
  `python3 scripts/refactor/refactor_guard.py --base <merge base> --head HEAD --label refactor --label refactor:tooling --label no-release-notes`.
- **Everything CI runs that needs no Docker:** `make check`; while
  working, `make check-fast`.
