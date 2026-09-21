# Stripe billing in-app, and making the tiers real

Status: approved 2026-09-21, nothing implemented yet. This plan supersedes
`docs/billing-odoo.md`, which proposed Odoo as the billing system of record;
that file is kept as the record of the rejected alternative. Requirements,
design items and test cases land in the OpenV Platform project phase by
phase alongside the code, per `CLAUDE.md`.

## 1. Where we are

OpenV has a pricing page with four hosted tiers, three of which say
`'Coming soon'`, and a `plan` column only a platform admin can change
(`PUT /api/v1/orgs/{id}/plan`, REQ-154). Two problems, not one.

**There is no way to buy anything.** The previous design put Odoo in the
middle — Odoo owned the commercial record and the checkout, OpenV mirrored an
entitlement. That is superseded: **Stripe direct, in the app.** Stripe Tax
and Stripe Invoicing cover the tax and invoicing that were Odoo's
justification; enterprise sales and CRM go by email for now.

**The tiers are not real.** Outside `internal/domain/orgs/limits.go` and
`channels.go`, the only plan check in the Go codebase is
`org.Plan != orgs.PlanOpenSource` in `internal/api/share_handlers.go`. Every
count limit ships at `0 = unlimited` on every plan. Of everything the pricing
page sells as paid, exactly two things are enforced — cloud-runner resource
numbers and the release channel. Always-on hosted runners, teams, per-project
access grants, shared company workspaces, workspace budgets and usage rollups
are all free today.

**Who pays, and for what.** The buyer of a hosted requirements/V&V tool is a
quality lead or engineering manager at a small regulated firm — medical,
aerospace subcontract, machine builders — who will not run Postgres, backups,
TLS and upgrades and would not be allowed to. Self-hosting being free under
ELv2 does not make gates pointless for that buyer; it means the people who
self-host were never going to pay and are worth more as references. So the paid
layer is the **company layer** — seats, shared workspaces, teams, per-project
grants, budget rollups, unattended hosted compute — and the product itself
(traceability, baselines, history, V&V, export, agent runs on your own AI)
stays free, because withholding it makes the free tier a worse advertisement
without changing who pays.

Outcome: a workspace admin upgrades from inside OpenV, pays by card, and sees
the entitlement live before the return page renders; tier boundaries are
enforced server-side; workspaces created before an announced date keep what
they have, permanently and mechanically; and an install with no Stripe keys
behaves exactly as today, with no outbound call.

## 2. Decisions taken

| | |
|---|---|
| Provider | Stripe direct. No Odoo, no CRM |
| Purchase surface | **Stripe Checkout, redirect**, returning to the Billing tab |
| Tax | **Stripe Tax**, OpenV stays merchant of record (we register and file) |
| Self-serve tiers | `business_lite` (**hard-coded quantity 1**, never seat-synced) and `business` (per-seat) |
| Enterprise | Email → platform-admin `PUT /plan` → invoice by hand. Never a Stripe subscription |
| Seats | Member count leads; quantity pushed after the fact; Stripe prorates. Business only |
| Trial | **14 days, card up front, one trial per buyer**, promotion codes on |
| Currencies | GBP, USD, EUR via `currency_options`; a customer's currency is fixed by its first purchase |
| Sync | **Poll-only at launch**: one paginated subscription list per 5-minute tick, plus a synchronous post-checkout refresh. No webhook receiver until the staleness metric says the poll cannot keep up |
| Moves to paid | Unattended hosted compute → Lite; shared workspaces + seats, teams and per-project grants, workspace budget + usage rollups → Business |
| Hosted compute | **Metered**: 300 hosted-runner minutes/month on free, unlimited on paid. Runs via the Agent Connector on your own machine are never metered |
| Free taste | One shared workspace **created by you**, two members (creator + one) |
| Over plan | **Read-only**: existing work readable and exportable, not editable, until the workspace upgrades or trims. Nothing is ever deleted |
| Grandfathering | Workspaces created **before a date announced with the first live price** keep alpha terms forever |
| Phases | 1) plumbing + client + read-only sync; 2) checkout; 3) seat sync; 4) gates + numbers + grandfather |

One risk stated once: the review of this plan predicted that metering hosted
minutes converts nobody, because the connector is unmetered and already the
better option, and Lite's real pitch (`hosted_automation`) is a feature gate
anyway. The maintainer chose to keep the hard allowance. The 80%/100% alerts
and the usage line in the tab will show whether it moves anyone; the number
is one value in `PlanDefaults`.

