package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
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
	list, err := h.orgTeamService.ListTeams(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list teams", err)
		return
	}
	json.NewEncoder(w).Encode(list)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	team, err := h.orgTeamService.CreateTeam(orgID, req.Name, req.Description, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

// orgTeamChecked loads a people-team and enforces the caller's org role.
func (h *Handler) orgTeamChecked(w http.ResponseWriter, r *http.Request, minRole string) *orgs.OrgTeam {
	team, err := h.orgTeamService.GetTeam(mux.Vars(r)["id"])
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.orgTeamService.UpdateTeam(team.ID, req.Name, req.Description)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteOrgTeam(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	if err := h.orgTeamService.DeleteTeam(team.ID); err != nil {
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
	if err := h.orgTeamService.AddTeamMember(team.ID, mux.Vars(r)["userId"]); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) RemoveOrgTeamMember(w http.ResponseWriter, r *http.Request) {
	team := h.orgTeamChecked(w, r, orgs.RoleAdmin)
	if team == nil {
		return
	}
	if err := h.orgTeamService.RemoveTeamMember(team.ID, mux.Vars(r)["userId"]); err != nil {
		respondInternal(w, r, "failed to remove team member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
