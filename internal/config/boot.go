package config

// The settings of the boot stages before notify: signals, config, core,
// workspace, runners, projects and agents, then release and jobs. Each
// accessor names the statement of cmd/server (or of the package helper a
// stage calls) whose read it reproduces.

import (
	"log/slog"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/envparse"
)

// LogLevel is OPENV_LOG_LEVEL as initLogging (stage signals) reads it,
// trimmed and lower-cased: debug; warn or warning; error; info, or nothing,
// for info. Any other value reads as info too, and unrecognized is then
// true, for today's warning, which names the variable, never the value
// (#379, bug 222). LogLevel logs nothing: that warning waits until the
// logger the level configures is installed, so the caller logs it.
func (c *Config) LogLevel() (level slog.Level, unrecognized bool) {
	switch strings.ToLower(strings.TrimSpace(c.getenv("OPENV_LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug, false
	case "warn", "warning":
		return slog.LevelWarn, false
	case "error":
		return slog.LevelError, false
	case "", "info":
		return slog.LevelInfo, false
	}
	return slog.LevelInfo, true
}

// DatabaseURL is DATABASE_URL exactly as set, or "" (stage config's first
// read). When it is not "", the server connects with it and reads no DB_*
// setting.
func (c *Config) DatabaseURL() string {
	return c.secret("DATABASE_URL", "")
}

// DatabaseParts are the DB_* settings stage config hands postgres.ConnString
// when DatabaseURL is "", read after it logs that it uses them, in the
// order of ConnString's arguments.
type DatabaseParts struct {
	Host, Port, User, Password, Name string
}

// DatabaseParts reads DB_HOST, DB_PORT, DB_USER, DB_PASSWORD (a credential,
// exactly as set) and DB_NAME, each with today's default.
func (c *Config) DatabaseParts() DatabaseParts {
	return DatabaseParts{
		Host:     c.text("DB_HOST", "localhost"),
		Port:     c.text("DB_PORT", "5432"),
		User:     c.text("DB_USER", "postgres"),
		Password: c.secret("DB_PASSWORD", "postgres"),
		Name:     c.text("DB_NAME", "openv"),
	}
}

// Port is the port the server listens on (PORT, default 8080), and the one
// the PUBLIC_URL fallbacks name.
func (c *Config) Port() string {
	return c.text("PORT", "8080")
}

// UploadsDir is the server's uploads directory (UPLOADS_DIR, default
// ./uploads), which stage config creates and the handlers store files in.
func (c *Config) UploadsDir() string {
	return c.text("UPLOADS_DIR", "./uploads")
}

// UploadsDirRaw is UPLOADS_DIR as the reports' attachment lookup reads it,
// trimmed, with no default: "" when unset. That read is made on every
// report and stays there (S8's per-request exemption); UploadsDirRaw keeps
// its value apart from UploadsDir, whose default it does not have (quirk
// Q12).
func (c *Config) UploadsDirRaw() string {
	return strings.TrimSpace(c.getenv("UPLOADS_DIR"))
}

// UploadSweep is whether stage storage sweeps the stored files no row names
// (OPENV_UPLOAD_SWEEP, a switch, on by default).
func (c *Config) UploadSweep() bool {
	return c.onOff("OPENV_UPLOAD_SWEEP", true)
}

// DataDir is OPENV_DATA_DIR (default ./data), which only AgentsDir's
// default uses.
func (c *Config) DataDir() string {
	return c.text("OPENV_DATA_DIR", "./data")
}

// AgentsDir is AGENTS_DIR, by default DataDir's agents directory.
func (c *Config) AgentsDir() string {
	return c.text("AGENTS_DIR", c.DataDir()+"/agents")
}

// WorkerAPIKey is the legacy single worker key (WORKER_API_KEY, a
// credential), "" when unset.
func (c *Config) WorkerAPIKey() string {
	return c.secret("WORKER_API_KEY", "")
}

// SelfHosted is OPENV_SELF_HOSTED (a boolean, false by default).
func (c *Config) SelfHosted() bool {
	return c.boolean("OPENV_SELF_HOSTED", false)
}

// DefaultPlan is the plan new workspaces are created on: OPENV_PLAN_DEFAULT
// when it names a plan, otherwise the deployment's own default, self_host on
// a self-hosted install and single elsewhere. A value that names no plan
// keeps that default and logs today's warning, naming the variable, never
// the value. Stage config reads it after SelfHosted, whose warning a
// malformed OPENV_SELF_HOSTED then does not repeat.
func (c *Config) DefaultPlan() string {
	plan := orgs.PlanSingle
	if c.SelfHosted() {
		plan = orgs.PlanSelfHost
	}
	if named := c.text("OPENV_PLAN_DEFAULT", ""); named != "" {
		if orgs.ValidPlan(named) {
			plan = named
		} else {
			slog.Warn("ignoring an unknown plan; the deployment's default plan applies",
				"var", "OPENV_PLAN_DEFAULT", "want", "one of "+strings.Join(orgs.PlanNames(), ", "))
		}
	}
	return plan
}

// DeploymentLimits are OPENV_LIMITS, trimmed, as orgs.ParseLimits reads
// them: nil when unset. Its error is fatal in stage config, before the
// server connects to the database ("OPENV_LIMITS is not usable").
func (c *Config) DeploymentLimits() (map[string]interface{}, error) {
	return orgs.ParseLimits(c.text("OPENV_LIMITS", ""))
}

// Embeddings are the semantic-search embedding settings: the
// OPENV_EMBEDDING_API_KEY credential stage core reads, and the base URL and
// model embeddings.ProviderFromEnv reads with it.
type Embeddings struct {
	APIKey  string // exactly as set; "" leaves embeddings off, as one of only spaces does
	BaseURL string // trimmed, without trailing slashes
	Model   string // trimmed
}

// Embeddings reads the key, then the base URL (default
// https://api.openai.com/v1) and the model (default
// embeddings.DefaultModel), as ProviderFromEnv does. The lines about the
// provider stay with the code that builds it.
func (c *Config) Embeddings() Embeddings {
	key := c.secret("OPENV_EMBEDDING_API_KEY", "")
	baseURL := strings.TrimRight(strings.TrimSpace(c.getenv("OPENV_EMBEDDING_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(c.getenv("OPENV_EMBEDDING_MODEL"))
	if model == "" {
		model = embeddings.DefaultModel
	}
	return Embeddings{APIKey: key, BaseURL: baseURL, Model: model}
}

// GrandfatherBefore is OPENV_BILLING_GRANDFATHER_BEFORE as stage workspace
// reads it: trimmed, and parsed as an RFC 3339 date-time only when it is set
// and the install is not self-hosted, so a malformed date on a self-hosted
// install is ignored and the server boots. on reports that the date applies
// (the plan tiers turn on); err is time.Parse's error for a malformed one,
// which is fatal in stage workspace, after the migrations (the variable
// "must be an RFC 3339 date-time").
func (c *Config) GrandfatherBefore() (cutoff time.Time, on bool, err error) {
	raw := strings.TrimSpace(c.getenv("OPENV_BILLING_GRANDFATHER_BEFORE"))
	if raw == "" || c.SelfHosted() {
		return time.Time{}, false, nil
	}
	cutoff, err = time.Parse(time.RFC3339, raw)
	return cutoff, true, err
}

// RunnerPoolKey is the transient runner pool's shared key (RUNNER_POOL_KEY,
// a credential), "" when unset, which leaves transient runners off.
func (c *Config) RunnerPoolKey() string {
	return c.secret("RUNNER_POOL_KEY", "")
}

// HostedRunnersOff is whether HOSTED_RUNNERS is off, in any case, which
// hosting.NewProvisioner reads first and which keeps it from dialling
// Docker. Any other value leaves hosted runners on, with today's warning.
func (c *Config) HostedRunnersOff() bool {
	return envparse.Off("HOSTED_RUNNERS", c.text("HOSTED_RUNNERS", ""))
}

// RunnerContainer is what a hosted runner container is started from,
// which hosting reads once Docker answers.
type RunnerContainer struct {
	Image   string // RUNNER_IMAGE
	Network string // RUNNER_NETWORK; "" is the default bridge
	APIURL  string // RUNNER_API_URL, the API as seen from inside the container
}

// RunnerContainer reads RUNNER_IMAGE, RUNNER_NETWORK and RUNNER_API_URL with
// hosting's defaults, in that order.
func (c *Config) RunnerContainer() RunnerContainer {
	return RunnerContainer{
		Image:   c.text("RUNNER_IMAGE", "openv-worker:latest"),
		Network: c.text("RUNNER_NETWORK", ""),
		APIURL:  c.text("RUNNER_API_URL", "http://api:8080"),
	}
}

// HostedRunnerPidsLimit is a hosted runner container's process cap as
// hosting.PidsLimit reads HOSTED_RUNNER_PIDS_LIMIT: a whole number of any
// sign, 1024 by default, and 0 (no cap) for 0 or less, the one count whose
// zero keeps a meaning of its own. hosting reads it once at boot when
// Docker answers and again for each runner it provisions.
func (c *Config) HostedRunnerPidsLimit() int64 {
	n := envparse.Number("HOSTED_RUNNER_PIDS_LIMIT", c.getenv("HOSTED_RUNNER_PIDS_LIMIT"), 1024)
	if n <= 0 {
		return 0
	}
	return int64(n)
}

// SharedProductDailyLimit is how many products one workspace may publish to
// the community pool a day (OPENV_SHARED_PRODUCT_DAILY_LIMIT, a count).
func (c *Config) SharedProductDailyLimit() int {
	return c.count("OPENV_SHARED_PRODUCT_DAILY_LIMIT", sharedproducts.DefaultDailyOrgLimit)
}

// SharedProductPoolLimit caps the community pool
// (OPENV_SHARED_PRODUCT_POOL_LIMIT, a count).
func (c *Config) SharedProductPoolLimit() int {
	return c.count("OPENV_SHARED_PRODUCT_POOL_LIMIT", sharedproducts.DefaultPoolLimit)
}

// RunMaxAttempts caps a run's retry chain (OPENV_RUN_MAX_ATTEMPTS, a count,
// agentruns.DefaultMaxAttempts by default).
func (c *Config) RunMaxAttempts() int {
	return c.count("OPENV_RUN_MAX_ATTEMPTS", agentruns.DefaultMaxAttempts)
}

// RunAutoRetry is whether a retryable failure is retried
// (OPENV_RUN_AUTO_RETRY, a boolean, true by default).
func (c *Config) RunAutoRetry() bool {
	return c.boolean("OPENV_RUN_AUTO_RETRY", true)
}

// Deployment is the deployment's kind (OPENV_DEPLOYMENT, default shared);
// dedicated starts the support-window watcher.
func (c *Config) Deployment() string {
	return c.text("OPENV_DEPLOYMENT", "shared")
}

// ReleaseFeedURL is the public release feed a dedicated instance reads
// (OPENV_RELEASE_FEED_URL).
func (c *Config) ReleaseFeedURL() string {
	return c.text("OPENV_RELEASE_FEED_URL", "https://api.openv.app/api/v1/public/release")
}

// BuildSHA is the commit /health reports: OPENV_BUILD_SHA, else
// RAILWAY_GIT_COMMIT_SHA, else "".
func (c *Config) BuildSHA() string {
	return c.text("OPENV_BUILD_SHA", c.text("RAILWAY_GIT_COMMIT_SHA", ""))
}

// BudgetEnforce is whether launches soft-block at 100% of a workspace's
// budget (OPENV_BUDGET_ENFORCE, a boolean, false by default).
func (c *Config) BudgetEnforce() bool {
	return c.boolean("OPENV_BUDGET_ENFORCE", false)
}
