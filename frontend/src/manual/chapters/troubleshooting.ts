// User manual chapter: Troubleshooting & FAQ.
const content = `
# Troubleshooting & FAQ

## Agent runs stay "queued"

No runner is online to claim them. The Runs page shows a banner with an
**Open Agent Connector** button — open (or install and pair) the connector, or
ask an admin to provision the hosted runner. See *Runs & runners*.

## "Some queued runs need repository access"

Only the hosted runner is online, and hosted runners never clone repositories.
Runs that need repo access wait for a personal or workspace runner — start
your Agent Connector.

## The guided copilot doesn't answer

Same cause: no runner online. The chat panel says replies are paused; your
messages are saved and answered as soon as a runner connects.

## A provider shows "never detected" or not logged in

The vendor CLI isn't installed on the runner machine, isn't on its PATH, or
hasn't been signed in. Install the CLI on the machine that runs your runner,
then sign in — from your user settings → Agent sign-ins (personal runner) or
Workspace settings → AI Providers → Connect (shared workers).

## I can't add someone to a project

Adding a member by email requires that the person **already has an OpenV
account**. Ask them to sign up first, then add them with the same email under
Project Settings → Access.

## A link won't create

Link types are directional and type-checked — for example *verifies* only goes
from a test-case to a requirement. Check the table in *Traceability links*;
the link panel only offers valid combinations, and the server explains
rejections.

## Everything is read-only all of a sudden

You're probably viewing a **baseline**. Baselines are immutable snapshots —
switch the baseline selector back to **Live** to edit. Completed or aborted
**test runs** are read-only too.

## A board card won't drag

A card assigned to an agent or crew is locked while its run is live (pulsing
blue dot). Wait for the run to finish, or cancel it from the Runs page.

## An interview link stopped working

The interview was **closed** — closing revokes its invite links, and a closed
interview can't issue new ones. Create a new interview if you need more
sessions.

## I abandoned a guided session — where did my work go?

Draft artifacts that were already materialized (green dot) are **kept** in the
project with draft status; only unsaved entries were discarded. Find them in
the Requirements view; delete the ones you don't want.

## Where does an agent's change actually land?

With *proposal* write mode (the default) every agent write waits under
**Pending approvals** on the Runs page until you approve it. If you can't find
an agent's output, check there first.

## Whose AI subscription pays for a run?

- Runs on your **personal runner** use your own CLI subscription, and your
  runner only ever claims runs you launched.
- Runs on the **hosted runner** bill against the org's provider API keys.
- Projects set to **API key** auth (Project Settings → Agents) always use the
  workspace's key regardless of who launches.

## How do I back up a project?

Use **↓ Export project** on the Projects page — it downloads the full project
as JSON, which **↑ Import Project** can restore on any server.
`;

export default content;
