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
`.3` … appended when there is more than one release in a day.

## Unreleased

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
