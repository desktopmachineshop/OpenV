package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeVerifyService is the slice of users.Service the verification handlers
// touch: a session lookup keyed by cookie value, an issue that hands back a
// fixed token, and a confirm that recognises one token.
type fakeVerifyService struct {
	users.Service
	sessions   map[string]*users.User // cookie value -> user
	issued     []string               // addresses links were issued for
	issueErr   error
	confirmOK  string
	confirmErr error
}

func (f *fakeVerifyService) Register(email, password, name string) (*users.User, error) {
	return &users.User{ID: "u-new", Email: strings.ToLower(email), Name: name}, nil
}

func (f *fakeVerifyService) Login(email, password string) (*users.User, string, error) {
	return &users.User{ID: "u-new", Email: email}, "session-new", nil
}

func (f *fakeVerifyService) GetBySessionToken(token string) (*users.User, error) {
	if u := f.sessions[token]; u != nil {
		return u, nil
	}
	return nil, users.ErrSessionInvalid
}

func (f *fakeVerifyService) IssueEmailVerification(userID, email string) (string, string, error) {
	if f.issueErr != nil {
		return "", "", f.issueErr
	}
	if email == "" {
		email = f.sessions["cookie-pending"].Email
	}
	f.issued = append(f.issued, email)
	return "raw-token-123", email, nil
}

func (f *fakeVerifyService) ConfirmEmailVerification(token string) (*users.User, error) {
	if f.confirmErr != nil {
		return nil, f.confirmErr
	}
	if token == f.confirmOK {
		return &users.User{ID: "u-pending", Email: "pending@example.com", EmailVerified: true}, nil
	}
	return nil, users.ErrVerificationInvalid
}

// testMailer records sends and can be told to fail; sent is closed-over by
// the register test, which has to wait for the background send.
type testMailer struct {
	mu      sync.Mutex
	enabled bool
	err     error
	to      []string
	body    []string
	sent    chan struct{}
}

func newTestMailer() *testMailer {
	return &testMailer{enabled: true, sent: make(chan struct{}, 8)}
}

func (m *testMailer) Enabled() bool { return m.enabled }
func (m *testMailer) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.to = append(m.to, to)
	m.body = append(m.body, body)
	m.sent <- struct{}{}
	return m.err
}

func newVerifyHandler(required bool) (*Handler, *fakeVerifyService, *testMailer) {
	svc := &fakeVerifyService{
		sessions: map[string]*users.User{
			"cookie-pending":  {ID: "u-pending", Email: "pending@example.com", Name: "Pending"},
			"cookie-verified": {ID: "u-ok", Email: "ok@example.com", EmailVerified: true},
		},
		confirmOK: "good-token",
	}
	mailer := newTestMailer()
	h := &Handler{
		userService:       svc,
		mailer:            mailer,
		emailLinkBase:     "https://app.example.com",
		emailVerification: users.EmailVerificationPolicy{Required: required},
	}
	return h, svc, mailer
}

func jsonReq(method, path, body, cookie string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.9:1234"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie})
	}
	return r
}

func waitSent(t *testing.T, m *testMailer) {
	t.Helper()
	select {
	case <-m.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no verification email was sent")
	}
}

