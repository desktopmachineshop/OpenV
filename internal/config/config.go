// Package config is the API server's configuration (refactor plan X10):
// Load reads, once, every environment variable the server's boot stages
// read, through the lookup it is given, and records each one's raw value
// and whether it is set; the accessors parse and validate a variable on
// each call with exactly the parser, default and condition the server uses
// today (invariant I13: internal/envparse's rule, and the parsers of the
// packages that read a setting themselves).
//
// The package reads no environment itself: cmd/server hands Load
// os.LookupEnv. Refactor step X10a adds it, with nothing calling it yet;
// X10b switches the boot stages to it.
//
// How an accessor matches today's code:
//
//   - It reads the variables the statement it stands for reads, in that
//     order, and no others, so a malformed setting's warning lands where it
//     does today and names only a variable that statement reads: the OIDC
//     settings only with OPENV_OIDC_ISSUER set, Google's only with
//     GOOGLE_CLIENT_ID, the DB_* parts only without DATABASE_URL, the
//     grandfather date only off a self-hosted install. The one read more is
//     PORT, which the PUBLIC_URL fallbacks read again where cmd/server uses
//     the port stage config read: a text read, which logs nothing.
//   - A check that is fatal today returns its error, the same error today's
//     check returns (orgs.ParseLimits, time.Parse, billing.ConfigFromEnv),
//     and the stage keeps exiting on it where it does today. CORS_ORIGIN's
//     check stays api.CORSMiddleware's.
//   - It logs what today's reading of the variable logs: internal/envparse's
//     warnings, and, for a helper that only reads settings
//     (users.SessionPolicyFromEnv, api.RegistrationPolicyFromEnv,
//     notify.VerificationPolicyFromEnv, notify.VAPIDFromEnv, the plan
//     default's check in stage config), that helper's lines too, so a stage
//     swaps the call for the accessor and its log stays the same. Where
//     today's code builds something from the settings (the mailer, the
//     embedding provider, the hosted runner provisioner, the logger), the
//     lines about what it built stay with what builds it, and the accessor
//     returns the settings it read.
//   - Each FRONTEND_URL and PUBLIC_URL call site keeps an accessor of its
//     own (quirk Q12), as do the server's UPLOADS_DIR and the reports' raw
//     one.
//
// Per-request reads stay per request (S8's per-request exemption; the
// reports' UPLOADS_DIR read among them, whose parse UploadsDirRaw only
// keeps apart from the server's), and OPENV_MCP_TOOLS (openv-mcp's) and the
// runner's and agentd's settings are not here.
package config

import "github.com/openv/requirements-platform/internal/envparse"

// names are the variables Load reads: every one an accessor reads, in the
// order of the boot stages that read them. Production code uses the table
// only to range over it (not even len), which lets S8's inventory resolve
// each variable's name once cmd/server hands Load os.LookupEnv.
var names = []string{
	// stage signals
	"OPENV_LOG_LEVEL",
	// stage config
	"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
	"PORT", "UPLOADS_DIR", "OPENV_UPLOAD_SWEEP", "OPENV_DATA_DIR", "AGENTS_DIR", "WORKER_API_KEY",
	"OPENV_SELF_HOSTED", "OPENV_PLAN_DEFAULT", "OPENV_LIMITS",
	// stage core
	"OPENV_EMBEDDING_API_KEY", "OPENV_EMBEDDING_BASE_URL", "OPENV_EMBEDDING_MODEL",
	// stages workspace and runners
	"OPENV_BILLING_GRANDFATHER_BEFORE", "RUNNER_POOL_KEY",
	"HOSTED_RUNNERS", "RUNNER_IMAGE", "RUNNER_NETWORK", "RUNNER_API_URL", "HOSTED_RUNNER_PIDS_LIMIT",
	// stage projects
	"OPENV_SHARED_PRODUCT_DAILY_LIMIT", "OPENV_SHARED_PRODUCT_POOL_LIMIT",
	// stage agents
	"OPENV_RUN_MAX_ATTEMPTS", "OPENV_RUN_AUTO_RETRY",
	// stage notify
	"OPENV_SMTP_HOST", "OPENV_SMTP_PORT", "OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD", "OPENV_SMTP_FROM",
	"FRONTEND_URL", "PUBLIC_URL", "OPENV_EMAIL_VERIFICATION", "OPENV_SESSION_MAX_AGE", "OPENV_SESSION_IDLE",
	"OPENV_REGISTRATION", "OPENV_EMAIL_NOTIFICATION_TYPES",
	"OPENV_VAPID_PUBLIC_KEY", "OPENV_VAPID_PRIVATE_KEY", "OPENV_VAPID_SUBJECT", "OPENV_PUSH_NOTIFICATION_TYPES",
	// stage release
	"OPENV_DEPLOYMENT", "OPENV_RELEASE_FEED_URL", "OPENV_BUILD_SHA", "RAILWAY_GIT_COMMIT_SHA",
	// stage jobs
	"OPENV_BUDGET_ENFORCE",
	// stage sso
	"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET",
	"OPENV_OIDC_ISSUER", "OPENV_OIDC_REDIRECT_URL", "OPENV_OIDC_SCOPES", "OPENV_OIDC_CLIENT_ID",
	"OPENV_OIDC_CLIENT_SECRET", "OPENV_OIDC_NAME",
	// stage billing
	"STRIPE_SECRET_KEY", "OPENV_STRIPE_API_VERSION", "OPENV_BILLING_RETURN_URL", "OPENV_BILLING_PORTAL_CONFIG",
	"OPENV_BILLING_MAX_SEATS", "OPENV_BILLING_TRIAL_DAYS", "OPENV_STRIPE_PRICES", "OPENV_BILLING_RECONCILE_MINUTES",
	// stage handlers (api.NewHandler's rate limiters among them)
	"SECURE_COOKIES", "CROSS_SITE_COOKIES", "CONNECTOR_DIST_DIR",
	"OPENV_INTERVIEW_MSG_BURST", "OPENV_INTERVIEW_MSG_REFILL_PER_HOUR",
	"OPENV_INTERVIEW_IP_BURST", "OPENV_INTERVIEW_IP_REFILL_PER_HOUR",
	"OPENV_INTERVIEW_STREAM_BURST", "OPENV_INTERVIEW_STREAM_REFILL_PER_HOUR",
	"OPENV_AUTH_IP_BURST", "OPENV_AUTH_IP_REFILL_PER_HOUR",
	"OPENV_AUTH_ACCOUNT_BURST", "OPENV_AUTH_ACCOUNT_REFILL_PER_HOUR",
	"OPENV_REGISTER_IP_BURST", "OPENV_REGISTER_IP_REFILL_PER_HOUR",
	"OPENV_SSO_IP_BURST", "OPENV_SSO_IP_REFILL_PER_HOUR",
	"OPENV_VERIFY_RESEND_BURST", "OPENV_VERIFY_RESEND_REFILL_PER_HOUR",
	"OPENV_PASSWORD_RESET_BURST", "OPENV_PASSWORD_RESET_REFILL_PER_HOUR",
	"OPENV_INVITE_PREVIEW_BURST", "OPENV_INVITE_PREVIEW_REFILL_PER_HOUR",
	"OPENV_BILLING_REFRESH_BURST", "OPENV_BILLING_REFRESH_REFILL_PER_HOUR",
	"OPENV_BILLING_WRITE_BURST", "OPENV_BILLING_WRITE_REFILL_PER_HOUR",
	"OPENV_INVITE_BURST", "OPENV_INVITE_REFILL_PER_HOUR",
	// stage server
	"OPENV_METRICS_TOKEN", "CORS_ORIGIN", "OPENV_MAX_BODY_MB",
}