## 3. Non-goals

- No `402`. Resource refusals keep **403 `limit_reached`** and its `remedy`;
  the header of `internal/api/limits.go` explains why, and it still holds on
  a deployment with nobody to pay.
- No new Railway service; no webhook receiver at launch; no `billing_events`.
- No plan-gated export or import, in any state including read-only (README
  §Pricing, `ALPHA_NOTE`, `PRICING_FOOTNOTE`, the site FAQ, REQ-113).
- No cap on `max_projects` beyond an abuse ceiling.
- No gate on baselines, version history, traceability, flow-down, V&V,
  sign-in methods, MCP/connector access, or agent runs on the user's own AI
  (NEED-4). **`internal/domain/teams` is agent crews, not people-teams, and
  is never gated.**
- No price number in the repository. The repo holds currency formatting,
  never an amount.

## 4. Entitlements: two axes, one path

### Rename `Org.Plan` → `Org.BilledPlan` (JSON tag stays `plan`)

The billed plan is not the entitled plan once billing exists, and every read
site must be reconsidered exactly once. A rename makes the compiler do that
audit. Known reads to fix on the way: `hosting/provisioner.go`
(`orgs.PlanDefaults(o.Plan)` as a fallback — must use the entitled plan),
`orgs.go`, `share_handlers.go` (open-source check; billed is correct there),
`feature_handlers.go`, and `effectiveChannelSQL` (billed is correct: the
channel follows the plan you bought).

### Status folds into `EffectiveLimits()`; no second function

```go
// EntitledPlan is the plan whose defaults apply RIGHT NOW: the billed plan
// while the billing relationship is in good standing, the free tier once it
// is not. A workspace with no billing relationship — every self-hosted
// workspace, every platform-admin grant, every workspace before the cutover —
// is on its billed plan. That is what keeps billing an optional module.
func (o *Org) EntitledPlan() string {
    switch o.Billing.Status {
    case "", PlanStatusNone, PlanStatusActive, PlanStatusTrialing, PlanStatusPastDue:
        return o.BilledPlan
    default: // canceled, unpaid, paused, incomplete, disputed, unrecognised
        return PlanSingle
    }
}
```

`EffectiveLimits()` calls it where it read the plan; the `selfHosted`
override still runs after, so every existing call site is unchanged and
cannot disagree. `past_due` keeps the paid plan with **no local grace
clock**: Stripe's retries run about three weeks and then take the operator's
terminal action, and two clocks on one fact is a bug generator. **The
terminal action must be `cancel`, not `mark unpaid`** — `unpaid` leaves a
zombie that keeps generating invoices and blocks re-subscribing.

`Entitlements(usage)` is a **reporting** function (entitled plan, billed
plan, status, interval, limits, period end, grandfathered,
`over_plan []string`), with usage injected so the domain stays free of
repositories.

### Boolean entitlements as flag keys in the limits catalogue

`hosted_automation`, `teams`, `workspace_budget` join `catalog []Definition`
with a new `Kind: KindFlag`; `ParseLimits` accepts a bool for flag keys;
`Allowed(limits, key) bool` reads one. One resolution path; per-org overrides
are exactly what grandfathering needs; `OPENV_LIMITS` retunes a deployment;
and `GET /orgs/{id}/limits` and the settings panel render from the
catalogue, so flags get docs and UI for free. Flags must be a distinct kind
because `Ceiling()` treats 0 as *unlimited* — which also means a ceiling of
zero is inexpressible, so no gate is ever expressed as "0 of X". This axis is
orthogonal to `internal/domain/release/features.go`, which gates by channel.

## 5. Schema — migration 45 on `organizations`

`organizations` uses `TIMESTAMP` (`schema_orgs.go`; `deleted_at` from
migration 19), so new timestamps match, written as UTC.

