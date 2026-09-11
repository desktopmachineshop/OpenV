# Subscriptions, signup, and membership tier — with Odoo as the billing system

How OpenV can sell and enforce paid workspaces without growing a billing
system of its own: **Odoo owns the commercial record, OpenV owns identity and
entitlements, and the marketing site stays in this repository** so it can be
designed, reviewed, and deployed like the rest of the product.

This is a design proposal, not shipped behaviour. Nothing here is implemented
yet; `orgs.Plan` is still set by hand in the database
(`internal/domain/orgs/limits.go`) and the workspace settings screen says
"billing coming soon" (`frontend/src/views/OrgSettings.tsx`).

**Platform constraint: Odoo 19 Online (SaaS), no custom modules.** That is
assumed throughout and it decides several things — the join key uses stock
fields, the integration is pull-only over the external API, and every Odoo-side
setup step below is something you click in the UI. §2 and §5 are written to
that constraint rather than around it.

## 1. The split

| Concern | Owner |
|---|---|
| Customer/company record, addresses, VAT number | Odoo (`res.partner`) |
| Plan catalogue, prices, currencies, pricelists | Odoo (`product.template`) |
| Subscription lifecycle, renewals, dunning, churn | Odoo |
| Payment capture, cards, mandates, PCI scope | Odoo payment provider (Stripe/Mollie/GoCardless) |
| Invoices, credit notes, tax, OSS/VAT filing | Odoo |
| Self-service billing portal ("change card", "cancel") | Odoo portal (`/my/subscriptions`) |
| Users, sessions, SSO | OpenV |
| Workspaces, members, projects, artifacts | OpenV |
| **Entitlements** — what a workspace may do today | OpenV |
| Usage metering (runs, tokens, cost) | OpenV, reported to Odoo |

The rule that keeps this clean: **no money in the OpenV database.** OpenV
stores a mirrored *entitlement snapshot* — tier, status, seats, period end,
and the Odoo record ids — and nothing else. No prices, no invoices, no card
references, no payment webhooks that move balances. If a question is "how
much and when", it is Odoo's; if it is "may this workspace do X right now",
it is OpenV's.

The payoff is not just tidiness. Odoo already does the parts that are
genuinely hard and legally consequential: EU VAT and OSS, invoice
numbering and sequences, proration on plan changes, retries and dunning
emails on failed payments, refunds and credit notes. None of that is worth
reimplementing in Go.

## 2. The join key: Odoo partner ↔ OpenV workspace

Subscriptions attach to **workspaces (`orgs`), not users.** A user is an
identity; a workspace is what has members, projects, hosted runners and
limits, and is therefore what has a bill. This also matches Odoo, where the
subscriber is a `res.partner`.

On SaaS you cannot add a field with a module, and Studio is only on the
Custom plan — so use fields that ship with Odoo and are free text:

- `res.partner.ref` ("Reference") holds `openv:<workspace-uuid>`.
- `sale.order.client_order_ref` ("Customer Reference") holds the same string
  on the subscription order.

Both are standard, searchable in a domain, and visible in the UI, so a human
can see which workspace an account belongs to without any customisation. If
you do have Studio, an `x_openv_org_id` field is tidier and the design is
otherwise unchanged — but nothing here needs it.

The two-place duplication is deliberate: the order ref survives a partner
being merged or re-created, and the partner ref lets you find the account
when someone buys again later.

On the OpenV side: `orgs.billing_customer_ref` (Odoo partner id) and
`orgs.billing_subscription_ref` (Odoo order id).

Personal workspaces get a partner lazily, only when someone first upgrades —
free users never touch Odoo, so the CRM does not fill with accounts that will
never buy anything.

## 3. Odoo setup

All of this is UI configuration in your existing database — no module, no
server-side code.

One product per tier, sold as a recurring product:

- `product.template` per tier ("OpenV Team", "OpenV Business"), *Sales OK*,
  with a recurring price line per billing period. Odoo 19's subscription
  pricing lives on the product's *Time-based pricing* / recurring price
  lines, keyed to a `sale.subscription.plan` (Monthly, Yearly).
- A pricelist per selling currency. The tax on the product decides whether
  the stored `list_price` is net or gross (`price_include`), which the
  pricing page has to know — see §7.
- A payment provider in live mode (Stripe is the least friction on Online),
  and the customer portal enabled so subscribers manage their own payment
  method and cancellation.

