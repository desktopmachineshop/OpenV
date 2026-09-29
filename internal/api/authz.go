package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// requireProjectRole enforces project access. Returns true when the request
// may proceed; otherwise it has already written a 401/403/404 response.
//
// Ladder: platform admins pass everything in a project that exists; org
// admins of the project's org act as owners; members pass when their
// effective role (direct grant or people-team grant, whichever is highest)
// meets minRole; agent runs pass as editor-equivalent inside their own
// project only; workers pass only for projects belonging to their own org, a
// workspace key as an editor there and a member's personal runner key above a
// viewer's read only where its holder would (personalKeyAccess).
func (h *Handler) requireProjectRole(w http.ResponseWriter, r *http.Request, projectID string, minRole string) bool {
	if !h.projectAccess(w, r, projectID, minRole) {
		return false
	}
	// Access decided, one more question for a write: is the workspace
	// over its plan? A read-only workspace refuses every write but the
	// ones that bring it back under plan (limits.go, requireWritable).
	if mutating(r.Method) {
		return h.requireWritable(w, r, h.orgIDForProject(projectID))
	}
	return true
}

func (h *Handler) projectAccess(w http.ResponseWriter, r *http.Request, projectID string, minRole string) bool {
	if projectID == "" {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return false
	}

	if workerOrg := WorkerOrg(r); workerOrg != "" {
		project, err := h.projectService.GetProject(projectID)
		if err != nil || project == nil {
			writeJSONError(w, http.StatusNotFound, "project not found")
			return false
		}
		if project.OrgID == workerOrg {
			return h.personalKeyAccess(w, r, project.OrgID, projectID, minRole)
		}
		writeJSONError(w, http.StatusForbidden, "worker key does not belong to this project's workspace")
		return false
	}

	if run := CurrentRun(r); run != nil {
		if run.ProjectID == nil || *run.ProjectID != projectID {
			writeJSONError(w, http.StatusForbidden, "agent run is not scoped to this project")
			return false
		}
		// In its own project a run is an editor, never an owner.
		if !members.RoleAtLeast(members.RoleEditor, minRole) {
			writeJSONError(w, http.StatusForbidden, "agent runs act at most as a project editor")
			return false
		}
		return true
	}

	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	if user.IsAdmin {
		// A platform admin needs no role, but the project must exist, as it
		// must for a worker key above: past the guard a handler reads,
		// writes or deletes it, and one no row has would answer 500, 204
		// for nothing, or 201 with rows no project owns.
		if h.projectService != nil {
			if project, err := h.projectService.GetProject(projectID); err != nil || project == nil {
				writeJSONError(w, http.StatusNotFound, "project not found")
				return false
			}
		}
		return true
	}

	// Org admins of the project's org act as owners.
	if h.orgService != nil {
		if project, err := h.projectService.GetProject(projectID); err == nil && project != nil && project.OrgID != "" {
			if role, err := h.orgService.RoleInOrg(project.OrgID, user.ID); err == nil && role == orgs.RoleAdmin {
				return true
			}
		}
	}

	role, err := h.memberService.EffectiveRole(projectID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to resolve project access", err)
		return false
	}
	if role == "" || !members.RoleAtLeast(role, minRole) {
		writeJSONError(w, http.StatusForbidden, "you do not have access to this project")
		return false
	}
	return true
}

// personalKeyAccess decides a worker key of the project's own workspace. A
// workspace key (no holder) carries workspace-wide editor rights (OpenV
// REQ-42): it passes up to an editor's guard and is refused an owner's with
// the 403 a project editor gets. A member's personal runner key (a session
// key among them) is its holder acting, so anything above a viewer's read
// needs what the holder's own session would: admin of the workspace, or an
// effective project role that meets minRole (REQ-16, REQ-79). A viewer's read
// still passes for any project of the workspace, since the runner reads a
// claimed ownerless run's repository connections with the key.
func (h *Handler) personalKeyAccess(w http.ResponseWriter, r *http.Request, orgID, projectID, minRole string) bool {
	holder := WorkerUser(r)
	if holder == "" {
		if members.RoleAtLeast(members.RoleEditor, minRole) {
			return true
		}
		writeJSONError(w, http.StatusForbidden, "you do not have access to this project")
		return false
	}
	if members.RoleAtLeast(members.RoleViewer, minRole) {
		return true
	}
	if h.orgService != nil {
		if role, err := h.orgService.RoleInOrg(orgID, holder); err == nil && role == orgs.RoleAdmin {
			return true
		}
	}
	role, err := h.memberService.EffectiveRole(projectID, holder)
	if err != nil {
		respondInternal(w, r, "failed to resolve project access", err)
		return false
	}
	if role == "" || !members.RoleAtLeast(role, minRole) {
		writeJSONError(w, http.StatusForbidden, "you do not have access to this project")
		return false
	}
	return true
}

