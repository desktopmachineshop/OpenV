package api

import (
    "encoding/json"
    "net/http"

    "github.com/gorilla/mux"
    "github.com/openv/requirements-platform/internal/domain/artifacts"
    "github.com/openv/requirements-platform/internal/domain/links"
    "github.com/openv/requirements-platform/internal/domain/projects"
)

// Handler holds references to domain services
type Handler struct {
	artifactService artifacts.Service
	linkService     links.Service
	projectService  projects.Service
}

// NewHandler creates a new API handler
func NewHandler(artifactService artifacts.Service, linkService links.Service, projectService projects.Service) *Handler {
	return &Handler{
		artifactService: artifactService,
		linkService:     linkService,
		projectService:  projectService,
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
