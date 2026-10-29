package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openv/requirements-platform/ext"
	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/billing/stripe"
	"github.com/openv/requirements-platform/internal/config"
	"github.com/openv/requirements-platform/internal/domain/help"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
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
	// The modules a program built on this server registered (ext.Register,
	// docs/extending.md), built here, beside billing. A registered billing
	// module replaces the one built into this repository, which then starts
	// nothing (docs/plans/hosted-edition-separation.md, Phase 3).
	a.extModules, err = ext.Build(extBoot{a})
	if err != nil {
		fatal("a module could not be built", err)
	}
	if err := installPolicies(a.extModules); err != nil {
		fatal("the modules' plan policies conflict", err)
	}
	switch {
	case ext.Registered("billing"):
		slog.Info("billing is a registered module; the built-in billing stays off")
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
	// The in-app help reads the user manual from OpenD when a docs site is
	// configured (docs/plans/opend-help.md); otherwise the app keeps the
	// help it ships.
	var helpService *help.Service
	if docs := cfg.HelpDocs(); docs != nil {
		helpService = help.NewService(help.Config{URL: docs.URL, Secret: docs.EmbedSecret, AdminToken: docs.AdminToken}, nil)
	}
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
		AccountDeleter:       users.NewAccountDeletionService(a.userDeletionRepo),
		MemberService:        a.memberService,
		ProductService:       a.productService,
		VVService:            a.vvService,
		EvidenceService:      a.evidenceService,
		SettingsService:      a.settingsService,
		ReleaseService:       a.releaseService,
		HelpService:          helpService,
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
		ProviderKeyService:   a.providerKeyService,
		OfferedProviders:     cfg.AgentProviders(),
		LoginService:         a.loginService,
		ProviderTokenService: a.providerTokenService,
		OrgService:           a.orgService,
		OrgTeamService:       a.orgTeamService,
		WorkerKeyService:     a.workerKeyService,
		HostedWorkerService:  a.hostedWorkerService,
		RunnerSessionService: a.runnerSessionService,
		NotificationService:  a.notificationService,
		PushSubService:       a.pushSubService,
		UserMetricsService:   a.userMetricsService,
		VisitorMetrics:       a.visitorMetrics,
		ConnectedApps:        a.connectedApps,
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
		Modules:           a.modules(cfg),
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
		Invite:          limit(r.Invite),
	}
}

// billingLimits hands the billing module its two buckets.
func billingLimits(r config.RateLimits) billing.Limits {
	limit := func(l config.RateLimit) billing.Limit {
		return billing.Limit{Burst: l.Burst, RefillPerHour: l.RefillPerHour}
	}
	return billing.Limits{Refresh: limit(r.BillingRefresh), Write: limit(r.BillingWrite)}
}

// modules are the API's modules: those registered, and the built-in
// billing unless a registered one replaces it.
func (a *app) modules(cfg *config.Config) []ext.Module {
	mods := append([]ext.Module(nil), a.extModules...)
	if !ext.Registered("billing") {
		mods = append(mods, billing.NewModule(a.billingService, billingLimits(cfg.RateLimits())))
	}
	return mods
}

// installPolicies installs the plan and channel policies a module sets
// (ext.PolicyModule) in place of the built-in ones. Two modules setting the
// same one is an error: neither could know which of them is in force.
func installPolicies(mods []ext.Module) error {
	var plans, channels string
	for _, m := range mods {
		pm, ok := m.(ext.PolicyModule)
		if !ok {
			continue
		}
		if p := pm.PlanPolicy(); p != nil {
			if plans != "" {
				return fmt.Errorf("modules %s and %s both set the plan policy", plans, m.Name())
			}
			plans = m.Name()
			orgs.SetPlanPolicy(p)
			slog.Info("plan policy set by a module", "module", plans)
		}
		if c := pm.ChannelPolicy(); c != nil {
			if channels != "" {
				return fmt.Errorf("modules %s and %s both set the channel policy", channels, m.Name())
			}
			channels = m.Name()
			orgs.SetChannelPolicy(c)
			slog.Info("channel policy set by a module", "module", channels)
		}
	}
	return nil
}

// extBoot is the app as a module's ext.Boot.
type extBoot struct{ a *app }

func (b extBoot) Context() context.Context         { return b.a.ctx }
func (b extBoot) Workspaces() ext.WorkspaceService { return b.a.orgService }
func (b extBoot) Users() ext.UserService           { return b.a.userService }
func (b extBoot) Metrics() any                     { return b.a.metricsCollector }
