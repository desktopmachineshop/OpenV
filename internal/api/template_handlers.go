package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/templates"
)

// registerTemplateRoutes wires project templates and the projects made
// from them.
func (h *Handler) registerTemplateRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/templates", h.ListTemplates).Methods("GET")
	router.HandleFunc("/api/v1/templates", h.CreateTemplate).Methods("POST")
	router.HandleFunc("/api/v1/templates/{id}/projects", h.CreateProjectFromTemplate).Methods("POST")
}

type createTemplateRequest struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// TemplateListResponse wraps template data with source information
type TemplateListResponse struct {
	ID          string    `json:"id"`
	Key         string    `json:"key,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Source      string    `json:"source"` // "database" or "file"
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListTemplates returns available templates (both database and file-based).
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	// Get database templates (the workspace's own plus global built-ins).
	dbTemplates, err := h.templateService.ListTemplates(ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to list templates", err)
		return
	}

	// Convert DB templates to response format
	var allTemplates []TemplateListResponse
	for _, t := range dbTemplates {
		allTemplates = append(allTemplates, TemplateListResponse{
			ID:          t.ID,
			Key:         t.Key,
			Name:        t.Name,
			Description: t.Description,
			Source:      "database",
			IsDefault:   t.IsDefault,
			CreatedAt:   t.CreatedAt,
		})
	}

	// Get file-based templates
	// Try different locations where examples might be
	var examplesDir string
	possiblePaths := []string{
		"examples",
		"./examples",
		"/root/examples",
	}

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			examplesDir = path
			break
		}
	}

	var fileTemplates []*templates.TemplateSummary
	if examplesDir != "" {
		var err error
		fileTemplates, err = templates.LoadFileBasedTemplates(examplesDir)
		if err != nil {
			// Log the error but continue - file-based templates are optional
			slog.Warn("api: failed to load file-based templates", "dir", examplesDir, "error", err)
		}
	}

	if fileTemplates != nil {
		for _, ft := range fileTemplates {
			allTemplates = append(allTemplates, TemplateListResponse{
				ID:          ft.ID,
				Key:         ft.Key,
				Name:        ft.Name,
				Description: ft.Description,
				Source:      ft.Source,
				IsDefault:   ft.IsDefault,
				CreatedAt:   time.Now(),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allTemplates)
}

// CreateTemplate saves a project as a template.
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req createTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ProjectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}

	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}

	created, err := h.templateService.CreateTemplateFromProject(req.ProjectID, req.Name, req.Description, ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to create template", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

type createProjectFromTemplateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateProjectFromTemplate creates a new project from a template.
func (h *Handler) CreateProjectFromTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := mux.Vars(r)["id"]

	// A project create like any other (requireProjectCreate).
	orgID, ok := h.requireProjectCreate(w, r)
	if !ok {
		return
	}

	var req createProjectFromTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Try to load file-based template first
	// Check multiple possible locations
	var examplesDir string
	possiblePaths := []string{
		"examples",
		"./examples",
		"/root/examples",
	}

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			examplesDir = path
			break
		}
	}

	var snapshot []byte
	var err error

	if examplesDir != "" {
		// Try to load file-based template first
		snapshot, err = templates.GetFileBasedTemplateSnapshot(examplesDir, templateID)
	}

	if snapshot == nil || err != nil {
		// Fall back to database template
		projectID, dbErr := h.templateService.CreateProjectFromTemplate(templateID, req.Name, req.Description, orgID)
		if errors.Is(dbErr, templates.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "template not found")
			return
		}
		if dbErr != nil {
			respondInternal(w, r, "failed to create project from template", dbErr)
			return
		}

		h.addProjectCreatorAsOwner(r, projectID)

		project, err := h.projectService.GetProject(projectID)
		if err != nil {
			respondInternal(w, r, "failed to load project", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(project)
		return
	}

	// Use file-based template
	projectID, err := h.exportService.ImportProjectWithOverrides(snapshot, req.Name, req.Description, orgID)
	if err != nil {
		respondInternal(w, r, "failed to create project from template", err)
		return
	}

	h.addProjectCreatorAsOwner(r, projectID)

	project, err := h.projectService.GetProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load project", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(project)
}

// addProjectCreatorAsOwner grants the requesting user owner membership on a
// freshly created project (mirrors CreateProject).
func (h *Handler) addProjectCreatorAsOwner(r *http.Request, projectID string) {
	if user := CurrentUser(r); user != nil && h.memberService != nil {
		if err := h.memberService.AddMember(projectID, user.ID, members.RoleOwner); err != nil {
			slog.Warn("api: failed to add creator as project owner", "project_id", projectID, "error", err)
		}
	}
}
