package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerOrgTeamRoutes wires a workspace's people-teams and their members.
func (h *Handler) registerOrgTeamRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/teams", h.ListOrgTeams).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/teams", h.CreateOrgTeam).Methods("POST")
	router.HandleFunc("/api/v1/org-teams/{id}", h.UpdateOrgTeam).Methods("PUT")
	router.HandleFunc("/api/v1/org-teams/{id}", h.DeleteOrgTeam).Methods("DELETE")
	router.HandleFunc("/api/v1/org-teams/{id}/members/{userId}", h.AddOrgTeamMember).Methods("POST")
	router.HandleFunc("/api/v1/org-teams/{id}/members/{userId}", h.RemoveOrgTeamMember).Methods("DELETE")
}

func (h *Handler) ListOrgTeams(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	list, err := h.OrgTeamService.ListTeams(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list teams", err)
		return
	}
	writeJSONBare(w, list)
}

func (h *Handler) CreateOrgTeam(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	if err := h.checkFlag(orgID, orgs.LimitTeams); err != nil {
		h.writeLimitError(w, err)
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	team, err := h.OrgTeamService.CreateTeam(orgID, req.Name, req.Description, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, team)
}

// orgTeamChecked loads a people-team and enforces the caller's org role.
func (h *Handler) orgTeamChecked(w http.ResponseWriter, r *http.Request, minRole string) *orgs.OrgTeam {
	team, err := h.OrgTeamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
		return nil
	}
	if !h.requireOrgRoleFor(w, r, team.OrgID, minRole, missing("team not found")) {
		return nil
	}
	return team
}

func (h *Handler) UpdateOrgTeam(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.OrgTeamService.UpdateTeam(team.ID, req.Name, req.Description)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, updated)
}

func (h *Handler) DeleteOrgTeam(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	if err := h.OrgTeamService.DeleteTeam(team.ID); err != nil {
		respondInternal(w, r, "failed to delete team", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddOrgTeamMember(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	userID := mux.Vars(r)["userId"]
	already := teamHasMember(team, userID)
	if err := h.OrgTeamService.AddTeamMember(team.ID, userID); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Joining a team hands over every project the team holds, so the person
	// is told; adding somebody who is already in it changes nothing and
	// says nothing.
	if !already {
		h.publishOrgEvent(r, events.OrgTeamMemberAdded, team.OrgID, userID, teamMemberPayload(team, userID))
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) RemoveOrgTeamMember(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	userID := mux.Vars(r)["userId"]
	was := teamHasMember(team, userID)
	if err := h.OrgTeamService.RemoveTeamMember(team.ID, userID); err != nil {
		respondInternal(w, r, "failed to remove team member", err)
		return
	}
	if was {
		h.publishOrgEvent(r, events.OrgTeamMemberRemoved, team.OrgID, userID, teamMemberPayload(team, userID))
	}
	w.WriteHeader(http.StatusNoContent)
}

// teamHasMember reports whether the team, as loaded with its members, holds
// the account: whether adding it changes anything, or removing it does.
func teamHasMember(team *orgs.OrgTeam, userID string) bool {
	for _, m := range team.Members {
		if m != nil && m.UserID == userID {
			return true
		}
	}
	return false
}

func teamMemberPayload(team *orgs.OrgTeam, userID string) map[string]interface{} {
	return map[string]interface{}{
		"user_id":   userID,
		"team_id":   team.ID,
		"team_name": team.Name,
	}
}