// requireOrgRole enforces workspace access: platform admins pass for a
// workspace that exists; org admins satisfy any minRole; members satisfy
// "member". Writes 401/403/404 on failure.
func (h *Handler) requireOrgRole(w http.ResponseWriter, r *http.Request, orgID string, minRole string) bool {
	if !h.orgAccess(w, r, orgID, minRole) {
		return false
	}
	return h.requireWritable(w, r, orgID)
}

func (h *Handler) orgAccess(w http.ResponseWriter, r *http.Request, orgID string, minRole string) bool {
	if orgID == "" {
		writeJSONError(w, http.StatusNotFound, "workspace not found")
		return false
	}
	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	if user.IsAdmin {
		// A platform admin needs no membership, but the workspace must
		// exist (a deleted one still does, until it is purged): past the
		// guard a handler reads or writes it, and one no row has would
		// answer 500, 204 for nothing, or a foreign key's refusal.
		if h.orgService != nil {
			if _, err := h.orgService.Get(orgID); err != nil {
				respondError(w, r, http.StatusNotFound, "workspace not found", err)
				return false
			}
		}
		return true
	}
	role, err := h.orgService.RoleInOrg(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to resolve workspace access", err)
		return false
	}
	if role == "" {
		writeJSONError(w, http.StatusForbidden, "you are not a member of this workspace")
		return false
	}
	if minRole == orgs.RoleAdmin && role != orgs.RoleAdmin {
		writeJSONError(w, http.StatusForbidden, "workspace admin access required")
		return false
	}
	return true
}

// discardResponse swallows the error responses the require* helpers write,
// for call sites that need the access decision without answering the request.
type discardResponse struct{}

func (discardResponse) Header() http.Header         { return http.Header{} }
func (discardResponse) Write(b []byte) (int, error) { return len(b), nil }
func (discardResponse) WriteHeader(int)             {}

// hasProjectRole reports whether the request would pass requireProjectRole.
// Unlike the require* helpers it writes no response.
func (h *Handler) hasProjectRole(r *http.Request, projectID, minRole string) bool {
	return h.requireProjectRole(discardResponse{}, r, projectID, minRole)
}

// isOrgAdmin reports whether the current user is a platform admin or an
// admin of the given org. Unlike the require* helpers it writes no response.
func (h *Handler) isOrgAdmin(r *http.Request, orgID string) bool {
	user := CurrentUser(r)
	if user == nil {
		return false
	}
	if user.IsAdmin {
		return true
	}
	if orgID == "" || h.orgService == nil {
		return false
	}
	role, err := h.orgService.RoleInOrg(orgID, user.ID)
	return err == nil && role == orgs.RoleAdmin
}

// requireRunAccess enforces access to an agent run: the user who launched it
// always passes; project-scoped runs fall back to the project role ladder;
// unscoped runs require workspace-admin rights on the run's org. Writes the
// error response itself on failure.
func (h *Handler) requireRunAccess(w http.ResponseWriter, r *http.Request, run *agentruns.Run, minRole string) bool {
	if user := CurrentUser(r); user != nil && run.LaunchedBy != nil && *run.LaunchedBy == user.ID {
		return true
	}
	if run.ProjectID != nil && *run.ProjectID != "" {
		return h.requireProjectRole(w, r, *run.ProjectID, minRole)
	}
	return h.requireOrgRole(w, r, run.OrgID, orgs.RoleAdmin)
}

// hasRunAccess reports whether the request would pass requireRunAccess.
// Unlike the require* helpers it writes no response.
func (h *Handler) hasRunAccess(r *http.Request, run *agentruns.Run, minRole string) bool {
	return h.requireRunAccess(discardResponse{}, r, run, minRole)
}

