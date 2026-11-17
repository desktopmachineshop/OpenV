package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// Where single sign-on is the only way in, an existing password account is
// linked to the provider with its password once. Someone who forgot it
// resets it by email; the reset only sets the password the link step asks
// for: it opens no session and password sign-in still refuses.
func TestOIDCOnlyPasswordResetServesTheLinkStep(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCOnlyHandler(t, stub)
	mailer := newTestMailer()
	h.Mailer = mailer
	h.EmailLinkBase = "https://app.example.com"
	existing, err := h.UserService.Register("dave@example.com", "password1", "Dave")
	if err != nil {
		t.Fatal(err)
	}

	if got := readAuthConfig(t, h); !got.PasswordResetEmail || got.PasswordEnabled || !got.OIDCOnly {
		t.Errorf("auth config = %+v, want reset mail offered beside oidc_only, no password form", got)
	}

	rec := httptest.NewRecorder()
	h.RequestPasswordReset(rec, jsonReq(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"Dave@Example.com"}`, ""))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request: got %d (%s), want 202", rec.Code, rec.Body.String())
	}
	waitForSend(t, mailer)
	m := regexp.MustCompile(`reset-password\?token=([A-Za-z0-9_-]+)`).FindStringSubmatch(mailer.body[0])
	if m == nil {
		t.Fatalf("no reset link in the mail:\n%s", mailer.body[0])
	}

	rec = httptest.NewRecorder()
	h.ConfirmPasswordReset(rec, jsonReq(http.MethodPost, "/api/v1/auth/password-reset/confirm",
		`{"token":"`+m[1]+`","new_password":"password2"}`, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("confirm: got %d (%s), want 204", rec.Code, rec.Body.String())
	}
	if responseCookie(rec, SessionCookieName) != nil {
		t.Fatal("a reset opened a session")
	}

	// Password sign-in still refuses, with the new password too.
	rec = httptest.NewRecorder()
	h.Login(rec, jsonReq(http.MethodPost, "/api/v1/auth/login", `{"email":"dave@example.com","password":"password2"}`, ""))
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusForbidden || body.Code != ErrCodeSSOOnly {
		t.Fatalf("login after the reset: got %d (%s), want 403 sso_only", rec.Code, rec.Body.String())
	}

	// The new password links the provider, and that signs in.
	signOn := oidcSignOn(t, h, stub, "dave@example.com")
	link := responseCookie(signOn, identityLinkCookie)
	if link == nil {
		t.Fatalf("sign-on: got %d %q, want a link request", signOn.Code, signOn.Header().Get("Location"))
	}
	old := httptest.NewRecorder()
	h.ConfirmIdentityLink(old, linkRequest(http.MethodPost, `{"password":"password1"}`, link))
	if old.Code != http.StatusUnauthorized {
		t.Errorf("the old password: got %d, want 401", old.Code)
	}
	ok := httptest.NewRecorder()
	h.ConfirmIdentityLink(ok, linkRequest(http.MethodPost, `{"password":"password2"}`, link))
	session := responseCookie(ok, SessionCookieName)
	if ok.Code != http.StatusOK || session == nil {
		t.Fatalf("link with the new password: got %d (%s), want a session", ok.Code, ok.Body.String())
	}
	if got, _ := h.UserService.GetBySessionToken(session.Value); got == nil || got.ID != existing.ID {
		t.Errorf("session resolves to %+v, want the password account", got)
	}
}

// A sign-on that comes back without a sign-in goes to the sign-in page on
// the configured frontend's origin with a short code, never to a place the
// request names, and ends the flow's cookies.
func TestOIDCCallbackErrorsGoBackToTheSignInPage(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	cfg := &OIDCConfig{Issuer: stub.server.URL, ClientID: "client-abc", ClientSecret: "stub-secret",
		RedirectURL: "https://app/api/v1/auth/oidc/callback", ProviderName: "Acme SSO",
		FrontendURL: "https://app.example.com/projects"}
	h, _ := newOIDCTestHandler(t, cfg)
	for _, c := range []struct{ query, want string }{
		{"error=access_denied&state=xyz", "https://app.example.com/login?error=sso_cancelled"},
		{"error=server_error&state=xyz&error_description=x", "https://app.example.com/login?error=sso_failed"},
		{"code=c&state=xyz&redirect_uri=https://evil.example", "https://app.example.com/login?error=sso_failed"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?"+c.query, nil)
		req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "other"})
		rec := httptest.NewRecorder()
		h.OIDCCallback(rec, req)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != c.want {
			t.Errorf("%s: got %d %q, want 302 to %s", c.query, rec.Code, rec.Header().Get("Location"), c.want)
		}
		if responseCookie(rec, SessionCookieName) != nil {
			t.Errorf("%s: a session was opened", c.query)
		}
	}
	// The provider's cancel clears the flow cookies.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?error=access_denied", nil)
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, req)
	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	for _, name := range []string{oidcStateCookie, oidcNonceCookie, oidcVerifierCookie} {
		if !cleared[name] {
			t.Errorf("cancel left cookie %s", name)
		}
	}
}
