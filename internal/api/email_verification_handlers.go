package api

// Sign-up email verification (SEC-15 / REQ-95). Registration signs the user
// in but, while the deployment requires verification, the auth middleware
// refuses every request outside the open auth paths until the emailed link
// is confirmed. These handlers are that path: confirm a link, resend it, or
// send it to a corrected address. They live under /api/v1/auth/, which the
// middleware leaves open, so the two that act on behalf of an account read
// the session cookie themselves (sessionUser) and never accept Bearer
// credentials.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/notify"
)

// requireJSONBody refuses a cookie-authenticated POST that did not declare a
// JSON body. A cross-site HTML form can post text/plain without a CORS
// preflight; requiring application/json forces the preflight, which the
// CORS middleware answers only for the configured frontend origin. That is
// what keeps a hostile page from redirecting a walled account's
// verification mail through the victim's own browser.
func requireJSONBody(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSONError(w, http.StatusUnsupportedMediaType, "expected a JSON body (Content-Type: application/json)")
		return false
	}
	return true
}

// VerifyEmail confirms an emailed link. Open: the person may follow the link
// in a browser that holds no session, or one that does. It sets no cookie;
// the SPA decides where to go next from the session it already has.
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
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
	user, err := h.userService.ConfirmEmailVerification(req.Token)
	switch {
	case errors.Is(err, users.ErrEmailTaken):
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, users.ErrVerificationInvalid):
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		respondInternal(w, r, "failed to verify email", err)
		return
	}
	// Confirming the link is the proof of control an invitation waits for:
	// every workspace that invited this address gets its member now. This is
	// the path for anyone who signed up without an invite token — including
	// someone the admin invited before they registered at all.
	h.acceptInvitationsForVerifiedEmail(user.ID, user.Email)
	json.NewEncoder(w).Encode(user)
}

// ResendVerification mails a fresh link to the account's current address.
func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	h.sendVerificationFor(w, r, "")
}

// ChangeVerificationEmail mails a fresh link to a corrected address. The
// account's address changes only when that link is confirmed, so a typo can
// be fixed without a support request and an unproven address is never
// applied.
func (h *Handler) ChangeVerificationEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !requireJSONBody(w, r) {
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		writeJSONError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	h.sendVerificationFor(w, r, req.Email)
}

// sendVerificationFor is the shared body of resend and change: authenticate
// the session, check the policy and the account's state, throttle per
// account, mint the link and send it synchronously so the person sees a real
// outcome (202 sent, or 502 when the mail could not go out).
func (h *Handler) sendVerificationFor(w http.ResponseWriter, r *http.Request, email string) {
	if email == "" && !requireJSONBody(w, r) {
		return
	}
	user := h.sessionUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if !h.emailVerification.Required {
		writeJSONError(w, http.StatusBadRequest, "email verification is not required on this server")
		return
	}
	if user.EmailVerified {
		writeJSONError(w, http.StatusConflict, users.ErrAlreadyVerified.Error())
		return
	}
	if ok, retryAfter := h.verifyResendLimiter.allow(user.ID); !ok {
		writeRateLimited(w, "Too many verification emails requested; try again later.", retryAfter)
		return
	}
	sentTo, err := h.issueAndSend(user, email, true)
	switch {
	case errors.Is(err, users.ErrEmailTaken), errors.Is(err, users.ErrAlreadyVerified):
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, errVerificationSend):
		writeJSONError(w, http.StatusBadGateway, "we could not send the verification email; try again in a minute")
		return
	case err != nil:
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"sent_to": sentTo})
}

// errVerificationSend wraps a mailer failure so the caller can tell "the
// link could not be sent" from "the link could not be issued".
var errVerificationSend = errors.New("verification email could not be sent")

// sendVerificationAsync mints and mails a link without making the request
// wait (registration). Failures are logged; the wall's Resend is the retry.
func (h *Handler) sendVerificationAsync(user *users.User, email string) {
	go func() {
		if _, err := h.issueAndSend(user, email, false); err != nil {
			slog.Error("email verification: failed to send sign-up link", "user_id", user.ID, "error", err)
		}
	}()
}

// issueAndSend mints a link through the user service and mails it. wait
// bounds the send at notify.VerificationSendTimeout and reports its failure;
// otherwise the send is left to finish (and log) on its own.
func (h *Handler) issueAndSend(user *users.User, email string, wait bool) (string, error) {
	token, sentTo, err := h.userService.IssueEmailVerification(user.ID, email)
	if err != nil {
		return "", err
	}
	link := notify.VerificationLink(h.emailLinkBase, token)
	subject, body := notify.RenderVerificationEmail(user.Name, link, users.EmailVerificationTTL)
	if h.mailer == nil || !h.mailer.Enabled() {
		return "", errVerificationSend
	}
	if !wait {
		if err := h.mailer.Send(sentTo, subject, body); err != nil {
			return "", errors.Join(errVerificationSend, err)
		}
		return sentTo, nil
	}
	if err := notify.SendWithTimeout(h.mailer, sentTo, subject, body, notify.VerificationSendTimeout); err != nil {
		return "", errors.Join(errVerificationSend, err)
	}
	return sentTo, nil
}
