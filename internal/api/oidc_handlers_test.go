package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// --- OIDC identity-provider stub -------------------------------------------
//
// A minimal but real OIDC provider: it serves discovery, a JWKS built from a
// freshly-generated RSA key, and a token endpoint that returns an RS256-signed
// ID token. That lets the callback exercise the genuine go-oidc verification
// path (signature, issuer, audience) end to end.

type oidcStub struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	// idToken is the raw ID token the /token endpoint returns; the test sets it
	// per case (controlling nonce/email/verified).
	idToken string
}

func newOIDCStub(t *testing.T, clientID string) *oidcStub {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	stub := &oidcStub{key: key, clientID: clientID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := stub.server.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"userinfo_endpoint":                     base + "/userinfo",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		pub := stub.key.PublicKey
		n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test-key",
				"n": n, "e": "AQAB",
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     stub.idToken,
		})
	})
	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)
	return stub
}

// signIDToken mints an RS256-signed ID token with the given claims.
func (s *oidcStub) signIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	base := map[string]any{
		"iss": s.server.URL,
		"aud": s.clientID,
		"sub": "subject-123",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range claims {
		base[k] = v
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}

	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal jwt segment: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := enc(header) + "." + enc(base)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (s *oidcStub) config(clientID, redirectURL string) *OIDCConfig {
	return &OIDCConfig{
		Issuer:       s.server.URL,
		ClientID:     clientID,
		ClientSecret: "stub-secret",
		RedirectURL:  redirectURL,
		ProviderName: "Acme SSO",
		FrontendURL:  "/projects",
	}
}

// --- in-memory users.Repository --------------------------------------------

type memUserRepo struct {
	users    map[string]*users.User
	sessions map[string]*users.Session
}

func newMemUserRepo() *memUserRepo {
	return &memUserRepo{users: map[string]*users.User{}, sessions: map[string]*users.Session{}}
}

func (m *memUserRepo) SaveUser(u *users.User) error   { m.users[u.ID] = u; return nil }
func (m *memUserRepo) UpdateUser(u *users.User) error { m.users[u.ID] = u; return nil }
func (m *memUserRepo) FindUserByEmail(email string) (*users.User, error) {
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, nil
}
func (m *memUserRepo) FindUserByID(id string) (*users.User, error) { return m.users[id], nil }
func (m *memUserRepo) ListUsers() ([]*users.User, error)           { return nil, nil }
func (m *memUserRepo) CountUsers() (int, error)                    { return len(m.users), nil }
func (m *memUserRepo) SetEmailNotifications(string, bool) error    { return nil }
func (m *memUserRepo) SaveSession(s *users.Session) error          { m.sessions[s.ID] = s; return nil }
func (m *memUserRepo) FindSessionByTokenHash(hash string) (*users.Session, error) {
	for _, s := range m.sessions {
		if s.TokenHash == hash {
			return s, nil
		}
	}
	return nil, nil
}
func (m *memUserRepo) TouchSession(string, time.Time) error     { return nil }
func (m *memUserRepo) SetSessionActiveOrg(string, string) error { return nil }
func (m *memUserRepo) DeleteSession(id string) error            { delete(m.sessions, id); return nil }
func (m *memUserRepo) DeleteExpiredSessions(time.Time) error    { return nil }

// --- tests -----------------------------------------------------------------

func newOIDCTestHandler(cfg *OIDCConfig) (*Handler, *memUserRepo) {
	repo := newMemUserRepo()
	return &Handler{
		userService: users.NewDefaultService(repo),
		oidc:        cfg,
	}, repo
}

func TestOIDCLoginBuildsAuthorizeURL(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCTestHandler(stub.config("client-abc", "https://app/api/v1/auth/oidc/callback"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
	rec := httptest.NewRecorder()
	h.OIDCLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d (%s)", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), stub.server.URL+"/authorize") {
		t.Fatalf("authorize URL not from discovery: %s", loc)
	}
	q := loc.Query()
	if q.Get("client_id") != "client-abc" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("redirect_uri") != "https://app/api/v1/auth/oidc/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("scope missing openid: %q", q.Get("scope"))
	}

	state, nonce := q.Get("state"), q.Get("nonce")
	if state == "" || nonce == "" {
		t.Fatalf("state/nonce missing from authorize URL: state=%q nonce=%q", state, nonce)
	}
	cookies := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c.Value
	}
	if cookies[oidcStateCookie] != state {
		t.Errorf("state cookie %q != authorize state %q", cookies[oidcStateCookie], state)
	}
	if cookies[oidcNonceCookie] != nonce {
		t.Errorf("nonce cookie %q != authorize nonce %q", cookies[oidcNonceCookie], nonce)
	}
}

