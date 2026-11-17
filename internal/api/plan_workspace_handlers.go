package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/plan"
)

// registerPlanWorkspaceRoutes registers the workspace's plan calendar: the
// days of the week its plans work, and its people's time off.
func (h *Handler) registerPlanWorkspaceRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/plan-calendar", h.GetWorkspacePlanCalendar).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/plan-calendar", h.SetWorkspacePlanCalendar).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/plan-calendar/time-off", h.AddPlanTimeOff).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/plan-calendar/time-off/{timeOffId}", h.DeletePlanTimeOff).Methods("DELETE")
}

// GetWorkspacePlanCalendar answers the workspace's working days and time
// off to its members.
func (h *Handler) GetWorkspacePlanCalendar(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if h.PlanService == nil || !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	cal, err := h.PlanService.WorkspaceCalendar(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load the plan calendar", err)
		return
	}
	writeJSONOK(w, cal)
}

// SetWorkspacePlanCalendar sets the days of the week every plan of the
// workspace works: {"working_days": [1..7]}. Admins only.
func (h *Handler) SetWorkspacePlanCalendar(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if h.PlanService == nil || !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		WorkingDays []int `json:"working_days"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := h.PlanService.SetWorkspaceWorkingDays(orgID, req.WorkingDays); err != nil {
		writePlanError(w, r, err)
		return
	}
	h.GetWorkspacePlanCalendar(w, r)
}

// AddPlanTimeOff records days a person or an agent of the workspace is
// away: {"type"?, "assignee_id", "start", "end", "name"?}. A member records
// their own; an admin anyone's. It answers the entry, 201.
func (h *Handler) AddPlanTimeOff(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if h.PlanService == nil || !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	var req plan.TimeOff
	if !decodeJSON(w, r, &req) {
		return
	}
	own := req.Type != "agent" && CurrentUserID(r) != nil && *CurrentUserID(r) == req.AssigneeID
	if !own && !h.workspaceAdmin(r, orgID) {
		writeJSONError(w, http.StatusForbidden, "only a workspace admin records someone else's time off")
		return
	}
	off, err := h.PlanService.AddTimeOff(orgID, req)
	if err != nil {
		writePlanError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, off)
}

// DeletePlanTimeOff removes a time-off entry: a member their own, an admin
// anyone's. One the workspace does not have is a 404.
func (h *Handler) DeletePlanTimeOff(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if h.PlanService == nil || !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	id := mux.Vars(r)["timeOffId"]
	cal, err := h.PlanService.WorkspaceCalendar(orgID)
	if err != nil {
		respondInternal(w, r, "failed to load the plan calendar", err)
		return
	}
	var entry *plan.TimeOff
	for i := range cal.TimeOff {
		if cal.TimeOff[i].ID == id {
			entry = &cal.TimeOff[i]
		}
	}
	if entry == nil {
		writeJSONError(w, http.StatusNotFound, "time off not found")
		return
	}
	own := entry.Type != "agent" && CurrentUserID(r) != nil && *CurrentUserID(r) == entry.AssigneeID
	if !own && !h.workspaceAdmin(r, orgID) {
		writeJSONError(w, http.StatusForbidden, "only a workspace admin removes someone else's time off")
		return
	}
	if err := h.PlanService.DeleteTimeOff(orgID, id); errors.Is(err, plan.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "time off not found")
		return
	} else if err != nil {
		writePlanError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
