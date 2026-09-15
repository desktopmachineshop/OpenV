# OpenV release notes

What changed for the people who use OpenV, newest release first. Each
release is a version number and three groups: what you can now do, what
quietly improved, and what stopped being broken.

Contributors: how to add to this file, and what the promotion does with it,
is in [CONTRIBUTING.md](CONTRIBUTING.md#release-notes).

## Unreleased

### New features

- Attach more than pictures to an artifact. A figure can now be a **PDF** —
  a supplier datasheet, a standard, a test report — or a **CAD file**: STEP,
  IGES, STL, 3MF, OBJ, PLY, glTF, DXF, DWG and the common native part
  formats. Clicking one opens it: a PDF in a reader, an STL as a 3D preview
  you can drag to turn, and every other format in a panel naming it with a
  Download button. Figures keep one numbering sequence whatever the format,
  so `REQ-17-FIG-2` may be a drawing today and the STEP model tomorrow.
- Cite a figure on **any** artifact in the project by typing `##` in a
  description. A single `#` still offers this artifact's own figures and the
  artifacts it is linked to; `##` offers every figure in the project, naming
  the artifact each belongs to, and writes the citation as
  `##REQ-99-FIG-2` so a reader can see it reaches outside what they are
  reading.

### Maintenance updates

- OpenV's own source code is now scanned by CodeQL on every change, on every
  release, and weekly against an updated set of rules. It looks for bugs we
  wrote — injection, path traversal and similar — which is the half that the
  existing dependency scanning cannot see.

### Bug fixes

- Clicking a figure or artifact citation in a description works again. Every
  citation had been rendering as a link with no destination, so following one
  opened a new tab showing the page you were already on instead of the figure
  or artifact it named.

## 0.8.2 — 2026-09-15

### Maintenance updates

- Every release from now on is tagged in the source repository as
  `v<version>`, so the exact code behind a version number can be found,
  compared against another release, or checked out. That matters when you
  are self-hosting, and when pinning down which release a problem started
  in. Releases up to 0.8.1 predate this and are untagged.

## 0.8.1 — 2026-09-15

### Bug fixes

- 0.8.0 could not start against an existing database, taking the service
  down until this release. Nothing was lost and no data was touched — the
  server refused to start rather than run a schema change it could not
  complete — but OpenV was unreachable in the meantime.

## 0.8.0 — 2026-09-15

### New features

- **To-dos**, under *Plan*, lists the project's work under the person it
  belongs to rather than the column it sits in — everyone with access gets a
  section, including those who owe nothing, so the page answers who owes
  what. It is the board's work rearranged, not a second list: move a card
  and this page says so. *Just mine* narrows it to you, and finished work is
  hidden until you ask for it.
- **Raise a to-do from a note.** A note that mentions someone with @name now
  offers *Add to-do*, which fills in the person it named and the note's
  first line, links the artifact and carries the whole note across as the
  description. Afterwards the note shows a small link to the to-do with its
  current status, so an old thread tells you whether the thing being
  discussed ever got done. Mentioning someone still just tells them: nothing
  is added to the board unless you ask for it.
- **Restore an older version of a figure.** Its history now offers *Restore*
  on any earlier version: the figure goes back to the image and the name it
  had then. Like restoring an artifact, it is recorded as a new version and
  nothing is deleted — the superseded drawing stays in the history and stays
  openable, which is what you need when a requirement was reviewed against
  what the figure used to show. The history entry says which version it
  brought back.

### Bug fixes

- Antigravity agents no longer stop five minutes in. The CLI applies its own
  five-minute cap to a single-prompt run and ends it there regardless of the
  timeout set on the agent — which defaults to thirty minutes — so a longer
  piece of work was cut off and whatever had been produced so far was
  reported as the answer. The agent's own timeout is now the only one that
  applies.

### Maintenance updates

- Cloud runners now build with a fixed, checksum-verified copy of the
  Antigravity CLI rather than fetching whichever version is current at build
  time. Two rebuilds of the same commit now give runners the same CLI, and a
  version change is a visible edit to this repository instead of something
  that happens quietly between builds.

## 0.7.1 — 2026-09-15

### Maintenance updates

- The **?** help button now sits beside the notification bell instead of
  floating over the bottom-right corner of the page. It used to cover
  whatever the page put in that corner — on the V&V dashboard, the *Complete*
  and *Abort* buttons of the last test run — and the first attempt at fixing
  that reserved the corner instead, which left an empty strip along the
  bottom of every page. Neither now: it is in the bar with the other controls
  that are about you rather than the page, which is where phones have had it
  all along.

## 0.7.0 — 2026-09-15

### New features

- **Antigravity CLI** is available as an agent provider. Google moved the
  free, Google One, AI Pro and AI Ultra tiers off the Gemini CLI and onto
  Antigravity on 18 June 2026, so agents can now run on it. It runs from a
  Gemini API key set on the workspace rather than a personal sign-in, because
  the CLI keeps its sign-in in the computer's keyring and a cloud runner has
  none. Agents that edit a connected repository still need Claude Code.

### Maintenance updates

- The floating **?** help button no longer sits on top of the buttons in the
  bottom-right corner of a page. On the V&V dashboard it covered the
  *Complete* and *Abort* buttons of the last test run, which could not be
  clicked at all once the table reached the bottom of the window.
- A failed **Gemini CLI** sign-in now explains the likeliest reason: since
  18 June 2026 the Gemini CLI signs in only Google accounts on a Gemini Code
  Assist Standard or Enterprise licence, and other tiers need an API key
  instead. The message used to be the CLI's raw output with no hint that no
  amount of retrying would help.

## 0.6.3 — 2026-09-14

### Bug fixes

- Signing the Gemini CLI in from a cloud runner works again. The CLI will
  only complete a sign-in from a session it considers interactive, and it
  counted the runner's as automated on three separate grounds — so it stopped
  with "Manual authorization is required but the current session is
  non-interactive" before showing the link. It now gets a real terminal and
  the sign-in link appears as it should. It also no longer refuses the
  runner's workspace as untrusted.

## 0.6.2 — 2026-09-14

### Maintenance updates

- The **Cloud runner** card now shows how busy the shared pool is as a
  traffic light — *Runners available*, *Runners busy*, *All runners in use* —
  instead of printing how many runners are free. You can hold one runner at a
  time, so the count was never something you could act on, and the deployment's
  capacity is no longer published to every account.

## 0.6.1 — 2026-09-14

### Bug fixes

- Signing the Gemini CLI in from a runner works again, on a phone or a
  desktop. The CLI now refuses to start unless it is told which kind of
  Google account to use, so the sign-in failed before it could show you a
  link — and headless Gemini runs failed the same way. OpenV names the mode
  for it. Workspaces that run Gemini on an API key or on Vertex AI keep the
  account they configured.

## 0.6.0 — 2026-09-14

### New features

- Choose the workspace OpenV opens in when you sign in. Personal settings →
  *Default workspace* lists your personal workspace and every company
  workspace you belong to; pick the one you work in and each sign-in starts
  there instead of in your personal space. Switching workspaces during a
  session is unchanged. Stable-channel workspaces get this with their next
  stable release.
- Figures can be renamed. A screenshot arrives called something like
  `Screenshot 2026-09-14 at 09.12.33.png`; use ✎ on the figure while editing
  to give it a name that says what it shows. The name appears under the
  figure and in PDF and Word documents, and every rename is a figure
  version, with who changed it and when, alongside the image history.
- Forgot your password? The sign-in screen can now email you a link to set
  a new one: it works once and lasts an hour, and setting the password signs
  the account out everywhere. On a server that sends no mail, or when the
  email does not arrive, a platform admin can make a reset link for your
  account from the Platform admin page and pass it to you.

### Bug fixes

- A duplicated or pasted artifact now says where it came from. Its history
  starts with one note, *Copied from REQ-12 (version 3)*, instead of looking
  like an artifact that was typed in from scratch.

- Uploading a new version of a figure works again. Pressing ⬆ (or the
  history, rename or delete buttons) on a figure used to save the artifact
  and leave the editor before the file was chosen, so the image never
  changed; the buttons no longer submit the editing form.
- PDF and Word downloads no longer print each requirement's description
  twice. It sits once, in the Description row of the fields table, and
  keeps its formatting there: lists, tables, code, links and emphasis all
  survive, and a description longer than a page carries its row across
  the break.

## 0.5.1 — 2026-09-13

### Maintenance updates

- OpenV's licence is now the Elastic License 2.0. Self-hosting stays free
  for anyone at any scale, the source stays public, and paid installation
  or support stays allowed; what is no longer allowed is offering OpenV to
  others as a hosted or managed service. The README, the site's licence
  section and the manual carry a plain-English summary.

## 0.5.0 — 2026-09-13

### New features

- The V&V Assistant can now change the project, not just add to it — from
  the notes panel and from the guided definition wizard alike. Ask it to
  add a test case, a design item, a heading or any other kind of artifact
  and it offers a card that files it where you said; ask it to rewrite or
  restructure and it offers *Apply change* and *Move* cards that edit an
  artifact's text or move it under another heading. It sees the whole
  project's outline while you talk and can read any artifact in full, so
  when you resume a definition it can work on what is already there, name
  things by their reference, and tell a locked wizard entry from a new
  one. Stable-channel workspaces get this with their next stable release.

## 0.4.2 — 2026-09-13

### Maintenance updates

- Platform admins have a page of their own, under the account menu: every
  workspace with its plan, changeable in place (which is how a workspace
  is granted the open-source tier), and every account, where platform-admin
  standing is granted or removed. Until now only the first account ever
  registered could be a platform admin.

## 0.4.1 — 2026-09-13

### Maintenance updates

- A platform admin can move a workspace to another plan through the API,
  which is how a workspace is granted the open-source tier; before, that
  took a hand edit of the database.

## 0.4.0 — 2026-09-13

### New features

- Share a project from one link. Under Project settings → Access, an owner
  mints a *public* link that opens the live project read only for anyone,
  no account needed, or a *reviewer* link that asks the holder to sign in
  and makes them a reviewer. Links can carry a label and an expiry, and can
  be revoked. Pasted into Slack, Discord, LinkedIn and the like, a link
  shows a preview card naming the project.
- A new project role, *reviewer*: reads everything, adds notes, comments
  and mentions, and changes nothing. It can be granted by name or team like
  the other roles.
- Workspaces on the open-source tier have every project's latest baseline
  published on the site's open-source page, where anyone can read it; live
  work stays private until the next baseline.

### Maintenance updates

- PDF and Word downloads put each requirement's description in its table,
  under the reference and type, so it no longer gets lost between figures.
- The site has grown from one page into a small storefront: how OpenV works
  with diagrams, five narrated demo videos (two of them on a phone), a
  security and subscription FAQ, an open-source projects page, and places
  for customer stories and white papers.

## 0.3.0 — 2026-09-13

### New features

- A project can name the project it refines, so a subsystem or a supplier
  works in its own project under the system it belongs to. Set it under
  Project settings → General.
- A requirement can *refine* a requirement of the parent project. The link
  is offered when editing a requirement, is seen from both projects, and the
  parent's V&V shows each refined requirement's own result and rolls the
  worst one up, so verification flows back up the tree.
- Every artifact can have an owner: a member, or one of the reference
  parties a project lists in its settings (your workspace is always one).
  The owner shows on the artifact, and the module view, the API and the
  agent tools can filter by it.
- A download can be narrowed to one or more owners, in every format, so a
  supplier's share of a project can be handed over on its own.

## 0.2.0 — 2026-09-13

### New features

- Workspace settings show the workspace's release channel: Business and
  Enterprise workspaces are on the monthly stable channel and their admins
  can switch to nightly; Personal and Lite workspaces run nightly.
- Stable-channel workspaces receive new features at a monthly stable
  release instead of with every nightly; maintenance updates and bug fixes
  still arrive with each nightly. Workspace admins choose the day and hour
  the monthly release turns on, up to 14 days after it is designated, and
  are told when it is designated and the day before it turns on.
- Any member of a Business or Enterprise workspace can try the next stable
  release early for their own account from workspace settings.
- What's new opens with your workspace's own channel: the stable release it
  runs and the one scheduled next, or the nightly it runs; stable releases
  are marked in the history.
- Release notifications follow the channel: nightly-channel members hear
  about each release, stable-channel members when their monthly release
  turns on.
- Dedicated OpenV instances are warned 30 and 7 days before their support
  window closes after a newer stable release, and again once it has.

## 0.1.0 — 2026-09-13

### New features

- OpenV releases are numbered now. What's new names the version you were
  upgraded to and keeps new features, maintenance updates and bug fixes
  apart instead of running them together in one list, and the notification
  that announces a release is grouped the same way.

### Bug fixes

- What's new shows the releases and nothing else. It was also showing the
  notes file's instructions to contributors, and changes that had not
  shipped yet.

## 2026-09-12.3

- The V&V Assistant's answers now read as they were written — lists as lists,
  emphasis as emphasis — instead of showing the raw markup around them. The
  same goes for the assistant in an interview.
- Add what the assistant suggests from wherever you are talking to it. A
  persona, need, requirement, NFR or hazard it proposes can be added to the
  project straight from the notes panel, filed under the usual heading and
  left as a draft for you to review; before, only the guided definition
  wizard could take them.

## 2026-09-12.2

- Get a notification whenever OpenV is updated, with a summary of what
  changed, and read the full history under What's new in the account menu.
- Open tabs learn about an update while they are open and offer a reload.

## 2026-09-12

- Upload your own profile picture from Personal settings. It replaces the
  picture your sign-in provider supplied and stays put across sign-ins.
- Member avatars keep their round shape beside long names on a phone.
- PDF and Word downloads are rebuilt on a document model: templates, field
  selection, evidence appendices and the workspace logo on the cover page.
- Baselines record who captured them, and large baselines are stored
  compressed.
- Workspace admins are notified when somebody joins, leaves or changes role,
  and members are told when their own access changes.
- Workspace limits are shown in workspace settings.
