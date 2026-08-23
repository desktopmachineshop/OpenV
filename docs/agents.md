# OpenV Agent Suite — Setup Guide

OpenV can delegate requirements work (drafting, review, interviews, V&V chores)
to AI agents. Agent runs execute on **runners** — small worker processes
(`agentd`) that poll the server's queue and drive a vendor CLI (`claude`,
`codex`, or `gemini`). The server itself never runs a model; it queues work
and reviews results.

## Architecture in one paragraph

The API server keeps a queue of `agent_runs`. A worker (`agentd`) polls that
queue, and for each run launches a vendor CLI with an MCP server (`openv-mcp`)
attached. The MCP server gives the agent tools to read and propose changes to
project data. Where the worker runs — and which credentials it uses — depends
on the runner tier below.

## Runner tiers

OpenV has two kinds of runners; a workspace can use either or both.

### Personal runners (your machine, your subscription)

A personal runner is `agentd` running on **your own machine**, signed into the
vendor CLIs with **your consumer subscription** (Claude Pro/Max, ChatGPT plan,
etc.). Each member mints their own **personal runner key** in workspace
settings (Settings → My Runner) and starts `agentd` with it.

- Claims **only runs you launched** — your subscription never executes a
  teammate's work.
- Full capability: repo-access runs (cloning connected repositories) are
  allowed, and the existing **Connect** sign-in flow works (the CLI's own
  login is relayed to your browser; credentials stay on your machine).
- Online/offline is derived from the key's poll heartbeat; the UI shows your
  runner's status live.

### Hosted runner (always-on, org API keys)

A hosted runner is a **platform-managed container** the server provisions on
its own Docker host — one per workspace, created by an org admin (Settings →
Hosted Runner). At provision time the admin supplies the org's **provider API
keys** (Anthropic / OpenAI / Gemini); they are injected into the container
environment only and are **never stored** by the platform.

- Always on: claims **ownerless work** (automations, cron, event triggers)
  and **overflow** — runs whose launcher has no runner online, or whose grace
  window expired.
- **No repo access**: hosted runners refuse runs whose agent needs a cloned
  repository, and the CLI sign-in (Connect) flow is disabled — hosted runs
  bill against the org's API keys, not anyone's subscription.
- Managed via the API/UI: provision, stop/start, delete (optionally purging
  the container's data volume).

### Routing and the first-refusal grace window

When you launch a run and your personal runner is online, the run is
**reserved for your runner** for a grace period (default **60 seconds**,
org-tunable via the workspace limit `runner_grace_seconds`). If your runner
does not claim it in time, the hosted/workspace runners take over.

**Ownerless runs** — launched by the system rather than a member: board
triggers, automations, delegations between agents — are claimable by **any
live runner** in the workspace (personal, workspace, or hosted) immediately,
first come first served, so the load spreads across every runner that is
online. Runs another member launched are never routed to your personal
runner; they stay with that member's runner or the workspace/hosted pool.

### Compliance note

Consumer CLI subscriptions run **only on the member's own machine** through
the vendor's own CLI — the platform never executes them server-side, shares
them between members, or proxies their credentials. Hosted runners are
API-billed: they use the org's provider API keys under the vendor's API terms.

## Prerequisites

1. A running OpenV stack (`make up`, or `docker compose up -d`).
2. For a **personal runner**: at least one vendor CLI installed on your host:
   - Claude Code: `npm install -g @anthropic-ai/claude-code`
   - Codex CLI: install per vendor docs
   - Gemini CLI: install per vendor docs
3. Docker (used by `make worker` to build the worker binaries, and by the
   hosted runner tier).

You can sign the CLIs in either way (personal runners only):

- **From the UI (recommended):** your **user settings** (click your user info
  in the bottom-left of the sidebar → Settings) → **Agent sign-ins**. This
  starts a *user-targeted* sign-in that only your own personal runner picks
  up, so the credential lands on your machine. The card relays the CLI's own
  flow: an "Open sign-in page" link, and — for CLIs that use a paste-back flow
  (Claude Code, Gemini) — a field to paste the authorization code. Codex opens
  a browser window directly on the runner machine and completes there.
  Credentials are stored by the CLI on the host; OpenV only brokers the URL
  and one-time code. Your runner (Agent Connector / `agentd`) must be running.
  Workspace admins can still run *workspace-targeted* sign-ins from
  Workspace settings → AI Providers → **Connect** — those are picked up by any
  of the workspace's shared workers.
- **From a terminal:** run `claude login` / `codex login` / `gemini` on the
  host yourself, then hit "Sync"/reload — the worker's next detection report
  updates the provider status.

## Per-project agent auth

Each project chooses how its runs authenticate (Project settings → Agents):

- **User account** (default): runs use the launcher's own local CLI sign-in.
  The project page has no sign-in UI — it only routes runs to the member's
  local login (set up in user settings, above).
- **API key**: overrides members' local sign-ins. At claim time the API tells
  the runner which environment variable holds the workspace's key (the
  provider setting's `api_key_env`, defaulting to the provider's native
  variable, e.g. `ANTHROPIC_API_KEY`); the runner reads it from its own host
  environment and injects it into the CLI. For now these runs still execute
  through each member's OpenV connector/runner.

