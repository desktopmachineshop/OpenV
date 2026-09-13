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
