package config

import (
	"bytes"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/notify"
)

// Stage notify's settings tests of internal/notify and internal/api, as
// notify.MailerFromEnv, notify.VAPIDFromEnv, notify.PushTypesFromEnv,
// notify.VerificationPolicyFromEnv and api.RegistrationPolicyFromEnv read the
// environment themselves, held here against the accessors, which read it
// once refactor step X10b takes those helpers out of production code.
// Each reads the process environment, set with t.Setenv, through a Config
// loaded from it.

// captureSlog sends slog's default logger to a buffer for the test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// credentialWarning is the line internal/envparse logs for a credential
// with spaces or a line break around it, less its time.
func credentialWarning(name string) string {
	return `level=WARN msg="a credential setting has spaces or a line break around it; it is used exactly as set" var=` + name
}

// credentialRuns numbers the runs of the tests below within one test binary.
var credentialRuns atomic.Int64

// freshCredential is a credential value no earlier run in this process has
// set: internal/envparse warns once per variable and value for the life of
// the process, by design, so a repeated run (go test -count=2) setting the
// same value would see no warning and fail on what a fresh process does.
// prefix and suffix are the spaces or line break around it under test.
func freshCredential(prefix, value, suffix string) string {
	return prefix + value + "_" + strconv.FormatInt(credentialRuns.Add(1), 10) + suffix
}

// TestSMTPKeepsTheCredentialsExactlyAsSet: the SMTP user and password are
// credentials, used exactly as set (#379, question 24), and one with spaces
// or a line break around it is named once in the log, never printed.
func TestSMTPKeepsTheCredentialsExactlyAsSet(t *testing.T) {
	log := captureSlog(t)
	t.Setenv("OPENV_SMTP_HOST", "smtp.example.com")
	t.Setenv("OPENV_SMTP_PORT", "")
	user := freshCredential(" ", "apikey", "")
	pass := freshCredential("", "sk_smtp_do_not_log", "\n")
	t.Setenv("OPENV_SMTP_USER", user)
	t.Setenv("OPENV_SMTP_PASSWORD", pass)
	t.Setenv("OPENV_SMTP_FROM", "ops@example.com")
	s := Load(os.LookupEnv).SMTP()
	if s.User != user || s.Password != pass {
		t.Errorf("user %q, password %q: want both exactly as set (%q, %q)", s.User, s.Password, user, pass)
	}
	for _, name := range []string{"OPENV_SMTP_USER", "OPENV_SMTP_PASSWORD"} {
		if strings.Count(log.String(), credentialWarning(name)+"\n") != 1 {
			t.Errorf("want one warning naming %s, got:\n%s", name, log.String())
		}
	}
	if strings.Contains(log.String(), "sk_smtp") || strings.Contains(log.String(), "apikey") {
		t.Errorf("the log printed a credential:\n%s", log.String())
	}
}

// TestSMTPDefaults: the port is 587 by default, and the From address falls
// back to the user, exactly as set.
func TestSMTPDefaults(t *testing.T) {
	quietLog(t)
	t.Setenv("OPENV_SMTP_HOST", "smtp.example.com")
	t.Setenv("OPENV_SMTP_PORT", "")
	t.Setenv("OPENV_SMTP_USER", "bot@example.com")
	t.Setenv("OPENV_SMTP_PASSWORD", "secret")
	t.Setenv("OPENV_SMTP_FROM", "")
	s := Load(os.LookupEnv).SMTP()
	if s.Port != "587" {
		t.Errorf("default port = %q, want 587", s.Port)
	}
	if s.From != "bot@example.com" {
		t.Errorf("from = %q, want fallback to user", s.From)
	}
}

// TestVAPIDKeepsThePrivateKeyExactlyAsSet: the VAPID private key is a
// credential, used exactly as set (#379, question 24), where it used to be
// trimmed, and named once in the log when a line break follows it. One of
// only spaces still counts as missing, as it always did, so push stays off.
func TestVAPIDKeepsThePrivateKeyExactlyAsSet(t *testing.T) {
	log := captureSlog(t)
	t.Setenv("OPENV_VAPID_PUBLIC_KEY", "pub")
	t.Setenv("OPENV_VAPID_SUBJECT", "mailto:ops@example.com")
	key := freshCredential("", "vapid_do_not_log", "\n")
	t.Setenv("OPENV_VAPID_PRIVATE_KEY", key)
	c := Load(os.LookupEnv).VAPID()
	if c.PrivateKey != key || !c.Enabled() {
		t.Errorf("private key %q, enabled %v: want it exactly as set (%q), and push on", c.PrivateKey, c.Enabled(), key)
	}
	if strings.Count(log.String(), credentialWarning("OPENV_VAPID_PRIVATE_KEY")+"\n") != 1 {
		t.Errorf("want one warning naming OPENV_VAPID_PRIVATE_KEY, got:\n%s", log.String())
	}

	log.Reset()
	t.Setenv("OPENV_VAPID_PRIVATE_KEY", "   ")
	c = Load(os.LookupEnv).VAPID()
	if c.PrivateKey != "   " || c.Enabled() {
		t.Errorf("private key %q, enabled %v: want it as set, and push off", c.PrivateKey, c.Enabled())
	}
	if !strings.Contains(log.String(), `msg="push: VAPID key pair incomplete; web push disabled" have_public=true have_private=false`) {
		t.Errorf("a private key of only spaces should read as missing:\n%s", log.String())
	}
	if strings.Contains(log.String(), "vapid_do_not_log") {
		t.Errorf("the log printed the private key:\n%s", log.String())
	}
}