**Model shape.** Since Odoo 17, subscriptions *are* sale orders: an order
with `is_subscription` true, a `plan_id` recurrence, and a
`subscription_state` driving the lifecycle. Odoo 19 keeps that shape, so
there is no `sale.subscription` model to look for. Confirm the exact
selection values on your database before coding against them — the read-only
`odoo-product-info` skill can introspect (`odoo_query.py fields sale.order`)
with no new access and no writes.

**Access.** The integration is the external API (XML-RPC/JSON-RPC), which
Odoo Online exposes on paid plans with an API key. Create a dedicated
integration user rather than reusing a person's login, so the key can be
revoked without locking someone out, and so the audit trail names the
integration. Credentials follow the pattern the existing skill establishes —
`ODOO_URL`, `ODOO_DB`, `ODOO_USER`, `ODOO_API_KEY`, from the environment,
never committed. In Railway they belong in the API service's variables.

**Do not extend `odoo-product-info` to write.** It is read-only by design and
other skills depend on that. Creating partners and quotations is a separate,
explicitly-scoped path inside the API service (`internal/billing/odoo`).

### What SaaS rules out, and what that forces

| Not available on Odoo 19 Online | Consequence |
|---|---|
| Custom modules / server-side Python | Every Odoo-side step is UI config; all logic lives in OpenV |
| Custom fields without Studio | Join key uses `ref` / `client_order_ref` (§2) |
| Outbound HTTP from automation rules | No webhook to OpenV — the sync is pull-only (§5) |
| Custom portal or signup pages in Odoo | Signup stays in OpenV; the landing page stays in this repo (§7) |

None of these is a real loss. The pull-only sync is the design you would
want anyway (§5), and keeping the logic in Go rather than split across an
Odoo module is easier to test and review.

## 4. Signup and upgrade flows

Signup stays in OpenV. Payment happens in Odoo. The two meet at a checkout
redirect and at a mirrored entitlement.

**Free signup (unchanged):** landing page → `/login` in register mode →
`POST /api/v1/auth/register` → `provisionPersonalWorkspace` creates the
personal workspace, plan `free`, seeded agents. Odoo is not involved.

**Upgrade (new):**

1. An org admin picks a tier in workspace settings.
2. `POST /api/v1/orgs/{id}/billing/checkout {plan, period, seats}` — OpenV
   ensures an Odoo partner exists for the workspace (creating it with `ref`
   set to `openv:<uuid>`), creates a subscription quotation for the tier
   product with `seats` as the quantity and the same string in
   `client_order_ref`, and returns the order's portal payment URL with its
   access token.
3. The browser is redirected to Odoo. Card details never reach OpenV.
4. Odoo confirms the order on payment; the subscription starts running.
5. Odoo redirects back to OpenV, which reconciles that one workspace on the
   spot (§5) — so the entitlement is live before the page finishes
   loading, with no webhook involved.

**Manage billing:** a link to the Odoo portal. OpenV builds no billing UI
beyond a status line and that link. Invoices, VAT receipts, payment method,
cancellation, and plan changes are all Odoo's screens, already built and
already localised.

**Odoo-first signup**, where someone buys from the marketing site before
having an account, is the same flow inverted: the order carries an email,
OpenV provisions (or invites into) a workspace on first reconcile, and sends
a set-password link. Worth supporting eventually; not worth building first,
because it doubles the identity edge cases (email already registered,
different email at checkout, invited-but-never-accepted) for a flow most
self-serve buyers do not take.

## 5. Keeping the entitlement in sync

**Pull only.** OpenV reads Odoo; Odoo never calls OpenV.

A reconcile job in the API service — the same shape as the workspace purge
job in `cmd/server/main.go`, a goroutine with a ticker — reads every
subscription order out of Odoo in one `search_read` (domain:
`client_order_ref` starts with `openv:`, plus `is_subscription`) and writes
the resulting tier, status and seats onto the matching workspaces. Run it
every few minutes, and on boot.

This is forced by the platform — automation rules on Odoo Online cannot make
outbound HTTP calls, and there is no module to add one — but it is also the
design to prefer on its own merits. A push-only integration is silently wrong
the first time a delivery is missed: a customer pays and stays locked out, or
churns and keeps their seats. A reconcile loop is self-healing, can be
re-run at any time to prove state, and has no secret to leak, no signature to
verify, and no replay window.

**Latency without webhooks.** The one moment a poll interval is too slow is
the seconds after someone pays. Handle it at the redirect: Odoo returns the
customer to `/settings/billing?checkout=done`, and that page calls
`POST /api/v1/orgs/{id}/billing/refresh`, which reconciles that single
workspace synchronously. Rate-limit it per workspace and treat it purely as
an early trigger of work the ticker would do anyway — it reads from Odoo, so
a forged call can at worst cause one redundant read and can never grant a
tier. Everything else (renewals, failed payments, cancellations from the
portal) is not latency-sensitive and rides the ticker.

