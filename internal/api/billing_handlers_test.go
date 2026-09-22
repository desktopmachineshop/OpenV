package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// The billing routes: public plans always answer and never call the
// provider; the workspace routes are admin-only and answer 404
// billing_unavailable with no provider; a refresh that the provider fails
// is a 503 that changes nothing.

// billingOrgFake is a fakeOrgService whose workspace holds a subscription,
// so a refresh reaches the provider. It satisfies billing.Orgs through the
// embedded interface; the methods a refresh needs are implemented here.
type billingOrgFake struct {
	fakeOrgService
	subscriptionRef string
	applied         []orgs.BillingState
}

func (f *billingOrgFake) Get(id string) (*orgs.Org, error) {
	o, err := f.fakeOrgService.Get(id)
	if err != nil {
		return nil, err
	}
	o.Billing.SubscriptionRef = f.subscriptionRef
	return o, nil
}

func (f *billingOrgFake) FindOrgByBillingRef(kind, ref string) (*orgs.Org, error) {
	if ref == f.subscriptionRef && ref != "" {
		return f.Get("org-1")
	}
	return nil, nil
}

func (f *billingOrgFake) ApplyBillingState(orgID string, st orgs.BillingState) (bool, error) {
	f.applied = append(f.applied, st)
	return true, nil
}

// failingProvider answers every read with an error. Anything else panics
// through the nil embed, which is the point: a refresh must only read.
type failingProvider struct {
	billing.Provider
	err error
}

func (p *failingProvider) Name() string { return "failing" }
func (p *failingProvider) GetSubscription(context.Context, string) (*billing.Subscription, error) {
	return nil, p.err
}

func billingReq(method, path, orgID string, user *users.User) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	if orgID != "" {
		r = mux.SetURLVars(r, map[string]string{"id": orgID})
	}
	return r
}

func TestPublicPlansAnswerWithoutAProvider(t *testing.T) {
	h := NewHandler(HandlerDeps{})
	w := httptest.NewRecorder()
	h.GetPublicPlans(w, billingReq(http.MethodGet, "/api/v1/public/plans", "", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", cc)
	}
	var out billing.PublicPlans
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.BillingEnabled || len(out.Plans) != 0 || out.Currencies == nil {
		t.Fatalf("plans without a provider = %+v", out)
	}
}

func TestWorkspaceBillingRoutesAreAdminOnlyAnd404WithoutAProvider(t *testing.T) {
	svc := &billingOrgFake{fakeOrgService: fakeOrgService{
		plan:  orgs.PlanBusiness,
		roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}},
	}}
	h := NewHandler(HandlerDeps{})
	h.orgService = svc
	admin, member := &users.User{ID: "admin"}, &users.User{ID: "member"}

	for name, call := range map[string]func(w http.ResponseWriter, r *http.Request){
		"state":   h.GetOrgBilling,
		"refresh": h.RefreshOrgBilling,
	} {
		w := httptest.NewRecorder()
		call(w, billingReq(http.MethodGet, "/api/v1/orgs/org-1/billing", "org-1", nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s visitor: status = %d", name, w.Code)
		}
		w = httptest.NewRecorder()
		call(w, billingReq(http.MethodGet, "/api/v1/orgs/org-1/billing", "org-1", member))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s member: status = %d", name, w.Code)
		}
		w = httptest.NewRecorder()
		call(w, billingReq(http.MethodGet, "/api/v1/orgs/org-1/billing", "org-1", admin))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s admin without a provider: status = %d", name, w.Code)
		}
		var body errorBody
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body.Code != ErrCodeBillingUnavailable {
			t.Errorf("%s: code = %q, want %q", name, body.Code, ErrCodeBillingUnavailable)
		}
	}
}

func TestWorkspaceBillingStateReadsThePlansWithoutTheProvider(t *testing.T) {
	svc := &billingOrgFake{fakeOrgService: fakeOrgService{
		plan:  orgs.PlanBusiness,
		roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin}},
	}}
	h := NewHandler(HandlerDeps{BillingService: billing.New(&failingProvider{err: errors.New("never called")}, svc, nil, nil)})
	h.orgService = svc

	w := httptest.NewRecorder()
	h.GetOrgBilling(w, billingReq(http.MethodGet, "/api/v1/orgs/org-1/billing", "org-1", &users.User{ID: "admin"}))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	var out orgBillingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Plan != orgs.PlanBusiness || out.EntitledPlan != orgs.PlanBusiness || out.Billing.Status != orgs.PlanStatusNone || out.Granted {
		t.Fatalf("state = %+v", out)
	}
	// Provider object ids never reach a client.
	if raw := w.Body.String(); contains(raw, "subscription_ref") || contains(raw, "customer_ref") {
		t.Fatalf("a provider ref leaked: %s", raw)
	}
}

