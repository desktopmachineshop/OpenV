package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Linking a single sign-on provider to a password account
// (users/identity_link.go). A Google or OIDC callback that meets an account
// created with a password does not sign in: it hands the browser a link
// request, in an HttpOnly cookie, and sends it to the sign-in page, which
// asks for the account's password. The password spends the request, the
// provider joins the account's sign-in methods, and later sign-ons through
// it go straight in.

func (h *Handler) registerIdentityLinkRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/auth/link", h.PreviewIdentityLink).Methods("GET")
	router.HandleFunc("/api/v1/auth/link", h.ConfirmIdentityLink).Methods("POST")
}

// PreviewIdentityLink tells the sign-in page which provider is being linked
// to which address, so it can ask for the right password.
func (h *Handler) PreviewIdentityLink(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(identityLinkCookie)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, users.ErrLinkInvalid.Error())
		return
	}
	preview, err := h.UserService.PreviewIdentityLink(cookie.Value)
	if err != nil {
		if errors.Is(err, users.ErrLinkInvalid) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		respondInternal(w, r, "failed to read the sign-in link", err)
		return
	}
	writeJSONBare(w, preview)
}

// ConfirmIdentityLink spends the browser's link request with the account's
// password and signs in. It is throttled like a password sign-in, since it
// is one.
func (h *Handler) ConfirmIdentityLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ip := clientIP(r)
	if ok, retryAfter := h.authIPLimiter.allow(ip); !ok {
		writeRateLimited(w, "Too many sign-in attempts from this address; try again later.", retryAfter)
		return
	}
	cookie, err := r.Cookie(identityLinkCookie)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, users.ErrLinkInvalid.Error())
		return
	}
	preview, err := h.UserService.PreviewIdentityLink(cookie.Value)
	if err != nil {
		if errors.Is(err, users.ErrLinkInvalid) {
			h.clearOIDCFlowCookie(w, identityLinkCookie)
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		respondInternal(w, r, "failed to read the sign-in link", err)
		return
	}
	account := accountKey(preview.Email)
	if ok, retryAfter := h.authAccountLimiter.check(account); !ok {
		writeRateLimited(w, "Too many failed sign-in attempts for this account; try again later.", retryAfter)
		return
	}
	user, token, err := h.UserService.ConfirmIdentityLink(cookie.Value, req.Password)
	switch {
	case errors.Is(err, users.ErrPasswordIncorrect):
		h.authAccountLimiter.penalize(account)
		writeJSONError(w, http.StatusUnauthorized, "incorrect password")
		return
	case errors.Is(err, users.ErrLinkInvalid):
		h.clearOIDCFlowCookie(w, identityLinkCookie)
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		respondInternal(w, r, "failed to link the sign-in method", err)
		return
	}
	h.clearOIDCFlowCookie(w, identityLinkCookie)
	slog.Info("sign-in method linked", slog.String("user_id", user.ID), slog.String("provider", preview.Provider))
	h.provisionPersonalWorkspace(user.ID, user.Name)
	// The provider asserted this address as verified before the request was
	// minted, and the password showed the account is its owner's: the same
	// proof a sign-on through that provider gives.
	h.acceptInvitationsForProviderVerifiedEmail(user.ID, user.Email)
	h.setSessionCookie(w, token)
	writeJSONBare(w, user)
}
