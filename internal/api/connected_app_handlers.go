package api

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// Connected apps (REQ-44): the consent the app's /connect page asks
// a member for, and the member's list of the AI clients they have attached,
// each revocable. Only the member themselves, signed in, uses these: a
// worker key, a run token or a connected app is refused.

// registerConnectedAppRoutes wires the consent and the member's list.
func (h *Handler) registerConnectedAppRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/oauth/consent", h.GetOAuthConsent).Methods("GET")
	// A consent only grants access, so a workspace over its plan may still
	// be chosen: whatever the app then writes meets the plan's gate as any
	// write does.
	router.HandleFunc("/api/v1/oauth/consent", h.alwaysWritable(h.DecideOAuthConsent)).Methods("POST")
	router.HandleFunc("/api/v1/me/connected-apps", h.ListConnectedApps).Methods("GET")
	router.HandleFunc("/api/v1/me/connected-apps/{id}", h.alwaysWritable(h.RevokeConnectedApp)).Methods("DELETE")
	router.HandleFunc("/api/v1/me/connected-apps/{id}/workspaces", h.alwaysWritable(h.SetConnectedAppWorkspaces)).Methods("PUT")
}

// grantableWorkspaces checks the workspaces a member is giving a connected
// app: they are a member of each, each has reached connected apps on its
// channel, and, beyond the first, each has reached connected apps with
// more than one workspace. It answers and reports false on the first that
// fails.
func (h *Handler) grantableWorkspaces(w http.ResponseWriter, r *http.Request, orgIDs []string) bool {
	for _, orgID := range orgIDs {
		if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
			return false
		}
		if !h.featureEnabled(r, orgID, release.FeatureRemoteMCP) {
			writeJSONError(w, http.StatusForbidden, "connecting an AI app is not yet available on this workspace's release channel")
			return false
		}
		if len(orgIDs) > 1 && !h.featureEnabled(r, orgID, release.FeatureConnectedAppWorkspaces) {
			writeJSONError(w, http.StatusForbidden, "giving an AI app more than one workspace is not yet available on this workspace's release channel")
			return false
		}
	}
	return true
}

// inPersonRefused refuses a connected app, which may not manage connected
// apps; workers and run tokens are refused by requireHumanUser after it.
func (h *Handler) inPersonRefused(w http.ResponseWriter, r *http.Request) bool {
	if h.connectedAppsOff(w) {
		return true
	}
	if ConnectedApp(r) != nil {
		writeJSONError(w, http.StatusForbidden, "a connected app cannot manage connected apps")
		return true
	}
	return false
}

// writeConsentRequestError answers a consent whose client or redirect URI
// is wrong. Nothing is sent to the redirect URI: it is not one the client
// registered.
func writeConsentRequestError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, connectedapps.ErrInvalidClient):
		writeJSONError(w, http.StatusBadRequest, "this app is not registered with OpenV; start the connection again from the app")
	case errors.Is(err, connectedapps.ErrInvalidRedirect):
		writeJSONError(w, http.StatusBadRequest, "this app did not register the address it asked to return to; start the connection again from the app")
	default:
		respondInternal(w, r, "failed to check the app", err)
	}
}

// GetOAuthConsent describes the app asking for consent: its name and where
// the member will be sent back to.
func (h *Handler) GetOAuthConsent(w http.ResponseWriter, r *http.Request) {
	if h.inPersonRefused(w, r) {
		return
	}
	if _, ok := h.requireHumanUser(w, r); !ok {
		return
	}
	q := r.URL.Query()
	c, err := h.ConnectedApps.Authorizing(q.Get("client_id"), q.Get("redirect_uri"))
	if err != nil {
		writeConsentRequestError(w, r, err)
		return
	}
	redirectHost := ""
	if u, err := url.Parse(q.Get("redirect_uri")); err == nil {
		redirectHost = u.Host
		if redirectHost == "" {
			redirectHost = u.Scheme + ":"
		}
	}
	writeJSONOK(w, map[string]interface{}{
		"client_id":     c.ID,
		"client_name":   c.Name,
		"redirect_host": redirectHost,
	})
}

