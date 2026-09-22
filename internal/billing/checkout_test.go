package billing

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// The purchase path decides every provider value itself, refuses what
// cannot be sold, and binds a completed checkout only to the workspace it
// names.

type purchaseProvider struct {
	*fakeProvider
	customers  []string
	checkouts  []CheckoutRequest
	sessions   map[string]*CheckoutSession
	portals    []string
	changes    []string
	sessionErr error
}

func (p *purchaseProvider) CreateCustomer(_ context.Context, name, email string, meta map[string]string, key string) (string, error) {
	p.customers = append(p.customers, name+"|"+email+"|"+meta[OrgMetadataKey]+"|"+key)
	return "cus_new", nil
}

func (p *purchaseProvider) CreateCheckoutSession(_ context.Context, req CheckoutRequest) (*CheckoutSession, error) {
	p.checkouts = append(p.checkouts, req)
	return &CheckoutSession{ID: "cs_1", URL: "https://checkout.example/cs_1", ClientReferenceID: req.ClientReferenceID}, nil
}

func (p *purchaseProvider) GetCheckoutSession(_ context.Context, id string) (*CheckoutSession, error) {
	if p.sessionErr != nil {
		return nil, p.sessionErr
	}
	s, ok := p.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (p *purchaseProvider) CreatePortalSession(_ context.Context, customerID, returnURL, cfg string) (string, error) {
	p.portals = append(p.portals, customerID+"|"+returnURL+"|"+cfg)
	return "https://portal.example/" + customerID, nil
}

func (p *purchaseProvider) UpdateSubscriptionItem(_ context.Context, subID, itemID, priceID string, qty int) error {
	p.changes = append(p.changes, subID+"|"+itemID+"|"+priceID+"|"+itoa(qty))
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

type trialUsers struct{ marked []string }

func (u *trialUsers) MarkBillingTrialUsed(id string) error {
	u.marked = append(u.marked, id)
	return nil
}

func (f *fakeOrgs) SetBillingCustomer(orgID, ref, currency string) error {
	o, ok := f.orgs[orgID]
	if !ok {
		return orgs.ErrNotFound
	}
	o.Billing.CustomerRef, o.Billing.Currency = ref, currency
	return nil
}

func confirmedPrices() map[string]*Price {
	return map[string]*Price{
		"price_bm": {ID: "price_bm", Interval: IntervalMonth, UsageType: "licensed", Amounts: map[string]int64{"gbp": 1200, "usd": 1500}},
		"price_lm": {ID: "price_lm", Interval: IntervalMonth, UsageType: "licensed", Amounts: map[string]int64{"gbp": 900}},
	}
}

func newPurchaseService(t *testing.T, o *fakeOrgs) (*Service, *purchaseProvider, *trialUsers) {
	t.Helper()
	p := &purchaseProvider{fakeProvider: &fakeProvider{prices: confirmedPrices()}, sessions: map[string]*CheckoutSession{}}
	s, _ := newTestService(p.fakeProvider, o)
	s.provider = p
	if err := s.RefreshPrices(context.Background()); err != nil {
		t.Fatal(err)
	}
	u := &trialUsers{}
	s.SetUsers(u)
	s.SetReturnURL("https://app.example/")
	s.SetPortalConfig("bpc_1")
	s.SetSeatCounter(func(orgID string) (int, error) { return 7, nil })
	return s, p, u
}

var buyer = Buyer{ID: "u1", Email: "dana@example.com", Name: "Dana"}

func TestCheckoutDecidesEverythingServerSide(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", Name: "Acme", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle}}}
	s, p, _ := newPurchaseService(t, o)

	url, err := s.Checkout(context.Background(), o.orgs["o1"], buyer, orgs.PlanBusiness, IntervalMonth, "")
	if err != nil || url != "https://checkout.example/cs_1" {
		t.Fatalf("checkout: %q %v", url, err)
	}
	// A customer was made once, named for the workspace and keyed so a
	// retry cannot make a second, and the currency is locked to GBP — the
	// default where offered.
	if len(p.customers) != 1 || p.customers[0] != "Acme|dana@example.com|o1|openv:cust:o1:v1" {
		t.Fatalf("customers = %v", p.customers)
	}
	if o.orgs["o1"].Billing.CustomerRef != "cus_new" || o.orgs["o1"].Billing.Currency != "gbp" {
		t.Fatalf("customer not recorded: %+v", o.orgs["o1"].Billing)
	}
	req := p.checkouts[0]
	if req.CustomerID != "cus_new" || req.PriceID != "price_bm" || req.Quantity != 7 || req.Currency != "gbp" ||
		req.TrialDays != DefaultTrialDays || req.ClientReferenceID != "o1" ||
		req.Metadata[OrgMetadataKey] != "o1" || req.Metadata[PlanMetadataKey] != orgs.PlanBusiness || req.Metadata[TrialBuyerMetadataKey] != "u1" {
		t.Fatalf("request = %+v", req)
	}
	if !strings.HasPrefix(req.SuccessURL, "https://app.example/org/settings?tab=billing&checkout=done&session_id=") ||
		!strings.Contains(req.CancelURL, "checkout=cancelled") {
		t.Fatalf("urls = %q %q", req.SuccessURL, req.CancelURL)
	}
	if !strings.HasPrefix(req.IdempotencyKey, "openv:co:o1:business:month:gbp:") {
		t.Fatalf("key = %q", req.IdempotencyKey)
	}

	// Lite is one seat whatever the member count, and a spent trial buys none.
	lite := &orgs.Org{ID: "o2", Name: "Solo", OrgType: orgs.TypePersonal, BilledPlan: orgs.PlanSingle}
	o.orgs["o2"] = lite
	if _, err := s.Checkout(context.Background(), lite, Buyer{ID: "u2", TrialUsed: true}, orgs.PlanBusinessLite, IntervalMonth, "gbp"); err != nil {
		t.Fatal(err)
	}
	req = p.checkouts[1]
	if req.Quantity != 1 || req.TrialDays != 0 || req.Metadata[TrialBuyerMetadataKey] != "" {
		t.Fatalf("lite request = %+v", req)
	}
}

func TestCheckoutRefusesWhatCannotBeSold(t *testing.T) {
	personal := &orgs.Org{ID: "p", OrgType: orgs.TypePersonal, BilledPlan: orgs.PlanSingle}
	live := &orgs.Org{ID: "l", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanBusiness, Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1"}}
	granted := &orgs.Org{ID: "g", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanEnterprise}
	locked := &orgs.Org{ID: "k", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle, Billing: orgs.Billing{CustomerRef: "cus_k", Currency: "usd"}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"p": personal, "l": live, "g": granted, "k": locked}}
	s, p, _ := newPurchaseService(t, o)
	ctx := context.Background()

	cases := []struct {
		name                     string
		org                      *orgs.Org
		plan, interval, currency string
		want                     error
	}{
		{"unknown plan", personal, orgs.PlanEnterprise, IntervalMonth, "", ErrUnknownPlan},
		{"unknown interval", personal, orgs.PlanBusiness, "week", "", ErrUnknownPlan},
		{"business on a personal workspace", personal, orgs.PlanBusiness, IntervalMonth, "", ErrPersonalWorkspace},
		{"already subscribed", live, orgs.PlanBusiness, IntervalMonth, "", ErrAlreadySubscribed},
		{"granted plan", granted, orgs.PlanBusiness, IntervalMonth, "", ErrGrantedPlan},
		{"currency not offered", personal, orgs.PlanBusinessLite, IntervalMonth, "eur", ErrUnknownCurrency},
		{"currency locked", locked, orgs.PlanBusiness, IntervalMonth, "gbp", ErrCurrencyLocked},
	}
	for _, tc := range cases {
		if _, err := s.Checkout(ctx, tc.org, buyer, tc.plan, tc.interval, tc.currency); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(p.checkouts) != 0 || len(p.customers) != 0 {
		t.Fatalf("a refusal reached the provider: %v %v", p.checkouts, p.customers)
	}
	// The locked customer's currency is used when none is asked for.
	if _, err := s.Checkout(ctx, locked, buyer, orgs.PlanBusiness, IntervalMonth, ""); err != nil {
		t.Fatal(err)
	}
	if p.checkouts[0].Currency != "usd" || p.checkouts[0].CustomerID != "cus_k" {
		t.Fatalf("locked checkout = %+v", p.checkouts[0])
	}
	// Unconfirmed prices refuse rather than sell blind, and no provider off.
	unconfirmed, _ := newTestService(&fakeProvider{}, o)
	if _, err := unconfirmed.Checkout(ctx, personal, buyer, orgs.PlanBusinessLite, IntervalMonth, ""); !errors.Is(err, ErrPricesUnconfirmed) {
		t.Errorf("unconfirmed: %v", err)
	}
	off := New(nil, o, nil, nil)
	if _, err := off.Checkout(ctx, personal, buyer, orgs.PlanBusinessLite, IntervalMonth, ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("off: %v", err)
	}
}

func TestBindCheckoutSessionAppliesAndRecordsTheTrial(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle, Billing: orgs.Billing{CustomerRef: "cus_1", Currency: "gbp"}}}}
	s, p, u := newPurchaseService(t, o)
	p.subs = []*Subscription{business("sub_1", "o1", "trialing")}
	p.sessions["cs_done"] = &CheckoutSession{ID: "cs_done", ClientReferenceID: "o1", SubscriptionID: "sub_1", Status: "complete",
		Metadata: map[string]string{TrialBuyerMetadataKey: "u1"}}
	p.sessions["cs_open"] = &CheckoutSession{ID: "cs_open", ClientReferenceID: "o1", Status: "open"}
	p.sessions["cs_theirs"] = &CheckoutSession{ID: "cs_theirs", ClientReferenceID: "o9", SubscriptionID: "sub_9"}

	// Not complete yet: nothing changes, no error.
	got, err := s.BindCheckoutSession(context.Background(), "o1", "cs_open")
	if err != nil || got.Billing.Status != "" {
		t.Fatalf("open session: %v %+v", err, got.Billing)
	}
	// Another workspace's session: refused, nothing changes.
	if _, err := s.BindCheckoutSession(context.Background(), "o1", "cs_theirs"); !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
	// Complete: applied, entitled at once, trial recorded on the buyer.
	got, err = s.BindCheckoutSession(context.Background(), "o1", "cs_done")
	if err != nil || got.BilledPlan != orgs.PlanBusiness || got.Billing.Status != orgs.PlanStatusTrialing || got.Billing.SubscriptionRef != "sub_1" {
		t.Fatalf("bound: %v plan=%q %+v", err, got.BilledPlan, got.Billing)
	}
	if got.EntitledPlan() != orgs.PlanBusiness {
		t.Fatal("not entitled after bind")
	}
	if len(u.marked) != 1 || u.marked[0] != "u1" {
		t.Fatalf("trial marked = %v", u.marked)
	}
	// A provider failure on the read leaves the workspace as it was.
	p.sessionErr = errors.New("timeout")
	before := *o.orgs["o1"]
	if _, err := s.BindCheckoutSession(context.Background(), "o1", "cs_done"); err == nil {
		t.Fatal("a failed read should be reported")
	}
	if o.orgs["o1"].Billing != before.Billing {
		t.Fatal("a failed read changed the workspace")
	}
}

