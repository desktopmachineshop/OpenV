package api

// Password change (REQ-99 / HAZ-16). The account's own route, behind the
// normal session middleware. Changing the password ends every other session
// of the account, because the reason to change one is usually that the old
// one — and whatever it opened — is no longer trusted.

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/users"
)

func (h *Handler) registerPasswordRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/password", h.ChangePassword).Methods("PUT")
}

// ChangePassword replaces the caller's password and signs their other
// browsers out. The current password is required even though the session
// already proves who the caller is: it is what makes a borrowed, unlocked
// browser unable to take the account over.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A wrong current password is a credential guess like any other, so it
	// spends the account's sign-in budget rather than being unlimited.
	if ok, retryAfter := h.authAccountLimiter.check(accountKey(user.Email)); !ok {
		writeRateLimited(w, "Too many attempts for this account; try again later.", retryAfter)
		return
	}

	// The caller's own session survives; every other one dies.
	keep := ""
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		keep = cookie.Value
	}
	err := h.userService.ChangePassword(user.ID, req.CurrentPassword, req.NewPassword, keep)
	switch {
	case errors.Is(err, users.ErrNoPassword):
		writeJSONErrorCode(w, http.StatusConflict, err.Error(), ErrCodeNoPassword)
		return
	case errors.Is(err, users.ErrPasswordIncorrect):
		h.authAccountLimiter.penalize(accountKey(user.Email))
		writeJSONErrorCode(w, http.StatusForbidden, err.Error(), ErrCodePasswordIncorrect)
		return
	case errors.Is(err, users.ErrWeakPassword):
		writeJSONErrorCode(w, http.StatusBadRequest, err.Error(), ErrCodeWeakPassword)
		return
	case err != nil:
		respondInternal(w, r, "failed to change password", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
