package api

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"

    "github.com/google/uuid"
    "github.com/gorilla/mux"
    "github.com/openv/requirements-platform/internal/domain/artifacts"
    "github.com/openv/requirements-platform/internal/domain/attachments"
    "github.com/openv/requirements-platform/internal/domain/links"
    "github.com/openv/requirements-platform/internal/domain/projects"
)

// Handler holds references to domain services
type Handler struct {
	artifactService artifacts.Service
	linkService     links.Service
	projectService  projects.Service
	attachmentService attachments.Service
	uploadsDir      string
}

// NewHandler creates a new API handler
func NewHandler(artifactService artifacts.Service, linkService links.Service, projectService projects.Service, attachmentService attachments.Service, uploadsDir string) *Handler {
	return &Handler{
		artifactService: artifactService,
		linkService:     linkService,
		projectService:  projectService,
		attachmentService: attachmentService,
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

	// Artifact endpoints
	router.HandleFunc("/api/v1/artifacts", h.CreateArtifact).Methods("POST")
	router.HandleFunc("/api/v1/artifacts", h.ListArtifacts).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.GetArtifact).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.UpdateArtifact).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}", h.DeleteArtifact).Methods("DELETE")

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

	artifact, err := h.artifactService.UpdateArtifact(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
