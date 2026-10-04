package main

import (
	"time"

	eventbus "github.com/openv/requirements-platform/internal/events"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// connect opens the database and returns its Close for main() to defer.
func (a *app) connect() func() error {
	// Connect and migrate.
	var err error
	a.db, err = postgres.Connect(a.dsn)
	if err != nil {
		fatal("failed to connect to database", err)
	}
	return a.db.Close
}

// storage migrates the database and builds the repositories and the event
// bus.
func (a *app) storage() {
	began := time.Now()
	// Schema migration plus the idempotent org backfill, serialized across
	// concurrently booting processes by one session-level advisory lock. The
	// backfill stays outside the numbered ledger because it guards itself
	// and touches the agents directory.
	if err := postgres.MigrateAndBackfill(a.db, a.agentsDir); err != nil {
		fatal("failed to migrate database", err)
	}
	// Once per database, after the migrations: the stored files no row
	// names, which deletes and purges before #379's bugs 136, 143 and 145
	// were fixed left behind, are removed (#379 question 48). It fails
	// safe, under the same advisory lock, and never fails the boot.
	sweepUnreferencedUploads(a.db, a.uploadsDir, began)

	// Repositories.
	a.artifactRepo = postgres.NewArtifactRepository(a.db)
	a.linkRepo = postgres.NewLinkRepository(a.db)
	a.projectRepo = postgres.NewProjectRepository(a.db)
	a.attachmentRepo = postgres.NewAttachmentRepository(a.db)
	a.projectInfoRepo = postgres.NewProjectInfoRepository(a.db)
	a.baselineRepo = postgres.NewBaselineRepository(a.db)
	a.templateRepo = postgres.NewTemplateRepository(a.db)
	a.chatterRepo = postgres.NewChatterRepository(a.db)
	a.userRepo = postgres.NewUserRepository(a.db)
	a.memberRepo = postgres.NewMemberRepository(a.db)
	a.eventRepo = postgres.NewEventRepository(a.db)
	a.productProfileRepo = postgres.NewProductProfileRepository(a.db)
	a.vvRepo = postgres.NewVVRepository(a.db)
	a.evidenceRepo = postgres.NewEvidenceRepository(a.db)
	a.workItemRepo = postgres.NewWorkItemRepository(a.db)
	a.guidedRepo = postgres.NewGuidedRepository(a.db)
	a.interviewRepo = postgres.NewInterviewRepository(a.db)
	a.agentRepo = postgres.NewAgentRepository(a.db)
	a.agentRunRepo = postgres.NewAgentRunRepository(a.db)
	a.automationRepo = postgres.NewAutomationRepository(a.db)
	a.proposalRepo = postgres.NewProposalRepository(a.db)
	a.repoConnRepo = postgres.NewRepoConnectionRepository(a.db)
	a.providerRepo = postgres.NewProviderSettingRepository(a.db)
	a.teamRepo = postgres.NewTeamRepository(a.db)
	a.orgRepo = postgres.NewOrgRepository(a.db)
	a.invitationRepo = postgres.NewInvitationRepository(a.db)
	a.workerKeyRepo = postgres.NewWorkerKeyRepository(a.db)
	a.hostedWorkerRepo = postgres.NewHostedWorkerRepository(a.db)
	a.runnerSessionRepo = postgres.NewRunnerSessionRepository(a.db)
	a.notificationRepo = postgres.NewNotificationRepository(a.db)
	a.pushSubRepo = postgres.NewPushSubscriptionRepository(a.db)
	a.attributeDefRepo = postgres.NewAttributeDefinitionRepository(a.db)
	a.sharedProductRepo = postgres.NewSharedProductRepository(a.db)

	// Event bus. The org resolver backfills tenant attribution for events
	// published by services that only know their project.
	a.bus = eventbus.NewBus(a.eventRepo, projectOrgResolver(a.db))
}