## Per-user repo locations

Repo connections carry a shared default local path, but each member can set
**their own** local path per repository (Project settings → Repositories →
"your local path") since the checkout lives somewhere different on every
machine. Personal runners automatically receive the claiming member's path;
members without one fall back to the shared default.

## Configure a runner key

Every runner authenticates to the API with an org-scoped worker key.

- **Personal runner:** mint your key in workspace settings (one active
  personal key per member; minting a new one rotates the old). Pass it to
  `agentd` via `--worker-key` or the `WORKER_API_KEY` env.
- **Workspace keys:** admins can mint shared workspace keys under Settings →
  Worker Keys for self-managed always-on workers.
- **Hosted runner:** the server mints the key automatically at provision time.
- Legacy fallback: a `WORKER_API_KEY` set in the API server environment is
  registered as a workspace key for the bootstrap org at startup.

## The Agent Connector (recommended personal-runner setup)

The **OpenV Agent Connector** is a small on-demand launcher — deliberately not
a background service. It runs only while its console window is open. When any
agent run in OpenV can't find an active runner, the UI prompts you to open the
connector, and offers install + pairing if it isn't set up yet.

Flow:

1. **Download** the bundle from the prompt — on a first-time setup (no runner
   key yet) the download starts automatically when the `openv-connector://`
   link gets no response. Also available at
   `GET /api/v1/public/connector/download?os=windows|linux`; operators build
   the bundles with `make connector-dist` — served from `CONNECTOR_DIST_DIR`,
   default `./dist`, mounted into the api container by compose. If the bundles
   haven't been built, the prompt says so inline instead of erroring.
2. Unzip somewhere permanent and double-click `openv-connector.exe` once —
   it self-registers the `openv-connector://` link handler (HKCU, no admin).
3. Click **Pair connector** in OpenV. The browser opens the connector with a
   deep link carrying a **one-time pairing code** (10-minute TTL, sha256-stored,
   single-use — never a credential in the URL). The connector exchanges it at
   `POST /api/v1/public/connector/pair` for your personal runner key (rotating
   any previous one), saves its config (`%APPDATA%\OpenV\connector.json`), and
   starts `agentd` in the same window.
4. From then on, **Open connector** (or `openv-connector://start`, or
   double-clicking the exe) starts your runner. Closing the window stops it.

Your CLI subscription sign-ins stay in the vendor CLIs on your machine — the
platform never sees them, and your runner only claims runs you launched.

## Build and run a personal runner manually

```bash
# Windows host (cross-compiles via Docker into bin/)
make worker
bin\agentd.exe --api http://localhost:8080 --worker-key <your-personal-key>

# Linux/macOS host
make worker-unix
./bin/agentd --api http://localhost:8080 --worker-key <your-personal-key>
```

`agentd` polls for queued runs, launches the configured provider CLI with
`openv-mcp` wired in, streams progress back, and reports completion. Stale runs
that stop heartbeating are failed automatically by the server's reaper.

## Enable the hosted runner tier (operators)

Hosted runners are off unless the API server can reach a Docker daemon:

1. Build the runner image on the API's Docker host: `make worker-image`
   (produces `openv-worker:latest` from `Dockerfile.worker`).