func TestASecondCompletedCheckoutLosesToTheHeldSubscription(t *testing.T) {
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": {ID: "o1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanBusiness,
		Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1", CustomerRef: "cus_1"}}}}
	s, p, _ := newPurchaseService(t, o)
	p.subs = []*Subscription{business("sub_1", "o1", "active"), business("sub_2", "o1", "active")}
	p.sessions["cs_2"] = &CheckoutSession{ID: "cs_2", ClientReferenceID: "o1", SubscriptionID: "sub_2"}

	_, err := s.BindCheckoutSession(context.Background(), "o1", "cs_2")
	if !errors.Is(err, ErrAlreadySubscribed) {
		t.Fatalf("err = %v", err)
	}
	if len(p.cancelled) != 1 || p.cancelled[0] != "sub_2" {
		t.Fatalf("cancelled = %v, want the newer subscription", p.cancelled)
	}
	if o.orgs["o1"].Billing.SubscriptionRef != "sub_1" {
		t.Fatal("the held subscription was replaced")
	}
}

func TestChangePlanMovesTheOneSubscriptionInPlace(t *testing.T) {
	org := &orgs.Org{ID: "o1", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanBusinessLite,
		Billing: orgs.Billing{Status: orgs.PlanStatusActive, SubscriptionRef: "sub_1", ItemRef: "si_1", CustomerRef: "cus_1"}}
	o := &fakeOrgs{orgs: map[string]*orgs.Org{"o1": org}}
	s, p, _ := newPurchaseService(t, o)
	after := business("sub_1", "o1", "active")
	after.ItemID = "si_1"
	p.subs = []*Subscription{after}

	got, err := s.ChangePlan(context.Background(), org, orgs.PlanBusiness, IntervalMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.changes) != 1 || p.changes[0] != "sub_1|si_1|price_bm|7" {
		t.Fatalf("changes = %v", p.changes)
	}
	if got.BilledPlan != orgs.PlanBusiness {
		t.Fatalf("plan after change = %q", got.BilledPlan)
	}
	// No live subscription: nothing to change. A granted plan: nothing to buy.
	none := &orgs.Org{ID: "o2", OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanSingle}
	if _, err := s.ChangePlan(context.Background(), none, orgs.PlanBusiness, IntervalMonth); !errors.Is(err, ErrNoSubscription) {
		t.Errorf("no subscription: %v", err)
	}
	if _, err := s.ChangePlan(context.Background(), org, "single", IntervalMonth); !errors.Is(err, ErrUnknownPlan) {
		t.Errorf("unsellable: %v", err)
	}
}

