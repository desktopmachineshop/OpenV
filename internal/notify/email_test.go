package notify

import (
	"net/smtp"
	"strings"
	"testing"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// captureMailer records the last message instead of sending it.
type captureMailer struct {
	enabled bool
	to      []string
	subject []string
	body    []string
}

func (m *captureMailer) Enabled() bool { return m.enabled }
func (m *captureMailer) Send(to, subject, body string) error {
	m.to = append(m.to, to)
	m.subject = append(m.subject, subject)
	m.body = append(m.body, body)
	return nil
}

// fakeDir answers a fixed user by id.
type fakeDir struct {
	byID map[string]*users.User
	err  error
}

func (d *fakeDir) GetByID(id string) (*users.User, error) {
	if d.err != nil {
		return nil, d.err
	}
	return d.byID[id], nil
}

func optedIn(id, email string) *users.User {
	return &users.User{ID: id, Email: email, EmailNotifications: true}
}
func optedOut(id, email string) *users.User {
	return &users.User{ID: id, Email: email, EmailNotifications: false}
}

// TestMailerNoopWhenUnconfigured: with no SMTP host, the mailer is disabled
// and Send is a silent no-op (email is opt-in infra — the app runs without it).
func TestMailerNoopWhenUnconfigured(t *testing.T) {
	for _, key := range []string{"OPENV_SMTP_HOST", "OPENV_SMTP_PORT", "OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD", "OPENV_SMTP_FROM"} {
		t.Setenv(key, "")
	}
	m := MailerFromEnv()
	if m.Enabled() {
		t.Fatal("mailer should be disabled with no OPENV_SMTP_HOST")
	}
	if err := m.Send("someone@example.com", "hi", "body"); err != nil {
		t.Fatalf("disabled Send returned error: %v", err)
	}
}

// TestMailerFromEnvConfigured: a host makes it enabled and derives sane
// defaults (port 587, From falls back to the user).
func TestMailerFromEnvConfigured(t *testing.T) {
	t.Setenv("OPENV_SMTP_HOST", "smtp.example.com")
	t.Setenv("OPENV_SMTP_PORT", "")
	t.Setenv("OPENV_SMTP_USER", "bot@example.com")
	t.Setenv("OPENV_SMTP_PASSWORD", "secret")
	t.Setenv("OPENV_SMTP_FROM", "")
	m := MailerFromEnv()
	if !m.Enabled() {
		t.Fatal("mailer should be enabled with a host set")
	}
	if m.port != "587" {
		t.Errorf("default port = %q, want 587", m.port)
	}
	if m.from != "bot@example.com" {
		t.Errorf("from = %q, want fallback to user", m.from)
	}
}

// TestEmailTypesFilteringAndOptOut drives Dispatch directly across the axes
// that gate an email: type eligibility, opt-out, missing address, and lookup
// errors — none of which may send.
func TestEmailTypesFilteringAndOptOut(t *testing.T) {
	dir := &fakeDir{byID: map[string]*users.User{
		"u-in":     optedIn("u-in", "in@example.com"),
		"u-out":    optedOut("u-out", "out@example.com"),
		"u-noaddr": optedIn("u-noaddr", ""),
	}}

	cases := []struct {
		name     string
		ntype    string
		userID   string
		wantSend bool
	}{
		{"eligible + opted in sends", notifications.TypeRunFailed, "u-in", true},
		{"eligible but opted out is silent", notifications.TypeRunFailed, "u-out", false},
		{"eligible but no address is silent", notifications.TypeRunFailed, "u-noaddr", false},
		{"ineligible mention is silent", notifications.TypeMention, "u-in", false},
		{"ineligible interview is silent", notifications.TypeInterviewCompleted, "u-in", false},
		{"eligible proposal sends", notifications.TypeProposalPending, "u-in", true},
		{"eligible budget sends", notifications.TypeBudgetThreshold, "u-in", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mailer := &captureMailer{enabled: true}
			d := NewEmailDispatcher(mailer, dir, "https://app.example.com", DefaultEmailTypes())
			d.Dispatch(&notifications.Notification{
				UserID: tc.userID, Type: tc.ntype, Title: "T", Body: "B",
			})
			if got := len(mailer.to) == 1; got != tc.wantSend {
				t.Fatalf("sent=%v, want %v", got, tc.wantSend)
			}
		})
	}
}