// readableRunTree is the part of a run tree (root first, each run after its
// parent) its reader may see: the root, whose access the caller passed, and
// each run below it that the reader could open by itself (hasRunAccess,
// as a viewer) under a parent kept too. A run launched by another run's
// token (launchParent) can sit outside its parent's scope, as an unscoped
// run a project's run launched does, which the workspace's admins alone may
// read; pruning its subtree keeps its id out of its children's parent too.
// The decision depends on a run's launcher, project and workspace alone, so
// it is asked once for each.
func (h *Handler) readableRunTree(r *http.Request, tree []*agentruns.Run) []*agentruns.Run {
	if len(tree) == 0 {
		return tree
	}
	kept := []*agentruns.Run{tree[0]}
	shown := map[string]bool{tree[0].ID: true}
	decided := map[[3]string]bool{}
	for _, run := range tree[1:] {
		if run.ParentRunID == nil || !shown[*run.ParentRunID] {
			continue
		}
		key := [3]string{"", "", run.OrgID}
		if run.LaunchedBy != nil {
			key[0] = *run.LaunchedBy
		}
		if run.ProjectID != nil {
			key[1] = *run.ProjectID
		}
		readable, known := decided[key]
		if !known {
			readable = h.hasRunAccess(r, run, members.RoleViewer)
			decided[key] = readable
		}
		if readable {
			kept = append(kept, run)
			shown[run.ID] = true
		}
	}
	return kept
}

// requireTeamWrite enforces crew mutations: project-pinned crews need project
// editor rights, workspace-wide crews need workspace admin rights.
func (h *Handler) requireTeamWrite(w http.ResponseWriter, r *http.Request, team *teams.Team) bool {
	if pin := h.teamPin(team); pin != "" {
		return h.requireProjectRole(w, r, pin, members.RoleEditor)
	}
	return h.requireOrgRole(w, r, team.OrgID, orgs.RoleAdmin)
}

// teamPin is the crew's pinned project while the pin still names a project of
// the crew's own workspace. A pin to a deleted project, or to another
// workspace's, counts as no pin, so the crew stays its workspace admins'.
func (h *Handler) teamPin(team *teams.Team) string {
	if team.ProjectID == nil || *team.ProjectID == "" {
		return ""
	}
	project, err := h.projectService.GetProject(*team.ProjectID)
	if err != nil || project == nil || (team.OrgID != "" && project.OrgID != team.OrgID) {
		return ""
	}
	return *team.ProjectID
}

// requireAutomationWrite enforces automation mutations: project-pinned
// automations need project editor rights, workspace-wide ones need workspace
// admin rights on the given org.
func (h *Handler) requireAutomationWrite(w http.ResponseWriter, r *http.Request, projectID *string, orgID string) bool {
	if projectID != nil && *projectID != "" {
		return h.requireProjectRole(w, r, *projectID, members.RoleEditor)
	}
	return h.requireOrgRole(w, r, orgID, orgs.RoleAdmin)
}

// projectIDForArtifact resolves an artifact id to its project id ("" on failure).
func (h *Handler) projectIDForArtifact(artifactID string) string {
	artifact, err := h.artifactService.GetArtifact(artifactID)
	if err != nil || artifact == nil {
		return ""
	}
	return artifact.ProjectID
}

// maybePropose diverts a write into the proposal queue when the request comes
// from a proposal-mode agent run. Returns true when the response has been
// written (either a proposal receipt or an error) and the handler must stop.
func (h *Handler) maybePropose(w http.ResponseWriter, r *http.Request, projectID, op string, targetID *string, reqBody interface{}) bool {
	run := CurrentRun(r)
	if run == nil {
		return false
	}
	agent, err := h.agentService.Get(run.AgentID)
	if err != nil || agent == nil {
		respondInternal(w, r, "agent not found for run", err)
		return true
	}
	if agent.WriteMode != "proposal" {
		return false
	}

	payload := map[string]interface{}{}
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err == nil {
			_ = json.Unmarshal(raw, &payload)
		}
	}
	proposal, err := h.proposalService.Propose(run.ID, projectID, op, targetID, payload)
	if err != nil {
		if errors.Is(err, proposals.ErrUnsupportedOp) || errors.Is(err, proposals.ErrRunWriteCap) || errors.Is(err, proposals.ErrDuplicateRef) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to record proposal", err)
		}
		return true
	}
	h.publish(r, events.ProposalCreated, projectID, proposal.ID, map[string]interface{}{
		"op":     op,
		"run_id": run.ID,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"proposed":    true,
		"proposal_id": proposal.ID,
		"note":        "This write is pending human review and has not been applied yet.",
	})
	return true
}

