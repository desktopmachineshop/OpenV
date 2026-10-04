package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/settings"
)

// registerReferencePartyRoutes wires reading and updating a project's
// reference parties (REQ-147).
func (h *Handler) registerReferencePartyRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/parties", h.GetProjectParties).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/parties", h.UpdateProjectParties).Methods("PUT")
}

// partiesResponse is the effective list: the workspace's own company first,
// then what the project stores.
type partiesResponse struct {
	Parties []settings.Party `json:"parties"`
}

// effectiveParties resolves a project's parties with the workspace default.
func (h *Handler) effectiveParties(projectID string) ([]settings.Party, error) {
	stored, err := h.SettingsService.ProjectParties(projectID)
	if err != nil {
		return nil, err
	}
	workspace := ""
	if project, err := h.ProjectService.GetProject(projectID); err == nil && project != nil && h.OrgService != nil {
		if org, err := h.OrgService.Get(project.OrgID); err == nil && org != nil {
			workspace = org.Name
		}
	}
	return settings.WithDefault(workspace, stored), nil
}

// GetProjectParties answers the parties a project recognises as owners.
func (h *Handler) GetProjectParties(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	parties, err := h.effectiveParties(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load parties", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(partiesResponse{Parties: parties})
}

// UpdateProjectParties replaces the project's own parties: {"parties":
// [{name, note}]}. The workspace default is never stored and cannot be
// removed; 400 names an empty or repeated party.
func (h *Handler) UpdateProjectParties(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if !h.projectFeatureEnabled(r, projectID, release.FeatureOwners) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req partiesResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, err := h.SettingsService.SetProjectParties(projectID, req.Parties); err != nil {
		if errors.Is(err, settings.ErrInvalidParties) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondInternal(w, r, "failed to save parties", err)
		return
	}
	parties, err := h.effectiveParties(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load parties", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(partiesResponse{Parties: parties})
}
