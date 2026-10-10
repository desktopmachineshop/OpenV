package config

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/notify"
)

// accessorCase is one way the server reads one row of S8's inventory
// (env_vars.txt), and the accessor that stands for that read. A row read at
// several call sites has a case per call site (quirk Q12).
type accessorCase struct {
	name     string            // the variable
	read     string            // its read column in env_vars.txt
	accessor string            // the accessor, as the report names it
	with     map[string]string // other variables set: the condition the read is made under
	def      any               // the accessor's value with name unset
	get      func(*Config) any // the accessor's value for name (an error as itself)
	// pinned, when set, is the read alone, which env_parse.txt pins, where
	// the accessor parses further than that read (inline after an os.Getenv,
	// or after the getter, as today's statement does); the further parse is
	// pinned against today's code by helpers_test.go and pairs_test.go.
	pinned func(*Config) any
	// computed is set when the read's default is computed (env_vars.txt
	// shows (computed)), not a constant.
	computed bool
}

const (
	googleRedirectPath = "/api/v1/auth/google/callback"
	oidcRedirectPath   = "/api/v1/auth/oidc/callback"
)

var (
	googleOn = map[string]string{"GOOGLE_CLIENT_ID": "client-id"}
	oidcOn   = map[string]string{"OPENV_OIDC_ISSUER": "https://idp.example.test"}
)

// raw is the pinned read of an os.Getenv row: the value exactly as set.
func raw(name string) func(*Config) any {
	return func(c *Config) any { return c.getenv(name) }
}

// billingField is one field of Billing(), or its error.
func billingField(field func(billing.Config) any) func(*Config) any {
	return func(c *Config) any {
		cfg, err := c.Billing()
		if err != nil {
			return err
		}
		return field(cfg)
	}
}

// rateCases are the two cases of one of api.NewHandler's limiters.
func rateCases(burstVar, refillVar, field string, defBurst int, defRefill float64, pick func(RateLimits) RateLimit) []accessorCase {
	return []accessorCase{
		{name: burstVar, read: "internal/api:newRateLimiterFromEnv(burstVar)", accessor: "RateLimits()." + field + ".Burst",
			def: defBurst, get: func(c *Config) any { return pick(c.RateLimits()).Burst }},
		{name: refillVar, read: "internal/api:newRateLimiterFromEnv(refillVar)", accessor: "RateLimits()." + field + ".RefillPerHour",
			def: defRefill, get: func(c *Config) any { return pick(c.RateLimits()).RefillPerHour }},
	}
}

