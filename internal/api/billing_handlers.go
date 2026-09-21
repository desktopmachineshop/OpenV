package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/orgs"
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
	org, err := h.billing.RefreshOrg(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, orgs.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "workspace not found")
			return
		}
		slog.Warn("billing refresh: provider did not answer", "org_id", orgID, "error", err)
		w.Header().Set("Retry-After", strconv.Itoa(30))
		writeJSONErrorCode(w, http.StatusServiceUnavailable,
			"the billing provider did not answer; the workspace's plan was left as it was", ErrCodeBillingUpstream)
		return
	}
	respondJSON(w, http.StatusOK, h.orgBilling(org))
}
