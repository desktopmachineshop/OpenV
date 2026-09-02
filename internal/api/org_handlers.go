package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/hosting"
)

func (h *Handler) registerOrgRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs", h.ListOrgs).Methods("GET")
	router.HandleFunc("/api/v1/orgs", h.CreateOrg).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}", h.GetOrg).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}", h.UpdateOrg).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}", h.DeleteOrg).Methods("DELETE")
	router.HandleFunc("/api/v1/orgs/{id}/restore", h.RestoreOrg).Methods("POST")
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

	// Requirement quality house style: readable by any member, set by admins,
	// inherited by every project in the workspace.
	router.HandleFunc("/api/v1/orgs/{id}/quality-rules", h.GetWorkspaceQualityRules).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/quality-rules", h.UpdateWorkspaceQualityRules).Methods("PUT")

	// Agent Connector pairing: browser issues a one-time code; the local
	// connector exchanges it (public route — the code is the credential).
	router.HandleFunc("/api/v1/orgs/{id}/connector-pairing", h.CreateConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/pair", h.ExchangeConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/download", h.DownloadConnector).Methods("GET", "HEAD")

	// Hosted runner: one platform-managed container per workspace (admin).
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.GetHostedRunner).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.CreateHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner/start", h.StartHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner/stop", h.StopHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.DeleteHostedRunner).Methods("DELETE")

	// Worker status: runner fleet + queue depth for the workspace (member).
	router.HandleFunc("/api/v1/orgs/{id}/worker-status", h.GetWorkerStatus).Methods("GET")

	// Usage rollup: run counts, tokens and cost by agent and by day (member).
	router.HandleFunc("/api/v1/orgs/{id}/usage", h.GetOrgUsage).Methods("GET")

	router.HandleFunc("/api/v1/projects/{id}/team-access", h.ListProjectTeamAccess).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/team-access", h.GrantProjectTeamAccess).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/team-access/{teamId}", h.RevokeProjectTeamAccess).Methods("DELETE")
}

// --- Workspaces ---

// ListOrgs returns the caller's workspaces with roles.
func (h *Handler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	list, err := h.orgService.ListForUser(user.ID)
	if r.URL.Query().Get("deleted") == "true" {
		list, err = h.orgService.ListDeletedForUser(user.ID)
	}
	if err != nil {
		respondInternal(w, r, "failed to list workspaces", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"orgs":       list,
		"active_org": ActiveOrg(r),
	})
}

// DeleteOrg soft-deletes a company workspace (admin). It disappears from
// pickers and every request against it is refused; it stays restorable for
// orgs.DeletionGraceDays, after which the daily purge hard-deletes it and all
// its data. Personal workspaces cannot be deleted.
func (h *Handler) DeleteOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	org, err := h.orgService.DeleteOrg(orgID)
	if err != nil {
		switch {
		case errors.Is(err, orgs.ErrPersonalOrgDelete):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, orgs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			respondInternal(w, r, "failed to delete workspace", err)
		}
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted_at":  org.DeletedAt,
		"purge_after": org.DeletedAt.Add(orgs.DeletionGraceDays * 24 * time.Hour),
	})
}

// RestoreOrg brings a soft-deleted workspace back within the grace period.
// Deleted workspaces fail the normal role check by design, so authorization
// uses the any-state role lookup: platform admins and the workspace's own
// admins may restore.
func (h *Handler) RestoreOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !user.IsAdmin {
		role, err := h.orgService.RoleInOrgAny(orgID, user.ID)
		if err != nil {
			respondInternal(w, r, "failed to resolve workspace access", err)
			return
		}
		if role != orgs.RoleAdmin {
			writeJSONError(w, http.StatusForbidden, "only workspace admins can restore a deleted workspace")
			return
		}
	}
	org, err := h.orgService.RestoreOrg(orgID)
	if err != nil {
		switch {
		case errors.Is(err, orgs.ErrNotDeleted):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, orgs.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			respondInternal(w, r, "failed to restore workspace", err)
		}
		return
	}
	json.NewEncoder(w).Encode(org)
}

