package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/plan"
)

// maxMasterProjects bounds the projects one master plan reads.
const maxMasterProjects = 60

// maxMasterLevels bounds the level a master plan may be asked for by number.
const maxMasterLevels = 20

// registerPlanMasterRoutes registers the master plan and the links between
// tasks of two projects.
func (h *Handler) registerPlanMasterRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/plan/master", h.GetMasterPlan).Methods("GET")
	router.HandleFunc("/api/v1/plan/mine", h.GetMyPlanTasks).Methods("GET")
	router.HandleFunc("/api/v1/work-items/{id}/external-links/{otherId}", h.RelateExternalWorkItems).Methods("PUT")
	router.HandleFunc("/api/v1/work-items/{id}/external-links/{otherId}", h.UnrelateExternalWorkItems).Methods("DELETE")
}

// GetMasterPlan answers the plans of the active workspace's projects the
// caller can read, each as a line: its span, figures, its tasks down to
// ?levels= (1, the top-level tasks, when left out; "all" for every level)
// and dated milestones, with the links between tasks of different projects.
func (h *Handler) GetMasterPlan(w http.ResponseWriter, r *http.Request) {
	levels := 1
	if q := r.URL.Query().Get("levels"); q == "all" {
		levels = plan.MasterAllLevels
	} else if q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > maxMasterLevels {
			writeJSONError(w, http.StatusBadRequest, "levels is a number from 1 to 20, or all")
			return
		}
		levels = n
	}
	out := plan.MasterPlan{Projects: []plan.MasterProject{}, Links: []plan.MasterLink{}}
	org := ActiveOrg(r)
	if h.PlanService == nil || org == "" {
		writeJSONOK(w, out)
		return
	}
	list, err := h.ProjectService.ListProjectsByOrg(org)
	if err != nil {
		respondInternal(w, r, "failed to list projects", err)
		return
	}
	for _, project := range list {
		if len(out.Projects) >= maxMasterProjects {
			break
		}
		if !h.hasProjectRole(r, project.ID, members.RoleViewer) {
			continue
		}
		p, ok := h.loadPlan(w, r, project.ID)
		if !ok {
			return
		}
		// A project with no tasks and no milestones has no plan to show.
		if len(p.Tasks) == 0 && len(p.Milestones) == 0 {
			continue
		}
		out.Add(project.Name, project.ParentProjectID, p, levels)
	}
	// Only links between two projects the caller can read are named.
	shown := map[string]bool{}
	for _, p := range out.Projects {
		shown[p.ProjectID] = true
	}
	links := out.Links[:0]
	for _, l := range out.Links {
		if shown[l.FromProjectID] {
			links = append(links, l)
		}
	}
	out.Links = links
	writeJSONOK(w, out)
}

// GetMyPlanTasks answers the tasks the caller works on, across the plans of
// the active workspace's projects they can read: the ones assigned to them
// and the ones they are a resource of. An account only: a key has no tasks.
func (h *Handler) GetMyPlanTasks(w http.ResponseWriter, r *http.Request) {
	out := plan.MyTasks{Tasks: []plan.MyTask{}}
	org, user := ActiveOrg(r), CurrentUserID(r)
	if h.PlanService == nil || org == "" || user == nil {
		writeJSONOK(w, out)
		return
	}
	list, err := h.ProjectService.ListProjectsByOrg(org)
	if err != nil {
		respondInternal(w, r, "failed to list projects", err)
		return
	}
	read := 0
	for _, project := range list {
		if read >= maxMasterProjects {
			break
		}
		if !h.hasProjectRole(r, project.ID, members.RoleViewer) {
			continue
		}
		p, ok := h.loadPlan(w, r, project.ID)
		if !ok {
			return
		}
		read++
		out.Add(project.Name, p, *user)
	}
	writeJSONOK(w, out)
}

// RelateExternalWorkItems makes the route's task driven by a task of
// another project of the same workspace: {type?, lag_days?, lag_unit?}.
// The caller edits the task's project and can read the other's. It answers
// the task's row of the plan.
func (h *Handler) RelateExternalWorkItems(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.planTask(w, r)
	if !ok {
		return
	}
	var req struct {
		Type    string `json:"type"`
		LagDays int    `json:"lag_days"`
		LagUnit string `json:"lag_unit"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	other, err := h.WorkItemService.Get(mux.Vars(r)["otherId"])
	if err != nil || other == nil || !h.hasProjectRole(r, other.ProjectID, members.RoleViewer) {
		writeJSONError(w, http.StatusNotFound, "work item not found")
		return
	}
	mine, err1 := h.ProjectService.GetProject(projectID)
	theirs, err2 := h.ProjectService.GetProject(other.ProjectID)
	if err1 != nil || err2 != nil || mine == nil || theirs == nil || mine.OrgID != theirs.OrgID {
		writeJSONError(w, http.StatusBadRequest, "the two tasks are in different workspaces")
		return
	}
	id := mux.Vars(r)["id"]
	if err := h.PlanService.RelateExternal(id, other.ID, req.Type, req.LagDays, req.LagUnit, Actor(r)); err != nil {
		writePlanError(w, r, err)
		return
	}
	h.planTaskRow(w, r, projectID, id)
}

// UnrelateExternalWorkItems removes the link between the route's task and
// a task of another project, whichever way round it runs.
func (h *Handler) UnrelateExternalWorkItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.planTask(w, r); !ok {
		return
	}
	err := h.PlanService.UnrelateExternal(mux.Vars(r)["id"], mux.Vars(r)["otherId"])
	if errors.Is(err, plan.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "the tasks are not linked")
		return
	}
	if err != nil {
		writePlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
