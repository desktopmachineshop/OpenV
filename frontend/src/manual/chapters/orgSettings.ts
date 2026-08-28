// User manual chapter: Workspace settings & teams.
const content = `
# Workspace settings & teams

**Workspace settings** (workspace switcher → *Workspace settings*, or the
Runner settings links around the app) manage the active workspace. Most tabs
are admin-only; other members see read-only or explanatory views.

## General

- **Workspace name** — admins can rename the workspace.
- **Plan** — the workspace's plan (billing coming soon).
- **Details** — workspace ID, slug, and creation date.

## Members

The people in the workspace. Admins can add members by email — the person
must already have an OpenV account (no invite emails are sent) — and manage
each member's workspace role (**member** or **admin**; new members default to
member). Note the distinction:

- **Workspace membership** gets someone into the workspace.
- **Project access** is granted per project (Project Settings → Access) —
  directly or via teams.

## Teams

**Teams** are named groups of workspace members (e.g. "Design engineering",
"QA"). Their purpose is project access management: a project can grant a team
access as a unit (Project Settings → Access → team access), with a role per
grant. Admins create teams, add/remove members, rename, and delete them —
deleting a team also removes its project access grants.

> Not to be confused with **Crews**, which are graphs of AI agents (and
> people) that execute work — see the *Crews* chapter.

## AI Providers (admin only)

Per-provider configuration for agent runs:

- **Detection status** — whether the provider's CLI was found on a worker
  host, whether it is logged in, and its version ("never detected" means no
  worker has reported yet).
- **Default model** — the model agents use when set to *provider default*.
- **Connect** — start a workspace-targeted CLI sign-in that any of the
  workspace's shared workers can pick up. (Personal sign-ins live in your user
  settings instead — see *Runs & runners*.)
- Enable/disable providers for runs.

## Runners

Everything that executes agent work:

- **Hosted runner** — admins can provision the always-on, platform-managed
  runner container by entering the org's provider API keys. Status, stop/start
  and remove (optionally purging its data volume) are managed here. Non-admins
  see read-only status.
- **My runner** — your personal runner key and online status, with the Agent
  Connector pairing flow. Also reachable from your user settings.
- **Worker keys** — shared workspace keys for self-managed always-on workers.
  The plaintext key is shown **once**, right after creation — store it
  safely. Keys can be revoked at any time.
`;

export default content;