// DecideOAuthConsent records the member's answer and tells the page where
// to send the browser: back to the app with a code, or with access_denied.
func (h *Handler) DecideOAuthConsent(w http.ResponseWriter, r *http.Request) {
	if h.inPersonRefused(w, r) {
		return
	}
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	var req struct {
		ClientID            string `json:"client_id"`
		RedirectURI         string `json:"redirect_uri"`
		CodeChallenge       string `json:"code_challenge"`
		CodeChallengeMethod string `json:"code_challenge_method"`
		State               string `json:"state"`
		Scope               string `json:"scope"`
		// OrgIDs are the workspaces the member chose; OrgID, one, is what
		// the page sent before more than one could be chosen.
		OrgIDs  []string `json:"org_ids"`
		OrgID   string   `json:"org_id"`
		Approve bool     `json:"approve"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := h.ConnectedApps.Authorizing(req.ClientID, req.RedirectURI); err != nil {
		writeConsentRequestError(w, r, err)
		return
	}
	back := func(params map[string]string) {
		params["state"] = req.State
		params["iss"] = h.oauthIssuer()
		writeJSONOK(w, map[string]string{"redirect_to": oauthRedirect(req.RedirectURI, params)})
	}
	if !req.Approve {
		back(map[string]string{"error": "access_denied", "error_description": "the member declined"})
		return
	}
	if req.CodeChallengeMethod != "S256" {
		writeJSONError(w, http.StatusBadRequest, "the app must use PKCE with S256; start the connection again from the app")
		return
	}
	orgIDs := req.OrgIDs
	if len(orgIDs) == 0 && req.OrgID != "" {
		orgIDs = []string{req.OrgID}
	}
	orgIDs, err := connectedapps.NormalizeWorkspaces(orgIDs)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "choose at least one workspace for the app, and no more than 50")
		return
	}
	if !h.grantableWorkspaces(w, r, orgIDs) {
		return
	}
	code, err := h.ConnectedApps.Approve(userID, orgIDs, req.ClientID, req.RedirectURI, req.CodeChallenge, req.Scope)
	switch {
	case errors.Is(err, connectedapps.ErrInvalidScope):
		writeJSONError(w, http.StatusBadRequest, "the scope is openv:read, or openv:read and openv:write")
		return
	case errors.Is(err, connectedapps.ErrInvalidRequest):
		writeJSONError(w, http.StatusBadRequest, "the app sent no valid PKCE challenge; start the connection again from the app")
		return
	case err != nil:
		respondInternal(w, r, "failed to record the consent", err)
		return
	}
	back(map[string]string{"code": code})
}

// ListConnectedApps lists the member's connected apps across workspaces.
func (h *Handler) ListConnectedApps(w http.ResponseWriter, r *http.Request) {
	if h.inPersonRefused(w, r) {
		return
	}
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	list, err := h.ConnectedApps.List(userID)
	if err != nil {
		respondInternal(w, r, "failed to list connected apps", err)
		return
	}
	writeJSONOK(w, list)
}

// RevokeConnectedApp revokes one of the member's connected apps: its tokens
// stop working at once.
func (h *Handler) RevokeConnectedApp(w http.ResponseWriter, r *http.Request) {
	if h.inPersonRefused(w, r) {
		return
	}
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	err := h.ConnectedApps.RevokeForUser(userID, mux.Vars(r)["id"])
	if errors.Is(err, connectedapps.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "connected app not found")
		return
	}
	if err != nil {
		respondInternal(w, r, "failed to revoke the connected app", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetConnectedAppWorkspaces changes the workspaces one of the member's
// connected apps reaches, without connecting it again: the app's next call
// reaches the new set, and a workspace taken out is out at once.
func (h *Handler) SetConnectedAppWorkspaces(w http.ResponseWriter, r *http.Request) {
	if h.inPersonRefused(w, r) {
		return
	}
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	var req struct {
		OrgIDs []string `json:"org_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	orgIDs, err := connectedapps.NormalizeWorkspaces(req.OrgIDs)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "choose at least one workspace for the app, and no more than 50; to stop it reaching any, disconnect it")
		return
	}
	if !h.grantableWorkspaces(w, r, orgIDs) {
		return
	}
	g, err := h.ConnectedApps.SetWorkspacesForUser(userID, mux.Vars(r)["id"], orgIDs)
	if errors.Is(err, connectedapps.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "connected app not found")
		return
	}
	if err != nil {
		respondInternal(w, r, "failed to change the connected app's workspaces", err)
		return
	}
	writeJSONOK(w, g)
}
