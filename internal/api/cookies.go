package api

import (
	"net/http"
	"time"
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
