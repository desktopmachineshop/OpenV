package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
