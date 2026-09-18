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
| **Nightly** | Every promotion of a green master that carries new release notes. Automated at 03:00 UTC once staging exists; manual runs at any time. | Semantic: `0.2.0`, `0.2.1`, derived from the notes (a new feature is a minor bump, maintenance and fixes a patch, a major is asked for) | Personal and Lite always. Business and Enterprise may opt in. |
| **Stable** | On the first working day of the month, the newest release that has served the nightly channel for at least 7 days is designated the stable release. Not a separate build or version: a marker line under that release's heading. Its notes for stable-channel members are every release since the previous stable, merged group by group. | The release's own version | Business and Enterprise by default. |
| **Dedicated** | The customer's instance on a stable release, upgraded on their chosen date within 90 days of the next stable, after a preview on their staging copy. | The stable release it runs | Enterprise that needs pinning. |

Semantic versioning throughout, with the version derived from the release
notes. The HTTP API, the MCP tools, the export formats and the runner
protocol stay backward compatible from one stable release to the next, and
a removal is announced in the notes of two consecutive stable releases
before it happens.

Security fixes go to every workspace at the next nightly and, by a manual
run of the stable workflow with `fix: true`, become the stable release the
same day. No channel or upgrade window holds them back.

## What a workspace controls

- **Channel.** Business and Enterprise admins choose nightly or stable in
  workspace settings. Personal and Lite are on nightly and cannot change it:
  that is the trade for the lower tier, newest features first and the risk
  of a nightly with them.
- **Upgrade window.** The day of the month and hour, in the workspace's
  time zone, at which each stable release turns on, up to 14 days after it
  is designated; admins are notified at the designation and 24 hours
  before.
- **Preview.** Any member can turn the next stable on for their own account
  alone to try it.

## Staging

Every merge to master deploys to a staging environment with its own
database, holding no production data. The smoke journey runs there against
that exact commit — both services report the commit they were built from, and
the run waits for them to match — so a green result is attributable to the
merge that caused it, and the API having booted at all means its migrations
applied.

Smoke on staging **reports**; it does not promote. Promotion to production
stays a human decision (see `docs/railway.md`): the nightly automation that
would promote a green master by itself is written and deliberately unarmed.
Gating promotion on staging is what arming it would add, and the two read
different repository variables so that one cannot turn on the other by
accident.

Staging is also where an enterprise previews its next stable and where the
maintainer checks a release by hand before a monthly cut — every master
commit already has a staging deployment, and Railway's deployment history
brings any of them back.

## Implementation status

| Piece | Status |
|---|---|
| Numbered releases, notes cut at promotion, announced to every account (REQ-134) | Shipped 2026-09-13 (`0.1.0`) |
| Workspace release channel by plan, admin-selectable on company plans, shown in settings (REQ-136) | Shipped |
| Feature gating by channel: *New features* wait for a stable release, maintenance and fixes do not (REQ-137) | Shipped: `internal/domain/release/features.go` registry, `GET /orgs/{id}/features`, `useFeature` |
| Per-channel notifications and What's new (REQ-140) | Shipped: nightly members at each release, stable members when their release turns on; What's new opens with the workspace's channel and marks stable releases |
| Stable designation in the pipeline (REQ-135) | Shipped: `scripts/release_notes.py cut-stable` and the *Cut stable release* workflow (first working day of the month, or `fix: true` by hand) |
| Upgrade window and personal preview (REQ-138) | Shipped: window in workspace settings, admin notices at the designation and a day before, per-member preview |
| Dedicated instance support window (REQ-139) | Shipped: `OPENV_DEPLOYMENT=dedicated` polls `GET /api/v1/public/release` and warns admins at 30 and 7 days and on close |
| Staging environment (REQ-141) | Repository side shipped: `/health` and `/build.json` name the running commit, and *Staging smoke* runs the journey on every merge once `STAGING_URL` names a staging environment. Needs the Railway environment built (see `docs/railway.md`, "Staging") |
| Nightly automation (REQ-141, REQ-135) | Workflow in place (*Nightly promotion*, 03:00 UTC) and deliberately unarmed: promotion stays a human decision, so the `STAGING_BASE_URL` repository variable that would gate and automate it is left unset |
| Compatibility and deprecation rule (REQ-143) | Enforced: `internal/api/testdata/routes.txt` pins the HTTP surface; a route cannot be removed without regenerating it on purpose |
