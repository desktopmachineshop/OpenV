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
	"mime"
	"net"
	"net/smtp"
	"strings"
	"unicode"

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

// SMTPSettings are the mail channel's settings, the OPENV_SMTP_* variables,
// which cmd/server reads (internal/config's SMTP) and hands NewMailer:
//
//	Host      OPENV_SMTP_HOST, the SMTP server host (empty => email disabled)
//	Port      OPENV_SMTP_PORT, the SMTP port (default 587)
//	User      OPENV_SMTP_USER, the username for PLAIN auth (empty => no auth)
//	Password  OPENV_SMTP_PASSWORD, the password for PLAIN auth
//	From      OPENV_SMTP_FROM, the envelope/From address (empty => User)
//
// User and Password are credentials, used exactly as set (#379, question 24).
type SMTPSettings struct {
	Host, Port, User, Password, From string
}

// NewMailer builds an SMTPMailer from s. With no host the returned mailer is
// disabled (Enabled()==false) and every Send is a silent no-op — the
// intended default for dev and any deployment that has not opted into
// email. It logs one line saying which.
func NewMailer(s SMTPSettings) *SMTPMailer {
	m := &SMTPMailer{
		host: s.Host,
		port: s.Port,
		user: s.User,
		pass: s.Password,
		from: s.From,
	}
	if m.from == "" {
		m.from = m.user
	}
	if m.host == "" {
		slog.Info("email: OPENV_SMTP_HOST unset; email notifications disabled (in-app + SSE delivery unaffected)")
	} else {
		slog.Info("email: SMTP delivery enabled", "host", m.host, "port", m.port, "from", fromForLog(m.from, m.user))
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
// Every header value it writes from data goes through headerLine or
// headerText, here and only here, so nothing a caller passes can end a
// header early and start another (#379, bug 64); the body follows the blank
// line and cannot add a header.
//
// Every line break in the body, LF, CR or CRLF, goes out as one CRLF, the
// only line end SMTP has (RFC 5321, section 2.3.8). The body used to turn
// only LF into CRLF, so a CR in the text went out bare: a server that takes
// a bare CR for a line end would read "\r.\r" as the lone dot that ends the
// message, and what followed as commands of its own. A body written with
// LF, as every template here is, goes out exactly as before.
func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", headerLine(from))
	fmt.Fprintf(&b, "To: %s\r\n", headerLine(to))
	fmt.Fprintf(&b, "Subject: %s\r\n", headerText(subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(crlfLines.Replace(body))
	return []byte(b.String())
}

// crlfLines writes each line break, CRLF, a bare CR or a bare LF, as CRLF.
// A replacer compares in argument order, so CRLF is taken whole before its
// CR could be.
var crlfLines = strings.NewReplacer("\r\n", "\r\n", "\r", "\r\n", "\n", "\r\n")

// memberText keeps text a member wrote (a display name, an address, a
// workspace's or an artifact's name) on the line of the sentence it is put
// into. Each run of control characters in it (CR, LF, tab, NUL, escape and
// the rest of Unicode's Cc, and the line and paragraph separators U+2028 and
// U+2029) becomes one space, and a run at either end is dropped, so a name
// cannot open a line or a paragraph of its own in a notification, an email
// or a phone alert, and pass what follows off as OpenV's: a display name
// "Ben\n\nYour account is locked" is read as "Ben Your account is locked
// joined this workspace". Text without them is returned as it is.
func memberText(v string) string {
	return strings.Join(strings.FieldsFunc(v, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}), " ")
}

// headerLine keeps a header value on its one line. A CR or LF inside it
// would end the header there and start another from the text after it, so
// each run of them becomes one space: a workspace named
// "Acme\r\nBcc: x@example.com" puts "Acme Bcc: x@example.com" in a subject
// and adds no header. From and To are written through it alone: each is a
// bare address, the envelope's own, with no display name, and an address
// cannot be RFC 2047-encoded (RFC 2047, section 5), so one outside ASCII is
// written as it is, as before. net/smtp refuses an envelope address with a
// line break before it sends anything.
func headerLine(v string) string {
	return strings.Join(strings.FieldsFunc(v, func(r rune) bool { return r == '\r' || r == '\n' }), " ")
}

// headerText is an unstructured header value, the subject: kept on its line
// (headerLine) and, when it holds anything but printable ASCII, written as
// RFC 2047 encoded words (UTF-8, Q encoding), which every mail client shows
// as written; a workspace named "Zürich Labs" used to reach the subject as
// raw UTF-8. Long text takes several encoded words, folded one to a line, so
// a long subject never makes one long line. A printable ASCII value is
// written as it is.
func headerText(v string) string {
	encoded := mime.QEncoding.Encode("utf-8", headerLine(v))
	return strings.ReplaceAll(encoded, "?= =?", "?=\r\n =?")
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
// notification types that email (DefaultEmailTypes, or the override
// OPENV_EMAIL_NOTIFICATION_TYPES, which cmd/server reads).
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
		notifications.TypeHostedMinutes,
		// Access changes email because they are precisely the thing somebody
		// needs to know while they are NOT looking at the app — being added to
		// a workspace, or losing access to one, is not news that can wait for
		// the next time they happen to open the bell.
		notifications.TypeAccessChanged,
		notifications.TypeMembershipChanged,
	}
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

// notificationPath is the one place the email link and the web push url are
// built: the page the bell opens for the same notification
// (pathForNotification in frontend/src/components/NotificationBellPaths.ts), case
// for case, so all three land in the same place.
func notificationPath(ref map[string]interface{}) string {
	kind := refString(ref, "kind")
	switch kind {
	case "org_usage":
		// Workspace budget alerts are not project-scoped — link to the usage tab.
		return "/org/settings?tab=usage"
	case "org_limits":
		// The cloud runner minutes allowance is raised from the Billing tab,
		// which is where its alert says to go.
		return "/org/settings?tab=billing"
	case "release":
		// A platform release is not scoped to anything: it opens the notes.
		return "/whats-new"
	case "support_window":
		// A dedicated instance leaving its support window is a workspace matter.
		return "/org/settings"
	case "membership":
		// Membership and privilege changes land on the people list they are
		// about: the workspace's members tab, or the project's own.
		return "/org/settings?tab=members"
	}
	projectID := refString(ref, "project_id")
	if projectID == "" {
		// A run with no project (a whole-workspace automation's, one
		// launched outside any project, or one its project's delete left
		// behind) is listed on the workspace Runs page, which opens it.
		// Where that page is not on yet (a stable-channel workspace before
		// the release that has it), the page sends a member to the
		// projects list, where this link went before.
		if runID := refString(ref, "run_id"); kind == "run" && runID != "" {
			return fmt.Sprintf("/org/runs?run=%s", runID)
		}
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
	case "project_membership":
		return fmt.Sprintf("/projects/%s/settings?tab=members", projectID)
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
