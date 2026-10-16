package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/providers/usertokens"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// registerProviderTokenRoutes wires a member's own stored provider sign-in
// token: its status, saving a new one and forgetting it. The token is never
// answered back.
func (h *Handler) registerProviderTokenRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/provider-tokens/{provider}", h.GetProviderToken).Methods("GET")
	router.HandleFunc("/api/v1/me/provider-tokens/{provider}", h.PutProviderToken).Methods("PUT")
	router.HandleFunc("/api/v1/me/provider-tokens/{provider}", h.DeleteProviderToken).Methods("DELETE")
}

// GetProviderToken answers whether the caller has a token stored and when
// it lapses.
func (h *Handler) GetProviderToken(w http.ResponseWriter, r *http.Request) {
	user := requireUserMsg(w, r, "authentication required", http.StatusUnauthorized)
	if user == nil {
		return
	}
	if h.ProviderTokenService == nil {
		writeProviderTokenError(w, r, usertokens.ErrUnavailable)
		return
	}
	st, err := h.ProviderTokenService.Status(user.ID, mux.Vars(r)["provider"])
	if err != nil {
		writeProviderTokenError(w, r, err)
		return
	}
	writeJSONOK(w, st)
}

// PutProviderToken stores the caller's token, replacing any earlier one.
// Saving is gated on the active workspace's channel; reading and forgetting
// never are, so a member can always remove what they stored.
func (h *Handler) PutProviderToken(w http.ResponseWriter, r *http.Request) {
	user := requireUserMsg(w, r, "authentication required", http.StatusUnauthorized)
	if user == nil {
		return
	}
	if h.ProviderTokenService == nil {
		writeProviderTokenError(w, r, usertokens.ErrUnavailable)
		return
	}
	if org := ActiveOrg(r); org != "" && !h.featureEnabled(r, org, release.FeatureSubscriptionToken) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := h.ProviderTokenService.Save(user.ID, mux.Vars(r)["provider"], req.Token)
	if err != nil {
		writeProviderTokenError(w, r, err)
		return
	}
	writeJSONOK(w, st)
}

// DeleteProviderToken forgets the caller's token.
func (h *Handler) DeleteProviderToken(w http.ResponseWriter, r *http.Request) {
	user := requireUserMsg(w, r, "authentication required", http.StatusUnauthorized)
	if user == nil {
		return
	}
	if h.ProviderTokenService == nil {
		writeProviderTokenError(w, r, usertokens.ErrUnavailable)
		return
	}
	if err := h.ProviderTokenService.Delete(user.ID, mux.Vars(r)["provider"]); err != nil {
		writeProviderTokenError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
