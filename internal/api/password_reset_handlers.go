package api

// Password reset (REQ-158). Two ways to get a reset link and one way to
// spend it:
//
//   - POST /auth/password-reset {email} — the sign-in page's "Forgot
//     password?", on a deployment that can send mail. It answers 202 for
//     every address, known or not, and does its work off the request path,
//     so neither the answer nor its timing says whether an account exists.
//   - POST /admin/users/{id}/password-reset — a platform admin mints a link
//     and hands it over however they talk to the person: the support path,
//     and the only path where there is no mailer. The link is shown once.
//   - POST /auth/password-reset/confirm {token, new_password} — spends the
//     link, sets the password and signs the account out everywhere.
//
// The two auth routes live under /api/v1/auth/, which the middleware leaves
// open; the admin one sits behind it like the rest of platform admin.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/notify"
)

func (h *Handler) registerPasswordResetRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/auth/password-reset", h.RequestPasswordReset).Methods("POST")
	router.HandleFunc("/api/v1/auth/password-reset/confirm", h.ConfirmPasswordReset).Methods("POST")
	router.HandleFunc("/api/v1/admin/users/{id}/password-reset", h.AdminIssuePasswordReset).Methods("POST")
}

// passwordResetEmailAvailable says whether the deployment can email a link.
func (h *Handler) passwordResetEmailAvailable() bool {
	return h.mailer != nil && h.mailer.Enabled()
}

// RequestPasswordReset mails a reset link to an address that has a password
// account. Open, and deliberately uninformative: 202 whether or not the
// address is known, with the lookup and the send done after the answer.
func (h *Handler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := users.NormalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		writeJSONError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	// Server configuration, not account state: safe to say out loud.
	if !h.passwordResetEmailAvailable() {
		writeJSONErrorCode(w, http.StatusConflict,
			"this server cannot send email; ask your OpenV administrator for a reset link", ErrCodeResetEmailUnavailable)
		return
	}
	if ok, retryAfter := h.authIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many attempts from this address; try again later.", retryAfter)
		return
	}
	// Bounded per address whether or not it is an account, so the bucket
	// itself cannot be read for existence either.
	if ok, retryAfter := h.passwordResetLimiter.allow(accountKey(email)); !ok {
		writeRateLimited(w, "Too many reset emails requested for this address; try again later.", retryAfter)
		return
	}

	go h.sendPasswordResetFor(email)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"sent_to": email})
}

// sendPasswordResetFor does the lookup, mint and send for RequestPasswordReset
// after the answer has gone out. Every failure is logged, none is reported:
// there is nobody to report it to who should learn from it.
func (h *Handler) sendPasswordResetFor(email string) {
	user, err := h.userService.FindByEmail(email)
	if err != nil {
		slog.Error("password reset: lookup failed", "error", err)
		return
	}
	if user == nil {
		return
	}
	token, expires, err := h.userService.IssuePasswordReset(user.ID, users.ResetDeliveryEmail, nil)
	if err != nil {
		if !errors.Is(err, users.ErrNoPassword) {
			slog.Error("password reset: could not issue a link", "user_id", user.ID, "error", err)
		}
		return
	}
	link := notify.PasswordResetLink(h.emailLinkBase, token)
	subject, body := notify.RenderPasswordResetEmail(user.Name, link, time.Until(expires))
	if err := h.mailer.Send(user.Email, subject, body); err != nil {
		slog.Error("password reset: could not send the link", "user_id", user.ID, "error", err)
	}
}

// ConfirmPasswordReset spends a reset link and sets the new password.
func (h *Handler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A 256-bit token is not guessable; this only bounds how fast one
	// address can make the database look.
	if ok, retryAfter := h.authIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w, "Too many attempts from this address; try again later.", retryAfter)
		return
	}
	_, err := h.userService.ResetPassword(req.Token, req.NewPassword)
	switch {
	case errors.Is(err, users.ErrWeakPassword):
		writeJSONErrorCode(w, http.StatusBadRequest, err.Error(), ErrCodeWeakPassword)
		return
	case errors.Is(err, users.ErrResetInvalid):
		writeJSONErrorCode(w, http.StatusBadRequest, err.Error(), ErrCodeResetInvalid)
		return
	case err != nil:
		respondInternal(w, r, "failed to reset password", err)
		return
	}
	// No session is created: the person signs in with the password they
	// just chose, on a page that has no token in its address bar.
	w.WriteHeader(http.StatusNoContent)
}

// AdminIssuePasswordReset mints a reset link for an account and answers it
// once: {link, expires_at}. The admin hands it over; nothing is emailed.
func (h *Handler) AdminIssuePasswordReset(w http.ResponseWriter, r *http.Request) {
	caller := h.requirePlatformAdmin(w, r)
	if caller == nil {
		return
	}
	id := mux.Vars(r)["id"]
	token, expires, err := h.userService.IssuePasswordReset(id, users.ResetDeliveryAdmin, &caller.ID)
	switch {
	case errors.Is(err, users.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, users.ErrNoPassword):
		writeJSONErrorCode(w, http.StatusConflict, "this account signs in through an identity provider and has no password to reset", ErrCodeNoPassword)
		return
	case err != nil:
		respondInternal(w, r, "failed to issue a password reset link", err)
		return
	}
	slog.Info("password reset: link minted by a platform admin", "admin_id", caller.ID, "user_id", id, "expires_at", expires.UTC().Format(time.RFC3339))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"link":       notify.PasswordResetLink(h.emailLinkBase, token),
		"expires_at": expires.UTC().Format(time.RFC3339),
	})
}
