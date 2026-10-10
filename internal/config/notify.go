package config

// The settings of stage notify: the mail and web push side channels, the
// links in mail, email verification, session lifetime and registration.
// Where today's stage calls a helper that only reads settings
// (notify.VerificationPolicyFromEnv, users.SessionPolicyFromEnv,
// api.RegistrationPolicyFromEnv, notify.VAPIDFromEnv), the accessor returns
// what it returns and logs what it logs; the mailer's lines stay with the
// mailer.

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/envparse"
	"github.com/openv/requirements-platform/internal/notify"
)

// SMTP is the mail channel's settings as notify.MailerFromEnv reads them.
type SMTP struct {
	Host     string // trimmed; "" leaves email off
	Port     string // trimmed, default 587
	User     string // a credential, exactly as set; "" means no auth
	Password string // a credential, exactly as set
	From     string // trimmed, or User when that leaves nothing
}

// SMTP reads OPENV_SMTP_HOST, _PORT, _USER, _PASSWORD and _FROM, in that
// order, as MailerFromEnv does.
func (c *Config) SMTP() SMTP {
	s := SMTP{
		Host:     strings.TrimSpace(c.getenv("OPENV_SMTP_HOST")),
		Port:     c.text("OPENV_SMTP_PORT", "587"),
		User:     c.credential("OPENV_SMTP_USER"),
		Password: c.credential("OPENV_SMTP_PASSWORD"),
		From:     strings.TrimSpace(c.getenv("OPENV_SMTP_FROM")),
	}
	if s.From == "" {
		s.From = s.User
	}
	return s
}

// EmailLinkBase is where links in mail point (stage notify's
// emailLinkBase): FRONTEND_URL, else PUBLIC_URL, else the dev frontend. It
// is the same chain as HandlerFrontendURL's, kept a field of its own (Q12).
func (c *Config) EmailLinkBase() string {
	return c.text("FRONTEND_URL", c.text("PUBLIC_URL", "http://localhost:3000"))
}

