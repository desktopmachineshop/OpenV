package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"

	openv "github.com/openv/requirements-platform"
	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/automation"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/evidence"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/settings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/templates"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/domain/workitems"
	eventbus "github.com/openv/requirements-platform/internal/events"
	"github.com/openv/requirements-platform/internal/hosting"
	"github.com/openv/requirements-platform/internal/metrics"
	"github.com/openv/requirements-platform/internal/notify"
	"github.com/openv/requirements-platform/internal/orchestration"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
	"github.com/openv/requirements-platform/internal/scheduler"
	"github.com/openv/requirements-platform/internal/seeds"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt reads a positive integer setting, falling back on anything unset,
// unparseable, or non-positive.
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// initLogging installs the process-wide slog default: a text handler on
// stderr with the level taken from OPENV_LOG_LEVEL (debug|info|warn|error,
// default info).
func initLogging() {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENV_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "", "info":
		// default
	default:
		// Unknown value: keep info, but say so once.
		defer slog.Warn("unrecognized OPENV_LOG_LEVEL, using info", "value", os.Getenv("OPENV_LOG_LEVEL"))
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// fatal logs a boot-blocking error and exits.
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

func main() {
	initLogging()

	// Root context: canceled on SIGINT/SIGTERM so background loops and the
	// HTTP server can shut down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Database connection: Railway-style DATABASE_URL or individual vars.
	var dsn string
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		slog.Info("using DATABASE_URL (Railway.app mode)")
		dsn = databaseURL
	} else {
		slog.Info("using individual environment variables (local development mode)")
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			envOr("DB_HOST", "localhost"), envOr("DB_PORT", "5432"), envOr("DB_USER", "postgres"),
			envOr("DB_PASSWORD", "postgres"), envOr("DB_NAME", "openv"))
	}

	port := envOr("PORT", "8080")
	uploadsDir := envOr("UPLOADS_DIR", "./uploads")
	dataDir := envOr("OPENV_DATA_DIR", "./data")
	agentsDir := envOr("AGENTS_DIR", dataDir+"/agents")
	// WORKER_API_KEY is a legacy single-key fallback; workers should use
	// org-scoped keys minted in workspace settings.
	workerKey := os.Getenv("WORKER_API_KEY")

	// Workspace limits. A deployment somebody runs themselves owns its own
	// hardware, so it sets its own ceilings: OPENV_SELF_HOSTED picks the plan
	// new workspaces are created on AND the remedy a refusal offers, because
	// telling a self-hoster to upgrade a plan they do not have would send
	// them nowhere. OPENV_LIMITS then retunes any individual limit across the
	// whole deployment without touching the database.
	selfHosted := os.Getenv("OPENV_SELF_HOSTED") == "true"
	orgs.SetSelfHosted(selfHosted)
	defaultPlan := envOr("OPENV_PLAN_DEFAULT", "")
	if defaultPlan == "" {
		defaultPlan = orgs.PlanSingle
		if selfHosted {
			defaultPlan = orgs.PlanSelfHost
		}
	}
	orgs.SetDefaultPlan(defaultPlan)
	// A malformed OPENV_LIMITS is fatal rather than ignored: a typo that
	// silently did nothing would be indistinguishable from a limit that does
	// not work, and the operator would discover it when somebody was wrongly
	// refused.
	deploymentLimits, err := orgs.ParseLimits(os.Getenv("OPENV_LIMITS"))
	if err != nil {
		fatal("OPENV_LIMITS is not usable", err)
	}
	orgs.SetDeploymentLimits(deploymentLimits)
	slog.Info("workspace limits configured",
		"self_hosted", selfHosted, "default_plan", defaultPlan, "overrides", len(deploymentLimits))

	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		fatal("failed to create uploads directory", err)
	}

	// Connect and migrate.
	db, err := postgres.Connect(dsn)
	if err != nil {
		fatal("failed to connect to database", err)
	}
	defer db.Close()
	// Schema migration plus the idempotent org backfill, serialized across
	// concurrently booting processes by one session-level advisory lock. The
	// backfill stays outside the numbered ledger because it guards itself
	// and touches the agents directory.
	if err := postgres.MigrateAndBackfill(db, agentsDir); err != nil {
		fatal("failed to migrate database", err)
	}

	// Repositories.
	artifactRepo := postgres.NewArtifactRepository(db)
	linkRepo := postgres.NewLinkRepository(db)
	projectRepo := postgres.NewProjectRepository(db)
	attachmentRepo := postgres.NewAttachmentRepository(db)
	projectInfoRepo := postgres.NewProjectInfoRepository(db)
	baselineRepo := postgres.NewBaselineRepository(db)
	templateRepo := postgres.NewTemplateRepository(db)
	chatterRepo := postgres.NewChatterRepository(db)
	userRepo := postgres.NewUserRepository(db)
	memberRepo := postgres.NewMemberRepository(db)
	eventRepo := postgres.NewEventRepository(db)
	productProfileRepo := postgres.NewProductProfileRepository(db)
	vvRepo := postgres.NewVVRepository(db)
	evidenceRepo := postgres.NewEvidenceRepository(db)
	workItemRepo := postgres.NewWorkItemRepository(db)
	guidedRepo := postgres.NewGuidedRepository(db)
	interviewRepo := postgres.NewInterviewRepository(db)
	agentRepo := postgres.NewAgentRepository(db)
	agentRunRepo := postgres.NewAgentRunRepository(db)
	automationRepo := postgres.NewAutomationRepository(db)
	proposalRepo := postgres.NewProposalRepository(db)
	repoConnRepo := postgres.NewRepoConnectionRepository(db)
	providerRepo := postgres.NewProviderSettingRepository(db)
	teamRepo := postgres.NewTeamRepository(db)
	orgRepo := postgres.NewOrgRepository(db)
	invitationRepo := postgres.NewInvitationRepository(db)
	workerKeyRepo := postgres.NewWorkerKeyRepository(db)
	hostedWorkerRepo := postgres.NewHostedWorkerRepository(db)
	runnerSessionRepo := postgres.NewRunnerSessionRepository(db)
	notificationRepo := postgres.NewNotificationRepository(db)
	pushSubRepo := postgres.NewPushSubscriptionRepository(db)
	attributeDefRepo := postgres.NewAttributeDefinitionRepository(db)
	sharedProductRepo := postgres.NewSharedProductRepository(db)

	// Event bus. The org resolver backfills tenant attribution for events
	// published by services that only know their project.
	bus := eventbus.NewBus(eventRepo, func(projectID string) string {
		var orgID string
		if err := db.QueryRow(`SELECT COALESCE(org_id::text, '') FROM projects WHERE id = $1::uuid`, projectID).Scan(&orgID); err != nil {
			return ""
		}
		return orgID
	})

	// Core services.
	artifactService := artifacts.NewDefaultService(artifactRepo)
	linkService := links.NewDefaultService(linkRepo)
	linkService.SetArtifactService(artifactService)
	// Content changes mark an artifact's links suspect; approval clears
	// them (issue #131).
	artifactService.SetLinkSuspector(linkService)
	// Semantic-search embeddings (issue #220). Env-gated and DISABLED by
	// default: with OPENV_EMBEDDING_API_KEY unset the provider is a no-op, so
	// this adds nothing to a default deployment. When configured, artifact
	// create/update best-effort (re)embeds content asynchronously, and the
	// admin reindex endpoint backfills a project on demand. The store no-ops
	// gracefully on a database where the vector extension was unavailable at
	// migration time (see migration 0016).
	embeddingProvider := embeddings.ProviderFromEnv()
	embeddingStore := postgres.NewEmbeddingRepository(db)
	embeddingService := embeddings.NewService(embeddingProvider, embeddingStore, artifactService)
	artifactService.SetEmbeddingIndexer(embeddingService)
	projectService := projects.NewService(projectRepo)
	attachmentService := attachments.NewDefaultService(attachmentRepo)
	baselineService := baselines.NewService(baselineRepo)
	chatterService := chatter.NewDefaultService(chatterRepo)
	exportService := exports.NewService(artifactService, linkService, attachmentService, projectInfoRepo, projectService)
	reportService := reports.NewService(exportService, baselineService)
	// One download service over both: it prepares the snapshot once, narrows it
	// to what was asked for, and hands it to the renderer for the chosen format.
	downloadService := downloads.NewService(exportService, reportService)
	templateService := templates.NewService(templateRepo, exportService)
	if err := templateService.SeedDefaults(); err != nil {
		slog.Warn("failed to seed templates", "error", err)
	}

	// Suite services.
	userService := users.NewDefaultService(userRepo)
	memberService := members.NewDefaultService(memberRepo)
	orgService := orgs.NewDefaultService(orgRepo)
	orgTeamService := orgs.NewTeamService(orgRepo, orgService)
	// Workspace invitations (REQ-95): the only way into a workspace for
	// someone with no account, and the prerequisite for closing self-service
	// registration below.
	invitationService := invitations.NewDefaultService(invitationRepo, orgService)
	workerKeyService := workerkeys.NewDefaultService(workerKeyRepo)
	workerKeyService.SetPairingRepository(workerKeyRepo)
	hostedWorkerService := hostedworkers.NewDefaultService(hostedWorkerRepo)

	// Transient runners. A deployment opts in by setting RUNNER_POOL_KEY —
	// the shared credential its pool nodes present — because without a pool
	// there is nothing to lease. Everything else about the feature is on by
	// default for the workspaces on that deployment.
	runnerPoolKey := os.Getenv("RUNNER_POOL_KEY")
	var runnerSessionService runnersessions.Service
	if runnerPoolKey != "" {
		runnerSessionService = runnersessions.NewDefaultService(runnerSessionRepo, workerKeyService)
		slog.Info("transient runners enabled (runner pool configured)")
	} else {
		slog.Info("transient runners disabled (set RUNNER_POOL_KEY to enable)")
	}

	// Hosted runner provisioner (Docker). Disabled when HOSTED_RUNNERS=off
	// or the docker daemon is unreachable. Boot reconcile syncs stored
	// records with actual container state.
	provisioner := hosting.NewProvisioner()
	if provisioner.Enabled() {
		if hostedList, err := hostedWorkerService.ListAll(); err != nil {
			slog.Warn("failed to list hosted workers for reconcile", "error", err)
		} else {
			for _, hw := range hostedList {
				state, err := provisioner.ContainerState(hw.ContainerName)
				if err != nil {
					slog.Warn("failed to inspect hosted runner", "container", hw.ContainerName, "error", err)
					continue
				}
				status, detail := hw.Status, hw.Detail
				switch state {
				case "missing":
					status, detail = hostedworkers.StatusError, "container not found"
				case "running":
					status, detail = hostedworkers.StatusRunning, ""
				case "exited", "created", "paused", "dead":
					status, detail = hostedworkers.StatusStopped, ""
				}
				if status != hw.Status || detail != hw.Detail {
					if _, err := hostedWorkerService.SetStatus(hw.ID, status, detail); err != nil {
						slog.Warn("failed to reconcile hosted runner", "container", hw.ContainerName, "error", err)
					}
				}
			}
		}
	}

	// bootstrapOrgID resolves the earliest personal org (legacy worker-key
	// fallback + env-key registration).
	bootstrapOrgID := func() string {
		var id string
		err := db.QueryRow(`
			SELECT o.id FROM organizations o
			JOIN org_members m ON m.org_id = o.id
			JOIN users u ON u.id = m.user_id
			WHERE o.org_type = 'personal'
			ORDER BY u.created_at LIMIT 1
		`).Scan(&id)
		if err != nil {
			return ""
		}
		return id
	}
	if workerKey != "" {
		if orgID := bootstrapOrgID(); orgID != "" {
			if err := workerKeyService.EnsureBootstrapKey(orgID, workerKey, "env-bootstrap"); err != nil {
				slog.Warn("failed to register WORKER_API_KEY as an org key", "error", err)
			}
		}
	}
	productService := products.NewDefaultService(productProfileRepo)
	exportService.SetProductService(productService)
	// Workspace and project preferences — today the requirement quality rule
	// set the linter judges wording against.
	settingsService := settings.NewService(postgres.NewSettingsRepository(db))
	// Typed attribute definitions (issue #219). The artifacts catalog validates
	// a definition's applies_to_type at create/update time.
	attributeService := attributes.NewDefaultService(attributeDefRepo, artifacts.ValidType)
	// The community pool of joke demo products for the new-project wizard.
	// Limits are env-tunable so a deployment can tighten them without a
	// release; the defaults bound both abuse and storage cost.
	sharedProductService := sharedproducts.NewDefaultService(
		sharedProductRepo,
		envInt("OPENV_SHARED_PRODUCT_DAILY_LIMIT", sharedproducts.DefaultDailyOrgLimit),
		envInt("OPENV_SHARED_PRODUCT_POOL_LIMIT", sharedproducts.DefaultPoolLimit),
	)

	// The starter pool (REQ-118). Deployment-wide, so it runs here rather
	// than in the per-org seeding below, and best-effort: an empty pool makes
	// the random-product roller duller, never broken, so a failure here is
	// logged and the server starts anyway.
	if _, err := seeds.EnsureSharedProductPool(sharedProductRepo); err != nil {
		slog.Warn("shared product pool: seeding incomplete", "error", err)
	}
	// Let the ReqIF export type enum attributes as ReqIF enumerations.
	exportService.SetAttributeService(attributeService)
	vvService := vv.NewDefaultService(vvRepo, artifactService, chatterService, bus)
	// The document downloads carry test evidence and the workspace logo when a
	// reader asks for them; both come from live state beside the snapshot.
	downloadService.SetEvidenceSource(func(projectID string) (map[string]*vv.TestResult, []*vv.TestRun, error) {
		latest, err := vvService.LatestResults(projectID)
		if err != nil {
			return nil, nil, err
		}
		runs, err := vvService.ListRuns(projectID)
		if err != nil {
			return nil, nil, err
		}
		return latest, runs, nil
	})
	downloadService.SetWorkspaceSource(func(projectID string) (reports.Workspace, error) {
		project, err := projectService.GetProject(projectID)
		if err != nil || project == nil || project.OrgID == "" {
			return reports.Workspace{}, err
		}
		org, err := orgService.Get(project.OrgID)
		if err != nil || org == nil {
			return reports.Workspace{}, err
		}
		ws := reports.Workspace{Name: org.Name}
		if org.LogoPath != "" {
			if logo, err := os.ReadFile(org.LogoPath); err == nil {
				ws.Logo, ws.LogoMime = logo, org.LogoMime
			} else {
				slog.Warn("download: workspace logo could not be read", "org_id", org.ID, "error", err)
			}
		}
		return ws, nil
	})
	evidenceService := evidence.NewDefaultService(evidenceRepo)
	workItemService := workitems.NewDefaultService(workItemRepo, bus)
	guidedService := guided.NewDefaultService(guidedRepo, artifactService, linkService, chatterService, productService, bus)
	interviewService := interviews.NewDefaultService(interviewRepo)

	// Agent engine services.
	// The file sync backfills a definition that carries no allowlist (REQ-91),
	// and it runs before seeds.EnsureOrgDefaults, so it is the one that
	// decides what a seeded agent ends up with — hence the seed lookup.
	agentService, err := agents.NewFileService(agentsDir, agentRepo,
		agents.WithSeedAllowedTools(seeds.SeedAllowedTools))
	if err != nil {
		fatal("failed to initialize agent service", err)
	}
	if err := agentService.SyncAllFromDisk(); err != nil {
		slog.Warn("agent sync completed with warnings", "error", err)
	}
	runService := agentruns.NewDefaultService(agentRunRepo, agentService, bus)
	// First-refusal routing: runs launched by a user with an online personal
	// runner wait for it before hosted/workspace runners may claim.
	runService.SetRoutingPolicy(
		func(orgID, userID string) bool {
			online, err := workerKeyService.HasOnlinePersonalRunner(orgID, userID, time.Now().Add(-30*time.Second))
			return err == nil && online
		},
		func(orgID string) int {
			org, err := orgService.Get(orgID)
			if err != nil {
				return 0
			}
			if v, ok := org.Limits["runner_grace_seconds"].(float64); ok {
				return int(v)
			}
			return 0
		})
	// Bounded auto-retry (issue #184): a retryable terminal failure
	// (provider_unavailable | timeout | worker_error) re-enqueues a fresh
	// attempt with backoff while attempts remain. OPENV_RUN_MAX_ATTEMPTS caps
	// the chain (default 3); OPENV_RUN_AUTO_RETRY=false (or a cap of 1) opts
	// out and the failure simply stands.
	maxAttempts := agentruns.DefaultMaxAttempts
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("OPENV_RUN_MAX_ATTEMPTS"))); err == nil && v > 0 {
		maxAttempts = v
	}
	autoRetry := !strings.EqualFold(strings.TrimSpace(os.Getenv("OPENV_RUN_AUTO_RETRY")), "false")
	runService.SetRetryPolicy(maxAttempts, autoRetry)
	automationService := automations.NewDefaultService(automationRepo)
	repoConnService := repoconns.NewDefaultService(repoConnRepo)
	providerService := providers.NewDefaultService(providerRepo)
	loginService := providers.NewLoginService(postgres.NewProviderLoginRepository(db))
	teamService := teams.NewDefaultService(teamRepo)
	// Human crew members must belong to the crew's workspace.
	teamService.SetMemberValidator(func(orgID, userID string) bool {
		ok, err := orgService.IsMember(orgID, userID)
		return err == nil && ok
	})

	// Proposal appliers execute approved agent writes via the real services.
	// They are wired after the HTTP handler is built (handler.ProposalAppliers
	// below): the appliers run the handler's own domain writes — events,
	// link-snapshot auto-versioning — but the handler needs this service, so
	// the callbacks are injected once the cycle can be closed.
	proposalService := proposals.NewDefaultService(proposalRepo, proposals.Appliers{})

	// When a run's last proposal is reviewed, finalize the run: an
	// awaiting_approval run leaves that absorbing state for succeeded (or
	// failed if any approved write failed to apply), publishing RunFinished so
	// crew successors, the notifier and automation triggers fire on the real
	// outcome. Covers single + bulk review and the applier path alike.
	proposalService.OnResolved(func(runID string) {
		if _, err := runService.FinalizeIfResolved(runID); err != nil {
			slog.Error("proposal resolution: failed to finalize run", "run_id", runID, "error", err)
		}
	})

	// Seed default agents + crew into every workspace missing them.
	if orgIDs, err := orgService.ListAll(); err != nil {
		slog.Warn("failed to list organizations for seeding", "error", err)
	} else {
		for _, orgID := range orgIDs {
			if err := seeds.EnsureOrgDefaults(orgID, agentService, teamService); err != nil {
				slog.Warn("failed to seed default agents/team", "org_id", orgID, "error", err)
			}
		}
	}

	// Prometheus metrics. The collector subscribes to run lifecycle events so
	// the run counter and queued/running gauges track transitions, and reports
	// live SSE connection counts on scrape. Wired below at /metrics.
	metricsCollector := metrics.New()
	runService.AddSubscriber(metricsCollector)

	// SSE hub + orchestration hooks.
	sseHub := api.NewSSEHub()
	runService.AddSubscriber(sseHub)
	metricsCollector.WatchSSEConnections(sseHub.ActiveConnections)
	hooks := orchestration.NewHooks(runService, teamService, workItemService, interviewService, guidedService, projectService, sseHub)
	runService.AddSubscriber(hooks)
	hooks.SubscribeBus(bus)

	// Optional email side channel for high-signal notifications (issue #187).
	// Strictly opt-in: with OPENV_SMTP_HOST unset the mailer is a no-op, so
	// in-app + SSE delivery (and dev/compose) are unaffected. Deep links point
	// at the frontend (FRONTEND_URL), falling back to PUBLIC_URL.
	emailMailer := notify.MailerFromEnv()
	// Links in mail point at the SPA, which the API itself never serves, so
	// the last fallback is the dev frontend, not this process.
	emailLinkBase := envOr("FRONTEND_URL", envOr("PUBLIC_URL", "http://localhost:3000"))
	// Sign-up email verification (SEC-15 / REQ-95): enforced only when the
	// mailer can send and the operator has not switched it off, so a stack
	// with no SMTP is unchanged. The policy reaches the user service (new
	// accounts start unverified), the handler (sends the link) and the auth
	// middleware (walls unverified sessions).
	emailVerification := notify.VerificationPolicyFromEnv(emailMailer)
	userService.SetEmailVerificationPolicy(emailVerification)
	// Session lifetime (REQ-99): an absolute deadline and an idle one, both
	// operator-shortenable, neither extendable past the defaults.
	sessionPolicy := users.SessionPolicyFromEnv()
	userService.SetSessionPolicy(sessionPolicy)
	// Registration policy (REQ-95): open unless the operator closes it.
	registrationPolicy := api.RegistrationPolicyFromEnv()
	emailDispatcher := notify.NewEmailDispatcher(emailMailer, userService, emailLinkBase, notify.EmailTypesFromEnv())

	// Optional web push side channel for the same high-signal types (REQ-109).
	// Also strictly opt-in: with no OPENV_VAPID_* key pair the dispatcher has
	// no sender, /api/v1/me/push/config reports enabled=false and nothing is
	// ever sent. Deep links use the same frontend base as the emails.
	vapid := notify.VAPIDFromEnv()
	pushSubService := pushsubs.NewDefaultService(pushSubRepo)
	var pushSender notify.PushSender
	if vapid.Enabled() {
		// An explicit client: webpush-go's fallback is a bare http.Client
		// with no timeout, which would let a push service that stops
		// answering hold a dispatcher worker indefinitely.
		pushSender = notify.NewWebPushSender(vapid, notify.DefaultPushHTTPClient())
	}
	// Push deep links are same-origin paths resolved by the service worker,
	// so unlike the emails above the dispatcher needs no base URL.
	pushDispatcher := notify.NewPushDispatcher(pushSender, pushSubService, userService, notify.PushTypesFromEnv())

	// Notification fan-out: bus events become per-user inbox rows plus live
	// SSE pushes on notify:<user_id> (issue #132), plus a best-effort email
	// for eligible types when the recipient is opted in and SMTP is on (#187).
	notificationService := notifications.NewDefaultService(notificationRepo)
	notify.NewNotifier(notificationService, memberService, sseHub).
		SetEmailDispatcher(emailDispatcher).
		SetPushDispatcher(pushDispatcher).
		// Membership and privilege changes: the affected member hears what
		// changed about their own access, and the workspace's admins hear who
		// joined and who left.
		SetOrgService(orgService).
		SetUserNamer(notify.UserNamerFunc(func(userID string) string {
			user, err := userService.GetByID(userID)
			if err != nil || user == nil {
				return ""
			}
			return user.Name
		})).
		Start(bus)

	// Workspace budget alerts (issue #186): a finishing run's cost can push
	// month-to-date spend across 80%/100% of the org's monthly budget; the
	// monitor alerts org admins once per threshold per month. Warn-only.
	notify.NewBudgetMonitor(orgService, runService, notificationService, sseHub).
		SetEmailDispatcher(emailDispatcher).
		SetPushDispatcher(pushDispatcher).
		Start(bus)

	// The running release: RELEASE_NOTES.md as built into this binary. Its
	// top dated section is what GET /api/v1/release reports and what every
	// account is told about, once per release, when a server first boots on
	// it. A notes file that fails to parse is logged and serves an empty
	// release rather than keeping the API down over documentation.
	releaseService, err := release.NewService(openv.ReleaseNotesMarkdown)
	if err != nil {
		slog.Error("release notes failed to parse; serving no release", "error", err)
		releaseService = release.Empty()
	}
	if cur := releaseService.Current(); cur != nil {
		slog.Info("release", "version", cur.Version)
		announcer := notify.NewReleaseAnnouncer(postgres.NewReleaseRepository(db), userService, notificationService, sseHub).
			SetEmailDispatcher(emailDispatcher).
			SetPushDispatcher(pushDispatcher)
		go announcer.Announce(cur)
	}

	// Optional over-budget soft-block (default OFF — warn-only). When
	// OPENV_BUDGET_ENFORCE=true, new launches are refused once a workspace has
	// hit 100% of its monthly budget. Fails open on lookup errors so a budget
	// hiccup never blocks work.
	if os.Getenv("OPENV_BUDGET_ENFORCE") == "true" {
		runService.SetBudgetGuard(func(orgID string) (bool, string) {
			org, err := orgService.Get(orgID)
			if err != nil || org == nil || org.MonthlyBudgetUSD == nil || *org.MonthlyBudgetUSD <= 0 {
				return false, ""
			}
			now := time.Now().UTC()
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			spend, err := runService.MonthlySpend(orgID, monthStart)
			if err != nil || spend < *org.MonthlyBudgetUSD {
				return false, ""
			}
			return true, fmt.Sprintf("this workspace has reached its $%.2f monthly budget ($%.2f spent); new runs are blocked until next month or the budget is raised", *org.MonthlyBudgetUSD, spend)
		})
		slog.Info("workspace budget enforcement enabled: launches soft-block at 100% of budget")
	}

	// Trigger matcher + scheduler + reaper. The scheduler and reaper loops
	// stop when the signal context is canceled.
	automation.NewTriggerMatcher(automationRepo, runService, teamService).Start(bus)
	scheduler.New(automationRepo, runService, teamService).Start(ctx)

	// Workspace purge: hard-delete workspaces whose soft-delete grace period
	// (orgs.DeletionGraceDays) has expired — once at boot, then daily.
	go func() {
		purge := func() {
			if ids, err := orgService.PurgeExpired(time.Now()); err != nil {
				slog.Error("workspace purge failed", "error", err, "purged", len(ids))
			} else if len(ids) > 0 {
				slog.Info("purged expired deleted workspaces", "count", len(ids), "ids", ids)
			}
		}
		purge()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				purge()
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ids, err := runService.FailStale(2 * time.Minute); err != nil {
					slog.Error("reaper FailStale failed", "error", err)
				} else if len(ids) > 0 {
					slog.Warn("reaper failed stale runs", "count", len(ids))
				}
				_ = userRepo.DeleteExpiredSessions(time.Now(), sessionPolicy.MaxAge, sessionPolicy.Idle)
				// Invitations that nobody accepted expire; the rows are of
				// no further use to anyone.
				_ = invitationService.PurgeExpired(time.Now())
				// Transient runners: end lapsed leases (hard expiry, idle
				// window, or a node that stopped heartbeating) so their
				// nodes go back to the pool and their credentials die.
				if runnerSessionService != nil {
					if ended, err := runnerSessionService.Sweep(time.Now()); err != nil {
						slog.Error("runner session sweep failed", "error", err)
					} else if len(ended) > 0 {
						slog.Info("ended lapsed runner sessions", "count", len(ended))
					}
				}
			}
		}
	}()

	// Google OAuth (optional).
	var googleOAuth *api.GoogleOAuthConfig
	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		publicURL := envOr("PUBLIC_URL", "http://localhost:"+port)
		googleOAuth = &api.GoogleOAuthConfig{
			ClientID:     clientID,
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  publicURL + "/api/v1/auth/google/callback",
			FrontendURL:  envOr("FRONTEND_URL", "http://localhost:3000"),
		}
	}

	// Generic OIDC single sign-on (optional, issue #225). One IdP per
	// deployment; strictly opt-in — with OPENV_OIDC_ISSUER unset the endpoints
	// report "not configured" and the default deployment is unaffected.
	var oidcConfig *api.OIDCConfig
	if issuer := os.Getenv("OPENV_OIDC_ISSUER"); issuer != "" {
		publicURL := envOr("PUBLIC_URL", "http://localhost:"+port)
		redirectURL := envOr("OPENV_OIDC_REDIRECT_URL", publicURL+"/api/v1/auth/oidc/callback")
		var scopes []string
		if raw := os.Getenv("OPENV_OIDC_SCOPES"); raw != "" {
			scopes = strings.Fields(raw)
		}
		oidcConfig = &api.OIDCConfig{
			Issuer:       issuer,
			ClientID:     os.Getenv("OPENV_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OPENV_OIDC_CLIENT_SECRET"),
			RedirectURL:  redirectURL,
			Scopes:       scopes,
			ProviderName: envOr("OPENV_OIDC_NAME", "SSO"),
			FrontendURL:  envOr("FRONTEND_URL", "http://localhost:3000"),
		}
	}

	// Handler.
	handler := api.NewHandler(api.HandlerDeps{
		ArtifactService:      artifactService,
		LinkService:          linkService,
		ProjectService:       projectService,
		AttachmentService:    attachmentService,
		ExportService:        exportService,
		DownloadService:      downloadService,
		BaselineService:      baselineService,
		ReportService:        reportService,
		TemplateService:      templateService,
		ChatterService:       chatterService,
		AttributeService:     attributeService,
		SharedProductService: sharedProductService,
		EmbeddingService:     embeddingService,
		UploadsDir:           uploadsDir,
		UserService:          userService,
		MemberService:        memberService,
		ProductService:       productService,
		VVService:            vvService,
		EvidenceService:      evidenceService,
		SettingsService:      settingsService,
		ReleaseService:       releaseService,
		WorkItemService:      workItemService,
		GuidedService:        guidedService,
		InterviewService:     interviewService,
		AgentService:         agentService,
		RunService:           runService,
		AutomationService:    automationService,
		ProposalService:      proposalService,
		RepoConnService:      repoConnService,
		ProviderService:      providerService,
		LoginService:         loginService,
		OrgService:           orgService,
		OrgTeamService:       orgTeamService,
		WorkerKeyService:     workerKeyService,
		HostedWorkerService:  hostedWorkerService,
		RunnerSessionService: runnerSessionService,
		NotificationService:  notificationService,
		PushSubService:       pushSubService,
		VAPID:                vapid,
		Provisioner:          provisioner,
		OrgSeeder: func(orgID string) error {
			return seeds.EnsureOrgDefaults(orgID, agentService, teamService)
		},
		TeamService:       teamService,
		Bus:               bus,
		EventRepo:         eventRepo,
		SSEHub:            sseHub,
		GoogleOAuth:       googleOAuth,
		OIDC:              oidcConfig,
		SecureCookies:     os.Getenv("SECURE_COOKIES") == "true",
		CrossSiteCookies:  os.Getenv("CROSS_SITE_COOKIES") == "true",
		PublicAPIURL:      envOr("PUBLIC_URL", "http://localhost:"+port),
		ConnectorDistDir:  envOr("CONNECTOR_DIST_DIR", "./dist"),
		Mailer:            emailMailer,
		EmailLinkBase:     emailLinkBase,
		EmailVerification: emailVerification,
		InvitationService: invitationService,
		Registration:      registrationPolicy,
		SessionPolicy:     sessionPolicy,
	})

	// Close the construction cycle: the proposal appliers run the handler's
	// own domain writes (events, link-snapshot auto-versioning) when a human
	// approves a proposal. Done before the server starts serving.
	proposalService.SetAppliers(handler.ProposalAppliers())
	// Same cycle, other direction: a wizard nudge parked while a copilot run
	// was in flight is launched by the handler when the hooks see that run
	// finish.
	hooks.SetGuidedNudgeLauncher(handler)

	// Router + middleware.
	router := mux.NewRouter()
	router.Use(api.ContentTypeMiddleware)
	handler.RegisterRoutes(router)

	// Prometheus scrape endpoint. Unauthenticated by default (firewall it to an
	// internal network in production); set OPENV_METRICS_TOKEN to require an
	// "Authorization: Bearer <token>" header. Registered as an open path in the
	// auth middleware so scraping is never blocked by session auth.
	router.Handle("/metrics", metricsCollector.Handler(os.Getenv("OPENV_METRICS_TOKEN"))).Methods("GET")

	authMiddleware := api.NewAuthMiddleware(userService, runService, orgService, workerKeyService, workerKey, bootstrapOrgID)
	authMiddleware.SetPoolKey(runnerPoolKey)
	authMiddleware.SetEmailVerificationPolicy(emailVerification)
	// Request logging wraps outside auth so rejected requests are logged too;
	// auth annotates the log line with the resolved org/user. The metrics HTTP
	// middleware sits between them, recording every request (including rejected
	// ones) labelled by mux route template; it is a distinct concern from the
	// access log and does not double-count.
	protected := api.RequestLogMiddleware(
		metricsCollector.HTTPMiddleware(router)(authMiddleware.Wrap(router)),
	)

	// Compression sits outside all of that so it sees the finished response,
	// whichever layer produced it. It leaves event streams alone — the API
	// holds SSE connections open for minutes and a compressor would batch
	// their events instead of delivering them — and skips anything too small
	// to be worth the header. The payload it exists for is a baseline
	// snapshot: a whole project export, which is close to a megabyte of JSON
	// for a real project and about a fifth of that compressed.
	protected = api.CompressionMiddleware(protected)

	// CORS: restricted to the configured frontend origin, with credentials.
	// A wildcard is refused at startup rather than reflected (see
	// api.CORSMiddleware).
	corsHandler, err := api.CORSMiddleware(envOr("CORS_ORIGIN", "http://localhost:3000"), protected)
	if err != nil {
		fatal("invalid CORS_ORIGIN", err)
	}
	// Outermost: cap every request body, then stamp the browser-hardening
	// headers on every response, including CORS preflights and rejections.
	// HSTS follows SECURE_COOKIES, the deployment's declaration that it is
	// only reached over TLS.
	secureDeployment := os.Getenv("SECURE_COOKIES") == "true" || os.Getenv("CROSS_SITE_COOKIES") == "true"
	rootHandler := api.SecurityHeadersMiddleware(secureDeployment)(
		api.BodyLimitMiddleware(maxRequestBodyBytes())(corsHandler),
	)

	// HTTP server. ReadHeaderTimeout defends against slowloris-style clients
	// holding connections open while trickling headers; IdleTimeout reclaims
	// idle keep-alive connections. ReadTimeout and WriteTimeout deliberately
	// stay 0 (unlimited): the API serves long-lived SSE streams (e.g.
	// /api/v1/agent-runs/{id}/stream, guided chat and interview streams)
	// that hold a response open indefinitely, and a nonzero WriteTimeout
	// is an absolute
	// deadline that would sever every stream after it elapsed. Slow-client
	// abuse on the read side is already bounded by ReadHeaderTimeout plus
	// per-handler request parsing.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// Derive request contexts from the signal context so long-lived SSE
		// handlers (which select on r.Context().Done()) exit promptly on
		// shutdown instead of pinning the drain for its full timeout.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting server", "port", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			fatal("failed to start server", err)
		}
	case <-ctx.Done():
		stop() // restore default signal behavior: a second Ctrl-C kills immediately
		slog.Info("shutdown signal received; draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("graceful shutdown incomplete", "error", err)
			_ = srv.Close()
		}
		<-errCh // wait for ListenAndServe to return
		slog.Info("server stopped")
	}
}

// maxRequestBodyBytes is the cap the API places on any single request body.
// OPENV_MAX_BODY_MB overrides the 32 MB default; attachment uploads carry a
// tighter cap of their own (OPENV_MAX_UPLOAD_MB).
func maxRequestBodyBytes() int64 {
	mb := int64(32)
	if v := os.Getenv("OPENV_MAX_BODY_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			mb = n
		}
	}
	return mb * 1024 * 1024
}
