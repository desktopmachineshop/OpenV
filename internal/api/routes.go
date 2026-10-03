package api

import "github.com/gorilla/mux"

// RegisterRoutes registers all API routes: one registrar per area, each in
// its area file. gorilla/mux serves the first registered route that
// matches, so the order of the calls is the contract, and
// testdata/route_handlers.txt pins it. That is why the project's own
// registrars interleave with the download, share, admin and billing ones.
func (h *Handler) RegisterRoutes(router *mux.Router) {
	h.registerProjectCoreRoutes(router)
	h.registerProjectIORoutes(router)
	// One download surface with a route per output; see download_handlers.go.
	h.registerDownloadRoutes(router)
	h.registerAIMapRoutes(router)
	h.registerProjectChildRoutes(router)
	h.registerShareRoutes(router)
	h.registerAdminRoutes(router)
	h.registerBillingRoutes(router)
	h.registerLinkedArtifactRoutes(router)
	h.registerReviewRoutes(router)
	h.registerReviewRoundRoutes(router)
	h.registerEmbeddingRoutes(router)
	h.registerBaselineRoutes(router)
	h.registerTemplateRoutes(router)
	h.registerArtifactRoutes(router)
	h.registerArtifactHistoryRoutes(router)
	h.registerLinkRoutes(router)
	h.registerAttachmentRoutes(router)
	h.registerChatterRoutes(router)
	h.registerSearchRoutes(router)
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
	h.registerHealthRoutes(router)
}
