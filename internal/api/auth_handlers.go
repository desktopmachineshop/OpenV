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

	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/invitations"
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
	// The token travels in the body, not the path: an invite link is a
	// credential, and a URL is logged (see PreviewInvitation).
	router.HandleFunc("/api/v1/auth/invitations/preview", h.PreviewInvitation).Methods("POST")
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
	if h.OrgService == nil {
		return
	}
	org, created, err := h.OrgService.EnsurePersonalOrg(userID, displayName)
	if err != nil {
		slog.Warn("failed to provision personal workspace", slog.String("user_id", userID), slog.Any("error", err))
		return
	}
	if created && h.OrgSeeder != nil {
		if err := h.OrgSeeder(org.ID); err != nil {
			slog.Warn("failed to seed personal workspace", slog.String("org_id", org.ID), slog.Any("error", err))
		}
	}
}

// AuthConfig tells the login page which sign-in methods are available.
func (h *Handler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"google_enabled":     h.GoogleOAuth != nil && h.GoogleOAuth.ClientID != "",
		"oidc_enabled":       h.OIDC.Enabled(),
		"oidc_provider_name": "",
		// Tells the SPA whether an unverified account meets the wall, so it
		// never walls anyone on a deployment that cannot send the link.
		"email_verification_required": h.EmailVerification.Required,
		// Whether the sign-in page can offer an emailed password reset
		// (REQ-158); without a mailer a platform admin mints the link.
		"password_reset_email": h.Mailer != nil && h.Mailer.Enabled(),
		// Whether the page should offer a sign-up form at all (REQ-95).
		"registration": h.registrationPolicy(),
	}
	if h.OIDC.Enabled() {
		resp["oidc_provider_name"] = h.OIDC.displayName()
	}
	writeJSONBare(w, resp)
}

// AuthPolicy is the narrow public answer to "can I sign myself up here?".
// It exists beside AuthConfig so a client that only needs the policy — a
// deployment check, a script — does not have to read the sign-in methods.
//
// min_password_length is the server's rule, published so the sign-up form
// and the change-password form state the length the server will actually
// enforce instead of a copy of it that can drift.
func (h *Handler) AuthPolicy(w http.ResponseWriter, r *http.Request) {
	writeJSONBare(w, map[string]any{
		"registration":        h.registrationPolicy(),
		"min_password_length": users.MinPasswordLength,
	})
}

// registrationPolicy reports the deployment's policy, defaulting to open so
// a handler constructed without one (tests) behaves as it always did.
func (h *Handler) registrationPolicy() string {
	if h.Registration == RegistrationClosed {
		return RegistrationClosed
	}
	return RegistrationOpen
}

// registrationAllowed reports whether this address may create an account,
// AND hands back the invitation its link resolved to, so the token is looked
// up exactly once per sign-up. Two lookups — one to open the door, one to
// grant the membership — leave a window in which the invitation is revoked
// in between: the account is created because it "passed", and then joins
// nothing, which is the confusing half-outcome this signature removes. The
// membership is still claimed separately (the caller accepts by the
// invitation returned here), so a revoke that lands after this read fails
// cleanly there rather than granting anything.
//
// outcome is what to tell the caller about the token they supplied: "" when
// it resolved (or when there was none), InviteOutcomeInvalid for a link that
// is unknown, revoked, spent or expired, InviteOutcomeEmailMismatch for a
// live link issued to a different address.
//
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
// acceptResolvedInvitation, and only for the address the token was issued to.
func (h *Handler) registrationAllowed(email, inviteToken string) (bool, *invitations.Invitation, string) {
	open := h.registrationPolicy() == RegistrationOpen
	if inviteToken == "" {
		return open, nil, ""
	}
	if h.InvitationService == nil {
		return open, nil, InviteOutcomeInvalid
	}
	// A lookup failure is treated as "no invitation": on a closed deployment
	// the safe answer to an unanswerable question is no.
	inv, err := h.InvitationService.Lookup(inviteToken)
	if err != nil || inv == nil {
		return open, nil, InviteOutcomeInvalid
	}
	if inv.Email != users.NormalizeEmail(email) {
		return open, nil, InviteOutcomeEmailMismatch
	}
	return true, inv, ""
}

