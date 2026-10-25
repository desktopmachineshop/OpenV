// User manual chapter: Getting started. Markdown shipped as a TS module
// as a TS module so it is typed and bundled with the rest of the manual.
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
- **Already have a password account?** Signing in with Google (or your
  server's single sign-on) at the same address brings you back to the
  sign-in screen, which asks for that account's password once. Enter it and
  Google is connected: from then on either one signs you in to the same
  account. The password is what proves the account is yours, so an account
  made with Google or single sign-on cannot be joined to another provider
  this way.

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

### Forgot your password?

On the sign-in screen, **Forgot your password?** asks for the address you sign
in with and emails it a link to set a new password. The link works once and
expires after an hour; the page says the same thing whether or not the
address has an account, so it cannot be used to find out who has one. Setting
the new password signs the account out everywhere, and you sign in again with
it. If nothing arrives, check the address and your spam folder.

On a server that cannot send email the screen says so, and the way in is your
**OpenV administrator**: a platform admin can make a reset link for your
account (see the *Platform admin* chapter) and pass it to you. That link lasts
a day and works once.

Accounts that sign in with Google or single sign-on have no password to
reset — sign in there.

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

### Where you start

OpenV opens in your **personal workspace** unless you say otherwise. If you
do most of your work in a company workspace, open the account menu →
**Settings** → *Default workspace* and pick it: every sign-in then starts
there. The list holds your personal workspace and every company workspace
you belong to; leaving a workspace quietly returns your sign-in to your
personal one. Switching workspaces during a session is unchanged, and a tab
you reload stays in the workspace it was in. On a stable-channel workspace
this setting arrives with the workspace's next stable release.

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

## Help on every page

Press **?** beside the notification bell to open the help panel. It is on
every page you reach signed in, workspace settings, Runs and What's new
included. It shows the page of this manual for what you are doing, written
for the release your workspace is on, so what it describes matches what you
see:

- **It follows what you are doing.** The panel opens on the topic for the
  screen you are on, and goes further where the manual does: the tab of a
  settings page, the step of a guided definition, a dialog such as citing
  evidence or downloading a project. Open a dialog with the panel open and
  it turns to the dialog's topic; close the dialog and it turns back.
- **Small ? buttons** beside section headings, dialog titles and empty
  pages open the panel at that part of the manual: coverage, gaps and test
  runs on the V&V page, a requirement's quality findings, the traceability
  matrix, a board card.
- **From the keyboard,** press **?** anywhere you are not typing, or **F1**,
  to open help for what is on screen.

Links between pages open in the panel, **Back** returns, and the search box
at the top searches the whole manual. **Open in the docs** opens the same
page on the docs site and **Full manual** the docs site's front page, each
in a new tab and at the release your workspace is on; a menu there lets you
read the manual for other releases.

## What's new

**What's new**, in the menu under your name, lists what changed in OpenV,
newest release first. It follows your workspace's channel: a workspace on
the stable channel sees the stable release it is on and the one scheduled
next, and a nightly workspace the release it runs. When OpenV is upgraded
while you have it open, a note at the bottom of the screen says so, with
**See what's new** and **Reload**.

## What's next

- Create your first project — see *Projects & members*.
- If you want AI agents working in your projects, set up a runner — see
  *Runs & runners*.
`;

export default content;
