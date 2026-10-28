package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// OPENV_OIDC_ONLY: a deployment that signs everyone in through its single
// sign-on provider only. The provider's own flow and the link of a provider
// to a password account stay; OpenV's other ways in refuse.

// newOIDCOnlyHandler is newOIDCTestHandler with the provider made the only
// way in, and Google configured beside it.
func newOIDCOnlyHandler(t *testing.T, stub *oidcStub) (*Handler, *memUserRepo) {
	cfg := stub.config("client-abc", "https://app/api/v1/auth/oidc/callback")
	cfg.Only = true
	h, repo := newOIDCTestHandler(t, cfg)
	h.GoogleOAuth = &GoogleOAuthConfig{ClientID: "google-client", RedirectURL: "https://app/g/cb"}
	return h, repo
}

type authConfigBody struct {
	GoogleEnabled      bool   `json:"google_enabled"`
	OIDCEnabled        bool   `json:"oidc_enabled"`
	OIDCOnly           bool   `json:"oidc_only"`
	OIDCProviderName   string `json:"oidc_provider_name"`
	PasswordEnabled    bool   `json:"password_enabled"`
	PasswordResetEmail bool   `json:"password_reset_email"`
}

func readAuthConfig(t *testing.T, h *Handler) authConfigBody {
	t.Helper()
	rec := httptest.NewRecorder()
	h.AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))
	var body authConfigBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return body
}

func TestAuthConfigReportsOIDCOnly(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCOnlyHandler(t, stub)
	got := readAuthConfig(t, h)
	want := authConfigBody{OIDCEnabled: true, OIDCOnly: true, OIDCProviderName: "Acme SSO"}
	if got != want {
		t.Errorf("auth config = %+v, want %+v", got, want)
	}

	// Off, the same deployment offers every method it has.
	h.OIDC.Only = false
	got = readAuthConfig(t, h)
	want = authConfigBody{GoogleEnabled: true, OIDCEnabled: true, OIDCProviderName: "Acme SSO", PasswordEnabled: true}
	if got != want {
		t.Errorf("auth config without the setting = %+v, want %+v", got, want)
	}

	// Only on a provider that is not configured turns nothing off.
	h.OIDC = &OIDCConfig{Issuer: "https://idp.example.test", Only: true}
	if got := readAuthConfig(t, h); got.OIDCOnly || !got.PasswordEnabled || !got.GoogleEnabled {
		t.Errorf("auth config with no client id = %+v, want password and Google on", got)
	}
}

