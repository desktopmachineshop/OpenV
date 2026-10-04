package api

// A work item's assignee, checked where the board takes one (REQ-23): the
// create and the update of a card in suite_handlers.go call it after their
// own checks, before the service stores the card.

import (
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// assigneeCrewChecked answers a work item's crew assignee (assignee_type
// "team", a crew's wire name) as the crew routes answer a crew no row has,
// 404 `team not found`, when no crew has the id or the caller may not know
// of it (I3), and refuses one the caller may know of in another workspace
// than the item's project 400, as a people-team's grant does. Any other
// assignee passes. Returns false when it has answered.
func (h *Handler) assigneeCrewChecked(w http.ResponseWriter, r *http.Request, projectID, assigneeType string, assigneeID *string) bool {
	if assigneeType != workitems.AssigneeTeam || assigneeID == nil {
		return true
	}
	absent := missing("team not found")
	if h.TeamService == nil {
		absent.write(w)
		return false
	}
	graph, err := h.TeamService.GetTeam(*assigneeID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
		return false
	}
	if !h.requireTeamVisible(w, r, graph.Team, absent) {
		return false
	}
	project, err := h.ProjectService.GetProject(projectID)
	if err != nil || project == nil {
		respondError(w, r, http.StatusNotFound, "project not found", err)
		return false
	}
	if graph.Team.OrgID != project.OrgID {
		writeJSONError(w, http.StatusBadRequest, "team belongs to a different workspace")
		return false
	}
	return true
}