func TestRefreshIsA503ThatChangesNothingWhenTheProviderFails(t *testing.T) {
	svc := &billingOrgFake{fakeOrgService: fakeOrgService{
		plan:  orgs.PlanBusiness,
		roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin}},
	}, subscriptionRef: "sub_1"}
	h := NewHandler(HandlerDeps{BillingService: billing.New(&failingProvider{err: errors.New("timeout")}, svc, nil, nil)})
	h.orgService = svc
	admin := &users.User{ID: "admin"}

	w := httptest.NewRecorder()
	h.RefreshOrgBilling(w, billingReq(http.MethodPost, "/api/v1/orgs/org-1/billing/refresh", "org-1", admin))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on a 503")
	}
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Code != ErrCodeBillingUpstream {
		t.Errorf("code = %q", body.Code)
	}
	if len(svc.applied) != 0 {
		t.Fatalf("a failed refresh wrote %+v", svc.applied)
	}

	// The refresh is bounded per workspace: after the burst it is 429.
	h.billingRefreshLimiter = newRateLimiter(2, 0)
	codes := []int{}
	for i := 0; i < 3; i++ {
		w = httptest.NewRecorder()
		h.RefreshOrgBilling(w, billingReq(http.MethodPost, "/api/v1/orgs/org-1/billing/refresh", "org-1", admin))
		codes = append(codes, w.Code)
	}
	if codes[0] != http.StatusServiceUnavailable || codes[1] != http.StatusServiceUnavailable || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("codes = %v, want two attempts then 429", codes)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// The purchase routes: admin-only and rate limited; the checkout is gated on
// the workspace-billing feature; every refusal carries the code a client
// branches on; a return page binds through its session id.

type purchaseProviderFake struct {
	billing.Provider
	prices     map[string]*billing.Price
	sessions   map[string]*billing.CheckoutSession
	subs       map[string]*billing.Subscription
	checkouts  int
	portals    int
	quantities []int
}

func (p *purchaseProviderFake) Name() string { return "fake" }
func (p *purchaseProviderFake) GetPrice(_ context.Context, id string) (*billing.Price, error) {
	if pr, ok := p.prices[id]; ok {
		return pr, nil
	}
	return nil, billing.ErrNotFound
}
func (p *purchaseProviderFake) CreateCustomer(context.Context, string, string, map[string]string, string) (string, error) {
	return "cus_1", nil
}
func (p *purchaseProviderFake) CreateCheckoutSession(_ context.Context, req billing.CheckoutRequest) (*billing.CheckoutSession, error) {
	p.checkouts++
	return &billing.CheckoutSession{ID: "cs_1", URL: "https://checkout.example/cs_1", ClientReferenceID: req.ClientReferenceID}, nil
}
func (p *purchaseProviderFake) GetCheckoutSession(_ context.Context, id string) (*billing.CheckoutSession, error) {
	if s, ok := p.sessions[id]; ok {
		return s, nil
	}
	return nil, billing.ErrNotFound
}
func (p *purchaseProviderFake) GetSubscription(_ context.Context, id string) (*billing.Subscription, error) {
	if s, ok := p.subs[id]; ok {
		return s, nil
	}
	return nil, billing.ErrNotFound
}
func (p *purchaseProviderFake) SetItemQuantity(_ context.Context, itemID string, qty int) error {
	p.quantities = append(p.quantities, qty)
	return errors.New("stripe is down")
}
func (p *purchaseProviderFake) CreatePortalSession(context.Context, string, string, string) (string, error) {
	p.portals++
	return "https://portal.example/cus_1", nil
}

// purchaseOrgFake records the billing writes a checkout makes.
type purchaseOrgFake struct {
	fakeOrgService
	orgType  string
	billing  orgs.Billing
	customer string
	removed  []string
}

