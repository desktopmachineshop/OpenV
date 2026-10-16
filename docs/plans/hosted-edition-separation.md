# Separating the hosted service from the open-source product

Status: proposed 2026-10-16. Not started. Decisions in section 3 are open
for the maintainer.

## 1. Why

OpenV is MIT-licensed. Most people who run it will run it for themselves,
and openv.app is one deployment among many — the one the maintainer
charges for. Today the code does not reflect that:

- **The default is the hosted service.** `OPENV_SELF_HOSTED` is false
  unless set (`internal/config/boot.go`), and neither `docker-compose.yml`
  nor `docker-compose.prod.yml` sets it. A plain `docker compose up` gets
  hosted plans (`single`), "upgrade" wording in limit refusals, a Billing
  tab that says paid plans are "not on sale on this instance yet", and the
  openv.app marketing site at `/`.
- **Hosted-only code is mixed into core packages.** Plan tiers,
  grandfathering, the open-source plan, release channels and visitor
  analytics live in `internal/domain/orgs`, `internal/api`,
  `internal/domain/release` and the frontend shell, next to the parts a
  self-hoster does need.
- **A latent self-host bug.** `Org.ResolveReleaseChannel`
  (`internal/domain/orgs/channels.go:156`) reads `BilledPlan`, not the
  self-host plan, so a self-hosted workspace whose row still carries
  `business`, `team` or `enterprise` sits on the stable channel and does
  not see features its own server already ships.

The goal is a product a self-hoster can run with no hosted-service
concepts in sight, and a hosted layer the maintainer can change without
touching core — in one repository, under one licence.

### Non-goals

- Hiding anything. Everything stays MIT and public; this is about
  boundaries and defaults, not secrecy.
- A second product. One codebase, one image per service, one test suite.
- Removing data. Migrations are frozen (`migration_freeze_*_test.go`);
  hosted-only columns and tables stay in the schema, inert on a
  self-hosted install.

## 2. What is hosted-only, what is core

Sorted from the code as it stands (file paths are where each lives today).

### Hosted-only (the "hosted edition")

| Concern | Backend | Frontend | Ops / docs |
|---|---|---|---|
| **Stripe billing**: catalogue, checkout, plan change, portal, seat sync, reconcile loop | `internal/billing/**`, `internal/api/billing_handlers.go`, `api/errmap.go` billing sentinels, `api/ratelimit.go` billing buckets, `metrics.go` billing collectors, `cmd/server/wire_http.go` billing stage | `components/org/OrgBillingTab.tsx`, `api/billing.ts`, `api/types/billing.ts`, the Billing tab and Plan card in `views/OrgSettings.tsx` | `docs/plans/billing-stripe.md`, `docs/billing-odoo.md`, `STRIPE_*`, `OPENV_STRIPE_*`, `OPENV_BILLING_*` |
| **Plan tiers**: single, business_lite, business, enterprise; tier defaults; alpha terms; grandfathering; trial | `orgs/plans.go` (tiered defaults), `orgs/entitlements.go`, `orgs/billing.go`, `postgres/org_repository_billing.go`, `users.BillingTrialUsedAt`, `wire_workspace.go` grandfather step | `PLANS` in `api/platform.ts`, `PLAN_LABELS`, plan selector in `PlatformAdmin.tsx`, "upgrade" copy in `OrgLimitsTab.tsx` | `OPENV_BILLING_GRANDFATHER_BEFORE` |
| **Open-source plan and showcase** | `api/share_handlers.go:46-49,428,522` (`PlanOpenSource` filter) | `site/OpenSourceProjects.tsx`, `/open-source/:id` source of `SharedProjectView.tsx` | `.github/ISSUE_TEMPLATE/free-hosting.md` |
| **Per-workspace release channels**: stable vs nightly for one shared build, upgrade windows, stable scheduler, dedicated-instance support window, release feed | `orgs/channels.go`, the channel half of `release/features.go`, `notify/stable.go`, `notify/dedicated.go`, `wire_notify.go` release stage | channel select and upgrade window in `OrgSettings.tsx` | `docs/release-policy.md`, `cut-stable.yml`, `OPENV_DEPLOYMENT`, `OPENV_RELEASE_FEED_URL` |
| **Visitor analytics** | `usermetrics/visitors.go`, `admin_visitor_handlers.go`, `/public/visits` | `site/visitBeacon.ts` (fires from `App.tsx` on every load), `components/admin/VisitorDashboard.tsx` | — |
| **Marketing site**: landing, pricing, how it works, FAQ, topics, customers, demos, white papers, prerender, crawl files | — | `views/Landing.tsx`, `landing/**`, `site/**`, `vite.config.ts` `crawlFiles()`/`prerenderPages()`, `nginx.conf` `location = /` | `46-openv-crawl.sh`, `45-openv-noindex.sh`, `OPENV_SITE_URL`, `OPENV_NOINDEX` |
| **openv.app operations**: Railway, staging, promotion | — | `50-openv-app-identity.sh`, `*-staging` icons and manifest | `frontend/railway.json`, `docs/railway.md`, `promote-release.yml`, `nightly-promote.yml`, `staging-smoke.yml`, `RAILWAY_GIT_COMMIT_SHA`, `OPENV_APP_VARIANT` |

