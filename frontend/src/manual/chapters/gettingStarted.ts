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

Some servers sign everyone in through **single sign-on only** (on
openv.app, that is DMSid, the one login for every DMS SaaS product). There
the sign-in screen has no form and no buttons: opening it, or any *Sign up*
or *Create free account* link, takes you straight to the provider's own
sign-in or sign-up page and brings you back signed in. After you sign out,
or if signing in did not finish, the screen stays put with a button to
continue or **Try again**, and says what happened: that you cancelled at the
provider, that the provider has not confirmed your address, or that the
sign-in did not complete. An account you made earlier with a password is
still yours: the first time you sign in through the provider at the same
address, OpenV asks for that account's password once to connect the two,
and from then on the provider signs you in. If you no longer know that
password, **Forgot your OpenV password?** on the same screen emails you a
link to set a new one (see *Forgot your password?* below); setting it does
not sign you in, it lets you finish connecting. An invitation link names the
workspace and the address it was sent to; continue to the provider and sign
in with that address to join. To sign in once connected, use the provider's
own password and its own reset.

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

Open **your settings** from the account menu (your picture at the right end of the top bar; on a phone, your name at the foot of the project menu)
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

On a server that signs everyone in through single sign-on only, the reset
is for one thing: connecting the provider to an account you made earlier
with a password. Setting the new password does not sign you in and does not
bring the password form back. The screen says your password is set; continue
to the provider, and enter the new password when OpenV asks for it.

### Removing your account

On a server that signs people in with DMSid, your DMSid page lists the
products you can be removed from. **Remove my account from OpenV** (and
**Delete my DMSid**, which removes you from every product first) ends your
OpenV account:

- You are signed out everywhere, and your runner keys, connected AI apps,
  notifications, workspace memberships and pending invitations go.
- A workspace only you belong to, your personal one among them, is deleted
  with everything in it.
- What you wrote in workspaces other people use — requirements, links,
  comments, test results, baselines — stays for them, shown as written by
  **Deleted user**. Nothing left there names you or your address.

Two things stop it, and DMSid shows you which, with nothing removed: being
the **only admin** of a workspace that has other members (make someone else
an admin, or remove the others, first), and a workspace that only you belong
to still having a **paid subscription** (cancel it in the workspace's
billing settings first). Signing in with DMSid again afterwards starts a new,
empty OpenV account.

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
- Inside a project, at the left end of the top bar (on a phone, at the top of
  the project menu).

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

Open **your settings** from the account menu (your picture at the right end
of the top bar) → **Notifications** to choose how the high-signal four reach you:

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

## Finding your way around

Every page is framed the same way: a narrow **rail** down the left and one
**top bar** across the top, so the work itself gets the rest of the screen.
The frame is the same inside a project and on the workspace's own pages
(Projects, workspace settings, workspace runs, What's new and platform
admin); only the rail's buttons change.

- **The top bar** reads left to right: the **workspace switcher**, the
  **project switcher**, the search across every project, **Help**, the
  notification bell and the account menu.
  - **Switch workspace**: click the workspace's name for the list of your
    workspaces, workspace settings and runs, and creating a company
    workspace.
  - **Switch project**: click the project's name (it reads **All projects**
    on a workspace page) for the workspace's projects. Choose one to open
    it at its Overview; **All projects** opens the Projects page and **+ New
    project** starts a project from wherever you are.
- **The rail inside a project** has a button for each part of the project,
  in the order the work runs — **Plan**, **Define** and **Verify** — then,
  below a rule, **Agents** and links to **Activity**, **Settings** and, for
  platform admins, **Admin**. Click a group to
  open its pages beside the rail (Plan holds Overview, Board, Gantt
  and Releases, for example), then click the page. The group holding the
  page you are on is marked. The list closes when you arrive, or with
  **Esc** or a click anywhere else.
- **The row under the top bar** lists the pages of the group you are in,
  with the one you are reading underlined, so the page next door is one
  click away. It is where a page is named: the page itself does not repeat
  its name. Activity and Settings belong to no group, so they have no row.
