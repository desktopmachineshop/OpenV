package api

import "github.com/gorilla/mux"

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Project endpoints
	router.HandleFunc("/api/v1/projects", h.CreateProject).Methods("POST")
	router.HandleFunc("/api/v1/projects", h.ListProjects).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.GetProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.UpdateProject).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}", h.alwaysWritable(h.DeleteProject)).Methods("DELETE")
	router.HandleFunc("/api/v1/projects/{id}/export", h.ExportProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/import", h.alwaysWritable(h.ImportProject)).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/report", h.GenerateReport).Methods("GET")
	// One download surface with a route per output; see download_handlers.go.
	h.registerDownloadRoutes(router)
	router.HandleFunc("/api/v1/projects/{id}/ai-map", h.ProjectAIMap).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/children", h.ListChildProjects).Methods("GET")
	h.registerShareRoutes(router)
	h.registerAdminRoutes(router)
	h.registerBillingRoutes(router)
	router.HandleFunc("/api/v1/projects/{id}/linked-artifacts", h.ListLinkedArtifacts).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/review-queue", h.ReviewQueue).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/review-round", h.StartProjectReview).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/reindex-embeddings", h.ReindexEmbeddings).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/duplicates", h.DuplicateCandidates).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.CreateBaseline).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.ListBaselines).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.GetBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}/diff", h.DiffBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.DeleteBaseline).Methods("DELETE")
	router.HandleFunc("/api/v1/templates", h.ListTemplates).Methods("GET")
	router.HandleFunc("/api/v1/templates", h.CreateTemplate).Methods("POST")
	router.HandleFunc("/api/v1/templates/{id}/projects", h.CreateProjectFromTemplate).Methods("POST")

	// Artifact endpoints
	router.HandleFunc("/api/v1/artifacts", h.CreateArtifact).Methods("POST")
	router.HandleFunc("/api/v1/artifacts", h.ListArtifacts).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.GetArtifact).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.UpdateArtifact).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}/status", h.ChangeArtifactStatus).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}", h.DeleteArtifact).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{id}/versions", h.GetArtifactVersions).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}/restore", h.RestoreArtifactVersion).Methods("POST")
	router.HandleFunc("/api/v1/artifacts/{id}/links", h.GetArtifactVersionLinks).Methods("GET")

	// Link endpoints
	router.HandleFunc("/api/v1/links", h.CreateLink).Methods("POST")
	router.HandleFunc("/api/v1/links", h.ListLinks).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.GetLink).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.UpdateLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}/confirm", h.ConfirmLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}", h.DeleteLink).Methods("DELETE")

	// Attachment endpoints
	router.HandleFunc("/api/v1/attachments/upload", h.UploadAttachment).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}", h.GetAttachmentMeta).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}", h.RenameAttachment).Methods("PUT")
	router.HandleFunc("/api/v1/attachments/{id}/download", h.DownloadAttachment).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}/versions", h.UploadAttachmentVersion).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}/versions", h.ListAttachmentVersions).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}/versions/{version}/restore", h.RestoreAttachmentVersion).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}", h.DeleteAttachment).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{artifactID}/attachments", h.ListArtifactAttachments).Methods("GET")
	router.HandleFunc("/api/v1/projects/{projectID}/attachments", h.ListProjectAttachments).Methods("GET")

	// Chatter endpoints
	router.HandleFunc("/api/v1/chatter", h.CreateChatterEntry).Methods("POST")
	router.HandleFunc("/api/v1/chatter", h.ListChatterEntries).Methods("GET")

	// Global artifact search (see search_handlers.go)
	router.HandleFunc("/api/v1/search", h.GlobalSearch).Methods("GET")

	// Extended route groups (auth, meta, suite, agents, orgs) live in their own files.
	h.registerAuthRoutes(router)
	h.registerMetaRoutes(router)
	h.registerSuiteRoutes(router)
	h.registerNotificationRoutes(router)
	h.registerPushRoutes(router)
	h.registerAgentRoutes(router)
	h.registerOrgRoutes(router)
	h.registerInvitationRoutes(router)
	h.registerPasswordRoutes(router)
	h.registerPasswordResetRoutes(router)
	h.registerAvatarRoutes(router)
	h.registerReleaseRoutes(router)
	h.registerFeatureRoutes(router)
	h.registerDefaultWorkspaceRoutes(router)
	h.registerRunnerSessionRoutes(router)
	h.registerAttributeDefinitionRoutes(router)
	h.registerSharedProductRoutes(router)
	h.registerEvidenceRoutes(router)

	// Health check
	router.HandleFunc("/health", h.Health).Methods("GET")
}