### Core (every install)

Kept as they are, because a self-hoster uses them:

- **Operator limits**: `OPENV_LIMITS`, `OPENV_MAX_*`, the `LimitEnforcer`
  mechanism and `OrgLimitsTab` (with self-host wording). The *mechanism*
  is core; only the *plan tiers* that feed it are hosted.
- **Budgets** (`OPENV_BUDGET_ENFORCE`, budget alerts).
- **Hosted runners on the local Docker daemon** (`internal/hosting`,
  `HOSTED_RUNNERS`): this is the self-host runner path.
- **Share links**, **What's new** and release notes, the
  `release_published` announcement, the running version.
- **Platform admin**: granting admin, minting a password-reset link (a
  self-hoster without SMTP needs it), the user activity dashboard.
- SMTP, push (VAPID), OIDC/Google sign-in, Prometheus `/metrics`, the
  provider allow-list (`OPENV_AGENT_PROVIDERS`), embeddings.

### Judgement calls (section 3)

- **Cloud runner pool** (`runnersessions`, `RUNNER_POOL_KEY`): built for
  openv.app on Railway, which has no Docker, but nothing in it is
  openv.app-specific. A self-hoster on a platform without Docker could use
  it.
- **Community product pool** (`sharedproducts`, `OPENV_SHARED_PRODUCT_*`):
  a cross-workspace voting pool that makes most sense on a shared service.
- **Feature gates** (`release/features.go`): the gate mechanism is how a
  feature ships dark and is useful anywhere; the *stable channel* rule
  behind it is hosted-only.

## 3. Decisions for the maintainer

1. **Flip the default to self-hosted.** Recommended. A new
   `OPENV_EDITION` (`self` | `hosted`) defaults to `self`; openv.app sets
   `hosted`. `OPENV_SELF_HOSTED` stays as a deprecated alias for one
   release (`true` → `self`, `false` → `hosted`, with a boot warning).
   The alternative is to keep the hosted default and document
   `OPENV_SELF_HOSTED=true` harder. That leaves every new self-hoster one
   forgotten variable away from a pricing page.
2. **Switch at run time or compile out?** Recommended: run time, one image.
   A Go build tag (`-tags hosted`) would drop Stripe and the rest from the
   self-host binary, but it doubles the build and test matrix and makes CI
   prove two binaries. Run time keeps one image and one route set to test.
   We can add the tag later behind the same boundary if binary size or
   audit scope ever matters.
3. **Where the hosted code lives.** Recommended: an in-tree
   `internal/hosted/` (backend) and `frontend/src/hosted/` (frontend),
   MIT like everything else, with an architecture rule that core never
   imports them. The `ee/` paragraph in `CONTRIBUTING.md` (separately
   licensed enterprise code) no longer fits an all-MIT repo; replace it
   with a pointer to this boundary. A separate private repository would
   only make sense for code you want to keep closed, which is not the
   goal.
4. **Cloud runner pool and community pool.** Recommended: keep the cloud
   runner pool in core (opt-in by `RUNNER_POOL_KEY`, as now), and move the
   community product pool to the hosted edition.
5. **Does the marketing site stay in this repository?** Recommended: yes
   for now, under `frontend/src/hosted/site/`, because it shares the
   manual, components and prerender build with the app. Splitting it into
   its own repo and deploy is a later option once it is isolated.

The phases below assume the recommended answers.