- **The rail on a workspace page** is laid out the same way: **Home**;
  **Plan**, which holds every project's plan (**All projects**) and what is
  yours across them (**My tasks** and **To-dos**); then **Agents**, which
  holds the workspace's agents and their **Runs**, **Settings**, and
  **Admin** for platform admins. The row under the top bar lists the pages
  of the group you are in, as inside a project. What's new is not in the
  rail: a release reaches you as a notification, and its notes are in your
  account menu and beside the release in the workspace's settings.
- **Home** is at the top of the rail on every page and opens the
  workspace's Home: what needs you across every project, the workspace's
  activity, and its projects to choose from.

On a phone there is no rail. On a workspace page the top bar holds the
workspace switcher above the project switcher, with help, the bell and your
account beside them. Inside a project the top bar names the project, and a
bar along the bottom reads **Home**, **Plan**, **Define**, **Verify**,
**More** from left to right: the same groups in the same order as the rail
on a computer. A group's tab opens the page you last read in that group,
and the row under the top bar lists the group's other pages (swipe it
sideways when it is wider than the screen). **More** is the whole menu as a
page of its own: the workspace switcher, the project switcher and the
search, then every page as a tile under its group, **Help** for the page you
were on, the **Manual**, **All projects**, and under **You** your account
menu. The same bar stays under a workspace page: **Home** is the workspace's
Home, a group's tab opens the project you last opened at the page you last
read in that group (it is greyed until you have opened a project), and
**More** lists the workspace's own pages.

A form that opens as a dialog on a computer (recording evidence, launching
a run, the download wizard) takes the whole screen on a phone: its title
stays at the top beside **✕**, which closes it, and its fields scroll
underneath. A short question, such as a confirmation, stays a small card.
Every page keeps the same margin from the edge of the screen.

## Keyboard shortcuts

| Key | What it does |
| --- | --- |
| **?** or **F1** | Opens or closes the help panel for the page you are on |
| **Esc** | Closes the open project menu or group of pages |
| **J** / **K** | Next / previous artifact in the Requirements document |
| **Alt + A** (Option + A on a Mac) | Opens the V&V Assistant and puts the cursor in its message box |
| **Esc** in the V&V Assistant | Minimises it back to its button |
| **Ctrl + Enter** | Sends your message to the V&V Assistant |

**?** is ignored while you are typing in a field, so it can still be typed;
**F1** works everywhere.

## Help on every page

Press **Help** in the top bar, or the **?** or **F1** key, to open the help
panel (on a phone, tap **?** in the top bar). Outside a project — the
projects list, workspace settings, workspace runs, platform admin and
*What's new* — the **?** sits beside the notification bell, and the keys work
there too. It shows
the page of this manual for the screen you are on, written for the release
your workspace is on, so what it describes matches what you see. Links
between pages open in the panel, **Back** returns, and the search box at
the top searches the whole manual. **Open in the docs** opens the same page
on the docs site, where a menu lets you read the manual for other releases.

Help also follows what you are doing on the page. It opens on the part of
the manual for the form, field or dialog you are working in, the button you
pressed last or the tab you are on, and on the page's own topic where the
manual has nothing more specific. For example, the board's **+ Add card**
opens *Creating a card* and a card's drawer *The card drawer*; the agent
editor's write-mode setting opens *Write mode: proposal vs direct*; **New
Run** on the V&V page opens *Test runs*; the **Members** tab of project
settings opens *Project access (members)*; and the link panel's add-link
form opens *Creating links*. While the panel is open it moves with you, a
moment after you change section or tab. Once you follow a link or search in
the panel, it stays on what you chose until you go to another page or open
it again. Only which section of the screen you are in is used to choose the
page, never anything you have typed.

## What's next

- Create your first project — see *Projects & members*.
- If you want AI agents working in your projects, set up a runner — see
  *Runs & runners*.
`;

export default content;
