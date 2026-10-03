package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/seeds"
)

// registerAgentRunLaunchRoutes wires launching a run, from an agent or as a
// project's test-case draft, and listing runs. A run's own {id} routes come
// from registerAgentRunReadRoutes and registerAgentRunControlRoutes, which
// registerAgentRoutes interleaves with the worker protocol's registrars in
// the order route_handlers.txt pins.
func (h *Handler) registerAgentRunLaunchRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agents/{slug}/runs", h.LaunchAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/draft-test-cases", h.DraftTestCases).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs", h.ListAgentRuns).Methods("GET")
}

// registerAgentRunReadRoutes wires reading a run, its tree and its logs.
func (h *Handler) registerAgentRunReadRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-runs/{id}", h.GetAgentRun).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/tree", h.GetAgentRunTree).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/logs", h.GetAgentRunLogs).Methods("GET")
}

// registerAgentRunControlRoutes wires following a run's stream, cancelling
// it (alwaysWritable) and retrying it.
func (h *Handler) registerAgentRunControlRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/agent-runs/{id}/stream", h.StreamAgentRun).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/cancel", h.alwaysWritable(h.CancelAgentRun)).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/retry", h.RetryAgentRun).Methods("POST")
}

// launchParent is the parent of a run a request launches: the agent run
// whose token sent it, recorded as a delegation records its parent
// (ParentRunID), so the launching run's tree shows the run it set going; nil
// for a person or a runner key.
func launchParent(r *http.Request) *string {
	if run := CurrentRun(r); run != nil {
		id := run.ID
		return &id
	}
	return nil
}

// launchRun enqueues a run a request asked for, with its launchParent.
func (h *Handler) launchRun(r *http.Request, launch agentruns.LaunchRequest) (*agentruns.Run, error) {
	if launch.ParentRunID == nil {
		launch.ParentRunID = launchParent(r)
	}
	run, _, err := h.RunService.Launch(launch)
	return run, err
}

