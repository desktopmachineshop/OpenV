package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// oidcSignOn drives one OIDC callback for email and returns the recorder.
func oidcSignOn(t *testing.T, h *Handler, stub *oidcStub, email string) *httptest.ResponseRecorder {
	t.Helper()
	const nonce = "n"
	stub.idToken = stub.signIDToken(t, map[string]any{
		"email": email, "email_verified": true, "name": "From The IdP", "nonce": nonce,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authcode&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: nonce})
	req.AddCookie(&http.Cookie{Name: oidcVerifierCookie, Value: testVerifier})
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, req)
	return rec
}

func responseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	return nil
}

func linkRequest(method, body string, link *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, "/api/v1/auth/link", strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if link != nil {
		req.AddCookie(link)
	}
	return req
}

// TestSignOnMeetingAPasswordAccountLinksWithThePassword: a sign-on whose
// verified email belongs to a password account sends the browser to the
// sign-in page holding a link request, and no session. The account's
// password confirms the link and signs in; from then on the provider goes
// straight in, and the account keeps its own name and method.
func TestSignOnMeetingAPasswordAccountLinksWithThePassword(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, repo := newOIDCTestHandler(t, stub.config("client-abc", "https://app/api/v1/auth/oidc/callback"))
	existing, err := h.UserService.Register("dave@example.com", "password1", "Dave")
	if err != nil {
		t.Fatal(err)
	}

	rec := oidcSignOn(t, h, stub, "Dave@example.com")
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: got %d (%s), want 302", rec.Code, rec.Body.String())
	}
	// The stub's FrontendURL is a bare path; TestLinkPageURL covers an origin.
	if got := rec.Header().Get("Location"); got != "/login?link=oidc" {
		t.Errorf("callback redirected to %q, want the sign-in page", got)
	}
	if responseCookie(rec, SessionCookieName) != nil {
		t.Fatal("a session was opened before the password confirmed the link")
	}
	link := responseCookie(rec, identityLinkCookie)
	if link == nil || !link.HttpOnly {
		t.Fatalf("link cookie = %+v, want an HttpOnly cookie", link)
	}

	// The page asks which account it is confirming.
	prev := httptest.NewRecorder()
	h.PreviewIdentityLink(prev, linkRequest(http.MethodGet, "", link))
	if prev.Code != http.StatusOK {
		t.Fatalf("preview: got %d (%s)", prev.Code, prev.Body.String())
	}
	var preview users.IdentityLinkPreview
	if err := json.Unmarshal(prev.Body.Bytes(), &preview); err != nil || preview.Provider != "oidc" || preview.Email != "dave@example.com" {
		t.Fatalf("preview = %+v, %v", preview, err)
	}

	// A wrong password is refused and keeps the request.
	bad := httptest.NewRecorder()
	h.ConfirmIdentityLink(bad, linkRequest(http.MethodPost, `{"password":"nope-nope"}`, link))
	if bad.Code != http.StatusUnauthorized || responseCookie(bad, SessionCookieName) != nil {
		t.Fatalf("wrong password: got %d (%s)", bad.Code, bad.Body.String())
	}

	ok := httptest.NewRecorder()
	h.ConfirmIdentityLink(ok, linkRequest(http.MethodPost, `{"password":"password1"}`, link))
	if ok.Code != http.StatusOK {
		t.Fatalf("confirm: got %d (%s)", ok.Code, ok.Body.String())
	}
	session := responseCookie(ok, SessionCookieName)
	if session == nil {
		t.Fatal("confirm opened no session")
	}
	if got, _ := h.UserService.GetBySessionToken(session.Value); got == nil || got.ID != existing.ID {
		t.Fatalf("session resolves to %+v, want the password account", got)
	}

	// Spent: the same cookie cannot link again.
	again := httptest.NewRecorder()
	h.ConfirmIdentityLink(again, linkRequest(http.MethodPost, `{"password":"password1"}`, link))
	if again.Code != http.StatusNotFound {
		t.Errorf("second confirm: got %d, want 404", again.Code)
	}

	// The next sign-on goes straight in, to the same account, unchanged.
	next := oidcSignOn(t, h, stub, "dave@example.com")
	if next.Code != http.StatusFound || responseCookie(next, SessionCookieName) == nil {
		t.Fatalf("sign-on after linking: got %d (%s), want a session", next.Code, next.Body.String())
	}
	after, _ := repo.FindUserByEmail("dave@example.com")
	if after.AuthProvider != users.ProviderPassword || after.Name != "Dave" {
		t.Errorf("account after linking = %q, %q; want password, Dave", after.AuthProvider, after.Name)
	}
}

func TestIdentityLinkWithoutARequest(t *testing.T) {
	h, _ := newOIDCTestHandler(t, nil)
	rec := httptest.NewRecorder()
	h.PreviewIdentityLink(rec, linkRequest(http.MethodGet, "", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("preview with no cookie: got %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ConfirmIdentityLink(rec, linkRequest(http.MethodPost, `{"password":"password1"}`,
		&http.Cookie{Name: identityLinkCookie, Value: "made-up"}))
	if rec.Code != http.StatusNotFound || responseCookie(rec, SessionCookieName) != nil {
		t.Errorf("confirm with an unknown request: got %d (%s), want 404", rec.Code, rec.Body.String())
	}
}

func TestLinkPageURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://openv.app":           "https://openv.app/login?link=google",
		"https://openv.app/":          "https://openv.app/login?link=google",
		"http://localhost:3000/x?y=z": "http://localhost:3000/login?link=google",
		"/projects":                   "/login?link=google",
		"":                            "/login?link=google",
	} {
		if got := linkPageURL(in, "google"); got != want {
			t.Errorf("linkPageURL(%q) = %q, want %q", in, got, want)
		}
	}
}
