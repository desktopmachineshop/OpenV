package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/crewtemplates"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
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
	router.HandleFunc("/api/v1/agent-runs/{id}/retry", h.RetryAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/start", h.StartAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/release", h.ReleaseAgentRun).Methods("POST")
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
	router.HandleFunc("/api/v1/proposals/bulk", h.BulkReviewProposals).Methods("POST")
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
	router.HandleFunc("/api/v1/crews/{id}/export", h.ExportCrew).Methods("GET")
	router.HandleFunc("/api/v1/crews/import", h.ImportCrew).Methods("POST")
	router.HandleFunc("/api/v1/crew-templates", h.ListCrewTemplates).Methods("GET")
	router.HandleFunc("/api/v1/crews/{id}/nodes", h.AddTeamNode).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}/runs", h.LaunchTeamRun).Methods("POST")
	router.HandleFunc("/api/v1/crew-nodes/{id}", h.UpdateTeamNode).Methods("PUT")
	router.HandleFunc("/api/v1/crew-nodes/{id}", h.RemoveTeamNode).Methods("DELETE")
	router.HandleFunc("/api/v1/crews/{id}/edges", h.AddTeamEdge).Methods("POST")
	router.HandleFunc("/api/v1/crew-edges/{id}", h.UpdateTeamEdge).Methods("PUT")
	router.HandleFunc("/api/v1/crew-edges/{id}", h.RemoveTeamEdge).Methods("DELETE")

	// Teams (deprecated aliases for the crews routes above).
	router.HandleFunc("/api/v1/teams", h.ListTeams).Methods("GET")                   // deprecated: use /api/v1/crews
	router.HandleFunc("/api/v1/teams", h.CreateTeam).Methods("POST")                 // deprecated: use /api/v1/crews
	router.HandleFunc("/api/v1/teams/{id}", h.GetTeam).Methods("GET")                // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}", h.UpdateTeam).Methods("PUT")             // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}", h.DeleteTeam).Methods("DELETE")          // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}/clone", h.CloneTeam).Methods("POST")       // deprecated: use /api/v1/crews/{id}/clone
	router.HandleFunc("/api/v1/teams/{id}/export", h.ExportCrew).Methods("GET")      // deprecated: use /api/v1/crews/{id}/export
	router.HandleFunc("/api/v1/teams/import", h.ImportCrew).Methods("POST")          // deprecated: use /api/v1/crews/import
	router.HandleFunc("/api/v1/teams/{id}/nodes", h.AddTeamNode).Methods("POST")     // deprecated: use /api/v1/crews/{id}/nodes
	router.HandleFunc("/api/v1/teams/{id}/runs", h.LaunchTeamRun).Methods("POST")    // deprecated: use /api/v1/crews/{id}/runs
	router.HandleFunc("/api/v1/team-nodes/{id}", h.UpdateTeamNode).Methods("PUT")    // deprecated: use /api/v1/crew-nodes/{id}
	router.HandleFunc("/api/v1/team-nodes/{id}", h.RemoveTeamNode).Methods("DELETE") // deprecated: use /api/v1/crew-nodes/{id}
	router.HandleFunc("/api/v1/teams/{id}/edges", h.AddTeamEdge).Methods("POST")     // deprecated: use /api/v1/crews/{id}/edges
	router.HandleFunc("/api/v1/team-edges/{id}", h.UpdateTeamEdge).Methods("PUT")    // deprecated: use /api/v1/crew-edges/{id}
	router.HandleFunc("/api/v1/team-edges/{id}", h.RemoveTeamEdge).Methods("DELETE") // deprecated: use /api/v1/crew-edges/{id}

	// Domain event audit.
	router.HandleFunc("/api/v1/events", h.ListDomainEvents).Methods("GET")
}

func requireUser(w http.ResponseWriter, r *http.Request) bool {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	return true
}

