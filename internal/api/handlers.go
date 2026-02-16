package api

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
	"time"

    "github.com/google/uuid"
    "github.com/gorilla/mux"
    "github.com/openv/requirements-platform/internal/domain/artifacts"
    "github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
    "github.com/openv/requirements-platform/internal/domain/exports"
    "github.com/openv/requirements-platform/internal/domain/links"
    "github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/templates"
)

// Handler holds references to domain services
type Handler struct {
	artifactService artifacts.Service
	linkService     links.Service
	projectService  projects.Service
	attachmentService attachments.Service
	exportService   exports.Service
	baselineService baselines.Service
	reportService   reports.Service
	templateService templates.Service
	uploadsDir      string
}

// NewHandler creates a new API handler
func NewHandler(artifactService artifacts.Service, linkService links.Service, projectService projects.Service, attachmentService attachments.Service, exportService exports.Service, baselineService baselines.Service, reportService reports.Service, templateService templates.Service, uploadsDir string) *Handler {
	return &Handler{
		artifactService: artifactService,
		linkService:     linkService,
		projectService:  projectService,
		attachmentService: attachmentService,
		exportService:   exportService,
		baselineService: baselineService,
		reportService:   reportService,
		templateService: templateService,
		uploadsDir:      uploadsDir,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Project endpoints
	router.HandleFunc("/api/v1/projects", h.CreateProject).Methods("POST")
	router.HandleFunc("/api/v1/projects", h.ListProjects).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.GetProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.UpdateProject).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}", h.DeleteProject).Methods("DELETE")
	router.HandleFunc("/api/v1/projects/{id}/export", h.ExportProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/import", h.ImportProject).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/report", h.GenerateReport).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.CreateBaseline).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.ListBaselines).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.GetBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.DeleteBaseline).Methods("DELETE")
	router.HandleFunc("/api/v1/templates", h.ListTemplates).Methods("GET")
	router.HandleFunc("/api/v1/templates", h.CreateTemplate).Methods("POST")
	router.HandleFunc("/api/v1/templates/{id}/projects", h.CreateProjectFromTemplate).Methods("POST")

	// Artifact endpoints
	router.HandleFunc("/api/v1/artifacts", h.CreateArtifact).Methods("POST")
	router.HandleFunc("/api/v1/artifacts", h.ListArtifacts).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.GetArtifact).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.UpdateArtifact).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}", h.DeleteArtifact).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{id}/versions", h.GetArtifactVersions).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}/restore", h.RestoreArtifactVersion).Methods("POST")
	router.HandleFunc("/api/v1/artifacts/{id}/links", h.GetArtifactVersionLinks).Methods("GET")

	// Link endpoints
	router.HandleFunc("/api/v1/links", h.CreateLink).Methods("POST")
	router.HandleFunc("/api/v1/links", h.ListLinks).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.GetLink).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.UpdateLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}", h.DeleteLink).Methods("DELETE")

	// Attachment endpoints
	router.HandleFunc("/api/v1/attachments/upload", h.UploadAttachment).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}", h.GetAttachmentMeta).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}/download", h.DownloadAttachment).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}", h.DeleteAttachment).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{artifactID}/attachments", h.ListArtifactAttachments).Methods("GET")

	// Health check
	router.HandleFunc("/health", h.Health).Methods("GET")
}

// Health returns the health status
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// CreateArtifact creates a new artifact
func (h *Handler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	var req artifacts.CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	artifact := artifacts.NewArtifact(req)
	if err := h.artifactService.CreateArtifact(artifact); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(artifact)
}

