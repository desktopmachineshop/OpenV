package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/scheduler"
)

func (h *Handler) registerAgentRoutes(router *mux.Router) {
	// Agent definitions (file-backed).
	router.HandleFunc("/api/v1/agents", h.ListAgents).Methods("GET")
	router.HandleFunc("/api/v1/agents", h.CreateAgent).Methods("POST")
	router.HandleFunc("/api/v1/agents/sync", h.SyncAgents).Methods("POST")
	router.HandleFunc("/api/v1/agents/{slug}", h.GetAgent).Methods("GET")
	router.HandleFunc("/api/v1/agents/{slug}", h.UpdateAgent).Methods("PUT")
	router.HandleFunc("/api/v1/agents/{slug}", h.DeleteAgent).Methods("DELETE")
	router.HandleFunc("/api/v1/agents/{slug}/raw", h.GetAgentRaw).Methods("GET")
	router.HandleFunc("/api/v1/agents/{slug}/raw", h.SaveAgentRaw).Methods("PUT")
	router.HandleFunc("/api/v1/agents/{slug}/runs", h.LaunchAgentRun).Methods("POST")

	// Runs.
	router.HandleFunc("/api/v1/agent-runs", h.ListAgentRuns).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/claim", h.ClaimAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/delegate", h.DelegateRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/delegate/{id}", h.DelegateStatus).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}", h.GetAgentRun).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/tree", h.GetAgentRunTree).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/logs", h.GetAgentRunLogs).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/logs", h.AppendAgentRunLogs).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/stream", h.StreamAgentRun).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/cancel", h.CancelAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/start", h.StartAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/finish", h.FinishAgentRun).Methods("POST")

	// Automations.
	router.HandleFunc("/api/v1/automations", h.ListAutomations).Methods("GET")
	router.HandleFunc("/api/v1/automations", h.CreateAutomation).Methods("POST")
	router.HandleFunc("/api/v1/automations/{id}", h.GetAutomation).Methods("GET")
	router.HandleFunc("/api/v1/automations/{id}", h.UpdateAutomation).Methods("PUT")
	router.HandleFunc("/api/v1/automations/{id}", h.DeleteAutomation).Methods("DELETE")
	router.HandleFunc("/api/v1/automations/{id}/run-now", h.RunAutomationNow).Methods("POST")

	// Proposals.
	router.HandleFunc("/api/v1/proposals", h.ListProposals).Methods("GET")
	router.HandleFunc("/api/v1/proposals/{id}/approve", h.ApproveProposal).Methods("POST")
	router.HandleFunc("/api/v1/proposals/{id}/reject", h.RejectProposal).Methods("POST")

	// Repo connections.
	router.HandleFunc("/api/v1/projects/{id}/repo-connections", h.ListRepoConnections).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/repo-connections", h.CreateRepoConnection).Methods("POST")
	router.HandleFunc("/api/v1/repo-connections/{id}", h.UpdateRepoConnection).Methods("PUT")
	router.HandleFunc("/api/v1/repo-connections/{id}", h.DeleteRepoConnection).Methods("DELETE")
	router.HandleFunc("/api/v1/repo-connections/{id}/my-path", h.SetMyRepoPath).Methods("PUT")

	// Provider settings.
	router.HandleFunc("/api/v1/provider-settings", h.ListProviderSettings).Methods("GET")
	router.HandleFunc("/api/v1/provider-settings", h.UpsertProviderSetting).Methods("PUT")
	router.HandleFunc("/api/v1/provider-settings/detect", h.RecordProviderDetection).Methods("POST")

	// Provider CLI login broker.
	router.HandleFunc("/api/v1/provider-logins", h.StartProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/claim", h.ClaimProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}", h.GetProviderLogin).Methods("GET")
	router.HandleFunc("/api/v1/provider-logins/{id}/code", h.SubmitProviderLoginCode).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/cancel", h.CancelProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/progress", h.ProgressProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/full", h.GetProviderLoginFull).Methods("GET")

	// Crews (canonical routes).
	router.HandleFunc("/api/v1/crews", h.ListTeams).Methods("GET")
	router.HandleFunc("/api/v1/crews", h.CreateTeam).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}", h.GetTeam).Methods("GET")
	router.HandleFunc("/api/v1/crews/{id}", h.UpdateTeam).Methods("PUT")
	router.HandleFunc("/api/v1/crews/{id}", h.DeleteTeam).Methods("DELETE")
	router.HandleFunc("/api/v1/crews/{id}/clone", h.CloneTeam).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}/nodes", h.AddTeamNode).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}/runs", h.LaunchTeamRun).Methods("POST")
	router.HandleFunc("/api/v1/crew-nodes/{id}", h.UpdateTeamNode).Methods("PUT")
	router.HandleFunc("/api/v1/crew-nodes/{id}", h.RemoveTeamNode).Methods("DELETE")
	router.HandleFunc("/api/v1/crews/{id}/edges", h.AddTeamEdge).Methods("POST")
	router.HandleFunc("/api/v1/crew-edges/{id}", h.UpdateTeamEdge).Methods("PUT")
	router.HandleFunc("/api/v1/crew-edges/{id}", h.RemoveTeamEdge).Methods("DELETE")

	// Teams (deprecated aliases for the crews routes above).
	router.HandleFunc("/api/v1/teams", h.ListTeams).Methods("GET")                    // deprecated: use /api/v1/crews
	router.HandleFunc("/api/v1/teams", h.CreateTeam).Methods("POST")                  // deprecated: use /api/v1/crews
	router.HandleFunc("/api/v1/teams/{id}", h.GetTeam).Methods("GET")                 // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}", h.UpdateTeam).Methods("PUT")              // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}", h.DeleteTeam).Methods("DELETE")           // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}/clone", h.CloneTeam).Methods("POST")        // deprecated: use /api/v1/crews/{id}/clone
	router.HandleFunc("/api/v1/teams/{id}/nodes", h.AddTeamNode).Methods("POST")      // deprecated: use /api/v1/crews/{id}/nodes
	router.HandleFunc("/api/v1/teams/{id}/runs", h.LaunchTeamRun).Methods("POST")     // deprecated: use /api/v1/crews/{id}/runs
	router.HandleFunc("/api/v1/team-nodes/{id}", h.UpdateTeamNode).Methods("PUT")     // deprecated: use /api/v1/crew-nodes/{id}
	router.HandleFunc("/api/v1/team-nodes/{id}", h.RemoveTeamNode).Methods("DELETE")  // deprecated: use /api/v1/crew-nodes/{id}
	router.HandleFunc("/api/v1/teams/{id}/edges", h.AddTeamEdge).Methods("POST")      // deprecated: use /api/v1/crews/{id}/edges
	router.HandleFunc("/api/v1/team-edges/{id}", h.UpdateTeamEdge).Methods("PUT")     // deprecated: use /api/v1/crew-edges/{id}
	router.HandleFunc("/api/v1/team-edges/{id}", h.RemoveTeamEdge).Methods("DELETE")  // deprecated: use /api/v1/crew-edges/{id}

	// Domain event audit.
	router.HandleFunc("/api/v1/events", h.ListDomainEvents).Methods("GET")
}

