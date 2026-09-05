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

`openv-mcp` also runs outside a platform run: given a workspace runner key in
`OPENV_API_TOKEN` instead of the `OPENV_RUN_TOKEN` `agentd` injects, it serves
the same tools to any MCP host — for instance an agent session working in this
repository, which starts it from `.mcp.json`. The credential, not the server,
sets the limits: a run token is scoped to its run's project and its agent's
write mode, a runner key carries workspace-wide editor rights with no proposal
gating. See `docs/requirements-maintenance.md`.

## Runner tiers

OpenV has three kinds of runners; a workspace can use any mix of them.

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

### Transient runners (in the cloud, nothing to install)

A transient runner is a **pre-warmed runner the platform already has running**,
leased to **one member at a time** for a bounded stretch of time. The member
presses **Start a cloud runner** (user settings → Cloud runner, or the prompt a
run shows when no runner is online), signs their vendor CLIs into it **from
their browser**, and launches runs — with nothing downloaded, installed or
paired.

- **Per member.** While the lease lasts, the runner is that member's alone; it
  claims their runs exactly as their own machine would, because it holds a
  personal runner key scoped to the lease.
- **Their own sign-ins.** The CLI sign-in flow runs *on the leased runner* and
  is relayed to the member's browser: an "Open sign-in page" link, and a field
  to paste back whatever the CLI asks for. See **Signing in on a transient
  runner** below for what each vendor's flow looks like.
- **It ends, and it is wiped.** A lease has a hard lifetime (default **60
  minutes**, org-tunable via `runner_session_minutes`) and an idle window
  (default **15 minutes** with no run activity, `runner_session_idle_minutes`;
  the clock resets whenever the runner claims a run or a sign-in). Whichever
  comes first ends the lease: the session credential is revoked server-side and
  the node deletes the lease's HOME — where the vendor CLIs keep their
  credentials — before taking another member. **So the next session starts with
  signing in again.** That is the design, not a limitation: nothing of a member
  survives on a shared pool node.
- **Extend** resets both clocks, capped at 8 hours from the original start so a
  forgotten tab cannot hold a node forever.
- **Repo access** works by cloning the connection's repository URL; a leased
  runner has no local checkout to point at, so per-member local paths do not
  apply to it. Private repositories that need credentials on the host will not
  clone.

Members keep whichever tier suits the moment: a cloud runner for a quick piece
of work on a machine with nothing installed, the connector on their own machine
for everyday work. Leasing one does not disturb the other — a cloud lease mints
its own credential and never rotates the member's connector key.

#### The pool

Transient runners come from a **pool of always-on `agentd` processes** started
with the deployment's `RUNNER_POOL_KEY` (`agentd --pool-key …`). A pool node
belongs to nobody: it registers, heartbeats, and waits. When a member leases
one, the API hands that node a session credential on its next heartbeat; when
the lease ends, the node wipes and reports itself free.

The pool is deliberately not "a container per request": pre-warmed nodes are
ready in seconds, and — unlike hosted runners — the design needs no Docker
daemon at the API, so it works on platforms (Railway among them) that give you
replicas but no docker socket. Sizing the pool is sizing concurrency: **one
member can hold one node**, so a pool of three serves three simultaneous
members. When every node is busy, the UI says so and the member can try again
or fall back to the connector.

Operators: see **Enable transient runners (operators)** below.

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
- **Resource-capped**: the container is created with memory and CPU limits
  from the org's `limits` (`runner_memory_mb`, `runner_cpus`), falling back
  per key to the org plan's defaults (free: 2048 MB / 1 CPU; team: 4096 MB /
  2 CPUs). There is no UI/API for editing org limits yet — operators set the
  keys directly on the `organizations.limits` JSONB column; changes apply the
  next time the runner is provisioned.

### Routing and the first-refusal grace window

When you launch a run and your personal runner is online, the run is
**reserved for your runner** for a grace period (default **60 seconds**,
org-tunable via the workspace limit `runner_grace_seconds`). If your runner
does not claim it in time, the hosted/workspace runners take over. A transient
runner you hold a lease on counts as your runner here: runs you launch prefer
whichever of your runners is online.

