package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeNotificationService records which user id every call was scoped to,
// proving the handlers always pass the session user (own-rows-only) and
// never an id from the request.
type fakeNotificationService struct {
	notifications.Service
	listUser    string
	listUnread  bool
	listLimit   int
	readUser    string
	readIDs     []string
	readAllUser string
	list        []*notifications.Notification
	unread      int
}

func (f *fakeNotificationService) ListForUser(userID string, unreadOnly bool, limit int) ([]*notifications.Notification, error) {
	f.listUser, f.listUnread, f.listLimit = userID, unreadOnly, limit
	return f.list, nil
}

func (f *fakeNotificationService) MarkRead(userID string, ids []string) (int64, error) {
	f.readUser, f.readIDs = userID, ids
	return int64(len(ids)), nil
}

func (f *fakeNotificationService) MarkAllRead(userID string) (int64, error) {
	f.readAllUser = userID
	return 3, nil
}

func (f *fakeNotificationService) CountUnread(userID string) (int, error) {
	return f.unread, nil
}

func notificationReq(method, target, body string, user *users.User) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

// TestNotificationEndpointsRequireUser: every notification endpoint answers
// 401 without a signed-in user — run tokens and worker keys never reach the
// service.
func TestNotificationEndpointsRequireUser(t *testing.T) {
	svc := &fakeNotificationService{}
	h := &Handler{notificationService: svc, sseHub: NewSSEHub()}

	calls := []struct {
		name string
		do   func(w http.ResponseWriter, r *http.Request)
		req  *http.Request
	}{
		{"list", h.ListNotifications, notificationReq(http.MethodGet, "/api/v1/notifications", "", nil)},
		{"read", h.MarkNotificationsRead, notificationReq(http.MethodPost, "/api/v1/notifications/read", `{"ids":["n-1"]}`, nil)},
		{"read-all", h.MarkAllNotificationsRead, notificationReq(http.MethodPost, "/api/v1/notifications/read-all", "", nil)},
		{"stream", h.StreamNotifications, notificationReq(http.MethodGet, "/api/v1/notifications/stream", "", nil)},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.do(w, tc.req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
		})
	}
	if svc.listUser != "" || svc.readUser != "" || svc.readAllUser != "" {
		t.Fatal("service must not be reached without an authenticated user")
	}
}

// TestListNotificationsScopedToSessionUser: the listing is always keyed by
// the session user's id, honors unread/limit, and carries the unread count.
func TestListNotificationsScopedToSessionUser(t *testing.T) {
	svc := &fakeNotificationService{
		list:   []*notifications.Notification{{ID: "n-1", UserID: "u-1", Title: "t"}},
		unread: 4,
	}
	h := &Handler{notificationService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.ListNotifications(w, notificationReq(http.MethodGet, "/api/v1/notifications?unread=true&limit=10", "", user))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if svc.listUser != "u-1" || !svc.listUnread || svc.listLimit != 10 {
		t.Fatalf("service called with (user=%q unread=%v limit=%d), want (u-1 true 10)",
			svc.listUser, svc.listUnread, svc.listLimit)
	}
	var resp struct {
		Notifications []*notifications.Notification `json:"notifications"`
		UnreadCount   int                           `json:"unread_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Notifications) != 1 || resp.UnreadCount != 4 {
		t.Fatalf("response = %+v, want 1 notification and unread_count 4", resp)
	}

	// A limit beyond the cap clamps; a bad limit answers 400.
	w = httptest.NewRecorder()
	h.ListNotifications(w, notificationReq(http.MethodGet, "/api/v1/notifications?limit=9999", "", user))
	if svc.listLimit != maxNotificationLimit {
		t.Fatalf("limit = %d, want clamp to %d", svc.listLimit, maxNotificationLimit)
	}
	w = httptest.NewRecorder()
	h.ListNotifications(w, notificationReq(http.MethodGet, "/api/v1/notifications?limit=nope", "", user))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for non-numeric limit", w.Code)
	}
}

// TestMarkReadScopedToSessionUser: mark-read passes the session user id to
// the service (the SQL scopes the update), and validates the body.
func TestMarkReadScopedToSessionUser(t *testing.T) {
	svc := &fakeNotificationService{unread: 1}
	h := &Handler{notificationService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.MarkNotificationsRead(w, notificationReq(http.MethodPost, "/api/v1/notifications/read", `{"ids":["n-1","n-2"]}`, user))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if svc.readUser != "u-1" || len(svc.readIDs) != 2 {
		t.Fatalf("service called with (user=%q ids=%v), want (u-1, 2 ids)", svc.readUser, svc.readIDs)
	}

	for _, body := range []string{`{nope`, `{"ids":[]}`, `{}`} {
		w = httptest.NewRecorder()
		h.MarkNotificationsRead(w, notificationReq(http.MethodPost, "/api/v1/notifications/read", body, user))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, w.Code)
		}
	}

	w = httptest.NewRecorder()
	h.MarkAllNotificationsRead(w, notificationReq(http.MethodPost, "/api/v1/notifications/read-all", "", user))
	if w.Code != http.StatusOK || svc.readAllUser != "u-1" {
		t.Fatalf("read-all: status = %d user = %q, want 200 for u-1", w.Code, svc.readAllUser)
	}
}