func (f *purchaseOrgFake) Get(id string) (*orgs.Org, error) {
	o, err := f.fakeOrgService.Get(id)
	if err != nil {
		return nil, err
	}
	o.OrgType = f.orgType
	o.Billing = f.billing
	return o, nil
}
func (f *purchaseOrgFake) RemoveMember(orgID, userID string) error {
	f.removed = append(f.removed, userID)
	return nil
}
func (f *purchaseOrgFake) ListMembers(orgID string) ([]*orgs.Member, error) {
	return []*orgs.Member{{OrgID: orgID, UserID: "admin", Role: orgs.RoleAdmin}, {OrgID: orgID, UserID: "member", Role: orgs.RoleMember}}, nil
}
func (f *purchaseOrgFake) SetBillingCustomer(orgID, ref, currency string) error {
	f.customer = ref
	f.billing.CustomerRef, f.billing.Currency = ref, currency
	return nil
}
func (f *purchaseOrgFake) FindOrgByBillingRef(kind, ref string) (*orgs.Org, error) {
	if ref != "" && ref == f.billing.SubscriptionRef {
		return f.Get("org-1")
	}
	return nil, nil
}
func (f *purchaseOrgFake) ApplyBillingState(orgID string, st orgs.BillingState) (bool, error) {
	f.billing.Status, f.billing.SubscriptionRef, f.billing.ItemRef = st.Status, st.SubscriptionRef, st.ItemRef
	f.plan = st.Plan
	return true, nil
}

func purchaseHandler(t *testing.T, orgType string) (*Handler, *purchaseOrgFake, *purchaseProviderFake) {
	t.Helper()
	svc := &purchaseOrgFake{fakeOrgService: fakeOrgService{
		plan:  orgs.PlanSingle,
		roles: map[string]map[string]string{"org-1": {"admin": orgs.RoleAdmin, "member": orgs.RoleMember}},
	}, orgType: orgType}
	provider := &purchaseProviderFake{
		prices:   map[string]*billing.Price{"price_bm": {ID: "price_bm", Interval: "month", UsageType: "licensed", Amounts: map[string]int64{"gbp": 1200}}},
		sessions: map[string]*billing.CheckoutSession{},
		subs:     map[string]*billing.Subscription{},
	}
	reg, _ := billing.ParseRegistry(`[{"price":"price_bm","plan":"business","interval":"month"}]`)
	b := billing.New(provider, svc, reg, nil)
	if err := b.RefreshPrices(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(HandlerDeps{BillingService: b, FrontendURL: "https://app.example"})
	h.orgService = svc
	// The feature resolves through the org's channel: a nightly workspace
	// sees every gate, which is what a Single-plan workspace is.
	return h, svc, provider
}

func postJSON(path, orgID, body string, user *users.User) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return mux.SetURLVars(r, map[string]string{"id": orgID})
}

