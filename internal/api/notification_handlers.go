package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/notify"
)

// Notification API (issue #132). All endpoints require a signed-in human
// user (agent runs and worker keys are refused) and only ever operate on the
// caller's own rows — the service layer scopes every query by user id.

const (
	defaultNotificationLimit = 50
	maxNotificationLimit     = 200
)

func (h *Handler) registerNotificationRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/notifications", h.ListNotifications).Methods("GET")
	router.HandleFunc("/api/v1/notifications/read", h.MarkNotificationsRead).Methods("POST")
	router.HandleFunc("/api/v1/notifications/read-all", h.MarkAllNotificationsRead).Methods("POST")
	// Clearing archives (POST, because rows move rather than go); deleting
	// what has already been cleared is the one destructive path (DELETE).
	router.HandleFunc("/api/v1/notifications/clear", h.ClearNotifications).Methods("POST")
	router.HandleFunc("/api/v1/notifications/cleared", h.DeleteClearedNotifications).Methods("DELETE")
	router.HandleFunc("/api/v1/notifications/{id}/flag", h.FlagNotification).Methods("PUT")
	router.HandleFunc("/api/v1/notifications/stream", h.StreamNotifications).Methods("GET")
	router.HandleFunc("/api/v1/me/notification-prefs", h.GetNotificationPrefs).Methods("GET")
	router.HandleFunc("/api/v1/me/notification-prefs", h.UpdateNotificationPrefs).Methods("PUT")
}

// notificationPrefs is the wire shape for a user's own notification
// preferences: the email opt-out (issue #187) and the web push opt-in
// (REQ-109). In-app + SSE delivery is not a preference — it is always on.
type notificationPrefs struct {
	EmailNotifications bool `json:"email_notifications"`
	PushNotifications  bool `json:"push_notifications"`
}

// notificationPrefsUpdate is the PUT body. Both fields are POINTERS so a
// client can change one preference without having to know (or resend) the
// other: an absent field is left exactly as it is.
type notificationPrefsUpdate struct {
	EmailNotifications *bool `json:"email_notifications"`
	PushNotifications  *bool `json:"push_notifications"`
}

// GetNotificationPrefs returns the caller's own notification preferences.
func (h *Handler) GetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notificationPrefs{
		EmailNotifications: user.EmailNotifications,
		PushNotifications:  user.PushNotifications,
	})
}

// UpdateNotificationPrefs updates the caller's own notification preferences.
// Own-user only: the update is keyed on the authenticated user id, so a caller
// can never change another user's preferences.
func (h *Handler) UpdateNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req notificationPrefsUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prefs := notificationPrefs{
		EmailNotifications: user.EmailNotifications,
		PushNotifications:  user.PushNotifications,
	}
	if req.EmailNotifications != nil {
		if err := h.userService.SetEmailNotifications(user.ID, *req.EmailNotifications); err != nil {
			respondInternal(w, r, "failed to update notification preferences", err)
			return
		}
		prefs.EmailNotifications = *req.EmailNotifications
	}
	if req.PushNotifications != nil {
		if err := h.userService.SetPushNotifications(user.ID, *req.PushNotifications); err != nil {
			respondInternal(w, r, "failed to update notification preferences", err)
			return
		}
		prefs.PushNotifications = *req.PushNotifications
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prefs)
}

// requireHumanUser answers the current user or writes a 401. Notifications
// are strictly per-person, so run tokens and worker keys never pass.
func (h *Handler) requireHumanUser(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	return user.ID, true
}

// ListNotifications answers the caller's notifications, newest first.
// Query: unread=true limits to unread rows; limit caps the page size.
// The unread count always rides along so the bell badge needs one request.
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}

	query := notifications.ListQuery{
		View:       notifications.ParseView(r.URL.Query().Get("view")),
		UnreadOnly: r.URL.Query().Get("unread") == "true",
		Limit:      defaultNotificationLimit,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		query.Limit = parsed
		if query.Limit > maxNotificationLimit {
			query.Limit = maxNotificationLimit
		}
	}
	// The cursor is opaque to the client: it hands back whatever the previous
	// page's next_cursor said. A malformed one is the client's bug, not a
	// silent first page, so it is refused.
	if raw := r.URL.Query().Get("before"); raw != "" {
		at, id, err := parseNotificationCursor(raw)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "before is not a valid cursor")
			return
		}
		query.BeforeTime, query.BeforeID = at, id
	}

	list, err := h.notificationService.List(userID, query)
	if err != nil {
		respondInternal(w, r, "failed to load notifications", err)
		return
	}
	unread, err := h.notificationService.CountUnread(userID)
	if err != nil {
		respondInternal(w, r, "failed to count unread notifications", err)
		return
	}
	// A full page means there may be more; a short one is the end. The cursor
	// names the last row, so the next page starts strictly after it.
	body := map[string]interface{}{
		"notifications": list,
		"unread_count":  unread,
	}
	if len(list) == query.Limit && query.Limit > 0 {
		last := list[len(list)-1]
		body["next_cursor"] = formatNotificationCursor(last.CreatedAt, last.ID)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(body)
}

