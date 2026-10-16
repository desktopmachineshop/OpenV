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

## Runs with no project: Workspace runs

Some runs belong to no project: the scheduled and event runs of a
whole-workspace automation (and its **Run now**), the products an agent
invents from the projects list, and the runs of a project that has since
been deleted. No project's Runs page lists them; **Workspace runs** does.
Open it from the workspace menu (the workspace's name, top left), beside
**Workspace settings**.

- It lists the workspace's runs with no project, newest first, with the same
  columns, **Status** filter and detail panel as a project's Runs page:
  click a run to follow it, cancel it or retry it.
- The address names the run that is open (\`?run=\`), so a reload or a
  shared link opens it again.
- A **workspace admin** sees every such run; any other member sees the ones
  they launched.
- A notification that such a run failed, in the bell, by email or as a web
  push, opens it here.

## Pending approvals (proposal review)

Agents with *proposal* write mode don't change your project directly — each
write becomes a proposal. The **Pending approvals** counter at the top of the
Runs page opens the review panel where you **approve** or **reject** each
proposed artifact change, link, or test result. Only approved proposals are
applied.

## Runner types

### Personal runner (your machine, your subscription)

Runs on **your own computer**, signed into the vendor CLIs (Claude Code,
Codex, Gemini) with **your own subscription**. It claims **the runs you
launched** — never a teammate's — and ownerless work only where you could
see it yourself (see *How runs are routed* below).

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

The card shows how busy the shared pool is as a traffic light rather than a
count — **Runners available** (green), **Runners busy** (amber) or **All
runners in use** (red). You can hold one runner at a time, so the only
question it needs to answer is whether starting one now is likely to work. If
every cloud runner is in use, the card says so: they're leased one member at
a time, so you either wait or use your own machine.

**On a phone** the whole lease is one column: the status, the countdown and
the pool's traffic light, each under its own label, with **Start a cloud
runner** / **Extend the lease** / **End now** as full-width buttons you can
hit with a thumb. The sign-in relay below it is built for a phone too — the
**Open sign-in page** link hands the vendor's page to your browser, and the
field it comes back to takes a paste from your password manager (there's a
**Paste** button where the browser allows it) and submits with the keyboard's
send key.

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
whole workspace. Give the key to the worker in its environment, as
\`WORKER_API_KEY\`, not on its command line, where every user of the machine
can see it.

On a deployment you run yourself, the server's own key (\`WORKER_API_KEY\` in
the server's settings) is listed there as **env-bootstrap** once the server
has registered it. Revoking it stops it at once, even while the server still
has that value, and it stays revoked across restarts until the operator sets
a new one.

### What an agent can read on its runner

An agent runs as the same operating-system user as the runner it runs on, so
it can read whatever that user can: on a personal runner that includes your
files and the Agent Connector's key file, and on Windows and macOS the
runner's own environment, where its key is. The runner never hands its keys
to the programs it starts, and on Linux it also stops them reading its
environment, but it cannot keep an agent from your user's own files. For
agents you don't fully trust, use a **cloud runner** or the **hosted
runner**, which run on Linux with nothing of yours on them, or run the worker
as a separate user on your machine.

## Connect an AI app to a workspace

An AI app that speaks MCP, such as Claude or Cursor, can use OpenV's tools
directly, with no runner and no key to copy. Its address is your OpenV
address followed by \`/mcp\`, for example \`https://openv.app/mcp\`;
**Settings → Connected apps** shows the exact one.

1. Add OpenV to the app:
   - **Claude** (claude.ai, the desktop app and the mobile app): open
     **Settings → Connectors**, choose **Add custom connector**, name it
     OpenV and paste the address. On a Claude Team or Enterprise plan an
     owner of your Claude organization adds it first, and each member then
     connects.
   - **Claude Code**: run
     \`claude mcp add --transport http openv https://openv.app/mcp\`, then
     \`/mcp\` to sign in.
   - **Another app**: add OpenV as a **remote MCP server** at the address.
2. The app opens OpenV in your browser. Sign in if asked, choose the
   **workspace** the app may work in and whether it may **read and change**
   or **read only**, and choose **Connect**.
3. You go back to the app, which can now list your projects, read and edit
   artifacts and links, record test results and comment, as you.

Everything the app does is recorded as yours, it reaches only the workspace
you chose, and your role there still applies: a viewer's app cannot edit.
It cannot start agent runs. To stop it, choose **Disconnect** beside it
under **Settings → Connected apps**; it stops working at once. To work in
another workspace, connect the app again and choose that one.

## How runs are routed

- When you launch a run and a runner of yours is online — your own machine or
  a cloud runner you hold — the run is **reserved for your runner** for a grace
  period (default 60 seconds). If it isn't claimed in time, hosted/workspace
  runners take over.
- **Ownerless runs** (automations, board triggers, crew delegations) are
  claimable immediately, first come first served, by every workspace or
  hosted runner, and by the personal runner of every member who could see
  the run: one with a role in its project, or a workspace admin. A personal
  runner never takes a run its owner could not see, so an ownerless run with
  no project, which only workspace admins can open, goes to an admin's
  personal runner or to the workspace and hosted runners.
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

A workspace admin pastes that key once, in **Workspace settings → AI
providers**, on the provider's card (one Anthropic key serves Claude Code and
the Anthropic API). It is saved encrypted and never shown again — only its
last four characters — and every runner that picks up a run of an API-key
project gets it with the run, members' own machines included, so nobody sets
an environment variable. **Remove key** takes it back; runners then fall back
to the variable on their own machine, if it is set there.

## Repositories

**Project Settings → Repositories** connects repositories (name, remote URL,
default branch). The checkout location is **per member** — each person sets
"your local path" for their own machine. Personal runners use the claiming
member's path; without one, the repository URL is cloned fresh — which is
always what a cloud runner does, since it has no checkout of yours.
`;

export default content;
