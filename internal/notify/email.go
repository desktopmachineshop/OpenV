package notify

// Email delivery for high-signal notifications (issue #187). This is a thin,
// best-effort side channel bolted onto the existing fan-out: when a
// notification row is created (and pushed over SSE), an eligible type destined
// for an opted-in recipient also produces one templated plain-text email —
// provided the server has SMTP configured. Email is strictly OPT-IN
// INFRASTRUCTURE: with no OPENV_SMTP_HOST set the mailer is a no-op, so the
// app (and compose/dev) runs exactly as before. Send failures are logged and
// swallowed; email never fails a run or blocks a notification.

import (
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Mailer sends plain-text email. Enabled reports whether real delivery is
// wired; when false, Send is a no-op that logs at debug.
type Mailer interface {
	Enabled() bool
	Send(to, subject, body string) error
}

// SMTPMailer delivers over SMTP using the stdlib net/smtp (no dependency).
// The zero host disables it. send is injectable so tests can capture the wire
// message without a live server; it defaults to smtp.SendMail.
type SMTPMailer struct {
	host string
	port string
	user string
	pass string
	from string
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// MailerFromEnv builds an SMTPMailer from the OPENV_SMTP_* environment. With
// OPENV_SMTP_HOST unset the returned mailer is disabled (Enabled()==false) and
// every Send is a silent no-op — the intended default for dev and any
// deployment that has not opted into email.
//
// Recognized variables:
//
//	OPENV_SMTP_HOST      SMTP server host (empty => email disabled)
//	OPENV_SMTP_PORT      SMTP port (default 587)
//	OPENV_SMTP_USER      username for PLAIN auth (empty => no auth)
//	OPENV_SMTP_PASSWORD  password for PLAIN auth
//	OPENV_SMTP_FROM      envelope/From address (default: OPENV_SMTP_USER)
func MailerFromEnv() *SMTPMailer {
	host := strings.TrimSpace(os.Getenv("OPENV_SMTP_HOST"))
	m := &SMTPMailer{
		host: host,
		port: envDefault("OPENV_SMTP_PORT", "587"),
		user: os.Getenv("OPENV_SMTP_USER"),
		pass: os.Getenv("OPENV_SMTP_PASSWORD"),
		from: strings.TrimSpace(os.Getenv("OPENV_SMTP_FROM")),
	}
	if m.from == "" {
		m.from = m.user
	}
	if host == "" {
		slog.Info("email: OPENV_SMTP_HOST unset; email notifications disabled (in-app + SSE delivery unaffected)")
	} else {
		slog.Info("email: SMTP delivery enabled", "host", host, "port", m.port, "from", m.from)
	}
	return m
}

// Enabled reports whether a host is configured.
func (m *SMTPMailer) Enabled() bool { return m != nil && m.host != "" }

// Send delivers one plain-text message. Best-effort: a disabled mailer or an
// empty recipient returns nil without error.
func (m *SMTPMailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		slog.Debug("email: SMTP not configured; skipping send", "to", to, "subject", subject)
		return nil
	}
	if strings.TrimSpace(to) == "" {
		return nil
	}
	var auth smtp.Auth
	if m.user != "" {
		auth = smtp.PlainAuth("", m.user, m.pass, m.host)
	}
	sendFn := m.send
	if sendFn == nil {
		sendFn = smtp.SendMail
	}
	return sendFn(net.JoinHostPort(m.host, m.port), auth, m.from, []string{to}, buildMessage(m.from, to, subject, body))
}

// buildMessage renders a minimal RFC 5322 plain-text message with CRLF lines.
func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}

// UserDirectory resolves a recipient's address and email opt-out.
// users.Service satisfies it.
type UserDirectory interface {
	GetByID(id string) (*users.User, error)
}

// EmailDispatcher turns a freshly-created notification into a best-effort
// email. It gates on: SMTP configured, the type being email-eligible, and the
// recipient being opted in with a real address.
type EmailDispatcher struct {
	mailer   Mailer
	users    UserDirectory
	linkBase string // frontend base URL for deep links, no trailing slash
	eligible map[string]bool
}

// NewEmailDispatcher wires a dispatcher. eligibleTypes is the allow-list of
// notification types that email (see DefaultEmailTypes / EmailTypesFromEnv).
// linkBase is the externally reachable frontend base URL used to build deep
// links.
func NewEmailDispatcher(mailer Mailer, dir UserDirectory, linkBase string, eligibleTypes []string) *EmailDispatcher {
	elig := make(map[string]bool, len(eligibleTypes))
	for _, t := range eligibleTypes {
		elig[strings.TrimSpace(t)] = true
	}
	return &EmailDispatcher{
		mailer:   mailer,
		users:    dir,
		linkBase: strings.TrimRight(strings.TrimSpace(linkBase), "/"),
		eligible: elig,
	}
}