Suggested state mapping (verify the selection values on your database):

| Odoo `subscription_state` | OpenV `plan` | OpenV `plan_status` |
|---|---|---|
| draft / quotation sent | `free` | `none` |
| in progress, paid | tier product's plan | `active` |
| in progress, invoice overdue | tier product's plan | `past_due` |
| paused | tier product's plan | `paused` |
| closed / churned | `free` | `canceled` |

Read the tier from the order line's product, not from a name-matched string,
and keep the product-id → plan-key mapping in one place in Go so renaming a
product in Odoo cannot silently change anyone's entitlement.

Usage flows the other way, on the same XML-RPC connection.
`GET /api/v1/orgs/{id}/usage` already rolls up runs, tokens and cost per
workspace; a monthly job can write those quantities onto the subscription as
a usage line if you ever meter agent spend rather than just capping it. The
`monthly_budget_usd` field already on `Org` is the natural soft guard-rail
alongside it.

**Failure behaviour matters more than in a push design.** If Odoo is
unreachable, the reconcile job must leave the last known entitlement in place
and log — never downgrade on a failed read. `plan_synced_at` makes a stale
mirror visible; alert on it rather than acting on it.

## 6. Membership tier and status inside OpenV

The existing shape is most of the way there and should be extended, not
replaced. `Org.Plan` plus the `limits` JSONB, merged by `EffectiveLimits()`
over `PlanDefaults(plan)`, is exactly the right structure: the plan sets
defaults, a per-workspace override wins, and an unknown plan degrades to free
rather than to unlimited.

**Schema** (a numbered migration, 0024+, per `migrations.go`):

```
ALTER TABLE orgs
  ADD COLUMN plan_status              TEXT   NOT NULL DEFAULT 'none',
  ADD COLUMN plan_period_end          TIMESTAMPTZ,
  ADD COLUMN plan_seats               INT,
  ADD COLUMN billing_provider         TEXT,
  ADD COLUMN billing_customer_ref     TEXT,
  ADD COLUMN billing_subscription_ref TEXT,
  ADD COLUMN plan_synced_at           TIMESTAMPTZ;
```

**Domain.** Add tiers to the `Plan*` constants and give `PlanDefaults` the
keys a tier actually sells: seats, projects, hosted-runner memory and CPU
(already there), monthly agent-run or token allowance, and boolean feature
gates (hosted runners, OIDC/SSO, baseline export). Then add

```go
func (o *Org) Entitlements() map[string]interface{}
```

