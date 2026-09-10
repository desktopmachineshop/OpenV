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

After signing in you are taken to the **Projects** page. On a server that
sends email, a new account first sees a **Check your inbox** page: click the
link in the email (valid for 24 hours) and you are through. The page can
resend the email, send it to a corrected address, or sign you out.

Some servers **close registration**: there is then no *Create a new account*
button, and the sign-in screen tells you to ask a workspace admin for an
invitation. The invitation **link** is the way in — it opens the sign-up form
with your address already filled in, and joins you to the workspace that
invited you. Use the link itself, whether you are creating an account or
signing in to one you already have; typing the invited address into a sign-up
form without the link joins nothing, and on a closed server does not let you
sign up at all. If you are already signed in when you open the link, OpenV
shows you the invitation and you press **Join** — and if you are signed in as
a different address, it says which address to sign in as instead. Single
sign-on is unaffected: an invited address joins its workspaces as it signs
in, because the identity provider vouches for the address.

### Your password and your sessions

Open **your settings** from the user block at the bottom-left of the sidebar
to **change your password** — current password, then the new one twice.
Changing it signs out every other browser and device you are signed in on,
which is what you want if you are changing it because the old one may have
leaked; the browser you are using stays signed in. Accounts that sign in with
Google or single sign-on have no password here — change it at that provider.

Sessions do not last forever: one expires a set time after you sign in, and
sooner if you stop using it. Your server's administrator sets both limits
(30 days and 7 days unless they shortened them), and you simply sign in again.

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
