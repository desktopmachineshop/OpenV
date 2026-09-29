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

// notFound is the answer to an id no row has: its status and message. The
// guards give exactly this answer to a caller with no access at all, so that
// a refusal tells nothing of whether the id exists (I3, OpenV REQ-17): a
// project no row has and one the caller cannot reach both answer the project
// guard's 404 "project not found", and a workspace likewise "workspace not
// found". A guard that stands for a resource the handler looked up by its
// own id answers that resource's own not-found (missing), the answer the
// lookup gives an id no row has. A caller who reaches the project or
// workspace but lacks the role a write needs still gets 403: the resource
// exists for it. The zero notFound stands for the guard's own answer.
type notFound struct {
	status  int
	message string
}

var (
	unknownProject   = notFound{http.StatusNotFound, "project not found"}
	unknownWorkspace = notFound{http.StatusNotFound, "workspace not found"}
)

// missing is a resource's not-found answer: 404 with its message.
func missing(message string) notFound { return notFound{http.StatusNotFound, message} }

// or is n, or def when n is the zero notFound.
func (n notFound) or(def notFound) notFound {
	if n == (notFound{}) {
		return def
	}
	return n
}

func (n notFound) write(w http.ResponseWriter) { writeJSONError(w, n.status, n.message) }

// requireProjectRole enforces project access. Returns true when the request
// may proceed; otherwise it has already written a 401/403/404 response.
//
// Ladder: platform admins pass everything in a project that exists; org
// admins of the project's org act as owners; members pass when their
// effective role (direct grant or people-team grant, whichever is highest)
// meets minRole; agent runs pass as editor-equivalent inside their own
// project only; workers pass only for projects belonging to their own org, a
// workspace key as an editor there and a member's personal runner key above a
// viewer's read only where its holder would (personalKeyAccess). A caller
// with no access at all to the project gets 404 "project not found", as for
// a project no row has (notFound).
func (h *Handler) requireProjectRole(w http.ResponseWriter, r *http.Request, projectID string, minRole string) bool {
	return h.requireProjectRoleFor(w, r, projectID, minRole, notFound{})
}

