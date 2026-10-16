package billing

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/openv/requirements-platform/ext"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Module is billing's HTTP surface and its workspace observer, plugged into
// the server through ext (docs/plans/billing-stripe.md,
// docs/plans/hosted-edition-separation.md).
//
// Every route is registered whether or not a provider is configured, so the
// HTTP surface is the same on every deployment and the route inventory does
// not depend on the environment; a workspace route answers 404
// billing_unavailable inside the handler when billing is off. The public
// catalogue never calls the provider: it serves the last confirmed reading,
// so an unauthenticated endpoint cannot be made to spend the provider's
// rate limit and a provider outage never blanks the pricing page.
type Module struct {
	svc     *Service
	limits  Limits
	host    ext.Host
	refresh ext.Limiter
	write   ext.Limiter
}

// Limit is one token bucket: burst tokens refilled at RefillPerHour. A zero
// field takes the bucket's default.
type Limit struct {
	Burst         int
	RefillPerHour float64
}

// Limits are the module's two buckets (OPENV_BILLING_REFRESH_* and
// OPENV_BILLING_WRITE_*).
type Limits struct {
	// Refresh bounds synchronous subscription re-reads per workspace. The
	// post-checkout return page calls it once, and a person staring at a
	// slow page calls it again; it re-reads the workspace's subscription
	// from the provider, so it is bounded per workspace to keep a stuck
	// client from spending the provider's rate limit.
	Refresh Limit
	// Write bounds the purchase writes — checkout, plan change, portal —
	// which each create a provider object, so a compromised admin session
	// cannot spam them.
	Write Limit
}

const (
	defaultRefreshBurst  = 10
	defaultRefreshRefill = 120.0
	defaultWriteBurst    = 5
	defaultWriteRefill   = 20.0
)

// retryAfterSeconds is the Retry-After of a 503 the provider caused.
const retryAfterSeconds = 30

// NewModule builds the module around svc, which is nil where no provider
// is configured: the routes then answer as billing off.
func NewModule(svc *Service, limits Limits) *Module {
	return &Module{svc: svc, limits: limits}
}

var (
	_ ext.RouteModule       = (*Module)(nil)
	_ ext.HostAware         = (*Module)(nil)
	_ ext.WorkspaceObserver = (*Module)(nil)
)

// Name implements ext.Module.
func (m *Module) Name() string { return "billing" }

// SetHost implements ext.HostAware. Billed seats are the seat limit's own
// reading, and the return origin is the app's unless the operator named
// another.
func (m *Module) SetHost(h ext.Host) {
	m.host = h
	m.refresh = h.NewLimiter(orDefault(m.limits.Refresh, defaultRefreshBurst, defaultRefreshRefill))
	m.write = h.NewLimiter(orDefault(m.limits.Write, defaultWriteBurst, defaultWriteRefill))
	if m.svc != nil {
		m.svc.SetSeatCounter(h.CountSeats)
		m.svc.DefaultReturnURL(h.FrontendURL())
	}
}

func orDefault(l Limit, burst int, refill float64) (int, float64) {
	if l.Burst == 0 {
		l.Burst = burst
	}
	if l.RefillPerHour == 0 {
		l.RefillPerHour = refill
	}
	return l.Burst, l.RefillPerHour
}

// Routes implements ext.RouteModule.
func (m *Module) Routes(r ext.Router) {
	admin := ext.RequireWorkspaceRole("id", ext.RoleAdmin)
	r.Handle("GET", "/api/v1/public/plans", m.GetPublicPlans)
	r.Handle("GET", "/api/v1/orgs/{id}/billing", m.GetOrgBilling, admin)
	r.Handle("POST", "/api/v1/orgs/{id}/billing/refresh", m.RefreshOrgBilling, ext.AlwaysWritable(), admin)
	// The purchase path: admin only, rate limited, every provider value
	// decided by the server.
	r.Handle("POST", "/api/v1/orgs/{id}/billing/checkout", m.CheckoutOrgBilling, ext.AlwaysWritable(), admin)
	r.Handle("POST", "/api/v1/orgs/{id}/billing/change", m.ChangeOrgBillingPlan, ext.AlwaysWritable(), admin)
	r.Handle("POST", "/api/v1/orgs/{id}/billing/portal", m.OpenOrgBillingPortal, ext.AlwaysWritable(), admin)
}

// SeatsChanged implements ext.WorkspaceObserver: the seat sync queue.
func (m *Module) SeatsChanged(orgID string) {
	if m.svc != nil {
		m.svc.SeatsChanged(orgID)
	}
}

// WorkspaceDeleted implements ext.WorkspaceObserver.
func (m *Module) WorkspaceDeleted(ctx context.Context, org *ext.Workspace) {
	m.svc.OnWorkspaceDeleted(ctx, org)
}

// WorkspaceRestored implements ext.WorkspaceObserver.
func (m *Module) WorkspaceRestored(ctx context.Context, org *ext.Workspace) {
	m.svc.OnWorkspaceRestored(ctx, org)
}