```sql
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS billing_customer_ref      VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS billing_subscription_ref  VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS billing_item_ref          VARCHAR(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS billing_currency          VARCHAR(3)   NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS plan_status               VARCHAR(32)  NOT NULL DEFAULT 'none',
  ADD COLUMN IF NOT EXISTS plan_interval             VARCHAR(8)   NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS plan_seats                INT          NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS plan_cancel_at_period_end BOOLEAN      NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS plan_grandfathered        BOOLEAN      NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS plan_period_end           TIMESTAMP,
  ADD COLUMN IF NOT EXISTS plan_synced_at            TIMESTAMP;
ALTER TABLE users ADD COLUMN IF NOT EXISTS billing_trial_used_at TIMESTAMP;

CREATE UNIQUE INDEX IF NOT EXISTS idx_orgs_billing_subscription
  ON organizations(billing_subscription_ref) WHERE billing_subscription_ref <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_orgs_billing_customer
  ON organizations(billing_customer_ref) WHERE billing_customer_ref <> '';
```

Why these and not more: `plan_interval` so the tab renders without a Stripe
call; `billing_currency` because a Stripe customer's currency is locked by
its first subscription and a later checkout in another currency is a 400;
`plan_grandfathered` because a promise made in public deserves a column with
its name on it; `billing_trial_used_at` on `users` because a person can
create workspaces freely, each a fresh Stripe customer, so the one-trial rule
has to be keyed on the human. No `billing_provider` (the customer ref being
set is the flag), no stored price ref (the unknown-price check runs against
the live read), no event table (no webhooks).

`idx_orgs_billing_subscription` is the most important line: one subscription
can never entitle two workspaces, as a constraint rather than a code path.

**Repository** (`internal/persistence/postgres/org_repository.go`): append
the columns to `orgColumns`, `orgColumnsQualified` and `scanOrg` at the end
(the `extra` tail is positional); `Org` gains a `Billing` sub-struct with the
refs `json:"-"`. Narrow writers in the existing `SetPlan`/`ClaimBudgetAlert`
style: `SetBillingCustomer`, `ApplyBillingState`, `SetBilledSeats`,
`ClearBillingSubscription`, `SetGrandfathered`, `FindOrgByBillingRef`,
`ListBillingOrgs`, `MarkTrialUsed`.

`ApplyBillingState` is one atomic conditional UPDATE writing plan, status,
interval, seats, period end, cancel flag, refs and `plan_synced_at`, guarded
by `WHERE (plan_synced_at IS NULL OR plan_synced_at <= $readAt)` so a slower
refresh holding an older snapshot never overwrites a fresher reconcile;
`RowsAffected() == 0` is success. It never writes the plan column when the
billed plan is `enterprise` or `open_source` — those are platform-admin
grants, and a grant wins over a subscription.

**Two existing bugs to close first, in Phase 1:**

- `OrgRepository.UpdateOrg` writes `plan` and `limits` from the in-memory
  struct via a read-modify-write. Not a privilege bug today; a silent-
  downgrade race once billing writes concurrently. Narrow its `SET` list to
  `name, updated_at`.
- `checkSharedWorkspaceCount` (`internal/api/limits.go`) counts workspaces
  the user *belongs to*, not ones they created, and lets the most generous
  membership lift the cap — so with `max_shared_workspaces: 1` a free user
  invited into one shared workspace could never create their own, and every
  member of any Business (or grandfathered) workspace would have unlimited
  free ones. Fix: count shared workspaces where `created_by = userID` and not
  deleted; take the ceiling from the user's personal workspace. `countFor`
  mirrors the same fix. The catalogue text already says "how many you can
  create".

## 6. Module shape and client

`internal/billing/` — not `ee/`. This is hosted plumbing under ELv2, inert
without keys; `TestSelfHostedNeverContactsStripe` makes that a tested
property. Files: `billing.go` (Provider interface, state, status mapping),
`prices.go` (registry + cached amounts), `service.go` (customer, checkout,
portal, change plan, cancel, apply), `reconcile.go`, `seats.go`,
`stripe/{client,types}.go`.

**Hand-rolled client rather than `stripe-go`** — to be confirmed at
implementation time, the `Provider` interface being the escape hatch. Nine
calls (customers, checkout sessions, portal sessions, subscriptions
get/list/update/cancel, subscription items update, prices get, disputes
list), form-encoded in, JSON out; the repo hand-rolls every outbound client;
the SDK pins the Stripe API version to a library major; a generated 400-field
struct invites reading `price.nickname`, the exact "rename changes an
entitlement" failure. Mitigations: a `stripeAPIVersion` const on every
request, one `do()` with backoff on 429/5xx/transport only, three attempts,
per-call deadlines, `*APIError{Status,Type,Code,Message,RequestID}` with
`RequestID` logged and `Message` never forwarded, never log a body. The
counter-argument is the `go-oidc` precedent: this repo does take a library
for security-critical protocol code.

