package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
)

// The OAuth 2.1 authorization server of the remote MCP endpoint (REQ-44),
// in the profile the MCP authorization spec names: protected resource
// metadata (RFC 9728) pointing at this server's metadata (RFC 8414), dynamic
// client registration (RFC 7591) for public clients, the authorization code
// grant with PKCE S256 only, rotating refresh tokens, and revocation (RFC
// 7009). The consent itself is the app's page (/connect), which posts
// the member's decision to connected_app_handlers.go: a page served from
// here could run no script, and a cross-site session cookie would not
// reach it.
//
// These paths are open in the auth middleware: none of them needs a
// session, and each answers in OAuth's own error shape, {"error": code}.

// registerOAuthRoutes wires the OAuth endpoints. With no ConnectedApps
// service they answer 404, as an unknown path does.
func (h *Handler) registerOAuthRoutes(router *mux.Router) {
	router.HandleFunc("/.well-known/oauth-protected-resource", h.OAuthProtectedResource).Methods("GET")
	router.HandleFunc("/.well-known/oauth-protected-resource/mcp", h.OAuthProtectedResource).Methods("GET")
	router.HandleFunc("/.well-known/oauth-authorization-server", h.OAuthAuthorizationServer).Methods("GET")
	router.HandleFunc("/oauth/register", h.OAuthRegister).Methods("POST")
	router.HandleFunc("/oauth/authorize", h.OAuthAuthorize).Methods("GET")
	router.HandleFunc("/oauth/token", h.OAuthToken).Methods("POST")
	router.HandleFunc("/oauth/revoke", h.OAuthRevoke).Methods("POST")
}

// oauthError writes an OAuth error body (RFC 6749 5.2).
func oauthError(w http.ResponseWriter, status int, code, description string) {
	body := map[string]string{"error": code}
	if description != "" {
		body["error_description"] = description
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, body)
}

var oauthScopes = []string{connectedapps.ScopeRead, connectedapps.ScopeWrite}

// OAuthProtectedResource is the protected resource metadata: what the MCP
// endpoint is and which server issues its tokens.
func (h *Handler) OAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	issuer := h.oauthIssuer()
	writeJSONOK(w, map[string]interface{}{
		"resource":                 issuer + "/mcp",
		"authorization_servers":    []string{issuer},
		"scopes_supported":         oauthScopes,
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "OpenV",
	})
}

// OAuthAuthorizationServer is the authorization server metadata.
func (h *Handler) OAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	issuer := h.oauthIssuer()
	writeJSONOK(w, map[string]interface{}{
		"issuer":                                         issuer,
		"authorization_endpoint":                         issuer + "/oauth/authorize",
		"token_endpoint":                                 issuer + "/oauth/token",
		"registration_endpoint":                          issuer + "/oauth/register",
		"revocation_endpoint":                            issuer + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"scopes_supported":                               oauthScopes,
		"authorization_response_iss_parameter_supported": true,
	})
}

// OAuthRegister registers a public client (RFC 7591). Whatever
// authentication method the client asks for, it is registered as public,
// with no secret, and the answer says so.
func (h *Handler) OAuthRegister(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	if ok, _ := h.oauthLimiter.allow("register:" + clientIP(r)); !ok {
		oauthError(w, http.StatusTooManyRequests, "invalid_request", "too many registrations from this address; try again later")
		return
	}
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body must be a JSON client registration")
		return
	}
	c, err := h.ConnectedApps.Register(req.ClientName, req.RedirectURIs)
	if errors.Is(err, connectedapps.ErrInvalidRedirect) {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri",
			"give one to five redirect URIs, each https, http on localhost, or a private-use scheme, with no fragment")
		return
	}
	if err != nil {
		respondInternal(w, r, "failed to register the client", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.CreatedAt.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// OAuthAuthorize checks an authorization request and sends the browser to
// the app's consent page with it. A request whose client or redirect URI is
// wrong is answered here, never redirected: sending the browser to an
// address the client did not register is what the check exists to stop.
// Anything else wrong goes back to the client as an error redirect.
func (h *Handler) OAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	q := r.URL.Query()
	clientID, redirectURI, state := q.Get("client_id"), q.Get("redirect_uri"), q.Get("state")
	if _, err := h.ConnectedApps.Authorizing(clientID, redirectURI); err != nil {
		if errors.Is(err, connectedapps.ErrInvalidClient) || errors.Is(err, connectedapps.ErrInvalidRedirect) {
			oauthError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		respondInternal(w, r, "failed to check the client", err)
		return
	}
	fail := func(code, description string) {
		http.Redirect(w, r, oauthRedirect(redirectURI, map[string]string{
			"error": code, "error_description": description, "state": state, "iss": h.oauthIssuer(),
		}), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only the authorization code flow is supported")
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with code_challenge_method S256 is required")
		return
	}
	if _, err := connectedapps.NormalizeScope(q.Get("scope")); err != nil {
		fail("invalid_scope", "the scopes are openv:read and openv:write")
		return
	}
	consent := url.Values{}
	for _, k := range []string{"client_id", "redirect_uri", "code_challenge", "code_challenge_method", "state", "scope"} {
		if v := q.Get(k); v != "" {
			consent.Set(k, v)
		}
	}
	http.Redirect(w, r, h.frontendURL+"/connect?"+consent.Encode(), http.StatusFound)
}

// OAuthToken spends a code or a refresh token for a token pair. The body is
// form-encoded, as RFC 6749 has it.
func (h *Handler) OAuthToken(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the body must be form-encoded")
		return
	}
	// Spent per client, not per address: a hosted app such as Claude sends
	// every one of its users' token requests from the same few addresses.
	key := "token:" + clientIP(r)
	if id, err := uuid.Parse(r.PostForm.Get("client_id")); err == nil {
		key = "token:client:" + id.String()
	}
	if ok, _ := h.oauthLimiter.allow(key); !ok {
		oauthError(w, http.StatusTooManyRequests, "invalid_request", "too many token requests for this client; try again later")
		return
	}
	var (
		tok *connectedapps.Tokens
		err error
	)
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		tok, err = h.ConnectedApps.Exchange(r.PostForm.Get("code"), r.PostForm.Get("client_id"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"))
	case "refresh_token":
		tok, err = h.ConnectedApps.Refresh(r.PostForm.Get("refresh_token"), r.PostForm.Get("client_id"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type is authorization_code or refresh_token")
		return
	}
	switch {
	case errors.Is(err, connectedapps.ErrInvalidRequest):
		oauthError(w, http.StatusBadRequest, "invalid_request", "code, client_id, redirect_uri and a code_verifier of 43 to 128 characters are required")
		return
	case errors.Is(err, connectedapps.ErrInvalidClient):
		oauthError(w, http.StatusUnauthorized, "invalid_client", "no client is registered under this client_id; register again")
		return
	case errors.Is(err, connectedapps.ErrInvalidGrant):
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code or refresh token is not valid for this client")
		return
	case err != nil:
		respondInternal(w, r, "failed to issue tokens", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSONOK(w, tok)
}

// OAuthRevoke ends a token pair (RFC 7009). It answers 200 whether or not
// the token existed, so it tells a caller nothing about tokens it does not
// hold.
func (h *Handler) OAuthRevoke(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "the body must be form-encoded")
		return
	}
	if err := h.ConnectedApps.Revoke(r.PostForm.Get("token"), r.PostForm.Get("client_id")); err != nil {
		respondInternal(w, r, "failed to revoke the token", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
