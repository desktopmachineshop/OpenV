package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/providers/usertokens"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// registerProviderLoginRoutes wires the provider CLI login broker: the
// member's side (start, read, submit a code, cancel) and the worker's
// (claim, progress, the full request).
func (h *Handler) registerProviderLoginRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/provider-logins", h.StartProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/claim", h.ClaimProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}", h.GetProviderLogin).Methods("GET")
	router.HandleFunc("/api/v1/provider-logins/{id}/code", h.SubmitProviderLoginCode).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/cancel", h.CancelProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/progress", h.ProgressProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/token", h.SaveProviderLoginToken).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/full", h.GetProviderLoginFull).Methods("GET")
}

// StartProviderLogin creates (or resumes) a CLI sign-in request for a worker
// to execute on a host. Workspace-targeted sign-ins (shared workers) need a
// workspace admin; user-targeted sign-ins run only on the requester's own
// personal runner, so any workspace member may start one.
func (h *Handler) StartProviderLogin(w http.ResponseWriter, r *http.Request) {
	if requireUserMsg(w, r, "authentication required", http.StatusUnauthorized) == nil {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Target   string `json:"target"`
		// Purpose "token" asks the member's cloud runner to mint them a
		// saved subscription token instead of signing its CLI in.
		Purpose string `json:"purpose"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Purpose == providers.LoginPurposeToken {
		h.startTokenLogin(w, r)
		return
	}
	if req.Target == "" {
		req.Target = providers.LoginTargetWorkspace
	}
	minRole := orgs.RoleAdmin
	if req.Target == providers.LoginTargetUser {
		minRole = orgs.RoleMember
	}
	if !h.requireOrgRole(w, r, ActiveOrg(r), minRole) {
		return
	}
	if !h.providerOffered(r, ActiveOrg(r), req.Provider) {
		writeJSONError(w, http.StatusForbidden, h.providerRefusal(req.Provider))
		return
	}
	login, err := h.LoginService.StartLogin(ActiveOrg(r), req.Provider, req.Target, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, login.Sanitized())
}

// startTokenLogin starts a request for the member's cloud runner to mint
// them a saved subscription token: their own request, in the active
// workspace, gated on the workspace's channel.
func (h *Handler) startTokenLogin(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleMember) {
		return
	}
	if !h.featureEnabled(r, ActiveOrg(r), release.FeatureCloudRunnerToken) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	if h.ProviderTokenService == nil {
		writeProviderTokenError(w, r, usertokens.ErrUnavailable)
		return
	}
	userID := CurrentUser(r).ID
	if st, err := h.ProviderTokenService.Status(userID, providers.ProviderClaudeCode); err != nil || !st.Available {
		writeProviderTokenError(w, r, usertokens.ErrUnavailable)
		return
	}
	login, err := h.LoginService.StartTokenLogin(ActiveOrg(r), userID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, login.Sanitized())
}

// userLoginChecked loads a login request and verifies it belongs to the
// caller's active workspace; user-targeted requests are additionally private
// to their requester (org admins excepted). Writes the error response on
// failure.
func (h *Handler) userLoginChecked(w http.ResponseWriter, r *http.Request) *providers.LoginRequest {
	login, err := h.LoginService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "login request not found", err)
		return nil
	}
	if login.OrgID != ActiveOrg(r) {
		writeJSONError(w, http.StatusNotFound, "login request not found")
		return nil
	}
	if login.Target == providers.LoginTargetUser && !h.isOrgAdmin(r, login.OrgID) {
		user := CurrentUser(r)
		if user == nil || login.RequestedBy == nil || *login.RequestedBy != user.ID {
			writeJSONError(w, http.StatusNotFound, "login request not found")
			return nil
		}
	}
	return login
}

// userLoginWriteChecked is userLoginChecked for a change to the request (a
// pasted code, a cancel): a workspace-targeted sign-in, which signs the
// shared workers in, takes the workspace admin rights that start one
// (StartProviderLogin), while a user-targeted one stays its requester's.
// Writes the error response on failure.
func (h *Handler) userLoginWriteChecked(w http.ResponseWriter, r *http.Request) *providers.LoginRequest {
	login := h.userLoginChecked(w, r)
	if login == nil {
		return nil
	}
	if login.Target != providers.LoginTargetUser && !h.requireOrgRole(w, r, login.OrgID, orgs.RoleAdmin) {
		return nil
	}
	return login
}

// GetProviderLogin returns login progress for the UI (code never echoed).
func (h *Handler) GetProviderLogin(w http.ResponseWriter, r *http.Request) {
	if requireUserMsg(w, r, "authentication required", http.StatusUnauthorized) == nil {
		return
	}
	login := h.userLoginChecked(w, r)
	if login == nil {
		return
	}
	writeJSONBare(w, login.Sanitized())
}

// SubmitProviderLoginCode records the user's pasted authorization code.
func (h *Handler) SubmitProviderLoginCode(w http.ResponseWriter, r *http.Request) {
	if requireUserMsg(w, r, "authentication required", http.StatusUnauthorized) == nil {
		return
	}
	if h.userLoginWriteChecked(w, r) == nil {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	login, err := h.LoginService.SubmitCode(mux.Vars(r)["id"], req.Code)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, login.Sanitized())
}

// CancelProviderLogin abandons a login request.
func (h *Handler) CancelProviderLogin(w http.ResponseWriter, r *http.Request) {
	if requireUserMsg(w, r, "authentication required", http.StatusUnauthorized) == nil {
		return
	}
	if h.userLoginWriteChecked(w, r) == nil {
		return
	}
	login, err := h.LoginService.Cancel(mux.Vars(r)["id"])
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, login.Sanitized())
}

// ClaimProviderLogin hands the oldest pending login request this worker may
// execute to the worker: workspace-targeted requests for any worker, plus
// the owner's user-targeted requests for personal runners.
func (h *Handler) ClaimProviderLogin(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	// An older runner sends no body: it cannot mint a token, and is never
	// handed a token request.
	var req workerproto.LoginClaim
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, invalidRequestBody)
		return
	}
	login, err := h.LoginService.Claim(WorkerOrg(r), WorkerUser(r), req.TokenLogins)
	if err != nil {
		respondInternal(w, r, "failed to claim login request", err)
		return
	}
	if login == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Signing a CLI in is use of the runner too — a member part-way through
	// an OAuth flow must not have the runner pulled from under them.
	h.touchRunnerSession(r)
	writeJSONBare(w, login)
}

// workerLoginChecked loads a login request and verifies it belongs to the
// worker's org. Writes the error response on failure.
func (h *Handler) workerLoginChecked(w http.ResponseWriter, r *http.Request) *providers.LoginRequest {
	login, err := h.LoginService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "login request not found", err)
		return nil
	}
	if login.OrgID != WorkerOrg(r) {
		writeJSONError(w, http.StatusNotFound, "login request not found")
		return nil
	}
	return login
}

// ProgressProviderLogin records worker-side login progress.
func (h *Handler) ProgressProviderLogin(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	if h.workerLoginChecked(w, r) == nil {
		return
	}
	var req workerproto.LoginProgress
	if !decodeJSON(w, r, &req) {
		return
	}
	login, err := h.LoginService.Progress(mux.Vars(r)["id"], req.Status, req.AuthURL, req.Detail, req.PasteKind)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, login)
}

// GetProviderLoginFull returns the request including any pasted code
// (worker only — this is how the code reaches the CLI's stdin).
func (h *Handler) GetProviderLoginFull(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	login := h.workerLoginChecked(w, r)
	if login == nil {
		return
	}
	writeJSONBare(w, login)
}

// SaveProviderLoginToken takes the token a member's cloud runner minted for
// a token-purpose request and seals it as that member's saved subscription
// token. Only the member's own runner may hand it in (their personal or
// lease key), and only for their active token request; the token is never
// echoed or logged.
func (h *Handler) SaveProviderLoginToken(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	login := h.workerLoginChecked(w, r)
	if login == nil {
		return
	}
	if login.Purpose != providers.LoginPurposeToken || !login.Active() || login.RequestedBy == nil ||
		WorkerUser(r) != *login.RequestedBy {
		writeJSONError(w, http.StatusConflict, providers.ErrNotTokenLogin.Error())
		return
	}
	if !h.memberFeatureEnabled(login.OrgID, login.RequestedBy, release.FeatureCloudRunnerToken) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req workerproto.LoginToken
	if !decodeJSON(w, r, &req) {
		return
	}
	if h.ProviderTokenService == nil {
		writeProviderTokenError(w, r, usertokens.ErrUnavailable)
		return
	}
	if _, err := h.ProviderTokenService.Save(*login.RequestedBy, providers.ProviderClaudeCode, req.Token); err != nil {
		writeProviderTokenError(w, r, err)
		return
	}
	done, err := h.LoginService.CompleteTokenLogin(login.ID)
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSONBare(w, done.Sanitized())
}
