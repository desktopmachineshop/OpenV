//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestTourS5cBilling is the S5c tour's billing area (refactor plan §6.4 S5c,
// before M9 and M14; M14's "S5c billing return URL", X12's seat counter and
// DefaultReturnURL after billing.Start (Q11); invariants I3, I4, I5, I8
// (Cache-Control, Retry-After), I13 (FRONTEND_URL, the billing variables);
// quirk Q18's family of shared buckets; OpenV REQ-18, REQ-143). Its golden is
// testdata/tour/s5c/billing.json. It takes the six billing routes: the public
// catalogue, and a workspace's billing read, refresh, checkout, plan change
// and portal.
//
// The server runs with billing on (a Stripe test key and a price map of Lite
// and Business by the month) and FRONTEND_URL written with a trailing slash,
// and no OPENV_BILLING_RETURN_URL, so the return URLs the provider is handed
// are FRONTEND_URL, trimmed, through DefaultReturnURL, which NewHandler calls
// after billing.Start (Q11). The recording proxy hands every CONNECT to
// api.stripe.com:443 to the area's stand-in (billingStripe): the tour's
// Stripe script (newTourStripe), which confirms both prices when the boot
// reconcile reads them (awaited before the first step, bootStandIns), makes
// customers, checkouts and portal sessions, and reads a checkout back open;
// grown here by what the purchase path does past a checkout: a checkout the
// area completes (its read then names a subscription, trialing when the
// checkout carried a trial), that subscription's read and its plan change in
// place, a checkout whose read the provider answers 503 every time, and a
// workspace whose checkouts and portal sessions it answers 503. Each
// request the server sent the provider is recorded under the step it came
// in: the form's fields sorted, the Authorization, Idempotency-Key and
// Content-Type headers.
//
// The area walks, in order: the public catalogue; the billing read (an
// admin, a plain member, a workspace that does not exist, no session, the
// personal workspace, a workspace on a granted plan); the refresh (no body,
// a malformed body, a checkout the provider does not know, which is answered
// as one, 404 with no Retry-After, and one whose read the provider answers
// 503, retried three times); the purchase path's refusals, each before any
// provider call (a plan or interval not for sale, a currency not offered, a
// malformed body, no subscription to change, a plain member, Business for a
// personal workspace, no customer for the portal); the purchase path on W,
// which the owner shares with a member and has invited a third person to
// (a Lite checkout that makes W's customer, whose return URLs M14 relies on;
// a Business checkout whose quantity is W's seats, members plus pending
// invitations, X12's countOrgSeats; the portal's return URL; binding an open
// checkout, which leaves W as it was, then the Lite checkout completed, which
// binds its subscription and records the buyer's trial; the re-read; another
// workspace naming W's checkout; the plan change to Business in place, again
// with W's seats as its quantity, and the nightly channel override the
// billing path writes when a workspace moves onto a channel-choosing plan;
// a second checkout of a subscribed workspace; a platform admin's grant over
// a live subscription, refused as that checkout is, 409 already_subscribed,
// with the domain's text); the owner's second checkout, with no trial left,
// and a currency its customer is not locked to; the provider failing writes
// on Y (a checkout, which carries an idempotency key, tried three times with
// the same key after the customer it made was kept; a portal session, which
// carries none, tried once); and last the two
// per-workspace buckets, drained on D: the refresh's, and the one checkout,
// plan change and portal share (Q18's family), which a refresh does not
// touch and another workspace's writes do not share (G, on a granted plan,
// whose checkout and change are refused as such); and, back on W, the seat
// sync (X12, Q11): W's pending invitation revoked, then its member removed,
// each pushed by the goroutine that billing.Start launches as the item's new
// quantity and followed by a read of the subscription, which W's billing then
// shows (stand_in_requests_outside_steps).
//
// Nondeterminism: ids and minted times (the generic tokens, the catalogue's
// as_of <time@boot>); a workspace slug's last 8 hex digits (the area's
// pattern); the five-minute bucket that ends a checkout's idempotency key
// (the area's pattern); the uuid that makes a plan change's idempotency key
// fresh per attempt (the generic token); two Retry-After countdowns (header
// patterns, the refresh bucket's on its own step only, so that
// billing_upstream's fixed 30 stays pinned). The Stripe client's jittered
// backoff before a retry changes only how long a step takes. Not pinned:
// the unconfirmed catalogue and the refusing provider, which S4b's billing
// profile pins (boot/billing.txt:
// public/plans with billing_enabled false, checkout 503 with the prices
// unconfirmed, the reconcile's nine refused CONNECTs), since a server
// confirms its prices once at boot and then only on the reconcile tick, five
// minutes later; billing_unavailable (404 with billing off: S4a's default
// profile); the checkout's workspace-billing feature gate (every tour
// workspace is on the nightly channel, and a stable one's gates change with
// the monthly cut); OPENV_BILLING_RETURN_URL's precedence over FRONTEND_URL
// and OPENV_BILLING_PORTAL_CONFIG (one server per area: internal/billing's
// unit tests); the reconcile tick (every five minutes, and
// OPENV_BILLING_RECONCILE_MINUTES takes whole minutes); a second live subscription for one workspace (two admins
// checking out at once), and a subscription whose workspace is gone; and a
// billing webhook, since there is none: the provider is reconciled, not
// heard from.
func TestTourS5cBilling(t *testing.T) {
	stripe := newBillingStripe(billingPrices)
	runTourArea(t, tourArea{
		slice: "s5c",
		key:   "billing",
		about: "Billing: the public catalogue, a workspace's billing read and refresh, checkout, plan change and portal " +
			"against a stand-in for Stripe, the return URLs FRONTEND_URL gives the provider (M14), the seats a Business " +
			"checkout bills (X12), and the two per-workspace buckets the billing writes spend (Q18's family).",
		run: func(tr *tour) { billingTour(tr, stripe) },
		env: map[string]string{
			"STRIPE_SECRET_KEY":   testStripeKey,
			"OPENV_STRIPE_PRICES": billingPrices,
			"FRONTEND_URL":        "https://app.tour.example/",
		},
		accounts: []tourAccount{{name: "member", display: "Tour Member",
			about: "a plain member of W, one of its three seats; refused every billing route"}},
		standIns: []*tourStandIn{stripe.standIn()},
		// The boot reconcile: open disputes, subscriptions, then each price.
		bootStandIns: map[string]int{"api.stripe.com:443": 2 + 2},
	})
}

