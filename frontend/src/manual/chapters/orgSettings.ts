// User manual chapter: Workspace settings & teams.
const content = `
# Workspace settings & teams

**Workspace settings** (workspace switcher → *Workspace settings*, or the
Runner settings links around the app) manage the active workspace. Most tabs
are admin-only; other members see read-only or explanatory views.

## General

- **Workspace name** — admins can rename the workspace.
- **Plan** — the workspace's plan. Every workspace is on the free plan while
  OpenV is in alpha, with every feature included; the hosted-runner limits in
  force are listed on the [pricing page](/pricing).
- **Details** — workspace ID, slug, and creation date.

## Members

The people in the workspace. Admins add members by email and manage each
member's workspace role (**member** or **admin**; new members default to
member). Note the distinction:

- **Workspace membership** gets someone into the workspace.
- **Project access** is granted per project (Project Settings → Access) —
  directly or via teams.

### Inviting someone who has no account yet

Adding an email that already has an OpenV account joins that person straight
away — if they are already in the workspace, OpenV says so rather than adding
them twice; change their role in the members table instead. An email with no
account gets an **invitation**, so you never have to ask someone to sign up
first:

- The invitation is valid for **seven days** and can be used once. It names
  the workspace and the role you chose.
- If the server has email configured, the invitation is sent to that address.
  If it does not, OpenV shows you the invitation **link once**, right after
  you create it — copy it then and send it yourself. It cannot be shown
  again; create a fresh invitation if you lose it.
- Opening the link signed out starts sign-up with the address filled in, and
  the link carries through whether they create an account or sign in to one
  they already have; opening it while signed in simply joins that account to
  the workspace. The link is what grants the membership — somebody who signs
  up for the invited address without it joins the workspace when they confirm
  their verification email instead.
- Re-inviting an address is how you resend: the previous link stops working,
  expired or not, and only the newest one lets them in.
- **Pending invitations** are listed above the Add member form while any are
  outstanding, with who they were sent to and when they expire. *Revoke*
  stops a link working immediately.

On a server where the operator has closed public registration, an invitation
is the only way in besides single sign-on — the sign-in page then says so
instead of offering *Create a new account*.

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
