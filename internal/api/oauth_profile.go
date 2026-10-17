package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// Helpers the connected-apps files share (REQ-44): the OAuth handlers, the
// consent and the MCP endpoint, and the project and workspace guards, which
// hold a connected app to the workspace its member consented to.

// oauthIssuer is this server's public address: the issuer, and the base of
// every endpoint the metadata names.
func (h *Handler) oauthIssuer() string { return strings.TrimRight(h.PublicAPIURL, "/") }

// connectedAppsOff answers 404 when the deployment has no connected-apps
// service.
func (h *Handler) connectedAppsOff(w http.ResponseWriter) bool {
	if h.ConnectedApps == nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return true
	}
	return false
}

// oauthRedirect adds params, those not empty, to a client's redirect URI.
func oauthRedirect(redirectURI string, params map[string]string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// scopedUser is the signed-in user a project or workspace guard decides
// for, as requireUserMsg answers it, held to the workspaces a connected
// app's grant reaches: a project or workspace of another is, to the app,
// one no row has. org is asked only for a connected app.
func (h *Handler) scopedUser(w http.ResponseWriter, r *http.Request, org func() string, absent notFound) *users.User {
	user := requireUserMsg(w, r, "authentication required", http.StatusUnauthorized)
	if user == nil {
		return nil
	}
	if app := ConnectedApp(r); app != nil && !app.Allows(org()) {
		absent.write(w)
		return nil
	}
	return user
}
