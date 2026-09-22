package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// Billing routes (docs/plans/billing-stripe.md).
//
// Every route is registered whether or not a provider is configured, so the
// HTTP surface is the same on every deployment and the route inventory does
// not depend on the environment; a workspace route answers 404
// billing_unavailable inside the handler when billing is off. The public
// catalogue never calls the provider: it serves the last confirmed reading,
// so an unauthenticated endpoint cannot be made to spend the provider's
// rate limit and a provider outage never blanks the pricing page.
func (h *Handler) registerBillingRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/public/plans", h.GetPublicPlans).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/billing", h.GetOrgBilling).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/billing/refresh", h.RefreshOrgBilling).Methods("POST")
	// The purchase path: admin only, rate limited, every provider value
	// decided by the server.
	router.HandleFunc("/api/v1/orgs/{id}/billing/checkout", h.CheckoutOrgBilling).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/billing/change", h.ChangeOrgBillingPlan).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/billing/portal", h.OpenOrgBillingPortal).Methods("POST")
}

// writeBillingError answers a purchase-path refusal with the status and code
// a client branches on. Anything unrecognised is the provider not answering:
// 503 with Retry-After, the workspace left as it was.
func (h *Handler) writeBillingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, orgs.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "workspace not found")
	case errors.Is(err, billing.ErrNotConfigured):
		writeJSONErrorCode(w, http.StatusNotFound, err.Error(), ErrCodeBillingUnavailable)
	case errors.Is(err, billing.ErrUnknownPlan), errors.Is(err, billing.ErrUnknownCurrency),
		errors.Is(err, billing.ErrCurrencyLocked), errors.Is(err, billing.ErrPersonalWorkspace):
		writeJSONErrorCode(w, http.StatusBadRequest, err.Error(), ErrCodeUnknownPlan)
	case errors.Is(err, billing.ErrAlreadySubscribed):
		writeJSONErrorCode(w, http.StatusConflict, err.Error(), ErrCodeAlreadySubscribed)
	case errors.Is(err, billing.ErrNoSubscription):
		writeJSONErrorCode(w, http.StatusConflict, err.Error(), ErrCodeNoSubscription)
	case errors.Is(err, billing.ErrGrantedPlan):
		writeJSONErrorCode(w, http.StatusConflict, err.Error(), ErrCodeGrantedPlan)
	case errors.Is(err, billing.ErrNoCustomer):
		writeJSONErrorCode(w, http.StatusConflict, err.Error(), ErrCodeNoCustomer)
	case errors.Is(err, billing.ErrSessionMismatch):
		writeJSONErrorCode(w, http.StatusForbidden, err.Error(), ErrCodeCheckoutMismatch)
	default:
		slog.Warn("billing: provider did not answer", "path", r.URL.Path, "org_id", mux.Vars(r)["id"], "error", err)
		w.Header().Set("Retry-After", strconv.Itoa(30))
		writeJSONErrorCode(w, http.StatusServiceUnavailable,
			"the billing provider did not answer; the workspace was left as it was", ErrCodeBillingUpstream)
	}
}

// billingAdmin runs the checks every purchase-path handler shares: an admin
// of a live workspace, billing configured, the write bucket not spent. It
// returns the workspace, or nil after answering.
func (h *Handler) billingAdmin(w http.ResponseWriter, r *http.Request) (*orgs.Org, string) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return nil, orgID
	}
	if !h.billingAvailable(w) {
		return nil, orgID
	}
	if ok, retryAfter := h.billingWriteLimiter.allow(orgID); !ok {
		writeRateLimited(w, "too many billing requests for this workspace; try again shortly", retryAfter)
		return nil, orgID
	}
	org, err := h.orgService.Get(orgID)
	if err != nil {
		h.writeBillingError(w, r, err)
		return nil, orgID
	}
	return org, orgID
}

// CheckoutOrgBilling starts a hosted checkout: {"plan","interval","currency"}
// → {"url"}. Gated on the workspace-billing feature for a workspace that has
// no subscription yet; a workspace that already holds one is never gated
// out of managing it.
func (h *Handler) CheckoutOrgBilling(w http.ResponseWriter, r *http.Request) {
	org, orgID := h.billingAdmin(w, r)
	if org == nil {
		return
	}
	if !h.featureEnabled(r, orgID, release.FeatureWorkspaceBilling) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req struct {
		Plan     string `json:"plan"`
		Interval string `json:"interval"`
		Currency string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user := CurrentUser(r)
	buyer := billing.Buyer{ID: user.ID, Email: user.Email, Name: user.Name, TrialUsed: user.BillingTrialUsedAt != nil}
	url, err := h.billing.Checkout(r.Context(), org, buyer, req.Plan, req.Interval, req.Currency)
	if err != nil {
		h.writeBillingError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"url": url})
}