// accessorCases are the server's reads of the inventory, each with its
// accessor. A row of env_vars.txt the server reads at boot and that has no
// case here, and is not in notConfig, fails TestEveryInventoryRowIsClaimed.
func accessorCases() []accessorCase {
	cases := []accessorCase{
		// stage signals
		{name: "OPENV_LOG_LEVEL", read: "os.Getenv", accessor: "LogLevel()", def: "INFO",
			get: func(c *Config) any { l, _ := c.LogLevel(); return l.String() }, pinned: raw("OPENV_LOG_LEVEL")},
		// stage config
		{name: "DATABASE_URL", read: "cmd/server:envSecret(key)", accessor: "DatabaseURL()", def: "",
			get: func(c *Config) any { return c.DatabaseURL() }},
		{name: "DB_HOST", read: "cmd/server:envOr(key)", accessor: "DatabaseParts().Host", def: "localhost",
			get: func(c *Config) any { return c.DatabaseParts().Host }},
		{name: "DB_PORT", read: "cmd/server:envOr(key)", accessor: "DatabaseParts().Port", def: "5432",
			get: func(c *Config) any { return c.DatabaseParts().Port }},
		{name: "DB_USER", read: "cmd/server:envOr(key)", accessor: "DatabaseParts().User", def: "postgres",
			get: func(c *Config) any { return c.DatabaseParts().User }},
		{name: "DB_PASSWORD", read: "cmd/server:envSecret(key)", accessor: "DatabaseParts().Password", def: "postgres",
			get: func(c *Config) any { return c.DatabaseParts().Password }},
		{name: "DB_NAME", read: "cmd/server:envOr(key)", accessor: "DatabaseParts().Name", def: "openv",
			get: func(c *Config) any { return c.DatabaseParts().Name }},
		{name: "PORT", read: "cmd/server:envOr(key)", accessor: "Port()", def: "8080",
			get: func(c *Config) any { return c.Port() }},
		{name: "UPLOADS_DIR", read: "cmd/server:envOr(key)", accessor: "UploadsDir()", def: "./uploads",
			get: func(c *Config) any { return c.UploadsDir() }},
		{name: "UPLOADS_DIR", read: "os.Getenv", accessor: "UploadsDirRaw() (the reports' per-request read)", def: "",
			get: func(c *Config) any { return c.UploadsDirRaw() }, pinned: raw("UPLOADS_DIR")},
		{name: "OPENV_UPLOAD_SWEEP", read: "cmd/server:envSwitch(key)", accessor: "UploadSweep()", def: true,
			get: func(c *Config) any { return c.UploadSweep() }},
		{name: "OPENV_DATA_DIR", read: "cmd/server:envOr(key)", accessor: "DataDir()", def: "./data",
			get: func(c *Config) any { return c.DataDir() }},
		{name: "AGENTS_DIR", read: "cmd/server:envOr(key)", accessor: "AgentsDir()", computed: true, def: "./data/agents",
			get: func(c *Config) any { return c.AgentsDir() }},
		{name: "WORKER_API_KEY", read: "cmd/server:envSecret(key)", accessor: "WorkerAPIKey()", def: "",
			get: func(c *Config) any { return c.WorkerAPIKey() }},
		{name: "OPENV_SELF_HOSTED", read: "cmd/server:envBool(key)", accessor: "SelfHosted()", def: false,
			get: func(c *Config) any { return c.SelfHosted() }},
		{name: "OPENV_PLAN_DEFAULT", read: "cmd/server:envOr(key)", accessor: "DefaultPlan()", def: "single",
			get:    func(c *Config) any { return c.DefaultPlan() },
			pinned: func(c *Config) any { return c.text("OPENV_PLAN_DEFAULT", "") }},
		{name: "OPENV_LIMITS", read: "cmd/server:envOr(key)", accessor: "DeploymentLimits()", def: map[string]interface{}(nil),
			get: func(c *Config) any {
				m, err := c.DeploymentLimits()
				if err != nil {
					return err
				}
				return m
			},
			pinned: func(c *Config) any { return c.text("OPENV_LIMITS", "") }},
		// stage core
		{name: "OPENV_EMBEDDING_API_KEY", read: "cmd/server:envSecret(key)", accessor: "Embeddings().APIKey", def: "",
			get: func(c *Config) any { return c.Embeddings().APIKey }},
		{name: "OPENV_EMBEDDING_BASE_URL", read: "os.Getenv", accessor: "Embeddings().BaseURL", def: "https://api.openai.com/v1",
			get: func(c *Config) any { return c.Embeddings().BaseURL }, pinned: raw("OPENV_EMBEDDING_BASE_URL")},
		{name: "OPENV_EMBEDDING_MODEL", read: "os.Getenv", accessor: "Embeddings().Model", def: embeddings.DefaultModel,
			get: func(c *Config) any { return c.Embeddings().Model }, pinned: raw("OPENV_EMBEDDING_MODEL")},
		// stages workspace and runners
		{name: "OPENV_BILLING_GRANDFATHER_BEFORE", read: "os.Getenv", accessor: "GrandfatherBefore()", def: false,
			get: func(c *Config) any { _, on, _ := c.GrandfatherBefore(); return on }, pinned: raw("OPENV_BILLING_GRANDFATHER_BEFORE")},
		{name: "RUNNER_POOL_KEY", read: "cmd/server:envSecret(key)", accessor: "RunnerPoolKey()", def: "",
			get: func(c *Config) any { return c.RunnerPoolKey() }},
		{name: "HOSTED_RUNNERS", read: "internal/hosting:envOr(key)", accessor: "HostedRunnersOff()", def: false,
			get:    func(c *Config) any { return c.HostedRunnersOff() },
			pinned: func(c *Config) any { return c.text("HOSTED_RUNNERS", "") }},
		{name: "RUNNER_IMAGE", read: "internal/hosting:envOr(key)", accessor: "RunnerContainer().Image", def: "openv-worker:latest",
			get: func(c *Config) any { return c.RunnerContainer().Image }},
		{name: "RUNNER_NETWORK", read: "internal/hosting:envOr(key)", accessor: "RunnerContainer().Network", def: "",
			get: func(c *Config) any { return c.RunnerContainer().Network }},
		{name: "RUNNER_API_URL", read: "internal/hosting:envOr(key)", accessor: "RunnerContainer().APIURL", def: "http://api:8080",
			get: func(c *Config) any { return c.RunnerContainer().APIURL }},
		{name: "HOSTED_RUNNER_PIDS_LIMIT", read: "os.Getenv", accessor: "HostedRunnerPidsLimit()", def: int64(1024),
			get: func(c *Config) any { return c.HostedRunnerPidsLimit() }, pinned: raw("HOSTED_RUNNER_PIDS_LIMIT")},
		// stage projects
		{name: "OPENV_SHARED_PRODUCT_DAILY_LIMIT", read: "cmd/server:envInt(key)", accessor: "SharedProductDailyLimit()",
			def: sharedproducts.DefaultDailyOrgLimit, get: func(c *Config) any { return c.SharedProductDailyLimit() }},
		{name: "OPENV_SHARED_PRODUCT_POOL_LIMIT", read: "cmd/server:envInt(key)", accessor: "SharedProductPoolLimit()",
			def: sharedproducts.DefaultPoolLimit, get: func(c *Config) any { return c.SharedProductPoolLimit() }},
		// stage agents
		{name: "OPENV_RUN_MAX_ATTEMPTS", read: "cmd/server:envInt(key)", accessor: "RunMaxAttempts()", def: agentruns.DefaultMaxAttempts,
			get: func(c *Config) any { return c.RunMaxAttempts() }},
		{name: "OPENV_RUN_AUTO_RETRY", read: "cmd/server:envBool(key)", accessor: "RunAutoRetry()", def: true,
			get: func(c *Config) any { return c.RunAutoRetry() }},
		// stage notify
		{name: "OPENV_SMTP_HOST", read: "os.Getenv", accessor: "SMTP().Host", def: "",
			get: func(c *Config) any { return c.SMTP().Host }, pinned: raw("OPENV_SMTP_HOST")},
		{name: "OPENV_SMTP_PORT", read: "internal/notify:envDefault(key)", accessor: "SMTP().Port", def: "587",
			get: func(c *Config) any { return c.SMTP().Port }},
		{name: "OPENV_SMTP_USER", read: "internal/notify:envSecret(key)", accessor: "SMTP().User", def: "",
			get: func(c *Config) any { return c.SMTP().User }},
		{name: "OPENV_SMTP_PASSWORD", read: "internal/notify:envSecret(key)", accessor: "SMTP().Password", def: "",
			get: func(c *Config) any { return c.SMTP().Password }},
		{name: "OPENV_SMTP_FROM", read: "os.Getenv", accessor: "SMTP().From", def: "",
			get: func(c *Config) any { return c.SMTP().From }, pinned: raw("OPENV_SMTP_FROM")},
		{name: "FRONTEND_URL", read: "cmd/server:envOr(key)", accessor: "EmailLinkBase()", computed: true, def: "http://localhost:3000",
			get: func(c *Config) any { return c.EmailLinkBase() }},
		{name: "PUBLIC_URL", read: "cmd/server:envOr(key)", accessor: "EmailLinkBase() (FRONTEND_URL unset)", def: "http://localhost:3000",
			get: func(c *Config) any { return c.EmailLinkBase() }},
		{name: "OPENV_EMAIL_VERIFICATION", read: "os.Getenv", accessor: "EmailVerification(mailer)", def: true,
			get: func(c *Config) any { return c.EmailVerification(enabledMailer{}).Required }, pinned: raw("OPENV_EMAIL_VERIFICATION")},
		{name: "OPENV_SESSION_MAX_AGE", read: "internal/domain/users:envDuration(name)", accessor: "SessionPolicy().MaxAge",
			def: users.DefaultSessionMaxAge, get: func(c *Config) any { return c.SessionPolicy().MaxAge }},
		{name: "OPENV_SESSION_IDLE", read: "internal/domain/users:envDuration(name)", accessor: "SessionPolicy().Idle",
			def: users.DefaultSessionIdle, get: func(c *Config) any { return c.SessionPolicy().Idle }},
		{name: "OPENV_REGISTRATION", read: "os.Getenv", accessor: "Registration()", def: "open",
			get: func(c *Config) any { return c.Registration() }, pinned: raw("OPENV_REGISTRATION")},
		{name: "OPENV_EMAIL_NOTIFICATION_TYPES", read: "internal/notify:typeListFromEnv(key)", accessor: "EmailTypes()", computed: true,
			def: notify.DefaultEmailTypes(), get: func(c *Config) any { return c.EmailTypes() }},
		{name: "OPENV_VAPID_PUBLIC_KEY", read: "os.Getenv", accessor: "VAPID().PublicKey", def: "",
			get: func(c *Config) any { return c.VAPID().PublicKey }, pinned: raw("OPENV_VAPID_PUBLIC_KEY")},
		{name: "OPENV_VAPID_PRIVATE_KEY", read: "internal/notify:envSecret(key)", accessor: "VAPID().PrivateKey", def: "",
			get: func(c *Config) any { return c.VAPID().PrivateKey }},
		{name: "OPENV_VAPID_SUBJECT", read: "os.Getenv", accessor: "VAPID().Subject", def: "",
			get: func(c *Config) any { return c.VAPID().Subject }, pinned: raw("OPENV_VAPID_SUBJECT")},
		{name: "OPENV_PUSH_NOTIFICATION_TYPES", read: "internal/notify:typeListFromEnv(key)", accessor: "PushTypes()", computed: true,
			def: notify.DefaultPushTypes(), get: func(c *Config) any { return c.PushTypes() }},
		// stage release
		{name: "OPENV_DEPLOYMENT", read: "cmd/server:envOr(key)", accessor: "Deployment()", def: "shared",
			get: func(c *Config) any { return c.Deployment() }},
		{name: "OPENV_RELEASE_FEED_URL", read: "cmd/server:envOr(key)", accessor: "ReleaseFeedURL()",
			def: "https://api.openv.app/api/v1/public/release", get: func(c *Config) any { return c.ReleaseFeedURL() }},
		{name: "OPENV_BUILD_SHA", read: "cmd/server:envOr(key)", accessor: "BuildSHA()", computed: true, def: "",
			get: func(c *Config) any { return c.BuildSHA() }},
		{name: "RAILWAY_GIT_COMMIT_SHA", read: "cmd/server:envOr(key)", accessor: "BuildSHA() (OPENV_BUILD_SHA unset)", def: "",
			get: func(c *Config) any { return c.BuildSHA() }},
		// stage jobs
		{name: "OPENV_BUDGET_ENFORCE", read: "cmd/server:envBool(key)", accessor: "BudgetEnforce()", def: false,
			get: func(c *Config) any { return c.BudgetEnforce() }},
		// stage sso
		{name: "GOOGLE_CLIENT_ID", read: "cmd/server:envOr(key)", accessor: "GoogleOAuth().ClientID (nil when unset)", def: "",
			get: func(c *Config) any {
				if g := c.GoogleOAuth(); g != nil {
					return g.ClientID
				}
				return ""
			}},
		{name: "GOOGLE_CLIENT_SECRET", read: "cmd/server:envSecret(key)", accessor: "GoogleOAuth().ClientSecret", with: googleOn,
			def: "", get: func(c *Config) any { return c.GoogleOAuth().ClientSecret }},
		{name: "PUBLIC_URL", read: "cmd/server:envOr(key)", accessor: "GoogleOAuth().RedirectURL", computed: true, with: googleOn,
			def: "http://localhost:8080", get: func(c *Config) any {
				return strings.TrimSuffix(c.GoogleOAuth().RedirectURL, googleRedirectPath)
			}},
		{name: "FRONTEND_URL", read: "cmd/server:envOr(key)", accessor: "GoogleOAuth().FrontendURL", with: googleOn,
			def: "http://localhost:3000", get: func(c *Config) any { return c.GoogleOAuth().FrontendURL }},
		{name: "OPENV_OIDC_ISSUER", read: "cmd/server:envOr(key)", accessor: "OIDC().Issuer (nil when unset)", def: "",
			get: func(c *Config) any {
				if o := c.OIDC(); o != nil {
					return o.Issuer
				}
				return ""
			}},
		{name: "PUBLIC_URL", read: "cmd/server:envOr(key)", accessor: "OIDC().RedirectURL (OPENV_OIDC_REDIRECT_URL unset)", computed: true,
			with: oidcOn, def: "http://localhost:8080", get: func(c *Config) any {
				return strings.TrimSuffix(c.OIDC().RedirectURL, oidcRedirectPath)
			}},
		{name: "OPENV_OIDC_REDIRECT_URL", read: "cmd/server:envOr(key)", accessor: "OIDC().RedirectURL", computed: true, with: oidcOn,
			def: "http://localhost:8080" + oidcRedirectPath, get: func(c *Config) any { return c.OIDC().RedirectURL }},
		{name: "OPENV_OIDC_SCOPES", read: "os.Getenv", accessor: "OIDC().Scopes", with: oidcOn, def: []string(nil),
			get: func(c *Config) any { return c.OIDC().Scopes }, pinned: raw("OPENV_OIDC_SCOPES")},
		{name: "OPENV_OIDC_CLIENT_ID", read: "cmd/server:envOr(key)", accessor: "OIDC().ClientID", with: oidcOn, def: "",
			get: func(c *Config) any { return c.OIDC().ClientID }},
		{name: "OPENV_OIDC_CLIENT_SECRET", read: "cmd/server:envSecret(key)", accessor: "OIDC().ClientSecret", with: oidcOn,
			def: "", get: func(c *Config) any { return c.OIDC().ClientSecret }},
		{name: "OPENV_OIDC_NAME", read: "cmd/server:envOr(key)", accessor: "OIDC().ProviderName", with: oidcOn, def: "SSO",
			get: func(c *Config) any { return c.OIDC().ProviderName }},
		{name: "FRONTEND_URL", read: "cmd/server:envOr(key)", accessor: "OIDC().FrontendURL", with: oidcOn,
			def: "http://localhost:3000", get: func(c *Config) any { return c.OIDC().FrontendURL }},
		// stage billing
		{name: "STRIPE_SECRET_KEY", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().SecretKey", def: "",
			get: billingField(func(b billing.Config) any { return b.SecretKey })},
		{name: "OPENV_STRIPE_API_VERSION", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().APIVersion",
			def: "", get: billingField(func(b billing.Config) any { return b.APIVersion })},
		{name: "OPENV_BILLING_RETURN_URL", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().ReturnURL",
			def: "", get: billingField(func(b billing.Config) any { return b.ReturnURL })},
		{name: "OPENV_BILLING_PORTAL_CONFIG", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().PortalConfig",
			def: "", get: billingField(func(b billing.Config) any { return b.PortalConfig })},
		{name: "OPENV_BILLING_MAX_SEATS", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().MaxSeats",
			def: billing.DefaultMaxSeats, get: billingField(func(b billing.Config) any { return b.MaxSeats })},
		{name: "OPENV_BILLING_TRIAL_DAYS", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().TrialDays",
			def: billing.DefaultTrialDays, get: billingField(func(b billing.Config) any { return b.TrialDays })},
		{name: "OPENV_STRIPE_PRICES", read: "internal/billing:ConfigFromEnv(getenv)", accessor: "Billing().Registry",
			def: []billing.PriceEntry{}, get: billingField(func(b billing.Config) any { return b.Registry.Entries() })},
		{name: "OPENV_BILLING_RECONCILE_MINUTES", read: "internal/billing:ConfigFromEnv(getenv)",
			accessor: "Billing().ReconcileInterval", def: 5 * time.Minute,
			get: billingField(func(b billing.Config) any { return b.ReconcileInterval })},
		// stage handlers
		{name: "FRONTEND_URL", read: "cmd/server:envOr(key)", accessor: "HandlerFrontendURL()", computed: true, def: "http://localhost:3000",
			get: func(c *Config) any { return c.HandlerFrontendURL() }},
		{name: "PUBLIC_URL", read: "cmd/server:envOr(key)", accessor: "HandlerFrontendURL() (FRONTEND_URL unset)",
			def: "http://localhost:3000", get: func(c *Config) any { return c.HandlerFrontendURL() }},
		{name: "SECURE_COOKIES", read: "cmd/server:envBool(key)", accessor: "SecureCookies()", def: false,
			get: func(c *Config) any { return c.SecureCookies() }},
		{name: "CROSS_SITE_COOKIES", read: "cmd/server:envBool(key)", accessor: "CrossSiteCookies()", def: false,
			get: func(c *Config) any { return c.CrossSiteCookies() }},
		{name: "PUBLIC_URL", read: "cmd/server:envOr(key)", accessor: "PublicAPIURL()", computed: true, def: "http://localhost:8080",
			get: func(c *Config) any { return c.PublicAPIURL() }},
		{name: "CONNECTOR_DIST_DIR", read: "cmd/server:envOr(key)", accessor: "ConnectorDistDir()", def: "./dist",
			get: func(c *Config) any { return c.ConnectorDistDir() }},
		// stage server
		{name: "OPENV_METRICS_TOKEN", read: "cmd/server:envSecret(key)", accessor: "MetricsToken()", def: "",
			get: func(c *Config) any { return c.MetricsToken() }},
		{name: "CORS_ORIGIN", read: "cmd/server:envOr(key)", accessor: "CORSOrigin()", def: "http://localhost:3000",
			get: func(c *Config) any { return c.CORSOrigin() }},
		{name: "OPENV_MAX_BODY_MB", read: "cmd/server:envInt(key)", accessor: "MaxBodyBytes() / MiB", def: 32,
			get: func(c *Config) any { return int(c.MaxBodyBytes() / (1 << 20)) }},
	}
	type limiter struct {
		burstVar, refillVar, field string
		burst                      int
		refill                     float64
		pick                       func(RateLimits) RateLimit
	}
	for _, l := range []limiter{
		{"OPENV_INTERVIEW_MSG_BURST", "OPENV_INTERVIEW_MSG_REFILL_PER_HOUR", "InterviewMsg", 5, 20, func(r RateLimits) RateLimit { return r.InterviewMsg }},
		{"OPENV_INTERVIEW_IP_BURST", "OPENV_INTERVIEW_IP_REFILL_PER_HOUR", "InterviewIP", 20, 60, func(r RateLimits) RateLimit { return r.InterviewIP }},
		{"OPENV_INTERVIEW_STREAM_BURST", "OPENV_INTERVIEW_STREAM_REFILL_PER_HOUR", "InterviewStream", 30, 120, func(r RateLimits) RateLimit { return r.InterviewStream }},
		{"OPENV_AUTH_IP_BURST", "OPENV_AUTH_IP_REFILL_PER_HOUR", "AuthIP", 30, 120, func(r RateLimits) RateLimit { return r.AuthIP }},
		{"OPENV_AUTH_ACCOUNT_BURST", "OPENV_AUTH_ACCOUNT_REFILL_PER_HOUR", "AuthAccount", 5, 20, func(r RateLimits) RateLimit { return r.AuthAccount }},
		{"OPENV_REGISTER_IP_BURST", "OPENV_REGISTER_IP_REFILL_PER_HOUR", "RegisterIP", 5, 10, func(r RateLimits) RateLimit { return r.RegisterIP }},
		{"OPENV_SSO_IP_BURST", "OPENV_SSO_IP_REFILL_PER_HOUR", "SSOIP", 20, 60, func(r RateLimits) RateLimit { return r.SSOIP }},
		{"OPENV_VERIFY_RESEND_BURST", "OPENV_VERIFY_RESEND_REFILL_PER_HOUR", "VerifyResend", 3, 6, func(r RateLimits) RateLimit { return r.VerifyResend }},
		{"OPENV_PASSWORD_RESET_BURST", "OPENV_PASSWORD_RESET_REFILL_PER_HOUR", "PasswordReset", 3, 6, func(r RateLimits) RateLimit { return r.PasswordReset }},
		{"OPENV_INVITE_PREVIEW_BURST", "OPENV_INVITE_PREVIEW_REFILL_PER_HOUR", "InvitePreview", 60, 240, func(r RateLimits) RateLimit { return r.InvitePreview }},
		{"OPENV_BILLING_REFRESH_BURST", "OPENV_BILLING_REFRESH_REFILL_PER_HOUR", "BillingRefresh", 10, 120, func(r RateLimits) RateLimit { return r.BillingRefresh }},
		{"OPENV_BILLING_WRITE_BURST", "OPENV_BILLING_WRITE_REFILL_PER_HOUR", "BillingWrite", 5, 20, func(r RateLimits) RateLimit { return r.BillingWrite }},
		{"OPENV_INVITE_BURST", "OPENV_INVITE_REFILL_PER_HOUR", "Invite", 20, 60, func(r RateLimits) RateLimit { return r.Invite }},
	} {
		cases = append(cases, rateCases(l.burstVar, l.refillVar, l.field, l.burst, l.refill, l.pick)...)
	}
	return cases
}