// billingPrices is the area's price map: Lite and Business by the month.
// Lite's price is S4b's billing profile's (testPriceMap).
const billingPrices = `[{"price":"price_boot_harness_business_lite_month","plan":"business_lite","interval":"month"},` +
	`{"price":"price_tour_business_month","plan":"business","interval":"month"}]`

// billingOutageSession is a checkout session whose read the area's stand-in
// answers 503 every time: a provider in trouble.
const billingOutageSession = "cs_tour_outage"

// billingPeriodEnd is the end of every stand-in subscription's current
// period (2030-01-01T00:00:00Z).
const billingPeriodEnd = 1893456000

// billingStripe is the area's stand-in for Stripe: the tour's script
// (tourStripe), plus a checkout the area completes, the subscription it
// makes, that subscription's read and plan change, and outages: a checkout
// whose read always fails, and a workspace whose checkouts and portal
// sessions always fail.
type billingStripe struct {
	script    *tourStripe
	mu        sync.Mutex
	forms     map[string]url.Values           // checkout session id -> the form that made it
	done      map[string]string               // a completed checkout session -> its subscription
	subs      map[string]*billingSubscription // by id
	customers map[string]string               // customer id -> the workspace its metadata names
	down      map[string]bool                 // workspaces whose checkouts and portal sessions answer 503
}

// billingSubscription is a subscription the stand-in holds.
type billingSubscription struct {
	id, item, customer, price, status string
	quantity                          int
	metadata                          map[string]string
}

func newBillingStripe(prices string) *billingStripe {
	return &billingStripe{script: newTourStripe(prices), forms: map[string]url.Values{}, done: map[string]string{},
		subs: map[string]*billingSubscription{}, customers: map[string]string{}, down: map[string]bool{}}
}

