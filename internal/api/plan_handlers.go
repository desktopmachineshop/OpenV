package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/plan"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/targetreleases"
	"github.com/openv/requirements-platform/internal/domain/todos"
)

// registerPlanRoutes wires a project's plan (internal/domain/plan): the
// Gantt over the board with its milestones, a task's schedule and to-dos,
// the milestones' edits, and the plan's export.
// Its call is the last in RegisterRoutes (routes.go): gorilla/mux serves
// the first registered route that matches, so the call's position decides
// which of two overlapping templates answers a path (I2).
func (h *Handler) registerPlanRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/plan", h.GetPlan).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/plan/export", h.ExportPlan).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/plan/milestones", h.CreatePlanMilestone).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/plan/milestones/{milestoneId}", h.UpdatePlanMilestone).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/plan/milestones/{milestoneId}", h.DeletePlanMilestone).Methods("DELETE")
	router.HandleFunc("/api/v1/projects/{id}/plan/release-milestones/{releaseId}", h.SetReleaseMilestoneCriteria).Methods("PUT")
	router.HandleFunc("/api/v1/work-items/{id}/schedule", h.ScheduleWorkItem).Methods("PUT")
	router.HandleFunc("/api/v1/work-items/{id}/todos", h.AddWorkItemTodo).Methods("POST")
}

// writePlanError answers a plan write's domain error.
func writePlanError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, plan.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "milestone not found")
	case errors.Is(err, plan.ErrTaskNotFound):
		writeJSONError(w, http.StatusNotFound, "work item not found")
	case errors.Is(err, plan.ErrNameRequired), errors.Is(err, plan.ErrInvalidDate), errors.Is(err, plan.ErrDateOrder),
		errors.Is(err, plan.ErrParentSelf), errors.Is(err, plan.ErrParentCycle), errors.Is(err, plan.ErrOtherProject),
		errors.Is(err, plan.ErrDependsOnSelf), errors.Is(err, plan.ErrDependencyCycle), errors.Is(err, plan.ErrUnknownMetric),
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

// GetPlan answers the project's plan to its readers: every board task with
// its schedule and progress, the milestones with their criteria measured,
// the dependencies and the metrics a criterion can name.
func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.planReady(w, r, projectID, members.RoleViewer, false) {
		return
	}
	p, ok := h.loadPlan(w, r, projectID)
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, p)
}

// ExportPlan answers the plan as a file: ?format=pdf (the default), excel,
// csv or json.
func (h *Handler) ExportPlan(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.planReady(w, r, projectID, members.RoleViewer, false) {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = plan.FormatPDF
	}
	supported := false
	for _, f := range plan.Formats {
		supported = supported || f == format
	}
	if !supported {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("%s: %s", plan.ErrUnsupportedFormat, format))
		return
	}
	p, ok := h.loadPlan(w, r, projectID)
	if !ok {
		return
	}
	// Only whoever a task is assigned to is named, not the whole project.
	names, assignees := h.planAssignees(projectID), map[string]string{}
	for _, t := range p.Tasks {
		if t.AssigneeID != nil && names[*t.AssigneeID] != "" {
			assignees[*t.AssigneeID] = names[*t.AssigneeID]
		}
	}
	doc := plan.Document{ExportedAt: time.Now().UTC(), Plan: p, Assignees: assignees}
	if h.ProjectService != nil {
		if project, err := h.ProjectService.GetProject(projectID); err == nil && project != nil {
			doc.ProjectName = project.Name
		}
	}
	out, err := plan.Render(doc, format)
	if err != nil {
		respondInternal(w, r, "failed to export the plan", err)
		return
	}
	w.Header().Set("Content-Type", out.ContentType)
	// The filename is quoted; plan.Render strips what a quoted one cannot hold.
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", out.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out.Data)
}

// planAssignees names whoever a task can be assigned to, by id: the
// project's people and the workspace's agents. A lookup that fails leaves
// its names out; the export then shows the id.
func (h *Handler) planAssignees(projectID string) map[string]string {
	names := map[string]string{}
	if h.MemberService != nil {
		if list, err := h.MemberService.ListMembers(projectID); err == nil {
			for _, m := range list {
				names[m.UserID] = m.UserName
				if strings.TrimSpace(m.UserName) == "" {
					names[m.UserID] = m.UserEmail
				}
			}
		}
	}
	if h.AgentService != nil {
		if list, err := h.AgentService.List(h.orgIDForProject(projectID)); err == nil {
			for _, a := range list {
				names[a.ID] = a.Name
			}
		}
	}
	return names
}

