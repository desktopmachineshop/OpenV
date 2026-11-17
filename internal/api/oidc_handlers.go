package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2"

	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Cookies carrying the CSRF state, the OIDC replay-binding nonce and the PKCE
// code verifier between the login redirect and the callback. Short-lived and
// HttpOnly, mirroring the Google flow's state cookie.
const (
	oidcStateCookie    = "openv_oidc_state"
	oidcNonceCookie    = "openv_oidc_nonce"
	oidcVerifierCookie = "openv_oidc_pkce"
)

// OIDCConfig enables generic OIDC single sign-on ("Sign in with SSO") when
// configured. It is strictly opt-in: with no issuer/client set the endpoints
// report "not configured" and nothing else about the deployment changes.
//
// Design (issue #225): one identity provider per deployment, configured via
// environment. The issue's ideal is per-org OIDC config; env-level generic OIDC
// is the pragmatic MVP that covers the self-hosted single-IdP 80% case. Per-org
// configuration is a documented follow-up (it needs a config table, an admin
// UI, and host-based issuer routing — out of scope here).
//
// ID-token verification: we use coreos/go-oidc to (a) discover the provider's
// endpoints from the issuer's /.well-known/openid-configuration and (b) verify
// the ID token's signature (via the provider JWKS), issuer, audience, and the
// nonce. This is preferred over the userinfo-after-code-exchange shortcut the
// Google flow uses because it lets us bind and verify the nonce, closing ID
// token replay in addition to the state CSRF check.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string   // e.g. https://host/api/v1/auth/oidc/callback
	Scopes       []string // defaults to openid, email, profile
	ProviderName string   // display label for the button, e.g. "Okta"; defaults to "SSO"
	// FrontendURL is where the callback redirects after a successful login.
	FrontendURL string
	// Only makes this provider the only way in (OPENV_OIDC_ONLY): password
	// sign-in, sign-up and reset and Google refuse, and the sign-in page goes
	// straight to the provider. Linking a provider to a password account
	// (auth_link_handlers.go) still asks for the password once.
	Only bool

	// Lazily-initialized discovery state. Discovery makes a network call to the
	// issuer, so it is deferred to first use (and retried on each use until it
	// succeeds) rather than run at boot — a temporarily unreachable IdP must not
	// block server startup.
	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

// Enabled reports whether OIDC is configured (issuer + client id present). It
// does not perform discovery, so it stays cheap for the auth-config endpoint
// and mirrors how the Google button's visibility is decided.
func (c *OIDCConfig) Enabled() bool {
	return c != nil && c.Issuer != "" && c.ClientID != ""
}

// displayName is the human label for the sign-in button.
func (c *OIDCConfig) displayName() string {
	if c.ProviderName != "" {
		return c.ProviderName
	}
	return "SSO"
}

// ensure performs OIDC discovery once and builds the oauth2 config and ID-token
// verifier. Safe for concurrent callers; re-attempts until discovery succeeds.
func (c *OIDCConfig) ensure(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return err
	}
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	c.provider = provider
	c.oauth = &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}
	c.verifier = provider.Verifier(&oidc.Config{ClientID: c.ClientID})
	return nil
}

func (h *Handler) registerOIDCRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/auth/oidc/login", h.OIDCLogin).Methods("GET")
	router.HandleFunc("/api/v1/auth/oidc/callback", h.OIDCCallback).Methods("GET")
	router.HandleFunc("/api/v1/auth/oidc/account-deletion", h.OIDCAccountDeletion).Methods("POST")
}

