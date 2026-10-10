package config

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/envparse"
	"github.com/openv/requirements-platform/internal/notify"
)

// The helpers of other packages (and cmd/server's envSecret) that read the
// process environment themselves until refactor step X10b, which switches
// the server's stages to this package's accessors and takes them out of
// production code. They are kept here, word for word as they were before
// X10b (only renamed, with their named constants written out, and with a
// struct for what MailerFromEnv and ProviderFromEnv built, less the lines
// those logged about what they built), as the oracle helpers_test.go and
// pairs_test.go hold the accessors to. oracle_twin_test.go proves each the
// same as the helper it copies while that helper exists.

// oracleSessionPolicyFromEnv is users.SessionPolicyFromEnv.
func oracleSessionPolicyFromEnv() users.SessionPolicy {
	p := users.SessionPolicy{
		MaxAge: oracleEnvDuration("OPENV_SESSION_MAX_AGE", users.DefaultSessionMaxAge),
		Idle:   oracleEnvDuration("OPENV_SESSION_IDLE", users.DefaultSessionIdle),
	}
	slog.Info("session lifetime", "max_age", p.MaxAge, "idle", p.Idle)
	return p
}

// oracleEnvDuration is users' envDuration.
func oracleEnvDuration(name string, max time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return max
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		oracleWarnLifetimeOnce(name, raw, "session lifetime: ignoring unusable value", max)
		return max
	}
	if d > max {
		oracleWarnLifetimeOnce(name, raw, "session lifetime: value above the ceiling, clamping", max)
		return max
	}
	return d
}

// oracleWarnedLifetimes is users' warnedLifetimes.
var oracleWarnedLifetimes sync.Map

// oracleWarnLifetimeOnce is users' warnLifetimeOnce.
func oracleWarnLifetimeOnce(name, raw, msg string, using time.Duration) {
	if _, seen := oracleWarnedLifetimes.LoadOrStore(name+"\x00"+raw, true); seen {
		return
	}
	slog.Warn(msg, "var", name, "using", using)
}

// oracleRegistrationPolicyFromEnv is api.RegistrationPolicyFromEnv.
func oracleRegistrationPolicyFromEnv() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENV_REGISTRATION"))) {
	case "closed":
		slog.Info("registration: closed (new accounts arrive by workspace invitation or single sign-on)")
		return "closed"
	case "", "open":
		slog.Info("registration: open (set OPENV_REGISTRATION=closed to require an invitation)")
		return "open"
	default:
		slog.Warn("registration: unrecognised OPENV_REGISTRATION value; leaving registration open",
			"want", "open"+" or "+"closed")
		return "open"
	}
}

// oracleVerificationPolicyFromEnv is notify.VerificationPolicyFromEnv.
func oracleVerificationPolicyFromEnv(m notify.Mailer) users.EmailVerificationPolicy {
	switch {
	case envparse.Off("OPENV_EMAIL_VERIFICATION", os.Getenv("OPENV_EMAIL_VERIFICATION")):
		slog.Info("email verification: disabled (OPENV_EMAIL_VERIFICATION=off)")
		return users.EmailVerificationPolicy{}
	case m == nil || !m.Enabled():
		slog.Info("email verification: disabled (no OPENV_SMTP_HOST; accounts are verified at sign-up)")
		return users.EmailVerificationPolicy{}
	default:
		slog.Info("email verification: required for password accounts (SMTP configured; set OPENV_EMAIL_VERIFICATION=off to disable)")
		return users.EmailVerificationPolicy{Required: true}
	}
}

// oracleVAPIDFromEnv is notify.VAPIDFromEnv.
func oracleVAPIDFromEnv() notify.VAPIDConfig {
	c := notify.VAPIDConfig{
		PublicKey:  strings.TrimSpace(os.Getenv("OPENV_VAPID_PUBLIC_KEY")),
		PrivateKey: oracleNotifyEnvSecret("OPENV_VAPID_PRIVATE_KEY"),
		Subject:    strings.TrimSpace(os.Getenv("OPENV_VAPID_SUBJECT")),
	}
	havePrivate := strings.TrimSpace(c.PrivateKey) != ""
	switch {
	case c.PublicKey == "" && !havePrivate && c.Subject == "":
		slog.Info("push: " + "OPENV_VAPID_PUBLIC_KEY" + " unset; web push disabled (in-app + SSE delivery unaffected)")
	case c.PublicKey == "" || !havePrivate:
		slog.Warn("push: VAPID key pair incomplete; web push disabled",
			"have_public", c.PublicKey != "", "have_private", havePrivate)
	case !(strings.HasPrefix(c.Subject, "mailto:") || strings.HasPrefix(c.Subject, "https://")):
		slog.Warn("push: " + "OPENV_VAPID_SUBJECT" + " must be a mailto: or https: URI; web push disabled")
	default:
		slog.Info("push: web push enabled", "subject", c.Subject)
	}
	return c
}

// oracleEmailTypesFromEnv is notify.EmailTypesFromEnv.
func oracleEmailTypesFromEnv() []string {
	return oracleTypeListFromEnv("OPENV_EMAIL_NOTIFICATION_TYPES", notify.DefaultEmailTypes)
}

// oraclePushTypesFromEnv is notify.PushTypesFromEnv.
func oraclePushTypesFromEnv() []string {
	return oracleTypeListFromEnv("OPENV_PUSH_NOTIFICATION_TYPES", notify.DefaultPushTypes)
}

// oracleTypeListFromEnv is notify's typeListFromEnv.
func oracleTypeListFromEnv(key string, fallback func() []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback()
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return fallback()
	}
	return out
}

// oracleNotifyEnvSecret is notify's envSecret.
func oracleNotifyEnvSecret(key string) string {
	return envparse.Secret(key, os.Getenv(key))
}

// oracleEnvDefault is notify's envDefault.
func oracleEnvDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// oracleMailerFromEnv is what notify.MailerFromEnv read into its mailer, in
// its order, without the lines it logged about the mailer it built (those
// stay with notify.NewMailer).
func oracleMailerFromEnv() SMTP {
	host := strings.TrimSpace(os.Getenv("OPENV_SMTP_HOST"))
	m := SMTP{
		Host:     host,
		Port:     oracleEnvDefault("OPENV_SMTP_PORT", "587"),
		User:     oracleNotifyEnvSecret("OPENV_SMTP_USER"),
		Password: oracleNotifyEnvSecret("OPENV_SMTP_PASSWORD"),
		From:     strings.TrimSpace(os.Getenv("OPENV_SMTP_FROM")),
	}
	if m.From == "" {
		m.From = m.User
	}
	return m
}

// oracleProviderFromEnv is what embeddings.ProviderFromEnv read into its
// provider, given the key cmd/server read through envSecret, without the
// lines it logged about the provider it built (those stay with
// embeddings.NewProvider).
func oracleProviderFromEnv(apiKey string) Embeddings {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENV_EMBEDDING_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(os.Getenv("OPENV_EMBEDDING_MODEL"))
	if model == "" {
		model = embeddings.DefaultModel
	}
	return Embeddings{APIKey: apiKey, BaseURL: baseURL, Model: model}
}

// oracleServerEnvSecret is cmd/server's envSecret.
func oracleServerEnvSecret(key, fallback string) string {
	if v := envparse.Secret(key, os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// oraclePidsLimit is hosting.PidsLimit.
func oraclePidsLimit() int64 {
	n := envparse.Number("HOSTED_RUNNER_PIDS_LIMIT", os.Getenv("HOSTED_RUNNER_PIDS_LIMIT"), int(1024))
	if n <= 0 {
		return 0
	}
	return int64(n)
}