// requireProjectRoleFor is requireProjectRole for a resource the handler
// looked up by its own id: a caller with no access at all to the resource's
// project gets absent, the answer the handler gives an id no row has.
func (h *Handler) requireProjectRoleFor(w http.ResponseWriter, r *http.Request, projectID string, minRole string, absent notFound) bool {
	if !h.projectAccess(w, r, projectID, minRole, absent) {
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

func (h *Handler) projectAccess(w http.ResponseWriter, r *http.Request, projectID string, minRole string, absent notFound) bool {
	absent = absent.or(unknownProject)
	if projectID == "" {
		absent.write(w)
		return false
	}

	if workerOrg := WorkerOrg(r); workerOrg != "" {
		// Another workspace's project is, to a worker key, one no row has.
		project, err := h.projectService.GetProject(projectID)
		if err != nil || project == nil || project.OrgID != workerOrg {
			absent.write(w)
			return false
		}
		return h.personalKeyAccess(w, r, project.OrgID, projectID, minRole)
	}

	if run := CurrentRun(r); run != nil {
		// A run reaches its own project only; any other is one no row has.
		if run.ProjectID == nil || *run.ProjectID != projectID {
			absent.write(w)
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
				absent.write(w)
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
	if role == "" {
		// No role at all: the project, if it exists, is not the caller's
		// to know of.
		absent.write(w)
		return false
	}
	if !members.RoleAtLeast(role, minRole) {
		writeJSONError(w, http.StatusForbidden, "you do not have access to this project")
		return false
	}
	return true
}

// requireProjectVisible is the first question of a guard on a project a
// request names in its body or query and looks up there: does the caller
// reach the project at all, a reader's access at least? If not, it answers
// absent, as for a project no row has (I3), or the 401 or 500 the guard
// would, and reports false. It asks no role beyond that and takes no plan
// gate: the route's own guard does.
func (h *Handler) requireProjectVisible(w http.ResponseWriter, r *http.Request, projectID string, absent notFound) bool {
	return h.projectAccess(w, r, projectID, members.RoleViewer, absent)
}

// requireOrgVisible is requireProjectVisible for a workspace: is the caller
// a member of it, or a platform admin? If not, it answers absent, as for
// what the request names there when no row has it (I3).
func (h *Handler) requireOrgVisible(w http.ResponseWriter, r *http.Request, orgID string, absent notFound) bool {
	return h.orgAccess(w, r, orgID, orgs.RoleMember, absent)
}

// requireTeamVisible reports whether the request may know of the crew: a
// member of its workspace, its workspace's worker keys, its workspace's runs
// (of a pinned crew only a run of the pinned project, the one project a run
// reaches), or a caller who reaches its pinned project. Otherwise it answers
// absent, as for a crew no row has (I3), and reports false. A crew with no
// workspace is anyone's to know of.
func (h *Handler) requireTeamVisible(w http.ResponseWriter, r *http.Request, team *teams.Team, absent notFound) bool {
	switch run := CurrentRun(r); {
	case team.OrgID == "":
		return true
	case WorkerOrg(r) != "":
		if WorkerOrg(r) == team.OrgID {
			return true
		}
	case run != nil:
		if pin := h.teamPin(team); run.OrgID == team.OrgID && (pin == "" || sameProject(run.ProjectID, &pin)) {
			return true
		}
	default:
		if pin := h.teamPin(team); pin != "" && h.reachesProject(r, pin) {
			return true
		}
		if h.orgAccess(discardResponse{}, r, team.OrgID, orgs.RoleMember, notFound{}) {
			return true
		}
	}
	absent.write(w)
	return false
}

// reachesProject reports whether the request has any access to the project,
// a reader's at least, without writing a response: false where a guard would
// answer the caller as for a project no row has.
func (h *Handler) reachesProject(r *http.Request, projectID string) bool {
	return h.requireProjectVisible(discardResponse{}, r, projectID, notFound{})
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
// "member". Writes 401/403/404 on failure: a caller who is no member gets
// 404 "workspace not found", as for a workspace no row has (notFound).
func (h *Handler) requireOrgRole(w http.ResponseWriter, r *http.Request, orgID string, minRole string) bool {
	return h.requireOrgRoleFor(w, r, orgID, minRole, notFound{})
}

// requireOrgRoleFor is requireOrgRole for a resource the handler looked up by
// its own id: a caller who is no member of the resource's workspace gets
// absent, the answer the handler gives an id no row has.
func (h *Handler) requireOrgRoleFor(w http.ResponseWriter, r *http.Request, orgID string, minRole string, absent notFound) bool {
	if !h.orgAccess(w, r, orgID, minRole, absent) {
		return false
	}
	return h.requireWritable(w, r, orgID)
}

func (h *Handler) orgAccess(w http.ResponseWriter, r *http.Request, orgID string, minRole string, absent notFound) bool {
	lookedUp := absent != (notFound{})
	absent = absent.or(unknownWorkspace)
	if orgID == "" {
		absent.write(w)
		return false
	}
	user := CurrentUser(r)
	if user == nil {
		// A caller with no session (a worker key, a run token) gets the
		// guard's 401; but where the handler looked the resource up first,
		// one that is not of the resource's workspace gets the lookup's
		// answer to an id no row has, or the 401 would tell it the resource
		// exists (I3).
		if lookedUp && sessionlessOrg(r) != orgID {
			absent.write(w)
			return false
		}
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
				absent.write(w)
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
		// No member: the workspace, if it exists, is not the caller's to
		// know of.
		absent.write(w)
		return false
	}
	if minRole == orgs.RoleAdmin && role != orgs.RoleAdmin {
		writeJSONError(w, http.StatusForbidden, "workspace admin access required")
		return false
	}
	return true
}

// sessionlessOrg is the workspace of a caller with no session: a worker
// key's, or a run token's ("" for neither).
func sessionlessOrg(r *http.Request) string {
	if org := WorkerOrg(r); org != "" {
		return org
	}
	if run := CurrentRun(r); run != nil {
		return run.OrgID
	}
	return ""
}

// sameProject reports whether two runs' projects are one, or both runs have
// none.
func sameProject(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
// unscoped runs require workspace-admin rights on the run's org. A caller
// with no access at all gets the 404 of a run no row has. Writes the error
// response itself on failure.
func (h *Handler) requireRunAccess(w http.ResponseWriter, r *http.Request, run *agentruns.Run, minRole string) bool {
	if user := CurrentUser(r); user != nil && run.LaunchedBy != nil && *run.LaunchedBy == user.ID {
		return true
	}
	absent := missing("agent run not found")
	if run.ProjectID != nil && *run.ProjectID != "" {
		return h.requireProjectRoleFor(w, r, *run.ProjectID, minRole, absent)
	}
	return h.requireOrgRoleFor(w, r, run.OrgID, orgs.RoleAdmin, absent)
}

// requireTeamWrite enforces crew mutations: project-pinned crews need project
// editor rights, workspace-wide crews need workspace admin rights. A caller
// with no access at all gets absent, the answer to an id no row has of the
// crew, node or edge the handler looked up.
func (h *Handler) requireTeamWrite(w http.ResponseWriter, r *http.Request, team *teams.Team, absent notFound) bool {
	if pin := h.teamPin(team); pin != "" {
		return h.requireProjectRoleFor(w, r, pin, members.RoleEditor, absent)
	}
	return h.requireOrgRoleFor(w, r, team.OrgID, orgs.RoleAdmin, absent)
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
// admin rights on the given org. A caller with no access at all gets absent:
// a stored automation's own 404, or, for a create, the guards' own (the zero
// notFound).
func (h *Handler) requireAutomationWrite(w http.ResponseWriter, r *http.Request, projectID *string, orgID string, absent notFound) bool {
	if projectID != nil && *projectID != "" {
		return h.requireProjectRoleFor(w, r, *projectID, members.RoleEditor, absent)
	}
	return h.requireOrgRoleFor(w, r, orgID, orgs.RoleAdmin, absent)
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