## 4. Target shape

```
OPENV_EDITION=self (default)          OPENV_EDITION=hosted (openv.app)
───────────────────────────           ─────────────────────────────────
core only                             core + hosted modules
"/"  → sign-in                        "/"  → marketing site
no Billing tab, no plans              Billing tab, plan tiers, Stripe
operator limits only                  plan tiers feed the same limits
every gated feature on for the        stable / nightly channels per
version the server runs               workspace, stable scheduler
no visitor beacon                     visitor analytics
```

**Backend.** Core defines small interfaces where it needs hosted behaviour,
and the hosted package provides the implementations. Core ships a no-op or
operator-limits default for each:

| Core seam (interface in core) | Self edition (core default) | Hosted edition |
|---|---|---|
| `orgs.PlanPolicy`: plan a new workspace gets, limits per plan, whether tiers apply | `self_host` plan, operator limits | tiers, alpha terms, grandfathering |
| `orgs.ChannelPolicy`: a workspace's release channel | always the running version | stable/nightly per plan, upgrade windows |
| `api.SeatObserver`: told on member add/remove and workspace delete/restore | no-op | Stripe seat sync |
| `api.Module`: registers extra routes and jobs | none | billing, showcase, visitors, community pool |

`cmd/server` composes the hosted modules in its existing stages (no new
stage; `cmd/server/CLAUDE.md`) only when the edition is `hosted`.
`api.HandlerDeps` loses its concrete `*billing.Service` and other hosted
types in favour of these interfaces.

**Routes.** Today every route is always registered, and hosted ones answer
404 when off; `internal/api/testdata/routes.txt` pins the full set. Under
modules, hosted routes are registered only in the hosted edition, and the
route golden splits into `routes.txt` (core) and `routes_hosted.txt`.

**Architecture rule.** A new archtest rule, K7-style: no package outside
`internal/hosted/**` and `cmd/server` imports `internal/hosted/**`, and
`internal/hosted/**` imports only core domain packages and `envparse`.
The `import_edges` ratchet records the removed edges
(`internal/api → internal/billing`, `internal/config → internal/billing`)
so they cannot return.

**Frontend.** `GET /api/v1/auth/config`, which `App.tsx` already fetches
before routing, gains `edition`. Routes, tabs and menu entries owned by
`frontend/src/hosted/**` are registered from one list the hosted
directory contributes (`pages.ts`, `PUBLIC_SEGMENTS`, `PRERENDER_ROUTES`)
and appear only when `edition === 'hosted'`. A frontend arch test enforces
that only the shell's registration point imports `src/hosted/**`.

**Frontend container.** `46-openv-crawl.sh` and `nginx.conf` publish the
prerendered pages and crawl files only when `OPENV_SITE_URL` is set, so a
self-hosted `/` serves the app, not the landing snapshot.

## 5. Phases

Each phase is one or a few pull requests, keeps `make check` green and
ships a release-notes bullet unless noted. Moves that change nothing
observable go as `refactor` PRs under the pure-move rules
(`docs/plans/codebase-refactor.md`, the `pure-move-refactor` skill).

### Phase 0: fix what self-hosters hit today (small, ship first)

- `ResolveReleaseChannel` uses the effective plan (`self_host` on a
  self-hosted install), so self-hosted workspaces are never on stable.
  Test: a self-hosted org whose `BilledPlan` is `business` resolves to
  nightly and every feature in the running version is on.
  *Bug fixes.*
- Set `OPENV_SELF_HOSTED=true` in `docker-compose.yml` and
  `docker-compose.prod.yml`, and say so in `docs/QUICKSTART.md`, so
  today's documented install is self-hosted before the default flips.
  *Maintenance updates.*

### Phase 1: one edition switch

- `OPENV_EDITION` in `internal/config` (allow-list, `docs/env-vars.md`,
  `TestEnvInventory`). `OPENV_SELF_HOSTED` becomes an alias with a
  deprecation warning. `DeploymentPolicy` holds the edition.
- **Railway first:** set `OPENV_EDITION=hosted` on openv.app's API service
  (and staging) *before* the release that flips the default. The server
  logs the edition at boot. When `STRIPE_SECRET_KEY` is set but the edition
  is `self`, it warns loudly but does not refuse to start (today's
  behaviour, kept).
