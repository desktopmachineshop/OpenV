package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// signals installs the logger and makes the root context, canceled on
// SIGINT or SIGTERM so that the background loops and the HTTP server shut
// down gracefully; it returns the context's stop for main() to defer.
func (a *app) signals() context.CancelFunc {
	initLogging()

	// Root context: canceled on SIGINT/SIGTERM so background loops and the
	// HTTP server can shut down gracefully.
	a.ctx, a.stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return a.stop
}

// config reads the database, port and directory settings and the
// workspace limits, and creates the uploads directory.
func (a *app) config() {
	// Database connection: Railway-style DATABASE_URL or individual vars.
	if databaseURL := envSecret("DATABASE_URL", ""); databaseURL != "" {
		slog.Info("using DATABASE_URL (Railway.app mode)")
		a.dsn = databaseURL
	} else {
		slog.Info("using individual environment variables (local development mode)")
		a.dsn = postgres.ConnString(
			envOr("DB_HOST", "localhost"), envOr("DB_PORT", "5432"), envOr("DB_USER", "postgres"),
			envSecret("DB_PASSWORD", "postgres"), envOr("DB_NAME", "openv"))
	}

	a.port = envOr("PORT", "8080")
	a.uploadsDir = envOr("UPLOADS_DIR", "./uploads")
	// OPENV_UPLOAD_SWEEP=off skips the boot's once-only sweep of the stored
	// files no row names (stage storage), on a deployment whose uploads
	// directory another one shares (#379 question 55).
	a.uploadSweep = envSwitch(uploadSweepSetting, true)
	dataDir := envOr("OPENV_DATA_DIR", "./data")
	a.agentsDir = envOr("AGENTS_DIR", dataDir+"/agents")
	// WORKER_API_KEY is a legacy single-key fallback; workers should use
	// org-scoped keys minted in workspace settings.
	a.workerKey = envSecret("WORKER_API_KEY", "")

	// Workspace limits. A deployment somebody runs themselves owns its own
	// hardware, so it sets its own ceilings: OPENV_SELF_HOSTED picks the plan
	// new workspaces are created on AND the remedy a refusal offers, because
	// telling a self-hoster to upgrade a plan they do not have would send
	// them nowhere. OPENV_LIMITS then retunes any individual limit across the
	// whole deployment without touching the database.
	a.selfHosted = envBool("OPENV_SELF_HOSTED", false)
	orgs.SetSelfHosted(a.selfHosted)
	// OPENV_PLAN_DEFAULT names the plan new workspaces are created on in
	// place of the deployment's own default. A name that is not a plan
	// keeps that default, with one warning naming the variable (#379,
	// question 15's rule), and the boot log below shows the plan used.
	defaultPlan := orgs.PlanSingle
	if a.selfHosted {
		defaultPlan = orgs.PlanSelfHost
	}
	if named := envOr("OPENV_PLAN_DEFAULT", ""); named != "" {
		if orgs.ValidPlan(named) {
			defaultPlan = named
		} else {
			slog.Warn("ignoring an unknown plan; the deployment's default plan applies",
				"var", "OPENV_PLAN_DEFAULT", "want", "one of "+strings.Join(orgs.PlanNames(), ", "))
		}
	}
	orgs.SetDefaultPlan(defaultPlan)
	// A malformed OPENV_LIMITS is fatal rather than ignored: a typo that
	// silently did nothing would be indistinguishable from a limit that does
	// not work, and the operator would discover it when somebody was wrongly
	// refused.
	deploymentLimits, err := orgs.ParseLimits(envOr("OPENV_LIMITS", ""))
	if err != nil {
		fatal("OPENV_LIMITS is not usable", err)
	}
	orgs.SetDeploymentLimits(deploymentLimits)
	slog.Info("workspace limits configured",
		"self_hosted", a.selfHosted, "default_plan", defaultPlan, "overrides", len(deploymentLimits))

	if err := os.MkdirAll(a.uploadsDir, 0o755); err != nil {
		fatal("failed to create uploads directory", err)
	}
}