// DefaultEmailTypes are the higher-signal notification types that email by
// default. Chatter @mentions and interview-completed are intentionally
// excluded — too chatty for email; they stay in-app only.
func DefaultEmailTypes() []string {
	return []string{
		notifications.TypeRunFailed,
		notifications.TypeProposalPending,
		notifications.TypeReviewRequested,
		notifications.TypeBudgetThreshold,
	}
}

// EmailTypesFromEnv reads the comma-separated OPENV_EMAIL_NOTIFICATION_TYPES
// override, or returns DefaultEmailTypes when it is unset/empty.
func EmailTypesFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("OPENV_EMAIL_NOTIFICATION_TYPES"))
	if raw == "" {
		return DefaultEmailTypes()
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return DefaultEmailTypes()
	}
	return out
}

// Eligible reports whether a type is on the email allow-list. Exported for
// tests and callers that want to skip work before building a notification.
func (d *EmailDispatcher) Eligible(ntype string) bool {
	return d != nil && d.eligible[ntype]
}

// Dispatch sends a best-effort email for one notification. It is a no-op when
// the dispatcher is nil, SMTP is unconfigured, the type is not eligible, the
// recipient has opted out, or the recipient has no address. A send error is
// logged and swallowed — email must never fail the notification or the run.
func (d *EmailDispatcher) Dispatch(n *notifications.Notification) {
	if d == nil || d.mailer == nil || !d.mailer.Enabled() {
		return
	}
	if n == nil || !d.eligible[n.Type] {
		return
	}
	u, err := d.users.GetByID(n.UserID)
	if err != nil {
		slog.Error("email: failed to load recipient", "user_id", n.UserID, "error", err)
		return
	}
	if u == nil || !u.EmailNotifications || strings.TrimSpace(u.Email) == "" {
		return
	}
	subject, body := renderEmail(n, d.linkBase)
	if err := d.mailer.Send(u.Email, subject, body); err != nil {
		slog.Error("email: failed to send notification email",
			"user_id", n.UserID, "type", n.Type, "error", err)
	}
}

// renderEmail builds the subject and plain-text body for a notification,
// including a deep link when one can be derived from the entity ref.
func renderEmail(n *notifications.Notification, linkBase string) (subject, body string) {
	subject = n.Title
	var b strings.Builder
	b.WriteString(n.Title)
	b.WriteString("\n\n")
	if n.Body != "" {
		b.WriteString(n.Body)
		b.WriteString("\n\n")
	}
	if link := deepLink(n, linkBase); link != "" {
		b.WriteString("Open it in OpenV:\n")
		b.WriteString(link)
		b.WriteString("\n\n")
	}
	b.WriteString("—\nYou are receiving this because email notifications are enabled for your OpenV account. Turn them off in Settings → Notifications.")
	return subject, b.String()
}

// deepLink builds a link to the notification's subject in the frontend,
// mirroring pathForNotification in the web NotificationBell so the email and
// the in-app bell land in the same place.
func deepLink(n *notifications.Notification, linkBase string) string {
	if linkBase == "" || n == nil {
		return ""
	}
	return linkBase + notificationPath(n.EntityRef)
}

func notificationPath(ref map[string]interface{}) string {
	kind := refString(ref, "kind")
	// Workspace budget alerts are not project-scoped — link to the usage tab.
	if kind == "org_usage" {
		return "/org/settings?tab=usage"
	}
	projectID := refString(ref, "project_id")
	if projectID == "" {
		return "/projects"
	}
	switch kind {
	case "run", "proposal":
		if runID := refString(ref, "run_id"); runID != "" {
			return fmt.Sprintf("/projects/%s/agent-runs?run=%s", projectID, runID)
		}
		return fmt.Sprintf("/projects/%s/agent-runs", projectID)
	case "interview":
		return fmt.Sprintf("/projects/%s/interviews", projectID)
	case "artifact":
		return fmt.Sprintf("/projects/%s/requirements", projectID)
	default:
		return fmt.Sprintf("/projects/%s", projectID)
	}
}

func refString(ref map[string]interface{}, key string) string {
	if ref == nil {
		return ""
	}
	v, ok := ref[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
