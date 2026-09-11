package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
)

// Web push subscription API (REQ-109). Every endpoint is own-user only: the
// caller must be a signed-in human (agent run tokens and worker keys are
// refused by requireHumanUser) and every query is keyed on that session's
// user id, so a member can only ever see or withdraw their own devices.

const (
	// maxPushEndpointLen bounds the stored endpoint. Real push service URLs
	// are well under 500 bytes; this only stops an absurd body.
	maxPushEndpointLen = 2048
	// maxPushKeyLen bounds the base64url client keys (p256dh is 65 bytes
	// encoded to 88 chars, auth 16 bytes to 24).
	maxPushKeyLen = 256
	// maxPushUserAgentLen bounds the cosmetic device label.
	maxPushUserAgentLen = 400
)

func (h *Handler) registerPushRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/push/config", h.GetPushConfig).Methods("GET")
	router.HandleFunc("/api/v1/me/push-subscriptions", h.ListPushSubscriptions).Methods("GET")
	router.HandleFunc("/api/v1/me/push-subscriptions", h.CreatePushSubscription).Methods("POST")
	router.HandleFunc("/api/v1/me/push-subscriptions", h.DeletePushSubscription).Methods("DELETE")
}

// pushConfig tells the browser whether this deployment can send web push and,
// if so, the application server key it must subscribe with. The PUBLIC half
// of the VAPID pair only — it is meant to be handed out.
type pushConfig struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
}

// pushSubscriptionRequest is the wire shape of a PushSubscription as
// serialized by the browser, so the client can post what it got back from
// pushManager.subscribe() almost verbatim.
type pushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	UserAgent string `json:"user_agent"`
}

// GetPushConfig answers whether push is configured and with which key.
// enabled is false when the server has no VAPID key pair, which is what the
// settings toggle shows as "not available on this server".
func (h *Handler) GetPushConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireHumanUser(w, r); !ok {
		return
	}
	cfg := pushConfig{Enabled: h.vapid.Enabled()}
	if cfg.Enabled {
		cfg.PublicKey = h.vapid.PublicKey
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// ListPushSubscriptions answers the caller's own devices. The encryption keys
// are never serialized (see pushsubs.Subscription's json tags): the browser
// already has them and nothing else has any business reading them.
func (h *Handler) ListPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	if h.pushSubService == nil {
		writePushSubscriptions(w, nil)
		return
	}
	list, err := h.pushSubService.ListForUser(userID)
	if err != nil {
		respondInternal(w, r, "failed to load push subscriptions", err)
		return
	}
	writePushSubscriptions(w, list)
}

// writePushSubscriptions serializes the device list, normalising nil to an
// empty array so the client never has to special-case null.
func writePushSubscriptions(w http.ResponseWriter, list []*pushsubs.Subscription) {
	if list == nil {
		list = []*pushsubs.Subscription{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"subscriptions": list})
}

// CreatePushSubscription stores one device's subscription for the caller.
// Idempotent on the endpoint: re-posting the same device refreshes its keys
// rather than creating a second row, which is exactly what a browser does
// when it rotates them. Answers 201 either way.
func (h *Handler) CreatePushSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	if h.pushSubService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "push notifications are not available on this server")
		return
	}
	var req pushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	p256dh := strings.TrimSpace(req.Keys.P256dh)
	auth := strings.TrimSpace(req.Keys.Auth)
	if msg := validatePushSubscription(endpoint, p256dh, auth); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}
	userAgent := strings.TrimSpace(req.UserAgent)
	if userAgent == "" {
		userAgent = strings.TrimSpace(r.UserAgent())
	}
	if len(userAgent) > maxPushUserAgentLen {
		userAgent = userAgent[:maxPushUserAgentLen]
	}

	sub := pushsubs.New(userID, endpoint, p256dh, auth, userAgent)
	if err := h.pushSubService.Subscribe(sub); err != nil {
		respondInternal(w, r, "failed to store push subscription", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sub)
}

// validatePushSubscription answers a client-facing message, or "" when the
// subscription is well-formed enough to store.
func validatePushSubscription(endpoint, p256dh, auth string) string {
	switch {
	case endpoint == "":
		return "endpoint is required"
	case !strings.HasPrefix(endpoint, "https://"):
		// Push services are https-only; anything else is a mistake or an
		// attempt to make the server post somewhere it should not.
		return "endpoint must be an https URL"
	case len(endpoint) > maxPushEndpointLen:
		return "endpoint is too long"
	case p256dh == "" || auth == "":
		return "keys.p256dh and keys.auth are required"
	case len(p256dh) > maxPushKeyLen || len(auth) > maxPushKeyLen:
		return "keys are too long"
	}
	return ""
}

// DeletePushSubscription withdraws one of the caller's own devices. 204
// whether or not a row was there: withdrawing something already gone is the
// state the caller asked for, and answering 404 would leak nothing useful.
func (h *Handler) DeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	if h.pushSubService == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" {
		writeJSONError(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	if _, err := h.pushSubService.Unsubscribe(userID, endpoint); err != nil {
		respondInternal(w, r, "failed to remove push subscription", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
