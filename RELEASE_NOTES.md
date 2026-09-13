# OpenV release notes

What changed for the people who use OpenV, newest release first. Each
release is a version number and three groups: what you can now do, what
quietly improved, and what stopped being broken.

Contributors: how to add to this file, and what the promotion does with it,
is in [CONTRIBUTING.md](CONTRIBUTING.md#release-notes).

## Unreleased

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