**Idempotency keys.** Customer creation: `openv:cust:<orgID>:v1`. Checkout:
`openv:co:<orgID>:<plan>:<interval>:<currency>:<5-min bucket>` (currency
included, or two currencies in one bucket hit "same key, different params").
**Quantity updates carry a fresh UUID key per attempt** — a deterministic
`…:<qty>` key is replayed by Stripe for 24 hours, so 7→8→7 sticks at 8 and
drift repair replays forever. Setting a quantity is naturally idempotent; the
key only needs to protect a single retry burst.

**Price registry** — `OPENV_STRIPE_PRICES`, a JSON array parsed at boot.
Shape errors are fatal (unknown plan, bad interval, duplicate
(plan, interval), `plan` outside {`business_lite`, `business`} — the line
that makes "a Stripe misconfiguration cannot grant enterprise" true). Fetch
failures are not fatal: the price fetch that catches a test-key/live-id
mismatch and checks `interval` and `usage_type=licensed` runs at boot and on
every reconcile tick; until it succeeds the catalogue is unsellable and
`/public/plans` reports `billing_enabled:false`. Boot must never depend on
Stripe being up. One Price per tier per interval with `currency_options` for
GBP/USD/EUR, so the registry stays one-dimensional. The plan key comes only
from our price-id map; Stripe's product name, nickname and metadata are never
read to decide a plan. An unknown price or a multi-item subscription never
changes a plan: `billing_unknown_price_total++`, alert, leave it.

**Configuration** (Railway API-service variables only): `STRIPE_SECRET_KEY`
(unset ⇒ billing off), `OPENV_STRIPE_PRICES`, `OPENV_BILLING_RETURN_URL`,
`OPENV_BILLING_RECONCILE_MINUTES` (5), `OPENV_BILLING_MAX_SEATS` (500),
`OPENV_BILLING_PORTAL_CONFIG`, `OPENV_BILLING_GRANDFATHER_BEFORE` (the
announced date, RFC3339). `OPENV_SELF_HOSTED=true` with keys present disables
billing with a warning rather than failing to boot. All billing routes are
registered unconditionally (the route-inventory test builds a bare handler)
and answer 404 `billing_unavailable` inside the handler when off.

## 7. Endpoints

| Method + path | Auth | Purpose |
|---|---|---|
| `GET /api/v1/public/plans` | none | Catalogue with live amounts per currency. Served from cache; never calls Stripe per request |
| `GET /api/v1/orgs/{id}/billing` | org admin | State for the tab. No Stripe call |
| `POST /api/v1/orgs/{id}/billing/checkout` | org admin | `{plan, interval, currency}` → `{url}` |
| `POST /api/v1/orgs/{id}/billing/change` | org admin | `{plan, interval}` → update the existing subscription's item price and quantity in place; Stripe prorates |
| `POST /api/v1/orgs/{id}/billing/portal` | org admin | `{url}` — payment method, address, tax id, email, invoices, cancel at period end. Product switching disabled in the portal configuration |
| `POST /api/v1/orgs/{id}/billing/refresh` | org admin | Synchronous re-read and apply; `{session_id}` binds a just-completed checkout after verifying `client_reference_id == orgID` |

`change` exists because plan changes must stay on one subscription per
workspace (a second subscription cannot be prorated against the first);
`business_lite` → quantity 1, `business` → `countOrgSeats`. Checkout is
refused (`409`) when a non-terminal subscription exists, when the billed plan
is `enterprise` or `open_source`, or for `business` on a personal workspace;
allowed again once the status is terminal, with `ClearBillingSubscription` on
the new bind. Currency comes from `billing_currency` when set, else from the
request (the selector shows only before the first purchase). The trial is
attached only when the buyer's `billing_trial_used_at` is null, and set on
bind. Two admins completing two checkouts is handled at bind: if the org
already holds a different non-terminal subscription, the newer one is
cancelled immediately with refund and an alert fires. The client never sends
a price id, customer id, subscription id or quantity; the server derives all
four.

