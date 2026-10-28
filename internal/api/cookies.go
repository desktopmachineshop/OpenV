package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	// The cookie's own expiry tracks the server's absolute session lifetime,
	// so a browser stops presenting a cookie the server would refuse anyway
	// (REQ-99). Idle expiry is not expressible in a cookie and stays a
	// server-side check.
	maxAge := h.SessionPolicy.Normalized().MaxAge
	http.SetCookie(w, &http.Cookie{
		Name:        SessionCookieName,
		Value:       token,
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		Expires:     time.Now().Add(maxAge),
		MaxAge:      int(maxAge.Seconds()),
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:        SessionCookieName,
		Value:       "",
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      -1,
	})
}

// partitionedCookies reports whether auth cookies carry the Partitioned
// attribute: only for cross-site deployments (SameSite=None), where a
// partitioned cookie is the one third-party cookie Chromium-based browsers
// still store. Same-site deployments must not set it — a partitioned cookie
// is keyed by the top-level site as well, which is pointless there.
func (h *Handler) partitionedCookies() bool {
	return h.cookieSameSite == http.SameSiteNoneMode
}

func (h *Handler) setOIDCFlowCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:        name,
		Value:       value,
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      600,
	})
}

func (h *Handler) clearOIDCFlowCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:        name,
		Value:       "",
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      -1,
	})
}

// VisitorCookieName holds the random id that names a browser to the site
// visitor dashboard (usermetrics/visitors.go). It identifies no account and
// opens nothing.
const VisitorCookieName = "openv_vid"

// visitorCookieMaxAge keeps a visitor's id for a year after its last visit.
const visitorCookieMaxAge = 365 * 24 * time.Hour

func (h *Handler) setVisitorCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:        VisitorCookieName,
		Value:       id,
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      int(visitorCookieMaxAge.Seconds()),
	})
}

// identityLinkCookie holds the raw link request token between the callback
// and the password form; it lives as long as the request does.
const identityLinkCookie = "openv_identity_link"

// offerIdentityLink answers a callback whose sign-on met an account of
// another method (users.ErrProviderMismatch). An account with a password is
// offered the link: the browser goes to the sign-in page holding the
// request. Any other account keeps the 409, since it has no password to
// prove itself with.
func (h *Handler) offerIdentityLink(w http.ResponseWriter, r *http.Request, provider, email, frontendURL string) {
	token, err := h.UserService.RequestIdentityLink(provider, email)
	if err != nil {
		if errors.Is(err, users.ErrNoPassword) || errors.Is(err, users.ErrUserNotFound) {
			writeJSONError(w, http.StatusConflict, users.ErrProviderMismatch.Error())
			return
		}
		respondInternal(w, r, "failed to start linking the sign-in method", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:        identityLinkCookie,
		Value:       token,
		Path:        "/",
		HttpOnly:    true,
		Secure:      h.secureCookies,
		SameSite:    h.cookieSameSite,
		Partitioned: h.partitionedCookies(),
		MaxAge:      int(users.IdentityLinkTTL.Seconds()),
	})
	http.Redirect(w, r, linkPageURL(frontendURL, provider), http.StatusFound)
}

// linkPageURL is the sign-in page on the app's origin, where a sign-on
// would have landed, opened to ask for the password.
func linkPageURL(frontendURL, provider string) string {
	page := "/login?link=" + url.QueryEscape(provider)
	if u, err := url.Parse(frontendURL); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + page
	}
	return page
}

// ownSignInRefused guards OpenV's own ways in: password sign-in, sign-up and
// reset, and Google. On a deployment that signs everyone in through its
// single sign-on provider only (OPENV_OIDC_ONLY) it answers 403 sso_only,
// naming the provider, and returns true: the caller stops. Linking a
// provider to a password account is not one of them: it stays open in that
// mode.
func (h *Handler) ownSignInRefused(w http.ResponseWriter) bool {
	if !h.ssoOnly() {
		return false
	}
	writeJSONErrorCode(w, http.StatusForbidden,
		"this server signs everyone in through "+h.OIDC.displayName()+" only", ErrCodeSSOOnly)
	return true
}

// ssoOnly reports whether single sign-on through the configured provider is
// the only way in (OPENV_OIDC_ONLY). It needs the provider configured: the
// setting alone turns nothing off.
func (h *Handler) ssoOnly() bool {
	return h.OIDC.Enabled() && h.OIDC.Only
}
