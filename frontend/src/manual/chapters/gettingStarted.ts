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

## Notifications

The **bell** in the top bar is your inbox. It badges unread items and updates
live, so a failed run or a proposal waiting on you appears without a reload.
Clicking an item marks it read and takes you to its subject.

Four kinds of event are treated as **high-signal** and can follow you out of
the app: a run you launched failing, an agent proposal awaiting your
approval, an artifact entering the review queue, and your workspace crossing
80 % or 100 % of its monthly budget. Comment @mentions and finished
interviews stay in the bell only — they are too frequent to be worth
interrupting you for.

Open **your settings** (your name, bottom-left in a project, or the account
menu) → **Notifications** to choose how the high-signal four reach you:

- **Email me** — one plain-text email per event, with a link straight to it.
  On by default, and only does anything when the server has email configured.
- **Push notifications on this device** — a system notification on the phone,
  tablet or desktop you are on, even when OpenV is closed. Off until you turn
  it on, and **set per device**: turning it on on your phone does not turn it
  on on your laptop. Your browser will ask permission the first time.
  Tapping the notification opens OpenV at the thing it is about.

  If the switch is greyed out, the text under it says why: the server has no
  push keys configured, your browser cannot receive push, this site's service
  worker is unavailable (reload, or leave a private window), or notifications
  are blocked for the site in your browser settings. The switch reads off
  until this device is subscribed *and* the server has it on file — if the
  browser is subscribed but the server is not, it says so and turning it on
  again re-registers the device. On an iPhone or iPad,
  push only works once OpenV is **installed to the Home Screen** (Share →
  Add to Home Screen).

In-app notifications are always on; neither switch turns the bell off.

### Installing OpenV

OpenV installs as an app on a phone, tablet or desktop — "Add to Home
Screen" on iOS, "Install app" in Chrome and Edge. Installed, it opens in its
own window with no browser chrome, and long-pressing (or right-clicking) its
icon offers shortcuts straight to your **review queue**, your **board** and
your **notifications**. The review queue and board shortcuts open your most
recent project; the first time, they ask you to pick one.

## What's next

- Create your first project — see *Projects & members*.
- If you want AI agents working in your projects, set up a runner — see
  *Runs & runners*.
`;

export default content;
