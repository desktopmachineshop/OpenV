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
export OPENV_EMAIL=...        # an account with access to the workspace
export OPENV_PASSWORD=...
python3 scripts/openv/sync.py <command>
```

(PowerShell: `$env:OPENV_API_URL = "..."` etc.)

| Command | What it does |
|---|---|
| `register` | Create the account (open registration) — first-time setup only |
| `bootstrap [--invite-admin EMAIL]` | Idempotent full load of the seed: ensures the workspace and project exist, saves the product profile, creates/updates artifacts (matched by title), creates missing links, captures a baseline when anything changed. Safe to re-run at any time. |
| `vv` | Phase 2: assigns verification methods to every requirement and records the standing evidence test run |
| `status` | Live vs. seed counts, anything missing, V&V coverage summary |
| `export [--out FILE]` | Download the live project export JSON (periodic backup) |
| `api METHOD PATH [JSON]` | Authenticated ad-hoc call for anything else, e.g. `api POST /api/v1/artifacts '{...}'` |

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
3. After a coherent set of changes, capture a baseline
   (`sync.py api POST /api/v1/projects/{id}/baselines '{"name":"..."}'`).
4. Periodically (or before risky changes) run `sync.py export` and commit
   the export under `docs/exports/` as an off-instance backup.

`examples/openv-multi-agent-suite/requirements.json` remains the historical
bootstrap seed. Re-running `bootstrap` never destroys live edits: it only
creates what is missing and updates artifacts whose seed entry changed, so
live-only artifacts and attribute changes survive.

## Credentials and agent sessions

- Never commit credentials. Locally, set the `OPENV_*` variables in your
  shell; in Claude Code cloud sessions, set them in the cloud environment
  configuration (claude.ai/code → environment → settings).
- The cloud environment's **Network access** must allow the instance —
  add `*.up.railway.app` to the Custom allowed-domains list, otherwise the
  session's egress proxy blocks every API call.
- Worker keys (Settings → Worker Keys in the UI) authenticate agent
  *runners*, not this tool: workspace/project creation and import require a
  user session, so `sync.py` uses email + password login.
