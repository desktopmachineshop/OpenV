package main

import (
	"log/slog"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/billing/stripe"
	"github.com/openv/requirements-platform/internal/config"
	"github.com/openv/requirements-platform/internal/seeds"
)

// billing reads the billing settings and, when billing is on, starts the
// billing service.
func (a *app) billing() {
	// Handler.
	// Billing (docs/plans/billing-stripe.md). Off — nothing started, nothing
	// dialled, every billing route answering 404 — unless STRIPE_SECRET_KEY
	// is set. A malformed OPENV_STRIPE_PRICES is fatal like OPENV_LIMITS: a
	// typo that silently sold nothing would look exactly like a price that
	// does not work. The prices themselves are confirmed against the
	// provider on the first reconcile, not here: boot never waits on it. On
	// a self-hosted deployment a key is ignored with a warning rather than
	// refused, so a copied env template cannot lock somebody out of their
	// own install — and such a deployment must never dial a provider.
	cfg := a.env()
	billingCfg, err := cfg.Billing()
	if err != nil {
		fatal("billing configuration is not usable", err)
	}
	switch {
	case billingCfg.Enabled() && a.selfHosted:
		slog.Warn("STRIPE_SECRET_KEY is set on a self-hosted deployment (OPENV_SELF_HOSTED=true); billing stays off")
	case billingCfg.Enabled():
		provider := stripe.New(billingCfg.SecretKey, stripe.WithAPIVersion(billingCfg.APIVersion), stripe.WithMetrics(a.metricsCollector))
		a.billingService = billing.New(provider, a.orgService, billingCfg.Registry, a.metricsCollector)
		a.billingService.SetUsers(a.userService)
		a.billingService.SetPortalConfig(billingCfg.PortalConfig)
		a.billingService.SetTrialDays(billingCfg.TrialDays)
		a.billingService.SetMaxSeats(billingCfg.MaxSeats)
		if billingCfg.ReturnURL != "" {
			a.billingService.SetReturnURL(billingCfg.ReturnURL)
		}
		a.billingService.Start(a.ctx, billingCfg.ReconcileInterval)
		slog.Info("billing enabled", "provider", provider.Name(), "prices", billingCfg.Registry.Len(), "reconcile_every", billingCfg.ReconcileInterval)
	}
}

// handlers builds the API handler, hands the proposal service the appliers
// stage agents built, and closes the construction cycle with the guided
// nudges.
func (a *app) handlers() {
	cfg := a.env()
	a.handler = api.NewHandler(api.HandlerDeps{
		ArtifactService:      a.artifactService,
		LinkService:          a.linkService,
		ProjectService:       a.projectService,
		AttachmentService:    a.attachmentService,
		ExportService:        a.exportService,
		DownloadService:      a.downloadService,
		BaselineService:      a.baselineService,
		ReportService:        a.reportService,
		TemplateService:      a.templateService,
		ChatterService:       a.chatterService,
		AttributeService:     a.attributeService,
		SharedProductService: a.sharedProductService,
		EmbeddingService:     a.embeddingService,
		UploadsDir:           a.uploadsDir,
		UserService:          a.userService,
		MemberService:        a.memberService,
		ProductService:       a.productService,
		VVService:            a.vvService,
		EvidenceService:      a.evidenceService,
		SettingsService:      a.settingsService,
		ReleaseService:       a.releaseService,
		DeploymentKind:       a.deploymentKind,
		BuildSHA:             a.buildSHA,
		WorkItemService:      a.workItemService,
		GuidedService:        a.guidedService,
		InterviewService:     a.interviewService,
		ShareLinkService:     a.shareLinkService,
		FrontendURL:          cfg.HandlerFrontendURL(),
		AgentService:         a.agentService,
		RunService:           a.runService,
		AutomationService:    a.automationService,
		ProposalService:      a.proposalService,
		RepoConnService:      a.repoConnService,
		ProviderService:      a.providerService,
		LoginService:         a.loginService,
		OrgService:           a.orgService,
		OrgTeamService:       a.orgTeamService,
		WorkerKeyService:     a.workerKeyService,
		HostedWorkerService:  a.hostedWorkerService,
		RunnerSessionService: a.runnerSessionService,
		NotificationService:  a.notificationService,
		PushSubService:       a.pushSubService,
		UserMetricsService:   a.userMetricsService,
		VAPID:                a.vapid,
		Provisioner:          a.provisioner,
		OrgSeeder: func(orgID string) error {
			return seeds.EnsureOrgDefaults(orgID, a.agentService, a.teamService)
		},
		TeamService:       a.teamService,
		Bus:               a.bus,
		EventRepo:         a.eventRepo,
		SSEHub:            a.sseHub,
		GoogleOAuth:       a.googleOAuth,
		OIDC:              a.oidcConfig,
		SecureCookies:     cfg.SecureCookies(),
		CrossSiteCookies:  cfg.CrossSiteCookies(),
		PublicAPIURL:      cfg.PublicAPIURL(),
		ConnectorDistDir:  cfg.ConnectorDistDir(),
		Mailer:            a.emailMailer,
		EmailLinkBase:     a.emailLinkBase,
		EmailVerification: a.emailVerification,
		InvitationService: a.invitationService,
		BillingService:    a.billingService,
		MinutesAlerts:     a.minutesMonitor,
		Registration:      a.registrationPolicy,
		SessionPolicy:     a.sessionPolicy,
		// Last: NewHandler read the rate limits itself, after every field
		// above, before refactor step X10b.
		RateLimits: handlerRateLimits(cfg.RateLimits()),
	})

	// The proposal appliers stage agents built from the services run when a
	// human approves a proposal; handed over here, after NewHandler, where
	// they always have been, and before the server starts serving.
	a.proposalService.SetAppliers(a.proposalAppliers)
	// Close the construction cycle: a wizard nudge parked while a copilot run
	// was in flight is launched by the handler when the hooks see that run
	// finish.
	a.hooks.SetGuidedNudgeLauncher(a.handler)
}

// server builds the router, the middleware chain and the HTTP server.
func (a *app) server() {
	// Router + middleware: the chain is buildHTTPHandler's (http.go).
	rootHandler, err := buildHTTPHandler(a.env(), a.handler, a.metricsCollector, a.userService, a.runService, a.orgService, a.workerKeyService, a.workerKey, a.bootstrapOrgID, a.runnerPoolKey, a.emailVerification)
	if err != nil {
		fatal("invalid CORS_ORIGIN", err)
	}

	a.srv = newServer(a.ctx, a.port, rootHandler)
}

// handlerRateLimits hands the API handler the rate limits r holds, bucket
// for bucket.
func handlerRateLimits(r config.RateLimits) api.RateLimits {
	limit := func(l config.RateLimit) api.RateLimit {
		return api.RateLimit{Burst: l.Burst, RefillPerHour: l.RefillPerHour}
	}
	return api.RateLimits{
		InterviewMsg:    limit(r.InterviewMsg),
		InterviewIP:     limit(r.InterviewIP),
		InterviewStream: limit(r.InterviewStream),
		AuthIP:          limit(r.AuthIP),
		AuthAccount:     limit(r.AuthAccount),
		RegisterIP:      limit(r.RegisterIP),
		SSOIP:           limit(r.SSOIP),
		VerifyResend:    limit(r.VerifyResend),
		PasswordReset:   limit(r.PasswordReset),
		InvitePreview:   limit(r.InvitePreview),
		BillingRefresh:  limit(r.BillingRefresh),
		BillingWrite:    limit(r.BillingWrite),
		Invite:          limit(r.Invite),
	}
}
