package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
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
	list, err := h.memberService.ListTeamGrants(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list team grants", err)
		return
	}
	json.NewEncoder(w).Encode(list)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The team must live in the project's org.
	team, err := h.orgTeamService.GetTeam(req.OrgTeamID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "team not found")
		return
	}
	project, err := h.projectService.GetProject(projectID)
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
	if err := h.memberService.GrantTeam(projectID, req.OrgTeamID, req.Role); err != nil {
		if errors.Is(err, members.ErrInvalidRole) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to grant team access", err)
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) RevokeProjectTeamAccess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireProjectRole(w, r, vars["id"], members.RoleOwner) {
		return
	}
	if err := h.memberService.RevokeTeam(vars["id"], vars["teamId"]); err != nil {
		respondInternal(w, r, "failed to revoke team grant", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
