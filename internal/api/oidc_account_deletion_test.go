package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// DMSid's account-deletion protocol (dms-id ADR 0002): a deletion token,
// signed with the provider's keys like an ID token, removes the account its
// email names. The stub provider of oidc_handlers_test.go serves the JWKS
// the endpoint verifies against.

type fakeAccountDeleter struct {
	emails []string
	out    *users.AccountDeletion
}

func (f *fakeAccountDeleter) DeleteAccount(email string) (*users.AccountDeletion, error) {
	f.emails = append(f.emails, email)
	return f.out, nil
}

// signDeletionToken mints an RS256 JWT with the given header typ and claims
// over the DMSid defaults, signed with key.
func signDeletionToken(t *testing.T, key *rsa.PrivateKey, typ string, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(map[string]any{"alg": "RS256", "typ": typ, "kid": "test-key"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func deletionClaims(stub *oidcStub, over map[string]any) map[string]any {
	now := time.Now()
	c := map[string]any{
		"iss":    stub.server.URL,
		"aud":    "client-abc",
		"iat":    now.Unix(),
		"exp":    now.Add(120 * time.Second).Unix(),
		"jti":    "jti-1",
		"sub":    "subject-123",
		"email":  "Dave@Example.com",
		"events": map[string]any{accountDeletionEvent: map[string]any{}},
	}
	for k, v := range over {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	return c
}

func deletionRequest(token string) *http.Request {
	form := url.Values{}
	if token != "" {
		form.Set("deletion_token", token)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/account-deletion", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "203.0.113.20:5000"
	return r
}

func newDeletionHandler(t *testing.T, out *users.AccountDeletion) (*Handler, *oidcStub, *fakeAccountDeleter) {
	stub := newOIDCStub(t, "client-abc")
	h, _ := newOIDCTestHandler(t, stub.config("client-abc", "https://app/api/v1/auth/oidc/callback"))
	deleter := &fakeAccountDeleter{out: out}
	h.AccountDeleter = deleter
	h.authIPLimiter = newRateLimiter(100, 1)
	return h, stub, deleter
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestAccountDeletionRemovesTheAccountTheTokenNames(t *testing.T) {
	avatar := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(avatar, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, stub, deleter := newDeletionHandler(t, &users.AccountDeletion{
		Status: users.DeletionDeleted, UserID: "u-1", LeftWorkspaces: []string{"org-1"}, Files: []string{avatar},
	})
	rec := httptest.NewRecorder()
	h.OIDCAccountDeletion(rec, deletionRequest(signDeletionToken(t, stub.key, "account-deletion+jwt", deletionClaims(stub, nil))))
	if rec.Code != http.StatusOK || decodeStatus(t, rec)["status"] != "deleted" || len(decodeStatus(t, rec)) != 1 {
		t.Fatalf("got %d %s, want 200 {\"status\":\"deleted\"}", rec.Code, rec.Body.String())
	}
	if len(deleter.emails) != 1 || deleter.emails[0] != "Dave@Example.com" {
		t.Errorf("deleted %v, want the token's address", deleter.emails)
	}
	if _, err := os.Stat(avatar); !os.IsNotExist(err) {
		t.Error("the account's stored files were left on disk")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestAccountDeletionAnswersNotFoundAndBlocked(t *testing.T) {
	h, stub, _ := newDeletionHandler(t, &users.AccountDeletion{Status: users.DeletionNotFound})
	token := signDeletionToken(t, stub.key, "account-deletion+jwt", deletionClaims(stub, nil))
	rec := httptest.NewRecorder()
	h.OIDCAccountDeletion(rec, deletionRequest(token))
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"status\":\"not_found\"}\n" {
		t.Errorf("not found: got %d %q", rec.Code, rec.Body.String())
	}

	block := &users.DeletionBlock{Kind: users.BlockSoleOwner, Workspace: "Acme"}
	h.AccountDeleter = &fakeAccountDeleter{out: &users.AccountDeletion{Status: users.DeletionBlocked, UserID: "u-1", Blocked: block}}
	rec = httptest.NewRecorder()
	h.OIDCAccountDeletion(rec, deletionRequest(token))
	body := decodeStatus(t, rec)
	want := "You are the only owner of the workspace “Acme” in OpenV. Make someone else its owner, or remove the other members, then try again."
	if rec.Code != http.StatusConflict || body["status"] != "blocked" || body["reason"] != want {
		t.Errorf("blocked: got %d %s", rec.Code, rec.Body.String())
	}
}

// Every token that fails a check is 400 {"status":"invalid"} and removes
// nothing.
func TestAccountDeletionRefusesATokenThatDoesNotVerify(t *testing.T) {
	h, stub, deleter := newDeletionHandler(t, &users.AccountDeletion{Status: users.DeletionDeleted, UserID: "u-1"})
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sign := func(typ string, over map[string]any) string {
		return signDeletionToken(t, stub.key, typ, deletionClaims(stub, over))
	}
	for _, c := range []struct{ name, token string }{
		{"no token", ""},
		{"not a JWT", "garbage"},
		{"an ID token's typ", sign("JWT", nil)},
		{"no typ", sign("", nil)},
		{"another key", signDeletionToken(t, other, "account-deletion+jwt", deletionClaims(stub, nil))},
		{"another issuer", sign("account-deletion+jwt", map[string]any{"iss": "https://elsewhere.example"})},
		{"another audience", sign("account-deletion+jwt", map[string]any{"aud": "opend"})},
		{"expired past the skew", sign("account-deletion+jwt", map[string]any{"exp": now.Add(-90 * time.Second).Unix()})},
		{"no exp", sign("account-deletion+jwt", map[string]any{"exp": nil})},
		{"no events", sign("account-deletion+jwt", map[string]any{"events": nil})},
		{"another event", sign("account-deletion+jwt", map[string]any{"events": map[string]any{"https://id.desktopmachineshop.com/events/logout": map[string]any{}}})},
		{"no email", sign("account-deletion+jwt", map[string]any{"email": nil})},
		{"a tampered payload", func() string {
			parts := strings.Split(sign("account-deletion+jwt", nil), ".")
			b, _ := json.Marshal(deletionClaims(stub, map[string]any{"email": "victim@example.com"}))
			parts[1] = base64.RawURLEncoding.EncodeToString(b)
			return strings.Join(parts, ".")
		}()},
	} {
		rec := httptest.NewRecorder()
		h.OIDCAccountDeletion(rec, deletionRequest(c.token))
		if rec.Code != http.StatusBadRequest || rec.Body.String() != "{\"status\":\"invalid\"}\n" {
			t.Errorf("%s: got %d %q, want 400 invalid", c.name, rec.Code, rec.Body.String())
		}
	}
	if len(deleter.emails) != 0 {
		t.Fatalf("an invalid token removed %v", deleter.emails)
	}

	// Within the skew, under the configured issuer's own event key, and
	// with the media-type form of typ, the token verifies.
	for _, c := range []struct{ name, token string }{
		{"expired within the skew", sign("account-deletion+jwt", map[string]any{"exp": now.Add(-30 * time.Second).Unix()})},
		{"the issuer's own event key", sign("account-deletion+jwt", map[string]any{"events": map[string]any{stub.server.URL + "/events/account-deletion": map[string]any{}}})},
		{"application/ typ", sign("application/account-deletion+jwt", nil)},
	} {
		rec := httptest.NewRecorder()
		h.OIDCAccountDeletion(rec, deletionRequest(c.token))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: got %d %s, want 200", c.name, rec.Code, rec.Body.String())
		}
	}
}

func TestAccountDeletionIsOffWithoutOIDC(t *testing.T) {
	h, _ := newOIDCTestHandler(t, nil)
	deleter := &fakeAccountDeleter{}
	h.AccountDeleter = deleter
	rec := httptest.NewRecorder()
	h.OIDCAccountDeletion(rec, deletionRequest("x.y.z"))
	if rec.Code != http.StatusNotFound || len(deleter.emails) != 0 {
		t.Errorf("got %d, want 404 and nothing removed", rec.Code)
	}
}