func TestPortalNeedsACustomer(t *testing.T) {
	with := &orgs.Org{ID: "o1", Billing: orgs.Billing{CustomerRef: "cus_1"}}
	without := &orgs.Org{ID: "o2"}
	s, p, _ := newPurchaseService(t, &fakeOrgs{orgs: map[string]*orgs.Org{}})
	url, err := s.PortalURL(context.Background(), with)
	if err != nil || url != "https://portal.example/cus_1" {
		t.Fatalf("portal: %q %v", url, err)
	}
	if p.portals[0] != "cus_1|https://app.example/org/settings?tab=billing|bpc_1" {
		t.Fatalf("portal request = %q", p.portals[0])
	}
	if _, err := s.PortalURL(context.Background(), without); !errors.Is(err, ErrNoCustomer) {
		t.Fatalf("without a customer: %v", err)
	}
}

func TestRegistryPriceLookup(t *testing.T) {
	e, ok := testRegistry.Price(orgs.PlanBusiness, IntervalMonth)
	if !ok || e.Price != "price_bm" {
		t.Fatalf("Price = %+v %v", e, ok)
	}
	if _, ok := testRegistry.Price(orgs.PlanBusiness, IntervalYear); ok {
		t.Fatal("an unregistered interval resolved")
	}
}