// OIDCLogin redirects to the configured provider's authorization endpoint with
// a fresh state (CSRF), nonce (ID-token replay binding) and PKCE challenge
// (S256, binding the code to this browser; some providers, the Desktop Machine
// Shop sign-in among them, require it), each secret stored in a short-lived
// HttpOnly cookie for the callback.
//
// ?signup=1 asks the provider for its sign-up page with prompt=create
// (OpenID Connect Prompt Create 1.0). It is sent only when asked for: a
// provider that does not know the value may refuse the request.
func (h *Handler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	if !h.OIDC.Enabled() {
		writeJSONError(w, http.StatusNotFound, "sso sign-in is not configured")
		return
	}
	if !h.throttleSSO(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.OIDC.ensure(ctx); err != nil {
		respondError(w, r, http.StatusBadGateway, "sso provider discovery failed", err)
		return
	}

	state, err := users.NewToken()
	if err != nil {
		respondInternal(w, r, "failed to start sso sign-in", err)
		return
	}
	nonce, err := users.NewToken()
	if err != nil {
		respondInternal(w, r, "failed to start sso sign-in", err)
		return
	}
	verifier := oauth2.GenerateVerifier()
	h.setOIDCFlowCookie(w, oidcStateCookie, state)
	h.setOIDCFlowCookie(w, oidcNonceCookie, nonce)
	h.setOIDCFlowCookie(w, oidcVerifierCookie, verifier)

	opts := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if r.URL.Query().Get("signup") == "1" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "create"))
	}
	http.Redirect(w, r, h.OIDC.oauth.AuthCodeURL(state, opts...), http.StatusFound)
}

// oidcProviderRefused ends a flow the provider reports did not happen
// (RFC 6749 4.1.2.1, ?error=): the person cancelled (access_denied), or it
// refused. The flow's cookies are spent either way.
func (h *Handler) oidcProviderRefused(w http.ResponseWriter, r *http.Request, perr string) {
	h.clearOIDCFlowCookie(w, oidcStateCookie)
	h.clearOIDCFlowCookie(w, oidcNonceCookie)
	h.clearOIDCFlowCookie(w, oidcVerifierCookie)
	code := ssoErrFailed
	if perr == "access_denied" {
		code = ssoErrCancelled
	}
	h.oidcCallbackFailed(w, r, code, "the provider answered "+perr, nil)
}

// oidcFlowCookies reads the callback's flow cookies: the state, which must
// match the query's, the nonce and the PKCE verifier. why says what is
// wrong, "" when nothing is.
func oidcFlowCookies(r *http.Request) (nonce, verifier, why string) {
	state, err := r.Cookie(oidcStateCookie)
	if err != nil || state.Value == "" || state.Value != r.URL.Query().Get("state") {
		return "", "", "invalid oauth state"
	}
	n, err := r.Cookie(oidcNonceCookie)
	if err != nil || n.Value == "" {
		return "", "", "missing oauth nonce"
	}
	v, err := r.Cookie(oidcVerifierCookie)
	if err != nil || v.Value == "" {
		return "", "", "missing oauth code verifier"
	}
	return n.Value, v.Value, ""
}

// OIDCCallback completes the code flow: it validates state, exchanges the code,
// verifies the ID token (signature/issuer/audience/nonce), finds-or-creates the
// user by verified email, and issues the session cookie exactly like Google.
// A sign-on that fails goes back to the sign-in page with ?error= (see
// oidcCallbackFailed); only an unconfigured provider and the throttle answer
// JSON.
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !h.OIDC.Enabled() {
		writeJSONError(w, http.StatusNotFound, "sso sign-in is not configured")
		return
	}
	if !h.throttleSSO(w, r) {
		return
	}
	q := r.URL.Query()
	if perr := q.Get("error"); perr != "" {
		h.oidcProviderRefused(w, r, perr)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := h.OIDC.ensure(ctx); err != nil {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "provider discovery failed", err)
		return
	}

	nonce, verifier, why := oidcFlowCookies(r)
	if why != "" {
		h.oidcCallbackFailed(w, r, ssoErrFailed, why, nil)
		return
	}
	// One-shot: clear the flow cookies so a state/nonce pair cannot be replayed.
	h.clearOIDCFlowCookie(w, oidcStateCookie)
	h.clearOIDCFlowCookie(w, oidcNonceCookie)
	h.clearOIDCFlowCookie(w, oidcVerifierCookie)

	code := q.Get("code")
	if code == "" {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "missing authorization code", nil)
		return
	}

	oauthToken, err := h.OIDC.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "token exchange failed", err)
		return
	}
	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "the response is missing an id_token", nil)
		return
	}
	idToken, err := h.OIDC.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "id_token verification failed", err)
		return
	}
	if idToken.Nonce != nonce {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "invalid oauth nonce", nil)
		return
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		h.oidcCallbackFailed(w, r, ssoErrFailed, "invalid id_token claims", err)
		return
	}
	if claims.Email == "" {
		h.oidcCallbackFailed(w, r, ssoErrUnverified, "the id_token has no email claim", nil)
		return
	}
	// Require a positively-verified email (issue #242). An account is provisioned
	// and matched by this email, so an unverified — or absent — email_verified
	// claim would let anyone who can make the IdP mint a token for an
	// address-they-do-not-own take over that address's account. Reject unless the
	// IdP asserts email_verified == true.
	if claims.EmailVerified == nil || !*claims.EmailVerified {
		h.oidcCallbackFailed(w, r, ssoErrUnverified, "the email is not verified by the identity provider", nil)
		return
	}

	user, token, err := h.UserService.LoginWithSSO(users.ProviderOIDC, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		// A cross-provider collision (an account with this email that signed up
		// via a different method): a password account is offered the link,
		// any other is a client-visible 409, not a 500.
		if errors.Is(err, users.ErrProviderMismatch) {
			h.offerIdentityLink(w, r, users.ProviderOIDC, claims.Email, h.OIDC.FrontendURL)
			return
		}
		h.oidcCallbackFailed(w, r, ssoErrFailed, "failed to sign in", err)
		return
	}
	h.provisionPersonalWorkspace(user.ID, user.Name)
	// The registration policy never applies to single sign-on (the IdP is
	// doing the admitting), and an invited address joins its workspaces here.
	// This is reached only past the email_verified check above: the provider
	// asserting the address as verified is the proof of control that a
	// membership requires. An unverified — or absent — claim never gets here.
	h.acceptInvitationsForProviderVerifiedEmail(user.ID, user.Email)
	h.setSessionCookie(w, token)

	dest := h.OIDC.FrontendURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// OIDCAccountDeletion verifies a deletion token and removes the account its