func TestCheckoutIsAdminOnlyAndDecidesTheProviderValues(t *testing.T) {
	h, svc, provider := purchaseHandler(t, orgs.TypeCompany)
	admin := &users.User{ID: "admin", Email: "admin@example.com", Name: "Admin"}

	w := httptest.NewRecorder()
	h.CheckoutOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/checkout", "org-1", `{"plan":"business","interval":"month"}`, &users.User{ID: "member"}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("member: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.CheckoutOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/checkout", "org-1", `{"plan":"business","interval":"month"}`, admin))
	if w.Code != http.StatusOK {
		t.Fatalf("admin: %d %s", w.Code, w.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["url"] != "https://checkout.example/cs_1" || provider.checkouts != 1 || svc.customer != "cus_1" {
		t.Fatalf("checkout = %v checkouts=%d customer=%q", out, provider.checkouts, svc.customer)
	}

	// Refusals carry their codes and never reach the provider.
	for name, tc := range map[string]struct {
		body string
		code int
		want string
	}{
		"unknown plan": {`{"plan":"enterprise","interval":"month"}`, http.StatusBadRequest, ErrCodeUnknownPlan},
		"bad currency": {`{"plan":"business","interval":"month","currency":"jpy"}`, http.StatusBadRequest, ErrCodeUnknownPlan},
	} {
		w = httptest.NewRecorder()
		h.CheckoutOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/checkout", "org-1", tc.body, admin))
		var body errorBody
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != tc.code || body.Code != tc.want {
			t.Errorf("%s: %d %q", name, w.Code, body.Code)
		}
	}
	if provider.checkouts != 1 {
		t.Fatalf("a refusal reached the provider: %d", provider.checkouts)
	}

	// A live subscription: 409 already_subscribed; the portal then works.
	svc.billing.Status, svc.billing.SubscriptionRef = orgs.PlanStatusActive, "sub_1"
	w = httptest.NewRecorder()
	h.CheckoutOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/checkout", "org-1", `{"plan":"business","interval":"month"}`, admin))
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusConflict || body.Code != ErrCodeAlreadySubscribed {
		t.Fatalf("live: %d %q", w.Code, body.Code)
	}
	w = httptest.NewRecorder()
	h.OpenOrgBillingPortal(w, postJSON("/api/v1/orgs/org-1/billing/portal", "org-1", ``, admin))
	if w.Code != http.StatusOK || provider.portals != 1 {
		t.Fatalf("portal: %d %s", w.Code, w.Body.String())
	}

	// The write bucket bounds all three.
	h.billingWriteLimiter = newRateLimiter(1, 0)
	w = httptest.NewRecorder()
	h.OpenOrgBillingPortal(w, postJSON("/api/v1/orgs/org-1/billing/portal", "org-1", ``, admin))
	w = httptest.NewRecorder()
	h.OpenOrgBillingPortal(w, postJSON("/api/v1/orgs/org-1/billing/portal", "org-1", ``, admin))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("past the burst: %d", w.Code)
	}
}

func TestBusinessIsRefusedOnAPersonalWorkspace(t *testing.T) {
	h, _, provider := purchaseHandler(t, orgs.TypePersonal)
	w := httptest.NewRecorder()
	h.CheckoutOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/checkout", "org-1", `{"plan":"business","interval":"month"}`, &users.User{ID: "admin"}))
	if w.Code != http.StatusBadRequest || provider.checkouts != 0 {
		t.Fatalf("personal + business: %d (%s)", w.Code, w.Body.String())
	}
}

func TestRefreshBindsThroughTheSessionAndRefusesAnotherWorkspaces(t *testing.T) {
	h, svc, provider := purchaseHandler(t, orgs.TypeCompany)
	admin := &users.User{ID: "admin"}
	provider.subs["sub_1"] = &billing.Subscription{ID: "sub_1", Status: "trialing", PriceID: "price_bm", ItemID: "si_1", Quantity: 1, ItemCount: 1,
		Metadata: map[string]string{billing.OrgMetadataKey: "org-1"}}
	provider.sessions["cs_ok"] = &billing.CheckoutSession{ID: "cs_ok", ClientReferenceID: "org-1", SubscriptionID: "sub_1"}
	provider.sessions["cs_theirs"] = &billing.CheckoutSession{ID: "cs_theirs", ClientReferenceID: "org-2", SubscriptionID: "sub_2"}

	w := httptest.NewRecorder()
	h.RefreshOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/refresh", "org-1", `{"session_id":"cs_theirs"}`, admin))
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusForbidden || body.Code != ErrCodeCheckoutMismatch {
		t.Fatalf("another workspace's session: %d %q", w.Code, body.Code)
	}
	w = httptest.NewRecorder()
	h.RefreshOrgBilling(w, postJSON("/api/v1/orgs/org-1/billing/refresh", "org-1", `{"session_id":"cs_ok"}`, admin))
	if w.Code != http.StatusOK {
		t.Fatalf("bind: %d %s", w.Code, w.Body.String())
	}
	var out orgBillingResponse
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Plan != orgs.PlanBusiness || out.Billing.Status != orgs.PlanStatusTrialing || svc.billing.SubscriptionRef != "sub_1" {
		t.Fatalf("bound state = %+v", out)
	}
}

// The load-bearing property of seat sync: a membership change commits and
// answers before the provider is asked anything, and a provider that is
// down changes nothing about the answer. The push happens on the queue's
// drain, and its failure is the reconciler's to repair.
func TestMembershipChangeSucceedsWhenTheProviderIsDown(t *testing.T) {
	h, svc, provider := purchaseHandler(t, orgs.TypeCompany)
	svc.plan = orgs.PlanBusiness
	svc.billing = orgs.Billing{Status: orgs.PlanStatusActive, Seats: 5, SubscriptionRef: "sub_1", ItemRef: "si_1", CustomerRef: "cus_1"}
	admin := &users.User{ID: "admin", Email: "admin@example.com", Name: "Admin"}

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/orgs/org-1/members/member", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, admin))
	r = mux.SetURLVars(r, map[string]string{"id": "org-1", "userId": "member"})
	w := httptest.NewRecorder()
	h.RemoveOrgMember(w, r)
	if w.Code != http.StatusNoContent || len(svc.removed) != 1 {
		t.Fatalf("remove: %d %s removed=%v", w.Code, w.Body.String(), svc.removed)
	}
	if len(provider.quantities) != 0 {
		t.Fatalf("the handler waited on the provider: %v", provider.quantities)
	}
	if n := h.billing.FlushSeats(context.Background()); n != 1 {
		t.Fatalf("queued %d workspaces, want 1", n)
	}
	// The drain tried — with the seat count the members panel reads — and
	// the provider's failure went nowhere near the member.
	if len(provider.quantities) != 1 || provider.quantities[0] != 2 {
		t.Fatalf("pushed %v, want [2]", provider.quantities)
	}
}
