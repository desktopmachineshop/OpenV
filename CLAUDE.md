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

Two paths reach the live project, and both authenticate with the same
workspace runner key:

- The **`openv` MCP tools** (`.mcp.json` starts `openv-mcp` in this repo) are
  the normal way to do the work in session: `get_project_map` to orient,
  `create_artifact` / `update_artifact` / `create_link` to edit,
  `create_test_run` + `record_test_result` for evidence, `get_vv_coverage`
  and `get_vv_gaps` to check V&V, `create_baseline` to snapshot. Writes made
  with a runner key land directly — they are your edits, not agent proposals.
- **`scripts/openv/sync.py`** — stdlib-only Python 3, so it runs from any
  machine or session with no build step — covers the rest: `bootstrap`,
  `vv`, `status`, `export`, and `sync.py api METHOD PATH [JSON]` for any
  endpoint without a tool or subcommand.

See `docs/requirements-maintenance.md` for the process and
`docs/api-spec.md` for the endpoints.

Credentials come from environment variables: `OPENV_API_URL` plus
`OPENV_API_TOKEN`, a workspace runner key minted under Settings → Runners →
Workspace keys. It is scoped to one workspace, stored hashed, and revocable,
so prefer it to `OPENV_EMAIL` / `OPENV_PASSWORD` — those are still accepted,
and are required only by `register` and `bootstrap`, which create accounts
and workspaces. Never commit any of them. In Claude Code cloud sessions they
belong in the cloud environment configuration, which must also allow network
egress to `*.up.railway.app`; variables are injected at session start, so a
new one only reaches the next session.

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
