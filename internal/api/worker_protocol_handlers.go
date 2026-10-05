package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// registerWorkerDispatchRoutes wires a worker claiming a run, and a running
// agent delegating to a child and polling the child's status.
// registerAgentRoutes calls it before a run's {id} routes, so that
// GET /api/v1/agent-runs/delegate/{id} goes on shadowing the {id}/tree,
// {id}/logs and {id}/stream reads (route_overlaps.txt).
func (h *Handler) registerWorkerDispatchRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-runs/claim", h.ClaimAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/delegate", h.DelegateRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/delegate/{id}", h.DelegateStatus).Methods("GET")
}

// registerWorkerLogRoutes wires a worker appending to a run's logs.
func (h *Handler) registerWorkerLogRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-runs/{id}/logs", h.AppendAgentRunLogs).Methods("POST")
}

// registerWorkerLifecycleRoutes wires a worker starting, releasing and
// finishing a run it claimed.
func (h *Handler) registerWorkerLifecycleRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-runs/{id}/start", h.StartAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/release", h.ReleaseAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/finish", h.FinishAgentRun).Methods("POST")
}

func (h *Handler) ClaimAgentRun(w http.ResponseWriter, r *http.Request) {
	if !requireWorker(w, r) {
		return
	}
	var req workerproto.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A transient runner's idle clock measures work, not polling: claiming a
	// run is what counts as use.
	h.touchRunnerSession(r)
	// Unattended hosted compute is a plan flag. The gate is here, at the
	// hosted claim, and not at the automation: the same queued run claimed
	// by the member's own machine through the Agent Connector is exactly
	// the run that is never gated.
	if req.Hosted {
		if err := h.checkFlag(WorkerOrg(r), orgs.LimitHostedAutomation); err != nil {
			h.writeLimitError(w, err)
			return
		}
	}
	// Hosted runners never execute repo-access agents. A personal key takes
	// its member's runs and only the ownerless ones its member could see
	// (the claim query asks it, as holderSeesRun does), and the claim
	// records the member, whose local paths the run's token reads.
	run, err := h.RunService.Claim(req.WorkerID, WorkerOrg(r), WorkerUser(r), req.Providers, req.MinPriority, req.Hosted)
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
	agent, err := h.AgentService.Get(run.AgentID)
	if err != nil || agent == nil {
		h.releaseFailedClaim(run.ID, req.WorkerID)
		respondInternal(w, r, "agent not found for claimed run", err)
		return
	}
	token, err := h.RunService.ReissueToken(run.ID)
	if errors.Is(err, agentruns.ErrInvalidTransition) {
		// The run was asked to stop between the claim and now (a project's
		// delete revoked its token, #379 bug 151), or is no longer held: it
		// gets no token. Handed back, a run asked to stop ends cancelled;
		// the worker has nothing to run.
		h.releaseFailedClaim(run.ID, req.WorkerID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		h.releaseFailedClaim(run.ID, req.WorkerID)
		respondInternal(w, r, "failed to issue run token", err)
		return
	}
	json.NewEncoder(w).Encode(workerproto.ClaimResponse{
		Agent:    agent,
		Auth:     h.resolveRunAuth(run, agent),
		Run:      run,
		RunToken: token,
	})
}

// releaseFailedClaim rolls a claim back to queued after a failed claim
// handshake (best effort — the stale reaper remains the backstop).
func (h *Handler) releaseFailedClaim(runID, workerID string) {
	if err := h.RunService.ReleaseClaim(runID, workerID); err != nil {
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
	project, err := h.ProjectService.GetProject(*run.ProjectID)
	if err != nil || project == nil || project.AgentAuth != projects.AgentAuthAPIKey {
		return auth
	}
	auth["mode"] = projects.AgentAuthAPIKey
	keyEnv := providers.DefaultAPIKeyEnv(agent.Provider)
	if h.ProviderService != nil {
		if settings, err := h.ProviderService.List(run.OrgID); err == nil {
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
	run := h.requireWorkerRun(w, r)
	if run == nil {
		return
	}
	if err := h.RunService.MarkRunning(run.ID); err != nil {
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
	var req workerproto.ReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.RunService.ReleaseClaim(run.ID, req.WorkerID); err != nil {
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
	entries, partialText, err := decodeRunLogBody(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	run, err := h.RunService.AppendLogs(mux.Vars(r)["id"], entries, partialText)
	if err != nil {
		respondInternal(w, r, "failed to append run logs", err)
		return
	}
	json.NewEncoder(w).Encode(workerproto.LogsResponse{
		CancelRequested: run.CancelRequested,
		Status:          run.Status,
	})
}

// decodeRunLogBody reads a worker's log push in either shape: the current
// object — {"entries": [...], "partial_text": "..."} — or the bare array of
// entries that runners built before streaming shipped still send. partial_text
// is the whole assistant answer so far, not a delta; empty means "unchanged".
func decodeRunLogBody(body io.Reader) ([]agentruns.LogEntry, string, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, "", err
	}
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var entries workerproto.LegacyLogsRequest
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, "", err
		}
		return entries, "", nil
	}
	var payload workerproto.LogsRequest
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", err
	}
	return payload.Entries, payload.PartialText, nil
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
	run, err := h.RunService.Finish(mux.Vars(r)["id"], req)
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

	children, err := h.TeamService.ResolveDelegates(*run.TeamNodeID)
	if err != nil {
		// The run's crew node was removed after the run launched (the run
		// keeps its id, with no foreign key): the node routes' answer.
		if errors.Is(err, teams.ErrNodeNotFound) {
			writeJSONError(w, http.StatusNotFound, "team node not found")
			return
		}
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
	child, _, err := h.RunService.Launch(agentruns.LaunchRequest{
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
		writeLaunchError(w, r, launchErrsDelegate, err)
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
	child, err := h.RunService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if child.ParentRunID == nil || *child.ParentRunID != run.ID {
		// A run outside the caller's project, of its workspace or another,
		// is one no row has (I3); one of its own project is not its child.
		if child.OrgID != run.OrgID || !sameProject(child.ProjectID, run.ProjectID) {
			writeJSONError(w, http.StatusNotFound, "agent run not found")
			return
		}
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
