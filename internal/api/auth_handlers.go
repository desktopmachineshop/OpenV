package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// GoogleOAuthConfig enables "Sign in with Google" when configured.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string // e.g. https://host/api/v1/auth/google/callback
	// FrontendURL is where the callback redirects after login.
	FrontendURL string
}

func (g *GoogleOAuthConfig) oauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.ClientID,
		ClientSecret: g.ClientSecret,
		RedirectURL:  g.RedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func (h *Handler) registerAuthRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/auth/register", h.Register).Methods("POST")
	router.HandleFunc("/api/v1/auth/login", h.Login).Methods("POST")
	router.HandleFunc("/api/v1/auth/logout", h.Logout).Methods("POST")
	router.HandleFunc("/api/v1/auth/me", h.Me).Methods("GET")
	router.HandleFunc("/api/v1/auth/config", h.AuthConfig).Methods("GET")
	router.HandleFunc("/api/v1/auth/verify-email", h.VerifyEmail).Methods("POST")
	router.HandleFunc("/api/v1/auth/verify-email/resend", h.ResendVerification).Methods("POST")
	router.HandleFunc("/api/v1/auth/verify-email/change", h.ChangeVerificationEmail).Methods("POST")
	// Registration policy and invitation links: the login page has to know
	// whether the sign-up form exists at all, and an invite link must resolve
	// for a browser that holds no session yet.
	router.HandleFunc("/api/v1/auth/policy", h.AuthPolicy).Methods("GET")
	router.HandleFunc("/api/v1/auth/invitations/accept", h.AcceptInvitation).Methods("POST")
	router.HandleFunc("/api/v1/auth/invitations/{token}", h.PreviewInvitation).Methods("GET")
	router.HandleFunc("/api/v1/auth/google", h.GoogleLogin).Methods("GET")
	router.HandleFunc("/api/v1/auth/google/callback", h.GoogleCallback).Methods("GET")
	h.registerOIDCRoutes(router)

	router.HandleFunc("/api/v1/users", h.ListUsers).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/members", h.ListProjectMembers).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/members", h.AddProjectMember).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/members/{userId}", h.UpdateProjectMember).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/members/{userId}", h.RemoveProjectMember).Methods("DELETE")
}

// provisionPersonalWorkspace ensures a new user's personal org exists and is
// seeded with default agents/crew. Failures are non-fatal (retried on next
// login via the middleware's personal-org fallback).
func (h *Handler) provisionPersonalWorkspace(userID, displayName string) {
	if h.orgService == nil {
		return
	}
	org, created, err := h.orgService.EnsurePersonalOrg(userID, displayName)
	if err != nil {
		slog.Warn("failed to provision personal workspace", slog.String("user_id", userID), slog.Any("error", err))
		return
	}
	if created && h.orgSeeder != nil {
		if err := h.orgSeeder(org.ID); err != nil {
			slog.Warn("failed to seed personal workspace", slog.String("org_id", org.ID), slog.Any("error", err))
		}
	}
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	// The cookie's own expiry tracks the server's absolute session lifetime,
	// so a browser stops presenting a cookie the server would refuse anyway
	// (REQ-99). Idle expiry is not expressible in a cookie and stays a
	// server-side check.
	maxAge := h.sessionPolicy.Normalized().MaxAge
	http.SetCookie(w, &http.Cookie{
		Name:        SessionCookieName,
		Value:       token,
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		Expires:     time.Now().Add(maxAge),
		MaxAge:      int(maxAge.Seconds()),
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:        SessionCookieName,
		Value:       "",
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      -1,
	})
}

// partitionedCookies reports whether auth cookies carry the Partitioned
// attribute: only for cross-site deployments (SameSite=None), where a
// partitioned cookie is the one third-party cookie Chromium-based browsers
// still store. Same-site deployments must not set it — a partitioned cookie
// is keyed by the top-level site as well, which is pointless there.
func (h *Handler) partitionedCookies() bool {
	return h.cookieSameSite == http.SameSiteNoneMode
}

// AuthConfig tells the login page which sign-in methods are available.
func (h *Handler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"google_enabled":     h.googleOAuth != nil && h.googleOAuth.ClientID != "",
		"oidc_enabled":       h.oidc.Enabled(),
		"oidc_provider_name": "",
		// Tells the SPA whether an unverified account meets the wall, so it
		// never walls anyone on a deployment that cannot send the link.
		"email_verification_required": h.emailVerification.Required,
		// Whether the page should offer a sign-up form at all (REQ-95).
		"registration": h.registrationPolicy(),
	}
	if h.oidc.Enabled() {
		resp["oidc_provider_name"] = h.oidc.displayName()
	}
	json.NewEncoder(w).Encode(resp)
}

