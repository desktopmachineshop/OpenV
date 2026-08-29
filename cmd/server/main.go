package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"

	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/automation"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/templates"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/domain/workitems"
	eventbus "github.com/openv/requirements-platform/internal/events"
	"github.com/openv/requirements-platform/internal/hosting"
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
	workerKeyRepo := postgres.NewWorkerKeyRepository(db)
	hostedWorkerRepo := postgres.NewHostedWorkerRepository(db)

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
	projectService := projects.NewService(projectRepo)
	attachmentService := attachments.NewDefaultService(attachmentRepo)
	baselineService := baselines.NewService(baselineRepo)
	chatterService := chatter.NewDefaultService(chatterRepo)
	exportService := exports.NewService(artifactService, linkService, attachmentService, projectInfoRepo, projectService)
	reportService := reports.NewService(exportService, baselineService)
	templateService := templates.NewService(templateRepo, exportService)
	if err := templateService.SeedDefaults(); err != nil {
		slog.Warn("failed to seed templates", "error", err)
	}

	// Suite services.
	userService := users.NewDefaultService(userRepo)
	memberService := members.NewDefaultService(memberRepo)
	orgService := orgs.NewDefaultService(orgRepo)
	orgTeamService := orgs.NewTeamService(orgRepo, orgService)
	workerKeyService := workerkeys.NewDefaultService(workerKeyRepo)
	workerKeyService.SetPairingRepository(workerKeyRepo)
	hostedWorkerService := hostedworkers.NewDefaultService(hostedWorkerRepo)

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
	vvService := vv.NewDefaultService(vvRepo, artifactService, chatterService, bus)
	workItemService := workitems.NewDefaultService(workItemRepo, bus)
	guidedService := guided.NewDefaultService(guidedRepo, artifactService, linkService, chatterService, productService, bus)
	interviewService := interviews.NewDefaultService(interviewRepo)

	// Agent engine services.
	agentService, err := agents.NewFileService(agentsDir, agentRepo)
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
	proposalService := proposals.NewDefaultService(proposalRepo, proposals.Appliers{
		CreateArtifact: func(payload map[string]interface{}) (string, error) {
			var req artifacts.CreateArtifactRequest
			if err := decodePayload(payload, &req); err != nil {
				return "", err
			}
			artifact := artifacts.NewArtifact(req)
			if err := artifactService.CreateArtifact(artifact); err != nil {
				return "", err
			}
			return artifact.ID, nil
		},
		UpdateArtifact: func(targetID string, payload map[string]interface{}) (string, error) {
			var req artifacts.UpdateArtifactRequest
			if err := decodePayload(payload, &req); err != nil {
				return "", err
			}
			updated, err := artifactService.UpdateArtifact(targetID, req)
			if err != nil {
				return "", err
			}
			return updated.ID, nil
		},
		DeleteArtifact: func(targetID string) error {
			return artifactService.DeleteArtifact(targetID)
		},
		CreateLink: func(payload map[string]interface{}) (string, error) {
			var req links.CreateLinkRequest
			if err := decodePayload(payload, &req); err != nil {
				return "", err
			}
			link := links.NewLink(req)
			if err := linkService.CreateLink(link); err != nil {
				return "", err
			}
			return link.ID, nil
		},
		DeleteLink: func(targetID string) error {
			return linkService.DeleteLink(targetID)
		},
		RecordTestResult: func(payload map[string]interface{}) (string, error) {
			runID, _ := payload["run_id"].(string)
			if runID == "" {
				return "", errors.New("record_test_result payload requires run_id")
			}
			var req vv.UpsertResultRequest
			if err := decodePayload(payload, &req); err != nil {
				return "", err
			}
			// Applying an approved proposal: a human signed off on this
			// result, so it is not stamped as agent-executed.
			result, err := vvService.UpsertResult(runID, req, nil, "system", "")
			if err != nil {
				return "", err
			}
			return result.ID, nil
		},
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

	// SSE hub + orchestration hooks.
	sseHub := api.NewSSEHub()
	runService.AddSubscriber(sseHub)
	hooks := orchestration.NewHooks(runService, teamService, workItemService, interviewService, guidedService, projectService, sseHub)
	runService.AddSubscriber(hooks)
	hooks.SubscribeBus(bus)

	// Trigger matcher + scheduler + reaper. The scheduler and reaper loops
	// stop when the signal context is canceled.
	automation.NewTriggerMatcher(automationRepo, runService, teamService).Start(bus)
	scheduler.New(automationRepo, runService, teamService).Start(ctx)
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
				_ = userRepo.DeleteExpiredSessions(time.Now())
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

	// Handler.
	handler := api.NewHandler(api.HandlerDeps{
		ArtifactService:     artifactService,
		LinkService:         linkService,
		ProjectService:      projectService,
		AttachmentService:   attachmentService,
		ExportService:       exportService,
		BaselineService:     baselineService,
		ReportService:       reportService,
		TemplateService:     templateService,
		ChatterService:      chatterService,
		UploadsDir:          uploadsDir,
		UserService:         userService,
		MemberService:       memberService,
		ProductService:      productService,
		VVService:           vvService,
		WorkItemService:     workItemService,
		GuidedService:       guidedService,
		InterviewService:    interviewService,
		AgentService:        agentService,
		RunService:          runService,
		AutomationService:   automationService,
		ProposalService:     proposalService,
		RepoConnService:     repoConnService,
		ProviderService:     providerService,
		LoginService:        loginService,
		OrgService:          orgService,
		OrgTeamService:      orgTeamService,
		WorkerKeyService:    workerKeyService,
		HostedWorkerService: hostedWorkerService,
		Provisioner:         provisioner,
		OrgSeeder: func(orgID string) error {
			return seeds.EnsureOrgDefaults(orgID, agentService, teamService)
		},
		TeamService:   teamService,
		Bus:           bus,
		EventRepo:     eventRepo,
		SSEHub:        sseHub,
		GoogleOAuth:      googleOAuth,
		SecureCookies:    os.Getenv("SECURE_COOKIES") == "true",
		PublicAPIURL:     envOr("PUBLIC_URL", "http://localhost:"+port),
		ConnectorDistDir: envOr("CONNECTOR_DIST_DIR", "./dist"),
	})

	// Router + middleware.
	router := mux.NewRouter()
	router.Use(api.ContentTypeMiddleware)
	handler.RegisterRoutes(router)

	authMiddleware := api.NewAuthMiddleware(userService, runService, orgService, workerKeyService, workerKey, bootstrapOrgID)
	// Request logging wraps outside auth so rejected requests are logged too;
	// auth annotates the log line with the resolved org/user.
	protected := api.RequestLogMiddleware(authMiddleware.Wrap(router))

	// CORS: restricted to the configured frontend origin, with credentials.
	corsOrigin := envOr("CORS_ORIGIN", "http://localhost:3000")
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == corsOrigin || corsOrigin == "*" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Org-ID")
			w.Header().Set("Access-Control-Max-Age", "3600")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		protected.ServeHTTP(w, r)
	})

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
		Handler:           corsHandler,
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

// decodePayload converts a generic JSON payload into a typed request.
func decodePayload(payload map[string]interface{}, out interface{}) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
