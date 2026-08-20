package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

func (h *Handler) registerOrgRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs", h.ListOrgs).Methods("GET")
	router.HandleFunc("/api/v1/orgs", h.CreateOrg).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}", h.GetOrg).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}", h.UpdateOrg).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/activate", h.ActivateOrg).Methods("POST")

	router.HandleFunc("/api/v1/orgs/{id}/members", h.ListOrgMembers).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members", h.AddOrgMember).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/members/{userId}", h.UpdateOrgMember).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/members/{userId}", h.RemoveOrgMember).Methods("DELETE")

	router.HandleFunc("/api/v1/orgs/{id}/teams", h.ListOrgTeams).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/teams", h.CreateOrgTeam).Methods("POST")
	router.HandleFunc("/api/v1/org-teams/{id}", h.UpdateOrgTeam).Methods("PUT")
	router.HandleFunc("/api/v1/org-teams/{id}", h.DeleteOrgTeam).Methods("DELETE")
	router.HandleFunc("/api/v1/org-teams/{id}/members/{userId}", h.AddOrgTeamMember).Methods("POST")
	router.HandleFunc("/api/v1/org-teams/{id}/members/{userId}", h.RemoveOrgTeamMember).Methods("DELETE")

	router.HandleFunc("/api/v1/orgs/{id}/worker-keys", h.ListWorkerKeys).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys", h.CreateWorkerKey).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys/{keyId}", h.RevokeWorkerKey).Methods("DELETE")

	// Personal runner keys: every member manages their own.
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.GetMyRunnerKey).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.CreateMyRunnerKey).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.RevokeMyRunnerKey).Methods("DELETE")

	// Agent Connector pairing: browser issues a one-time code; the local
	// connector exchanges it (public route — the code is the credential).
	router.HandleFunc("/api/v1/orgs/{id}/connector-pairing", h.CreateConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/pair", h.ExchangeConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/download", h.DownloadConnector).Methods("GET")

	// Hosted runner: one platform-managed container per workspace (admin).
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.GetHostedRunner).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.CreateHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner/start", h.StartHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner/stop", h.StopHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.DeleteHostedRunner).Methods("DELETE")

	// Worker status: runner fleet + queue depth for the workspace (member).
	router.HandleFunc("/api/v1/orgs/{id}/worker-status", h.GetWorkerStatus).Methods("GET")

	router.HandleFunc("/api/v1/projects/{id}/team-access", h.ListProjectTeamAccess).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/team-access", h.GrantProjectTeamAccess).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/team-access/{teamId}", h.RevokeProjectTeamAccess).Methods("DELETE")
}

// --- Workspaces ---

// ListOrgs returns the caller's workspaces with roles.
func (h *Handler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	list, err := h.orgService.ListForUser(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"orgs":       list,
		"active_org": ActiveOrg(r),
	})
}