// email names: 200 deleted or not_found, 409 blocked with a reason for the
// person, 400 invalid for any token that does not verify. Only on a
// deployment with OIDC configured; 404 otherwise.
func (h *Handler) OIDCAccountDeletion(w http.ResponseWriter, r *http.Request) {
	if !h.OIDC.Enabled() || h.AccountDeleter == nil {
		writeJSONError(w, http.StatusNotFound, "sso sign-in is not configured")
		return
	}
	if ok, retryAfter := h.authIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many attempts from this address; try again later.", retryAfter)
		return
	}
	invalid := func(why string, err error) {
		slog.Warn("account deletion: request refused", "reason", why, "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "invalid"})
	}
	r.Body = http.MaxBytesReader(w, r.Body, accountDeletionMaxBody)
	if err := r.ParseForm(); err != nil {
		invalid("unreadable form", err)
		return
	}
	raw := strings.TrimSpace(r.PostForm.Get("deletion_token"))
	if raw == "" {
		invalid("no deletion_token", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.OIDC.ensure(ctx); err != nil {
		respondError(w, r, http.StatusBadGateway, "sso provider discovery failed", err)
		return
	}
	email, err := h.OIDC.verifyDeletionToken(ctx, raw)
	if err != nil {
		invalid("token", err)
		return
	}

	out, err := h.AccountDeleter.DeleteAccount(email)
	if err != nil {
		respondInternal(w, r, "failed to remove the account", err)
		return
	}
	switch out.Status {
	case users.DeletionBlocked:
		slog.Info("account deletion: blocked", "user_id", out.UserID, "kind", out.Blocked.Kind)
		writeJSON(w, http.StatusConflict, map[string]string{"status": "blocked", "reason": out.Blocked.Reason()})
		return
	case users.DeletionNotFound:
		slog.Info("account deletion: no account for the address")
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_found"})
		return
	}
	removeStoredFiles(out.Files)
	h.recordAccountRemoved(out)
	// The audit line names the account by id only, never by address.
	slog.Info("account deletion: account removed at the identity provider's request",
		"user_id", out.UserID, "workspaces_purged", len(out.PurgedWorkspaces), "workspaces_left", len(out.LeftWorkspaces))
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// recordAccountRemoved records, in the history of each workspace a removed
// account left, its leaving as its own: admins are told who went, and
// nobody is told twice.
func (h *Handler) recordAccountRemoved(out *users.AccountDeletion) {
	for _, orgID := range out.LeftWorkspaces {
		h.publishOrgEventAs("user:"+out.UserID, events.OrgMemberRemoved, orgID, out.UserID, map[string]interface{}{
			"user_id": out.UserID,
			"self":    true,
		})
	}
}
