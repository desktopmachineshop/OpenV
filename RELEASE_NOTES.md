# OpenV release notes

What changed for the people who use OpenV, one release at a time. This file
is customer-facing: write for a member of a workspace, not for a developer
(what they can now do, what looks different, what they no longer have to do),
and keep the internals for the pull request.

Every pull request adds at least one bullet under **Unreleased** (CI refuses
one that does not, unless it carries the `no-release-notes` label). When
master is promoted to `release`, the Unreleased bullets become a dated
section, the API serves it at `GET /api/v1/release`, and every account is
notified that the platform has been updated.

Section headings are the release version: the promotion date, with `.2`,
`.3` … appended when there is more than one release in a day, and once a
month a stable release `YYYY.MM` (a fix release is `YYYY.MM.1`) whose section
opens with "Cut on <date> from <nightly>." and merges the nightlies since
the previous stable. A bullet that starts with `fix:` is a fix and reaches
every workspace at the next nightly; every other bullet is a change that
stable-channel workspaces wait for. See `docs/release-policy.md`.

## Unreleased

- Workspace settings show the workspace's release channel: Business and
  Enterprise workspaces are on the monthly stable channel and their admins
  can switch to nightly; Personal and Lite workspaces run nightly.
- Stable-channel workspaces receive new features at a monthly stable
  release instead of with every nightly; fixes still arrive with each
  nightly. Workspace admins choose the day and hour the monthly release
  turns on, up to 14 days after it is cut, and are told when it is cut and
  the day before it turns on.
- Any member of a Business or Enterprise workspace can try the next stable
  release early for their own account from workspace settings.
- What's new shows your workspace's own channel first: the stable release
  it runs and the one scheduled next, or the nightly it runs.
- Release notifications follow the channel: nightly-channel members hear
  about each nightly, stable-channel members when their monthly release
  turns on.
- Dedicated OpenV instances are warned 30 and 7 days before their support
  window closes after a newer stable release, and again once it has.

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