// EmailVerification is notify.VerificationPolicyFromEnv(m): verification is
// required when the mailer can send and OPENV_EMAIL_VERIFICATION is not
// off, in any case. It logs the line naming the state, as that helper does,
// and its warning for a value other than off.
func (c *Config) EmailVerification(m notify.Mailer) users.EmailVerificationPolicy {
	switch {
	case envparse.Off("OPENV_EMAIL_VERIFICATION", c.getenv("OPENV_EMAIL_VERIFICATION")):
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

// SessionPolicy is users.SessionPolicyFromEnv(): OPENV_SESSION_MAX_AGE and
// OPENV_SESSION_IDLE as Go durations, each falling back to its default,
// which is also its ceiling, with the warnings and the line that helper
// logs.
func (c *Config) SessionPolicy() users.SessionPolicy {
	p := users.SessionPolicy{
		MaxAge: c.sessionLifetime("OPENV_SESSION_MAX_AGE", users.DefaultSessionMaxAge),
		Idle:   c.sessionLifetime("OPENV_SESSION_IDLE", users.DefaultSessionIdle),
	}
	slog.Info("session lifetime", "max_age", p.MaxAge, "idle", p.Idle)
	return p
}

// sessionLifetime is users' envDuration: a positive duration, trimmed, or
// ceiling when unset, unusable or above it, each of the last two with a
// warning, once per variable and value, that names the variable and never
// the value.
func (c *Config) sessionLifetime(name string, ceiling time.Duration) time.Duration {
	raw := strings.TrimSpace(c.getenv(name))
	if raw == "" {
		return ceiling
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		warnSessionOnce(name, raw, "session lifetime: ignoring unusable value", ceiling)
		return ceiling
	}
	if d > ceiling {
		warnSessionOnce(name, raw, "session lifetime: value above the ceiling, clamping", ceiling)
		return ceiling
	}
	return d
}

// sessionWarned is users' warnedLifetimes: the session lifetime variable and
// value pairs already warned about.
var sessionWarned sync.Map

// warnSessionOnce is users' warnLifetimeOnce: msg, once per variable and
// value, with the variable and the duration that applies, never the value.
func warnSessionOnce(name, raw, msg string, using time.Duration) {
	if _, seen := sessionWarned.LoadOrStore(name+"\x00"+raw, true); seen {
		return
	}
	slog.Warn(msg, "var", name, "using", using)
}

// The registration policies, as api.RegistrationOpen and
// api.RegistrationClosed spell them.
const (
	registrationOpen   = "open"
	registrationClosed = "closed"
)

// Registration is api.RegistrationPolicyFromEnv(): OPENV_REGISTRATION,
// trimmed and in any case, closed or open, and open for anything else, with
// the line or warning that helper logs.
func (c *Config) Registration() string {
	switch strings.ToLower(strings.TrimSpace(c.getenv("OPENV_REGISTRATION"))) {
	case registrationClosed:
		slog.Info("registration: closed (new accounts arrive by workspace invitation or single sign-on)")
		return registrationClosed
	case "", registrationOpen:
		slog.Info("registration: open (set OPENV_REGISTRATION=closed to require an invitation)")
		return registrationOpen
	default:
		slog.Warn("registration: unrecognised OPENV_REGISTRATION value; leaving registration open",
			"want", registrationOpen+" or "+registrationClosed)
		return registrationOpen
	}
}

// EmailTypes is notify.EmailTypesFromEnv(): the notification types that
// mail (OPENV_EMAIL_NOTIFICATION_TYPES, a comma-separated list), or
// notify.DefaultEmailTypes.
func (c *Config) EmailTypes() []string {
	return c.typeList("OPENV_EMAIL_NOTIFICATION_TYPES", notify.DefaultEmailTypes)
}

// PushTypes is notify.PushTypesFromEnv(): the notification types that reach
// a phone (OPENV_PUSH_NOTIFICATION_TYPES), or notify.DefaultPushTypes.
func (c *Config) PushTypes() []string {
	return c.typeList("OPENV_PUSH_NOTIFICATION_TYPES", notify.DefaultPushTypes)
}

// typeList is notify's typeListFromEnv: a comma-separated list, each item
// trimmed and the empty ones dropped, or fallback() when that leaves none.
func (c *Config) typeList(name string, fallback func() []string) []string {
	raw := strings.TrimSpace(c.getenv(name))
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

// VAPID is notify.VAPIDFromEnv(): the web push key pair (the private key a
// credential, exactly as set) and contact subject, with the line or warning
// that helper logs about whether push is on.
func (c *Config) VAPID() notify.VAPIDConfig {
	v := notify.VAPIDConfig{
		PublicKey:  strings.TrimSpace(c.getenv("OPENV_VAPID_PUBLIC_KEY")),
		PrivateKey: c.credential("OPENV_VAPID_PRIVATE_KEY"),
		Subject:    strings.TrimSpace(c.getenv("OPENV_VAPID_SUBJECT")),
	}
	havePrivate := strings.TrimSpace(v.PrivateKey) != ""
	switch {
	case v.PublicKey == "" && !havePrivate && v.Subject == "":
		slog.Info("push: OPENV_VAPID_PUBLIC_KEY unset; web push disabled (in-app + SSE delivery unaffected)")
	case v.PublicKey == "" || !havePrivate:
		slog.Warn("push: VAPID key pair incomplete; web push disabled",
			"have_public", v.PublicKey != "", "have_private", havePrivate)
	case !strings.HasPrefix(v.Subject, "mailto:") && !strings.HasPrefix(v.Subject, "https://"):
		slog.Warn("push: OPENV_VAPID_SUBJECT must be a mailto: or https: URI; web push disabled")
	default:
		slog.Info("push: web push enabled", "subject", v.Subject)
	}
	return v
}
