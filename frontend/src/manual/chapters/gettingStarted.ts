// User manual chapter: Getting started. Markdown shipped as a TS module
// because CRA cannot raw-import .md files without ejecting.
const content = `
# Getting started

OpenV is a requirements management platform with full traceability, verification
& validation (V&V) tracking, and a built-in multi-agent suite. This chapter gets
you from the sign-in screen to your first workspace.

## Signing in

Open the app in your browser and you land on the sign-in screen.

- **Sign in** with your email and password.
- **Create a new account** switches the form to registration: enter your name,
  email, and a password (minimum 8 characters).
- **Sign in with Google** appears when the server has been configured with a
  Google OAuth client. If it is not configured, a note on the sign-in screen
  says so.

The **first user to register becomes the server admin**.

After signing in you are taken to the **Projects** page.

## Workspaces

Everything in OpenV lives inside a **workspace** (also called an org):

- **Personal workspace** — created for you automatically. Marked with a
  "personal" pill. Good for solo work and trying things out.
- **Company workspace** — a shared workspace for a team or organization.
  Anyone can create one: open the workspace switcher and choose
  **+ Create a company workspace**. The creator becomes its admin (shown with
  an "admin" pill).

### Switching workspaces

The workspace switcher appears in two places:

- On the **Projects** page, in the "Workspace:" box at the top right.
- Inside a project, at the top of the dark left sidebar.

Click the workspace name to open the dropdown. It lists all your workspaces
(a green check marks the active one) and offers:

- **Workspace settings** — opens the settings page for the active workspace
  (see the *Workspace settings & teams* chapter).
- **+ Create a company workspace**

Switching workspaces returns you to the Projects page and shows that
workspace's projects. Your choice is remembered between visits.

## What's next

- Create your first project — see *Projects & members*.
- If you want AI agents working in your projects, set up a runner — see
  *Runs & runners*.
`;

export default content;
