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

### Cloud runner (nothing to install, yours for a while)

A **transient runner**: a runner the platform already has warm, leased to
**you alone** for a stretch of time. Press **Start a cloud runner** (user
settings → **Cloud runner**, or the prompt a queued run shows), sign your
agents into it from this browser, and launch runs — no download, no pairing,
no machine of your own involved.

The trade is that it doesn't last:

- It ends on its **idle window** (default 15 minutes after you stop using it)
  or its **maximum lifetime** (default 60 minutes), whichever comes first. The
  card counts down to whichever is nearer, and the clock resets whenever the
  runner is actually working.
- **Extend** pushes both clocks out; **End now** hands it back immediately.
- When it ends it is **wiped** — including the agent sign-ins you made on it.
  Your next cloud runner starts by signing in again. Sign-ins on your own
  machine are untouched, as is your Agent Connector pairing.

If every cloud runner is in use, the card says so: they're leased one member
at a time, so you either wait or use your own machine.

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

- When you launch a run and a runner of yours is online — your own machine or
  a cloud runner you hold — the run is **reserved for your runner** for a grace
  period (default 60 seconds). If it isn't claimed in time, hosted/workspace
  runners take over.
- **Ownerless runs** (automations, board triggers, crew delegations) are
  claimable by any live runner immediately, first come first served.
- Runs another member launched are never routed to your personal runner.

## Signing the CLIs in

Personal and cloud runners both use your own CLI sign-ins:

- Open your user settings (bottom-left of the sidebar) → **Agent sign-ins**.
  Each provider card relays the CLI's own sign-in flow to whichever runner of
  yours is online, and each CLI does it differently.
- **On your own machine:** **Gemini** shows an "Open sign-in page" link plus a
  field to paste the authorization code back; **Codex** opens its sign-in page
  in a browser on that machine; **Claude Code** opens its sign-in in a terminal
  window there. Credentials stay on your machine and persist.
- **On a cloud runner:** there is no terminal or browser to open, so every step
  comes back to this browser. **Claude Code** and **Gemini** show a link and a
  paste-back field. **Codex** sends you to its sign-in page and then redirects
  your browser to an address only the runner can serve — you'll see a
  connection error, which is expected: copy that whole address from the
  address bar and paste it into the card. These sign-ins last only as long as
  the runner does.
- Alternatively sign in from a terminal on your own runner machine.
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
member's path; without one, the repository URL is cloned fresh — which is
always what a cloud runner does, since it has no checkout of yours.
`;

export default content;