// CreateOrg creates a company workspace with the caller as admin.
func (h *Handler) CreateOrg(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	org, err := h.orgService.CreateOrg(req.Name, orgs.TypeCompany, user.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.orgSeeder != nil {
		if err := h.orgSeeder(org.ID); err != nil {
			// Non-fatal: the workspace exists; defaults can be re-seeded.
			respondInternal(w, r, "workspace created but seeding defaults failed", err)
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
		respondError(w, r, http.StatusNotFound, "workspace not found", err)
		return
	}
	json.NewEncoder(w).Encode(org)
}

// UpdateOrg renames a workspace and/or sets its monthly spend budget (admin).
// monthly_budget_usd is honored only when present in the body: a JSON number
// sets the budget, JSON null clears it, and an omitted key leaves it
// unchanged (so a plain rename never disturbs the budget).
func (h *Handler) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Name             *string         `json:"name"`
		MonthlyBudgetUSD json.RawMessage `json:"monthly_budget_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	org, err := h.orgService.UpdateOrg(orgID, req.Name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Budget is only touched when the key is present. json.RawMessage is nil
	// for an absent key; "null" clears the budget, a number sets it.
	if len(req.MonthlyBudgetUSD) > 0 {
		var budget *float64
		if err := json.Unmarshal(req.MonthlyBudgetUSD, &budget); err != nil {
			writeJSONError(w, http.StatusBadRequest, "monthly_budget_usd must be a number or null")
			return
		}
		org, err = h.orgService.SetMonthlyBudget(orgID, budget)
		if err != nil {
			if errors.Is(err, orgs.ErrInvalidBudget) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			} else {
				respondInternal(w, r, "failed to update budget", err)
			}
			return
		}
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
		writeJSONError(w, http.StatusBadRequest, "session required")
		return
	}
	if err := h.userService.SetActiveOrg(cookie.Value, orgID); err != nil {
		respondInternal(w, r, "failed to switch workspace", err)
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
		respondInternal(w, r, "failed to list workspace members", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role == "" {
		req.Role = orgs.RoleMember
	}
	user, err := h.userService.FindByEmail(req.Email)
	if err != nil {
		respondInternal(w, r, "failed to look up user", err)
		return
	}
	if user == nil {
		writeJSONError(w, http.StatusNotFound, "no user with that email — they must sign up first")
		return
	}
	if err := h.orgService.AddMember(orgID, user.ID, req.Role); err != nil {
		if errors.Is(err, orgs.ErrInvalidRole) || errors.Is(err, orgs.ErrPersonalOrgMembers) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to add workspace member", err)
		}
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.orgService.SetMemberRole(vars["id"], vars["userId"], req.Role); err != nil {
		if errors.Is(err, orgs.ErrInvalidRole) || errors.Is(err, orgs.ErrNotMember) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to update workspace member", err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to list teams", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	team, err := h.orgTeamService.CreateTeam(orgID, req.Name, req.Description, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

// orgTeamChecked loads a people-team and enforces the caller's org role.
func (h *Handler) orgTeamChecked(w http.ResponseWriter, r *http.Request, minRole string) *orgs.OrgTeam {
	team, err := h.orgTeamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.orgTeamService.UpdateTeam(team.ID, req.Name, req.Description)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to delete team", err)
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to remove team member", err)
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
		respondInternal(w, r, "failed to list worker keys", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	key, plaintext, err := h.workerKeyService.Create(orgID, req.Name, CurrentUserID(r), nil)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to load personal runner key", err)
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to load personal runner key", err)
		return
	}
	if key == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.workerKeyService.Revoke(orgID, key.ID); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to load hosted runner", err)
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
		writeJSONError(w, http.StatusBadRequest, "hosted runners are not enabled on this deployment")
		return
	}
	var req struct {
		ProviderKeys map[string]string `json:"provider_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	existing, err := h.hostedWorkerService.Get(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load hosted runner", err)
		return
	}
	if existing != nil {
		writeJSONError(w, http.StatusConflict, "a hosted runner already exists for this workspace")
		return
	}

	// The container is capped at the org's effective limits (explicit
	// org limits merged over its plan's defaults).
	org, err := h.orgService.Get(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load workspace", err)
		return
	}
	limits := hosting.ResourceLimitsForOrg(org)

	// Mint a workspace worker key for the container (plaintext shown to the
	// container env only).
	key, plaintext, err := h.workerKeyService.Create(orgID, hostedRunnerKeyName, CurrentUserID(r), nil)
	if err != nil {
		respondInternal(w, r, "failed to create worker key", err)
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
		respondInternal(w, r, "failed to create hosted runner", err)
		return
	}

	status, detail := hostedworkers.StatusRunning, ""
	if err := h.provisioner.Provision(orgID, record.ContainerName, plaintext, extraEnv, limits); err != nil {
		status, detail = hostedworkers.StatusError, err.Error()
	}
	record, err = h.hostedWorkerService.SetStatus(record.ID, status, detail)
	if err != nil {
		respondInternal(w, r, "failed to update hosted runner status", err)
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
		respondInternal(w, r, "failed to load hosted runner", err)
		return nil
	}
	if record == nil {
		writeJSONError(w, http.StatusNotFound, "no hosted runner for this workspace")
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
		respondInternal(w, r, "failed to start hosted runner", err)
		return
	}
	updated, err := h.hostedWorkerService.SetStatus(record.ID, hostedworkers.StatusRunning, "")
	if err != nil {
		respondInternal(w, r, "failed to update hosted runner status", err)
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
		respondInternal(w, r, "failed to stop hosted runner", err)
		return
	}
	updated, err := h.hostedWorkerService.SetStatus(record.ID, hostedworkers.StatusStopped, "")
	if err != nil {
		respondInternal(w, r, "failed to update hosted runner status", err)
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
			respondInternal(w, r, "failed to remove hosted runner", err)
			return
		}
	}
	if record.WorkerKeyID != nil {
		_ = h.workerKeyService.Revoke(orgID, *record.WorkerKeyID)
	}
	if err := h.hostedWorkerService.Delete(record.ID); err != nil {
		respondInternal(w, r, "failed to delete hosted runner", err)
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
		respondInternal(w, r, "failed to list worker keys", err)
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
		respondInternal(w, r, "failed to load queue stats", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": workers,
		"queue":   queue,
	})
}