// TestPushTypes: the override is parsed as the email one is.
func TestPushTypes(t *testing.T) {
	t.Setenv("OPENV_PUSH_NOTIFICATION_TYPES", " run_failed , ,mention ")
	got := Load(os.LookupEnv).PushTypes()
	if len(got) != 2 || got[0] != notifications.TypeRunFailed || got[1] != notifications.TypeMention {
		t.Fatalf("PushTypes() = %v, want [run_failed mention]", got)
	}
	t.Setenv("OPENV_PUSH_NOTIFICATION_TYPES", " , ")
	if got, want := Load(os.LookupEnv).PushTypes(), len(notify.DefaultPushTypes()); len(got) != want {
		t.Fatalf("a separators-only override must fall back to the default, got %v", got)
	}
}

// mailerWith is a notify.Mailer whose Enabled is fixed.
type mailerWith struct{ enabled bool }

func (m mailerWith) Enabled() bool           { return m.enabled }
func (mailerWith) Send(_, _, _ string) error { return nil }

func TestEmailVerification(t *testing.T) {
	quietLog(t)
	t.Setenv("OPENV_EMAIL_VERIFICATION", "")
	if Load(os.LookupEnv).EmailVerification(mailerWith{enabled: false}).Required {
		t.Error("a disabled mailer must not require verification")
	}
	if !Load(os.LookupEnv).EmailVerification(mailerWith{enabled: true}).Required {
		t.Error("an enabled mailer must require verification by default")
	}
	t.Setenv("OPENV_EMAIL_VERIFICATION", "OFF")
	if Load(os.LookupEnv).EmailVerification(mailerWith{enabled: true}).Required {
		t.Error("OPENV_EMAIL_VERIFICATION=off must switch verification off")
	}
	if Load(os.LookupEnv).EmailVerification(nil).Required {
		t.Error("a nil mailer must not require verification")
	}
}

// verificationSwitchRuns numbers the runs of the test below within one test
// binary: internal/envparse names a variable once per value for the life of
// the process, so each run (go test -count=2) spells its value differently.
var verificationSwitchRuns atomic.Int64

// verificationSwitchOffset keeps the values the test below sets apart from
// those the other tests of this package set.
const verificationSwitchOffset = 16

// Only off switches verification off (#379, bug 225). Any other value, false
// and 0 among them, keeps verification on where the mailer can send, as
// before, but is no longer passed over in silence: the log names the
// variable and the value it takes once, never the value it has. A blank
// value is silent.
func TestVerificationSwitchWarnsOnAValueItDoesNotTake(t *testing.T) {
	// false, with spaces after it, more on each later run, which the switch
	// trims.
	value := "false" + strings.Repeat(" ", int(verificationSwitchRuns.Add(1)+verificationSwitchOffset))
	log := captureSlog(t)
	t.Setenv("OPENV_EMAIL_VERIFICATION", value)
	for range 3 {
		if !Load(os.LookupEnv).EmailVerification(mailerWith{enabled: true}).Required {
			t.Errorf("OPENV_EMAIL_VERIFICATION=%q switched verification off", value)
		}
	}
	warning := `level=WARN msg="ignoring a malformed setting; its default applies" var=OPENV_EMAIL_VERIFICATION want="off (any case), or unset"`
	if n := strings.Count(log.String(), "level=WARN"); n != 1 || !strings.Contains(log.String(), warning) {
		t.Errorf("OPENV_EMAIL_VERIFICATION=%q read three times: want the one warning\n%s\ngot:\n%s", value, warning, log)
	}
	if strings.Contains(log.String(), "false") {
		t.Errorf("the warning printed the value:\n%s", log)
	}

	for _, quiet := range []string{"off", " OFF ", "", "   "} {
		log.Reset()
		t.Setenv("OPENV_EMAIL_VERIFICATION", quiet)
		off := strings.TrimSpace(quiet) != ""
		if got := Load(os.LookupEnv).EmailVerification(mailerWith{enabled: true}).Required; got == off {
			t.Errorf("OPENV_EMAIL_VERIFICATION=%q: required %v, want %v", quiet, got, !off)
		}
		if strings.Contains(log.String(), "level=WARN") {
			t.Errorf("OPENV_EMAIL_VERIFICATION=%q warned:\n%s", quiet, log)
		}
	}
}

// An OPENV_REGISTRATION the server does not know leaves registration open
// and says so, naming the variable and never the value (#379, bug 222): a
// secret pasted into the wrong variable must not reach the log.
func TestAnUnknownRegistrationPolicyIsNamedNotQuoted(t *testing.T) {
	const value = "invite-only-sk_live_bug222"
	t.Setenv("OPENV_REGISTRATION", value)
	log := captureSlog(t)

	if got := Load(os.LookupEnv).Registration(); got != registrationOpen {
		t.Errorf("OPENV_REGISTRATION=%q: policy %q, want %q", value, got, registrationOpen)
	}
	logged := log.String()
	if n := strings.Count(logged, "level=WARN"); n != 1 || !strings.Contains(logged, "OPENV_REGISTRATION") {
		t.Errorf("OPENV_REGISTRATION=%q: want one warning naming the variable, got:\n%s", value, logged)
	}
	if strings.Contains(logged, value) {
		t.Errorf("the warning printed the value of OPENV_REGISTRATION:\n%s", logged)
	}
}