// AuthPolicy is the narrow public answer to "can I sign myself up here?".
// It exists beside AuthConfig so a client that only needs the policy — a
// deployment check, a script — does not have to read the sign-in methods.
func (h *Handler) AuthPolicy(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"registration": h.registrationPolicy()})
}

// registrationPolicy reports the deployment's policy, defaulting to open so
// a handler constructed without one (tests) behaves as it always did.
func (h *Handler) registrationPolicy() string {
	if h.registration == RegistrationClosed {
		return RegistrationClosed
	}
	return RegistrationOpen
}

// registrationAllowed reports whether this address may create an account.
// Open deployments allow everyone. A closed one has exactly one door here —
// a live invitation link whose address is the one being registered — and
// single sign-on, which never reaches this function because the identity
// provider is doing the admitting.
//
// A pending invitation for the address, WITHOUT its link, is deliberately
// not a door. It would answer 403-or-200 by whether an address has been
// invited, which turns the sign-up form into an oracle for who an admin has
// invited; and it would let whoever learns an invited address register it
// first and sit on it, so the real invitee finds their address taken.
//
// This is a door, not a grant: passing it creates the account. The
// membership the invitation names is granted separately, by
// acceptInvitationToken, and only for the address the token was issued to.
func (h *Handler) registrationAllowed(email, inviteToken string) bool {
	if h.registrationPolicy() == RegistrationOpen {
		return true
	}
	if h.invitationService == nil || inviteToken == "" {
		return false
	}
	// A lookup failure is treated as "no invitation": on a closed deployment
	// the safe answer to an unanswerable question is no.
	inv, err := h.invitationService.Lookup(inviteToken)
	if err != nil || inv == nil {
		return false
	}
	return inv.Email == users.NormalizeEmail(email)
}

