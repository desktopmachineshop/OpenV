package notify

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"net/smtp"
	"sort"
	"strings"
	"testing"
	"time"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
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

// TestEmailAndPushLinkWhereTheBellOpens pins, for every notification type
// and each entity kind it carries, the page the email's link and the web
// push's url open: the page the bell opens for the same notification
// (pathForNotification in frontend/src/components/NotificationBell.tsx, whose
// own table is in NotificationBell.test.tsx). They used to part ways for
// workspace and project membership, releases and the support window, which
// email and push sent to the projects list or the project's overview, and
// all three sent the cloud runner minutes alert to the projects list,
// though it points at the Billing tab (#379, bugs 58 and 59).
func TestEmailAndPushLinkWhereTheBellOpens(t *testing.T) {
	type ref = map[string]interface{}
	cases := []struct {
		ntype string
		ref   ref
		want  string
	}{
		{notifications.TypeProposalPending, ref{"kind": "proposal", "proposal_id": "prop-7", "project_id": "p1", "run_id": "r1"}, "/projects/p1/agent-runs?run=r1"},
		{notifications.TypeProposalPending, ref{"kind": "proposal", "proposal_id": "prop-8", "project_id": "p1", "run_id": ""}, "/projects/p1/agent-runs"},
		{notifications.TypeRunFailed, ref{"kind": "run", "project_id": "p1", "run_id": "r1"}, "/projects/p1/agent-runs?run=r1"},
		{notifications.TypeInterviewCompleted, ref{"kind": "interview", "project_id": "p1", "session_id": "s1"}, "/projects/p1/interviews"},
		{notifications.TypeMention, ref{"kind": "artifact", "project_id": "p1", "artifact_id": "a1", "chatter_id": "c1"}, "/projects/p1/requirements"},
		{notifications.TypeReviewRequested, ref{"kind": "artifact", "project_id": "p1", "artifact_id": "a1"}, "/projects/p1/requirements"},
		{notifications.TypeBudgetThreshold, ref{"kind": "org_usage", "org_id": "o1", "threshold": 80}, "/org/settings?tab=usage"},
		{notifications.TypeHostedMinutes, ref{"kind": "org_limits", "org_id": "o1", "threshold": 100, "month": "2026-10"}, "/org/settings?tab=billing"},
		{notifications.TypeAccessChanged, ref{"kind": "membership", "org_id": "o1", "user_id": "u1"}, "/org/settings?tab=members"},
		{notifications.TypeAccessChanged, ref{"kind": "project_membership", "org_id": "o1", "user_id": "u1", "project_id": "p1"}, "/projects/p1/settings?tab=members"},
		{notifications.TypeMembershipChanged, ref{"kind": "membership", "org_id": "o1", "user_id": "u1"}, "/org/settings?tab=members"},
		{notifications.TypeReleasePublished, ref{"kind": "release", "version": "0.16.0"}, "/whats-new"},
		{notifications.TypeReleasePublished, ref{"kind": "release", "version": "0.15.0", "org_id": "o1"}, "/whats-new"},
		{notifications.TypeReleaseScheduled, ref{"kind": "release", "version": "0.15.0", "org_id": "o1"}, "/whats-new"},
		{notifications.TypeReleaseSupportWindow, ref{"kind": "support_window", "running": "0.14.0", "available": "0.15.0", "closes": "2026-11-30"}, "/org/settings"},
		// The fallbacks: a project-scoped kind with no project, a kind the
		// mapping does not know, and no entity reference at all.
		{notifications.TypeRunFailed, ref{"kind": "run"}, "/projects"},
		{notifications.TypeAccessChanged, ref{"kind": "project_membership", "org_id": "o1", "user_id": "u1"}, "/projects"},
		{"some_future_type", ref{"kind": "something_new", "project_id": "p1"}, "/projects/p1"},
		{"some_future_type", nil, "/projects"},
	}
	const base = "https://app.example.com"
	covered := map[string]bool{}
	for _, tc := range cases {
		covered[tc.ntype] = true
		n := &notifications.Notification{Type: tc.ntype, Title: "T", Body: "B", EntityRef: tc.ref}
		if got := deepLink(n, base); got != base+tc.want {
			t.Errorf("%s %v: email link %q, want %q", tc.ntype, tc.ref, got, base+tc.want)
		}
		if got := renderPush(n).URL; got != tc.want {
			t.Errorf("%s %v: push url %q, want %q", tc.ntype, tc.ref, got, tc.want)
		}
	}
	for _, c := range ncTypeConstants(t) {
		if !covered[c.value] {
			t.Errorf("notifications.%s (%q) has no row here: add the page its email and push open", c.name, c.value)
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

// wireMail is one message as the mailer hands it to net/smtp.
type wireMail struct {
	to  []string
	msg []byte
}

// capturingMailer is the real SMTPMailer with its send replaced, so a test
// reads the exact bytes that would go on the wire.
func capturingMailer(sent *[]wireMail) *SMTPMailer {
	return &SMTPMailer{
		host: "smtp.example.com",
		port: "587",
		from: "openv@example.com",
		send: func(_ string, _ smtp.Auth, _ string, to []string, msg []byte) error {
			*sent = append(*sent, wireMail{to: to, msg: msg})
			return nil
		},
	}
}

// wireHeaders parses a wire message's header section as a mail client
// would, and returns every header line as written and the parsed message.
func wireHeaders(t *testing.T, msg []byte) ([]string, *mail.Message) {
	t.Helper()
	end := bytes.Index(msg, []byte("\r\n\r\n"))
	if end < 0 {
		t.Fatalf("no blank line ends the headers:\n%q", msg)
	}
	lines := strings.Split(string(msg[:end]), "\r\n")
	for _, l := range lines {
		if strings.ContainsAny(l, "\r\n") {
			t.Errorf("a bare CR or LF in header line %q", l)
		}
		if len(l) > 998 {
			t.Errorf("header line of %d characters, over RFC 5322's 998", len(l))
		}
		for _, r := range l {
			if r > 0x7e || (r < 0x20 && r != '\t') {
				t.Errorf("header line %q carries %q, outside printable ASCII", l, r)
				break
			}
		}
	}
	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("parse the message: %v\n%s", err, msg)
	}
	return lines, m
}

// TestMailerHeadersCarryNoInjectedHeaderAndEncodeNonASCII: every header the
// mailer writes from data stays on its own line, so a subject (here a
// workspace's name) or an address with CR or LF in it adds no header, and a
// subject outside ASCII goes out as RFC 2047 encoded words a mail client
// decodes back to the text. The subject used to be written raw: "Zürich
// Labs" went out unencoded, and a workspace name with "\r\nBcc: ..." in it
// would have added a Bcc header to the stable release emails (#379, bug 64).
func TestMailerHeadersCarryNoInjectedHeaderAndEncodeNonASCII(t *testing.T) {
	long := strings.Repeat("Zürich Labs, ", 30) + "end"
	for _, tc := range []struct {
		name, to, subject   string
		wantTo, wantSubject string // as a mail client reads them
		wantRawSubject      string // as written, when it is pinned
		wantFolded          bool   // several encoded words, one to a line
	}{
		{name: "ASCII stays as it is", to: "ada@example.com", subject: "Agent run failed",
			wantTo: "ada@example.com", wantSubject: "Agent run failed", wantRawSubject: "Subject: Agent run failed"},
		{name: "a workspace name outside ASCII", to: "ada@example.com",
			subject: "Stable release 0.15.0 is ready for Zürich Labs",
			wantTo:  "ada@example.com", wantSubject: "Stable release 0.15.0 is ready for Zürich Labs",
			wantRawSubject: "Subject: =?utf-8?q?Stable_release_0.15.0_is_ready_for_Z=C3=BCrich_Labs?="},
		{name: "a workspace name with a header in it", to: "ada@example.com",
			subject:        "Stable release 0.15.0 is ready for Acme\r\nBcc: attacker@example.com",
			wantTo:         "ada@example.com",
			wantSubject:    "Stable release 0.15.0 is ready for Acme Bcc: attacker@example.com",
			wantRawSubject: "Subject: Stable release 0.15.0 is ready for Acme Bcc: attacker@example.com"},
		{name: "a bare LF, a bare CR and a blank line", to: "ada@example.com",
			subject:     "Acme\nBcc: a@example.com\rCc: b@example.com\n\nInjected body",
			wantTo:      "ada@example.com",
			wantSubject: "Acme Bcc: a@example.com Cc: b@example.com Injected body"},
		{name: "outside ASCII and a header in it", to: "ada@example.com",
			subject:     "Zürich\r\nBcc: attacker@example.com",
			wantTo:      "ada@example.com",
			wantSubject: "Zürich Bcc: attacker@example.com"},
		{name: "a long subject outside ASCII", to: "ada@example.com", subject: long,
			wantTo: "ada@example.com", wantSubject: long, wantFolded: true},
		{name: "an address with a header in it", to: "ada@example.com\r\nBcc: attacker@example.com",
			subject: "Agent run failed",
			wantTo:  "ada@example.com Bcc: attacker@example.com", wantSubject: "Agent run failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []wireMail
			if err := capturingMailer(&sent).Send(tc.to, tc.subject, "Body line one\nBcc: in the body stays body"); err != nil {
				t.Fatalf("send: %v", err)
			}
			if len(sent) != 1 {
				t.Fatalf("sent %d messages, want 1", len(sent))
			}
			lines, m := wireHeaders(t, sent[0].msg)
			var names []string
			for name := range m.Header {
				names = append(names, name)
			}
			sort.Strings(names)
			if want := []string{"Content-Type", "From", "Mime-Version", "Subject", "To"}; strings.Join(names, ",") != strings.Join(want, ",") {
				t.Errorf("headers %v, want exactly %v:\n%s", names, want, sent[0].msg)
			}
			for _, name := range names {
				if n := len(m.Header[name]); n != 1 {
					t.Errorf("%d %s headers, want 1", n, name)
				}
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
			if err != nil || subject != tc.wantSubject {
				t.Errorf("subject reads %q (%v), want %q", subject, err, tc.wantSubject)
			}
			if got := m.Header.Get("To"); got != tc.wantTo {
				t.Errorf("To reads %q, want %q", got, tc.wantTo)
			}
			if folded := len(lines) > 5 && strings.HasPrefix(lines[3], " =?utf-8?q?"); folded != tc.wantFolded {
				t.Errorf("subject folded %v, want %v:\n%s", folded, tc.wantFolded, sent[0].msg)
			}
			if tc.wantRawSubject != "" && lines[2] != tc.wantRawSubject {
				t.Errorf("subject line %q, want %q", lines[2], tc.wantRawSubject)
			}
			body, _ := io.ReadAll(m.Body)
			if string(body) != "Body line one\r\nBcc: in the body stays body" {
				t.Errorf("body %q", body)
			}
		})
	}
}

// TestStableReleaseEmailCarriesNoHeaderFromTheWorkspaceName drives the path
// the bug was found on: an operator emails release_scheduled and
// release_published, and a stable-channel workspace's name carries a line
// break and a would-be header. Each email the scheduler sends names the
// workspace in its subject, and adds no header (#379, bug 64).
func TestStableReleaseEmailCarriesNoHeaderFromTheWorkspaceName(t *testing.T) {
	var sent []wireMail
	dir := &fakeDir{byID: map[string]*users.User{
		"o1-admin":  optedIn("o1-admin", "admin@example.com"),
		"o1-member": optedIn("o1-member", "member@example.com"),
	}}
	emails := NewEmailDispatcher(capturingMailer(&sent), dir, "https://app.example.com",
		[]string{notifications.TypeReleaseScheduled, notifications.TypeReleasePublished})
	stable := stableOf("0.4.0", "2026-10-01", release.Category{Name: release.CategoryFixes, Notes: []string{"A fix"}})
	org := &orgs.Org{ID: "o1", Name: "Acme\r\nBcc: attacker@example.com", StableRelease: "0.3.0"}
	s, _, _ := schedulerFixture(stable, []*orgs.Org{org})
	s.SetEmailDispatcher(emails)
	s.now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	s.Run()

	if len(sent) != 3 {
		t.Fatalf("sent %d emails, want 3 (the admin's announcement, and the turn-on to both members)", len(sent))
	}
	for _, w := range sent {
		_, m := wireHeaders(t, w.msg)
		if bcc := m.Header["Bcc"]; len(bcc) != 0 {
			t.Errorf("a Bcc header reached the wire: %q\n%s", bcc, w.msg)
		}
		subject, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
		if !strings.Contains(subject, "Acme Bcc: attacker@example.com") {
			t.Errorf("subject %q does not name the workspace on one line", subject)
		}
	}
}
