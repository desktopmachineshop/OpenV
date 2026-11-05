package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/workerproto"
)

// registerWorkerDispatchRoutes wires a worker claiming a run. A running
// agent delegating to a crew delegate went with crews (issue 732).
// registerAgentRoutes calls it before a run's {id} routes.
func (h *Handler) registerWorkerDispatchRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-runs/claim", h.ClaimAgentRun).Methods("POST")
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
	if !decodeJSON(w, r, &req) {
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
	writeJSONBare(w, workerproto.ClaimResponse{
		Agent:    agent,
		Auth:     h.resolveRunAuth(r, run, agent),
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
// injects the workspace's pasted key for api_key_env (org provider setting,
// or the provider's native variable) when there is one, else that variable
// from its host environment. Otherwise a run launched by a member who stored
// a subscription token carries that token, but only to a runner that serves
// that member alone (their personal runner key, or a lease bound to them): a
// workspace runner is someone else's host, and it keeps using its own
// sign-in. Everything else uses the runner's local sign-in.
func (h *Handler) resolveRunAuth(r *http.Request, run *agentruns.Run, agent *agents.Agent) map[string]string {
	auth := map[string]string{"mode": projects.AgentAuthUserAccount}
	if run.ProjectID != nil && *run.ProjectID != "" {
		project, err := h.ProjectService.GetProject(*run.ProjectID)
		if err == nil && project != nil && project.AgentAuth == projects.AgentAuthAPIKey {
			h.resolveAPIKeyAuth(auth, run, agent)
			return auth
		}
	}
	if token := h.subscriptionTokenFor(r, run, agent); token != "" {
		auth["mode"] = workerproto.AuthSubscriptionToken
		auth["oauth_token"] = token
	}
	return auth
}

// resolveAPIKeyAuth fills auth for a run of a project on api-key auth.
func (h *Handler) resolveAPIKeyAuth(auth map[string]string, run *agentruns.Run, agent *agents.Agent) {
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
	// A key a workspace admin pasted in the app goes with the claim, so the
	// runner needs nothing in its host's environment. A key that does not
	// open (the server's secrets key changed) is left out, and the runner
	// falls back to its host's variable.
	if h.ProviderKeyService != nil {
		key, err := h.ProviderKeyService.RevealKey(run.OrgID, keyEnv)
		if err != nil {
			// Nothing derived from the key is logged: only the workspace,
			// whose admin can paste the key again.
			slog.Warn("worker claim: the workspace's stored API key could not be opened (was OPENV_SECRETS_KEY changed?)",
				"org_id", run.OrgID)
		} else if key != "" {
			auth["api_key"] = key
		}
	}
}

// subscriptionTokenFor is the launching member's stored subscription token
// for a run claimed by a runner that is theirs alone, or "".
func (h *Handler) subscriptionTokenFor(r *http.Request, run *agentruns.Run, agent *agents.Agent) string {
	if h.ProviderTokenService == nil || run.LaunchedBy == nil || *run.LaunchedBy == "" {
		return ""
	}
	if WorkerUser(r) != *run.LaunchedBy {
		return ""
	}
	if !h.memberFeatureEnabled(run.OrgID, run.LaunchedBy, release.FeatureSubscriptionToken) {
		return ""
	}
	token, err := h.ProviderTokenService.ForRun(*run.LaunchedBy, agent.Provider)
	if err != nil {
		slog.Warn("api: stored provider token unusable; the run uses the runner's own sign-in",
			"run_id", run.ID, "error", err)
		return ""
	}
	return token
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
	if !decodeJSON(w, r, &req) {
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
		writeJSONError(w, http.StatusBadRequest, invalidRequestBody)
		return
	}
	run, err := h.RunService.AppendLogs(mux.Vars(r)["id"], entries, partialText)
	if err != nil {
		respondInternal(w, r, "failed to append run logs", err)
		return
	}
	writeJSONBare(w, workerproto.LogsResponse{
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
	if !decodeJSON(w, r, &req) {
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
	writeJSONBare(w, run)
}