// projectMilestone loads a stored milestone of the project, answering 404
// for one that belongs to another project as for one no row has (I3).
func (h *Handler) projectMilestone(w http.ResponseWriter, r *http.Request, projectID string) (*plan.Milestone, bool) {
	m, err := h.PlanService.GetMilestone(mux.Vars(r)["milestoneId"])
	if errors.Is(err, plan.ErrNotFound) || (err == nil && m.ProjectID != projectID) {
		writeJSONError(w, http.StatusNotFound, "milestone not found")
		return nil, false
	}
	if err != nil {
		respondInternal(w, r, "failed to load the milestone", err)
		return nil, false
	}
	return m, true
}

// CreatePlanMilestone adds a custom milestone: a name, and optionally a
// date, a description and completion criteria.
func (h *Handler) CreatePlanMilestone(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.planReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	var req plan.MilestoneRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := h.PlanService.CreateMilestone(projectID, req)
	if err != nil {
		writePlanError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, m)
}

// UpdatePlanMilestone changes a milestone's name, description, date or
// criteria; a field the body leaves out stays as it is.
func (h *Handler) UpdatePlanMilestone(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.planReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	m, ok := h.projectMilestone(w, r, projectID)
	if !ok {
		return
	}
	var req plan.MilestoneRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.PlanService.UpdateMilestone(m.ID, req)
	if err != nil {
		writePlanError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// DeletePlanMilestone removes a milestone. Removing a release's stored
// milestone puts its criteria back to the defaults; the release stays.
func (h *Handler) DeletePlanMilestone(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.planReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	m, ok := h.projectMilestone(w, r, projectID)
	if !ok {
		return
	}
	if err := h.PlanService.DeleteMilestone(m.ID); err != nil {
		writePlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetReleaseMilestoneCriteria sets the completion criteria of a release's
// milestone: {"criteria": [{"metric": ..., "target": ...}]}.
func (h *Handler) SetReleaseMilestoneCriteria(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.planReady(w, r, projectID, members.RoleEditor, true) {
		return
	}
	if h.TargetReleaseService == nil {
		writeJSONError(w, http.StatusNotFound, "release not found")
		return
	}
	// A release of another project answers as one no row has (I3).
	rel, err := h.TargetReleaseService.Get(mux.Vars(r)["releaseId"])
	if errors.Is(err, targetreleases.ErrNotFound) || (err == nil && rel.ProjectID != projectID) {
		writeJSONError(w, http.StatusNotFound, "release not found")
		return
	}
	if err != nil {
		respondInternal(w, r, "failed to load the release", err)
		return
	}
	var req struct {
		Criteria []plan.Criterion `json:"criteria"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := h.PlanService.SetReleaseCriteria(projectID, rel.ID, req.Criteria)
	if err != nil {
		writePlanError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, m)
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

// ScheduleWorkItem places a task on the plan: its start and due dates, its
// parent task and the tasks it depends on. A field the body leaves out
// stays as it is; an empty date or parent clears it. It answers the plan's
// row for the task as the change left it.
func (h *Handler) ScheduleWorkItem(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.planTask(w, r)
	if !ok {
		return
	}
	var req plan.ScheduleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.PlanService.Schedule(id, req, Actor(r)); err != nil {
		writePlanError(w, r, err)
		return
	}
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

// AddWorkItemTodo raises a to-do on a task: {"text": ...}. The to-do is the
// project's, listed with its others, and counts toward the task's progress.
func (h *Handler) AddWorkItemTodo(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.planTask(w, r)
	if !ok {
		return
	}
	if h.TodoService == nil {
		writeJSONError(w, http.StatusNotFound, "to-dos are not available")
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := h.PlanService.AddTodo(mux.Vars(r)["id"], h.orgIDForProject(projectID), req.Text, CurrentUserID(r))
	if err != nil {
		writePlanError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, t)
}