which is `EffectiveLimits()` adjusted by `plan_status`: `active` and
`past_due` get the paid limits (`past_due` keeps working through the grace
period Odoo's dunning is already retrying in), `paused` and `canceled` fall
back to free defaults. One function, so "what may this workspace do" has
exactly one answer everywhere.

**Enforcement is server-side, at the point of creation** — adding an org
member, creating a project, enqueueing an agent run — never only in the UI.
The API returns entitlements on `GET /api/v1/orgs/{id}` so the frontend can
grey out what would fail, but the check that matters lives next to the write.

**Downgrade must not destroy data.** A requirements tool holds the evidence
someone's compliance argument rests on. When a workspace drops below its
current usage, block creation and keep everything readable — never
auto-delete projects or evict members. "You are over your plan; existing work
is read-only until you upgrade or remove some" is the correct behaviour and
the only defensible one for this product.

**Seats.** OpenV counts members; Odoo bills quantity. Let the count lead:
adding a teammate succeeds and pushes the new quantity onto the subscription
(Odoo prorates), rather than blocking a hire mid-sprint. Cap it well above
the paid seats so a runaway invite loop still can't happen.

## 7. Where the landing page lives

**In this repository, not in Odoo Website.**

Odoo's website builder is a WYSIWYG editing a page stored in the Odoo
database. That means no git history, no review, no CI, no ability for Claude
to iterate on a design in a branch, and a theme whose CSS fights any markup
pasted into it. It is a good tool for someone editing copy in a browser and a
bad target for designed, versioned, tested pages. On Online you also cannot
install a custom theme or override its assets, so the ceiling on how the page
can look is Odoo's, not yours.

Concretely:

- **Start here:** public routes in the existing frontend service — `/`,
  `/pricing`, and whatever else — rendered by the same React app, deployed by
  the same Railway service, with no new infrastructure and no extra build
  minutes per promotion. `frontend/nginx.conf` already falls back to
  `index.html` for arbitrary paths, so routing needs no change. Claude
  designs freely in components and CSS; the work is reviewable as a diff.
- **Split later, if needed:** if marketing wants to ship copy several times a
  week while the app ships fortnightly, move the marketing pages to their own
  static site and Railway service on the apex domain, with the app on
  `app.`. Do this when the release cadences actually diverge, not before —
  each Railway service rebuilds on every promotion, and build minutes are the
  scarce resource here (see `docs/railway.md` and the promotion rule in
  `CLAUDE.md`).
- **SEO caveat:** a client-rendered SPA route is weak for marketing. If
  organic search matters, prerender the public routes at build time to real
  HTML rather than relying on the SPA shell.

**Prices come from Odoo, not from the markup.** Add
`GET /api/v1/public/plans` (open route, cached for minutes, served from the
last successful Odoo read so an Odoo outage never blanks the pricing page).
It returns, per tier: the plan key, monthly and yearly prices per currency
from the Odoo pricelists, the tax rate and whether the price is tax-inclusive
(`price_include`), and the feature list. The pricing page renders that.

This is what makes the whole arrangement work: **Claude can redesign the
pricing page as often as it likes without ever touching a price, and you can
reprice in Odoo without a deploy.** The pricing table always shows both
ex-tax and inc-tax figures, clearly labelled, because the Odoo tax objects
that make that possible are right there in the payload.

**The CTA points at OpenV, not at Odoo.** Checkout needs a workspace to bill
(§2), so "Start free" goes to `/login` in register mode and paid tiers go to
`/login?next=/settings/billing?plan=team`, which lands a signed-in admin on
the upgrade action that creates the Odoo quotation. A signed-in visitor skips
straight there. Sending a stranger to an Odoo product page instead would
create an order with no workspace to attach it to, which is exactly the
reconciliation mess §4 avoids by keeping signup in OpenV.

## 8. Self-hosted and open source

Billing must be an **optional module, off by default**. With no
`ODOO_URL`/`BILLING_PROVIDER` configured, the reconcile job does not start,
the billing endpoints 404, and `Entitlements()` returns the free defaults
exactly as today — a local-first install has no phone-home and no licence
check. That constraint is not a nicety: a requirements tool that calls a
vendor server to decide what its owner may do is not one an engineering team
will adopt on their own hardware.

## 9. Security notes

- The Odoo API key is a Railway variable on the API service only. It never
  reaches the frontend, an agent run, or a runner.
- There is no inbound billing webhook and so no webhook secret to manage.
  The only billing endpoint a browser can reach is the refresh trigger, which
  reads from Odoo and grants nothing on its own (§5).
- The Odoo integration user is dedicated to the integration and scoped to
  Sales, so a leaked key cannot read HR or accounting data.
- Card data never touches OpenV; the checkout redirect keeps the whole
  platform out of PCI scope.
- `plan` and the billing refs are writable only by the reconcile path — not
  by `PUT /api/v1/orgs/{id}`, and not by an org admin. Otherwise the upgrade
  button is a `curl` command.
- The email address is the fallback join key; nothing else about a customer
  needs to be duplicated into OpenV.

## 10. What to record in the OpenV Platform project

Per `CLAUDE.md`, this changes what the platform does, so it lands in the live
project alongside the code — requirements in ISO/IEC/IEEE 29148 "shall" form,
linked to design items and test cases, with evidence recorded when
verification lands. The requirement set is roughly:

- the system shall mirror each workspace's subscription tier and status from
  the billing system of record;
- the system shall enforce plan limits at the point of resource creation;
- the system shall preserve existing artifacts when a workspace's plan is
  downgraded below its current usage;
- the system shall reconcile entitlements from the billing system of record
  on a recurring schedule;
- the system shall retain the last known entitlement when the billing system
  of record is unreachable;
- the system shall operate with all plan limits at their free-tier defaults
  when no billing backend is configured.

Lint the wording with `get_quality_findings` before saving, and baseline
after the set is coherent.

## 11. Suggested order of work

1. Public landing and pricing pages, static copy, no billing. Ships value
   immediately and is pure frontend.
2. Entitlement plumbing: schema, `PlanDefaults` extension, `Entitlements()`,
   server-side enforcement, status shown in workspace settings. Still no
   Odoo — operators set the tier by hand, as today.
3. Odoo catalogue and `GET /api/v1/public/plans`; the pricing page goes
   live-priced.
4. Checkout: partner creation, quotation, redirect, portal link.
5. Reconcile job on a ticker, plus the post-checkout refresh trigger.

Each step is useful on its own, and nothing before step 4 can charge anyone
by accident.
