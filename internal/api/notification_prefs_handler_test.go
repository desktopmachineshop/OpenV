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

// fakeUserPrefService records the SetEmailNotifications call so the test can
// assert the update is scoped to the session user (own-user only).
type fakeUserPrefService struct {
	users.Service
	setUser    string
	setEnabled bool
	setCalled  bool
}

func (f *fakeUserPrefService) SetEmailNotifications(userID string, enabled bool) error {
	f.setUser, f.setEnabled, f.setCalled = userID, enabled, true
	return nil
}

func prefReq(method, body string, user *users.User) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/api/v1/me/notification-prefs", nil)
	} else {
		r = httptest.NewRequest(method, "/api/v1/me/notification-prefs", strings.NewReader(body))
	}
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

// TestNotificationPrefsRequireUser: both endpoints answer 401 without a
// signed-in user, and the service is never reached.
func TestNotificationPrefsRequireUser(t *testing.T) {
	svc := &fakeUserPrefService{}
	h := &Handler{userService: svc}

	for _, tc := range []struct {
		name string
		do   func(http.ResponseWriter, *http.Request)
		req  *http.Request
	}{
		{"get", h.GetNotificationPrefs, prefReq(http.MethodGet, "", nil)},
		{"put", h.UpdateNotificationPrefs, prefReq(http.MethodPut, `{"email_notifications":false}`, nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.do(w, tc.req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
		})
	}
	if svc.setCalled {
		t.Fatal("service must not be reached without an authenticated user")
	}
}

// TestGetNotificationPrefsReflectsUser: GET returns the session user's stored
// opt-out value.
func TestGetNotificationPrefsReflectsUser(t *testing.T) {
	h := &Handler{userService: &fakeUserPrefService{}}
	for _, want := range []bool{true, false} {
		user := &users.User{ID: "u-1", EmailNotifications: want}
		w := httptest.NewRecorder()
		h.GetNotificationPrefs(w, prefReq(http.MethodGet, "", user))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		var resp notificationPrefs
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if resp.EmailNotifications != want {
			t.Fatalf("email_notifications = %v, want %v", resp.EmailNotifications, want)
		}
	}
}

// TestUpdateNotificationPrefsOwnUserOnly: PUT persists the new value keyed to
// the SESSION user's id — never an id from the request body or URL.
func TestUpdateNotificationPrefsOwnUserOnly(t *testing.T) {
	svc := &fakeUserPrefService{}
	h := &Handler{userService: svc}
	user := &users.User{ID: "u-session", EmailNotifications: true}

	w := httptest.NewRecorder()
	h.UpdateNotificationPrefs(w, prefReq(http.MethodPut, `{"email_notifications":false}`, user))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if !svc.setCalled || svc.setUser != "u-session" || svc.setEnabled {
		t.Fatalf("SetEmailNotifications(user=%q enabled=%v called=%v), want (u-session, false, true)",
			svc.setUser, svc.setEnabled, svc.setCalled)
	}
	var resp notificationPrefs
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.EmailNotifications {
		t.Fatalf("response email_notifications = true, want false")
	}

	// A malformed body is a 400 and does not touch the service.
	svc.setCalled = false
	w = httptest.NewRecorder()
	h.UpdateNotificationPrefs(w, prefReq(http.MethodPut, `{nope`, user))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: status = %d, want 400", w.Code)
	}
	if svc.setCalled {
		t.Fatal("service reached on malformed body")
	}
}