// registerResponse is the created account plus, when the sign-up carried an
// invite token, what that token did (see the InviteOutcome constants). The
// user's own fields stay at the top level, so a client that ignores the
// outcome reads exactly the body it always did.
type registerResponse struct {
	*users.User
	Invitation string `json:"invitation,omitempty"`
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
	if !decodeJSON(w, r, &req) {
		return
	}
	if ok, retryAfter := h.registerIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many accounts created from this address; try again later.", retryAfter)
		return
	}
	// On a closed deployment an invitation is the door: an invited address
	// registers normally, everyone else is turned away (REQ-95). The token
	// is resolved once, here, and the membership is granted from what it
	// resolved to.
	allowed, invite, outcome := h.registrationAllowed(req.Email, req.InviteToken)
	if !allowed {
		writeJSONErrorCode(w, http.StatusForbidden, "registration is closed", ErrCodeRegistrationClosed)
		return
	}
	user, err := h.UserService.Register(req.Email, req.Password, req.Name)
	if err != nil {
		// A password under the minimum carries the code a password change
		// and a reset give it (#379's bug 20, OpenV REQ-18); sign-in's
		// refusal stays the one generic answer.
		if errors.Is(err, users.ErrWeakPassword) {
			writeJSONErrorCode(w, http.StatusBadRequest, err.Error(), ErrCodeWeakPassword)
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.provisionPersonalWorkspace(user.ID, user.Name)
	// Only the invitation whose link this sign-up carried is taken up, and
	// only when it was issued to the address being registered: a membership
	// follows proof that the invited mailbox was read, never the mere claim
	// of an address. Someone who registered without the link uses it
	// afterwards, signed in, through POST /auth/invitations/accept.
	if invite != nil {
		outcome = h.acceptResolvedInvitation(invite, user)
		user = h.verifiedByInvitation(user, outcome)
	}
	_, token, err := h.UserService.Login(req.Email, req.Password)
	if err != nil {
		respondInternal(w, r, "failed to sign in after registration", err)
		return
	}
	h.setSessionCookie(w, token)
	// The link goes out in the background: registration never waits on SMTP,
	// and the wall's Resend covers a mail that did not arrive.
	if h.EmailVerification.Required && !user.EmailVerified {
		h.sendVerificationAsync(user, user.Email)
	}
	writeJSONBare(w, registerResponse{User: user, Invitation: outcome})
}

// verifiedByInvitation marks the new account's address verified when an
// invitation token it carried was taken up. The token was mailed to that
// address and nowhere else, so presenting it proves the mailbox was read —
// the same proof an emailed verification link provides, and the same one an
// identity provider asserts for LoginWithSSO, which verifies its accounts
// from the start for exactly this reason.
//
// Without this a closed, verification-required deployment walls its invitees
// behind a SECOND mail immediately after they followed the first, which is
// both pointless and the most likely place to lose somebody. A failure here
// is not fatal: the account exists, and the wall's Resend still works.
func (h *Handler) verifiedByInvitation(user *users.User, outcome string) *users.User {
	if user == nil || user.EmailVerified {
		return user
	}
	if outcome != InviteOutcomeAccepted && outcome != InviteOutcomeAlreadyMember {
		return user
	}
	verified, err := h.UserService.MarkEmailVerified(user.ID)
	if err != nil {
		slog.Warn("invitation: could not mark an invited address verified",
			slog.String("user_id", user.ID), slog.Any("error", err))
		return user
	}
	return verified
}

// Login authenticates email/password credentials.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
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
	user, token, err := h.UserService.Login(req.Email, req.Password)
	if err != nil {
		h.authAccountLimiter.penalize(account)
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	h.setSessionCookie(w, token)
	writeJSONBare(w, user)
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
		_ = h.UserService.Logout(cookie.Value)
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
	user, err := h.UserService.GetBySessionToken(cookie.Value)
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
	writeJSONBare(w, user)
}

// GoogleLogin redirects to Google's consent screen.
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.GoogleOAuth == nil || h.GoogleOAuth.ClientID == "" {
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
	http.Redirect(w, r, h.GoogleOAuth.oauthConfig().AuthCodeURL(state), http.StatusFound)
}

// GoogleCallback completes the OIDC code flow.
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	if h.GoogleOAuth == nil || h.GoogleOAuth.ClientID == "" {
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
	conf := h.GoogleOAuth.oauthConfig()
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

	googleUser, token, err := h.UserService.LoginWithGoogle(info.Email, info.Name, info.Picture)
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

	dest := h.GoogleOAuth.FrontendURL
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// ListUsers returns the active workspace's members (for member invitations);
// the global user directory is never exposed.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	if requireUserMsg(w, r, "authentication required", http.StatusUnauthorized) == nil {
		return
	}
	out := []map[string]string{}
	if orgID := ActiveOrg(r); orgID != "" {
		list, err := h.OrgService.ListMembers(orgID)
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
	writeJSONBare(w, out)
}

// ListProjectMembers returns a project's members.
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.MemberService.ListMembers(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list project members", err)
		return
	}
	writeJSONBare(w, list)
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
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := h.UserService.FindByEmail(req.Email)
	if err != nil {
		respondInternal(w, r, "failed to look up user", err)
		return
	}
	if user == nil {
		writeJSONError(w, http.StatusNotFound, "no user with that email — they must sign up first")
		return
	}
	if err := h.MemberService.AddMember(projectID, user.ID, req.Role); err != nil {
		switch {
		case errors.Is(err, members.ErrInvalidRole):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, members.ErrUnknownProject):
			// A project gone since the guard: its 404 (bug 90).
			unknownProject.write(w)
		default:
			respondInternal(w, r, "failed to add member", err)
		}
		return
	}
	h.publish(r, events.ProjectMemberAdded, projectID, user.ID, map[string]interface{}{
		"user_id": user.ID,
		"role":    req.Role,
	})
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
	if !decodeJSON(w, r, &req) {
		return
	}
	previous, _ := h.MemberService.RoleFor(projectID, vars["userId"])
	if err := h.MemberService.SetRole(projectID, vars["userId"], req.Role); err != nil {
		switch {
		case errors.Is(err, members.ErrInvalidRole):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, members.ErrUnknownUser):
			// An account no row has, or an id that is not one: the 404 of any
			// lookup of an account (bug 15).
			writeJSONError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, members.ErrUnknownProject):
			// A project gone since the guard: its 404 (bug 90).
			unknownProject.write(w)
		default:
			respondInternal(w, r, "failed to update member role", err)
		}
		return
	}
	h.publish(r, events.ProjectMemberRoleChanged, projectID, vars["userId"], map[string]interface{}{
		"user_id": vars["userId"],
		"from":    previous,
		"to":      req.Role,
	})
	w.WriteHeader(http.StatusNoContent)
}

// RemoveProjectMember removes a member.
func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	projectID := vars["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	if err := h.MemberService.RemoveMember(projectID, vars["userId"]); err != nil {
		respondInternal(w, r, "failed to remove member", err)
		return
	}
	self := false
	if u := CurrentUser(r); u != nil {
		self = u.ID == vars["userId"]
	}
	h.publish(r, events.ProjectMemberRemoved, projectID, vars["userId"], map[string]interface{}{
		"user_id": vars["userId"],
		"self":    self,
	})
	w.WriteHeader(http.StatusNoContent)
}