func TestOIDCCallbackCreatesUserAndSession(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	cfg := stub.config("client-abc", "https://app/api/v1/auth/oidc/callback")
	h, repo := newOIDCTestHandler(cfg)

	const nonce = "the-expected-nonce"
	stub.idToken = stub.signIDToken(t, map[string]any{
		"email":          "alice@example.com",
		"email_verified": true,
		"name":           "Alice Example",
		"nonce":          nonce,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authcode&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: nonce})
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/projects" {
		t.Errorf("redirect to %q, want /projects", got)
	}

	// Session cookie issued.
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName && c.Value != "" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie set")
	}

	// User created with the oidc provider.
	created, _ := repo.FindUserByEmail("alice@example.com")
	if created == nil {
		t.Fatal("user not created")
	}
	if created.AuthProvider != users.ProviderOIDC {
		t.Errorf("auth_provider = %q, want %q", created.AuthProvider, users.ProviderOIDC)
	}
	if created.Name != "Alice Example" {
		t.Errorf("name = %q", created.Name)
	}
	// Session cookie resolves back to the same user.
	got, err := h.userService.GetBySessionToken(sessionCookie.Value)
	if err != nil || got == nil || got.ID != created.ID {
		t.Errorf("session does not resolve to created user: %v", err)
	}
}

// TestOIDCCallbackRequiresVerifiedEmail: a positively-verified email is
// required (issue #242). An absent or false email_verified claim is rejected
// with 403 and provisions no account, closing the address-takeover vector.
func TestOIDCCallbackRequiresVerifiedEmail(t *testing.T) {
	cases := []struct {
		name         string
		emailClaims  map[string]any
		wantRejected bool
	}{
		{
			name:         "verified true is accepted",
			emailClaims:  map[string]any{"email_verified": true},
			wantRejected: false,
		},
		{
			name:         "verified false is rejected",
			emailClaims:  map[string]any{"email_verified": false},
			wantRejected: true,
		},
		{
			name:         "verified absent is rejected",
			emailClaims:  map[string]any{}, // no email_verified claim at all
			wantRejected: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newOIDCStub(t, "client-abc")
			cfg := stub.config("client-abc", "https://app/api/v1/auth/oidc/callback")
			h, repo := newOIDCTestHandler(cfg)

			const nonce = "the-nonce"
			claims := map[string]any{
				"email": "verify@example.com",
				"name":  "Verify Me",
				"nonce": nonce,
			}
			for k, v := range tc.emailClaims {
				claims[k] = v
			}
			stub.idToken = stub.signIDToken(t, claims)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authcode&state=xyz", nil)
			req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "xyz"})
			req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: nonce})
			rec := httptest.NewRecorder()
			h.OIDCCallback(rec, req)

			if tc.wantRejected {
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 for unverified email, got %d (%s)", rec.Code, rec.Body.String())
				}
				if u, _ := repo.FindUserByEmail("verify@example.com"); u != nil {
					t.Error("no account should be provisioned for an unverified email")
				}
				if !strings.Contains(rec.Body.String(), "verif") {
					t.Errorf("error should mention verification: %s", rec.Body.String())
				}
			} else {
				if rec.Code != http.StatusFound {
					t.Fatalf("expected 302 for verified email, got %d (%s)", rec.Code, rec.Body.String())
				}
				if u, _ := repo.FindUserByEmail("verify@example.com"); u == nil {
					t.Error("verified email should provision an account")
				}
			}
		})
	}
}