// ChangeOrgBillingPlan moves the workspace's live subscription to another
// plan or interval in place: {"plan","interval"} → the billing state.
func (h *Handler) ChangeOrgBillingPlan(w http.ResponseWriter, r *http.Request) {
	org, _ := h.billingAdmin(w, r)
	if org == nil {
		return
	}
	var req struct {
		Plan     string `json:"plan"`
		Interval string `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.billing.ChangePlan(r.Context(), org, req.Plan, req.Interval)
	if err != nil {
		h.writeBillingError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, h.orgBilling(updated))
}

// OpenOrgBillingPortal opens the provider's self-service portal → {"url"}.
func (h *Handler) OpenOrgBillingPortal(w http.ResponseWriter, r *http.Request) {
	org, _ := h.billingAdmin(w, r)
	if org == nil {
		return
	}
	url, err := h.billing.PortalURL(r.Context(), org)
	if err != nil {
		h.writeBillingError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"url": url})
}

// orgBillingResponse is what the Billing tab reads: the billed and entitled
// plans, the mirrored subscription snapshot (never a provider id), and the
// catalogue so the tab can offer what is for sale.
// seatsChanged tells the billing service a workspace's seat count may have
// moved, after the local write committed. Never inline and never a failure
// the caller sees: a membership change must not wait on, or fail because
// of, the billing provider. Accepting an invitation is not a change — the
// invitation was already a seat — so acceptance does not call this.
func (h *Handler) seatsChanged(orgID string) {
	if h.billing != nil {
		h.billing.SeatsChanged(orgID)
	}
}

type orgBillingResponse struct {
	OrgID        string `json:"org_id"`
	Plan         string `json:"plan"`
	EntitledPlan string `json:"entitled_plan"`
	// Granted marks a plan a platform admin gave rather than one a
	// subscription bought; nothing here is for the workspace to buy.
	Granted    bool                `json:"granted"`
	Billing    orgs.Billing        `json:"billing"`
	SelfHosted bool                `json:"self_hosted"`
	Plans      billing.PublicPlans `json:"plans"`
}

func (h *Handler) orgBilling(org *orgs.Org) orgBillingResponse {
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
		Plans:        h.billing.PublicPlans(),
	}
}

// billingAvailable answers 404 billing_unavailable when no provider is
// configured, and reports whether the handler may go on.
func (h *Handler) billingAvailable(w http.ResponseWriter) bool {
	if !h.billing.Enabled() {
		writeJSONErrorCode(w, http.StatusNotFound, "billing is not available on this deployment", ErrCodeBillingUnavailable)
		return false
	}
	return true
}

// GetPublicPlans is the pricing page's catalogue: every plan for sale with
// its confirmed amounts per currency. Public, cached, and served from
// memory. With no provider it says so and lists nothing.
func (h *Handler) GetPublicPlans(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	respondJSON(w, http.StatusOK, h.billing.PublicPlans())
}

// GetOrgBilling is the workspace's billing state, for admins: the plan it
// is billed for, the plan it is entitled to, and the subscription snapshot.
// No provider call.
func (h *Handler) GetOrgBilling(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.billingAvailable(w) {
		return
	}
	org, err := h.orgService.Get(orgID)
	if err != nil {
		if errors.Is(err, orgs.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "workspace not found")
			return
		}
		respondInternal(w, r, "failed to read the workspace", err)
		return
	}
	respondJSON(w, http.StatusOK, h.orgBilling(org))
}

// RefreshOrgBilling re-reads the workspace's subscription from the provider
// and applies it now, so a return page can show the entitlement before it
// renders rather than a reconcile interval later. It reads; it can never
// grant anything the provider does not hold. A provider failure is a 503
// with the workspace left exactly as it was.
func (h *Handler) RefreshOrgBilling(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.billingAvailable(w) {
		return
	}
	if ok, retryAfter := h.billingRefreshLimiter.allow(orgID); !ok {
		writeRateLimited(w, "billing was refreshed too recently; try again shortly", retryAfter)
		return
	}
	// A return page carries the checkout session it came back from; binding
	// through it is what makes the entitlement live before the page
	// renders. An empty body is a plain re-read.
	var req struct {
		SessionID string `json:"session_id"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	var (
		org *orgs.Org
		err error
	)
	if req.SessionID != "" {
		org, err = h.billing.BindCheckoutSession(r.Context(), orgID, req.SessionID)
	} else {
		org, err = h.billing.RefreshOrg(r.Context(), orgID)
	}
	if err != nil {
		h.writeBillingError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, h.orgBilling(org))
}