func requireUser(w http.ResponseWriter, r *http.Request) bool {
	if CurrentUser(r) == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return false
	}
	return true
}

func requireWorker(w http.ResponseWriter, r *http.Request) bool {
	if !IsWorker(r) {
		http.Error(w, "worker credentials required", http.StatusForbidden)
		return false
	}
	return true
}

// --- Agent definitions ---

func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.agentService.List(ActiveOrg(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var def agents.Definition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if existing, _ := h.agentService.GetBySlug(ActiveOrg(r), def.Slug); existing != nil {
		http.Error(w, "an agent with this slug already exists", http.StatusConflict)
		return
	}
	agent, err := h.agentService.SaveDefinition(ActiveOrg(r), &def)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	agent, err := h.agentService.GetBySlug(ActiveOrg(r), mux.Vars(r)["slug"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if agent == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	slug := mux.Vars(r)["slug"]
	var def agents.Definition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if def.Slug == "" {
		def.Slug = slug
	}
	if def.Slug != slug {
		http.Error(w, "slug in body does not match URL", http.StatusBadRequest)
		return
	}
	agent, err := h.agentService.SaveDefinition(ActiveOrg(r), &def)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	if err := h.agentService.Delete(ActiveOrg(r), mux.Vars(r)["slug"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetAgentRaw(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	content, err := h.agentService.RawFile(ActiveOrg(r), mux.Vars(r)["slug"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"content": content})
}

func (h *Handler) SaveAgentRaw(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	agent, err := h.agentService.SaveRawFile(ActiveOrg(r), mux.Vars(r)["slug"], req.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) SyncAgents(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	if err := h.agentService.SyncFromDisk(ActiveOrg(r)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	list, err := h.agentService.List(ActiveOrg(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// --- Runs ---

// LaunchAgentRun starts a manual run for an agent (by slug).
func (h *Handler) LaunchAgentRun(w http.ResponseWriter, r *http.Request) {
	agent, err := h.agentService.GetBySlug(ActiveOrg(r), mux.Vars(r)["slug"])
	if err != nil || agent == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	var req struct {
		ProjectID  string `json:"project_id"`
		Prompt     string `json:"prompt"`
		WorkItemID string `json:"work_item_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ProjectID != "" && !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}
	// The run belongs to the project's org when project-scoped, else to the
	// caller's active workspace.
	orgID := ActiveOrg(r)
	if req.ProjectID != "" {
		if project, err := h.projectService.GetProject(req.ProjectID); err == nil && project != nil && project.OrgID != "" {
			orgID = project.OrgID
		}
	}
	launch := agentruns.LaunchRequest{
		OrgID:      orgID,
		AgentID:    agent.ID,
		Prompt:     req.Prompt,
		LaunchedBy: CurrentUserID(r),
	}
	if req.ProjectID != "" {
		launch.ProjectID = &req.ProjectID
	}
	if req.WorkItemID != "" {
		launch.WorkItemID = &req.WorkItemID
	}
	run, _, err := h.runService.Launch(launch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

func (h *Handler) ListAgentRuns(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	q := r.URL.Query()
	projectID := q.Get("project_id")
	if projectID != "" && !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	runs, err := h.runService.List(agentruns.ListFilter{
		AgentID:   q.Get("agent_id"),
		ProjectID: projectID,
		Status:    q.Get("status"),
		ParentID:  q.Get("parent_id"),
		Limit:     limit,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Scope the listing to the active workspace; without a project filter,
	// non-admin members only see the runs they launched themselves.
	activeOrg := ActiveOrg(r)
	inOrg := runs[:0]
	for _, run := range runs {
		if activeOrg != "" && run.OrgID != activeOrg {
			continue
		}
		inOrg = append(inOrg, run)
	}
	runs = inOrg
	if projectID == "" && !h.isOrgAdmin(r, activeOrg) {
		user := CurrentUser(r)
		own := runs[:0]
		for _, run := range runs {
			if run.LaunchedBy != nil && *run.LaunchedBy == user.ID {
				own = append(own, run)
			}
		}
		runs = own
	}
	json.NewEncoder(w).Encode(runs)
}

func (h *Handler) GetAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	json.NewEncoder(w).Encode(run)
}

func (h *Handler) GetAgentRunTree(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	tree, err := h.runService.Tree(run.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(tree)
}

func (h *Handler) GetAgentRunLogs(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	afterSeq, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
	logs, err := h.runService.Logs(run.ID, afterSeq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if logs == nil {
		logs = []agentruns.LogEntry{}
	}
	json.NewEncoder(w).Encode(logs)
}

// StreamAgentRun serves the SSE live tail for a run.
func (h *Handler) StreamAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	runID := mux.Vars(r)["id"]
	run, err := h.runService.Get(runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	afterSeq, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
	h.sseHub.ServeStream(w, r, runID, func(emit func(event string, data interface{})) error {
		logs, err := h.runService.Logs(runID, afterSeq)
		if err != nil {
			return err
		}
		for _, entry := range logs {
			emit("log", entry)
		}
		emit("status", map[string]interface{}{"run_id": run.ID, "status": run.Status})
		return nil
	})
}

func (h *Handler) CancelAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleEditor) {
		return
	}
	cancelled, err := h.runService.RequestCancel(run.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(cancelled)
}

// --- Worker endpoints ---

func (h *Handler) ClaimAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	var req struct {
		WorkerID    string   `json:"worker_id"`
		Providers   []string `json:"providers"`
		MinPriority int      `json:"min_priority"`
		Hosted      bool     `json:"hosted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Hosted runners never execute repo-access agents.
	run, err := h.runService.Claim(req.WorkerID, WorkerOrg(r), WorkerUser(r), req.Providers, req.MinPriority, req.Hosted)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if run == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// The worker needs the agent definition and the run token context; the
	// token itself was minted at enqueue and is returned only here, derived
	// fresh so the worker can hand it to the MCP server.
	agent, err := h.agentService.Get(run.AgentID)
	if err != nil || agent == nil {
		http.Error(w, "agent not found for claimed run", http.StatusInternalServerError)
		return
	}
	token, err := h.runService.ReissueToken(run.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run":       run,
		"agent":     agent,
		"run_token": token,
		"auth":      h.resolveRunAuth(run, agent),
	})
}

// resolveRunAuth picks the provider credential mode for a claimed run. A
// project set to api-key overrides the member's local CLI sign-in: the worker
// injects the key named by api_key_env (org provider setting, or the
// provider's native variable) from its host environment. Everything else —
// including non-project runs — uses the runner's local sign-in.
func (h *Handler) resolveRunAuth(run *agentruns.Run, agent *agents.Agent) map[string]string {
	auth := map[string]string{"mode": projects.AgentAuthUserAccount}
	if run.ProjectID == nil || *run.ProjectID == "" {
		return auth
	}
	project, err := h.projectService.GetProject(*run.ProjectID)
	if err != nil || project == nil || project.AgentAuth != projects.AgentAuthAPIKey {
		return auth
	}
	auth["mode"] = projects.AgentAuthAPIKey
	keyEnv := providers.DefaultAPIKeyEnv(agent.Provider)
	if h.providerService != nil {
		if settings, err := h.providerService.List(run.OrgID); err == nil {
			for _, s := range settings {
				if s.Provider == agent.Provider && s.APIKeyEnv != "" {
					keyEnv = s.APIKeyEnv
					break
				}
			}
		}
	}
	auth["api_key_env"] = keyEnv
	return auth
}

func (h *Handler) StartAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	if err := h.runService.MarkRunning(mux.Vars(r)["id"]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AppendAgentRunLogs(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	var entries []agentruns.LogEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	run, err := h.runService.AppendLogs(mux.Vars(r)["id"], entries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cancel_requested": run.CancelRequested,
		"status":           run.Status,
	})
}

func (h *Handler) FinishAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	var req agentruns.FinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	run, err := h.runService.Finish(mux.Vars(r)["id"], req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(run)
}

// DelegateRun lets a running team agent invoke one of its delegates-to
// children. Run-token auth only; targets are restricted server-side to the
// caller's delegation children.
func (h *Handler) DelegateRun(w http.ResponseWriter, r *http.Request) {
	run := CurrentRun(r)
	if run == nil {
		http.Error(w, "delegation requires an agent run token", http.StatusForbidden)
		return
	}
	if run.TeamNodeID == nil {
		http.Error(w, "this run is not part of a team; delegation is unavailable", http.StatusBadRequest)
		return
	}
	var req struct {
		RoleLabel string `json:"role_label"`
		Prompt    string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	children, err := h.teamService.ResolveDelegates(*run.TeamNodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var target *teamNodeRef
	for _, child := range children {
		if strings.EqualFold(child.Label, req.RoleLabel) {
			target = &teamNodeRef{ID: child.ID, AgentID: child.AgentID}
			break
		}
	}
	if target == nil {
		labels := make([]string, 0, len(children))
		for _, child := range children {
			labels = append(labels, child.Label)
		}
		http.Error(w, fmt.Sprintf("no delegate named %q; available delegates: %s", req.RoleLabel, strings.Join(labels, ", ")), http.StatusBadRequest)
		return
	}

	parentID := run.ID
	child, _, err := h.runService.Launch(agentruns.LaunchRequest{
		OrgID:       run.OrgID,
		AgentID:     target.AgentID,
		ProjectID:   run.ProjectID,
		TeamID:      run.TeamID,
		TeamNodeID:  &target.ID,
		ParentRunID: &parentID,
		WorkItemID:  run.WorkItemID,
		Priority:    agentruns.PriorityChild,
		Prompt:      req.Prompt,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"run_id": child.ID, "status": child.Status})
}

type teamNodeRef struct {
	ID      string
	AgentID string
}

// DelegateStatus lets a parent run poll its child run's status/result.
func (h *Handler) DelegateStatus(w http.ResponseWriter, r *http.Request) {
	run := CurrentRun(r)
	if run == nil {
		http.Error(w, "delegation requires an agent run token", http.StatusForbidden)
		return
	}
	child, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if child.ParentRunID == nil || *child.ParentRunID != run.ID {
		http.Error(w, "not your delegated run", http.StatusForbidden)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run_id":     child.ID,
		"status":     child.Status,
		"final_text": child.FinalText,
		"error":      child.Error,
	})
}

// --- Automations ---

func (h *Handler) ListAutomations(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.automationService.List(ActiveOrg(r), r.URL.Query().Get("project_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	var req automations.CreateAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.OrgID = ActiveOrg(r)
	if !h.requireAutomationWrite(w, r, req.ProjectID, req.OrgID) {
		return
	}
	automation, err := h.automationService.Create(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(automation)
}

func (h *Handler) GetAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.automationService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireOrgRole(w, r, automation.OrgID, orgs.RoleMember) {
		return
	}
	json.NewEncoder(w).Encode(automation)
}

func (h *Handler) UpdateAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.automationService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID) {
		return
	}
	var req automations.UpdateAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.automationService.Update(automation.ID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteAutomation(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.automationService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID) {
		return
	}
	if err := h.automationService.Delete(automation.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunAutomationNow launches an automation's run immediately.
func (h *Handler) RunAutomationNow(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	automation, err := h.automationService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID) {
		return
	}
	agentID, teamID, teamNodeID, err := scheduler.ResolveTarget(automation, h.teamService)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prompt := automations.RenderPrompt(automation.PromptTemplate, map[string]string{
		"automation.name": automation.Name,
	})
	if prompt == "" {
		prompt = "Manual run of automation: " + automation.Name
	}
	automationID := automation.ID
	run, _, err := h.runService.Launch(agentruns.LaunchRequest{
		OrgID:        automation.OrgID,
		AgentID:      agentID,
		ProjectID:    automation.ProjectID,
		AutomationID: &automationID,
		TeamID:       teamID,
		TeamNodeID:   teamNodeID,
		Prompt:       prompt,
		LaunchedBy:   CurrentUserID(r),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

// --- Proposals ---

func (h *Handler) ListProposals(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	q := r.URL.Query()
	projectID := q.Get("project_id")
	activeOrg := ActiveOrg(r)
	if projectID != "" {
		if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
			return
		}
	} else if !h.isOrgAdmin(r, activeOrg) {
		// Non-admin members must scope the listing to a project they can view.
		http.Error(w, "project_id is required", http.StatusForbidden)
		return
	}
	list, err := h.proposalService.List(projectID, q.Get("status"), q.Get("run_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if projectID == "" {
		// Workspace admins without a project filter see only proposals whose
		// projects belong to the active workspace.
		orgOf := map[string]string{}
		filtered := list[:0]
		for _, p := range list {
			org, ok := orgOf[p.ProjectID]
			if !ok {
				if project, err := h.projectService.GetProject(p.ProjectID); err == nil && project != nil {
					org = project.OrgID
				}
				orgOf[p.ProjectID] = org
			}
			if org != "" && org == activeOrg {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) reviewProposal(w http.ResponseWriter, r *http.Request, approve bool) {
	id := mux.Vars(r)["id"]
	proposal, err := h.proposalService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, proposal.ProjectID, members.RoleEditor) {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if approve {
		proposal, err = h.proposalService.Approve(id, CurrentUserID(r), req.Note)
	} else {
		proposal, err = h.proposalService.Reject(id, CurrentUserID(r), req.Note)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(proposal)
}

func (h *Handler) ApproveProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, true)
}

func (h *Handler) RejectProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, false)
}

// --- Repo connections ---

func (h *Handler) ListRepoConnections(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	// Personal runners get the claiming user's per-user local paths applied
	// directly: the checkout location differs per member's machine.
	if workerUser := WorkerUser(r); workerUser != "" {
		list, err := h.repoConnService.ListByProjectForUser(projectID, workerUser)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, c := range list {
			if c.MyLocalPath != "" {
				c.LocalPath = c.MyLocalPath
			}
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	// Users see the shared connection plus their own my_local_path.
	if user := CurrentUser(r); user != nil {
		list, err := h.repoConnService.ListByProjectForUser(projectID, user.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	list, err := h.repoConnService.ListByProject(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// SetMyRepoPath stores the caller's per-user local path for a repo
// connection (empty local_path clears it). Any project member may set their
// own path — it only affects runs on their own machine.
func (h *Handler) SetMyRepoPath(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	id := mux.Vars(r)["id"]
	conn, err := h.repoConnService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, conn.ProjectID, members.RoleViewer) {
		return
	}
	var req struct {
		LocalPath string `json:"local_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	user := CurrentUser(r)
	if err := h.repoConnService.SetMyPath(user.ID, id, strings.TrimSpace(req.LocalPath)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	conn.MyLocalPath = strings.TrimSpace(req.LocalPath)
	json.NewEncoder(w).Encode(conn)
}

func (h *Handler) CreateRepoConnection(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	var req repoconns.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.ProjectID = projectID
	conn, err := h.repoConnService.Create(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conn)
}

func (h *Handler) UpdateRepoConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	conn, err := h.repoConnService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, conn.ProjectID, members.RoleOwner) {
		return
	}
	var req repoconns.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.repoConnService.Update(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteRepoConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	conn, err := h.repoConnService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, conn.ProjectID, members.RoleOwner) {
		return
	}
	if err := h.repoConnService.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Provider settings ---

func (h *Handler) ListProviderSettings(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.providerService.List(ActiveOrg(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) UpsertProviderSetting(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	var setting providers.ProviderSetting
	if err := json.NewDecoder(r.Body).Decode(&setting); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	setting.OrgID = ActiveOrg(r)
	if err := h.providerService.Upsert(&setting); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(setting)
}

// RecordProviderDetection stores the worker's provider availability report.
func (h *Handler) RecordProviderDetection(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	var req map[string]map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	for provider, detected := range req {
		if err := h.providerService.RecordDetection(WorkerOrg(r), provider, detected); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Provider CLI login broker ---

// StartProviderLogin creates (or resumes) a CLI sign-in request for a worker
// to execute on a host. Workspace-targeted sign-ins (shared workers) need a
// workspace admin; user-targeted sign-ins run only on the requester's own
// personal runner, so any workspace member may start one.
func (h *Handler) StartProviderLogin(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Target   string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
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
	login, err := h.loginService.StartLogin(ActiveOrg(r), req.Provider, req.Target, CurrentUserID(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	login, err := h.loginService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil
	}
	if login.OrgID != ActiveOrg(r) {
		http.Error(w, "login request not found", http.StatusNotFound)
		return nil
	}
	if login.Target == providers.LoginTargetUser && !h.isOrgAdmin(r, login.OrgID) {
		user := CurrentUser(r)
		if user == nil || login.RequestedBy == nil || *login.RequestedBy != user.ID {
			http.Error(w, "login request not found", http.StatusNotFound)
			return nil
		}
	}
	return login
}

// GetProviderLogin returns login progress for the UI (code never echoed).
func (h *Handler) GetProviderLogin(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
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
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if h.userLoginChecked(w, r) == nil {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	login, err := h.loginService.SubmitCode(mux.Vars(r)["id"], req.Code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(login.Sanitized())
}

// CancelProviderLogin abandons a login request.
func (h *Handler) CancelProviderLogin(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if h.userLoginChecked(w, r) == nil {
		return
	}
	login, err := h.loginService.Cancel(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	login, err := h.loginService.Claim(WorkerOrg(r), WorkerUser(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if login == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	json.NewEncoder(w).Encode(login)
}

// workerLoginChecked loads a login request and verifies it belongs to the
// worker's org. Writes the error response on failure.
func (h *Handler) workerLoginChecked(w http.ResponseWriter, r *http.Request) *providers.LoginRequest {
	login, err := h.loginService.Get(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil
	}
	if login.OrgID != WorkerOrg(r) {
		http.Error(w, "login request not found", http.StatusNotFound)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	login, err := h.loginService.Progress(mux.Vars(r)["id"], req.Status, req.AuthURL, req.Detail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

// --- Teams ---

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.teamService.ListTeams(ActiveOrg(r), r.URL.Query().Get("project_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		ProjectID   *string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// A project-pinned crew must belong to a project in the same workspace.
	if req.ProjectID != nil && *req.ProjectID != "" {
		project, err := h.projectService.GetProject(*req.ProjectID)
		if err != nil || project == nil {
			http.Error(w, "project not found", http.StatusBadRequest)
			return
		}
		if project.OrgID != ActiveOrg(r) {
			http.Error(w, "project does not belong to this workspace", http.StatusBadRequest)
			return
		}
		if !h.requireProjectRole(w, r, *req.ProjectID, members.RoleEditor) {
			return
		}
	} else if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	team, err := h.teamService.CreateTeam(ActiveOrg(r), req.Name, req.Description, req.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	graph, err := h.teamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireOrgRole(w, r, graph.Team.OrgID, orgs.RoleMember) {
		return
	}
	json.NewEncoder(w).Encode(graph)
}

// teamWriteChecked loads a crew and enforces the crew-write guard. Returns
// nil when the response has already been written.
func (h *Handler) teamWriteChecked(w http.ResponseWriter, r *http.Request, teamID string) *teams.Team {
	graph, err := h.teamService.GetTeam(teamID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil
	}
	if !h.requireTeamWrite(w, r, graph.Team) {
		return nil
	}
	return graph.Team
}

func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"]) == nil {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		EntryNodeID *string `json:"entry_node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	team, err := h.teamService.UpdateTeam(mux.Vars(r)["id"], req.Name, req.Description, req.EntryNodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(team)
}

func (h *Handler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"]) == nil {
		return
	}
	if err := h.teamService.DeleteTeam(mux.Vars(r)["id"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CloneTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"]) == nil {
		return
	}
	var req struct {
		Name      string  `json:"name"`
		ProjectID *string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	team, err := h.teamService.CloneTeam(mux.Vars(r)["id"], req.Name, req.ProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

func (h *Handler) AddTeamNode(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"]) == nil {
		return
	}
	var req struct {
		NodeType   string                 `json:"node_type"`
		AgentID    string                 `json:"agent_id"`
		UserID     string                 `json:"user_id"`
		Label      string                 `json:"label"`
		Department string                 `json:"department"`
		Position   map[string]interface{} `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	node, err := h.teamService.AddNode(mux.Vars(r)["id"], teams.NodeSpec{
		NodeType:   req.NodeType,
		AgentID:    req.AgentID,
		UserID:     req.UserID,
		Label:      req.Label,
		Department: req.Department,
		Position:   req.Position,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(node)
}

func (h *Handler) UpdateTeamNode(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	node, err := h.teamService.GetNode(mux.Vars(r)["id"])
	if err != nil || node == nil {
		http.Error(w, "team node not found", http.StatusNotFound)
		return
	}
	if h.teamWriteChecked(w, r, node.TeamID) == nil {
		return
	}
	var req struct {
		Label      *string                `json:"label"`
		AgentID    *string                `json:"agent_id"`
		UserID     *string                `json:"user_id"`
		Department *string                `json:"department"`
		Position   map[string]interface{} `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.teamService.UpdateNode(mux.Vars(r)["id"], req.Label, req.AgentID, req.UserID, req.Department, req.Position)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) RemoveTeamNode(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	node, err := h.teamService.GetNode(mux.Vars(r)["id"])
	if err != nil || node == nil {
		http.Error(w, "team node not found", http.StatusNotFound)
		return
	}
	if h.teamWriteChecked(w, r, node.TeamID) == nil {
		return
	}
	if err := h.teamService.RemoveNode(mux.Vars(r)["id"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddTeamEdge(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"]) == nil {
		return
	}
	var req struct {
		FromNodeID string                 `json:"from_node_id"`
		ToNodeID   string                 `json:"to_node_id"`
		EdgeType   string                 `json:"edge_type"`
		Config     map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	edge, err := h.teamService.AddEdge(mux.Vars(r)["id"], req.FromNodeID, req.ToNodeID, req.EdgeType, req.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(edge)
}

func (h *Handler) UpdateTeamEdge(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	edge, err := h.teamService.GetEdge(mux.Vars(r)["id"])
	if err != nil || edge == nil {
		http.Error(w, "team edge not found", http.StatusNotFound)
		return
	}
	if h.teamWriteChecked(w, r, edge.TeamID) == nil {
		return
	}
	var req struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.teamService.UpdateEdge(mux.Vars(r)["id"], req.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) RemoveTeamEdge(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	edge, err := h.teamService.GetEdge(mux.Vars(r)["id"])
	if err != nil || edge == nil {
		http.Error(w, "team edge not found", http.StatusNotFound)
		return
	}
	if h.teamWriteChecked(w, r, edge.TeamID) == nil {
		return
	}
	if err := h.teamService.RemoveEdge(mux.Vars(r)["id"]); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LaunchTeamRun starts a run at the team's entry node.
func (h *Handler) LaunchTeamRun(w http.ResponseWriter, r *http.Request) {
	graph, err := h.teamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if graph.Team.EntryNodeID == nil {
		http.Error(w, "team has no entry node", http.StatusBadRequest)
		return
	}
	var entryAgentID string
	for _, node := range graph.Nodes {
		if node.ID == *graph.Team.EntryNodeID {
			entryAgentID = node.AgentID
		}
	}
	if entryAgentID == "" {
		http.Error(w, "entry node not found in team", http.StatusBadRequest)
		return
	}
	var req struct {
		ProjectID string `json:"project_id"`
		Prompt    string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// Project-pinned crews launch with project editor rights on the pin;
	// otherwise the request's project scope needs editor rights; a launch
	// with no project scope at all needs workspace admin rights.
	if graph.Team.ProjectID != nil && *graph.Team.ProjectID != "" {
		if !h.requireProjectRole(w, r, *graph.Team.ProjectID, members.RoleEditor) {
			return
		}
	} else if req.ProjectID == "" {
		launchOrg := graph.Team.OrgID
		if launchOrg == "" {
			launchOrg = ActiveOrg(r)
		}
		if !h.requireOrgRole(w, r, launchOrg, orgs.RoleAdmin) {
			return
		}
	}
	if req.ProjectID != "" && !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}
	// Crew runs belong to the crew's org, falling back to the project's org
	// then the caller's active workspace.
	orgID := graph.Team.OrgID
	if orgID == "" && req.ProjectID != "" {
		if project, err := h.projectService.GetProject(req.ProjectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	if orgID == "" {
		orgID = ActiveOrg(r)
	}
	teamID := graph.Team.ID
	launch := agentruns.LaunchRequest{
		OrgID:      orgID,
		AgentID:    entryAgentID,
		TeamID:     &teamID,
		TeamNodeID: graph.Team.EntryNodeID,
		Prompt:     req.Prompt,
		LaunchedBy: CurrentUserID(r),
	}
	if req.ProjectID != "" {
		launch.ProjectID = &req.ProjectID
	}
	run, _, err := h.runService.Launch(launch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

// --- Domain events ---

func (h *Handler) ListDomainEvents(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	q := r.URL.Query()
	if projectID := q.Get("project_id"); projectID != "" {
		if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
			return
		}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := h.eventRepo.List(ActiveOrg(r), q.Get("project_id"), q.Get("event_type"), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}