// GetArtifact retrieves an artifact by ID
func (h *Handler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	artifact, err := h.artifactService.GetArtifact(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// ListArtifacts lists artifacts by project and optional type filter
func (h *Handler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	artifactType := r.URL.Query().Get("type")

	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}

	artifacts, err := h.artifactService.ListArtifacts(projectID, artifactType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifacts)
}

// UpdateArtifact updates an artifact
func (h *Handler) UpdateArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req artifacts.UpdateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Ensure attributes is initialized
	if req.Attributes == nil {
		req.Attributes = make(map[string]interface{})
	}

	// Process link changes FIRST (add/remove from table)
	var affectedArtifactIDs []string
	if len(req.PendingLinkAdds) > 0 || len(req.PendingLinkRemoves) > 0 {
		// Convert string array to interface array for removal IDs
		removeInterfaceArray := make([]interface{}, len(req.PendingLinkRemoves))
		for i, v := range req.PendingLinkRemoves {
			removeInterfaceArray[i] = v
		}
		affected, err := h.processManagedLinkChanges(id, req.PendingLinkAdds, removeInterfaceArray)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to process link changes: %v", err), http.StatusInternalServerError)
			return
		}
		affectedArtifactIDs = affected

		// After processing link changes, fetch current links and store in snapshot (deduplicated)
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)
		
		incomingLinks, err := h.linkService.GetLinksTo(id)
		if err == nil {
			for _, link := range incomingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}
		
		outgoingLinks, err := h.linkService.GetLinksFrom(id)
		if err == nil {
			for _, link := range outgoingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}
		
		if len(allLinks) > 0 {
			req.Attributes["links_snapshot"] = allLinks
		}
	}

	// Update the artifact ONCE with all changes including link snapshot
	// This single update will create ONE new version
	artifact, err := h.artifactService.UpdateArtifact(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-version any artifacts that had link changes
	// These are OTHER artifacts affected by link changes, not the one we just updated
	if len(affectedArtifactIDs) > 0 {
		err = h.autoVersionLinkedArtifacts(affectedArtifactIDs)
		if err != nil {
			// Log but don't fail the request
			fmt.Printf("Warning: failed to auto-version linked artifacts: %v\n", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// DeleteArtifact deletes an artifact
func (h *Handler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	err := h.artifactService.DeleteArtifact(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetArtifactVersions retrieves all versions of an artifact
func (h *Handler) GetArtifactVersions(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	versions, err := h.artifactService.GetArtifactVersions(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

// RestoreArtifactVersion restores a previous version of an artifact
func (h *Handler) RestoreArtifactVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	artifact, err := h.artifactService.RestoreArtifactVersion(id, req.Version)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// GetArtifactVersionLinks retrieves links for a specific artifact version
func (h *Handler) GetArtifactVersionLinks(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	versionStr := r.URL.Query().Get("version")
	
	// If no version specified, return current links from link table
	if versionStr == "" {
		outgoingLinks, err := h.linkService.GetLinksFrom(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		incomingLinks, err := h.linkService.GetLinksTo(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		// Combine both incoming and outgoing links and deduplicate by ID
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]*links.Link, 0)
		
		// Add outgoing links
		for _, link := range outgoingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}
		
		// Add incoming links (deduplicated)
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(allLinks)
		return
	}
	
	// Parse version number
	version := 0
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil {
		http.Error(w, "invalid version parameter", http.StatusBadRequest)
		return
	}
	
	// Get the artifact version from database
	versions, err := h.artifactService.GetArtifactVersions(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Find the specific version
	var artifactVersion *artifacts.Artifact
	for _, v := range versions {
		if v.Version == version {
			artifactVersion = v
			break
		}
	}
	
	if artifactVersion == nil {
		http.Error(w, "artifact version not found", http.StatusNotFound)
		return
	}
	
	// Extract links_snapshot from artifact attributes
	var linksSnapshot []*links.Link
	if artifactVersion.Attributes != nil {
		if snapshot, ok := artifactVersion.Attributes["links_snapshot"]; ok {
			// Convert interface{} to []*links.Link
			if snapshotData, ok := snapshot.([]interface{}); ok {
				linksSnapshot = make([]*links.Link, 0, len(snapshotData))
				for _, linkData := range snapshotData {
					// Each linkData should be a map or already a Link struct
					if linkBytes, err := json.Marshal(linkData); err == nil {
						var link links.Link
						if err := json.Unmarshal(linkBytes, &link); err == nil {
							linksSnapshot = append(linksSnapshot, &link)
						}
					}
				}
			}
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(linksSnapshot)
}

// CreateLink creates a new link
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	var req links.CreateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	link := links.NewLink(req)
	if err := h.linkService.CreateLink(link); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(link)
}

// GetLink retrieves a link by ID
func (h *Handler) GetLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	link, err := h.linkService.GetLink(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// ListLinks lists links by project
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}

	links, err := h.linkService.GetAllLinks(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

// UpdateLink updates a link
func (h *Handler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req links.UpdateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	link, err := h.linkService.UpdateLink(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// DeleteLink deletes a link
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	err := h.linkService.DeleteLink(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ContentTypeMiddleware adds CORS and content type headers
func ContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CreateProject creates a new project
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req projects.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	project := projects.NewProject(req)
	if err := h.projectService.CreateProject(project); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(project)
}

// GetProject retrieves a project by ID
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	project, err := h.projectService.GetProject(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

// ListProjects lists all projects
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projectList, err := h.projectService.ListProjects()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projectList)
}

// UpdateProject updates a project
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req projects.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	project, err := h.projectService.UpdateProject(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

// DeleteProject deletes a project
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	err := h.projectService.DeleteProject(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ExportProject exports a project in the specified format
func (h *Handler) ExportProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	
	// Get format from query parameter (default to JSON)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	exportFormat := exports.ExportFormat(format)
	
	// Export project
	data, filename, err := h.exportService.ExportProject(id, exportFormat)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ImportProject imports project data from uploaded JSON file and creates a new project
func (h *Handler) ImportProject(w http.ResponseWriter, r *http.Request) {
	// Read the uploaded file
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	
	// Import and create new project
	projectID, err := h.exportService.ImportProject(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"message": "Project imported successfully",
		"project_id": projectID,
	})
}

// GenerateReport generates a PDF report for a project or baseline.
func (h *Handler) GenerateReport(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	baselineID := r.URL.Query().Get("baseline_id")

	data, filename, err := h.reportService.GenerateProjectReport(projectID, baselineID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
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
	// Get database templates
	dbTemplates, err := h.templateService.ListTemplates()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
			fmt.Printf("Warning: failed to load file-based templates from %s: %v\n", examplesDir, err)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ProjectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}

	created, err := h.templateService.CreateTemplateFromProject(req.ProjectID, req.Name, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	var req createProjectFromTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		projectID, dbErr := h.templateService.CreateProjectFromTemplate(templateID, req.Name, req.Description)
		if dbErr != nil {
			http.Error(w, dbErr.Error(), http.StatusInternalServerError)
			return
		}

		project, err := h.projectService.GetProject(projectID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(project)
		return
	}

	// Use file-based template
	projectID, err := h.exportService.ImportProjectWithOverrides(snapshot, req.Name, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	project, err := h.projectService.GetProject(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(project)
}

type createBaselineRequest struct {
	Name string `json:"name"`
}

// CreateBaseline captures a baseline snapshot for a project.
func (h *Handler) CreateBaseline(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	var req createBaselineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := req.Name
	if name == "" {
		name = fmt.Sprintf("Baseline %s", time.Now().Format("2006-01-02 15:04"))
	}

	data, _, err := h.exportService.ExportProject(projectID, exports.FormatJSON)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	baseline, err := h.baselineService.CreateBaseline(projectID, name, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(baseline)
}

// ListBaselines returns baselines for a project.
func (h *Handler) ListBaselines(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	baselines, err := h.baselineService.ListBaselines(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(baselines)
}

// GetBaseline returns the snapshot JSON for a baseline.
func (h *Handler) GetBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.baselineService.GetBaseline(baselineID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(baseline.Snapshot)
}

// DeleteBaseline deletes a baseline by ID.
func (h *Handler) DeleteBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	if err := h.baselineService.DeleteBaseline(baselineID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UploadAttachment uploads an image attachment for an artifact
func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	artifactID := r.FormValue("artifact_id")
	if artifactID == "" {
		http.Error(w, "artifact_id is required", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file from request", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file is an image
	mimeType := header.Header.Get("Content-Type")
	if !isImageMimeType(mimeType) {
		http.Error(w, "File must be an image", http.StatusBadRequest)
		return
	}

	// Read file content
	fileData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%s_%s", uuid.New().String(), header.Filename)
	filepath := filepath.Join(h.uploadsDir, filename)

	// Write file to disk
	if err := os.WriteFile(filepath, fileData, 0644); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Create attachment record
	attachment := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID: artifactID,
		Filename:   header.Filename,
		MimeType:   mimeType,
		FilePath:   filepath,
		FileSize:   len(fileData),
	})

	if err := h.attachmentService.CreateAttachment(attachment); err != nil {
		// Clean up file if database save fails
		_ = os.Remove(filepath)
		http.Error(w, "Failed to save attachment metadata", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(attachment)
}

// GetAttachmentMeta retrieves attachment metadata
func (h *Handler) GetAttachmentMeta(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.attachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		http.Error(w, "Attachment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachment)
}

// DownloadAttachment serves the attachment file
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.attachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		http.Error(w, "Attachment not found", http.StatusNotFound)
		return
	}

	// Set appropriate headers for image serving
	w.Header().Set("Content-Type", attachment.MimeType)
	w.Header().Set("Content-Length", strconv.Itoa(attachment.FileSize))
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	// Serve the file
	http.ServeFile(w, r, attachment.FilePath)
}

// DeleteAttachment deletes an attachment
func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.attachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		http.Error(w, "Attachment not found", http.StatusNotFound)
		return
	}

	// Delete file from disk
	if err := os.Remove(attachment.FilePath); err != nil && !os.IsNotExist(err) {
		http.Error(w, "Failed to delete file", http.StatusInternalServerError)
		return
	}

	// Delete database record
	if err := h.attachmentService.DeleteAttachment(id); err != nil {
		http.Error(w, "Failed to delete attachment", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListArtifactAttachments lists all attachments for an artifact
func (h *Handler) ListArtifactAttachments(w http.ResponseWriter, r *http.Request) {
	artifactID := mux.Vars(r)["artifactID"]

	attachmentList, err := h.attachmentService.GetAttachmentsByArtifact(artifactID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachmentList)
}

// isImageMimeType checks if the mime type is a valid image type
func isImageMimeType(mimeType string) bool {
	validTypes := map[string]bool{
		"image/jpeg":      true,
		"image/png":       true,
		"image/gif":       true,
		"image/webp":      true,
		"image/svg+xml":   true,
		"image/tiff":      true,
		"image/bmp":       true,
	}
	return validTypes[mimeType]
}
// processManagedLinkChanges handles link additions and removals
// Returns list of artifact IDs that had links change (for auto-versioning)
func (h *Handler) processManagedLinkChanges(fromArtifactID string, toAdd, toRemove []interface{}) ([]string, error) {
	affectedArtifactIDs := make(map[string]bool) // Use map to avoid duplicates

	// Process removals (hard delete from table)
	for _, linkIDInterface := range toRemove {
		linkID, ok := linkIDInterface.(string)
		if !ok {
			continue
		}
		
		// Get the link before deleting to determine affected artifact
		link, err := h.linkService.GetLink(linkID)
		if err == nil && link != nil {
			// Mark the 'to' artifact as affected (it had an incoming link removed)
			if link.ToID == fromArtifactID {
				affectedArtifactIDs[link.FromID] = true
			} else {
				affectedArtifactIDs[link.ToID] = true
			}
		}
		
		// Hard delete the link
		err = h.linkService.DeleteLink(linkID)
		if err != nil {
			fmt.Printf("Warning: failed to delete link %s: %v\n", linkID, err)
		}
	}

	// Process additions (create new links)
	for _, linkDataInterface := range toAdd {
		linkDataMap, ok := linkDataInterface.(map[string]interface{})
		if !ok {
			continue
		}

		fromID, ok := linkDataMap["from_id"].(string)
		if !ok {
			continue
		}
		toID, ok := linkDataMap["to_id"].(string)
		if !ok {
			continue
		}
		linkType, ok := linkDataMap["type"].(string)
		if !ok {
			continue
		}

		// Get or create attributes
		var attributes map[string]interface{}
		if attrs, ok := linkDataMap["attributes"].(map[string]interface{}); ok {
			attributes = attrs
		} else {
			attributes = make(map[string]interface{})
		}

		// Create the link
		linkReq := links.CreateLinkRequest{
			FromID:     fromID,
			ToID:       toID,
			Type:       linkType,
			Attributes: attributes,
		}
		link := links.NewLink(linkReq)
		err := h.linkService.CreateLink(link)
		if err != nil {
			fmt.Printf("Warning: failed to create link: %v\n", err)
			continue
		}

		// Mark both artifacts as affected
		affectedArtifactIDs[fromID] = true
		affectedArtifactIDs[toID] = true
	}

	// Convert map to slice
	result := make([]string, 0, len(affectedArtifactIDs))
	for id := range affectedArtifactIDs {
		if id != fromArtifactID { // Don't include the artifact we're currently updating
			result = append(result, id)
		}
	}

	return result, nil
}

// autoVersionLinkedArtifacts creates new versions for artifacts that had link changes
func (h *Handler) autoVersionLinkedArtifacts(affectedArtifactIDs []string) error {
	fmt.Printf("DEBUG autoVersionLinkedArtifacts: Processing %d affected artifacts: %v\n", len(affectedArtifactIDs), affectedArtifactIDs)
	
	for _, artifactID := range affectedArtifactIDs {
		artifact, err := h.artifactService.GetArtifact(artifactID)
		if err != nil {
			fmt.Printf("Warning: could not find artifact %s for auto-versioning: %v\n", artifactID, err)
			continue
		}

		// Get current links for this artifact and deduplicate
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)
		
		incomingLinks, err := h.linkService.GetLinksTo(artifactID)
		if err != nil {
			fmt.Printf("Warning: could not get incoming links for %s: %v\n", artifactID, err)
			continue
		}

		outgoingLinks, err := h.linkService.GetLinksFrom(artifactID)
		if err != nil {
			fmt.Printf("Warning: could not get outgoing links for %s: %v\n", artifactID, err)
			continue
		}

		// Combine all links with deduplication
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}
		for _, link := range outgoingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		// Ensure attributes is initialized
		attributes := artifact.Attributes
		if attributes == nil {
			attributes = make(map[string]interface{})
		}

		// Merge link snapshot into attributes
		attributes["links_snapshot"] = allLinks

		// Create a new version with updated link snapshot
		updateReq := artifacts.UpdateArtifactRequest{
			ParentID:   artifact.ParentID,
			Type:       artifact.Type,
			Title:      artifact.Title,
			Body:       artifact.Body,
			SortOrder:  &artifact.SortOrder,
			Attributes: attributes,
		}

		_, err = h.artifactService.UpdateArtifact(artifactID, updateReq)
		if err != nil {
			fmt.Printf("Warning: failed to auto-version artifact %s: %v\n", artifactID, err)
			continue
		}

		fmt.Printf("Auto-versioned artifact %s due to link changes; incoming links: %d, outgoing links: %d\n", artifactID, len(incomingLinks), len(outgoingLinks))
	}

	return nil
}