// Config is what Load read: each variable's raw value and whether it was
// set. It never changes after Load, so every accessor returns the same value
// however often it is called; internal/envparse warns once per variable and
// value, so reading a malformed one again warns no more than today's second
// read does.
type Config struct {
	vars map[string]setting
	// trace, nil but in this package's tests, is told each variable an
	// accessor reads, in order: the order in which a malformed one's
	// warning reaches the boot log.
	trace func(name string)
}

// setting is one variable as Load found it. Every server setting reads an
// empty value as an unset one today (os.Getenv); set is recorded so that a
// setting that tells them apart can.
type setting struct {
	raw string
	set bool
}

// Load reads every variable in names through lookup (os.LookupEnv, in
// cmd/server) and records what it returns. It parses nothing: each accessor
// parses its variables when it is called, at the statement that reads them
// today.
func Load(lookup func(string) (string, bool)) *Config {
	c := &Config{vars: map[string]setting{}}
	for _, name := range names {
		raw, set := lookup(name)
		c.vars[name] = setting{raw: raw, set: set}
	}
	return c
}

// lookup is what Load recorded for name. A name Load does not read is a
// mistake in this package, which its tests catch; it panics rather than
// read as unset.
func (c *Config) lookup(name string) (string, bool) {
	s, ok := c.vars[name]
	if !ok {
		panic("config: " + name + " is not a variable Load reads; add it to names")
	}
	if c.trace != nil {
		c.trace(name)
	}
	return s.raw, s.set
}

// getenv is name's raw value, empty when unset, as os.Getenv returns it.
func (c *Config) getenv(name string) string {
	raw, _ := c.lookup(name)
	return raw
}

// text reads a text setting, trimmed, or def when that leaves nothing: the
// server's envOr, hosting's envOr and notify's envDefault.
func (c *Config) text(name, def string) string {
	return envparse.Text(c.getenv(name), def)
}

// count reads a whole number above 0, or def: the server's envInt.
func (c *Config) count(name string, def int) int {
	return envparse.Count(name, c.getenv(name), def)
}

// boolean reads true or false in any case, or 1 or 0, or def: the server's
// envBool.
func (c *Config) boolean(name string, def bool) bool {
	return envparse.Bool(name, c.getenv(name), def)
}

// onOff reads on or off in any case, or a boolean as boolean reads one, or
// def: the server's envSwitch.
func (c *Config) onOff(name string, def bool) bool {
	return envparse.Switch(name, c.getenv(name), def)
}

// secret reads a credential exactly as set, never trimmed, warning once
// when spaces or a line break sit around it, or def when it is unset or
// empty: the server's envSecret, and notify's with an empty def.
func (c *Config) secret(name, def string) string {
	if v := envparse.Secret(name, c.getenv(name)); v != "" {
		return v
	}
	return def
}
