@README.md

Before you finish a change here or in `.github/workflows`, run:
- `python3 scripts/refactor/refactor_guard_test.py` (the guard's self-test)
- `python3 -m unittest scripts/refactor/classify_commits_test.py scripts/release_notes_test.py`
- `python3 scripts/release_notes.py check RELEASE_NOTES.md`
- `make check-fast` while working, and `make check` (CI's jobs without Docker) before pushing

Don't:
- add a dependency outside the Python 3 standard library to a script here
- add a `GOLDEN_LIST` or `GUARD_CODE` entry outside a class T commit of the step that owns it (S14b)
- let `ci.yml` and `make check` drift apart: a CI step gets its `Makefile` line
- touch `promote-release.yml`, `nightly-promote.yml`, `cut-stable.yml` or `staging-smoke.yml` in a refactor (plan §7.9)
