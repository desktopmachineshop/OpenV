# Release policy

Approved 2026-09-12 (OpenV Platform project: NEED-15, DSC-7, REQ-135 to
REQ-143). This page is the policy and how far it is implemented; the
mechanics of a promotion are in `docs/railway.md`, "Release pipeline".

## Principle

The shared service runs **one build** for every workspace. What differs
between workspaces is *when* user-visible changes turn on for them, not
which code serves them. Pinning a build is an enterprise product: a
dedicated instance (self-hosted or single-tenant hosted) upgraded on the
customer's own date.

## Channels

| Channel | Cadence | Version | Who |
|---|---|---|---|
| **Nightly** | Every promotion of a green master that carries new release notes. Automated at 03:00 UTC once staging exists; manual runs at any time. | `YYYY-MM-DD`, then `.2`, `.3` for more than one in a day | Personal and Lite always. Business and Enterprise may opt in. |
| **Stable** | Cut on the first working day of the month from a nightly that has served the nightly channel for at least 7 days without a fix release. Its notes are the nightly notes since the previous stable, merged and grouped as changes and fixes. | `YYYY.MM`; fix releases `YYYY.MM.1`, `.2` | Business and Enterprise by default. |
| **Dedicated** | The customer's instance on a stable release, upgraded on their chosen date within 90 days of the next stable, after a preview on their staging copy. | The stable release it runs | Enterprise that needs pinning. |

Calendar versioning throughout; there is no major version. The HTTP API,
the MCP tools, the export formats and the runner protocol stay backward
compatible from one stable release to the next, and a removal is announced
in the notes of two consecutive stable releases before it happens.

Security fixes go to every workspace at the next nightly and as a stable
fix release the same day. No channel or upgrade window holds them back.

## What a workspace controls

- **Channel.** Business and Enterprise admins choose nightly or stable in
  workspace settings. Personal and Lite are on nightly and cannot change it:
  that is the trade for the lower tier, newest features first and the risk
  of a nightly with them.
- **Upgrade window.** The day of the month and hour, in the workspace's
  time zone, at which each stable release turns on, up to 14 days after the
  cut; admins are notified at the cut and 24 hours before.
- **Preview.** Any member can turn the next stable on for their own account
  alone to try it.

## Staging

After the alpha, every merge to master deploys to a staging environment
with its own anonymised database. The nightly promotion is gated on that
commit's smoke tests and migrations passing there. Staging is also where an
enterprise previews its next stable and where the maintainer checks a
release by hand before a monthly cut.

## Implementation status

| Piece | Status |
|---|---|
| Dated nightly releases, notes cut at promotion, announced to every account (REQ-134) | Shipped 2026-09-12 |
| Workspace release channel by plan, admin-selectable on company plans, shown in settings (REQ-136) | Shipped |
| Feature gating by channel; `fix:` bullets (REQ-137) | Shipped: `internal/domain/release/features.go` registry, `GET /orgs/{id}/features`, `useFeature` |
| Per-channel notifications and What's new (REQ-140) | Shipped: nightly members at each nightly, stable members when their release turns on; What's new shows the workspace's channel first |
| Stable cut in the pipeline, `YYYY.MM` versions (REQ-135) | Shipped: `scripts/release_notes.py cut-stable` and the *Cut stable release* workflow (first working day of the month, or `fix: true` by hand) |
| Upgrade window and personal preview (REQ-138) | Shipped: window in workspace settings, admin notices at the cut and a day before, per-member preview |
| Dedicated instance support window (REQ-139) | Shipped: `OPENV_DEPLOYMENT=dedicated` polls `GET /api/v1/public/release` and warns admins at 30 and 7 days and on close |
| Staging environment and nightly automation (REQ-141, REQ-135) | Workflow in place (*Nightly promotion*, 03:00 UTC), a no-op until the `STAGING_BASE_URL` repository variable names a staging environment (after the alpha; see `docs/railway.md`, "Staging") |
| Compatibility and deprecation rule (REQ-143) | Enforced: `internal/api/testdata/routes.txt` pins the HTTP surface; a route cannot be removed without regenerating it on purpose |