// Register creates a password account and logs it in.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
		// InviteToken is the token from an invite link the sign-up form was
		// opened with (optional). It is what turns the invitation into a
		// membership here: holding it proves the invited mailbox was read.
		InviteToken string `json:"invite_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if ok, retryAfter := h.registerIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many accounts created from this address; try again later.", retryAfter)
		return
	}
	// On a closed deployment an invitation is the door: an invited address
	// registers normally, everyone else is turned away (REQ-95).
	if !h.registrationAllowed(req.Email, req.InviteToken) {
		writeJSONErrorCode(w, http.StatusForbidden, "registration is closed", ErrCodeRegistrationClosed)
		return
	}
	user, err := h.userService.Register(req.Email, req.Password, req.Name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.provisionPersonalWorkspace(user.ID, user.Name)
	// Only the invitation whose link this sign-up carried is taken up, and
	// only when it was issued to the address being registered: a membership
	// follows proof that the invited mailbox was read, never the mere claim
	// of an address. Someone who registered without the link uses it
	// afterwards, signed in, through POST /auth/invitations/accept.
	h.acceptInvitationToken(req.InviteToken, user)
	_, token, err := h.userService.Login(req.Email, req.Password)
	if err != nil {
		respondInternal(w, r, "failed to sign in after registration", err)
		return
	}
	h.setSessionCookie(w, token)
	// The link goes out in the background: registration never waits on SMTP,
	// and the wall's Resend covers a mail that did not arrive.
	if h.emailVerification.Required && !user.EmailVerified {
		h.sendVerificationAsync(user, user.Email)
	}
	json.NewEncoder(w).Encode(user)
}

// Login authenticates email/password credentials.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Every attempt charges the client address; only a failure charges the
	// account, so a targeted account closes after a few wrong passwords
	// while its owner, signing in correctly, is never locked out by them.
	ip := clientIP(r)
	if ok, retryAfter := h.authIPLimiter.allow(ip); !ok {
		writeRateLimited(w, "Too many sign-in attempts from this address; try again later.", retryAfter)
		return
	}
	account := accountKey(req.Email)
	if ok, retryAfter := h.authAccountLimiter.check(account); !ok {
		writeRateLimited(w, "Too many failed sign-in attempts for this account; try again later.", retryAfter)
		return
	}
	user, token, err := h.userService.Login(req.Email, req.Password)
	if err != nil {
		h.authAccountLimiter.penalize(account)
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	h.setSessionCookie(w, token)
	json.NewEncoder(w).Encode(user)
}

// accountKey normalises an email into the key its failed sign-ins are
// counted under, so "Dave@Example.com" and "dave@example.com" share one
// bucket.
func accountKey(email string) string {
	return users.NormalizeEmail(email)
}

// throttleSSO charges one SSO start or callback against the client address
// and answers 429 when the address has spent its budget. Returns false when
// the caller must stop.
func (h *Handler) throttleSSO(w http.ResponseWriter, r *http.Request) bool {
	if ok, retryAfter := h.ssoIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many sign-in attempts from this address; try again later.", retryAfter)
		return false
	}
	return true
}

// Logout invalidates the current session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		_ = h.userService.Logout(cookie.Value)
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// sessionUser resolves the browser session on an open auth route, where the
// middleware has not run. Cookie only: Bearer credentials are never accepted
// here, so a runner key or run token can neither read the account nor drive
// its verification. nil when there is no valid session.
func (h *Handler) sessionUser(r *http.Request) *users.User {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	user, err := h.userService.GetBySessionToken(cookie.Value)
	if err != nil {
		return nil
	}
	return user
}

// Me returns the authenticated user (401 when logged out).
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user := h.sessionUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	json.NewEncoder(w).Encode(user)
}

// GoogleLogin redirects to Google's consent screen.
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.googleOAuth == nil || h.googleOAuth.ClientID == "" {
		writeJSONError(w, http.StatusNotFound, "google sign-in is not configured")
		return
	}
	if !h.throttleSSO(w, r) {
		return
	}
	state, err := users.NewToken()
	if err != nil {
		respondInternal(w, r, "failed to start google sign-in", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:        "openv_oauth_state",
		Value:       state,
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      600,
	})
	http.Redirect(w, r, h.googleOAuth.oauthConfig().AuthCodeURL(state), http.StatusFound)
}

// GoogleCallback completes the OIDC code flow.
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	if h.googleOAuth == nil || h.googleOAuth.ClientID == "" {
		writeJSONError(w, http.StatusNotFound, "google sign-in is not configured")
		return
	}
	if !h.throttleSSO(w, r) {
		return
	}
	stateCookie, err := r.Cookie("openv_oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		writeJSONError(w, http.StatusBadRequest, "invalid oauth state")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSONError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	conf := h.googleOAuth.oauthConfig()
	oauthToken, err := conf.Exchange(ctx, code)
	if err != nil {
		respondError(w, r, http.StatusBadGateway, "google token exchange failed", err)
		return
	}

	client := conf.Client(ctx, oauthToken)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		respondError(w, r, http.StatusBadGateway, "google userinfo fetch failed", err)
		return
	}
	defer resp.Body.Close()

	var info struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		writeJSONError(w, http.StatusBadGateway, "invalid userinfo response")
		return
	}
	if info.Email == "" || !info.EmailVerified {
		writeJSONError(w, http.StatusForbidden, "google account email is missing or unverified")
		return
	}

	googleUser, token, err := h.userService.LoginWithGoogle(info.Email, info.Name, info.Picture)
	if err != nil {
		// An email already registered via a different sign-in method is a
		// client-visible 409, not a 500 (issue #242): we refuse to auto-link.
		if errors.Is(err, users.ErrProviderMismatch) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		respondInternal(w, r, "failed to sign in with google", err)
		return
	}
	h.provisionPersonalWorkspace(googleUser.ID, googleUser.Name)
	// Single sign-on is never subject to the registration policy — the IdP is
	// doing the admitting — and an invited address joins its workspaces on
	// the way in. Google's userinfo is refused above unless email_verified is
	// true, which is the proof of control this join rests on.
	h.acceptInvitationsForProviderVerifiedEmail(googleUser.ID, googleUser.Email)
	h.setSessionCookie(w, token)

	dest := h.googleOAuth.FrontendURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// ListUsers returns the active workspace's members (for member invitations);
// the global user directory is never exposed.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	out := []map[string]string{}
	if orgID := ActiveOrg(r); orgID != "" {
		list, err := h.orgService.ListMembers(orgID)
		if err != nil {
			respondInternal(w, r, "failed to list workspace members", err)
			return
		}
		for _, m := range list {
			out = append(out, map[string]string{
				"id":         m.UserID,
				"name":       m.UserName,
				"email":      m.UserEmail,
				"avatar_url": m.AvatarURL,
			})
		}
	}
	json.NewEncoder(w).Encode(out)
}

// ListProjectMembers returns a project's members.
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.memberService.ListMembers(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list project members", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// AddProjectMember invites an existing user by email.
func (h *Handler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := h.userService.FindByEmail(req.Email)
	if err != nil {
		respondInternal(w, r, "failed to look up user", err)
		return
	}
	if user == nil {
		writeJSONError(w, http.StatusNotFound, "no user with that email — they must sign up first")
		return
	}
	if err := h.memberService.AddMember(projectID, user.ID, req.Role); err != nil {
		if errors.Is(err, members.ErrInvalidRole) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to add member", err)
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// UpdateProjectMember changes a member's role.
func (h *Handler) UpdateProjectMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	projectID := vars["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.memberService.SetRole(projectID, vars["userId"], req.Role); err != nil {
		if errors.Is(err, members.ErrInvalidRole) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to update member role", err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveProjectMember removes a member.
func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	projectID := vars["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	if err := h.memberService.RemoveMember(projectID, vars["userId"]); err != nil {
		respondInternal(w, r, "failed to remove member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
