# Subscriptions, signup, and membership tier — with Odoo as the billing system

How OpenV can sell and enforce paid workspaces without growing a billing
system of its own: **Odoo owns the commercial record, OpenV owns identity and
entitlements, and the marketing site stays in this repository** so it can be
designed, reviewed, and deployed like the rest of the product.

This is a design proposal, not shipped behaviour. Nothing here is implemented
yet; `orgs.Plan` is still set by hand in the database
(`internal/domain/orgs/limits.go`) and the workspace settings screen says
"billing coming soon" (`frontend/src/views/OrgSettings.tsx`).

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

- On the Odoo side: a custom field `x_openv_org_id` on `res.partner` and on
  the subscription order, holding the OpenV workspace UUID. Studio can add
  it; a tiny custom module is cleaner if you have Odoo.sh or self-host. If
  you want to avoid both, `res.partner.ref` works as a stopgap.
- On the OpenV side: `orgs.billing_customer_ref` (Odoo partner id) and
  `orgs.billing_subscription_ref` (Odoo order/subscription id).

Personal workspaces get a partner lazily, only when someone first upgrades —
free users never touch Odoo, so the CRM does not fill with accounts that will
never buy anything.

## 3. Odoo setup

One product per tier, sold as a recurring product:

- `product.template` per tier ("OpenV Team", "OpenV Business"), `Sales OK`,
  recurring/subscription enabled, priced per seat per month and per year.
- A subscription plan/recurrence per billing period (monthly, yearly).
- A pricelist per selling currency; the tax on the product decides whether
  the stored `list_price` is net or gross (`price_include`), which matters
  for the pricing page — see §7.
- A payment provider in live mode, and the customer portal enabled so
  subscribers manage their own payment method and cancellation.

**Version note.** Odoo 17 rebuilt Subscriptions on top of `sale.order`
(`is_subscription`, `subscription_state`, `sale.subscription.plan`); Odoo 16
and earlier used a separate `sale.subscription` model. The mapping in §5
assumes the newer shape. Confirm the field names on your instance before
coding against them — the read-only `odoo-product-info` skill can introspect
(`odoo_query.py fields sale.order`) without any new access.

Credentials follow the pattern that skill already establishes: `ODOO_URL`,
`ODOO_DB`, `ODOO_USER`, `ODOO_API_KEY`, from the environment, never
committed. In Railway they belong in the API service's variables.

**Do not extend `odoo-product-info` to write.** It is read-only by design and
other skills depend on that. Creating partners and quotations is a separate,
explicitly-scoped path inside the API service (`internal/billing/odoo`).

## 4. Signup and upgrade flows

Signup stays in OpenV. Payment happens in Odoo. The two meet at a checkout
redirect and at a mirrored entitlement.

**Free signup (unchanged):** landing page → `/login` in register mode →
`POST /api/v1/auth/register` → `provisionPersonalWorkspace` creates the
personal workspace, plan `free`, seeded agents. Odoo is not involved.

**Upgrade (new):**

1. An org admin picks a tier in workspace settings.
2. `POST /api/v1/orgs/{id}/billing/checkout {plan, period, seats}` — OpenV
   ensures an Odoo partner exists for the workspace (creating it with
   `x_openv_org_id` set), creates a subscription quotation for the tier
   product with `seats` as the quantity, and returns the Odoo payment link.
3. The browser is redirected to Odoo. Card details never reach OpenV.
4. Odoo confirms the order on payment; the subscription starts running.
5. OpenV learns about it (§5) and the entitlement goes live — typically
   within seconds via push, within minutes via the reconcile job regardless.

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

**Pull is the backbone; push is an accelerator.**

A periodic reconcile job in the API service — the same shape as the workspace
purge job in `cmd/server/main.go`, a goroutine with a ticker — reads every
active subscription out of Odoo (one `search_read` over orders with
`x_openv_org_id` set) and writes the resulting tier/status/seats onto the
matching workspaces. Run it every few minutes, and on boot.

This ordering is deliberate. Odoo core has no first-class *outgoing* webhook,
and the automated-action Python sandbox on Odoo Online does not generally
give you an HTTP client, so a push channel may require a custom module —
easy on Odoo.sh or self-hosted, awkward on Odoo Online. More importantly, a
push-only integration is silently wrong the first time a delivery is missed:
a customer pays and stays locked out, or churns and keeps their seats. A
reconcile loop is self-healing and can be re-run at any time to prove state.

If you can build push, add it as latency reduction only: an automated action
on subscription state change POSTs `{org_id, subscription_id}` to
`POST /api/v1/public/billing/odoo`, which authenticates an HMAC over the body
with a shared secret and a timestamp for replay protection, and then does
nothing but **trigger a pull for that one workspace**. The webhook body is a
hint, never the source of truth — that way a forged or replayed webhook can
at worst cause a redundant read from Odoo, and can never grant a tier.

Suggested state mapping (verify the selection values on your version):

| Odoo subscription state | OpenV `plan` | OpenV `plan_status` |
|---|---|---|
| draft / quotation sent | `free` | `none` |
| in progress, paid | tier product's plan | `active` |
| in progress, invoice overdue | tier product's plan | `past_due` |
| paused | tier product's plan | `paused` |
| closed / churned | `free` | `canceled` |

Usage flows the other way. `GET /api/v1/orgs/{id}/usage` already rolls up
runs, tokens and cost per workspace; a monthly job can post those quantities
onto the subscription as a usage-based line if you ever meter agent spend
rather than just capping it. The `monthly_budget_usd` field already on `Org`
is the natural soft guard-rail alongside it.

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
bad target for designed, versioned, tested pages.

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

The CTA on each tier is a link into the Odoo checkout for that product,
carrying the workspace id when the visitor is already signed in.

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
- The webhook secret is separate from `WORKER_API_KEY` and from any org key,
  and the webhook grants nothing on its own (§5).
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
- the system shall reconcile entitlements on a recurring schedule
  independently of webhook delivery;
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
5. Reconcile job, then the push webhook if the Odoo edition allows it.

Each step is useful on its own, and nothing before step 4 can charge anyone
by accident.
