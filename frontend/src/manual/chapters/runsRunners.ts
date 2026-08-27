// User manual chapter: Agent runs, proposal review, and runners.
const content = `
# Runs & runners

Every piece of agent work is a **run**. Runs are executed by **runners** —
worker processes that drive an AI vendor CLI on a real machine. The server
itself never runs a model; it queues work and reviews results.

## The Runs page

**Runs** in the project sidebar lists agent runs with status, start time,
duration, token usage, and cost. Statuses:

queued · running · awaiting_approval · succeeded · failed · timed_out ·
cancelled

- Click a run to open the **detail panel**: the prompt, live progress/log,
  results, and links to related runs (e.g. crew delegations).
- The **Status** dropdown filters the list.
- A queued run may show **"reserved for launcher's runner"** — it's waiting
  briefly for the launcher's personal runner before other runners may claim it.

## Pending approvals (proposal review)

Agents with *proposal* write mode don't change your project directly — each
write becomes a proposal. The **Pending approvals** counter at the top of the
Runs page opens the review panel where you **approve** or **reject** each
proposed artifact change, link, or test result. Only approved proposals are
applied.

## Runner types

### Personal runner (your machine, your subscription)

Runs on **your own computer**, signed into the vendor CLIs (Claude Code,
Codex, Gemini) with **your own subscription**. It claims **only runs you
launched** — never a teammate's.

The easiest setup is the **Agent Connector**:

1. When a run is queued and no runner is online, the app prompts you — or open
   your user settings (bottom-left of the sidebar) → **My Runner**.
2. **Download** the connector bundle, unzip it somewhere permanent, and run it
   once so it registers itself.
3. Click **Pair connector** in OpenV — the browser opens the connector with a
   one-time pairing code, which it exchanges for your personal runner key.
4. From then on, opening the connector starts your runner; closing its window
   stops it. Online/offline status shows live in the UI.

You can also mint a personal runner key manually and run the *agentd* worker
yourself. One personal key is active per member; minting a new one rotates the
old.

### Hosted runner (always-on, org API keys)

A workspace admin can provision a **hosted runner** — a platform-managed
container — under **Workspace settings → Runners**. At setup the admin enters
the org's provider **API keys** (Anthropic / OpenAI / Gemini); they are
injected into the container only and never stored by the platform.

- Always on: claims **ownerless work** (automations, cron and event triggers,
  board-driven runs) and **overflow** — runs whose launcher has no runner
  online once the grace window expires.
- **No repository access**: runs that need a cloned repo are refused and wait
  for a personal or workspace runner (the Runs page shows a banner when this
  happens).
- Billed against the org's API keys, not anyone's subscription.

### Workspace worker keys

Admins can also mint shared **worker keys** (Workspace settings → Runners) for
self-managed always-on workers, e.g. a machine in your shop that serves the
whole workspace.

## How runs are routed

- When you launch a run and your personal runner is online, the run is
  **reserved for your runner** for a grace period (default 60 seconds). If it
  isn't claimed in time, hosted/workspace runners take over.
- **Ownerless runs** (automations, board triggers, crew delegations) are
  claimable by any live runner immediately, first come first served.
- Runs another member launched are never routed to your personal runner.

## Signing the CLIs in

Personal runners use your own CLI sign-ins:

- Open your user settings (bottom-left of the sidebar) → **Agent sign-ins**.
  Each provider card relays the CLI's own sign-in flow — an "Open sign-in
  page" link plus, for CLIs that use a paste-back code (Claude Code, Gemini),
  a field for the authorization code. Credentials stay on your machine.
- Alternatively sign in from a terminal on the runner machine.
- Workspace admins can run workspace-targeted sign-ins from **Workspace
  settings → AI Providers → Connect** for shared workers.

## Per-project agent auth

**Project Settings → Agents** chooses how the project's runs authenticate:

- **User account** (default) — runs use the launcher's own local CLI sign-in.
- **API key** — overrides local sign-ins; runners inject the workspace's
  provider API key into the CLI instead.

## Repositories

**Project Settings → Repositories** connects repositories (name, remote URL,
default branch). The checkout location is **per member** — each person sets
"your local path" for their own machine. Personal runners use the claiming
member's path; without one, the repository URL is cloned fresh.
`;

export default content;
