# Separating the hosted service from the open-source product

Status: decisions taken 2026-10-16 (section 3): one private repository,
one domain. Not started.

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

The goal: this repository is the open-source product, complete for anyone
who runs it, with no hosted-service concepts in sight. The hosted service
— billing, plans, the marketing site, openv.app operations — moves to
one private repository, `openv-hosted`, that builds on top of it.

### Non-goals

- A fork. The private repository consumes a released version of this
  one; they never carry patches to core. Anything core needs is
  contributed here, in the open.
- Removing data. Migrations are frozen (`migration_freeze_*_test.go`);
  hosted-only columns and tables already in the schema stay, inert on a
  self-hosted install.

## 2. What is hosted-only, what is core

Sorted from the code as it stands (file paths are where each lives today).

### Hosted-only: moves to the private repository

| Concern | Backend | Frontend | Ops / docs |
|---|---|---|---|
| **Stripe billing**: catalogue, checkout, plan change, portal, seat sync, reconcile loop | `internal/billing/**`, `internal/api/billing_handlers.go`, `api/errmap.go` billing sentinels, `api/ratelimit.go` billing buckets, `metrics.go` billing collectors, `cmd/server/wire_http.go` billing stage | `components/org/OrgBillingTab.tsx`, `api/billing.ts`, `api/types/billing.ts`, the Billing tab and Plan card in `views/OrgSettings.tsx` | `docs/plans/billing-stripe.md`, `docs/billing-odoo.md`, `STRIPE_*`, `OPENV_STRIPE_*`, `OPENV_BILLING_*` |
| **Plan tiers**: single, business_lite, business, enterprise; tier defaults; alpha terms; grandfathering; trial | `orgs/plans.go` (tiered defaults), `orgs/entitlements.go`, `orgs/billing.go`, `postgres/org_repository_billing.go`, `users.BillingTrialUsedAt`, `wire_workspace.go` grandfather step | `PLANS` in `api/platform.ts`, `PLAN_LABELS`, plan selector in `PlatformAdmin.tsx`, "upgrade" copy in `OrgLimitsTab.tsx` | `OPENV_BILLING_GRANDFATHER_BEFORE` |
| **Open-source plan and showcase** | `api/share_handlers.go:46-49,428,522` (`PlanOpenSource` filter) | `site/OpenSourceProjects.tsx`, `/open-source/:id` source of `SharedProjectView.tsx` | `.github/ISSUE_TEMPLATE/free-hosting.md` |
| **Community product pool**: cross-workspace shared products and voting | `domain/sharedproducts/**`, `api/shared_product_handlers.go`, `postgres/shared_product_*.go`, `seeds/shared_products*.go`, `seeds/product_concepts*.go` | `sharedProductsAPI` in `api/community.ts`, `components/SharedProductVotes.tsx`, the pool in `components/ProjectList.tsx` | `OPENV_SHARED_PRODUCT_*` |
| **Per-workspace release channels**: stable vs nightly for one shared build, upgrade windows, stable scheduler, dedicated-instance support window, release feed | `orgs/channels.go`, the channel half of `release/features.go`, `notify/stable.go`, `notify/dedicated.go`, `wire_notify.go` release stage | channel select and upgrade window in `OrgSettings.tsx` | `docs/release-policy.md`, `cut-stable.yml`, `OPENV_DEPLOYMENT`, `OPENV_RELEASE_FEED_URL` |
| **Visitor analytics** | `usermetrics/visitors.go`, `admin_visitor_handlers.go`, `/public/visits` | `site/visitBeacon.ts` (fires from `App.tsx` on every load), `components/admin/VisitorDashboard.tsx` | — |
| **Marketing site**: landing, pricing, how it works, FAQ, topics, customers, demos, white papers, prerender, crawl files | — | `views/Landing.tsx`, `landing/**`, `site/**`, `vite.config.ts` `crawlFiles()`/`prerenderPages()`, `nginx.conf` `location = /` | `46-openv-crawl.sh`, `45-openv-noindex.sh`, `OPENV_SITE_URL`, `OPENV_NOINDEX`, `docs/plans/search-and-ai-visibility.md`, `docs/assessments/2026-10-13-ai-visibility.md` |
| **openv.app operations**: Railway, staging, promotion | — | `50-openv-app-identity.sh`, `*-staging` icons and manifest | `frontend/railway.json`, `docs/railway.md`, `promote-release.yml`, `nightly-promote.yml`, `cut-stable.yml`, `staging-smoke.yml`, `RAILWAY_GIT_COMMIT_SHA`, `OPENV_APP_VARIANT` |

