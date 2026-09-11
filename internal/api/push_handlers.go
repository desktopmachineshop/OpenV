package api

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

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
	// maxPushUserAgentLen bounds the cosmetic device label, in RUNES.
	maxPushUserAgentLen = 400

	// envPushEndpointHosts names extra push-service hosts a self-hosted
	// deployment accepts, on top of the built-in list. Comma-separated;
	// entries may be exact hosts or a leading-wildcard pattern
	// ("*.push.example.net").
	envPushEndpointHosts = "OPENV_PUSH_ENDPOINT_HOSTS"
)

// defaultPushEndpointHosts is the allow-list of push services a subscription
// endpoint may point at. A subscription endpoint is a URL this server will
// later POST to, unauthenticated and from inside the deployment's network, so
// "any https URL" is a server-side request forgery primitive: an endpoint is
// only ever minted by a browser's push service, and those are these.
//
// Matching is on the host alone and never resolves a name — DNS would only
// add a TOCTOU window (a name that answers with a public address now can
// answer with 169.254.169.254 at send time). The allow-list IS the guard, so
// it holds names only: an address literal is refused outright, including one
// reached through the override.
var defaultPushEndpointHosts = []string{
	"fcm.googleapis.com",                // Chrome, Chromium, Android
	"*.push.apple.com",                  // Safari, iOS / iPadOS / macOS
	"*.notify.windows.com",              // Edge (Windows Notification Service)
	"push.services.mozilla.com",         // Firefox
	"updates.push.services.mozilla.com", // Firefox (current autopush host)
	"*.push.services.mozilla.com",       // Firefox (regional autopush hosts)
}

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
	userAgent = truncateRunes(userAgent, maxPushUserAgentLen)

	// Subscribe fills sub in from the PERSISTED row (the upsert returns it),
	// so a re-post of a device already on file answers with that row's id and
	// created_at rather than the ones generated a moment ago for a row that
	// was never inserted — the 201 body matches what GET lists.
	sub := pushsubs.New(userID, endpoint, p256dh, auth, userAgent)
	if err := h.pushSubService.Subscribe(sub); err != nil {
		respondInternal(w, r, "failed to store push subscription", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sub)
}

// truncateRunes cuts s to at most max runes. Slicing bytes instead would cut
// a multi-byte character in half — a user agent is free text from the browser
// and routinely carries non-ASCII — and store a string ending in an invalid
// UTF-8 sequence, which postgres refuses outright. Same rule as
// notify.truncate, minus the ellipsis: this is a device label, not prose.
func truncateRunes(s string, max int) string {
	// A string of at most max BYTES cannot hold more than max runes.
	if len(s) <= max || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// validatePushSubscription answers a client-facing message, or "" when the
// subscription is well-formed enough to store.
func validatePushSubscription(endpoint, p256dh, auth string) string {
	switch {
	case endpoint == "":
		return "endpoint is required"
	case len(endpoint) > maxPushEndpointLen:
		return "endpoint is too long"
	case p256dh == "" || auth == "":
		return "keys.p256dh and keys.auth are required"
	case len(p256dh) > maxPushKeyLen || len(auth) > maxPushKeyLen:
		return "keys are too long"
	}
	return validatePushEndpoint(endpoint)
}

// validatePushEndpoint answers "" when the endpoint is an https URL at a
// known push service, and a client-facing message otherwise. See
// defaultPushEndpointHosts for why the host is checked against a list rather
// than merely required to be https.
func validatePushEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		// Push services are https-only; anything else is a mistake or an
		// attempt to make the server post somewhere it should not.
		return "endpoint must be an https URL"
	}
	if u.User != nil {
		return "endpoint must not carry credentials"
	}
	if port := u.Port(); port != "" && port != "443" {
		return "endpoint must use the default https port"
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	// An address literal can never be a push service, and is the shape an
	// SSRF attempt takes (127.0.0.1, 169.254.169.254, 10.x). Refused before
	// the allow-list so an over-broad override cannot let one through.
	if net.ParseIP(host) != nil {
		return "endpoint must name a known push service"
	}
	if !pushEndpointHostAllowed(host) {
		return "endpoint is not a known push service; a self-hosted push service must be listed in " + envPushEndpointHosts
	}
	return ""
}

// pushEndpointHostAllowed matches a host against the built-in list plus the
// operator's additions. Read from the environment per call: the list is a few
// entries long, this runs once per subscribe, and it keeps the override
// changeable without a restart-shaped cache.
func pushEndpointHostAllowed(host string) bool {
	for _, pattern := range pushEndpointHostPatterns() {
		if matchPushEndpointHost(pattern, host) {
			return true
		}
	}
	return false
}

func pushEndpointHostPatterns() []string {
	patterns := append([]string(nil), defaultPushEndpointHosts...)
	for _, extra := range strings.Split(os.Getenv(envPushEndpointHosts), ",") {
		if extra = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(extra), "."))); extra != "" {
			patterns = append(patterns, extra)
		}
	}
	return patterns
}

// matchPushEndpointHost compares one pattern to one host. "*.example.com"
// matches any SUBDOMAIN of example.com and not example.com itself; anything
// else is an exact match.
func matchPushEndpointHost(pattern, host string) bool {
	if suffix, ok := strings.CutPrefix(pattern, "*"); ok {
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return host == pattern
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