func (s *billingStripe) standIn() *tourStandIn {
	return newTourStandIn("api.stripe.com:443", "Stripe's API: the tour's script, a completed checkout, its "+
		"subscription, and outages", s.answer)
}

// billingOutage is the stand-in's answer while it is having an outage.
func billingOutage() tourStandInAnswer {
	return standInJSON(http.StatusServiceUnavailable, map[string]any{"error": map[string]string{
		"type": "api_error", "message": "the tour's Stripe stand-in is having an outage"}})
}

// createdID is the id of the object a script's answer made.
func (s *billingStripe) createdID(ans tourStandInAnswer) string {
	var made struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(ans.body, &made)
	return made.ID
}

func (s *billingStripe) answer(r *tourStandInRequest) tourStandInAnswer {
	s.mu.Lock()
	defer s.mu.Unlock()
	const sessions, subscription = "/v1/checkout/sessions", "/v1/subscriptions/"
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.Path, sessions+"/"):
		id := strings.TrimPrefix(r.Path, sessions+"/")
		if id == billingOutageSession {
			return billingOutage()
		}
		if sub, ok := s.done[id]; ok {
			form := s.forms[id]
			return standInJSON(http.StatusOK, map[string]any{"id": id, "object": "checkout.session",
				"client_reference_id": form.Get("client_reference_id"), "customer": form.Get("customer"),
				"subscription": sub, "status": "complete", "metadata": billingFormMap(form, "metadata[")})
		}
	case r.Method == http.MethodPost && r.Path == "/v1/customers":
		ans := s.script.answer(r)
		if id := s.createdID(ans); id != "" {
			s.customers[id] = r.Form.Get("metadata[openv_org_id]")
		}
		return ans
	case r.Method == http.MethodPost && r.Path == sessions:
		if s.down[r.Form.Get("client_reference_id")] {
			return billingOutage()
		}
		ans := s.script.answer(r)
		if id := s.createdID(ans); id != "" {
			s.forms[id] = r.Form
		}
		return ans
	case r.Method == http.MethodPost && r.Path == "/v1/billing_portal/sessions":
		if s.down[s.customers[r.Form.Get("customer")]] {
			return billingOutage()
		}
	case r.Method == http.MethodPost && strings.HasPrefix(r.Path, "/v1/subscription_items/"):
		// The seat sync: an item's quantity set in place.
		item := strings.TrimPrefix(r.Path, "/v1/subscription_items/")
		for _, sub := range s.subs {
			if sub.item == item {
				if q, err := strconv.Atoi(r.Form.Get("quantity")); err == nil {
					sub.quantity = q
				}
				return standInJSON(http.StatusOK, map[string]any{"id": item, "object": "subscription_item",
					"quantity": sub.quantity})
			}
		}
		return stripeError(http.StatusNotFound, "resource_missing", "No such subscription_item")
	case strings.HasPrefix(r.Path, subscription):
		sub := s.subs[strings.TrimPrefix(r.Path, subscription)]
		if sub == nil {
			return stripeError(http.StatusNotFound, "resource_missing", "No such subscription")
		}
		switch r.Method {
		case http.MethodPost: // a plan change in place: the item's price and quantity
			if p := r.Form.Get("items[0][price]"); p != "" {
				sub.price = p
			}
			if q, err := strconv.Atoi(r.Form.Get("items[0][quantity]")); err == nil {
				sub.quantity = q
			}
			return standInJSON(http.StatusOK, sub.render())
		case http.MethodGet:
			return standInJSON(http.StatusOK, sub.render())
		}
	}
	return s.script.answer(r)
}

