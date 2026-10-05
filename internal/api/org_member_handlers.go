package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerOrgMemberRoutes wires a workspace's limits and its members.
func (h *Handler) registerOrgMemberRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/limits", h.GetOrgLimits).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members", h.ListOrgMembers).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members", h.AddOrgMember).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/members/{userId}", h.UpdateOrgMember).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/members/{userId}", h.alwaysWritable(h.RemoveOrgMember)).Methods("DELETE")
}

// GetOrgLimits returns every limit this workspace is subject to, with usage
// where usage can be counted. Any member may read it: knowing what the
// workspace allows is not privileged, and hiding it only produces a surprise
// at the moment somebody is refused.
func (h *Handler) GetOrgLimits(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	out, err := h.buildLimitsResponse(orgID)
	if err != nil {
		if errors.Is(err, orgs.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "workspace not found")
			return
		}
		respondInternal(w, r, "failed to read the workspace limits", err)
		return
	}
	respondJSON(w, http.StatusOK, out)
}

func (h *Handler) ListOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	list, err := h.OrgService.ListMembers(orgID)
	if err != nil {
		respondInternal(w, r, "failed to list workspace members", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// AddOrgMember adds a member by email (admin). An address that already has
// an account joins immediately (201 with the membership); one that is
// already a member is a conflict (409 — changing a role is PUT, not a second
// add); one with no account gets an invitation instead of the old "they must
// sign up first" 404 (202 with the invitation and its one-time link), which
// is what makes a closed deployment usable. POST /orgs/{id}/invitations goes
// through the same branch AND the same status writer, so the two cannot
// disagree about either the outcome or how it is reported.
func (h *Handler) AddOrgMember(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	outcome, err := h.addOrInviteToOrg(r, orgID, req.Email, req.Role)
	if err != nil {
		h.writeInvitationError(w, r, err)
		return
	}
	writeAddOrInviteOutcome(w, outcome)
}

func (h *Handler) UpdateOrgMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if !h.requireOrgRole(w, r, vars["id"], orgs.RoleAdmin) {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Read the role before changing it: "you are now an admin" is what the
	// member needs, and "was a member" is what an admin reviewing the change
	// needs, so both ends of the transition are carried on the event.
	previous, _ := h.OrgService.RoleInOrg(vars["id"], vars["userId"])
	if err := h.OrgService.SetMemberRole(vars["id"], vars["userId"], req.Role); err != nil {
		if errors.Is(err, orgs.ErrInvalidRole) || errors.Is(err, orgs.ErrNotMember) || errors.Is(err, orgs.ErrLastAdmin) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to update workspace member", err)
		}
		return
	}
	h.publishOrgEvent(r, events.OrgMemberRoleChanged, vars["id"], vars["userId"], map[string]interface{}{
		"user_id": vars["userId"],
		"from":    previous,
		"to":      req.Role,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	user := requireUserMsg(w, r, "authentication required", http.StatusUnauthorized)
	if user == nil {
		return
	}
	// Members may remove themselves (leave); removing others requires admin.
	minRole := orgs.RoleAdmin
	if user.ID == vars["userId"] {
		minRole = orgs.RoleMember
	}
	if !h.requireOrgRole(w, r, vars["id"], minRole) {
		return
	}
	if err := h.OrgService.RemoveMember(vars["id"], vars["userId"]); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.seatsChanged(vars["id"])
	// "self" separates leaving from being removed. They read completely
	// differently to both audiences, and only one of them is news to the
	// person it happened to.
	h.publishOrgEvent(r, events.OrgMemberRemoved, vars["id"], vars["userId"], map[string]interface{}{
		"user_id": vars["userId"],
		"self":    user.ID == vars["userId"],
	})
	w.WriteHeader(http.StatusNoContent)
}
