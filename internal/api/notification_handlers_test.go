package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
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
	clearUser   string
	listView    notifications.View
	listBefore  string
	flagUser    string
	flagID      string
	flagValue   bool
	flagFound   bool
	deleteUser  string
	list        []*notifications.Notification
	unread      int
}

func (f *fakeNotificationService) List(userID string, q notifications.ListQuery) ([]*notifications.Notification, error) {
	f.listUser, f.listUnread, f.listLimit = userID, q.UnreadOnly, q.Limit
	f.listView = q.View
	f.listBefore = q.BeforeID
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

func (f *fakeNotificationService) ClearInbox(userID string) (int64, error) {
	f.clearUser = userID
	// A clear empties the inbox, so the badge that follows it is zero.
	f.unread = 0
	return 7, nil
}

func (f *fakeNotificationService) SetFlagged(userID, id string, flagged bool) (bool, error) {
	f.flagUser, f.flagID, f.flagValue = userID, id, flagged
	return f.flagFound, nil
}

func (f *fakeNotificationService) DeleteCleared(userID string) (int64, error) {
	f.deleteUser = userID
	return 4, nil
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
		{"clear", h.ClearNotifications, notificationReq(http.MethodPost, "/api/v1/notifications/clear", "", nil)},
		{"delete-cleared", h.DeleteClearedNotifications, notificationReq(http.MethodDelete, "/api/v1/notifications/cleared", "", nil)},
		{"flag", h.FlagNotification, notificationReq(http.MethodPut, "/api/v1/notifications/n-1/flag", `{"flagged":true}`, nil)},
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
	if svc.listUser != "" || svc.readUser != "" || svc.readAllUser != "" || svc.clearUser != "" ||
		svc.flagUser != "" || svc.deleteUser != "" {
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

// TestClearNotificationsArchivesForSessionUser: clearing is keyed by the
// session user, reports how many rows moved under the name of what happened to
// them, and leaves the caller with an unread count of zero — the badge reads
// from that number, and a cleared row never counts towards it.
func TestClearNotificationsArchivesForSessionUser(t *testing.T) {
	svc := &fakeNotificationService{unread: 4}
	h := &Handler{notificationService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.ClearNotifications(w, notificationReq(http.MethodPost, "/api/v1/notifications/clear", "", user))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.clearUser != "u-1" {
		t.Fatalf("cleared for %q, want the session user u-1", svc.clearUser)
	}
	var body struct {
		Cleared     int64 `json:"cleared"`
		Deleted     int64 `json:"deleted"`
		UnreadCount int   `json:"unread_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Cleared != 7 {
		t.Errorf("cleared = %d, want 7", body.Cleared)
	}
	if body.Deleted != 0 {
		t.Errorf("a clear reported %d deleted; it archives, it does not delete", body.Deleted)
	}
	if body.UnreadCount != 0 {
		t.Errorf("unread_count = %d after a clear, want 0", body.UnreadCount)
	}
}

// TestDeleteClearedNotificationsScopedToSessionUser: the purge is the one
// destructive call, and it is keyed by the session user like everything else.
func TestDeleteClearedNotificationsScopedToSessionUser(t *testing.T) {
	svc := &fakeNotificationService{unread: 0}
	h := &Handler{notificationService: svc}

	w := httptest.NewRecorder()
	h.DeleteClearedNotifications(w, notificationReq(http.MethodDelete, "/api/v1/notifications/cleared", "", &users.User{ID: "u-1"}))

	if w.Code != http.StatusOK || svc.deleteUser != "u-1" {
		t.Fatalf("status = %d deleted for %q, want 200 for u-1", w.Code, svc.deleteUser)
	}
	var body struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Deleted != 4 {
		t.Errorf("deleted = %d, want 4", body.Deleted)
	}
}

// TestListNotificationsHonoursViewAndCursor: the view and the paging cursor
// reach the service, and a malformed cursor is refused rather than silently
// serving the first page again.
func TestListNotificationsHonoursViewAndCursor(t *testing.T) {
	svc := &fakeNotificationService{}
	h := &Handler{notificationService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	h.ListNotifications(w, notificationReq(http.MethodGet,
		"/api/v1/notifications?view=cleared&before=2026-09-12T10:00:00Z%7Cn-9", "", user))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.listView != notifications.ViewCleared {
		t.Errorf("view = %q, want cleared", svc.listView)
	}
	if svc.listBefore != "n-9" {
		t.Errorf("cursor id = %q, want n-9", svc.listBefore)
	}

	// An unknown view falls back to the inbox rather than erroring: the
	// parameter is new, and an old client must keep working.
	w = httptest.NewRecorder()
	h.ListNotifications(w, notificationReq(http.MethodGet, "/api/v1/notifications?view=nonsense", "", user))
	if w.Code != http.StatusOK || svc.listView != notifications.ViewInbox {
		t.Errorf("unknown view: status = %d view = %q, want 200 inbox", w.Code, svc.listView)
	}

	// A malformed cursor is the client's bug, and is named as one.
	w = httptest.NewRecorder()
	h.ListNotifications(w, notificationReq(http.MethodGet, "/api/v1/notifications?before=rubbish", "", user))
	if w.Code != http.StatusBadRequest {
		t.Errorf("malformed cursor: status = %d, want 400", w.Code)
	}
}

// TestFlagNotification: flags for the session user, and answers 404 for an id
// that is not theirs — the same answer a missing one gets, so the endpoint
// cannot be used to discover that a notification exists.
func TestFlagNotification(t *testing.T) {
	svc := &fakeNotificationService{flagFound: true}
	h := &Handler{notificationService: svc}
	user := &users.User{ID: "u-1"}

	w := httptest.NewRecorder()
	req := mux.SetURLVars(
		notificationReq(http.MethodPut, "/api/v1/notifications/n-1/flag", `{"flagged":true}`, user),
		map[string]string{"id": "n-1"},
	)
	h.FlagNotification(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.flagUser != "u-1" || svc.flagID != "n-1" || !svc.flagValue {
		t.Fatalf("flagged (%q, %q, %v), want (u-1, n-1, true)", svc.flagUser, svc.flagID, svc.flagValue)
	}

	// Somebody else's id: nothing matched.
	svc.flagFound = false
	w = httptest.NewRecorder()
	req = mux.SetURLVars(
		notificationReq(http.MethodPut, "/api/v1/notifications/n-2/flag", `{"flagged":true}`, user),
		map[string]string{"id": "n-2"},
	)
	h.FlagNotification(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("another user's id: status = %d, want 404", w.Code)
	}

	// A body with no flagged field cannot be guessed at.
	w = httptest.NewRecorder()
	req = mux.SetURLVars(
		notificationReq(http.MethodPut, "/api/v1/notifications/n-1/flag", `{}`, user),
		map[string]string{"id": "n-1"},
	)
	h.FlagNotification(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing flagged: status = %d, want 400", w.Code)
	}
}