// LaunchAgentRun starts a manual run for an agent (by slug).
func (h *Handler) LaunchAgentRun(w http.ResponseWriter, r *http.Request) {
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
	agent, err := h.AgentService.GetBySlug(ActiveOrg(r), mux.Vars(r)["slug"])
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
	// A launch in a project takes its editor; one with none runs in the
	// active workspace, where the agent was found (requireUnscopedLaunch).
	if req.ProjectID != "" {
		if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
			return
		}
	} else if !h.requireUnscopedLaunch(w, r, ActiveOrg(r)) {
		return
	}
	// The run belongs to the project's org when project-scoped, else to the
	// caller's active workspace.
	orgID := ActiveOrg(r)
	if req.ProjectID != "" {
		if project, err := h.ProjectService.GetProject(req.ProjectID); err == nil && project != nil && project.OrgID != "" {
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
	run, err := h.launchRun(r, launch)
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

// DraftTestCases launches the seeded test-case-author agent scoped to a set of
// requirement artifacts in a project. It is the "Draft test cases" action: the
// caller passes the requirement IDs to cover, and the requirement content is
// NOT inlined — per the lean-context rule the agent fetches each requirement
// through its OpenV tools at run time. The IDs are conveyed in the launch
// prompt the handler builds. The agent runs in proposal mode, so its drafted
// test-case artifacts and verifies links flow through the normal proposal
// review path before they land.
//
// A proposal-mode run is refused, as a status change is: the draft launches a
// run and puts its card on the board, which no proposal can carry, so a
// review-gated agent could otherwise set work going that no person asked for
// (REQ-21, REQ-75).
func (h *Handler) DraftTestCases(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if _, proposalRun := h.proposalRunID(r); proposalRun {
		writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot draft test cases")
		return
	}
	var req struct {
		RequirementIDs []string `json:"requirement_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Normalize: drop blanks/dupes so a sloppy client can't launch a run with
	// an empty or padded ID list.
	ids := make([]string, 0, len(req.RequirementIDs))
	seen := map[string]bool{}
	for _, id := range req.RequirementIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		// Reject a malformed id before it reaches the launch prompt (issue #245):
		// the requirement ids are interpolated verbatim into the agent prompt, so
		// a non-UUID would send the agent chasing a bogus artifact instead of
		// failing fast.
		if _, err := uuid.Parse(id); err != nil {
			writeJSONError(w, http.StatusBadRequest, "requirement_ids must be valid artifact ids")
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeJSONError(w, http.StatusBadRequest, "requirement_ids is required")
		return
	}

	// The run belongs to the project's org.
	orgID := ActiveOrg(r)
	if project, err := h.ProjectService.GetProject(projectID); err == nil && project != nil && project.OrgID != "" {
		orgID = project.OrgID
	}

	agent, err := h.AgentService.GetBySlug(orgID, seeds.TestCaseAuthorSlug)
	if err != nil {
		respondInternal(w, r, "failed to load the test-case author agent", err)
		return
	}
	if agent == nil {
		writeJSONError(w, http.StatusNotFound, "the test-case author agent is not available in this workspace; sync agents from disk to seed it")
		return
	}

	prompt := fmt.Sprintf(
		"Draft verification test cases for the following requirement artifacts in project %s: %s.\n\n"+
			"For each requirement: fetch it with get_artifact, draft one or more test-case artifacts (preconditions, numbered steps, expected result tied to its fit criterion) via create_artifact, then create a verifies link from each new test case to the requirement via create_link. Everything you create is a proposal for human review.",
		projectID, strings.Join(ids, ", "))

	launch := agentruns.LaunchRequest{
		OrgID:      orgID,
		AgentID:    agent.ID,
		ProjectID:  &projectID,
		Prompt:     prompt,
		LaunchedBy: CurrentUserID(r),
	}
	run, err := h.launchRun(r, launch)
	if err != nil {
		// Mirror LaunchAgentRun: an over-budget soft-block is a distinct 402.
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
	// Scope the listing to a workspace in SQL (so a busy sibling workspace
	// can never starve the page before LIMIT applies): the named project's,
	// whose runs the guard just let the caller read, whatever workspace it
	// acts in, else the active one; without a project filter, non-admin
	// members only see the runs they launched themselves.
	activeOrg := ActiveOrg(r)
	filter := agentruns.ListFilter{
		OrgID:     h.listOrg(r, projectID),
		AgentID:   q.Get("agent_id"),
		ProjectID: projectID,
		Status:    q.Get("status"),
		ParentID:  q.Get("parent_id"),
		Limit:     limit,
	}
	if projectID == "" && !h.isOrgAdmin(r, activeOrg) {
		filter.LaunchedBy = CurrentUser(r).ID
	}
	runs, err := h.RunService.List(filter)
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
	run, err := h.RunService.Get(mux.Vars(r)["id"])
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
	run, err := h.RunService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	tree, err := h.RunService.Tree(run.ID)
	if err != nil {
		respondInternal(w, r, "failed to load run tree", err)
		return
	}
	// A child may sit outside the root's scope (readableRunTree).
	json.NewEncoder(w).Encode(h.readableRunTree(r, tree))
}

func (h *Handler) GetAgentRunLogs(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	run, err := h.RunService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	afterSeq, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
	logs, err := h.RunService.Logs(run.ID, afterSeq)
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
	run, err := h.RunService.Get(runID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleViewer) {
		return
	}
	afterSeq, _ := strconv.Atoi(r.URL.Query().Get("after_seq"))
	h.SSEHub.ServeStream(w, r, runID, func(emit func(event string, data interface{})) error {
		// Logs is paged (bounded per call), so drain every page before the live
		// tail takes over: keep advancing the cursor to the last seq seen until
		// a page comes back empty. seq strictly increases, so this terminates.
		cursor := afterSeq
		for {
			logs, err := h.RunService.Logs(runID, cursor)
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
	run, err := h.RunService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleEditor) {
		return
	}
	cancelled, err := h.RunService.RequestCancel(run.ID)
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
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
	if !requireUser(w, r) {
		return
	}
	run, err := h.RunService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "agent run not found", err)
		return
	}
	if !h.requireRunAccess(w, r, run, members.RoleEditor) {
		return
	}
	retried, err := h.RunService.Retry(run.ID, CurrentUserID(r))
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