// TestOIDCCallbackRejectsCrossProviderAccount: an OIDC login whose email
// already belongs to an account created with a DIFFERENT provider is rejected
// with 409 and does not log in or re-label the account (issue #242).
func TestOIDCCallbackRejectsCrossProviderAccount(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	cfg := stub.config("client-abc", "https://app/api/v1/auth/oidc/callback")
	h, repo := newOIDCTestHandler(cfg)

	// Pre-existing password account with the same email.
	existing, err := h.userService.Register("collide@example.com", "hunter2pw", "Pw User")
	if err != nil {
		t.Fatalf("seed password account: %v", err)
	}
	if existing.AuthProvider != users.ProviderPassword {
		t.Fatalf("precondition: seeded account provider = %q", existing.AuthProvider)
	}

	const nonce = "n"
	stub.idToken = stub.signIDToken(t, map[string]any{
		"email":          "collide@example.com",
		"email_verified": true,
		"name":           "Impersonator",
		"nonce":          nonce,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authcode&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: nonce})
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for cross-provider collision, got %d (%s)", rec.Code, rec.Body.String())
	}
	// No session cookie was issued.
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName && c.Value != "" {
			t.Error("a session cookie was issued despite the provider collision")
		}
	}
	// The existing account is untouched: still a password account, name not
	// overwritten by the OIDC claim.
	after, _ := repo.FindUserByEmail("collide@example.com")
	if after == nil {
		t.Fatal("existing account vanished")
	}
	if after.AuthProvider != users.ProviderPassword {
		t.Errorf("account was re-labelled to %q; must keep %q", after.AuthProvider, users.ProviderPassword)
	}
	if after.Name != "Pw User" {
		t.Errorf("account name overwritten to %q by rejected login", after.Name)
	}
}

func TestOIDCCallbackRejectsBadNonce(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	cfg := stub.config("client-abc", "https://app/api/v1/auth/oidc/callback")
	h, repo := newOIDCTestHandler(cfg)

	// Token is signed with a nonce that differs from the cookie.
	stub.idToken = stub.signIDToken(t, map[string]any{
		"email":          "mallory@example.com",
		"email_verified": true,
		"nonce":          "attacker-nonce",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=authcode&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: "session-nonce"})
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonce mismatch, got %d (%s)", rec.Code, rec.Body.String())
	}
	if u, _ := repo.FindUserByEmail("mallory@example.com"); u != nil {
		t.Error("user should not be created on nonce mismatch")
	}
}

func TestOIDCCallbackRejectsBadState(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCTestHandler(stub.config("client-abc", "https://app/cb"))

	// State cookie does not match the query state -> CSRF rejection, before any
	// token exchange happens.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=c&state=fromquery", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "different"})
	req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: "n"})
	rec := httptest.NewRecorder()
	h.OIDCCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for state mismatch, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "state") {
		t.Errorf("error should mention state: %s", rec.Body.String())
	}
}

func TestOIDCNotConfigured(t *testing.T) {
	// Nil OIDC config -> endpoints report not-configured, nothing else changes.
	h, _ := newOIDCTestHandler(nil)

	for _, path := range []string{"/api/v1/auth/oidc/login", "/api/v1/auth/oidc/callback"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if strings.HasSuffix(path, "login") {
			h.OIDCLogin(rec, req)
		} else {
			h.OIDCCallback(rec, req)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404 when unconfigured, got %d", path, rec.Code)
		}
	}
}

func TestAuthConfigReportsOIDC(t *testing.T) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCTestHandler(stub.config("client-abc", "https://app/cb"))

	rec := httptest.NewRecorder()
	h.AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))

	var body struct {
		GoogleEnabled    bool   `json:"google_enabled"`
		OIDCEnabled      bool   `json:"oidc_enabled"`
		OIDCProviderName string `json:"oidc_provider_name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OIDCEnabled {
		t.Error("oidc_enabled should be true")
	}
	if body.OIDCProviderName != "Acme SSO" {
		t.Errorf("provider name = %q", body.OIDCProviderName)
	}

	// And false when unconfigured.
	h2, _ := newOIDCTestHandler(nil)
	rec2 := httptest.NewRecorder()
	h2.AuthConfig(rec2, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))
	if strings.Contains(rec2.Body.String(), fmt.Sprintf("%q:true", "oidc_enabled")) {
		t.Errorf("oidc_enabled should be false when unconfigured: %s", rec2.Body.String())
	}
}
