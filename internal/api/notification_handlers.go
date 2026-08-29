package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
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
	router.HandleFunc("/api/v1/notifications/stream", h.StreamNotifications).Methods("GET")
	router.HandleFunc("/api/v1/me/notification-prefs", h.GetNotificationPrefs).Methods("GET")
	router.HandleFunc("/api/v1/me/notification-prefs", h.UpdateNotificationPrefs).Methods("PUT")
}

// notificationPrefs is the wire shape for a user's own notification
// preferences (issue #187). Minimal by design: a single email opt-out.
type notificationPrefs struct {
	EmailNotifications bool `json:"email_notifications"`
}

// GetNotificationPrefs returns the caller's own notification preferences.
func (h *Handler) GetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notificationPrefs{EmailNotifications: user.EmailNotifications})
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
	var req notificationPrefs
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.userService.SetEmailNotifications(user.ID, req.EmailNotifications); err != nil {
		respondInternal(w, r, "failed to update notification preferences", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notificationPrefs{EmailNotifications: req.EmailNotifications})
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

	unreadOnly := r.URL.Query().Get("unread") == "true"
	limit := defaultNotificationLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
		if limit > maxNotificationLimit {
			limit = maxNotificationLimit
		}
	}

	list, err := h.notificationService.ListForUser(userID, unreadOnly, limit)
	if err != nil {
		respondInternal(w, r, "failed to load notifications", err)
		return
	}
	unread, err := h.notificationService.CountUnread(userID)
	if err != nil {
		respondInternal(w, r, "failed to count unread notifications", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"notifications": list,
		"unread_count":  unread,
	})
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
