package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// requireProjectRole enforces project access. Returns true when the request
// may proceed; otherwise it has already written a 401/403/404 response.
//
// Ladder: platform admins pass everything; org admins of the project's org
// act as owners; members pass when their effective role (direct grant or
// people-team grant, whichever is highest) meets minRole; agent runs pass as
// editor-equivalent inside their own project only; workers pass only for
// projects belonging to their own org.
func (h *Handler) requireProjectRole(w http.ResponseWriter, r *http.Request, projectID string, minRole string) bool {
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
			return true
		}
		writeJSONError(w, http.StatusForbidden, "worker key does not belong to this project's workspace")
		return false
	}

	if run := CurrentRun(r); run != nil {
		if run.ProjectID != nil && *run.ProjectID == projectID && members.RoleAtLeast(members.RoleEditor, minRole) {
			return true
		}
		writeJSONError(w, http.StatusForbidden, "agent run is not scoped to this project")
		return false
	}

	user := CurrentUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return false
	}
	if user.IsAdmin {
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

// requireOrgRole enforces workspace access: platform admins pass; org admins
// satisfy any minRole; members satisfy "member". Writes 401/403 on failure.
func (h *Handler) requireOrgRole(w http.ResponseWriter, r *http.Request, orgID string, minRole string) bool {
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

// requireTeamWrite enforces crew mutations: project-pinned crews need project
// editor rights, workspace-wide crews need workspace admin rights.
func (h *Handler) requireTeamWrite(w http.ResponseWriter, r *http.Request, team *teams.Team) bool {
	if team.ProjectID != nil && *team.ProjectID != "" {
		return h.requireProjectRole(w, r, *team.ProjectID, members.RoleEditor)
	}
	return h.requireOrgRole(w, r, team.OrgID, orgs.RoleAdmin)
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
		if errors.Is(err, proposals.ErrUnsupportedOp) || errors.Is(err, proposals.ErrRunWriteCap) {
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