// billingAdmin runs the checks every purchase-path handler shares after the
// admin guard: billing configured, the write bucket not spent, a live
// workspace. It returns the workspace, or nil after answering.
func (m *Module) billingAdmin(w http.ResponseWriter, r *http.Request) (*orgs.Org, string) {
	orgID := m.host.PathVar(r, "id")
	if !m.available(w) {
		return nil, orgID
	}
	if ok, retryAfter := m.write.Allow(orgID); !ok {
		m.host.WriteRateLimited(w, "too many billing requests for this workspace; try again shortly", retryAfter)
		return nil, orgID
	}
	org, err := m.host.Workspaces().Get(orgID)
	if err != nil {
		m.WriteError(w, r, err)
		return nil, orgID
	}
	return org, orgID
}

// CheckoutOrgBilling starts a hosted checkout: {"plan","interval","currency"}
// → {"url"}. Gated on the workspace-billing feature for a workspace that has
// no subscription yet; a workspace that already holds one is never gated
// out of managing it.
func (m *Module) CheckoutOrgBilling(w http.ResponseWriter, r *http.Request) {
	org, orgID := m.billingAdmin(w, r)
	if org == nil {
		return
	}
	if !m.host.FeatureEnabled(r, orgID, ext.FeatureWorkspaceBilling) {
		m.host.WriteFeatureGated(w)
		return
	}
	var req struct {
		Plan     string `json:"plan"`
		Interval string `json:"interval"`
		Currency string `json:"currency"`
	}
	if !m.host.DecodeJSON(w, r, &req) {
		return
	}
	user := m.host.CurrentUser(r)
	buyer := Buyer{ID: user.ID, Email: user.Email, Name: user.Name, TrialUsed: user.BillingTrialUsedAt != nil}
	url, err := m.svc.Checkout(r.Context(), org, buyer, req.Plan, req.Interval, req.Currency)
	if err != nil {
		m.WriteError(w, r, err)
		return
	}
	m.host.RespondJSON(w, http.StatusOK, map[string]string{"url": url})
}

// ChangeOrgBillingPlan moves the workspace's live subscription to another
// plan or interval in place: {"plan","interval"} → the billing state.
func (m *Module) ChangeOrgBillingPlan(w http.ResponseWriter, r *http.Request) {
	org, _ := m.billingAdmin(w, r)
	if org == nil {
		return
	}
	var req struct {
		Plan     string `json:"plan"`
		Interval string `json:"interval"`
	}
	if !m.host.DecodeJSON(w, r, &req) {
		return
	}
	updated, err := m.svc.ChangePlan(r.Context(), org, req.Plan, req.Interval)
	if err != nil {
		m.WriteError(w, r, err)
		return
	}
	m.host.RespondJSON(w, http.StatusOK, m.orgBilling(updated))
}

// OpenOrgBillingPortal opens the provider's self-service portal → {"url"}.
func (m *Module) OpenOrgBillingPortal(w http.ResponseWriter, r *http.Request) {
	org, _ := m.billingAdmin(w, r)
	if org == nil {
		return
	}
	url, err := m.svc.PortalURL(r.Context(), org)
	if err != nil {
		m.WriteError(w, r, err)
		return
	}
	m.host.RespondJSON(w, http.StatusOK, map[string]string{"url": url})
}

// orgBillingResponse is what the Billing tab reads: the billed and entitled
// plans, the mirrored subscription snapshot (never a provider id), and the
// catalogue so the tab can offer what is for sale.
type orgBillingResponse struct {
	OrgID        string `json:"org_id"`
	Plan         string `json:"plan"`
	EntitledPlan string `json:"entitled_plan"`
	// Granted marks a plan a platform admin gave rather than one a
	// subscription bought; nothing here is for the workspace to buy.
	Granted    bool         `json:"granted"`
	Billing    orgs.Billing `json:"billing"`
	SelfHosted bool         `json:"self_hosted"`
	Plans      PublicPlans  `json:"plans"`
}

func (m *Module) orgBilling(org *orgs.Org) orgBillingResponse {
	b := org.Billing
	if b.Status == "" {
		b.Status = orgs.PlanStatusNone
	}
	return orgBillingResponse{
		OrgID:        org.ID,
		Plan:         org.BilledPlan,
		EntitledPlan: org.EntitledPlan(),
		Granted:      orgs.GrantedPlan(org.BilledPlan),
		Billing:      b,
		SelfHosted:   orgs.SelfHosted(),
		Plans:        m.svc.PublicPlans(),
	}
}

// available answers 404 billing_unavailable when no provider is
// configured, and reports whether the handler may go on.
func (m *Module) available(w http.ResponseWriter) bool {
	if !m.svc.Enabled() {
		m.host.WriteErrorCode(w, http.StatusNotFound, "billing is not available on this deployment", ext.ErrCodeBillingUnavailable)
		return false
	}
	return true
}

