# OpenV — agent working conventions

## Source of truth

The live OpenV instance is the source of truth for **what** this platform
must do:

- **Instance**: https://openv-production.up.railway.app
- **Workspace**: Desktop Machine Shop
- **Project**: OpenV Platform (requirements, traceability links, V&V evidence)

Repository instruction files — this file and `.github/instructions/*.md` —
define only **how** to work: architecture rules, coding style, process.
Where a requirement statement in a repo file disagrees with the live OpenV
project, the OpenV project wins. `examples/openv-multi-agent-suite/`
is the historical bootstrap seed, not a live document.

## Keeping the requirements project current

Any change that alters what the platform does — a new capability, a changed
behavior, a dropped feature — must be reflected in the OpenV Platform
project as part of the same piece of work, not batched for later:

- add or update artifacts (requirements, design items, test cases) and their
  traceability links;
- record verification evidence (test runs) when a requirement's verification
  lands;
- capture a baseline after any coherent set of changes.

Use `scripts/openv/sync.py` for all of this — it is stdlib-only Python 3 and
works from any machine or agent session. `sync.py api METHOD PATH [JSON]`
covers anything without a dedicated subcommand. See
`docs/requirements-maintenance.md` for the process and
`docs/api-spec.md` for the endpoints.

Credentials come from `OPENV_API_URL` / `OPENV_EMAIL` / `OPENV_PASSWORD`
environment variables. Never commit them. In Claude Code cloud sessions they
belong in the cloud environment configuration, which must also allow network
egress to `*.up.railway.app`.

## Deployment

Railway deploys the `release` branch, not `master`. Merges to `master`
deploy nothing; shipping means running the **Promote to release** workflow
(`.github/workflows/promote-release.yml`). See `docs/railway.md`.

**Never run Promote to release without the maintainer explicitly asking for
that release.** Each promotion rebuilds both Railway services, which
currently costs more in build minutes than real usage does. Releases are
batched — nightly or weekly — at the maintainer's discretion. Merging to
`master` is the normal end of a piece of work: say what is merged and
waiting, then leave promotion alone unless asked. Approval to merge is not
approval to promote.