// CreateOrg creates a company workspace with the caller as admin.
func (h *Handler) CreateOrg(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	org, err := h.orgService.CreateOrg(req.Name, orgs.TypeCompany, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.orgSeeder != nil {
		if err := h.orgSeeder(org.ID); err != nil {
			// Non-fatal: the workspace exists; defaults can be re-seeded.
			http.Error(w, "workspace created but seeding defaults failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(org)
}

// GetOrg returns a workspace the caller belongs to.
func (h *Handler) GetOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	org, err := h.orgService.Get(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(org)
}

// UpdateOrg renames a workspace (admin).
func (h *Handler) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	org, err := h.orgService.UpdateOrg(orgID, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(org)
}

// ActivateOrg persists the session's default workspace.
func (h *Handler) ActivateOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	if err := h.userService.SetActiveOrg(cookie.Value, orgID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Workspace members ---

func (h *Handler) ListOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	list, err := h.orgService.ListMembers(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// AddOrgMember invites an existing user by email (admin).
func (h *Handler) AddOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = orgs.RoleMember
	}
	user, err := h.userService.FindByEmail(req.Email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "no user with that email — they must sign up first", http.StatusNotFound)
		return
	}
	if err := h.orgService.AddMember(orgID, user.ID, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) UpdateOrgMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireOrgRole(w, r, vars["id"], orgs.RoleAdmin) {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.orgService.SetMemberRole(vars["id"], vars["userId"], req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	user := CurrentUser(r)
	if user == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	// Members may remove themselves (leave); removing others requires admin.
	minRole := orgs.RoleAdmin
	if user.ID == vars["userId"] {
		minRole = orgs.RoleMember
	}
	if !h.requireOrgRole(w, r, vars["id"], minRole) {
		return
	}
	if err := h.orgService.RemoveMember(vars["id"], vars["userId"]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- People-teams ---

func (h *Handler) ListOrgTeams(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	list, err := h.orgTeamService.ListTeams(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateOrgTeam(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	team, err := h.orgTeamService.CreateTeam(orgID, req.Name, req.Description, CurrentUserID(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

// orgTeamChecked loads a people-team and enforces the caller's org role.
func (h *Handler) orgTeamChecked(w http.ResponseWriter, r *http.Request, minRole string) *orgs.OrgTeam {
	team, err := h.orgTeamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil
	}
	if !h.requireOrgRole(w, r, team.OrgID, minRole) {
		return nil
	}
	return team
}

func (h *Handler) UpdateOrgTeam(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.orgTeamService.UpdateTeam(team.ID, req.Name, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteOrgTeam(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	if err := h.orgTeamService.DeleteTeam(team.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddOrgTeamMember(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	if err := h.orgTeamService.AddTeamMember(team.ID, mux.Vars(r)["userId"]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) RemoveOrgTeamMember(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	if err := h.orgTeamService.RemoveTeamMember(team.ID, mux.Vars(r)["userId"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Worker keys ---

func (h *Handler) ListWorkerKeys(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	list, err := h.workerKeyService.List(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// CreateWorkerKey mints a key; the plaintext is returned once.
func (h *Handler) CreateWorkerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	key, plaintext, err := h.workerKeyService.Create(orgID, req.Name, CurrentUserID(r), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key_record": key,
		"key":        plaintext,
	})
}

func (h *Handler) RevokeWorkerKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireOrgRole(w, r, vars["id"], orgs.RoleAdmin) {
		return
	}
	if err := h.workerKeyService.Revoke(vars["id"], vars["keyId"]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Personal runner keys ---

// GetMyRunnerKey returns the caller's personal runner key metadata (no
// plaintext) plus online state.
func (h *Handler) GetMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	key, err := h.workerKeyService.PersonalKey(orgID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if key == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"key_record": nil, "online": false})
		return
	}
	online := key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < 30*time.Second
	json.NewEncoder(w).Encode(map[string]interface{}{"key_record": key, "online": online})
}

// CreateMyRunnerKey mints (or rotates) the caller's personal runner key.
func (h *Handler) CreateMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	userID := user.ID
	name := "personal-runner"
	if user.Name != "" {
		name = user.Name + "'s runner"
	}
	key, plaintext, err := h.workerKeyService.Create(orgID, name, &userID, &userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"key_record": key,
		"key":        plaintext,
	})
}

// RevokeMyRunnerKey revokes the caller's personal runner key.
func (h *Handler) RevokeMyRunnerKey(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	key, err := h.workerKeyService.PersonalKey(orgID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if key == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.workerKeyService.Revoke(orgID, key.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Hosted runner ---

// hostedRunnerKeyName is the worker key name minted for hosted runners.
const hostedRunnerKeyName = "hosted-runner"

// workerOnlineWindow is how recently a key must have polled to count as
// online (worker_keys.last_used_at is touched on every poll).
const workerOnlineWindow = 30 * time.Second

// hostedProviderEnv maps provider_keys fields to the env var each vendor CLI
// reads inside the runner container.
var hostedProviderEnv = map[string]string{
	"anthropic": "ANTHROPIC_API_KEY",
	"openai":    "OPENAI_API_KEY",
	"gemini":    "GEMINI_API_KEY",
}

// hostedContainerName derives the org's runner container name.
func hostedContainerName(orgID string) string {
	short := orgID
	if len(short) > 8 {
		short = short[:8]
	}
	return "openv-runner-" + short
}

func (h *Handler) hostedRunnersEnabled() bool {
	return h.provisioner != nil && h.provisioner.Enabled()
}

// keyOnline reports whether a worker key has polled recently.
func (h *Handler) keyOnline(orgID string, keyID *string) bool {
	if keyID == nil {
		return false
	}
	key, err := h.workerKeyService.Get(orgID, *keyID)
	if err != nil || key == nil || key.Revoked {
		return false
	}
	return key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < workerOnlineWindow
}

// GetHostedRunner returns the workspace's hosted runner record plus live
// container/online state (admin).
func (h *Handler) GetHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	record, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	containerState := ""
	online := false
	if record != nil {
		if h.hostedRunnersEnabled() {
			if state, err := h.provisioner.ContainerState(record.ContainerName); err == nil {
				containerState = state
			}
		}
		online = h.keyOnline(orgID, record.WorkerKeyID)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"record":          record,
		"enabled":         h.hostedRunnersEnabled(),
		"container_state": containerState,
		"online":          online,
	})
}

// CreateHostedRunner provisions the workspace's hosted runner container
// (admin). Provider API keys travel to the container environment only — they
// are never persisted by the platform.
func (h *Handler) CreateHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if !h.hostedRunnersEnabled() {
		http.Error(w, "hosted runners are not enabled on this deployment", http.StatusBadRequest)
		return
	}
	var req struct {
		ProviderKeys map[string]string `json:"provider_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	existing, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing != nil {
		http.Error(w, "a hosted runner already exists for this workspace", http.StatusConflict)
		return
	}

	// Mint a workspace worker key for the container (plaintext shown to the
	// container env only).
	key, plaintext, err := h.workerKeyService.Create(orgID, hostedRunnerKeyName, CurrentUserID(r), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	extraEnv := map[string]string{}
	for provider, envName := range hostedProviderEnv {
		if v := strings.TrimSpace(req.ProviderKeys[provider]); v != "" {
			extraEnv[envName] = v
		}
	}

	keyID := key.ID
	record, err := h.hostedWorkerService.Create(orgID, hostedContainerName(orgID), &keyID, CurrentUserID(r))
	if err != nil {
		_ = h.workerKeyService.Revoke(orgID, key.ID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	status, detail := hostedworkers.StatusRunning, ""
	if err := h.provisioner.Provision(orgID, record.ContainerName, plaintext, extraEnv); err != nil {
		status, detail = hostedworkers.StatusError, err.Error()
	}
	record, err = h.hostedWorkerService.SetStatus(record.ID, status, detail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(record)
}

// hostedRunnerChecked loads the org's hosted runner record after enforcing
// admin role; writes the error response when absent.
func (h *Handler) hostedRunnerChecked(w http.ResponseWriter, r *http.Request, orgID string) *hostedworkers.HostedWorker {
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return nil
	}
	record, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if record == nil {
		http.Error(w, "no hosted runner for this workspace", http.StatusNotFound)
		return nil
	}
	return record
}

// StartHostedRunner starts a stopped hosted runner container (admin).
func (h *Handler) StartHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	record := h.hostedRunnerChecked(w, r, orgID)
	if record == nil {
		return
	}
	if err := h.provisioner.Start(record.ContainerName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := h.hostedWorkerService.SetStatus(record.ID, hostedworkers.StatusRunning, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

// StopHostedRunner stops the hosted runner container (admin).
func (h *Handler) StopHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	record := h.hostedRunnerChecked(w, r, orgID)
	if record == nil {
		return
	}
	if err := h.provisioner.Stop(record.ContainerName); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := h.hostedWorkerService.SetStatus(record.ID, hostedworkers.StatusStopped, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

// DeleteHostedRunner removes the hosted runner container (and optionally its
// data volume with ?purge=true), revokes its worker key, and deletes the
// record (admin).
func (h *Handler) DeleteHostedRunner(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	record := h.hostedRunnerChecked(w, r, orgID)
	if record == nil {
		return
	}
	purge := r.URL.Query().Get("purge") == "true"
	if h.hostedRunnersEnabled() {
		if err := h.provisioner.Remove(record.ContainerName, purge, orgID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if record.WorkerKeyID != nil {
		_ = h.workerKeyService.Revoke(orgID, *record.WorkerKeyID)
	}
	if err := h.hostedWorkerService.Delete(record.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Worker status ---

// GetWorkerStatus returns the workspace's runner fleet and queue depth
// (member).
func (h *Handler) GetWorkerStatus(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	keys, err := h.workerKeyService.List(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	hostedKeyID := ""
	if h.hostedWorkerService != nil {
		if record, err := h.hostedWorkerService.Get(orgID); err == nil && record != nil && record.WorkerKeyID != nil {
			hostedKeyID = *record.WorkerKeyID
		}
	}
	workers := []map[string]interface{}{}
	for _, key := range keys {
		online := !key.Revoked && key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < workerOnlineWindow
		workers = append(workers, map[string]interface{}{
			"id":           key.ID,
			"name":         key.Name,
			"personal":     key.UserID != nil,
			"hosted":       key.ID == hostedKeyID || key.Name == hostedRunnerKeyName,
			"user_name":    key.UserName,
			"online":       online,
			"revoked":      key.Revoked,
			"last_used_at": key.LastUsedAt,
		})
	}
	queue, err := h.runService.QueueStats(orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": workers,
		"queue":   queue,
	})
}

// --- Agent Connector pairing ---

// CreateConnectorPairing issues a one-time pairing code plus the deep link
// the browser uses to hand it to the local connector.
func (h *Handler) CreateConnectorPairing(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	code, expires, err := h.workerKeyService.CreatePairing(orgID, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	apiURL := h.publicAPIURL
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":       code,
		"expires_at": expires,
		"api_url":    apiURL,
		"deep_link":  "openv-connector://pair?code=" + code + "&api=" + url.QueryEscape(apiURL),
		"start_link": "openv-connector://start",
	})
}

// ExchangeConnectorPairing lets the local connector trade a code for the
// member's personal runner key (rotates any previous one).
func (h *Handler) ExchangeConnectorPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		http.Error(w, "pairing code is required", http.StatusBadRequest)
		return
	}
	key, plaintext, err := h.workerKeyService.ExchangePairing(req.Code, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	orgName := ""
	if org, err := h.orgService.Get(key.OrgID); err == nil {
		orgName = org.Name
	}
	userName := ""
	if key.UserID != nil {
		if u, err := h.userService.GetByID(*key.UserID); err == nil && u != nil {
			userName = u.Name
		}
	}
	apiURL := h.publicAPIURL
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"worker_key": plaintext,
		"api_url":    apiURL,
		"org_id":     key.OrgID,
		"org_name":   orgName,
		"user_name":  userName,
	})
}

// DownloadConnector serves a prebuilt Agent Connector bundle when present.
func (h *Handler) DownloadConnector(w http.ResponseWriter, r *http.Request) {
	osName := r.URL.Query().Get("os")
	if osName == "" {
		osName = "windows"
	}
	if osName != "windows" && osName != "linux" && osName != "darwin" {
		http.Error(w, "unknown os", http.StatusBadRequest)
		return
	}
	if h.connectorDistDir == "" {
		http.Error(w, "connector downloads are not configured on this deployment", http.StatusNotFound)
		return
	}
	filename := "openv-connector-" + osName + ".zip"
	path := filepath.Join(h.connectorDistDir, filename)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "connector bundle not available for "+osName+" — build it with `make connector-dist`", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	http.ServeFile(w, r, path)
}

// --- Project team grants ---

func (h *Handler) ListProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.memberService.ListTeamGrants(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// GrantProjectTeamAccess grants (or updates) a team's role on a project.
func (h *Handler) GrantProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	var req struct {
		OrgTeamID string `json:"org_team_id"`
		Role      string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// The team must live in the project's org.
	team, err := h.orgTeamService.GetTeam(req.OrgTeamID)
	if err != nil {
		http.Error(w, "team not found", http.StatusNotFound)
		return
	}
	project, err := h.projectService.GetProject(projectID)
	if err != nil || project == nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if project.OrgID != "" && team.OrgID != project.OrgID {
		http.Error(w, "team belongs to a different workspace", http.StatusBadRequest)
		return
	}
	if err := h.memberService.GrantTeam(projectID, req.OrgTeamID, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) RevokeProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireProjectRole(w, r, vars["id"], members.RoleOwner) {
		return
	}
	if err := h.memberService.RevokeTeam(vars["id"], vars["teamId"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
