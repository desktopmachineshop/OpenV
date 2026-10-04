package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
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
	router.HandleFunc("/api/v1/provider-logins/{id}/full", h.GetProviderLoginFull).Methods("GET")
}

// StartProviderLogin creates (or resumes) a CLI sign-in request for a worker
// to execute on a host. Workspace-targeted sign-ins (shared workers) need a
// workspace admin; user-targeted sign-ins run only on the requester's own
// personal runner, so any workspace member may start one.
func (h *Handler) StartProviderLogin(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Target   string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
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
	login, err := h.LoginService.StartLogin(ActiveOrg(r), req.Provider, req.Target, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(login.Sanitized())
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
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	login := h.userLoginChecked(w, r)
	if login == nil {
		return
	}
	json.NewEncoder(w).Encode(login.Sanitized())
}

// SubmitProviderLoginCode records the user's pasted authorization code.
func (h *Handler) SubmitProviderLoginCode(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if h.userLoginWriteChecked(w, r) == nil {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	login, err := h.LoginService.SubmitCode(mux.Vars(r)["id"], req.Code)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(login.Sanitized())
}

// CancelProviderLogin abandons a login request.
func (h *Handler) CancelProviderLogin(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
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
	json.NewEncoder(w).Encode(login.Sanitized())
}

// ClaimProviderLogin hands the oldest pending login request this worker may
// execute to the worker: workspace-targeted requests for any worker, plus
// the owner's user-targeted requests for personal runners.
func (h *Handler) ClaimProviderLogin(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	login, err := h.LoginService.Claim(WorkerOrg(r), WorkerUser(r))
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
	json.NewEncoder(w).Encode(login)
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
	var req struct {
		Status  string `json:"status"`
		AuthURL string `json:"auth_url"`
		Detail  string `json:"detail"`
		// PasteKind ("code" or "url") is what the worker is waiting for the
		// member to paste back; older workers omit it.
		PasteKind string `json:"paste_kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	login, err := h.LoginService.Progress(mux.Vars(r)["id"], req.Status, req.AuthURL, req.Detail, req.PasteKind)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(login)
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
	json.NewEncoder(w).Encode(login)
}
