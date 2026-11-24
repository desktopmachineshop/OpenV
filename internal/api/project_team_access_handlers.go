package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerProjectTeamAccessRoutes wires a project's grants to people-teams.
func (h *Handler) registerProjectTeamAccessRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/team-access", h.ListProjectTeamAccess).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/team-access", h.GrantProjectTeamAccess).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/team-access/{teamId}", h.RevokeProjectTeamAccess).Methods("DELETE")
}

func (h *Handler) ListProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.MemberService.ListTeamGrants(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list team grants", err)
		return
	}
	writeJSONBare(w, list)
}

// GrantProjectTeamAccess grants (or updates) a team's role on a project.
func (h *Handler) GrantProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	var req struct {
		OrgTeamID string `json:"org_team_id"`
		Role      string `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// The team must live in the project's org.
	team, err := h.OrgTeamService.GetTeam(req.OrgTeamID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "team not found")
		return
	}
	project, err := h.ProjectService.GetProject(projectID)
	if err != nil || project == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return
	}
	if project.OrgID != "" && team.OrgID != project.OrgID {
		// Another workspace's team the caller is no member of is one no
		// row has (I3).
		if !h.requireOrgVisible(w, r, team.OrgID, missing("team not found")) {
			return
		}
		writeJSONError(w, http.StatusBadRequest, "team belongs to a different workspace")
		return
	}
	if err := h.checkFlag(team.OrgID, orgs.LimitTeams); err != nil {
		h.writeLimitError(w, err)
		return
	}
	previous := h.teamGrantRole(projectID, team.ID)
	if err := h.MemberService.GrantTeam(projectID, req.OrgTeamID, req.Role); err != nil {
		if errors.Is(err, members.ErrInvalidRole) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to grant team access", err)
		}
		return
	}
	// Everyone in the team gains the project (or a new role on it), so
	// each of them is told; re-granting the role the team already has
	// changes nothing.
	if previous != req.Role {
		h.publish(r, events.ProjectTeamAccessGranted, projectID, team.ID, map[string]interface{}{
			"team_id":    team.ID,
			"team_name":  team.Name,
			"role":       req.Role,
			"member_ids": teamMemberIDs(team),
		})
	}
	w.WriteHeader(http.StatusCreated)
}

// teamGrantRole returns the role the project grants the team, "" when it
// grants none (or the grants cannot be read, which only costs a notice).
func (h *Handler) teamGrantRole(projectID, teamID string) string {
	grants, err := h.MemberService.ListTeamGrants(projectID)
	if err != nil {
		return ""
	}
	for _, g := range grants {
		if g != nil && g.OrgTeamID == teamID {
			return g.Role
		}
	}
	return ""
}

func (h *Handler) RevokeProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireProjectRole(w, r, vars["id"], members.RoleOwner) {
		return
	}
	held := h.teamGrantRole(vars["id"], vars["teamId"]) != ""
	if err := h.MemberService.RevokeTeam(vars["id"], vars["teamId"]); err != nil {
		respondInternal(w, r, "failed to revoke team grant", err)
		return
	}
	// Only a grant that existed tells anyone it ended. The team is read
	// for its name and members; one deleted since has no one left to tell.
	if held && h.OrgTeamService != nil {
		if team, err := h.OrgTeamService.GetTeam(vars["teamId"]); err == nil && team != nil {
			h.publish(r, events.ProjectTeamAccessRevoked, vars["id"], team.ID, map[string]interface{}{
				"team_id":    team.ID,
				"team_name":  team.Name,
				"member_ids": teamMemberIDs(team),
			})
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// teamMemberIDs lists the team's members, the people a grant to it reaches.
// Never nil, so the payload always carries a list.
func teamMemberIDs(team *orgs.OrgTeam) []string {
	ids := []string{}
	if team == nil {
		return ids
	}
	for _, m := range team.Members {
		if m != nil && m.UserID != "" {
			ids = append(ids, m.UserID)
		}
	}
	return ids
}