func TestRegisterSendsVerificationLinkWhenRequired(t *testing.T) {
	h, _, mailer := newVerifyHandler(true)
	w := httptest.NewRecorder()
	h.Register(w, jsonReq(http.MethodPost, "/api/v1/auth/register", `{"email":"New@Example.com","password":"password1","name":"New"}`, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("register status = %d body=%s", w.Code, w.Body.String())
	}
	waitSent(t, mailer)
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	if len(mailer.to) != 1 || mailer.to[0] != "new@example.com" {
		t.Errorf("recipients = %v", mailer.to)
	}
	if !strings.Contains(mailer.body[0], "https://app.example.com/verify-email?token=raw-token-123") {
		t.Errorf("mail body lacks the link:\n%s", mailer.body[0])
	}
	var user users.User
	_ = json.Unmarshal(w.Body.Bytes(), &user)
	if user.EmailVerified {
		t.Error("the registered user must be reported unverified")
	}
}

func TestRegisterSucceedsWhenTheMailFails(t *testing.T) {
	h, _, mailer := newVerifyHandler(true)
	mailer.err = errors.New("smtp down")
	w := httptest.NewRecorder()
	h.Register(w, jsonReq(http.MethodPost, "/api/v1/auth/register", `{"email":"a@example.com","password":"password1"}`, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200 even when the mail fails", w.Code)
	}
	waitSent(t, mailer)
}

func TestRegisterSendsNothingWhenNotRequired(t *testing.T) {
	h, _, mailer := newVerifyHandler(false)
	w := httptest.NewRecorder()
	h.Register(w, jsonReq(http.MethodPost, "/api/v1/auth/register", `{"email":"a@example.com","password":"password1"}`, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("register status = %d", w.Code)
	}
	select {
	case <-mailer.sent:
		t.Error("a verification email was sent although verification is off")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestVerifyEmailOutcomes(t *testing.T) {
	h, svc, _ := newVerifyHandler(true)
	cases := []struct {
		name string
		body string
		err  error
		want int
	}{
		{"good token", `{"token":"good-token"}`, nil, http.StatusOK},
		{"bad token", `{"token":"nope"}`, nil, http.StatusBadRequest},
		{"address taken", `{"token":"good-token"}`, users.ErrEmailTaken, http.StatusConflict},
		{"malformed", `{`, nil, http.StatusBadRequest},
	}
	for _, tc := range cases {
		svc.confirmErr = tc.err
		w := httptest.NewRecorder()
		h.VerifyEmail(w, jsonReq(http.MethodPost, "/api/v1/auth/verify-email", tc.body, ""))
		if w.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (body %s)", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestResendVerification(t *testing.T) {
	h, svc, mailer := newVerifyHandler(true)
	h.verifyResendLimiter = newRateLimiterFromEnv("OPENV_TEST_UNSET_BURST", "OPENV_TEST_UNSET_REFILL", 2, 1)

	do := func(cookie, contentType string) *httptest.ResponseRecorder {
		r := jsonReq(http.MethodPost, "/api/v1/auth/verify-email/resend", `{}`, cookie)
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		h.ResendVerification(w, r)
		return w
	}
	if w := do("", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no cookie: status = %d, want 401", w.Code)
	}
	if w := do("cookie-pending", "text/plain"); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: status = %d, want 415", w.Code)
	}
	if w := do("cookie-verified", ""); w.Code != http.StatusConflict {
		t.Errorf("already verified: status = %d, want 409", w.Code)
	}
	w := do("cookie-pending", "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("resend: status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["sent_to"] != "pending@example.com" {
		t.Errorf("sent_to = %q", resp["sent_to"])
	}
	if len(mailer.to) != 1 || mailer.to[0] != "pending@example.com" {
		t.Errorf("recipients = %v", mailer.to)
	}
	// Second within the burst succeeds; the third is throttled.
	if w := do("cookie-pending", ""); w.Code != http.StatusAccepted {
		t.Errorf("second resend: status = %d", w.Code)
	}
	if w := do("cookie-pending", ""); w.Code != http.StatusTooManyRequests {
		t.Errorf("third resend: status = %d, want 429", w.Code)
	}

	// Mail failure is reported, not swallowed.
	h.verifyResendLimiter = nil
	mailer.err = errors.New("smtp down")
	if w := do("cookie-pending", ""); w.Code != http.StatusBadGateway {
		t.Errorf("failed send: status = %d, want 502", w.Code)
	}
	mailer.err = nil
	svc.issueErr = users.ErrEmailTaken
	if w := do("cookie-pending", ""); w.Code != http.StatusConflict {
		t.Errorf("issue conflict: status = %d, want 409", w.Code)
	}

	h.emailVerification.Required = false
	svc.issueErr = nil
	if w := do("cookie-pending", ""); w.Code != http.StatusBadRequest {
		t.Errorf("policy off: status = %d, want 400", w.Code)
	}
}

func TestChangeVerificationEmail(t *testing.T) {
	h, svc, mailer := newVerifyHandler(true)
	w := httptest.NewRecorder()
	h.ChangeVerificationEmail(w, jsonReq(http.MethodPost, "/api/v1/auth/verify-email/change", `{"email":"fixed@example.com"}`, "cookie-pending"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("change: status = %d body=%s", w.Code, w.Body.String())
	}
	if len(svc.issued) != 1 || svc.issued[0] != "fixed@example.com" || mailer.to[0] != "fixed@example.com" {
		t.Errorf("issued=%v sent=%v", svc.issued, mailer.to)
	}
	w = httptest.NewRecorder()
	h.ChangeVerificationEmail(w, jsonReq(http.MethodPost, "/api/v1/auth/verify-email/change", `{"email":""}`, "cookie-pending"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty address: status = %d, want 400", w.Code)
	}
	r := jsonReq(http.MethodPost, "/api/v1/auth/verify-email/change", `{"email":"x@example.com"}`, "cookie-pending")
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	h.ChangeVerificationEmail(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: status = %d, want 415", w.Code)
	}
}

func TestAuthConfigReportsVerificationRequirement(t *testing.T) {
	for _, required := range []bool{true, false} {
		h, _, _ := newVerifyHandler(required)
		w := httptest.NewRecorder()
		h.AuthConfig(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["email_verification_required"] != required {
			t.Errorf("email_verification_required = %v, want %v", resp["email_verification_required"], required)
		}
	}
}

// --- middleware gate -------------------------------------------------------

type fakeGateUsers struct {
	users.Service
	byCookie map[string]*users.User
}

func (f *fakeGateUsers) GetBySessionToken(token string) (*users.User, error) {
	if u := f.byCookie[token]; u != nil {
		return u, nil
	}
	return nil, users.ErrSessionInvalid
}
func (f *fakeGateUsers) SessionByToken(string) (*users.Session, error) {
	return &users.Session{}, nil
}

type fakeGateOrgs struct{ orgs.Service }

func (fakeGateOrgs) IsMember(string, string) (bool, error) { return false, nil }
func (fakeGateOrgs) EnsurePersonalOrg(userID, _ string) (*orgs.Org, bool, error) {
	return &orgs.Org{ID: "org-" + userID}, false, nil
}

func TestMiddlewareWallsUnverifiedSessions(t *testing.T) {
	m := &AuthMiddleware{
		userService: &fakeGateUsers{byCookie: map[string]*users.User{
			"pending":  {ID: "u-pending", EmailVerified: false},
			"verified": {ID: "u-ok", EmailVerified: true},
		}},
		orgService:    fakeGateOrgs{},
		workerService: fakeWorkerKeys{},
		runService:    fakeRunLookup{},
	}
	var reached bool
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	call := func(path, cookie string) (int, string) {
		reached = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	// Policy off: everyone passes.
	if code, _ := call("/api/v1/projects", "pending"); code != http.StatusOK || !reached {
		t.Errorf("policy off: pending session got %d, reached=%v", code, reached)
	}

	m.SetEmailVerificationPolicy(users.EmailVerificationPolicy{Required: true})
	code, body := call("/api/v1/projects", "pending")
	if code != http.StatusForbidden || reached {
		t.Errorf("policy on: pending session got %d, reached=%v", code, reached)
	}
	var eb errorBody
	_ = json.Unmarshal([]byte(body), &eb)
	if eb.Code != ErrCodeEmailUnverified {
		t.Errorf("error code = %q, want %q (body %s)", eb.Code, ErrCodeEmailUnverified, body)
	}
	if code, _ := call("/api/v1/projects", "verified"); code != http.StatusOK || !reached {
		t.Errorf("verified session got %d, reached=%v", code, reached)
	}
	// The open auth paths are never gated: sign-out and the verification
	// endpoints must stay reachable.
	if code, _ := call("/api/v1/auth/logout", "pending"); code != http.StatusOK || !reached {
		t.Errorf("open path got %d, reached=%v", code, reached)
	}
	// Bearer credentials never meet the wall.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	r.Header.Set("Authorization", "Bearer pool-secret")
	m.SetPoolKey("pool-secret")
	reached = false
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !reached {
		t.Errorf("bearer credential got %d, reached=%v", w.Code, reached)
	}
	_ = context.Background()
}