### Core: stays here, for every install

- **Operator limits**: `OPENV_LIMITS`, `OPENV_MAX_*`, the `LimitEnforcer`
  mechanism and `OrgLimitsTab` (with self-host wording). The *mechanism*
  is core; only the *plan tiers* that feed it are hosted.
- **Budgets** (`OPENV_BUDGET_ENFORCE`, budget alerts).
- **Runners**: hosted runners on the local Docker daemon
  (`internal/hosting`, `HOSTED_RUNNERS`), and the cloud runner pool
  (`runnersessions`, opt-in by `RUNNER_POOL_KEY`) for platforms without
  Docker.
- **Feature gates** (`release/features.go`): the mechanism that ships a
  feature dark. Without the hosted channel policy, every feature in the
  running version is on.
- **Share links**, **What's new** and release notes, the
  `release_published` announcement, the running version, versioned
  releases and tags.
- **Platform admin**: granting admin, minting a password-reset link (a
  self-hoster without SMTP needs it), the user activity dashboard.
- **The manual**, less its hosted-service paragraphs.
- SMTP, push (VAPID), OIDC/Google sign-in, Prometheus `/metrics`, the
  provider allow-list (`OPENV_AGENT_PROVIDERS`), embeddings.

## 3. Decisions (taken 2026-10-16)

1. **The default is self-hosted.** An install with nothing hosted plugged
   in is the open-source product: `self_host` plan, operator limits, `/`
   opens sign-in. `OPENV_SELF_HOSTED` becomes redundant and is retired
   after one release as a no-op alias.
2. **One image per product, configured at run time.** No build tags. The
   open repository publishes one image per service; the hosted repository
   builds openv.app's images from that source plus its modules. Inside
   each image, behaviour is set by configuration, as today.
3. **Hosted code lives in a separate private repository.** This replaces
   the `ee/` paragraph in `CONTRIBUTING.md`. Consequence: Go's `internal/`
   rule forbids another module from importing this repository's
   `internal/**`, so the open repository must offer a small **public
   extension API** (section 4). That is the main new work.
4. **The community product pool is hosted-only.** The cloud runner pool
   stays in core.
5. **One private repository for everything hosted.** The marketing site
   moves out of this repository into `openv-hosted`, alongside the hosted
   modules and the openv.app operations.
6. **One domain.** openv.app keeps serving the marketing site and the app
   from the same origin, as today. URLs, share links and search rankings
   do not change, and there is no `app.` subdomain.

## 4. Target shape

Two repositories:

```
desktopmachineshop/OpenV  (public, MIT)          the product
  ├─ core server, frontend, runner, connector, manual
  ├─ ext/            public Go extension API (new)
  └─ frontend/src/ext/  frontend extension registry (new)

openv-hosted  (private)                          everything openv.app adds
  ├─ go.mod: requires OpenV at a release tag
  ├─ billing/, plans/, channels/, showcase/, community/, visitors/
  ├─ cmd/server/: main = OpenV server + hosted modules
  ├─ frontend/: OpenV frontend + hosted extensions
  ├─ site/: landing, pricing, FAQ, topics, crawl files, prerender
  └─ Railway config, promotion and staging workflows, ops docs
```

openv.app keeps its current two Railway services (API and frontend) and
the runner pool. Each is built from `openv-hosted` instead of this
repository. The frontend image serves the marketing pages and the app
from one nginx, exactly as today.

### 4.1 The Go extension API (`ext/`)

The hosted repository can import only non-`internal` packages, so the open
repository grows one public package that exposes exactly what a module
needs and nothing more:

| Extension point | Core default (open image) | Hosted module |
|---|---|---|
| `ext.PlanPolicy`: plan for a new workspace, limits per plan, whether tiers apply, plan names for the UI | `self_host`, operator limits | tiers, alpha terms, grandfathering |
| `ext.ChannelPolicy`: a workspace's release channel | the running version | stable/nightly per plan, upgrade windows |
| `ext.WorkspaceObserver`: member added/removed, workspace deleted/restored | no-op | Stripe seat sync |
| `ext.Routes`: extra HTTP routes behind core auth middleware | none | billing, showcase, community pool, visitors |
| `ext.Jobs`: background loops on the server's lifecycle | none | reconcile, stable scheduler, support window |
| `ext.Migrations`: a module's own schema, tracked in its own table | none | hosted tables added from now on |
| `ext.Capabilities`: names the frontend reads to show extension UI | none | `billing`, `plans`, `release_channels`, … |