2. Mount the docker socket into the API container (uncomment the
   `/var/run/docker.sock` volume line in `docker-compose.yml`; Linux hosts).
3. Optional env on the API service: `RUNNER_IMAGE` (default
   `openv-worker:latest`), `RUNNER_NETWORK` (attach runner containers to a
   compose network so they can reach the API), `RUNNER_API_URL` (API base URL
   as seen from inside a runner container, default `http://api:8080`), and
   `HOSTED_RUNNERS=off` to hard-disable the feature.

Each workspace's runner container gets a persistent volume
(`openv-runner-<org-id>`) holding its HOME and workspaces; deleting the
runner with `?purge=true` removes the volume too.

## Provider status

Open **Settings** in the UI to see per-provider status: whether the CLI was
detected on the worker host, whether it is logged in, and which providers are
enabled for runs. A provider that shows as unavailable usually means the CLI is
not on the worker's PATH or has not been logged in.

## Agent definitions

Agents are plain markdown files under the data volume:

```
$OPENV_DATA_DIR/agents/*.md      (default: /data/agents in the api container)
```

Each file has YAML frontmatter (slug, name, provider, `write_mode`, tools) and
a markdown body that becomes the agent's system prompt. The server syncs these
files into the database at startup, so editing a file and restarting (or
re-syncing) updates the agent. Seed agents (e.g. `requirements-interviewer`)
are created on first boot.

### write_mode: proposal vs direct

- `write_mode: proposal` (default) — every write the agent makes (create/update
  artifact, create link, record test result) is captured as a **proposal**.
  A human reviews and approves or rejects it in the UI before it is applied.
- `write_mode: direct` — writes are applied immediately. Reserve this for
  low-risk, well-trusted agents.

Imported or agent-drafted artifacts can also be stamped `status: draft`, so
nothing agent-generated silently masquerades as reviewed content.

## The lean-context rule

Prompts sent to an agent carry **identifiers, not content**. The run prompt
names the project, artifact IDs, or work item at hand; the agent fetches the
actual requirement text at run time via MCP tools. This keeps prompts small,
avoids stale copies of requirements, and means access control is enforced by
the API on every read — not by whatever happened to be pasted into a prompt.

## Teams and the org chart

Agents can be grouped into **teams** with a simple org-chart graph: a lead
agent can hand work to member agents, and orchestration hooks route follow-up
runs along the graph. Use teams when a task naturally splits (e.g. one agent
drafts requirements, another reviews for testability).

## Automations

Automations launch runs without a human clicking a button. Three trigger kinds:

- **manual** — run on demand from the UI.
- **cron** — the scheduler launches the run on a schedule (e.g. nightly
  gap-analysis review).
- **triggered** — the trigger matcher listens to the event bus (artifact
  created, test result recorded, work item moved, ...) and launches matching
  automations.

## Kanban-driven runs

Work items on the project kanban can drive agent work: moving a card into an
agent-owned column enqueues a run for that item, and the agent's progress and
comments land back on the card's activity feed. This makes "give this ticket to
the agent" a drag-and-drop action.

## Interview links

For requirements elicitation, create an **interview** in a project and generate
invite links. Each link is a public, token-authenticated page where a
stakeholder chats with an interviewer agent; answers stream in live, and the
agent records candidate user needs as it learns them. Sessions, transcripts,
and invites are managed from the project's Interviews tab; invites can expire
or be revoked at any time.

Interviews can optionally be linked to a **persona** artifact (many interviews
to one persona), so you can see how, say, a team of design engineers each
describe slightly different requirements for the same role. The linked persona
is shown on the interview list, can be used to filter interviews, and its
title and description are included in the interviewer agent's context so it
can probe persona-specific needs.

## Quick checklist

- [ ] `make up` — stack is running
- [ ] Vendor CLI installed and logged in on the host
- [ ] `WORKER_API_KEY` matches between server and worker
- [ ] `make worker` (or `make worker-unix`) built `bin/`
- [ ] `agentd` running and provider shows green in Settings
- [ ] Agent definitions present under `$OPENV_DATA_DIR/agents`

See also: `docs/architecture.md` (Multi-agent suite section) for the internal
topology, and `docs/link-type-rules.md` for the traceability vocabulary agents
are expected to use.