func requireWorker(w http.ResponseWriter, r *http.Request) bool {
	if !IsWorker(r) {
		writeJSONError(w, http.StatusForbidden, "worker credentials required")
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
		respondInternal(w, r, "failed to list agents", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Friendly pre-check; the (org_id, slug) unique index is the real guard,
	// so a concurrent create that slips past this still conflicts below.
	if existing, _ := h.agentService.GetBySlug(ActiveOrg(r), def.Slug); existing != nil {
		writeJSONError(w, http.StatusConflict, "an agent with this slug already exists")
		return
	}
	agent, err := h.agentService.SaveDefinition(ActiveOrg(r), &def)
	if err != nil {
		if errors.Is(err, agents.ErrSlugExists) {
			writeJSONError(w, http.StatusConflict, "an agent with this slug already exists")
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to load agent", err)
		return
	}
	if agent == nil {
		writeJSONError(w, http.StatusNotFound, "agent not found")
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if def.Slug == "" {
		def.Slug = slug
	}
	if def.Slug != slug {
		writeJSONError(w, http.StatusBadRequest, "slug in body does not match URL")
		return
	}
	agent, err := h.agentService.SaveDefinition(ActiveOrg(r), &def)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	if err := h.agentService.Delete(ActiveOrg(r), mux.Vars(r)["slug"]); err != nil {
		respondInternal(w, r, "failed to delete agent", err)
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
		respondError(w, r, http.StatusNotFound, "agent not found", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agent, err := h.agentService.SaveRawFile(ActiveOrg(r), mux.Vars(r)["slug"], req.Content)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(agent)
}

func (h *Handler) SyncAgents(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	if err := h.agentService.SyncFromDisk(ActiveOrg(r)); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := h.agentService.List(ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to list agents", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// --- Runs ---

// LaunchAgentRun starts a manual run for an agent (by slug).
func (h *Handler) LaunchAgentRun(w http.ResponseWriter, r *http.Request) {
	agent, err := h.agentService.GetBySlug(ActiveOrg(r), mux.Vars(r)["slug"])
	if err != nil || agent == nil {
		writeJSONError(w, http.StatusNotFound, "agent not found")
		return
	}
	var req struct {
		ProjectID  string `json:"project_id"`
		Prompt     string `json:"prompt"`
		WorkItemID string `json:"work_item_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
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
		// Over-budget soft-block (enforcement on) is a distinct, expected
		// refusal — surface it as 402 so the UI can message it clearly.
		if errors.Is(err, agentruns.ErrBudgetExceeded) {
			writeJSONError(w, http.StatusPaymentRequired, err.Error())
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
	// Scope the listing to the active workspace in SQL (so a busy sibling
	// workspace can never starve the page before LIMIT applies); without a
	// project filter, non-admin members only see the runs they launched
	// themselves.
	activeOrg := ActiveOrg(r)
	filter := agentruns.ListFilter{
		OrgID:     activeOrg,
		AgentID:   q.Get("agent_id"),
		ProjectID: projectID,
		Status:    q.Get("status"),
		ParentID:  q.Get("parent_id"),
		Limit:     limit,
	}
	if projectID == "" && !h.isOrgAdmin(r, activeOrg) {
		filter.LaunchedBy = CurrentUser(r).ID
	}
	runs, err := h.runService.List(filter)
	if err != nil {
		respondInternal(w, r, "failed to list agent runs", err)
		return
	}
	json.NewEncoder(w).Encode(runs)
}

func (h *Handler) GetAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
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
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	tree, err := h.runService.Tree(run.ID)
	if err != nil {
		respondInternal(w, r, "failed to load run tree", err)
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
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	afterSeq, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
	logs, err := h.runService.Logs(run.ID, afterSeq)
	if err != nil {
		respondInternal(w, r, "failed to load run logs", err)
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
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	afterSeq, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
	h.sseHub.ServeStream(w, r, runID, func(emit func(event string, data interface{})) error {
		// Logs is paged (bounded per call), so drain every page before the live
		// tail takes over: keep advancing the cursor to the last seq seen until
		// a page comes back empty. seq strictly increases, so this terminates.
		cursor := afterSeq
		for {
			logs, err := h.runService.Logs(runID, cursor)
			if err != nil {
				return err
			}
			if len(logs) == 0 {
				break
			}
			for _, entry := range logs {
				emit("log", entry)
				cursor = entry.Seq
			}
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
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleEditor) {
		return
	}
	cancelled, err := h.runService.RequestCancel(run.ID)
	if err != nil {
		respondInternal(w, r, "failed to cancel run", err)
		return
	}
	json.NewEncoder(w).Encode(cancelled)
}

// RetryAgentRun re-enqueues a terminal run as a NEW run with the same org,
// agent, project and prompt, launched by the retrying user (so their
// personal-runner reservation applies) with provenance in
// retried_from_run_id. Access mirrors launching/cancelling: the original
// launcher, project editors, or workspace admins for unscoped runs. Only
// failed, cancelled, and timed_out runs are retryable — a status conflict
// answers 409 with the sentinel text, like the worker lifecycle endpoints.
func (h *Handler) RetryAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleEditor) {
		return
	}
	retried, err := h.runService.Retry(run.ID, CurrentUserID(r))
	if err != nil {
		if errors.Is(err, agentruns.ErrNotRetryable) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		respondInternal(w, r, "failed to retry run", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(retried)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Hosted runners never execute repo-access agents.
	run, err := h.runService.Claim(req.WorkerID, WorkerOrg(r), WorkerUser(r), req.Providers, req.MinPriority, req.Hosted)
	if err != nil {
		respondInternal(w, r, "failed to claim a run", err)
		return
	}
	if run == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// The worker needs the agent definition and the run token context; the
	// token itself was minted at enqueue and is returned only here, derived
	// fresh so the worker can hand it to the MCP server. If the handshake
	// fails after the claim, release the run back to the queue so it isn't
	// stranded in 'claimed' until the stale reaper.
	agent, err := h.agentService.Get(run.AgentID)
	if err != nil || agent == nil {
		h.releaseFailedClaim(run.ID, req.WorkerID)
		respondInternal(w, r, "agent not found for claimed run", err)
		return
	}
	token, err := h.runService.ReissueToken(run.ID)
	if err != nil {
		h.releaseFailedClaim(run.ID, req.WorkerID)
		respondInternal(w, r, "failed to issue run token", err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run":       run,
		"agent":     agent,
		"run_token": token,
		"auth":      h.resolveRunAuth(run, agent),
	})
}

// releaseFailedClaim rolls a claim back to queued after a failed claim
// handshake (best effort — the stale reaper remains the backstop).
func (h *Handler) releaseFailedClaim(runID, workerID string) {
	if err := h.runService.ReleaseClaim(runID, workerID); err != nil {
		slog.Error("api: failed to release claim after failed handshake",
			"run_id", runID, "worker_id", workerID, "error", err)
	}
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

// requireWorkerRun resolves the {id} run for a worker lifecycle call and
// verifies the worker credential may act on it: the run must belong to the
// worker's org, and a personal runner key may only touch runs its user could
// have claimed (their own or ownerless — mirrors Claim). Cross-org and
// unknown run IDs both answer 404 so a foreign worker cannot probe whether a
// run exists. Returns nil after writing the response when access is denied.
func (h *Handler) requireWorkerRun(w http.ResponseWriter, r *http.Request) *agentruns.Run {
	run, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil || run == nil || run.OrgID != WorkerOrg(r) {
		writeJSONError(w, http.StatusNotFound, "agent run not found")
		return nil
	}
	if workerUser := WorkerUser(r); workerUser != "" && run.LaunchedBy != nil && *run.LaunchedBy != workerUser {
		writeJSONError(w, http.StatusNotFound, "agent run not found")
		return nil
	}
	return run
}

func (h *Handler) StartAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	run := h.requireWorkerRun(w, r)
	if run == nil {
		return
	}
	if err := h.runService.MarkRunning(run.ID); err != nil {
		// A conflicting status (e.g. the run was cancelled or reaped while
		// the worker was starting) is the worker's problem: 409 with the
		// domain sentinel. Anything else is ours — answer 5xx so the worker
		// knows to retry rather than abandon the run.
		if errors.Is(err, agentruns.ErrInvalidTransition) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		respondInternal(w, r, "failed to start run", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReleaseAgentRun hands a claimed/running run back to the queue at the
// worker's request — used when the worker is shutting down (SIGINT) mid-run,
// so the run is reclaimable by another (or a restarted) worker instead of
// being burned as failed. The release is conditional (still owned by this
// worker, not terminal), so it can never resurrect a run another actor already
// moved; a no-op is reported as success.
func (h *Handler) ReleaseAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	run := h.requireWorkerRun(w, r)
	if run == nil {
		return
	}
	var req struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.runService.ReleaseClaim(run.ID, req.WorkerID); err != nil {
		respondInternal(w, r, "failed to release run", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AppendAgentRunLogs(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	if h.requireWorkerRun(w, r) == nil {
		return
	}
	var entries []agentruns.LogEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, err := h.runService.AppendLogs(mux.Vars(r)["id"], entries)
	if err != nil {
		respondInternal(w, r, "failed to append run logs", err)
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
	if h.requireWorkerRun(w, r) == nil {
		return
	}
	var req agentruns.FinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, err := h.runService.Finish(mux.Vars(r)["id"], req)
	if err != nil {
		// Already-finished / bad-status transitions are 409 with the domain
		// sentinel. Every other failure (a DB blip, say) must be a 5xx: the
		// worker retries on 5xx, and a run's final result must not be lost
		// because we mislabeled an internal error as the worker's fault.
		if errors.Is(err, agentruns.ErrInvalidTransition) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		respondInternal(w, r, "failed to record run result", err)
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
		writeJSONError(w, http.StatusForbidden, "delegation requires an agent run token")
		return
	}
	if run.TeamNodeID == nil {
		writeJSONError(w, http.StatusBadRequest, "this run is not part of a team; delegation is unavailable")
		return
	}
	var req struct {
		RoleLabel string `json:"role_label"`
		Prompt    string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate the one caller-supplied Launch input up front so the only
	// Launch failures left are internal ones (org/agent/team are resolved
	// server-side).
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	children, err := h.teamService.ResolveDelegates(*run.TeamNodeID)
	if err != nil {
		respondInternal(w, r, "failed to resolve delegates", err)
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
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("no delegate named %q; available delegates: %s", req.RoleLabel, strings.Join(labels, ", ")))
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
		if errors.Is(err, agentruns.ErrInvalidTransition) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		respondInternal(w, r, "failed to launch delegated run", err)
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
		writeJSONError(w, http.StatusForbidden, "delegation requires an agent run token")
		return
	}
	child, err := h.runService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if child.ParentRunID == nil || *child.ParentRunID != run.ID {
		writeJSONError(w, http.StatusForbidden, "not your delegated run")
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
		respondInternal(w, r, "failed to list automations", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.OrgID = ActiveOrg(r)
	if !h.requireAutomationWrite(w, r, req.ProjectID, req.OrgID) {
		return
	}
	automation, err := h.automationService.Create(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondError(w, r, http.StatusNotFound, "automation not found", err)
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
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID) {
		return
	}
	var req automations.UpdateAutomationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.automationService.Update(automation.ID, req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID) {
		return
	}
	if err := h.automationService.Delete(automation.ID); err != nil {
		respondInternal(w, r, "failed to delete automation", err)
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
		respondError(w, r, http.StatusNotFound, "automation not found", err)
		return
	}
	if !h.requireAutomationWrite(w, r, automation.ProjectID, automation.OrgID) {
		return
	}
	agentID, teamID, teamNodeID, err := scheduler.ResolveTarget(automation, h.teamService)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusForbidden, "project_id is required")
		return
	}
	list, err := h.proposalService.List(projectID, q.Get("status"), q.Get("run_id"))
	if err != nil {
		respondInternal(w, r, "failed to list proposals", err)
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
		respondError(w, r, http.StatusNotFound, "proposal not found", err)
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
		// Genuine proposal-domain validation errors are safe to surface; an
		// applier failure may carry internal details, so route it through the
		// sanitized 500 path per the #146 contract (the proposal is still
		// persisted as apply_failed with the real error in its review note).
		if isProposalValidationError(err) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to apply approved proposal", err)
		}
		return
	}
	json.NewEncoder(w).Encode(proposal)
}

// isProposalValidationError reports whether err is a proposal-domain
// validation sentinel whose message is safe to show a client. Everything
// else from Approve/Reject is an applier failure to be sanitized (#146).
func isProposalValidationError(err error) bool {
	return errors.Is(err, proposals.ErrNotPending) ||
		errors.Is(err, proposals.ErrNotFound) ||
		errors.Is(err, proposals.ErrUnsupportedOp) ||
		errors.Is(err, proposals.ErrRunWriteCap)
}

// proposalReviewErrorMessage returns a client-safe message for a review
// error inside a bulk outcome: validation sentinels pass through, applier
// failures are logged with context and replaced with a stable message.
func proposalReviewErrorMessage(r *http.Request, id string, err error) string {
	if isProposalValidationError(err) {
		return err.Error()
	}
	slog.Error("failed to apply approved proposal",
		slog.String("proposal_id", id),
		slog.String("path", r.URL.Path),
		slog.Any("error", err))
	return "failed to apply approved proposal"
}

func (h *Handler) ApproveProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, true)
}

// bulkOutcome reports one proposal's fate inside a bulk review request.
type bulkOutcome struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// BulkReviewProposals approves or rejects up to MaxProposalsPerRun proposals
// in one request. Proposals are processed sequentially and each one goes
// through exactly the same ladder as a single review: load, project-editor
// authz, then the same Approve/Reject service methods (approvals apply real
// writes via the wired appliers). The response is 200 even on partial
// failure — the client renders the per-id outcomes.
func (h *Handler) BulkReviewProposals(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
		Note   string   `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		writeJSONError(w, http.StatusBadRequest, `action must be "approve" or "reject"`)
		return
	}
	if len(req.IDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "ids is required")
		return
	}
	if len(req.IDs) > proposals.MaxProposalsPerRun {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("at most %d proposals per bulk request", proposals.MaxProposalsPerRun))
		return
	}

	reviewer := CurrentUserID(r)
	results := make([]bulkOutcome, 0, len(req.IDs))
	for _, id := range req.IDs {
		proposal, err := h.proposalService.Get(id)
		if err != nil {
			results = append(results, bulkOutcome{ID: id, Error: "proposal not found"})
			continue
		}
		if !h.hasProjectRole(r, proposal.ProjectID, members.RoleEditor) {
			results = append(results, bulkOutcome{ID: id, Error: "you do not have access to this project"})
			continue
		}
		if req.Action == "approve" {
			_, err = h.proposalService.Approve(id, reviewer, req.Note)
		} else {
			_, err = h.proposalService.Reject(id, reviewer, req.Note)
		}
		if err != nil {
			results = append(results, bulkOutcome{ID: id, Error: proposalReviewErrorMessage(r, id, err)})
			continue
		}
		results = append(results, bulkOutcome{ID: id, OK: true})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
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

	// Personal runners get the claiming user's local paths: the checkout
	// lives somewhere different on every member's machine.
	if workerUser := WorkerUser(r); workerUser != "" {
		list, err := h.repoConnService.ListByProjectForUser(projectID, workerUser)
		if err != nil {
			respondInternal(w, r, "failed to list repo connections", err)
			return
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	// Users see the project's connections plus their own my_local_path.
	if user := CurrentUser(r); user != nil {
		list, err := h.repoConnService.ListByProjectForUser(projectID, user.ID)
		if err != nil {
			respondInternal(w, r, "failed to list repo connections", err)
			return
		}
		json.NewEncoder(w).Encode(list)
		return
	}

	list, err := h.repoConnService.ListByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list repo connections", err)
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
		respondError(w, r, http.StatusNotFound, "repo connection not found", err)
		return
	}
	if !h.requireProjectRole(w, r, conn.ProjectID, members.RoleViewer) {
		return
	}
	var req struct {
		LocalPath string `json:"local_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user := CurrentUser(r)
	if err := h.repoConnService.SetMyPath(user.ID, id, strings.TrimSpace(req.LocalPath)); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ProjectID = projectID
	conn, err := h.repoConnService.Create(req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(conn)
}

func (h *Handler) UpdateRepoConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	conn, err := h.repoConnService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "repo connection not found", err)
		return
	}
	if !h.requireProjectRole(w, r, conn.ProjectID, members.RoleOwner) {
		return
	}
	var req repoconns.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.repoConnService.Update(id, req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteRepoConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	conn, err := h.repoConnService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "repo connection not found", err)
		return
	}
	if !h.requireProjectRole(w, r, conn.ProjectID, members.RoleOwner) {
		return
	}
	if err := h.repoConnService.Delete(id); err != nil {
		respondInternal(w, r, "failed to delete repo connection", err)
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
		respondInternal(w, r, "failed to list provider settings", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	setting.OrgID = ActiveOrg(r)
	if err := h.providerService.Upsert(&setting); err != nil {
		if errors.Is(err, providers.ErrInvalidSetting) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to save provider setting", err)
		}
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for provider, detected := range req {
		if err := h.providerService.RecordDetection(WorkerOrg(r), provider, detected); err != nil {
			if errors.Is(err, providers.ErrInvalidSetting) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
			} else {
				respondInternal(w, r, "failed to record provider detection", err)
			}
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
	login, err := h.loginService.StartLogin(ActiveOrg(r), req.Provider, req.Target, CurrentUserID(r))
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
	login, err := h.loginService.Get(mux.Vars(r)["id"])
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
	if h.userLoginChecked(w, r) == nil {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	login, err := h.loginService.SubmitCode(mux.Vars(r)["id"], req.Code)
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
	if h.userLoginChecked(w, r) == nil {
		return
	}
	login, err := h.loginService.Cancel(mux.Vars(r)["id"])
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
	login, err := h.loginService.Claim(WorkerOrg(r), WorkerUser(r))
	if err != nil {
		respondInternal(w, r, "failed to claim login request", err)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	login, err := h.loginService.Progress(mux.Vars(r)["id"], req.Status, req.AuthURL, req.Detail)
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

// --- Teams ---

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.teamService.ListTeams(ActiveOrg(r), r.URL.Query().Get("project_id"))
	if err != nil {
		respondInternal(w, r, "failed to list crews", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A project-pinned crew must belong to a project in the same workspace.
	if req.ProjectID != nil && *req.ProjectID != "" {
		project, err := h.projectService.GetProject(*req.ProjectID)
		if err != nil || project == nil {
			writeJSONError(w, http.StatusBadRequest, "project not found")
			return
		}
		if project.OrgID != ActiveOrg(r) {
			writeJSONError(w, http.StatusBadRequest, "project does not belong to this workspace")
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondError(w, r, http.StatusNotFound, "team not found", err)
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
		respondError(w, r, http.StatusNotFound, "team not found", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	team, err := h.teamService.UpdateTeam(mux.Vars(r)["id"], req.Name, req.Description, req.EntryNodeID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		respondInternal(w, r, "failed to delete crew", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	team, err := h.teamService.CloneTeam(mux.Vars(r)["id"], req.Name, req.ProjectID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

// ExportCrew returns a crew as a portable, org-independent JSON document whose
// nodes reference agents by slug. Any member of the crew's workspace may
// export. Human nodes are omitted (their identities aren't portable).
func (h *Handler) ExportCrew(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	graph, err := h.teamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "crew not found", err)
		return
	}
	if !h.requireOrgRole(w, r, graph.Team.OrgID, orgs.RoleMember) {
		return
	}
	portable, err := crewtemplates.Serialize(graph, h.agentService)
	if err != nil {
		respondInternal(w, r, "failed to export crew", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", crewExportFilename(graph.Team.Name)))
	json.NewEncoder(w).Encode(portable)
}

// ImportCrew creates a crew in the active workspace from a portable crew
// document, resolving each node's agent slug to this workspace's agents.
// Missing slugs are skipped with a warning (returned in the response) rather
// than failing the import. Authz mirrors CreateTeam: a project-pinned import
// (?project_id=) needs project editor rights, a workspace-wide one needs
// workspace admin rights.
func (h *Handler) ImportCrew(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	orgID := ActiveOrg(r)
	projectID := optionalProjectID(r)
	if projectID != nil {
		project, err := h.projectService.GetProject(*projectID)
		if err != nil || project == nil {
			writeJSONError(w, http.StatusBadRequest, "project not found")
			return
		}
		if project.OrgID != orgID {
			writeJSONError(w, http.StatusBadRequest, "project does not belong to this workspace")
			return
		}
		if !h.requireProjectRole(w, r, *projectID, members.RoleEditor) {
			return
		}
	} else if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}

	var doc crewtemplates.PortableCrew
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := crewtemplates.Import(&doc, orgID, projectID, h.agentService, h.teamService)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// ListCrewTemplates returns the built-in crew presets. Each carries its full
// portable document so the client can hand a chosen preset straight to the
// import endpoint.
func (h *Handler) ListCrewTemplates(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	json.NewEncoder(w).Encode(crewtemplates.BuiltinCrewTemplates())
}

// optionalProjectID reads an optional ?project_id= pin from the request.
func optionalProjectID(r *http.Request) *string {
	v := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if v == "" {
		return nil
	}
	return &v
}

// crewExportFilename derives a safe download name from a crew name.
func crewExportFilename(name string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == ' ' || ch == '-' || ch == '_':
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "crew"
	}
	return slug + ".crew.json"
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusNotFound, "team node not found")
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.teamService.UpdateNode(mux.Vars(r)["id"], req.Label, req.AgentID, req.UserID, req.Department, req.Position)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusNotFound, "team node not found")
		return
	}
	if h.teamWriteChecked(w, r, node.TeamID) == nil {
		return
	}
	if err := h.teamService.RemoveNode(mux.Vars(r)["id"]); err != nil {
		respondInternal(w, r, "failed to remove crew node", err)
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
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	edge, err := h.teamService.AddEdge(mux.Vars(r)["id"], req.FromNodeID, req.ToNodeID, req.EdgeType, req.Config)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusNotFound, "team edge not found")
		return
	}
	if h.teamWriteChecked(w, r, edge.TeamID) == nil {
		return
	}
	var req struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.teamService.UpdateEdge(mux.Vars(r)["id"], req.Config)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
		writeJSONError(w, http.StatusNotFound, "team edge not found")
		return
	}
	if h.teamWriteChecked(w, r, edge.TeamID) == nil {
		return
	}
	if err := h.teamService.RemoveEdge(mux.Vars(r)["id"]); err != nil {
		respondInternal(w, r, "failed to remove crew edge", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LaunchTeamRun starts a run at the team's entry node.
func (h *Handler) LaunchTeamRun(w http.ResponseWriter, r *http.Request) {
	graph, err := h.teamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
		return
	}
	if graph.Team.EntryNodeID == nil {
		writeJSONError(w, http.StatusBadRequest, "team has no entry node")
		return
	}
	var entryAgentID string
	for _, node := range graph.Nodes {
		if node.ID == *graph.Team.EntryNodeID {
			entryAgentID = node.AgentID
		}
	}
	if entryAgentID == "" {
		writeJSONError(w, http.StatusBadRequest, "entry node not found in team")
		return
	}
	var req struct {
		ProjectID string `json:"project_id"`
		Prompt    string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
	projectID := q.Get("project_id")
	if projectID != "" && !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// "before" is a keyset cursor (an event ID from a previous page): the
	// repo returns only events strictly older than it, so "load more" pages
	// stay stable while new events keep arriving. It is cast to uuid in SQL,
	// so a malformed value would otherwise surface as a 500 — validate it here
	// and reject bad input with 400 instead.
	before := q.Get("before")
	if before != "" {
		if _, err := uuid.Parse(before); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid before cursor")
			return
		}
	}
	list, err := h.eventRepo.List(ActiveOrg(r), projectID, q.Get("event_type"), before, limit)
	if err != nil {
		respondInternal(w, r, "failed to list events", err)
		return
	}
	// Advertise the next cursor from the RAW page (before membership
	// filtering below): a full page means there may be older events. Using
	// the raw tail keeps the cursor monotonic even when the visibility filter
	// drops the trailing rows for non-admin members.
	if len(list) >= limit {
		w.Header().Set("X-Next-Cursor", list[len(list)-1].ID)
	}
	// Without a project filter, org admins see the whole workspace audit;
	// plain members only see events for projects they can access (mirrors
	// ListAgentRuns).
	if projectID == "" && !h.isOrgAdmin(r, ActiveOrg(r)) {
		user := CurrentUser(r)
		allowed := map[string]bool{}
		if h.memberService != nil {
			ids, err := h.memberService.ProjectIDsForUser(user.ID)
			if err != nil {
				respondInternal(w, r, "failed to resolve project memberships", err)
				return
			}
			for _, id := range ids {
				allowed[id] = true
			}
		}
		visible := list[:0]
		for _, e := range list {
			if e.ProjectID != "" && allowed[e.ProjectID] {
				visible = append(visible, e)
			}
		}
		list = visible
	}
	json.NewEncoder(w).Encode(list)
}