// TestDispatchDisabledMailer: an unconfigured (disabled) mailer never sends,
// even for an eligible, opted-in recipient.
func TestDispatchDisabledMailer(t *testing.T) {
	mailer := &captureMailer{enabled: false}
	dir := &fakeDir{byID: map[string]*users.User{"u-in": optedIn("u-in", "in@example.com")}}
	d := NewEmailDispatcher(mailer, dir, "https://app.example.com", DefaultEmailTypes())
	d.Dispatch(&notifications.Notification{UserID: "u-in", Type: notifications.TypeRunFailed, Title: "T"})
	if len(mailer.to) != 0 {
		t.Fatalf("disabled mailer sent %d emails, want 0", len(mailer.to))
	}
}

// TestNilDispatcherSafe: the nil-dispatcher path (email off) never panics.
func TestNilDispatcherSafe(t *testing.T) {
	var d *EmailDispatcher
	d.Dispatch(&notifications.Notification{UserID: "u", Type: notifications.TypeRunFailed})
}

// TestDeepLinkMirrorsBell: the email deep link matches the frontend
// NotificationBell's route mapping for each entity-ref kind.
func TestDeepLinkMirrorsBell(t *testing.T) {
	base := "https://app.example.com"
	cases := []struct {
		ref  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"kind": "run", "project_id": "p1", "run_id": "r1"}, base + "/projects/p1/agent-runs?run=r1"},
		{map[string]interface{}{"kind": "proposal", "project_id": "p1", "run_id": "r1"}, base + "/projects/p1/agent-runs?run=r1"},
		{map[string]interface{}{"kind": "artifact", "project_id": "p1"}, base + "/projects/p1/requirements"},
		{map[string]interface{}{"kind": "interview", "project_id": "p1"}, base + "/projects/p1/interviews"},
		{map[string]interface{}{"kind": "org_usage", "org_id": "o1"}, base + "/org/settings?tab=usage"},
		{map[string]interface{}{"kind": "run"}, base + "/projects"},
	}
	for _, tc := range cases {
		got := deepLink(&notifications.Notification{EntityRef: tc.ref}, base)
		if got != tc.want {
			t.Errorf("deepLink(%v) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

// TestRunFailedProducesEmailViaSMTP is the end-to-end capture: a run_failed
// notification for an opted-in launcher yields exactly one SMTP message
// carrying the deep link, and an opted-out launcher yields none — all through
// the real Notifier fan-out and a fake-SMTP transport.
func TestRunFailedProducesEmailViaSMTP(t *testing.T) {
	type sent struct {
		addr, from string
		to         []string
		msg        string
	}
	var captured []sent
	smtpMailer := &SMTPMailer{
		host: "smtp.example.com",
		port: "587",
		from: "openv@example.com",
		send: func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
			captured = append(captured, sent{addr: addr, from: from, to: to, msg: string(msg)})
			return nil
		},
	}
	dir := &fakeDir{byID: map[string]*users.User{
		"u-in":  optedIn("u-in", "launcher@example.com"),
		"u-out": optedOut("u-out", "quiet@example.com"),
	}}
	dispatcher := NewEmailDispatcher(smtpMailer, dir, "https://app.example.com", DefaultEmailTypes())

	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{}, nil).SetEmailDispatcher(dispatcher)

	// Opted-in launcher: one stored row and one email.
	n.Handle(domainevents.Event{
		EventType: domainevents.RunFinished,
		ProjectID: "p1",
		EntityID:  "run-42",
		Actor:     "agent:run-42",
		Payload:   map[string]interface{}{"status": "failed", "launched_by": "u-in"},
	})
	if len(captured) != 1 {
		t.Fatalf("captured %d emails, want 1", len(captured))
	}
	got := captured[0]
	if got.addr != "smtp.example.com:587" {
		t.Errorf("addr = %q, want smtp.example.com:587", got.addr)
	}
	if len(got.to) != 1 || got.to[0] != "launcher@example.com" {
		t.Errorf("to = %v, want [launcher@example.com]", got.to)
	}
	if !strings.Contains(got.msg, "https://app.example.com/projects/p1/agent-runs?run=run-42") {
		t.Errorf("message missing deep link; got:\n%s", got.msg)
	}
	if !strings.Contains(got.msg, "Subject: Agent run failed") {
		t.Errorf("message missing expected subject; got:\n%s", got.msg)
	}

	// Opted-out launcher: stored row but no email.
	captured = nil
	n.Handle(domainevents.Event{
		EventType: domainevents.RunFinished,
		ProjectID: "p1",
		EntityID:  "run-43",
		Actor:     "agent:run-43",
		Payload:   map[string]interface{}{"status": "failed", "launched_by": "u-out"},
	})
	if len(captured) != 0 {
		t.Fatalf("opted-out launcher produced %d emails, want 0", len(captured))
	}
}