// complete makes a checkout session the stand-in created complete: its read
// now names a subscription for the price and quantity it was made for, with
// its subscription metadata, trialing when it carried a trial.
func (s *billingStripe) complete(tr *tour, session string) {
	tr.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	form := s.forms[session]
	if form == nil {
		tr.t.Fatalf("the stand-in made no checkout session %s", session)
	}
	n := len(s.subs) + 1
	sub := &billingSubscription{id: fmt.Sprintf("sub_tour_%d", n), item: fmt.Sprintf("si_tour_%d", n),
		customer: form.Get("customer"), price: form.Get("line_items[0][price]"), status: "active",
		metadata: billingFormMap(form, "subscription_data[metadata][")}
	sub.quantity, _ = strconv.Atoi(form.Get("line_items[0][quantity]"))
	if form.Get("subscription_data[trial_period_days]") != "" {
		sub.status = "trialing"
	}
	s.subs[sub.id] = sub
	s.done[session] = sub.id
}

// fail makes the stand-in answer 503 to every checkout and portal session
// for a workspace (filled) from now on.
func (s *billingStripe) fail(tr *tour, workspace string) {
	tr.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down[tr.fill(workspace)] = true
}

func (b *billingSubscription) render() map[string]any {
	return map[string]any{"id": b.id, "object": "subscription", "customer": b.customer, "status": b.status,
		"cancel_at_period_end": false, "currency": "gbp", "metadata": b.metadata,
		"items": map[string]any{"object": "list", "has_more": false, "data": []any{map[string]any{
			"id": b.item, "object": "subscription_item", "quantity": b.quantity,
			"current_period_end": billingPeriodEnd, "price": map[string]any{"id": b.price, "object": "price"}}}}}
}

// billingFormMap is the fields of a form under a bracketed prefix
// ("metadata[" for metadata[openv_org_id]), keyed by the name inside.
func billingFormMap(form url.Values, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range form {
		if inner, ok := strings.CutPrefix(k, prefix); ok && strings.HasSuffix(inner, "]") && len(v) > 0 {
			out[strings.TrimSuffix(inner, "]")] = v[0]
		}
	}
	return out
}

// billingPlan is a checkout's or plan change's body.
func billingPlan(plan, interval string) tourOpt {
	return jsonBody(fmt.Sprintf(`{"plan":%q,"interval":%q}`, plan, interval))
}