// GetPublicPlans is the pricing page's catalogue: every plan for sale with
// its confirmed amounts per currency. Public, cached, and served from
// memory. With no provider it says so and lists nothing.
func (m *Module) GetPublicPlans(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	m.host.RespondJSON(w, http.StatusOK, m.svc.PublicPlans())
}

// GetOrgBilling is the workspace's billing state, for admins: the plan it
// is billed for, the plan it is entitled to, and the subscription snapshot.
// No provider call.
func (m *Module) GetOrgBilling(w http.ResponseWriter, r *http.Request) {
	orgID := m.host.PathVar(r, "id")
	if !m.available(w) {
		return
	}
	org, err := m.host.Workspaces().Get(orgID)
	if err != nil {
		if errors.Is(err, orgs.ErrNotFound) {
			m.host.WriteError(w, http.StatusNotFound, "workspace not found")
			return
		}
		m.host.RespondInternal(w, r, "failed to read the workspace", err)
		return
	}
	m.host.RespondJSON(w, http.StatusOK, m.orgBilling(org))
}

// RefreshOrgBilling re-reads the workspace's subscription from the provider
// and applies it now, so a return page can show the entitlement before it
// renders rather than a reconcile interval later. It reads; it can never
// grant anything the provider does not hold. A provider failure is a 503
// with the workspace left exactly as it was.
func (m *Module) RefreshOrgBilling(w http.ResponseWriter, r *http.Request) {
	orgID := m.host.PathVar(r, "id")
	if !m.available(w) {
		return
	}
	if ok, retryAfter := m.refresh.Allow(orgID); !ok {
		m.host.WriteRateLimited(w, "billing was refreshed too recently; try again shortly", retryAfter)
		return
	}
	// A return page carries the checkout session it came back from; binding
	// through it is what makes the entitlement live before the page
	// renders. An empty body is a plain re-read.
	var req struct {
		SessionID string `json:"session_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if !m.host.DecodeJSON(w, r, &req) {
			return
		}
	}
	var (
		org *orgs.Org
		err error
	)
	if req.SessionID != "" {
		org, err = m.svc.BindCheckoutSession(r.Context(), orgID, req.SessionID)
	} else {
		org, err = m.svc.RefreshOrg(r.Context(), orgID)
	}
	if err != nil {
		m.WriteError(w, r, err)
		return
	}
	m.host.RespondJSON(w, http.StatusOK, m.orgBilling(org))
}

// WriteError answers a billing error: the not-found and refusal codes a
// client branches on, and for anything else a 503 with Retry-After, since
// the provider is the only other thing that fails here. Prices the provider
// has not confirmed yet pass as its silence does, so they get the same 503,
// Retry-After and code, with a message that says what is missing (#379's
// bug 189).
func (m *Module) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	h := m.host
	switch {
	case errors.Is(err, orgs.ErrNotFound):
		h.WriteError(w, http.StatusNotFound, "workspace not found")
	case errors.Is(err, ErrCheckoutNotFound):
		h.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNotConfigured):
		h.WriteErrorCode(w, http.StatusNotFound, err.Error(), ext.ErrCodeBillingUnavailable)
	case errors.Is(err, ErrUnknownPlan), errors.Is(err, ErrUnknownCurrency),
		errors.Is(err, ErrCurrencyLocked), errors.Is(err, ErrPersonalWorkspace):
		h.WriteErrorCode(w, http.StatusBadRequest, err.Error(), ext.ErrCodeUnknownPlan)
	case errors.Is(err, ErrAlreadySubscribed):
		h.WriteErrorCode(w, http.StatusConflict, err.Error(), ext.ErrCodeAlreadySubscribed)
	case errors.Is(err, ErrNoSubscription):
		h.WriteErrorCode(w, http.StatusConflict, err.Error(), ext.ErrCodeNoSubscription)
	case errors.Is(err, ErrGrantedPlan):
		h.WriteErrorCode(w, http.StatusConflict, err.Error(), ext.ErrCodeGrantedPlan)
	case errors.Is(err, ErrNoCustomer):
		h.WriteErrorCode(w, http.StatusConflict, err.Error(), ext.ErrCodeNoCustomer)
	case errors.Is(err, ErrSessionMismatch):
		h.WriteErrorCode(w, http.StatusForbidden, err.Error(), ext.ErrCodeCheckoutMismatch)
	case errors.Is(err, ErrPricesUnconfirmed):
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
		h.WriteErrorCode(w, http.StatusServiceUnavailable,
			"prices have not been confirmed with the billing provider yet; try again shortly", ext.ErrCodeBillingUpstream)
	default:
		slog.Warn("billing: provider did not answer", "path", r.URL.Path, "org_id", h.PathVar(r, "id"), "error", err)
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
		h.WriteErrorCode(w, http.StatusServiceUnavailable,
			"the billing provider did not answer; the workspace was left as it was", ext.ErrCodeBillingUpstream)
	}
}
