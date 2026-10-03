package main

import (
	"log/slog"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/sharelinks"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workitems"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
	"github.com/openv/requirements-platform/internal/seeds"
)

// projects builds the product, settings, attribute, shared-product, V&V,
// evidence, work-item, guided, interview and share-link services, and gives
// the downloads their evidence and workspace sources.
func (a *app) projects() {
	a.productService = products.NewDefaultService(a.productProfileRepo)
	a.exportService.SetProductService(a.productService)
	// Workspace and project preferences — today the requirement quality rule
	// set the linter judges wording against.
	a.settingsService = settings.NewService(postgres.NewSettingsRepository(a.db))
	// Typed attribute definitions (issue #219). The artifacts catalog validates
	// a definition's applies_to_type at create/update time.
	a.attributeService = attributes.NewDefaultService(a.attributeDefRepo, artifacts.ValidType)
	// The community pool of joke demo products for the new-project wizard.
	// Limits are env-tunable so a deployment can tighten them without a
	// release; the defaults bound both abuse and storage cost.
	a.sharedProductService = sharedproducts.NewDefaultService(
		a.sharedProductRepo,
		envInt("OPENV_SHARED_PRODUCT_DAILY_LIMIT", sharedproducts.DefaultDailyOrgLimit),
		envInt("OPENV_SHARED_PRODUCT_POOL_LIMIT", sharedproducts.DefaultPoolLimit),
	)

	// The starter pool (REQ-118). Deployment-wide, so it runs here rather
	// than in the per-org seeding below, and best-effort: an empty pool makes
	// the random-product roller duller, never broken, so a failure here is
	// logged and the server starts anyway.
	if _, err := seeds.EnsureSharedProductPool(a.sharedProductRepo); err != nil {
		slog.Warn("shared product pool: seeding incomplete", "error", err)
	}
	// Let the ReqIF export type enum attributes as ReqIF enumerations.
	a.exportService.SetAttributeService(a.attributeService)
	a.vvService = vv.NewDefaultService(a.vvRepo, a.artifactService, a.chatterService, a.bus)
	// The document downloads carry test evidence and the workspace logo when a
	// reader asks for them; both come from live state beside the snapshot.
	a.downloadService.SetEvidenceSource(downloadEvidenceSource(a.vvService))
	a.downloadService.SetWorkspaceSource(downloadWorkspaceSource(a.projectService, a.orgService))
	a.evidenceService = evidence.NewDefaultService(a.evidenceRepo)
	a.workItemService = workitems.NewDefaultService(a.workItemRepo, a.bus)
	a.guidedService = guided.NewDefaultService(a.guidedRepo, a.artifactService, a.linkService, a.chatterService, a.productService, a.bus)
	a.interviewService = interviews.NewDefaultService(a.interviewRepo)
	a.shareLinkService = sharelinks.NewService(postgres.NewShareLinkRepository(a.db))
}
