package api

import (
	"context"
	"encoding/json"
	"fmt"
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
	router.HandleFunc("/api/v1/auth/google", h.GoogleLogin).Methods("GET")
	router.HandleFunc("/api/v1/auth/google/callback", h.GoogleCallback).Methods("GET")

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
		fmt.Printf("Warning: failed to provision personal workspace: %v\n", err)
		return
	}
	if created && h.orgSeeder != nil {
		if err := h.orgSeeder(org.ID); err != nil {
			fmt.Printf("Warning: failed to seed personal workspace: %v\n", err)
		}
	}
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(users.SessionDuration),
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// AuthConfig tells the login page which sign-in methods are available.
func (h *Handler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]bool{
		"google_enabled": h.googleOAuth != nil && h.googleOAuth.ClientID != "",
	})
}

// Register creates a password account and logs it in.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user, err := h.userService.Register(req.Email, req.Password, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.provisionPersonalWorkspace(user.ID, user.Name)
	_, token, err := h.userService.Login(req.Email, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.setSessionCookie(w, token)
	json.NewEncoder(w).Encode(user)
}

// Login authenticates email/password credentials.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user, token, err := h.userService.Login(req.Email, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	h.setSessionCookie(w, token)
	json.NewEncoder(w).Encode(user)
}

// Logout invalidates the current session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		_ = h.userService.Logout(cookie.Value)
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the authenticated user (401 when logged out).
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	user, err := h.userService.GetBySessionToken(cookie.Value)
	if err != nil || user == nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	json.NewEncoder(w).Encode(user)
}

// GoogleLogin redirects to Google's consent screen.
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.googleOAuth == nil || h.googleOAuth.ClientID == "" {
		http.Error(w, "google sign-in is not configured", http.StatusNotFound)
		return
	}
	state, err := users.NewToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "openv_oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, h.googleOAuth.oauthConfig().AuthCodeURL(state), http.StatusFound)
}

// GoogleCallback completes the OIDC code flow.
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	if h.googleOAuth == nil || h.googleOAuth.ClientID == "" {
		http.Error(w, "google sign-in is not configured", http.StatusNotFound)
		return
	}
	stateCookie, err := r.Cookie("openv_oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	conf := h.googleOAuth.oauthConfig()
	oauthToken, err := conf.Exchange(ctx, code)
	if err != nil {
		http.Error(w, fmt.Sprintf("token exchange failed: %v", err), http.StatusBadGateway)
		return
	}

	client := conf.Client(ctx, oauthToken)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		http.Error(w, fmt.Sprintf("userinfo fetch failed: %v", err), http.StatusBadGateway)
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
		http.Error(w, "invalid userinfo response", http.StatusBadGateway)
		return
	}
	if info.Email == "" || !info.EmailVerified {
		http.Error(w, "google account email is missing or unverified", http.StatusForbidden)
		return
	}

	googleUser, token, err := h.userService.LoginWithGoogle(info.Email, info.Name, info.Picture)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.provisionPersonalWorkspace(googleUser.ID, googleUser.Name)
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
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	out := []map[string]string{}
	if orgID := ActiveOrg(r); orgID != "" {
		list, err := h.orgService.ListMembers(orgID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user, err := h.userService.FindByEmail(req.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "no user with that email — they must sign up first", http.StatusNotFound)
		return
	}
	if err := h.memberService.AddMember(projectID, user.ID, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.memberService.SetRole(projectID, vars["userId"], req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