func billingTour(tr *tour, stripe *billingStripe) {
	o, anon, m := tr.owner, tr.anon, tr.actor("member")
	const (
		read     = "GET /api/v1/orgs/{id}/billing"
		refresh  = "POST /api/v1/orgs/{id}/billing/refresh"
		checkout = "POST /api/v1/orgs/{id}/billing/checkout"
		change   = "POST /api/v1/orgs/{id}/billing/change"
		portal   = "POST /api/v1/orgs/{id}/billing/portal"
	)
	tr.slugPattern()
	tr.keep("2030-01-01T00:00:00Z", "the end of every stand-in subscription's current period (billingPeriodEnd), "+
		"which the provider reports and a workspace's billing echoes as period_end")
	tr.pattern(`openv:co:[^"\s]*:(\d+)`, "<unix/300>", "the five-minute bucket a checkout's idempotency key ends in "+
		"(the server's clock in Unix seconds, divided by 300), so that the same checkout retried within it is one "+
		"checkout at the provider")
	tr.headerPattern("Retry-After", `^(1[67][0-9]|180)$`, "<retry-after 180 s>", "Retry-After on the sixth billing "+
		"write (checkout, plan change or portal) to one workspace: ceil(180 s less the seconds since the first of "+
		"the six, the write bucket's refill of 20 an hour), so 180 while they take under a second and no less "+
		"than 160 within twenty")

	// The fixtures: W, which the owner shares with the member and has invited
	// a third person to, so its seats are three; X for refusals; Y for a
	// provider that fails writes; D for the buckets; G on a plan a platform
	// admin granted. Each workspace's write bucket (5) is kept in mind: no
	// step before D's drain is a sixth write to one workspace.
	tr.sharedWorkspace("w", "Tour Billing W")
	tr.join(m, "{{w}}", "member")
	tr.setup("invite a third seat to W", o, "POST /api/v1/orgs/{id}/invitations", at("id", "{{w}}"),
		jsonBody(`{"email":"tour-billing-invitee@example.com","role":"member"}`),
		expect(http.StatusAccepted)).capture("w.invitation", "/invitation/id")
	tr.sharedWorkspace("x", "Tour Billing X")
	tr.sharedWorkspace("y", "Tour Billing Y")
	tr.sharedWorkspace("d", "Tour Billing D")
	tr.sharedWorkspace("g", "Tour Billing G")
	tr.setup("a platform admin grants G the open_source plan", tr.admin, "PUT /api/v1/orgs/{id}/plan", at("id", "{{g}}"),
		jsonBody(`{"plan":"open_source"}`))

	// The catalogue: served from what the boot reconcile confirmed, never
	// from a provider call.
	tr.step("the pricing catalogue: the prices the provider confirmed at boot, per currency, cached publicly", anon,
		"GET /api/v1/public/plans")

	// The billing read: admins only, and no provider call.
	tr.actIn(o, "{{w}}")
	tr.step("W's billing, as its admin: no subscription yet, and the catalogue", o, read, at("id", "{{w}}"))
	tr.step("W's billing, as a plain member: admins only", m, read, at("id", "{{w}}"))
	tr.step("the billing of a workspace that does not exist", o, read, at("id", "{{phantom}}"))
	tr.step("W's billing with no session: the middleware's 401", anon, read, at("id", "{{w}}"))
	tr.step("the owner's personal workspace's billing", o, read, at("id", "{{owner.workspace}}"),
		actingIn("{{owner.workspace}}"))
	tr.step("G's billing: the open_source plan a platform admin granted, nothing to buy", o, read, at("id", "{{g}}"),
		actingIn("{{g}}"))

	// The refresh, on X: its own bucket (10), which none of this spends to the
	// end.
	tr.actIn(o, "{{x}}")
	tr.step("refresh X with no body: X holds no subscription, so there is nothing to re-read and no provider call", o,
		refresh, at("id", "{{x}}"))
	tr.step("refresh X with a malformed body", o, refresh, at("id", "{{x}}"), jsonBody(`{"session_id":`))
	tr.step("refresh X from a checkout the provider does not know: its 404 is answered 404, with no Retry-After, "+
		"since the provider answered", o, refresh, at("id", "{{x}}"), jsonBody(`{"session_id":"cs_tour_missing"}`))
	tr.step("refresh X from a checkout whose read the provider answers 503: read three times, then 503 with "+
		"Retry-After", o, refresh, at("id", "{{x}}"), jsonBody(`{"session_id":"`+billingOutageSession+`"}`),
		note("the Stripe client retries a read the provider answered 5xx, three attempts in all, after a jittered "+
			"backoff that changes only how long the step takes"))
	tr.step("refresh W as a plain member: admins only", m, refresh, at("id", "{{w}}"))

	// The purchase path's refusals. Each spends a token of the workspace's
	// write bucket (5) once the caller is its admin, and none reaches the
	// provider.
	tr.step("check out X for Business by the year: no price is registered for it", o, checkout, at("id", "{{x}}"),
		billingPlan("business", "year"))
	tr.step("check out X for Lite in yen: a currency the price is not offered in", o, checkout, at("id", "{{x}}"),
		jsonBody(`{"plan":"business_lite","interval":"month","currency":" JPY "}`),
		note("the currency is trimmed and lower-cased first"))
	tr.step("check out X with a malformed body: read after the bucket", o, checkout, at("id", "{{x}}"),
		jsonBody(`{"plan":`))
	tr.step("change X to a plan that is not for sale", o, change, at("id", "{{x}}"), billingPlan("enterprise", "month"))
	tr.step("change X to Lite: X holds no subscription", o, change, at("id", "{{x}}"),
		billingPlan("business_lite", "month"))
	tr.step("check out W as a plain member: admins only", m, checkout, at("id", "{{w}}"),
		billingPlan("business_lite", "month"))
	tr.actIn(o, "{{owner.workspace}}")
	tr.step("check out the personal workspace for Business: Business is for a shared workspace", o, checkout,
		at("id", "{{owner.workspace}}"), billingPlan("business", "month"))
	tr.step("change the personal workspace to Business: the same refusal, before the missing subscription", o, change,
		at("id", "{{owner.workspace}}"), billingPlan("business", "month"))
	tr.step("the personal workspace's portal: it has no customer yet", o, portal, at("id", "{{owner.workspace}}"))

	// The purchase path on W (M14's return URLs, X12's seats).
	tr.actIn(o, "{{w}}")
	tr.step("check out W for Lite: W's customer is made, then the checkout, returning to FRONTEND_URL trimmed (M14), "+
		"for one seat, with the buyer's trial", o, checkout, at("id", "{{w}}"), billingPlan("business_lite", "month"))
	tr.step("check out W for Business: the same customer, and a quantity of W's seats, its two members and the "+
		"pending invitation (X12)", o, checkout, at("id", "{{w}}"), billingPlan("business", "month"))
	tr.step("W's portal: the customer's, returning to the billing tab", o, portal, at("id", "{{w}}"))
	tr.step("refresh W from the Business checkout, still open at the provider: W is left as it was", o, refresh,
		at("id", "{{w}}"), jsonBody(`{"session_id":"cs_tour_2"}`))
	stripe.complete(tr, "cs_tour_1")
	tr.step("refresh W from the Lite checkout, now complete: its subscription is bound to W and applied, and the "+
		"buyer's trial recorded", o, refresh, at("id", "{{w}}"), jsonBody(`{"session_id":"cs_tour_1"}`),
		note("before this step the stand-in completed the Lite checkout: its read names sub_tour_1, trialing, for "+
			"one seat"))
	tr.step("W's billing: the subscription as applied, read without a provider call", o, read, at("id", "{{w}}"))
	tr.step("refresh W with no body: the subscription is read again and applied", o, refresh, at("id", "{{w}}"))
	tr.step("refresh X from W's checkout: it names another workspace", o, refresh, at("id", "{{x}}"),
		actingIn("{{x}}"), jsonBody(`{"session_id":"cs_tour_1"}`))
	tr.step("change W to Business: the subscription's item moves in place, for W's three seats, and is read again", o,
		change, at("id", "{{w}}"), billingPlan("business", "month"))
	tr.step("W as a workspace: on Business, and on the nightly channel the billing path wrote as its override when "+
		"W moved onto a plan that chooses its channel", o, "GET /api/v1/orgs/{id}", at("id", "{{w}}"))
	tr.step("check out W again: it holds a live subscription", o, checkout, at("id", "{{w}}"),
		billingPlan("business_lite", "month"),
		note("W's fifth billing write: the Lite and Business checkouts, the portal and the plan change went before it"))
	tr.step("a platform admin grants W open_source while its subscription is live: 409 already_subscribed, as W's "+
		"checkout was", tr.admin,
		"PUT /api/v1/orgs/{id}/plan", at("id", "{{w}}"), jsonBody(`{"plan":"open_source"}`))

	// The owner's trial went with W's bound subscription; the personal
	// workspace's customer pays in the currency of its first checkout.
	tr.actIn(o, "{{owner.workspace}}")
	tr.step("check out the personal workspace for Lite: a customer of its own, and no trial, since the owner's was "+
		"recorded when W's checkout was bound", o, checkout, at("id", "{{owner.workspace}}"),
		billingPlan("business_lite", "month"))
	tr.step("check out the personal workspace in dollars: its customer pays in pounds", o, checkout,
		at("id", "{{owner.workspace}}"), jsonBody(`{"plan":"business_lite","interval":"month","currency":"usd"}`),
		note("the personal workspace's fifth billing write"))

	// The provider failing a write, on Y: a checkout carries an idempotency
	// key, so it is tried again; a portal session carries none, so it is not.
	tr.actIn(o, "{{y}}")
	stripe.fail(tr, "{{y}}")
	tr.step("check out Y while the provider fails every checkout: Y's customer is made and kept, and the checkout, "+
		"keyed, is tried three times with the same key, then 503", o, checkout, at("id", "{{y}}"),
		billingPlan("business_lite", "month"),
		note("from this step on, the stand-in answers 503 to every checkout and portal session for Y"))
	tr.step("Y's portal, for the customer the failed checkout kept: a portal session is unkeyed, so it is tried "+
		"once, then 503", o, portal, at("id", "{{y}}"))

	// The buckets, on D: the refresh's (10, one back every 30 s), then the one
	// checkout, plan change and portal share (5, one back every 180 s), which
	// the refreshes do not touch (Q18's family: one bucket, several routes).
	tr.actIn(o, "{{d}}")
	for i := 1; i < 10; i++ {
		tr.probe(o, refresh, at("id", "{{d}}"))
	}
	tr.step("the tenth refresh of D: the last its bucket allows", o, refresh, at("id", "{{d}}"),
		note("nine refreshes went before it, as probes"))
	tr.step("the eleventh: refused", o, refresh, at("id", "{{d}}"), headerPatternHere("Retry-After", `^(2[0-9]|30)$`,
		"<retry-after 30 s>", "Retry-After on the eleventh refresh of one workspace: ceil(30 s less the seconds "+
			"since the first of the eleven, the refresh bucket's refill of 120 an hour), so 30 while they take under "+
			"a second and no less than 20 within ten; billing_upstream's fixed Retry-After of 30 on the other steps "+
			"stays as it is"))
	tr.probe(o, checkout, at("id", "{{d}}"), billingPlan("business", "year"))
	tr.probe(o, change, at("id", "{{d}}"), billingPlan("business", "year"))
	tr.probe(o, portal, at("id", "{{d}}"))
	tr.probe(o, checkout, at("id", "{{d}}"), billingPlan("business", "year"))
	tr.step("D's portal, its fifth billing write: the last the bucket allows, which the refreshes left alone", o,
		portal, at("id", "{{d}}"), note("a checkout, a plan change, a portal and a checkout went before it, as probes"))
	tr.step("check out D: refused, the bucket the portal and the plan change spent", o, checkout, at("id", "{{d}}"),
		billingPlan("business_lite", "month"))
	tr.step("change D: refused by the same bucket", o, change, at("id", "{{d}}"), billingPlan("business_lite", "month"))

	// G: another workspace's bucket, untouched by D's.
	tr.actIn(o, "{{g}}")
	tr.step("check out G, right after D's bucket was spent: G's own bucket, and a granted plan has nothing to buy", o,
		checkout, at("id", "{{g}}"), billingPlan("business_lite", "month"))
	tr.step("change G to Business: the same refusal", o, change, at("id", "{{g}}"), billingPlan("business", "month"))
	tr.step("G's portal: no customer", o, portal, at("id", "{{g}}"))

	// The seat sync (X12, Q11): a membership change of W, on a live Business
	// subscription, is pushed to the provider by the goroutine billing.Start
	// launches (every two seconds), which sets the item's quantity to the
	// seats SetSeatCounter counts and reads the subscription again. Each
	// change is a setup and each wait a probe, so the push and the re-read
	// are recorded outside the steps, before the read that shows them.
	tr.actIn(o, "{{w}}")
	seats := func(n int) func(*tourResult) bool {
		return func(r *tourResult) bool {
			return r.status == http.StatusOK && strings.Contains(string(r.body), fmt.Sprintf(`"seats":%d`, n))
		}
	}
	tr.setup("revoke W's pending invitation", o, "DELETE /api/v1/orgs/{id}/invitations/{invId}",
		at("id", "{{w}}", "invId", "{{w.invitation}}"), expect(http.StatusNoContent))
	tr.await("W's billed seats pushed and read back", o, read, seats(2), at("id", "{{w}}"))
	tr.step("W's billing after its pending invitation was revoked: the seat sync pushed two seats and read the "+
		"subscription again", o, read, at("id", "{{w}}"), note("the invitation was revoked just before (a setup)"))
	tr.setup("remove the member from W", o, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{w}}", "userId", "{{member}}"), expect(http.StatusNoContent))
	tr.await("W's billed seat pushed and read back", o, read, seats(1), at("id", "{{w}}"))
	tr.step("W's billing after the member left: the seat sync pushed one seat and read the subscription again", o,
		read, at("id", "{{w}}"), note("the member was removed just before (a setup)"))
}