// Usage-window bounds: ?days= is clamped to [1, maxUsageDays]; 0/absent
// falls back to defaultUsageDays.
const (
	defaultUsageDays = 30
	maxUsageDays     = 365
)

// GetOrgUsage rolls up the workspace's agent-run usage (runs, tokens, cost)
// by agent and by day over a trailing window (?days=30 by default).
// Access decision: every workspace member gets the org-wide read — the
// workspace is their shared context and the rollup carries no run content,
// only counts and spend. Admin-only gating was considered and rejected as
// needless friction; revisit if orgs ever want per-member spend privacy.
func (h *Handler) GetOrgUsage(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	days := defaultUsageDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSONError(w, http.StatusBadRequest, "days must be a positive integer")
			return
		}
		days = parsed
		if days > maxUsageDays {
			days = maxUsageDays
		}
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	summary, err := h.runService.Usage(orgID, since)
	if err != nil {
		respondInternal(w, r, "failed to load usage", err)
		return
	}
	summary.Days = days
	// Month-to-date spend for the budget bar — independent of the trailing
	// window, and the same figure budget alerts fire against.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if spend, err := h.runService.MonthlySpend(orgID, monthStart); err == nil {
		summary.MonthToDateCostUSD = spend
	}
	json.NewEncoder(w).Encode(summary)
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
		respondInternal(w, r, "failed to create pairing code", err)
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
		// One-link flow: the connector starts with its existing pairing for
		// this workspace and only spends the code when it has none, so
		// opening never rotates a working key.
		"open_link": "openv-connector://open?org=" + orgID + "&code=" + code + "&api=" + url.QueryEscape(apiURL),
	})
}

// ExchangeConnectorPairing lets the local connector trade a code for the
// member's personal runner key (rotates any previous one).
func (h *Handler) ExchangeConnectorPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSONError(w, http.StatusBadRequest, "pairing code is required")
		return
	}
	key, plaintext, err := h.workerKeyService.ExchangePairing(req.Code, "")
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
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

// DownloadConnector serves a prebuilt Agent Connector when present:
// preferably the single self-contained executable (agentd and openv-mcp
// embedded — see cmd/openv-connector payload_embed.go), falling back to the
// legacy zip bundle for dist directories built before the single-file era.
func (h *Handler) DownloadConnector(w http.ResponseWriter, r *http.Request) {
	osName := r.URL.Query().Get("os")
	if osName == "" {
		osName = "windows"
	}
	if osName != "windows" && osName != "linux" && osName != "darwin" {
		writeJSONError(w, http.StatusBadRequest, "unknown os")
		return
	}
	if h.connectorDistDir == "" {
		writeJSONError(w, http.StatusNotFound, "connector downloads are not configured on this deployment")
		return
	}

	single := "openv-connector-" + osName
	serveAs := "openv-connector"
	if osName == "windows" {
		single += ".exe"
		serveAs += ".exe"
	}
	if path := filepath.Join(h.connectorDistDir, single); fileExists(path) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+serveAs)
		http.ServeFile(w, r, path)
		return
	}

	zipName := "openv-connector-" + osName + ".zip"
	if path := filepath.Join(h.connectorDistDir, zipName); fileExists(path) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename="+zipName)
		http.ServeFile(w, r, path)
		return
	}
	writeJSONError(w, http.StatusNotFound, "connector download not available for "+osName+" — build it with `make connector-dist`")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- Project team grants ---

func (h *Handler) ListProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.memberService.ListTeamGrants(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list team grants", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The team must live in the project's org.
	team, err := h.orgTeamService.GetTeam(req.OrgTeamID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "team not found")
		return
	}
	project, err := h.projectService.GetProject(projectID)
	if err != nil || project == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return
	}
	if project.OrgID != "" && team.OrgID != project.OrgID {
		writeJSONError(w, http.StatusBadRequest, "team belongs to a different workspace")
		return
	}
	if err := h.memberService.GrantTeam(projectID, req.OrgTeamID, req.Role); err != nil {
		if errors.Is(err, members.ErrInvalidRole) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to grant team access", err)
		}
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
		respondInternal(w, r, "failed to revoke team grant", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