- Flip the default to `self`. Release note: self-hosted installs no longer
  need `OPENV_SELF_HOSTED`. *Maintenance updates.*
- `/api/v1/auth/config` returns `edition`.

### Phase 2: the frontend follows the edition

- Self edition: `/` goes to sign-in for visitors with no session. The
  marketing routes, `/pricing`, the Billing tab, the Plan card, the plan
  selector in Platform admin, the Visitors dashboard and the visit beacon
  are absent. The release channel controls are hidden.
- `OrgLimitsTab` and limit refusals use operator wording only.
- Container: prerender and crawl publishing keyed on `OPENV_SITE_URL`.
- Manual: hosted-only paragraphs (plans and pricing in `orgSettings.ts`,
  `troubleshooting.ts`, `platformAdmin.ts`; cloud runner minutes in
  `runsRunners.ts`) move to a "Hosted service" chapter shown only on the
  hosted edition, and each core chapter reads correctly without it.
- Goldens to regenerate: `bundle-shape.json`, `arch/__snapshots__/*`,
  `sizeBudget` entries, `e2e/tests/landing.spec.ts` (runs against the
  hosted edition).
- *Maintenance updates* (what a self-hoster notices: no pricing or billing
  screens, `/` opens sign-in).

### Phase 3: move the backend behind the boundary

Pure moves first, then the seams, one concern per PR:

1. `internal/billing/**` → `internal/hosted/billing/**`; the billing
   handlers → `internal/hosted/billing/http.go`, registered as a module.
   `HandlerDeps` takes a `SeatObserver`.
2. Plan tiers, entitlements and grandfathering out of `orgs` into
   `internal/hosted/plans`, behind `orgs.PlanPolicy`. `LimitEnforcer`
   stays in `orgs`, reading limits from the policy.
3. Channel policy behind `orgs.ChannelPolicy`; the stable scheduler and
   dedicated support window run only in the hosted edition. Feature gates
   stay; on the self edition every key at or below the running version is
   on.
4. Showcase, visitor analytics and the community pool become hosted
   modules.
5. The archtest rule and the split route goldens land with the last move.

Each step records its design item and updates the requirements it touches
in the OpenV Platform project (`CLAUDE.md`).

### Phase 4: docs and operations

- `docs/hosted/` collects `railway.md`, `release-policy.md`, the billing
  plans, and the search and AI visibility plan and assessment. README and
  `docs/operations.md` lead with self-hosting; openv.app becomes one line.
- `docs/env-vars.md` groups variables as core, optional, and hosted
  edition only.
- `CLAUDE.md` keeps contributor rules at the top and moves the openv.app
  release rules (Railway, promotion, stable cut) under a "Maintainer:
  hosted service" heading. `CONTRIBUTING.md` replaces the `ee/`
  paragraph.
- The release workflows stay where they are; `scripts/README.md` already
  labels them as the maintainer's pipeline.

## 6. Risks

- **openv.app flips to the self edition by accident** if the default
  changes before `OPENV_EDITION=hosted` is set on Railway. That would mean
  no landing page, no billing and no plan tiers. Mitigation: set the
  variable on staging and production in Phase 1 before the flip ships; the
  promotion checklist checks the boot log line on staging.
- **Golden churn.** Route, bundle and arch snapshots move in Phases 2 and
  3. Each PR regenerates them with the repo's tools and explains the diff.
- **The codebase refactor in flight** (`docs/plans/codebase-refactor.md`)
  touches the same handler and wiring files. Sequence Phase 3 after the
  refactor steps on `internal/api` and `cmd/server`, or fold it into them.
- **Existing self-hosted rows.** Workspaces created before Phase 1 on a
  self-hosted install may carry hosted plan names. The self edition
  ignores `BilledPlan` everywhere (Phase 0 fixes the one place it doesn't
  today), so nothing needs migrating.

## 7. Effort

| Phase | Size | Depends on |
|---|---|---|
| 0 | 1 small PR | — |
| 1 | 1 PR + a Railway variable | 0 |
| 2 | 2–3 PRs | 1 |
| 3 | 5–6 PRs, mostly pure moves | 1; refactor plan sequencing |
| 4 | 1–2 docs PRs | 2 (can run alongside 3) |

Phases 0–2 deliver what a self-hoster sees. Phase 3 is the internal clean-up
that keeps it that way.