Error codes: `billing_unavailable` 404, `billing_upstream` 503 with
`Retry-After`, `already_subscribed` 409, `no_customer` 409, `unknown_plan`
400, and `plan_read_only` 403 (section 11). Rate limits via the existing
`ratelimit.go` limiters: writes (checkout, change, portal) burst 5 / 20 per
hour per workspace; refresh burst 10 / 120 per hour.

## 8. Reconcile — the only sync path at launch

`internal/billing/reconcile.go`, `Start(ctx, interval)` in the shape of
`notify.SupportWindowWatcher`, constructed only when enabled, run once at
boot like the purge job, then every five minutes. One loop: paginated
`GET /v1/subscriptions?status=all`, matched by stored ref first, then
`metadata.openv_org_id`, never by email or name; apply each with `readAt`;
repair seat drift for Business; refresh the price cache; list open disputes
and treat a disputed subscription's workspace as `disputed` (terminal) until
resolved, with an alert. A subscription whose workspace is deleted or purged
is cancelled immediately and alerted. A bind that would move a subscription
between workspaces is refused and alerted. A failed read never changes a
plan.

Metrics via `internal/metrics`: `billing_sync_stale_seconds` (alert above
three times the interval), `billing_stripe_requests_total{op,status}`,
`billing_unknown_price_total` (alert on any increase), `billing_seat_drift`.

Post-checkout: the return page calls `refresh` with the session id before it
renders, so the entitlement is live before the customer sees the page. If it
fails the page says "Payment received — your plan will update within a few
minutes", which is true and better than an error after someone has paid.

## 9. Workspace lifecycle collisions

- **Delete** (soft): cancel the subscription at period end; undo on restore.
  **Purge**: cancel immediately. Never delete the Stripe customer — invoices
  are tax records. `refresh` and bind refuse a deleted workspace.
- **Platform-admin `SetPlan`** to `enterprise` or `open_source` while a
  non-terminal subscription exists → `409`, cancel it first. Reconcile never
  overwrites a granted plan.
- **The channel flip.** `effectiveChannelSQL` derives the channel from the
  plan, and `release.Enabled()` returns `false` for every feature on the
  stable channel with no stable release. Applying `plan = business` would,
  by itself, close every channel-gated feature for the new subscriber —
  including the Billing tab they just used. Fix in `ApplyBillingState`: when
  moving a workspace from a non-choosable plan to a choosable one and
  `release_channel` is empty, write `release_channel = 'nightly'` as the
  override, so nothing changes for them and the existing Release-channel card
  lets them opt into stable. The Billing tab is never gated for a workspace
  that already has a subscription; the `workspace-billing` feature key gates
  only the plan picker for a workspace with no subscription.
- **The admin who bought leaves.** Receipts go to the Stripe customer email;
  the Portal allows updating it. The tab says so.

## 10. Seat sync (Business only)

Quantity = `countOrgSeats(orgID)` — members plus pending invitations, the
function `checkOrgSeats` and the limits panel already use, so the invoice and
the panel can never disagree. `SeatsChanged(orgID)` is a non-blocking enqueue
after the local write commits, from `addOrInviteToOrg`, `RemoveOrgMember`
and invite revoke; not on acceptance (the count is unchanged); not in the
purge sweep; skipped entirely for `business_lite`. A buffered channel feeds
one goroutine holding a set of dirty workspaces, drained on a two-second
timer, so a bulk invite is one push. Never inline: the handler never sees a
billing error; drift repair fixes a lost push within one tick. Floor 1; above
`OPENV_BILLING_MAX_SEATS` we refuse to push, log and alert — we would rather
under-bill than let a runaway invite loop auto-charge a five-figure invoice.

## 11. The gates, and read-only over plan

| Gate | Mechanism | Enforced at |
|---|---|---|
| Unattended hosted compute → Lite | flag `hosted_automation` | the hosted claim in `agentruns` (the `HostedAfter` path and `Claim` by a hosted worker) and `CreateHostedRunner`. Not at automation create — automations have no runner-targeting field, and a cron automation claimed by the user's own worker key is exactly the run never gated |
| Teams + per-project grants → Business | flag `teams` | `CreateOrgTeam` (table `org_teams`) and the project-role grant paths. Not `internal/domain/teams` |
| Workspace budget + usage rollups → Business | flag `workspace_budget` | the workspace rollup in `GET /orgs/{id}/usage` and the budget-enforce path; a member always sees their own runs |
| Shared workspaces + seats → Business | `max_shared_workspaces` (fixed to count creations), `max_members` | `checkSharedWorkspaceCount`, `checkOrgSeats` |
| Hosted-runner minutes → metered | `hosted_runner_minutes_month` | lease creation in `runner_session_handlers.go`; rolled up per workspace per calendar month from `runnersessions` lease timings; 80%/100% alerts via the `ClaimBudgetAlert` pattern |

