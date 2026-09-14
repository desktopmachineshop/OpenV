package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Password reset (REQ-158) through the handlers, with the user service
// faked: who has an account, who has a password, and what a token does.

type fakeResetService struct {
	users.Service
	byEmail map[string]*users.User
	byID    map[string]*users.User
	issued  []struct {
		userID, delivery string
		by               *string
	}
	resets []struct{ token, password string }
	// resetErr is what ResetPassword answers; nil means success.
	resetErr error
}

func (f *fakeResetService) FindByEmail(email string) (*users.User, error) {
	return f.byEmail[users.NormalizeEmail(email)], nil
}

func (f *fakeResetService) IssuePasswordReset(userID, delivery string, by *string) (string, time.Time, error) {
	u, ok := f.byID[userID]
	if !ok {
		return "", time.Time{}, users.ErrUserNotFound
	}
	if u.PasswordHash == "" {
		return "", time.Time{}, users.ErrNoPassword
	}
	f.issued = append(f.issued, struct {
		userID, delivery string
		by               *string
	}{userID, delivery, by})
	ttl := users.PasswordResetTTL
	if delivery == users.ResetDeliveryAdmin {
		ttl = users.AdminPasswordResetTTL
	}
	return "raw-reset-token", time.Now().Add(ttl), nil
}

func (f *fakeResetService) ResetPassword(token, password string) (*users.User, error) {
	f.resets = append(f.resets, struct{ token, password string }{token, password})
	if f.resetErr != nil {
		return nil, f.resetErr
	}
	return &users.User{ID: "u-owner"}, nil
}

func newResetHandler(mailerOn bool) (*Handler, *fakeResetService, *testMailer) {
	owner := &users.User{ID: "u-owner", Email: "owner@example.com", Name: "Owner", PasswordHash: "x"}
	sso := &users.User{ID: "u-sso", Email: "sso@example.com", Name: "SSO", AuthProvider: "oidc"}
	svc := &fakeResetService{
		byEmail: map[string]*users.User{owner.Email: owner, sso.Email: sso},
		byID:    map[string]*users.User{owner.ID: owner, sso.ID: sso},
	}
	mailer := newTestMailer()
	mailer.enabled = mailerOn
	h := &Handler{
		userService:          svc,
		mailer:               mailer,
		emailLinkBase:        "https://app.example.com",
		authIPLimiter:        newRateLimiter(100, 1),
		passwordResetLimiter: newRateLimiter(2, 1),
	}
	return h, svc, mailer
}

func waitForSend(t *testing.T, mailer *testMailer) {
	t.Helper()
	select {
	case <-mailer.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no reset email was sent")
	}
}

