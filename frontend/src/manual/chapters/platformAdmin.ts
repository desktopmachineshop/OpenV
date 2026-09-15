// User manual chapter: Platform admin.
const content = `
# Platform admin

A **platform admin** runs the deployment itself. The role passes every
check — every workspace, every project, every setting — so it is held by
few people. It is separate from being a workspace admin.

## Who is a platform admin

- The **first account registered** on a deployment is the platform admin.
  On a fresh self-hosted OpenV, register first and that account is it.
- A platform admin makes others: open **Platform admin** from the account
  menu (top right), find the person under *Platform admins* and click
  **Make admin**. **Remove** takes it away again. You cannot remove your
  own standing, and the last platform admin cannot be removed.

## Password reset links for support

Somebody who cannot get back into a password account — the reset email did
not arrive, the address was mistyped, or the server sends no mail at all —
asks you. In the *Platform admins* table, every password account has a
**Reset link** button. Confirm, and the page shows a link **once**: copy it
and pass it to the person however you talk to them. It works once, expires
after 24 hours, and lets whoever holds it set a new password for that
account, which signs the account out everywhere. Because the link did not
go through the person's inbox it proves nothing about their address, so
hand it to somebody you have identified. Each link you make is written to
the server log with your account. Accounts that sign in through Google or
single sign-on have no password, so they have no button.

## Workspaces and plans

The *Workspaces* table lists every workspace on the deployment with its
type, plan, member count and creation date. Change a plan from the
selector: a plan decides the workspace's limits and its default release
channel.

The **Open source** plan is how an open-source project is hosted free:
choose it and every project in that workspace is published, as of its
latest baseline, on the site's [open-source page](/open-source). Live work
stays private until the next baseline. The page asks you to confirm
before it changes a plan.

The same change is available from the command line for scripts:
\`PUT /api/v1/orgs/<id>/plan {"plan": "open_source"}\` signed in as a
platform admin.
`;

export default content;
