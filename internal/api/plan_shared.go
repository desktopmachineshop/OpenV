package api

// What the plan's handler files share: the error mapping, the guards on a
// project's plan and on a task, and the reads that answer a plan and one
// of its rows.

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/plan"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/todos"
)

// writePlanError answers a plan write's domain error.
func writePlanError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, plan.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "milestone not found")
	case errors.Is(err, plan.ErrTaskNotFound):
		writeJSONError(w, http.StatusNotFound, "work item not found")
	case errors.Is(err, plan.ErrNameRequired), errors.Is(err, plan.ErrInvalidDate), errors.Is(err, plan.ErrDateOrder),
		errors.Is(err, plan.ErrParentSelf), errors.Is(err, plan.ErrParentCycle), errors.Is(err, plan.ErrOtherProject),
		errors.Is(err, plan.ErrDependsOnSelf), errors.Is(err, plan.ErrDependencyCycle), errors.Is(err, plan.ErrRelationshipType), errors.Is(err, plan.ErrInvalidDuration), errors.Is(err, plan.ErrInvalidEffort),
		errors.Is(err, plan.ErrWorkingDays), errors.Is(err, plan.ErrParentDates), errors.Is(err, plan.ErrMilestone), errors.Is(err, plan.ErrInvalidLag), errors.Is(err, plan.ErrHoliday), errors.Is(err, plan.ErrCriticalSlack), errors.Is(err, plan.ErrSchedulingMode), errors.Is(err, plan.ErrAssignment), errors.Is(err, plan.ErrTooManyBaselines), errors.Is(err, plan.ErrPercent), errors.Is(err, plan.ErrPosition), errors.Is(err, plan.ErrImport), errors.Is(err, plan.ErrSameProject), errors.Is(err, plan.ErrTimeOff), errors.Is(err, plan.ErrResource), errors.Is(err, plan.ErrInvalidCost), errors.Is(err, plan.ErrConstraint), errors.Is(err, plan.ErrUnknownMetric),
		errors.Is(err, plan.ErrInvalidTarget), errors.Is(err, plan.ErrTooManyCriteria), errors.Is(err, plan.ErrDuplicateMetric),
		errors.Is(err, plan.ErrReleaseMilestone), errors.Is(err, plan.ErrUnsupportedFormat),
		errors.Is(err, todos.ErrTextRequired), errors.Is(err, todos.ErrTextTooLong), errors.Is(err, todos.ErrParagraphs):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		respondInternal(w, r, "failed to save the plan", err)
	}
}

// planReady guards a plan route under a project: the service is wired and
// the caller holds role on the project. A write also needs the feature on
// for the project's workspace.
func (h *Handler) planReady(w http.ResponseWriter, r *http.Request, projectID, role string, write bool) bool {
	if h.PlanService == nil {
		writeJSONError(w, http.StatusNotFound, "the plan is not available")
		return false
	}
	if !h.requireProjectRole(w, r, projectID, role) {
		return false
	}
	if write && !h.projectFeatureEnabled(r, projectID, release.FeatureGanttPlan) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return false
	}
	return true
}

// loadPlan computes the project's plan from the board and the evidence the
// V&V reads answer: the live artifacts and links, and the coverage with the
// flow-down from child projects.
func (h *Handler) loadPlan(w http.ResponseWriter, r *http.Request, projectID string) (*plan.Plan, bool) {
	export, err := h.projectExport(projectID, "")
	if err != nil {
		respondInternal(w, r, "failed to export project", err)
		return nil, false
	}
	latest, err := h.VVService.LatestResults(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load latest test results", err)
		return nil, false
	}
	statuses := make(map[string]string, len(latest))
	for id, res := range latest {
		if res != nil {
			statuses[id] = res.Status
		}
	}
	p, err := h.PlanService.Plan(projectID, plan.Evidence{
		Artifacts: export.Artifacts,
		Links:     export.Links,
		Coverage:  h.flowDownCoverage(export, latest),
		Latest:    statuses,
	}, time.Now().UTC())
	if err != nil {
		respondInternal(w, r, "failed to load the plan", err)
		return nil, false
	}
	return p, true
}

// planTask guards a plan write on one task: the work item exists, the
// caller edits its project (a caller with no access answered as for an id
// no row has, I3), and the feature is on for its workspace.
func (h *Handler) planTask(w http.ResponseWriter, r *http.Request) (projectID string, ok bool) {
	if h.PlanService == nil {
		writeJSONError(w, http.StatusNotFound, "the plan is not available")
		return "", false
	}
	item, err := h.WorkItemService.Get(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return "", false
	}
	if !h.requireProjectRoleFor(w, r, item.ProjectID, members.RoleEditor, missing("work item not found")) {
		return "", false
	}
	if !h.projectFeatureEnabled(r, item.ProjectID, release.FeatureGanttPlan) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return "", false
	}
	return item.ProjectID, true
}

// planTaskRow answers the plan's row for a task after a write to it.
func (h *Handler) planTaskRow(w http.ResponseWriter, r *http.Request, projectID, id string) {
	p, ok := h.loadPlan(w, r, projectID)
	if !ok {
		return
	}
	for _, t := range p.Tasks {
		if t.ID == id {
			respondJSON(w, http.StatusOK, t)
			return
		}
	}
	writeJSONError(w, http.StatusNotFound, "work item not found")
}

// workspaceAdmin reports whether the caller administers the workspace: a
// platform admin, an admin of it, or a workspace key acting for nobody. An
// agent's run never does.
func (h *Handler) workspaceAdmin(r *http.Request, orgID string) bool {
	if CurrentRun(r) != nil {
		return false
	}
	user := CurrentUser(r)
	if user == nil {
		return WorkerUser(r) == ""
	}
	if user.IsAdmin {
		return true
	}
	role, err := h.OrgService.RoleInOrg(orgID, user.ID)
	return err == nil && role == orgs.RoleAdmin
}