// proposalRunID reports the current run's id and whether its writes are
// diverted to the proposal queue (a proposal-mode agent). It mirrors the run +
// agent lookup maybePropose performs, exposed so the link handler can tell an
// unknown-artifact endpoint apart from a legitimate pending-proposal reference
// token (issue #235) before it decides whether the endpoint is an error.
func (h *Handler) proposalRunID(r *http.Request) (string, bool) {
	run := CurrentRun(r)
	if run == nil {
		return "", false
	}
	agent, err := h.agentService.Get(run.AgentID)
	if err != nil || agent == nil || agent.WriteMode != agents.WriteModeProposal {
		return "", false
	}
	return run.ID, true
}

// requireNoProposalRunLaunch refuses a proposal-mode run every route that
// sets a run going (a launch, a crew's, a test run's agent, a retry, an
// automation's run-now, an assistant turn) or arms one (an interview, which
// names the interviewer, and its invite, whose participant messages each
// launch that interviewer's run): no proposal can carry a launch, so a
// review-gated agent would otherwise start work whose writes land with no
// person reviewing them (REQ-21, REQ-75). It asks first, before any
// lookup, since the answer depends on the caller alone; a run whose agent
// cannot be read is refused too, as maybePropose refuses its writes. The
// draft of test cases refuses it in its own words (DraftTestCases).
func (h *Handler) requireNoProposalRunLaunch(w http.ResponseWriter, r *http.Request) bool {
	run := CurrentRun(r)
	if run == nil {
		return true
	}
	agent, err := h.agentService.Get(run.AgentID)
	if err != nil || agent == nil {
		respondInternal(w, r, "agent not found for run", err)
		return false
	}
	if agent.WriteMode == agents.WriteModeProposal {
		writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot launch agent runs")
		return false
	}
	return true
}

// requireUnscopedLaunch guards a launch that names no project, which runs in
// the workspace its agent was found in (orgID): a person must be a member
// there (requireOrgRole, with its plan gate), while a worker key or a run
// token, which the middleware already keeps to its own workspace, launches
// there as before, past the same read-only gate (REQ-176).
func (h *Handler) requireUnscopedLaunch(w http.ResponseWriter, r *http.Request, orgID string) bool {
	if CurrentUser(r) != nil {
		return h.requireOrgRole(w, r, orgID, orgs.RoleMember)
	}
	return h.requireWritable(w, r, orgID)
}

// requireProjectCreate decides every project create (POST /projects, a
// template's project, an import) and returns the workspace it lands in, the
// one the caller acts in. A person creates it and owns it: an agent run acts
// only inside its own project (REQ-42), and a runner key, workspace or
// personal, has no person to own what it would make, so both are refused
// before the body is read. Then, before anything is created, the
// workspace's read-only gate (which import's alwaysWritable passes, REQ-177)
// and the project maximum (REQ-176).
func (h *Handler) requireProjectCreate(w http.ResponseWriter, r *http.Request) (string, bool) {
	if CurrentRun(r) != nil {
		writeJSONError(w, http.StatusForbidden, "agent runs cannot create projects")
		return "", false
	}
	if IsWorker(r) {
		writeJSONError(w, http.StatusForbidden, "runner keys cannot create projects")
		return "", false
	}
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	orgID := ActiveOrg(r)
	if orgID == "" {
		writeJSONError(w, http.StatusBadRequest, "no active workspace for this request")
		return "", false
	}
	if !h.requireWritable(w, r, orgID) {
		return "", false
	}
	if err := h.checkProjectCount(orgID); err != nil {
		if !h.writeLimitError(w, err) {
			respondInternal(w, r, "failed to check the project limit", err)
		}
		return "", false
	}
	return orgID, true
}

// pendingArtifactRef returns the pending create_artifact proposal in runID that
// minted the temporary token ref, or nil. It is how a create_link proposal
// validates that a ref-shaped endpoint really points at a sibling artifact
// proposal in the same run rather than a typo (issue #235).
func (h *Handler) pendingArtifactRef(runID, ref string) *proposals.Proposal {
	if runID == "" || ref == "" {
		return nil
	}
	list, err := h.proposalService.List("", "", "", runID)
	if err != nil {
		return nil
	}
	for _, p := range list {
		if p.Ref == ref && p.Op == proposals.OpCreateArtifact && p.Status == proposals.StatusPending {
			return p
		}
	}
	return nil
}
