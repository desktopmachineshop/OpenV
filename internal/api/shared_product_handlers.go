package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// registerSharedProductRoutes wires the community pool of demo products.
//
// These are the only endpoints in OpenV whose reads and writes cross tenant
// boundaries by design, so the gates are spelled out on each handler rather
// than left to the usual org scoping:
//   - every route sits behind authentication (no /public/ prefix), so the
//     pool is never an anonymous, crawlable, spammable surface;
//   - publishing and reporting require a signed-in person — an agent run
//     token or a host worker key cannot put text in front of other tenants;
//   - deleting requires a platform admin.
func (h *Handler) registerSharedProductRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/shared-products", h.ListSharedProducts).Methods("GET")
	router.HandleFunc("/api/v1/shared-products", h.PublishSharedProduct).Methods("POST")
	router.HandleFunc("/api/v1/shared-products/{id}/report", h.ReportSharedProduct).Methods("POST")
	router.HandleFunc("/api/v1/shared-products/{id}", h.DeleteSharedProduct).Methods("DELETE")
}

// sharedProductRequest is the publish payload — the six fields the wizard
// card renders, and nothing else. Ids, timestamps and author metadata are
// assigned server-side; a client cannot set them.
type sharedProductRequest struct {
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Vision      string `json:"vision"`
	Problem     string `json:"problem"`
	TargetUsers string `json:"target_users"`
}

// ListSharedProducts returns the visible community pool. Any authenticated
// caller may read it; the payload carries no author identity.
func (h *Handler) ListSharedProducts(w http.ResponseWriter, r *http.Request) {
	if h.sharedProductService == nil {
		writeJSONError(w, http.StatusNotFound, "shared products are not available")
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	products, err := h.sharedProductService.List(limit)
	if err != nil {
		respondInternal(w, r, "failed to load shared products", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(products)
}

// PublishSharedProduct shares one product with every workspace.
//
// Sharing is a deliberate act by a person who has read the text on screen:
// only a session-authenticated member of the active workspace may publish,
// which keeps unread agent output from reaching other tenants. The service
// sanitizes and rate-limits from there.
func (h *Handler) PublishSharedProduct(w http.ResponseWriter, r *http.Request) {
	if h.sharedProductService == nil {
		writeJSONError(w, http.StatusNotFound, "shared products are not available")
		return
	}
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusForbidden, sharedproducts.ErrNotPublishable.Error())
		return
	}
	orgID := ActiveOrg(r)
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}

	var req sharedProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	product, err := h.sharedProductService.Publish(sharedproducts.Product{
		Category:    req.Category,
		Name:        req.Name,
		Description: req.Description,
		Vision:      req.Vision,
		Problem:     req.Problem,
		TargetUsers: req.TargetUsers,
	}, orgID, user.ID)
	if err != nil {
		writeSharedProductError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(product)
}

// ReportSharedProduct flags an entry for review. Any signed-in person may
// report; enough distinct reporters hide it from everyone pending admin
// review (one account reporting repeatedly changes nothing).
func (h *Handler) ReportSharedProduct(w http.ResponseWriter, r *http.Request) {
	if h.sharedProductService == nil {
		writeJSONError(w, http.StatusNotFound, "shared products are not available")
		return
	}
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusForbidden, "only a signed-in person can report a shared product")
		return
	}
	if err := h.sharedProductService.Report(mux.Vars(r)["id"], user.ID); err != nil {
		writeSharedProductError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteSharedProduct removes an entry outright. Platform admins only: this
// is the takedown path for anything the report threshold has not caught.
func (h *Handler) DeleteSharedProduct(w http.ResponseWriter, r *http.Request) {
	if h.sharedProductService == nil {
		writeJSONError(w, http.StatusNotFound, "shared products are not available")
		return
	}
	user := CurrentUser(r)
	if user == nil || !user.IsAdmin {
		writeJSONError(w, http.StatusForbidden, "platform admin required")
		return
	}
	if err := h.sharedProductService.Delete(mux.Vars(r)["id"]); err != nil {
		writeSharedProductError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeSharedProductError maps domain errors onto status codes. Validation
// refusals are reported verbatim so the publisher can see why their product
// was turned away (a link, a marker, an overlong field).
func writeSharedProductError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sharedproducts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, sharedproducts.ErrDuplicate):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, sharedproducts.ErrRateLimited), errors.Is(err, sharedproducts.ErrPoolFull):
		writeJSONError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, sharedproducts.ErrNotPublishable):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, sharedproducts.ErrEmptyField),
		errors.Is(err, sharedproducts.ErrTooLong),
		errors.Is(err, sharedproducts.ErrLinksNotAllowed),
		errors.Is(err, sharedproducts.ErrDisallowedText):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		respondInternal(w, r, "failed to update shared products", err)
	}
}
