package main

import (
	"log/slog"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/templates"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// core builds the artifact, link, embedding, project, export, report,
// download and template services.
func (a *app) core() {
	// Core services.
	a.artifactService = artifacts.NewDefaultService(a.artifactRepo)
	a.linkService = links.NewDefaultService(a.linkRepo)
	a.linkService.SetArtifactService(a.artifactService)
	// Content changes mark an artifact's links suspect; approval clears
	// them (issue #131).
	a.artifactService.SetLinkSuspector(a.linkService)
	// Semantic-search embeddings (issue #220). Env-gated and DISABLED by
	// default: with OPENV_EMBEDDING_API_KEY unset the provider is a no-op, so
	// this adds nothing to a default deployment. When configured, artifact
	// create/update best-effort (re)embeds content asynchronously, and the
	// admin reindex endpoint backfills a project on demand. The store no-ops
	// gracefully on a database where the vector extension was unavailable at
	// migration time (see migration 0016).
	emb := a.env().Embeddings()
	embeddingProvider := embeddings.NewProvider(emb.APIKey, emb.BaseURL, emb.Model)
	embeddingStore := postgres.NewEmbeddingRepository(a.db)
	a.embeddingService = embeddings.NewService(embeddingProvider, embeddingStore, a.artifactService)
	a.artifactService.SetEmbeddingIndexer(a.embeddingService)
	a.projectService = projects.NewService(a.projectRepo)
	a.attachmentService = attachments.NewDefaultService(a.attachmentRepo)
	a.baselineService = baselines.NewService(a.baselineRepo)
	a.chatterService = chatter.NewDefaultService(a.chatterRepo)
	a.exportService = exports.NewService(a.artifactService, a.linkService, a.attachmentService, a.projectInfoRepo, a.projectService)
	a.reportService = reports.NewService(a.exportService, a.baselineService)
	// One download service over both: it prepares the snapshot once, narrows it
	// to what was asked for, and hands it to the renderer for the chosen format.
	a.downloadService = downloads.NewService(a.exportService, a.reportService)
	a.templateService = templates.NewService(a.templateRepo, a.exportService)
	if err := a.templateService.SeedDefaults(); err != nil {
		slog.Warn("failed to seed templates", "error", err)
	}
}