// Notification cursors are "<RFC3339Nano>|<id>" — the ordering key of the last
// row of a page, in the same order the listing uses.
func formatNotificationCursor(at time.Time, id string) string {
	return at.UTC().Format(time.RFC3339Nano) + "|" + id
}

func parseNotificationCursor(raw string) (time.Time, string, error) {
	at, id, found := strings.Cut(raw, "|")
	if !found || id == "" {
		return time.Time{}, "", fmt.Errorf("cursor must be \"<timestamp>|<id>\"")
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", err
	}
	return parsed, id, nil
}

// MarkNotificationsRead marks the given ids read. Rows belonging to other
// users are silently unaffected (the update is scoped to the caller).
func (h *Handler) MarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "ids is required")
		return
	}
	updated, err := h.notificationService.MarkRead(userID, req.IDs)
	if err != nil {
		respondInternal(w, r, "failed to mark notifications read", err)
		return
	}
	h.respondNotificationCount(w, r, userID, updated)
}

// MarkAllNotificationsRead marks every unread notification of the caller read.
func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	updated, err := h.notificationService.MarkAllRead(userID)
	if err != nil {
		respondInternal(w, r, "failed to mark notifications read", err)
		return
	}
	h.respondNotificationCount(w, r, userID, updated)
}

// ClearNotifications archives the caller's inbox: every uncleared row is
// stamped cleared and marked read, and stays readable in the cleared view.
//
// POST rather than DELETE because nothing is destroyed — the rows move
// between views. Deleting what has been cleared is a separate, deliberate
// call (DeleteClearedNotifications).
func (h *Handler) ClearNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	cleared, err := h.notificationService.ClearInbox(userID)
	if err != nil {
		respondInternal(w, r, "failed to clear notifications", err)
		return
	}
	h.respondNotificationAction(w, r, userID, "cleared", cleared)
}

// DeleteClearedNotifications permanently removes what the caller has already
// cleared. The one path in the API that destroys a notification, and it can
// only reach rows the member has already put out of the way.
func (h *Handler) DeleteClearedNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	deleted, err := h.notificationService.DeleteCleared(userID)
	if err != nil {
		respondInternal(w, r, "failed to delete cleared notifications", err)
		return
	}
	h.respondNotificationAction(w, r, userID, "deleted", deleted)
}

// FlagNotification flags or unflags one of the caller's own notifications.
//
// The id comes from the path and the service scopes the update by session
// user, so somebody else's id matches nothing and answers 404 — the same
// answer a genuinely missing id gets, which is what keeps the endpoint from
// telling a caller that a notification exists but is not theirs.
func (h *Handler) FlagNotification(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Flagged *bool `json:"flagged"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Flagged == nil {
		writeJSONError(w, http.StatusBadRequest, "flagged must be true or false")
		return
	}
	found, err := h.notificationService.SetFlagged(userID, mux.Vars(r)["id"], *body.Flagged)
	if err != nil {
		respondInternal(w, r, "failed to flag notification", err)
		return
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "notification not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"flagged": *body.Flagged})
}

// respondNotificationAction answers a bulk action with how many rows it
// touched, under the name of what actually happened to them, plus the unread
// count the bell badge reads from.
func (h *Handler) respondNotificationAction(w http.ResponseWriter, r *http.Request, userID, verb string, n int64) {
	unread, err := h.notificationService.CountUnread(userID)
	if err != nil {
		respondInternal(w, r, "failed to count unread notifications", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		verb:           n,
		"unread_count": unread,
	})
}

func (h *Handler) respondNotificationCount(w http.ResponseWriter, r *http.Request, userID string, updated int64) {
	unread, err := h.notificationService.CountUnread(userID)
	if err != nil {
		respondInternal(w, r, "failed to count unread notifications", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"updated":      updated,
		"unread_count": unread,
	})
}

// StreamNotifications serves the caller's live notification stream over SSE.
// New notifications arrive as "notification" events; there is no replay —
// the client fetches the backlog via ListNotifications and then listens.
func (h *Handler) StreamNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	h.sseHub.ServeStream(w, r, notify.StreamKey(userID), nil)
}