`ext` also exposes the read-only views a module needs (workspace, member,
authenticated user, an error-response helper). It does not expose
repositories or the `api` package. The server entry point becomes
`server.Run(ctx, cfg, ...ext.Module)`. The open `cmd/server` calls it
with no modules; the hosted `cmd/server` passes its own.

`ext` is a public API with semantic versioning. A breaking change bumps
OpenV's minor version while the product is 0.x, is called out in the
release notes, and is announced to the hosted repository by its pinned
version failing to build. That costs nothing extra, since it is the same
maintainer.

**Architecture rules** (archtest): `ext` imports only the domain types it
re-exports. Core never imports anything hosted (there is nothing to
import). The `import_edges` ratchet loses `internal/api → internal/billing`
and `internal/config → internal/billing` when billing leaves.

**Existing data.** Hosted columns already created by frozen core
migrations (0045, 0046, 0047, 0059, 0060 and the baseline `plan`/`limits`
columns) stay in the core schema. They are inert without the module, and
the module reads and writes them through `ext`. New hosted tables are
created by the module's own migrations, tracked in its own
`hosted_schema_migrations` table.

### 4.2 The frontend extension registry

The hosted frontend is built from OpenV's frontend source plus its own
`src/`, using a Vite alias that points at a checkout of the pinned OpenV
tag (a git submodule, or the published npm package if we choose to
publish one later). OpenV's frontend gains `src/ext/registry.ts`. It is
the one place that accepts:

- routes (`pages.ts` entries, public segments, prerender routes),
- Org Settings tabs and General-tab cards,
- Platform admin sections,
- user-menu entries,
- app-start hooks (the place `sendVisitBeacon` hangs from).

Each is shown only when `/api/v1/auth/config` lists the matching
capability. With nothing registered, the open build has no Billing tab,
plan card, plan selector, channel controls, Visitors dashboard or
beacon. `/` opens sign-in.

### 4.3 The marketing site

`openv-hosted/site/` takes `views/Landing.tsx`, `landing/**`, `site/**`,
the crawl and prerender build steps (the `crawlFiles()` and
`prerenderPages()` Vite plugins) and their entrypoint scripts
(`45-openv-noindex.sh`, `46-openv-crawl.sh`, `50-openv-app-identity.sh`).
The site registers its routes and public segments through the frontend
registry (4.2), and its pages are built into the same frontend image as
the app, so `/` on openv.app is the landing page and the app is unchanged
behind it. The sitemap still lists the manual's chapters, which the
hosted build reads from the pinned OpenV source.

The open frontend keeps `/manual` and a plain `/` → sign-in. Its nginx
config loses the `location = /` prerender rule and the crawl-file
locations; the hosted image adds them back.

## 5. Phases

Each phase is one or a few pull requests in this repository, keeps
`make check` green, and records its design items and requirement changes
in the OpenV Platform project (`CLAUDE.md`). Moves that change nothing
observable go as `refactor` PRs under the pure-move rules
(`docs/plans/codebase-refactor.md`).

### Phase 0: fix what self-hosters hit today (small, ship first)

- `ResolveReleaseChannel` uses the effective plan (`self_host` on a
  self-hosted install), so self-hosted workspaces are never on stable.
  *Bug fixes.*
- Set `OPENV_SELF_HOSTED=true` in both compose files and say so in
  `docs/QUICKSTART.md`, so today's documented install is self-hosted
  before the default changes. *Maintenance updates.*

### Phase 1: seams inside this repository

Introduce the extension points as internal interfaces first, while all
the code is still here, so every later move is a pure move:

1. `PlanPolicy`, `ChannelPolicy` and `WorkspaceObserver` in core, with
   today's behaviour as the implementation. `HandlerDeps` loses
   `*billing.Service`.
2. A module type that registers routes, jobs and migrations. Billing,
   showcase, community pool and visitors are rewired as modules. They
   are still in this repository and still on, so the route golden is
   unchanged.
3. `/api/v1/auth/config` gains `capabilities`. The frontend registry
   lands, and the billing tab, plan UI, channel controls, Visitors
   dashboard and beacon register through it.