// enabledMailer is a notify.Mailer that can send, so that
// EmailVerification's value follows OPENV_EMAIL_VERIFICATION alone.
type enabledMailer struct{}

func (enabledMailer) Enabled() bool             { return true }
func (enabledMailer) Send(_, _, _ string) error { return nil }

// Reasons a row of the inventory is not this package's.
const (
	agentdSetting = "agentd's setting (K8 gives agentd a cmd/agentd/config.go of its own), not the server's"
	mcpSetting    = "openv-mcp's setting, not the server's"
	runnerRead    = "the runner's read, in agentd, not the server's"
)

// notConfig are the rows of the inventory the server's boot stages do not
// read, keyed by variable and read column, with the reason. A reason naming
// an S8 exemption ("exemption <id>") must name one that lists the variable.
var notConfig = map[string]string{
	"AGENT_CHILD_CONCURRENCY cmd/agentd:envIntOr(key)":        agentdSetting,
	"AGENT_CONCURRENCY cmd/agentd:envIntOr(key)":              agentdSetting,
	"AGENT_WORKSPACE_RETENTION cmd/agentd:envDurationOr(key)": agentdSetting,
	"OPENV_HOSTED cmd/agentd:envBool(key)":                    agentdSetting,
	"OPENV_API_URL cmd/agentd:envOr(key)":                     agentdSetting,
	"RUNNER_NODE_NAME cmd/agentd:envOr(key)":                  agentdSetting,
	"RUNNER_POOL cmd/agentd:envOr(key)":                       agentdSetting,
	"RUNNER_POOL_KEY cmd/agentd:envSecret(key)":               agentdSetting,
	"RUNNER_SESSION_ROOT cmd/agentd:envOr(key)":               agentdSetting,
	"WORKER_API_KEY cmd/agentd:envSecret(key)":                agentdSetting,
	"HOME os.LookupEnv":                                       "the runner pool's HOME (internal/runner/pool.go), read in agentd, not the server",
	"OPENV_API_TOKEN cmd/openv-mcp:resolveToken(env)":         mcpSetting,
	"OPENV_RUN_TOKEN cmd/openv-mcp:resolveToken(env)":         mcpSetting,
	"OPENV_API_URL os.Getenv":                                 mcpSetting + " (its default is inline there)",
	"OPENV_MCP_TOOLS os.LookupEnv":                            mcpSetting + "; exemption mcp-tool-allowlist",
	"ANTHROPIC_API_KEY os.Getenv !=\"\"":                      runnerRead + "; exemption runner-probe",
	"CODEX_HOME os.Getenv":                                    runnerRead + "; exemption runner-probe",
	"OPENAI_API_KEY os.Getenv !=\"\"":                         runnerRead + "; exemption runner-probe",
	"GEMINI_API_KEY os.Getenv !=\"\"":                         runnerRead + "; exemption runner-probe",
	"GOOGLE_API_KEY os.Getenv !=\"\"":                         runnerRead + "; exemption runner-probe",
	"CLOUD_SHELL os.Getenv !=\"\"":                            runnerRead + "; exemption runner-gemini-auth-probe",
	"GEMINI_CLI_USE_COMPUTE_ADC os.Getenv !=\"\"":             runnerRead + "; exemption runner-gemini-auth-probe",
	"GOOGLE_GEMINI_BASE_URL os.Getenv !=\"\"":                 runnerRead + "; exemption runner-gemini-auth-probe",
	"GOOGLE_GENAI_USE_GCA os.Getenv !=\"\"":                   runnerRead + "; exemption runner-gemini-auth-probe",
	"GOOGLE_GENAI_USE_VERTEXAI os.Getenv !=\"\"":              runnerRead + "; exemption runner-gemini-auth-probe",
	"OPENV_CLIENT_IP_HEADER os.Getenv":                        "read on every request, and stays so; exemption per-request",
	"OPENV_TRUSTED_PROXY_HOPS os.Getenv":                      "read on every request, and stays so; exemption per-request",
	"OPENV_TRUST_PROXY os.Getenv":                             "read on every request, and stays so; exemption per-request",
	"OPENV_MAX_UPLOAD_MB os.Getenv":                           "read on every request, and stays so; exemption per-request",
	"OPENV_MAX_EVIDENCE_MB os.Getenv":                         "read on every request, and stays so; exemption per-request",
	"OPENV_PUSH_ENDPOINT_HOSTS os.Getenv":                     "read on every request, and stays so; exemption per-request",
}