**Read-only over plan.** When `Entitlements().OverPlan` is non-empty, every
mutating request scoped to that workspace or its projects is refused with
`403 plan_read_only` and the same `remedy`, via one check
(`h.requireWritable(orgID)`) at the mutating handlers — except the actions
that get a workspace back under plan or out (remove member, leave, delete
project, delete workspace), the billing endpoints, and export and import,
which are never refused in any state. A lapsed trial therefore cannot mint a
permanent free team: the extra member is still there, still reads, still
exports, and nobody edits until someone pays or trims.

### Tier values (Phase 4)

| Plan | `max_members` | `max_shared_workspaces` | `max_projects` | flags | minutes/month |
|---|---|---|---|---|---|
| `single` / `free` | **2** | **1** | 200 (abuse) | all off | **300** |
| `business_lite` | 2 | 1 | 500 (abuse) | `hosted_automation` | unlimited |
| `business` / `team` / `open_source` | **0** (billed per seat) | 0 | 1000 (abuse) | all on | unlimited |
| `enterprise` / `self_host` | 0 | 0 | 0 | all on | unlimited |

`business` keeps `max_members: 0` — you do not cap seats on a plan that
bills per seat. Lite gets free's counts, not fewer. The funnel: a shared
workspace starts on `single`; the creator is seat 1, so the second invite
hits `checkOrgSeats` → 403 `limit_reached` → remedy naming the Billing tab.

## 12. Keeping the alpha promise — with a date

`ALPHA_NOTE` says: "When tiers launch you keep what you have until we
announce otherwise." The announcement is Phase 2's release notes, naming the
date after which new workspaces are on tier limits. Migration 46 (Phase 4)
then writes per-workspace `limits` overrides — every count 0, every flag
true, minutes 0 — and `plan_grandfathered = TRUE` for rows with
`created_at < OPENV_BILLING_GRANDFATHER_BEFORE`, including soft-deleted ones
(a restored workspace comes back to the terms it left under). Because
`org.Limits` is the most specific layer, that is the published sentence
enforced by the same data the enforcement reads. It is a numbered migration
and runs once; without the date predicate everyone who signed up between
Phases 2 and 4 would be exempt forever.

## 13. Frontend