4. Frontend goldens (`bundle-shape.json`, `arch/__snapshots__/*`,
   `sizeBudget`) are regenerated where registration changed imports.

### Phase 2: the public `ext` package

- Promote the interfaces and the module type to `ext/`, with the
  read-only views modules need, and `server.Run(..., modules...)`.
- Add an example module under `ext/example/`. It is tested in CI, so the
  API is proven from outside `internal/`.
- Document `ext` in `docs/extending.md`, which is also useful to a
  self-hoster who wants their own integration.

### Phase 3: stand up `openv-hosted` (no product change)

- Create `openv-hosted`. Copy the hosted modules and the marketing site
  into it, building against the current OpenV release.
- `openv-hosted` gets the Railway config, the promotion, staging and
  stable-cut workflows, and the ops docs (`railway.md`,
  `release-policy.md`, the billing plans).
- Deploy openv.app's staging from `openv-hosted`.
  Compare it against today's staging (smoke tests, billing in Stripe test
  mode, prerendered pages, crawl files) before production moves.
- Switch production's Railway services to `openv-hosted`. Railway
  then deploys from `openv-hosted`'s `release` branch. This repository
  keeps CI, CodeQL, release notes and version tags.

### Phase 4: remove the hosted code from this repository

Only after production runs from `openv-hosted`:

- Delete the hosted modules, the marketing site, `frontend/railway.json`,
  the staging icons and manifest, and the openv.app workflows and docs.
  Regenerate the route, bundle and arch goldens. Remove the moved globs
  from `docs/areas.json`.
- The open defaults are now simply what remains: `self_host` plan,
  operator limits, `/` → sign-in. `OPENV_SELF_HOSTED` becomes a no-op
  with a deprecation warning, removed a release later.
- README and `docs/operations.md` lead with self-hosting, and openv.app
  becomes one line. `docs/env-vars.md` loses the hosted variables.
  `CLAUDE.md` loses the Railway and promotion rules, which move to
  `openv-hosted`'s own `CLAUDE.md`. `CONTRIBUTING.md` loses the `ee/`
  paragraph.
- Release note (*Maintenance updates*): self-hosted installs no longer
  show billing, plans or the marketing site, and need no
  `OPENV_SELF_HOSTED`.

### Afterwards: releasing

- OpenV releases are tagged here as now. "Promote to release" no longer
  exists in this repository: it is replaced by tagging, and the release
  notes are cut when a version is tagged.
- `openv-hosted` bumps its OpenV dependency to a tag, adds its own notes
  for hosted-only changes, and runs its own promotion to Railway. The
  per-workspace stable channel lives entirely in its `ChannelPolicy`.

## 6. Risks

- **A long-lived extension API.** `ext` is a contract the maintainer now
  keeps. Mitigation: keep it small (the table in 4.1), grow it only when
  a module needs something, and test it through `ext/example`.
- **Billing reaches deep into workspaces.** Seat counts, plan state and
  read-only mode touch many handlers today (`api/authz.go`'s plan gate on
  every write). Phase 1 must route all of it through `PlanPolicy` and
  `WorkspaceObserver` before anything can move. That is the largest
  single piece of work.
- **The cut-over.** openv.app must run from `openv-hosted`
  (Phase 3) before the code is deleted here (Phase 4). Staging runs the
  new stack first, and Stripe live mode is switched only after staging
  passes.
- **Two repositories drift.** `openv-hosted` pins an OpenV tag, so core
  changes reach openv.app only by a deliberate bump. That is a feature,
  but bumps should be routine: weekly, or with every OpenV release.
- **The codebase refactor in flight** (`docs/plans/codebase-refactor.md`)
  touches the same handler and wiring files. Sequence Phase 1 after the
  refactor's steps on `internal/api` and `cmd/server`, or fold it into
  them.

## 7. Effort

| Phase | Size | Depends on |
|---|---|---|
| 0 | 1 small PR | — |
| 1 | 4–6 PRs, mostly behaviour-preserving | 0; refactor sequencing |
| 2 | 2 PRs | 1 |
| 3 | new repos, Railway reconfiguration, staging soak | 2 |
| 4 | 2–3 PRs, mostly deletion | 3 in production |

Phase 0 helps self-hosters this week. Phases 1–2 are the real work.
Phases 3–4 are mostly moving code and switching deployments.