// movedReads are the read columns of today's server reads, each with the
// internal/config getter that reads the same variable once refactor step
// X10b switches the stages to this package: S8's scan then shows the
// variable read through that getter, with the same default, and
// env_parse.txt pins the getter in a section of its own with the same rows
// (ENV_INVENTORY_CHANGES, scripts/refactor/refactor_guard.py, lists each
// move). Billing's reads stay billing.ConfigFromEnv's, and the reads Load
// makes are no rows of S8's.
var movedReads = map[string]string{
	"cmd/server:envOr(key)":                         "internal/config:Config.text(name)",
	"cmd/server:envInt(key)":                        "internal/config:Config.count(name)",
	"cmd/server:envBool(key)":                       "internal/config:Config.boolean(name)",
	"cmd/server:envSwitch(key)":                     "internal/config:Config.onOff(name)",
	"cmd/server:envSecret(key)":                     "internal/config:Config.secret(name)",
	"internal/hosting:envOr(key)":                   "internal/config:Config.text(name)",
	"internal/notify:envDefault(key)":               "internal/config:Config.text(name)",
	"internal/notify:envSecret(key)":                "internal/config:Config.secret(name)",
	"internal/notify:typeListFromEnv(key)":          "internal/config:Config.typeList(name)",
	"internal/api:newRateLimiterFromEnv(burstVar)":  "internal/config:Config.rateLimit(burstVar)",
	"internal/api:newRateLimiterFromEnv(refillVar)": "internal/config:Config.rateLimit(refillVar)",
	"internal/domain/users:envDuration(name)":       "internal/config:Config.sessionLifetime(name)",
	"os.Getenv": "internal/config:Config.getenv(name)",
}