A Billing tab in `views/OrgSettings.tsx` after Limits, `OrgBillingTab.tsx`
beside `OrgLimitsTab`, admin-only; the read-only Plan card links to it. Per
status: no subscription → the two plans with live per-seat prices, a
month/year toggle whose saving is computed, a seat preview ("7 members and
invitations × £X, plus VAT"), currency selector, "Continue to Stripe";
trialing/active → plan, interval, "billing 7 seats · 7 members" or
"syncing…", renews/ends on, Change plan (in-app), Manage billing (Portal);
past due → a red card and an admin banner: "A payment failed. Your workspace
keeps working — update the card to avoid interruption."; canceled/disputed →
"Everything you have made is still here and still exportable", plus what is
read-only and why; grandfathered → a quiet line. `/pricing`: `planKey` on
`PricingTier`, `'Coming soon'` kept as the fallback for
`billing_enabled:false` and self-hosted installs, live amounts otherwise.

**Copy tripwires, same PR as the first live price:** `ALPHA_NOTE`,
`PRICING_FOOTNOTE`, the `tiers` FAQ in `frontend/src/site/content.ts`,
README §Pricing, `e2e/tests/landing.spec.ts` (asserts `Coming soon` exactly
three times and the `ALPHA_NOTE` opening) and `Landing.test.tsx`.

Feature key `workspace-billing` gates the plan picker for an unsubscribed
workspace and the `checkout` handler. Phase 4 registers no gating key:
`PlanDefaults` is a pure function with no channel in scope and the plans
that get caps are always nightly, so a key there would be decorative — the
release-notes bullet says so.

## 14. Security — what must be true

No card data, no Stripe.js, no publishable key anywhere in the frontend. The
secret key is a Railway API-service variable only — not the frontend
service, `docker-compose*.yml`, `Dockerfile.api` build args, agent runs,
runners or `examples/`; before Phase 1 ships, verify the agent-run
environment builder passes an allowlist, not the process environment.
`gitleaks` already gates CI and covers `sk_live_`/`rk_live_`; fixtures stay
obviously fake. `PUT /orgs/{id}/plan` stays platform-admin; `PUT /orgs/{id}`
reaches only a name after the `UpdateOrg` narrowing; no new column is written
by any handler except the sync path and the admin endpoint. The client never
sends a price, customer, subscription or quantity. `client_reference_id` is
set at session creation and re-checked on `refresh`. The two unique indexes
make tenant confusion a database error. With no webhook receiver there is no
unauthenticated write-adjacent endpoint at all; `/public/plans` serves cache.
Self-hosted: no key, no goroutine, no outbound connection, endpoints 404.

## 15. Operator checklist (Stripe dashboard)

1. Legal entity, address, and a Stripe Tax registration per jurisdiction.
   Stripe calculates and reports; OpenV files. Confirm before the first live
   charge — the largest non-code commitment here.
2. Stripe Tax on; tax behaviour `exclusive`; SaaS tax code on both products;
   copy says "excluding VAT".
3. Two Products; four Prices with `currency_options` GBP/USD/EUR; Business
   per-seat.
4. Receipts and invoices emailed; invoice template with entity, VAT number,
   numbering; tax id on invoices.
5. Dunning terminal action = cancel. This *is* the grace period.
6. Portal configuration: payment method, address, tax id, email, invoices,
   cancel at period end. No product or price switching. Save the id into
   `OPENV_BILLING_PORTAL_CONFIG`.
7. Promotion codes on. Radar defaults. No webhook endpoint yet.
8. Test mode mirrored; staging gets test price ids. Set the Railway variables
   before the promotion that carries Phase 1 — the boot shape check is fatal.
9. Subprocessor disclosure line on the privacy page.
10. Later: "invoice me" as a Checkout payment option for the firm whose
    procurement will not use a card.

## 16. Phases

Every phase: a `RELEASE_NOTES.md` bullet under one group, `routes.txt`
regenerated when routes change, DCO sign-off, OpenV Platform artifacts.

**Phase 1 — plumbing, client, read-only sync. Cannot charge.** Migration 45;
the `BilledPlan` rename and every read site fixed; `EntitledPlan()` and the
`EffectiveLimits()` fold; flag kinds in the catalogue; `UpdateOrg` narrowed;
`checkSharedWorkspaceCount`/`countFor` fixed to count creations; the
registry (shape fatal, fetch not); the client; the reconciler with boot run,
disputes and deleted-workspace cancellation; `GET /public/plans`,
`GET /billing`, `refresh`; metrics; delete/purge hooks; `plan_status` and
`grandfathered` on the limits response; register `workspace-billing`; the
`docs/api-spec.md` route table and `docs/operations.md` env vars.
Milestone: an operator creates a subscription by hand in the dashboard with
`metadata.openv_org_id` and watches the entitlement land within a tick — the
whole sync path, tax and invoicing proven before any button exists.
Tests: entitlement resolution over every (plan × status) asserting the
no-subscription case is identical to today; flag parsing; the unique index,
the `readAt` guard and the round-trip on the existing Postgres fixture;
client tests against an `httptest.Server` asserting form encoding,
idempotency and `Stripe-Version`, a 429 retried and a 402 not; reconcile
against a fake (never downgrades on read error, unknown price leaves the
plan, refuses a conflicting bind, cancels an orphan);
`TestSelfHostedNeverContactsStripe`; the shared-workspace count fix; the
landing page in both catalogue states.
Notes: `### Maintenance updates`.

**Phase 2 — checkout, change, portal, tab. This is the phase that can
charge.** The three routes; `EnsureCustomer` with currency lock and
one-trial-per-buyer; the channel-flip fix and two-subscription handling at
bind; `OrgBillingTab.tsx`; all copy, e2e and landing test changes in the
same commit, and the release notes name the grandfather date.
Tests: member refused / admin allowed / already subscribed / enterprise
refused / personal-and-`business` refused / second trial refused / currency
mismatch refused / rate limit / 503 with `Retry-After`; bind with a competing
subscription cancels the newer; a bind on a nightly-plan workspace writes the
nightly override; the tab per status.
Notes: `### New features`, key `workspace-billing`.

**Phase 3 — seat sync.** `seats.go`, call sites, drift repair, the
billed-versus-counted line, `OPENV_BILLING_MAX_SEATS`. Tests — the
load-bearing one: the invite succeeds when the fake provider errors; once
per change, not on acceptance, never for Lite; coalescing; floor; the
MAX_SEATS refusal; drift both ways; a fresh key per quantity attempt.
Notes: `### Maintenance updates`.

**Phase 4 — gates, numbers, read-only, grandfather. Ships alone, on the
announced date.** The three flags at the call sites above; hosted-minutes
rollup, enforcement and alerts; `requireWritable` at the mutating handlers
with its allowlist; the tier values in `PlanDefaults`; migration 46 keyed on
`created_at < OPENV_BILLING_GRANDFATHER_BEFORE`; the hosted `remedy` names
the Billing tab and the UI links it.
Tests: the new numbers; a migration test that a row before the date keeps
unlimited and one after does not; the refusal on a second invite to a
`single` shared workspace; the hosted claim refused for a free workspace
while a connector claim of the same run succeeds; `CreateOrgTeam` refused on
free; an over-plan workspace refuses an artifact edit with
`plan_read_only`, allows member removal, and exports successfully.
Notes: `### New features`, grandfathering stated plainly.

Enterprise stays outside all four: email → `PUT /plan` → invoice by hand.

## 17. Testing strategy

Fakes follow `internal/api/org_plan_handler_test.go`: embed the `Provider`
interface as nil and implement only what the test needs, so an unexpected
call panics loudly; the fake records calls and scripts errors. No test reads
`STRIPE_SECRET_KEY` or calls Stripe. The Stripe CLI and test clocks are a
manual runbook, not CI — test clocks are the only practical way to see
`past_due` and `canceled` before a customer does.

## 18. Verification

- `make test`, the frontend suite, `make secrets` before every push;
  migration tests green; `routes.txt` regenerated deliberately.
- Disabled path: no `STRIPE_SECRET_KEY` → every `/billing/*` route 404s, no
  goroutine, no outbound connection, `GET /orgs/{id}/limits` byte-identical.
- Staging acceptance (test mode), Phases 2–3: live amounts and toggles on
  `/pricing`; checkout with a test card, an EU VAT id shows reverse charge, a
  consumer address adds VAT; the return page shows `active` before it
  finishes loading; DB and limits reflect Business; invite three → quantity
  4 with proration within a tick; break Stripe and invite a fifth → the
  invite succeeds, the tab says syncing, the quantity catches up; test clock
  a month → period end moves; fail the card → `past_due`, red card,
  everything still works; cancel at period end → "ends on"; past it →
  Single, project readable, creation refused, export succeeds; move the
  customer to an unlisted price → plan unchanged, metric increments; second
  checkout as another admin → the newer is cancelled with an alert; staleness
  low, drift zero, no key in the frontend service.
- Phase 4 on staging: a workspace created before the date keeps everything;
  one created after hits the second-invite refusal; a lapsed over-plan
  workspace is read-only, trims a member, becomes writable, exports
  throughout.
- OpenV Platform project: per phase, record requirements, design items and
  test cases with their links — entitlement resolution by billing status
  (refining REQ-77, REQ-123, DES-38); the grandfathering promise with its
  date; scheduled reconciliation that never downgrades on a failed read; the
  subscribe, change-plan and manage-billing journeys; per-seat billing that
  never blocks a membership change; plan-limited members and read-only over
  plan; a test case linked to REQ-113 asserting export on a read-only
  workspace. Lint wording with `get_quality_findings`, record test runs as
  evidence, baseline after Phases 2 and 4, and refresh `docs/exports/` after
  each baseline.

## 19. Decisions for the maintainer

1. Merchant-of-record reality — registrations before the first charge.
2. Hand-rolled client versus `stripe-go` (recommendation: hand-rolled;
   reversible through the `Provider` interface).
3. One Stripe customer per workspace (the unique index) — recommended now; a
   migration once duplicates exist.
4. The grandfather date itself, to go in Phase 2's release notes.
5. Tax display `exclusive`; seat ceiling 500.