**Ownerless runs** — launched by the system rather than a member: board
triggers, automations, delegations between agents — are claimable by **any
live runner** in the workspace (personal, workspace, or hosted) immediately,
first come first served, so the load spreads across every runner that is
online. Runs another member launched are never routed to your personal
runner; they stay with that member's runner or the workspace/hosted pool.

### Where your subscription runs

A consumer CLI subscription is always driven by **the vendor's own CLI**, signed
in by **the member who owns it**, and OpenV never sees or stores the resulting
credentials — the CLI writes them to the machine it runs on, and OpenV brokers
only the sign-in URL and the one-time code.

*Which* machine that is depends on the tier the member chose:

- **Personal runner** — their own machine. Credentials persist there between
  sessions, and never leave it.
- **Transient runner** — a pool node in the platform's own infrastructure,
  leased to that member alone. Its credentials live in a directory created for
  the lease and deleted when the lease ends, and no other member is ever served
  by that node while it holds them. This is a real difference from a personal
  runner and worth being deliberate about: a member signing in here is putting
  their subscription's credentials on infrastructure the operator controls, for
  as long as the lease lasts. Check your vendor's terms if that matters for
  your deployment — and note that operators can leave the feature off simply by
  not configuring a pool.
- **Hosted runner** — a platform container billed against the **org's provider
  API keys**, under the vendor's API terms. No consumer subscription is
  involved, and CLI sign-in is disabled there.

A subscription is never shared between members, proxied, or executed by the API
server itself in any tier.

## Prerequisites

1. A running OpenV stack (`make up`, or `docker compose up -d`).
2. For a **personal runner**: at least one vendor CLI installed on your host:
   - Claude Code: `npm install -g @anthropic-ai/claude-code`
   - Codex CLI: install per vendor docs
   - Gemini CLI: install per vendor docs
3. Docker (used by `make worker` to build the worker binaries, and by the
   hosted runner tier).

You can sign the CLIs in either way (personal and transient runners; hosted
runners are API-key only):

- **From the UI (recommended):** your **user settings** (click your user info
  in the bottom-left of the sidebar → Settings) → **Agent sign-ins**. This
  starts a *user-targeted* sign-in that only your own personal runner picks
  up, so the credential lands on your machine. The card relays the CLI's own
  flow: an "Open sign-in page" link, and — for Gemini, which uses a paste-back
  flow — a field to paste the authorization code. Codex opens a browser window
  directly on the runner machine and completes there. Claude Code runs an
  interactive `claude auth login` in a terminal window on the worker host.
  Credentials are stored by the CLI on the host; OpenV only brokers the URL
  and one-time code. Your runner (Agent Connector / `agentd`) must be running.
  Workspace admins can still run *workspace-targeted* sign-ins from
  Workspace settings → AI Providers → **Connect** — those are picked up by any
  of the workspace's shared workers.
- **From a terminal:** run `claude login` / `codex login` / `gemini` on the
  host yourself, then hit "Sync"/reload — the worker's next detection report
  updates the provider status. (Not available on a transient runner — there is
  no terminal on it to use.)

### Signing in on a transient runner

A leased runner has no console to open a TUI in and no browser to catch an
OAuth redirect, so each vendor's flow is relayed to the member's own browser:

| Provider | How it is relayed |
| --- | --- |
| **Claude Code** | `claude auth login` is a terminal UI that renders nothing over pipes, so it is driven over a **pseudo-terminal** inside the runner. The URL it prints is scraped out and shown as a link; the code the member pastes back is typed into that terminal. Not `claude setup-token`: that asks only for `user:inference` and mints a long-lived token for headless use, leaving the CLI itself signed out — runs after it fail with "Not logged in". Needs a CLI new enough to have `claude auth login` (`auth status` likewise, which is how a runner's sign-in state is read). |
| **Gemini CLI** | Already a paste-back flow (`NO_BROWSER=1`): URL out, code in. Unchanged. |
| **Codex CLI** | `codex login` completes against a **loopback port on the runner**, which the member's browser cannot reach. So after authorizing, the member's browser shows a connection error — they copy that whole address out of the address bar and paste it back, and the runner replays it against its own listener. Only the path and query of the paste are used, always against the port the CLI itself advertised, so a paste naming another host cannot make the runner fetch it. |

All three surface in the same **Agent sign-ins** cards. Sign-ins on a leased
runner last exactly as long as the lease.

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

A repo connection identifies the repository itself — name, repository URL
(required, e.g. a GitHub remote), default branch. The checkout location is
always per-member, since the clone lives somewhere different on every
machine, so each member sets **their own** local path per repository (Project
settings → Repositories → "your local path"). Personal runners automatically
receive the claiming member's path; runs for a member without one clone the
repository URL instead.

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

1. **Download** the connector from the prompt — on a first-time setup (no runner
   key yet) the download starts automatically when the `openv-connector://`
   link gets no response. Also available at
   `GET /api/v1/public/connector/download?os=windows|linux`. Nothing needs
   building or wiring first: `Dockerfile.api` builds one self-contained
   executable per OS into the image's `./dist`, which is the
   `CONNECTOR_DIST_DIR` default, so compose and Railway both serve it out of
   the box. `make connector-dist` builds the same executables on the host,
   for a deployment that points `CONNECTOR_DIST_DIR` at its own directory.
   Where no executable is present the prompt says so inline instead of
   erroring.
2. Put the executable somewhere permanent and double-click
   `openv-connector.exe` once — it self-registers the `openv-connector://`
   link handler (HKCU, no admin), pointing it at wherever that copy lives.
   Run a freshly downloaded copy once after any update: the registration
   follows the executable, so an old copy left registered is what makes the
   connector window report that it does not understand a link.
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

## Enable transient runners (operators)

Transient runners are off until a deployment configures a pool. Two things:

1. **Set `RUNNER_POOL_KEY`** on the API service — a long random string
   (`openssl rand -hex 32`). This is the shared credential pool nodes present.
   It authenticates *nothing but* the pool endpoints: a node holds no workspace
   identity until a member leases it, and then uses that lease's own key.
   Leaving it unset disables the feature (the UI card hides itself).
2. **Run some pool nodes** with the same key. Each is an `agentd` from the
   `openv-worker` image (`make worker-image`), started in pool mode:

   ```bash
   agentd --api https://<api-host> --pool-key <RUNNER_POOL_KEY>
   ```

   With compose, that is a scaled service — `make runner-pool-up POOL=3`, or:

   ```bash
   RUNNER_POOL_KEY=<same-key> docker compose --profile runner-pool \
     up -d --scale runner-pool=3 runner-pool
   ```

   On Railway, it is a third service from `Dockerfile.worker` with a replica
   count; see `docs/railway.md`.

Sizing: **one node serves one member at a time**, so the pool size is the
number of members who can hold a cloud runner simultaneously. Idle nodes cost
whatever an idle container costs on your platform — they are pre-warmed on
purpose, since provisioning on demand is what makes this slow elsewhere.

Pool node environment (all optional beyond the key):

| Variable | Meaning |
| --- | --- |
| `RUNNER_POOL_KEY` | Shared pool credential; presence switches `agentd` to pool mode. |
| `OPENV_API_URL` | API base URL as seen from the node. |
| `RUNNER_POOL` | Pool label, for running more than one pool later (default `default`). |
| `RUNNER_NODE_NAME` | Stable node name across restarts (default: hostname). A restarted node reclaims its own row rather than leaving a phantom in the pool. |
| `RUNNER_SESSION_ROOT` | Parent directory for per-lease HOME directories (default: under `--workspaces`). Wiped at startup and after every lease. |

Workspace tuning lives in the org's `limits` JSONB, like the hosted-runner
caps: `runner_session_minutes` (lease lifetime) and
`runner_session_idle_minutes` (idle window). Defaults are 60/15 on the free
plan and 120/20 on team.

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
$AGENTS_DIR/<org-id>/<slug>.md   (default: $OPENV_DATA_DIR/agents,
                                  i.e. /data/agents in the api container)
```

Each file has YAML frontmatter — `slug`, `name`, `description`, `provider`,
`model`, `effort`, `allowed_tools`, `write_mode`, `repo_access`, `max_turns`,
`timeout_seconds`, `config` (see `internal/domain/agents/agents.go`) — and a
markdown body that becomes the agent's system prompt. The server syncs these
files into the database at startup, so editing a file and restarting (or
re-syncing via `POST /api/v1/agents/sync`) updates the agent. Agent files are
per-workspace (`$AGENTS_DIR/<org-id>/<slug>.md`); seed agents (e.g.
`requirements-interviewer`, `requirements-copilot` — the V&V Assistant, whose
slug keeps its original name so workspaces provisioned before the rename keep
working) are created when a workspace is first provisioned.

A seeded agent belongs to its workspace once it exists: a member can rename it,
retune its prompt or point it at another model, and startup never overwrites
that. The one exception is a seed whose own identity changed — the requirements
copilot became the V&V Assistant — where the new name, description and system
prompt are adopted at startup **field by field, and only where the workspace
still carries the exact text the old seed wrote**. An agent anyone has edited
keeps what they wrote; an untouched one catches up.

### Choosing a model

The **Model** field in the agent editor (and **Default model** in Workspace
settings → AI Providers) is a dropdown of the selected provider's known
models, served by the API from a built-in catalog. Leave it on *provider
default* to inherit the provider setting, or pick **Custom…** to type any
model id the vendor's CLI accepts — vendors ship new models faster than OpenV
releases, so the catalog is a convenience, not a whitelist. A worker may also
report the models it found on the host (`models` in its detection payload);
anything it reports is listed ahead of the catalog.

### Reasoning effort

Each agent has an optional **Effort** setting (frontmatter `effort`,
`agents.effort` column): one of `low`, `medium`, `high`, `xhigh`, `max`, or
empty for the provider default. It is passed through to the vendor CLI by the
runner adapters (`internal/runner`):

- **Claude Code** — passed verbatim as `--effort <level>`.
- **Codex CLI** — set via `-c model_reasoning_effort=<level>`; `xhigh` and
  `max` are mapped down to `high` (the CLI's top tier).
- **Gemini CLI** — ignored (no headless reasoning-effort control).

### Tools an agent may use

`allowed_tools` (frontmatter, and **Allowed tools** in the agent editor) is a
comma-separated allowlist passed to the vendor CLI. `mcp__openv__*` grants the
OpenV tool surface; the vendor's own built-in tools can be named alongside it.

The seeded **V&V Assistant** carries `mcp__openv__*, WebSearch, WebFetch`: the
questions it exists to ask — is this limit real, does that standard say what
you think, is this hazard already covered — usually have an authoritative
source, and it is better for it to check one than to half-remember it. Fetch
accompanies search because search returns snippets, and a snippet is not a
citation.

Two things make that safe enough to ship on by default, and both are worth
keeping if you grant web access to an agent of your own:

- The agent **writes nothing itself**. What it reads can reach a project only
  as a suggestion a person accepted, or — for agents in `proposal` mode — a
  proposal a person approved.
- Its system prompt tells it to treat pages as **someone else's text, not as
  instructions**: a page cannot change its job or what it proposes. Web content
  is untrusted input in exactly the way a pasted document is.

Granting a tool without mentioning it in the system prompt mostly wastes it —
the model is left to discover it has the tool. Say what it is for.

**Changing an existing agent:** a seed describes a *new* workspace. Agents
already provisioned belong to their workspace and are never retrofitted (see
`adoptSeedRename`, which carries `AllowedTools` across untouched), so adding a
tool to a seed does nothing for a workspace that already has that agent — edit
it under **Agents → the agent → Allowed tools** instead.

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

## The AI context surface

Reading a whole project through `list_artifacts` costs tens of thousands of
tokens, most of them UUIDs, timestamps, and repeated JSON keys. The AI
context surface is the token-optimal read path:

- **Stable refs.** Every artifact has a short address like `REQ-12` or
  `TC-3` — unique within its project, assigned on create, constant across
  versions, preserved when a project export is imported as a new project.
  Prefixes come from the type (`REQ`, `TC`, `HAZ`, `DES`, `NEED`, `PER`,
  `HDG`, `DSC`, `ART`). Tools that take an artifact id also take a ref
  (plus `project_id` for scoping).
- **`get_project_map`** — the whole project as an indented outline: one
  line per artifact with ref, title, non-draft status, and inline link
  annotations (`→verifies TC-3`, `←refines REQ-2`, trailing `?` = suspect).
  Roughly 10× fewer tokens than the full listing; use it to orient. Also
  served raw at `GET /api/v1/projects/{id}/ai-map`.
- **Release-stamped maps.** Pass `baseline_id` and the map renders from
  that baseline's snapshot, stamped with its name and capture time — so
  every baseline/release produces a versioned map. Committing it into a
  code repo (e.g. `.openv/requirements.md`) gives any coding agent
  requirements context with zero round trips:

  ```sh
  curl -H "Authorization: Bearer $KEY" \
    "$OPENV_URL/api/v1/projects/$PROJECT/ai-map?baseline_id=$BASELINE" \
    > .openv/requirements.md
  ```

- **`get_context`** — one call, addressed by ref or id, returning the
  artifact's full body, its ancestor path, children, and every linked
  artifact with direction, link type, suspect flag, and a short excerpt.
  Replaces a `get_artifact` + `list_links_for_artifact` + N×`get_artifact`
  round trip.

The recommended agent read pattern is: `get_project_map` once, then
`get_context` on the handful of refs the task touches. The dogfood example
in `examples/openv-ai-context/project.json` specifies this feature in
OpenV's own terms — import it and run `get_project_map` to watch the
feature describe itself.

## Crews and the org chart

Agents can be grouped into **crews** (formerly "teams") with a simple
org-chart graph: a lead agent can delegate work to member agents, and
orchestration hooks route follow-up runs along the graph's edges
(`delegates-to`, `hands-off-to`, `reviews`). Crews can also contain **human
members** — hand-offs to a human create a card on the project board instead
of launching a run. Use crews when a task naturally splits (e.g. one agent
drafts requirements, another reviews for testability).

The canonical API is `/api/v1/crews` (with `/crew-nodes`, `/crew-edges`); the
old `/api/v1/teams` (and `/team-nodes`, `/team-edges`) paths remain as
deprecated aliases of the same handlers (`internal/api/agent_handlers.go`).
Crews pinned to a project require project editor rights to modify;
workspace-wide crews require workspace admin.

## Automations

Automations launch runs without a human clicking a button. Three trigger kinds:

- **manual** — run on demand from the UI.
- **scheduled** — the cron scheduler launches the run on a schedule (e.g.
  nightly gap-analysis review).
- **triggered** — the trigger matcher listens to the event bus (artifact
  created, test result recorded, work item moved, ...) and launches matching
  automations.

## Kanban-driven runs

Work items on the project kanban can drive agent work: moving a card into an
agent-owned column enqueues a run for that item, and the agent's progress and
comments land back on the card's activity feed. This makes "give this ticket to
the agent" a drag-and-drop action.

## Agent-executed V&V test runs

A test run's automated test cases can be handed to an agent: open the run and
press **Run N with agent**. The server builds the instruction, naming exactly
which test cases to execute and their IDs, and the agent records each outcome
with the `record_test_result` MCP tool. Results it records are stamped with the
agent run that produced them, and show a `⚙ agent` marker next to the execution
time so a reviewer can tell agent-executed evidence from human-executed.

Agents are told to record `blocked` (with a reason) rather than guess when they
cannot actually execute a case, and never to edit a test case to make it pass.

### Human- and physically-verified test cases

Not every test can honestly be run by software. Each test case carries an
`execution_method` attribute, set in its editor under **How is this verified?**:

| Value | Meaning |
| --- | --- |
| `automated` (default) | An agent or CI job can run it end to end. |
| `manual` | Needs a person: inspection, judgement, usability. |
| `physical` | Needs hardware, a rig, or lab measurement. |

`manual` and `physical` cases are excluded from the agent's instruction
entirely — their IDs are never given to it — and the API refuses any result an
agent run tries to record for one (`403`). They stay in the run for a person to
execute by hand in the same grid. A result applied from an approved *proposal*
is not treated as agent-executed, since a human signed off on it.

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
- [ ] A runner: a leased cloud runner (nothing to install — needs
      `RUNNER_POOL_KEY` and a pool), a personal key from Settings → My Runner
      (or the Agent Connector pairing flow), or a workspace key from
      Settings → Worker Keys
- [ ] `make worker` (or `make worker-unix`) built `bin/` — or use the
      connector download
- [ ] `agentd` running and provider shows green in Settings
- [ ] Agent definitions present under `$OPENV_DATA_DIR/agents/<org-id>/`

See also: `docs/architecture.md` (Multi-agent suite section) for the internal
topology, and `docs/link-type-rules.md` for the traceability vocabulary agents
are expected to use.
