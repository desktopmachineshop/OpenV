package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

func (h *Handler) registerAttributeDefinitionRoutes(router *mux.Router) {
	// Effective (merged) set for an artifact editor. Viewer role on the
	// project; also used to render typed inputs.
	router.HandleFunc("/api/v1/meta/attribute-definitions", h.MetaAttributeDefinitions).Methods("GET")

	// Management surface. Raw (non-merged) list, filtered by org_id or
	// project_id, plus admin CRUD.
	router.HandleFunc("/api/v1/attribute-definitions", h.ListAttributeDefinitions).Methods("GET")
	router.HandleFunc("/api/v1/attribute-definitions", h.CreateAttributeDefinition).Methods("POST")
	router.HandleFunc("/api/v1/attribute-definitions/{id}", h.UpdateAttributeDefinition).Methods("PUT")
	router.HandleFunc("/api/v1/attribute-definitions/{id}", h.DeleteAttributeDefinition).Methods("DELETE")
}

// orgForProject resolves a project's owning org id ("" on failure).
func (h *Handler) orgForProject(projectID string) string {
	if projectID == "" || h.projectService == nil {
		return ""
	}
	project, err := h.projectService.GetProject(projectID)
	if err != nil || project == nil {
		return ""
	}
	return project.OrgID
}

// MetaAttributeDefinitions serves the effective attribute definitions for a
// project: the org-wide set overlaid with the project's own overrides. Viewer
// role on the project suffices — this is read-only catalog data the editor
// renders. Without project_id the caller gets the org-wide set for the active
// workspace (any member).
func (h *Handler) MetaAttributeDefinitions(w http.ResponseWriter, r *http.Request) {
	if h.attributeService == nil {
		writeJSONError(w, http.StatusNotFound, "attribute definitions are not available")
		return
	}
	projectID := r.URL.Query().Get("project_id")

	var orgID string
	if projectID != "" {
		if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
			return
		}
		orgID = h.orgForProject(projectID)
	} else {
		orgID = ActiveOrg(r)
		if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
			return
		}
	}

	defs, err := h.attributeService.EffectiveForProject(orgID, projectID)
	if err != nil {
		respondInternal(w, r, "failed to load attribute definitions", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defs)
}

// ListAttributeDefinitions returns the raw (non-merged) definitions for a
// scope, for the management UI. Exactly one of project_id or org_id must be
// given. project_id → project viewer; org_id → org member.
func (h *Handler) ListAttributeDefinitions(w http.ResponseWriter, r *http.Request) {
	if h.attributeService == nil {
		writeJSONError(w, http.StatusNotFound, "attribute definitions are not available")
		return
	}
	q := r.URL.Query()
	projectID := q.Get("project_id")
	orgID := q.Get("org_id")

	var (
		defs []*attributes.Definition
		err  error
	)
	switch {
	case projectID != "":
		if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
			return
		}
		defs, err = h.attributeService.ListByProject(projectID)
	case orgID != "":
		if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
			return
		}
		defs, err = h.attributeService.ListByOrg(orgID)
	default:
		writeJSONError(w, http.StatusBadRequest, "project_id or org_id is required")
		return
	}
	if err != nil {
		respondInternal(w, r, "failed to list attribute definitions", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(defs)
}

// requireAttributeDefinitionWrite enforces the write gate for a definition's
// scope: project-scoped needs project editor, org-wide needs org admin.
func (h *Handler) requireAttributeDefinitionWrite(w http.ResponseWriter, r *http.Request, orgID, projectID *string) bool {
	if projectID != nil && *projectID != "" {
		return h.requireProjectRole(w, r, *projectID, members.RoleEditor)
	}
	if orgID != nil && *orgID != "" {
		return h.requireOrgRole(w, r, *orgID, orgs.RoleAdmin)
	}
	writeJSONError(w, http.StatusBadRequest, "a definition must be either org-wide (org_id) or project-scoped (project_id)")
	return false
}

// CreateAttributeDefinition defines a new typed attribute. Org-wide
// definitions (org_id) require org admin; project-scoped (project_id) require
// project editor.
func (h *Handler) CreateAttributeDefinition(w http.ResponseWriter, r *http.Request) {
	if h.attributeService == nil {
		writeJSONError(w, http.StatusNotFound, "attribute definitions are not available")
		return
	}
	var req attributes.CreateDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.requireAttributeDefinitionWrite(w, r, req.OrgID, req.ProjectID) {
		return
	}
	def, err := h.attributeService.CreateDefinition(req)
	if err != nil {
		writeAttributeDefinitionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(def)
}

// UpdateAttributeDefinition replaces a definition's editable fields. The write
// gate follows the existing definition's scope.
func (h *Handler) UpdateAttributeDefinition(w http.ResponseWriter, r *http.Request) {
	if h.attributeService == nil {
		writeJSONError(w, http.StatusNotFound, "attribute definitions are not available")
		return
	}
	id := mux.Vars(r)["id"]
	existing, err := h.attributeService.GetDefinition(id)
	if err != nil {
		if errors.Is(err, attributes.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "attribute definition not found")
			return
		}
		respondInternal(w, r, "failed to load attribute definition", err)
		return
	}
	if !h.requireAttributeDefinitionWrite(w, r, existing.OrgID, existing.ProjectID) {
		return
	}
	var req attributes.UpdateDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	def, err := h.attributeService.UpdateDefinition(id, req)
	if err != nil {
		writeAttributeDefinitionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(def)
}

// DeleteAttributeDefinition removes a definition. Values already stored in
// artifact attributes under its key are left untouched.
func (h *Handler) DeleteAttributeDefinition(w http.ResponseWriter, r *http.Request) {
	if h.attributeService == nil {
		writeJSONError(w, http.StatusNotFound, "attribute definitions are not available")
		return
	}
	id := mux.Vars(r)["id"]
	existing, err := h.attributeService.GetDefinition(id)
	if err != nil {
		if errors.Is(err, attributes.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "attribute definition not found")
			return
		}
		respondInternal(w, r, "failed to load attribute definition", err)
		return
	}
	if !h.requireAttributeDefinitionWrite(w, r, existing.OrgID, existing.ProjectID) {
		return
	}
	if err := h.attributeService.DeleteDefinition(id); err != nil {
		respondInternal(w, r, "failed to delete attribute definition", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeAttributeDefinitionError maps domain validation errors to 400s.
func writeAttributeDefinitionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attributes.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "attribute definition not found")
	case errors.Is(err, attributes.ErrInvalidScope),
		errors.Is(err, attributes.ErrKeyRequired),
		errors.Is(err, attributes.ErrInvalidKey),
		errors.Is(err, attributes.ErrInvalidType),
		errors.Is(err, attributes.ErrEnumValues),
		errors.Is(err, attributes.ErrInvalidTarget):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
}