// Every one of OpenV's own ways in answers 403 sso_only, naming the provider.
func TestOIDCOnlyRefusesOwnSignIn(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, repo := newOIDCOnlyHandler(t, stub)
	if _, err := h.UserService.Register("dave@example.com", "password1", "Dave"); err != nil {
		t.Fatal(err)
	}
	jsonReq := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	for _, c := range []struct {
		name    string
		handler http.HandlerFunc
		req     *http.Request
	}{
		{"login", h.Login, jsonReq(http.MethodPost, "/api/v1/auth/login", `{"email":"dave@example.com","password":"password1"}`)},
		{"register", h.Register, jsonReq(http.MethodPost, "/api/v1/auth/register", `{"email":"new@example.com","password":"password1","name":"New"}`)},
		{"google", h.GoogleLogin, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google", nil)},
		{"google callback", h.GoogleCallback, httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?state=s&code=c", nil)},
		{"password reset", h.RequestPasswordReset, jsonReq(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"dave@example.com"}`)},
		{"password reset confirm", h.ConfirmPasswordReset, jsonReq(http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"t","new_password":"password2"}`)},
	} {
		rec := httptest.NewRecorder()
		c.handler(rec, c.req)
		var body errorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusForbidden || body.Code != ErrCodeSSOOnly || !strings.Contains(body.Error, "Acme SSO") {
			t.Errorf("%s: got %d %s, want 403 sso_only naming the provider", c.name, rec.Code, rec.Body.String())
		}
		if responseCookie(rec, SessionCookieName) != nil {
			t.Errorf("%s: a session was opened", c.name)
		}
	}
	if u, _ := repo.FindUserByEmail("new@example.com"); u != nil {
		t.Error("register created an account")
	}

	// Without the setting, the same password signs in.
	h.OIDC.Only = false
	rec := httptest.NewRecorder()
	h.Login(rec, jsonReq(http.MethodPost, "/api/v1/auth/login", `{"email":"dave@example.com","password":"password1"}`))
	if rec.Code != http.StatusOK {
		t.Errorf("login without the setting: got %d (%s)", rec.Code, rec.Body.String())
	}
}

// An account made with a password still links the provider with its
// password once, and then signs in through the provider.
func TestOIDCOnlyKeepsTheIdentityLink(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCOnlyHandler(t, stub)
	existing, err := h.UserService.Register("dave@example.com", "password1", "Dave")
	if err != nil {
		t.Fatal(err)
	}
	rec := oidcSignOn(t, h, stub, "dave@example.com")
	link := responseCookie(rec, identityLinkCookie)
	if rec.Code != http.StatusFound || link == nil {
		t.Fatalf("callback: got %d (%s), want the link page", rec.Code, rec.Body.String())
	}
	prev := httptest.NewRecorder()
	h.PreviewIdentityLink(prev, linkRequest(http.MethodGet, "", link))
	if prev.Code != http.StatusOK {
		t.Fatalf("preview: got %d (%s)", prev.Code, prev.Body.String())
	}
	ok := httptest.NewRecorder()
	h.ConfirmIdentityLink(ok, linkRequest(http.MethodPost, `{"password":"password1"}`, link))
	session := responseCookie(ok, SessionCookieName)
	if ok.Code != http.StatusOK || session == nil {
		t.Fatalf("confirm: got %d (%s), want a session", ok.Code, ok.Body.String())
	}
	if got, _ := h.UserService.GetBySessionToken(session.Value); got == nil || got.ID != existing.ID {
		t.Fatalf("session resolves to %+v, want the password account", got)
	}
	next := oidcSignOn(t, h, stub, "dave@example.com")
	if next.Code != http.StatusFound || responseCookie(next, SessionCookieName) == nil {
		t.Errorf("sign-on after linking: got %d (%s), want a session", next.Code, next.Body.String())
	}
}

// Someone invited joins the workspace by signing in through the provider
// with the invited address, and the invitation link still resolves for the
// sign-in page to show.
func TestOIDCOnlyInviteeJoinsThroughTheProvider(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCOnlyHandler(t, stub)
	invites := newFakeInviteService()
	h.InvitationService = invites
	invites.invite("org-1", "invitee@example.com", orgs.RoleMember)

	prev := httptest.NewRecorder()
	h.PreviewInvitation(prev, previewReq("tok-invitee@example.com"))
	if prev.Code != http.StatusOK {
		t.Fatalf("preview: got %d (%s)", prev.Code, prev.Body.String())
	}

	rec := oidcSignOn(t, h, stub, "Invitee@example.com")
	if rec.Code != http.StatusFound || responseCookie(rec, SessionCookieName) == nil {
		t.Fatalf("sign-on: got %d (%s), want a session", rec.Code, rec.Body.String())
	}
	user, _ := h.UserService.GetBySessionToken(responseCookie(rec, SessionCookieName).Value)
	if user == nil || len(invites.accepted) != 1 || invites.accepted[0] != "invitee@example.com:"+user.ID {
		t.Errorf("accepted = %v, want the invitation taken up by the new account", invites.accepted)
	}
	if user != nil && user.AuthProvider != users.ProviderOIDC {
		t.Errorf("account provider = %q, want oidc", user.AuthProvider)
	}
}

// ?signup=1 asks the provider for its sign-up page with prompt=create; a
// plain sign-in sends no prompt, which some providers would refuse.
func TestOIDCLoginAsksForSignUp(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCTestHandler(t, stub.config("client-abc", "https://app/api/v1/auth/oidc/callback"))
	for _, c := range []struct{ path, prompt string }{
		{"/api/v1/auth/oidc/login", ""},
		{"/api/v1/auth/oidc/login?signup=1", "create"},
		{"/api/v1/auth/oidc/login?signup=yes", ""},
	} {
		rec := httptest.NewRecorder()
		h.OIDCLogin(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("%s: got %d (%s)", c.path, rec.Code, rec.Body.String())
		}
		loc, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		q := loc.Query()
		if got := q.Get("prompt"); got != c.prompt || (c.prompt == "" && q.Has("prompt")) {
			t.Errorf("%s: prompt = %q, want %q", c.path, got, c.prompt)
		}
		if q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" {
			t.Errorf("%s: authorize URL lost its PKCE or nonce: %s", c.path, loc)
		}
	}
}