func TestRequestPasswordResetEmailsAKnownAddressAndSaysNothingAboutAnUnknownOne(t *testing.T) {
	h, svc, mailer := newResetHandler(true)
	do := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.RequestPasswordReset(w, jsonReq(http.MethodPost, "/api/v1/auth/password-reset", body, ""))
		return w
	}
	w := do(`{"email":" Owner@Example.com "}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("known address: status = %d body=%s", w.Code, w.Body.String())
	}
	waitForSend(t, mailer)
	if len(mailer.to) != 1 || mailer.to[0] != "owner@example.com" {
		t.Errorf("mail went to %v, want the account's own address", mailer.to)
	}
	if !strings.Contains(mailer.body[0], "https://app.example.com/reset-password?token=raw-reset-token") {
		t.Errorf("mail body lacks the link:\n%s", mailer.body[0])
	}
	if len(svc.issued) != 1 || svc.issued[0].delivery != users.ResetDeliveryEmail || svc.issued[0].by != nil {
		t.Errorf("issued = %+v, want one emailed link with no issuer", svc.issued)
	}

	// Unknown address and an SSO-only account: the same 202, no mail, no
	// link, so the answer says nothing about who has an account.
	for _, email := range []string{"nobody@example.com", "sso@example.com"} {
		w := do(`{"email":"` + email + `"}`)
		if w.Code != http.StatusAccepted {
			t.Errorf("%s: status = %d, want 202", email, w.Code)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if len(mailer.to) != 1 {
		t.Errorf("mails sent = %v, want only the one to the known password account", mailer.to)
	}
	if len(svc.issued) != 1 {
		t.Errorf("links issued = %+v, want only the one", svc.issued)
	}
	// Per-address throttle: the third request for one address is refused.
	do(`{"email":"owner@example.com"}`)
	if w := do(`{"email":"owner@example.com"}`); w.Code != http.StatusTooManyRequests {
		t.Errorf("third request for one address: status = %d, want 429", w.Code)
	}
	if w := do(`{"email":"not-an-address"}`); w.Code != http.StatusBadRequest {
		t.Errorf("bad address: status = %d, want 400", w.Code)
	}
}

func TestRequestPasswordResetWithoutAMailerSaysSo(t *testing.T) {
	h, svc, _ := newResetHandler(false)
	w := httptest.NewRecorder()
	h.RequestPasswordReset(w, jsonReq(http.MethodPost, "/api/v1/auth/password-reset", `{"email":"owner@example.com"}`, ""))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), ErrCodeResetEmailUnavailable) {
		t.Fatalf("no mailer: status = %d body=%s, want 409 reset_email_unavailable", w.Code, w.Body.String())
	}
	if len(svc.issued) != 0 {
		t.Errorf("a link was issued with nowhere to send it: %+v", svc.issued)
	}
}

func TestConfirmPasswordReset(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"ok", nil, http.StatusNoContent, ""},
		{"weak", users.ErrWeakPassword, http.StatusBadRequest, ErrCodeWeakPassword},
		{"invalid", users.ErrResetInvalid, http.StatusBadRequest, ErrCodeResetInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, svc, _ := newResetHandler(true)
			svc.resetErr = tc.err
			w := httptest.NewRecorder()
			h.ConfirmPasswordReset(w, jsonReq(http.MethodPost, "/api/v1/auth/password-reset/confirm", `{"token":"raw-reset-token","new_password":"brand-new-password"}`, ""))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), tc.code) {
				t.Errorf("body %s lacks code %s", w.Body.String(), tc.code)
			}
			if len(svc.resets) != 1 || svc.resets[0].token != "raw-reset-token" || svc.resets[0].password != "brand-new-password" {
				t.Errorf("reset calls = %+v", svc.resets)
			}
			if w.Header().Get("Set-Cookie") != "" {
				t.Error("a reset must not create a session")
			}
		})
	}
	h, _, _ := newResetHandler(true)
	w := httptest.NewRecorder()
	h.ConfirmPasswordReset(w, jsonReq(http.MethodPost, "/api/v1/auth/password-reset/confirm", `{`, ""))
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad body: status = %d, want 400", w.Code)
	}
}

func adminResetReq(id string, user *users.User) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+id+"/password-reset", nil)
	r = mux.SetURLVars(r, map[string]string{"id": id})
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

func TestAdminIssuePasswordReset(t *testing.T) {
	root := &users.User{ID: "root", IsAdmin: true}
	h, svc, mailer := newResetHandler(false)
	w := httptest.NewRecorder()
	h.AdminIssuePasswordReset(w, adminResetReq("u-owner", root))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Link      string `json:"link"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Link != "https://app.example.com/reset-password?token=raw-reset-token" {
		t.Errorf("link = %q", got.Link)
	}
	if at, err := time.Parse(time.RFC3339, got.ExpiresAt); err != nil || time.Until(at) < users.AdminPasswordResetTTL-time.Minute {
		t.Errorf("expires_at = %q (%v), want about %s out", got.ExpiresAt, err, users.AdminPasswordResetTTL)
	}
	if len(svc.issued) != 1 || svc.issued[0].delivery != users.ResetDeliveryAdmin || svc.issued[0].by == nil || *svc.issued[0].by != "root" {
		t.Errorf("issued = %+v, want one admin link issued by root", svc.issued)
	}
	if len(mailer.to) != 0 {
		t.Errorf("an admin-minted link was emailed to %v; the admin hands it over", mailer.to)
	}

	refusals := []struct {
		name   string
		id     string
		user   *users.User
		status int
	}{
		{"anonymous", "u-owner", nil, http.StatusUnauthorized},
		{"not an admin", "u-owner", &users.User{ID: "u-owner"}, http.StatusForbidden},
		{"unknown account", "nobody", root, http.StatusNotFound},
		{"sso account", "u-sso", root, http.StatusConflict},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			h, svc, _ := newResetHandler(false)
			w := httptest.NewRecorder()
			h.AdminIssuePasswordReset(w, adminResetReq(tc.id, tc.user))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if len(svc.issued) != 0 {
				t.Errorf("a refused request still issued %+v", svc.issued)
			}
			if tc.name == "sso account" && !strings.Contains(w.Body.String(), ErrCodeNoPassword) {
				t.Errorf("sso refusal carries no code: %s", w.Body.String())
			}
		})
	}
}

func TestAuthConfigReportsWhetherResetMailIsAvailable(t *testing.T) {
	for _, on := range []bool{true, false} {
		h, _, _ := newResetHandler(on)
		w := httptest.NewRecorder()
		h.AuthConfig(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil))
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["password_reset_email"] != on {
			t.Errorf("mailer on=%v: password_reset_email = %v", on, got["password_reset_email"])
		}
	}
}
