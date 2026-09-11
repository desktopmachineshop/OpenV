package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakePasswordService records what the handler asked for and answers with
// whichever domain error the case under test needs.
type fakePasswordService struct {
	users.Service
	err  error
	call struct {
		userID, current, next, keep string
	}
	calls int
}

func (f *fakePasswordService) ChangePassword(userID, current, next, keep string) error {
	f.calls++
	f.call.userID, f.call.current, f.call.next, f.call.keep = userID, current, next, keep
	return f.err
}

func passwordReq(body, cookie string, user *users.User) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/me/password", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.13:7777"
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie})
	}
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

func newPasswordHandler(err error) (*Handler, *fakePasswordService) {
	svc := &fakePasswordService{err: err}
	return &Handler{
		userService:        svc,
		authAccountLimiter: newRateLimiter(100, 1),
	}, svc
}

func TestChangePasswordSucceeds(t *testing.T) {
	h, svc := newPasswordHandler(nil)
	user := &users.User{ID: "u-1", Email: "owner@example.com"}

	rec := httptest.NewRecorder()
	h.ChangePassword(rec, passwordReq(`{"current_password":"old-one","new_password":"new-secret"}`, "cookie-live", user))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if svc.call.userID != "u-1" || svc.call.current != "old-one" || svc.call.next != "new-secret" {
		t.Errorf("call = %+v", svc.call)
	}
	// The caller's own session token is passed through, so their browser is
	// the one session that survives.
	if svc.call.keep != "cookie-live" {
		t.Errorf("keep = %q, want the caller's session token", svc.call.keep)
	}
}

func TestChangePasswordStatusPerDomainError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		want     int
		wantCode string
	}{
		{"wrong current password", users.ErrPasswordIncorrect, http.StatusForbidden, ErrCodePasswordIncorrect},
		{"too short", users.ErrWeakPassword, http.StatusBadRequest, ErrCodeWeakPassword},
		{"no password to change", users.ErrNoPassword, http.StatusConflict, ErrCodeNoPassword},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newPasswordHandler(tc.err)
			rec := httptest.NewRecorder()
			h.ChangePassword(rec, passwordReq(`{"current_password":"x","new_password":"y"}`, "c",
				&users.User{ID: "u-1", Email: "owner@example.com"}))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var body errorBody
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Code != tc.wantCode || body.Error == "" {
				t.Errorf("body = %+v", body)
			}
		})
	}
}

func TestChangePasswordRefusesAnAnonymousCaller(t *testing.T) {
	h, svc := newPasswordHandler(nil)
	rec := httptest.NewRecorder()
	h.ChangePassword(rec, passwordReq(`{"current_password":"x","new_password":"y"}`, "c", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if svc.calls != 0 {
		t.Error("an anonymous request must not reach the service")
	}
}

// Guessing the current password spends the account's sign-in budget, so a
// borrowed browser cannot brute-force it.
func TestChangePasswordThrottlesGuesses(t *testing.T) {
	svc := &fakePasswordService{err: users.ErrPasswordIncorrect}
	h := &Handler{userService: svc, authAccountLimiter: newRateLimiter(3, 1)}
	user := &users.User{ID: "u-1", Email: "Owner@Example.com"}

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ChangePassword(rec, passwordReq(`{"current_password":"guess","new_password":"whatever"}`, "c", user))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("guess %d: %d", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ChangePassword(rec, passwordReq(`{"current_password":"guess","new_password":"whatever"}`, "c", user))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after the budget is spent: %d %v", rec.Code, rec.Header())
	}
	if svc.calls != 3 {
		t.Errorf("service calls = %d, want the throttled attempt to be refused before it", svc.calls)
	}
}