// rowsOf are the inventory rows ac claims: its variable read the way ac
// says, or through the internal/config getter that read moves to
// (movedReads), the one whose default is (computed) when ac.computed and
// the one whose default is not otherwise (FRONTEND_URL and PUBLIC_URL have
// one of each). ok is false unless there is one such row or one of each
// read; while X10b moves the reads, a variable can have both.
func rowsOf(rows []inventoryRow, ac accessorCase) (found []inventoryRow, ok bool) {
	perRead := map[string]int{}
	for _, r := range rows {
		if r.name == ac.name && (r.read == ac.read || r.read == movedReads[ac.read]) && (r.def == "(computed)") == ac.computed {
			found = append(found, r)
			perRead[r.read]++
		}
	}
	for _, n := range perRead {
		if n != 1 {
			return nil, false
		}
	}
	return found, len(found) > 0
}

// TestEveryInventoryRowIsClaimed holds the package to S8's inventory: every
// row of env_vars.txt has an accessor case or a reason in notConfig, never
// both; every case and reason names a row the inventory has; a reason that
// cites an exemption cites one that lists the variable; and the variables
// Load reads are exactly those the cases name. A variable added to the
// inventory fails here until it is given an accessor or a reason.
func TestEveryInventoryRowIsClaimed(t *testing.T) {
	rows, exemptions := readInventory(t)
	claimed := map[inventoryRow][]string{}
	caseNames := map[string]bool{}
	for _, ac := range accessorCases() {
		caseNames[ac.name] = true
		found, ok := rowsOf(rows, ac)
		if !ok {
			t.Errorf("the case for %s (%s) names no one row of env_vars.txt read by %s (or %s) with a default that is (computed): %v",
				ac.name, ac.accessor, ac.read, movedReads[ac.read], ac.computed)
			continue
		}
		for _, r := range found {
			claimed[r] = append(claimed[r], ac.accessor)
		}
	}
	excused := map[inventoryRow]string{}
	for k, reason := range notConfig {
		matched := false
		for _, r := range rows {
			if r.key() == k {
				matched = true
				excused[r] = reason
				if len(claimed[r]) > 0 {
					t.Errorf("%q has both an accessor (%s) and a reason it is not this package's (%s)", k, strings.Join(claimed[r], ", "), reason)
				}
			}
		}
		if !matched {
			t.Errorf("notConfig names %q, which is not a row of env_vars.txt", k)
		}
		if _, id, ok := strings.Cut(reason, "exemption "); ok {
			ex, found := exemptions[id]
			name, _, _ := strings.Cut(k, " ")
			if !found || !slices.Contains(ex.names, name) {
				t.Errorf("notConfig's reason for %q cites the exemption %s, which env_vars.txt does not list %s under", k, id, name)
			}
		}
	}
	for _, r := range rows {
		if len(claimed[r]) == 0 && excused[r] == "" {
			t.Errorf("env_vars.txt row %s\t%s\t%s has no accessor: give the server's read an accessor in internal/config "+
				"and a case in accessorCases, or say in notConfig why it is not the server's", r.name, r.read, r.def)
		}
	}
	loaded := map[string]bool{}
	for _, n := range names {
		if loaded[n] {
			t.Errorf("names lists %s twice", n)
		}
		loaded[n] = true
		if !caseNames[n] {
			t.Errorf("Load reads %s, which no accessor case names", n)
		}
	}
	for n := range caseNames {
		if !loaded[n] {
			t.Errorf("an accessor case names %s, which Load does not read (add it to names)", n)
		}
	}
	if t.Failed() {
		return
	}
	for _, r := range rows {
		if len(claimed[r]) > 0 {
			t.Logf("%s\t%s\t%s -> %s", r.name, r.read, r.def, strings.Join(claimed[r], "; "))
		} else {
			t.Logf("%s\t%s\t%s -> not the server's: %s", r.name, r.read, r.def, excused[r])
		}
	}
}

