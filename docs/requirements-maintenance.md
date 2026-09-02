# Maintaining the OpenV requirements project

OpenV dogfoods itself: the platform's own requirements live in a live OpenV
project, and from the moment that project exists it is the **source of
truth** for what the platform must do. Repo files (`CLAUDE.md`,
`.github/instructions/*.md`) only define how to work.

- **Instance**: https://openv-production.up.railway.app
- **Workspace**: Desktop Machine Shop (primary admin: the founder account)
- **Project**: OpenV Platform

## The tool

`scripts/openv/sync.py` — Python 3, standard library only, so it runs
anywhere: Windows PowerShell, Linux, CI, or an agent sandbox.

```bash
export OPENV_API_URL=https://openv-production.up.railway.app
export OPENV_API_TOKEN=...    # workspace runner key (preferred)
python3 scripts/openv/sync.py <command>
```

Mint the key in OpenV under **Settings → Runners → Workspace keys → + Create
key** (workspace admin only; it is shown once). It is scoped to that one
workspace, stored only as a hash, and revocable in a click — prefer it to a
password, which is unscoped and cannot be revoked without changing it
everywhere. With a key set, `OPENV_WORKSPACE` is ignored: the key already
names its workspace and the server scopes every request to it.

Use a **Workspace key**, not your personal *My Runner* key: creating a
personal key revokes your previous one, so pairing the Agent Connector would
silently kill a sync credential taken from there.

`register` and `bootstrap` still need `OPENV_EMAIL` / `OPENV_PASSWORD` — they
create accounts and workspaces, which a key cannot do. Everything else runs
on the key alone.

(PowerShell: `$env:OPENV_API_URL = "..."` etc.)

| Command | What it does |
|---|---|
| `register` | Create the account (open registration) — first-time setup only |
| `bootstrap [--invite-admin EMAIL]` | Idempotent full load of the seed: ensures the workspace and project exist, saves the product profile, creates/updates artifacts (matched by title), creates missing links, captures a baseline when anything changed. Safe to re-run at any time. |
| `vv` | Phase 2: assigns verification methods to every requirement and records the standing evidence test run |
| `status` | Live vs. seed counts, anything missing, V&V coverage summary |
| `export [--out FILE]` | Download the live project export JSON (periodic backup) |
| `api METHOD PATH [JSON]` | Authenticated ad-hoc call for anything else, e.g. `api POST /api/v1/artifacts '{...}'` |

## The tool server (agent sessions)

An agent session working in this repository does not have to drive the REST
API by hand: `.mcp.json` at the repository root starts **`openv-mcp`**, the
same MCP tool server `agentd` runs beside a vendor CLI, so the session gets
26 typed tools over the workspace instead of a script and an API spec.

It authenticates from the environment — `OPENV_API_URL` plus
`OPENV_API_TOKEN`, the same workspace runner key `sync.py` uses. A
platform-launched agent run gets `OPENV_RUN_TOKEN` from `agentd` instead, and
that token wins where both are set. The credential decides what the session
may touch: a run token stays scoped to its run's project and its agent's
write mode, while a **workspace runner key is a workspace-wide editor with no
proposal gating** — the same authority `sync.py` has, so treat MCP writes made
with it as your own edits, not as agent proposals.

`scripts/openv/mcp-server.sh` builds `bin/openv-mcp` on first use and whenever
its sources change; `make mcp` builds it ahead of time so a session starts
without waiting for the compile. The whole maintenance loop is covered by
tools — `get_project_map` to orient, `create_artifact` / `update_artifact` /
`create_link` to edit, `create_test_run`, `record_test_result` and
`close_test_run` for evidence, `get_vv_coverage` and `get_vv_gaps` to check
V&V, `create_baseline` to snapshot.

Bulk **export stays out of the tool surface** on purpose: it is a file-level
backup, and a tool that returns a whole project invites agents to spend their
context on it. Use `sync.py export` for that.

## Go-live (one time)

```bash
python3 scripts/openv/sync.py bootstrap --invite-admin dave@desktopmachineshop.com
python3 scripts/openv/sync.py vv
python3 scripts/openv/sync.py status
```

Run as any account; `--invite-admin` makes the named (already registered)
user a workspace admin. An account that runs `bootstrap` and creates the
workspace is its first admin; it can leave afterwards
(`DELETE /api/v1/orgs/{id}/members/{userId}` via `sync.py api`).

## Ongoing maintenance (every behavior-changing PR)

From go-live onwards, edits happen **directly in the live project** — via
the OpenV UI or `sync.py api` — not by editing the seed file:

1. When a PR adds, changes, or removes platform behavior, update the
   affected artifacts and links in the OpenV Platform project as part of
   that work.
2. When verification for a requirement lands (a test suite, an E2E check),
   record it: create or reuse a test run, add results, set
   `verification_status` on the requirement.
3. After a coherent set of changes, capture a baseline — the `create_baseline`
   tool, or `sync.py api POST /api/v1/projects/{id}/baselines '{"name":"..."}'`.
4. Periodically (or before risky changes) run `sync.py export` and commit
   the export under `docs/exports/` as an off-instance backup.

`examples/openv-multi-agent-suite/requirements.json` remains the historical
bootstrap seed. Re-running `bootstrap` never destroys live edits: it only
creates what is missing and updates artifacts whose seed entry changed, so
live-only artifacts and attribute changes survive.

## Credentials and agent sessions

- Never commit credentials. Locally, set the `OPENV_*` variables in your
  shell; in Claude Code cloud sessions, set them in the cloud environment
  configuration (claude.ai/code → environment → settings). Those variables
  are injected when a session starts, so one added mid-session only reaches
  the next session — an existing session keeps the environment it booted with.
- The cloud environment's **Network access** must allow the instance —
  add `*.up.railway.app` to the Custom allowed-domains list, otherwise the
  session's egress proxy blocks every API call.
- A workspace runner key authenticates both `sync.py` and `openv-mcp`; only
  `register` and `bootstrap` still need `OPENV_EMAIL` / `OPENV_PASSWORD`,
  because creating an account or a workspace requires a user session.