// TestAccessorDefaultsAreTheInventorys checks each case's default: what the
// accessor returns with its variable unset, and, where the inventory's
// read passes a constant default, that constant.
func TestAccessorDefaultsAreTheInventorys(t *testing.T) {
	quietLog(t)
	rows, _ := readInventory(t)
	for _, ac := range accessorCases() {
		got := ac.get(loadFrom(ac.with))
		if !reflect.DeepEqual(got, ac.def) {
			t.Errorf("%s with %s unset = %s (%T), want the default %s (%T)", ac.accessor, ac.name, render(got), got, render(ac.def), ac.def)
		}
		found, _ := rowsOf(rows, ac)
		for _, r := range found {
			if r.def == "-" || r.def == "(computed)" {
				continue
			}
			// Where the accessor parses further than the read, the read's
			// own default is the inventory's.
			what, d := ac.accessor, ac.def
			if ac.pinned != nil {
				what, d = "the read under "+ac.accessor, ac.pinned(loadFrom(ac.with))
			}
			if got := renderDefault(d); got != r.def {
				t.Errorf("%s defaults to %s; env_vars.txt says the server's %s read defaults to %s", what, got, r.read, r.def)
			}
		}
	}
}

// renderDefault shows a default as env_vars.txt does: text quoted, a
// number or a boolean bare, a duration as time.Duration prints it.
func renderDefault(v any) string {
	if i, ok := v.(int); ok {
		return strconv.Itoa(i)
	}
	return render(v)
}
