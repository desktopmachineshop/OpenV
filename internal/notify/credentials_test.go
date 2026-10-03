package notify

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

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

// TestMailerFromEnvKeepsTheCredentialsExactlyAsSet: the SMTP user and
// password are credentials, used exactly as set (#379, question 24), and one
// with spaces or a line break around it is named once in the log, never
// printed.
func TestMailerFromEnvKeepsTheCredentialsExactlyAsSet(t *testing.T) {
	log := captureSlog(t)
	t.Setenv("OPENV_SMTP_HOST", "smtp.example.com")
	t.Setenv("OPENV_SMTP_PORT", "")
	user := freshCredential(" ", "apikey", "")
	pass := freshCredential("", "sk_smtp_do_not_log", "\n")
	t.Setenv("OPENV_SMTP_USER", user)
	t.Setenv("OPENV_SMTP_PASSWORD", pass)
	t.Setenv("OPENV_SMTP_FROM", "ops@example.com")
	m := MailerFromEnv()
	if m.user != user || m.pass != pass {
		t.Errorf("user %q, password %q: want both exactly as set (%q, %q)", m.user, m.pass, user, pass)
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

// TestMailerFromEnvKeepsTheSMTPUserOutOfTheLogAsTheFromAddress: with
// OPENV_SMTP_FROM unset, mail goes out from OPENV_SMTP_USER, a credential,
// which the boot log names as the From address rather than printing (#379,
// question 24), as an access-key-style SMTP user would otherwise be.
func TestMailerFromEnvKeepsTheSMTPUserOutOfTheLogAsTheFromAddress(t *testing.T) {
	log := captureSlog(t)
	t.Setenv("OPENV_SMTP_HOST", "email-smtp.eu-west-1.amazonaws.com")
	t.Setenv("OPENV_SMTP_PORT", "")
	t.Setenv("OPENV_SMTP_USER", " AKIAEXAMPLEDONOTLOG")
	t.Setenv("OPENV_SMTP_PASSWORD", "")
	t.Setenv("OPENV_SMTP_FROM", "")
	m := MailerFromEnv()
	if m.from != " AKIAEXAMPLEDONOTLOG" {
		t.Errorf("from %q: want OPENV_SMTP_USER exactly as set", m.from)
	}
	if strings.Contains(log.String(), "AKIAEXAMPLEDONOTLOG") {
		t.Errorf("the log printed the SMTP user:\n%s", log.String())
	}
	if !strings.Contains(log.String(), `msg="email: SMTP delivery enabled" host=email-smtp.eu-west-1.amazonaws.com port=587 from=OPENV_SMTP_USER`+"\n") {
		t.Errorf("want the boot log to name OPENV_SMTP_USER as the From address, got:\n%s", log.String())
	}

	log.Reset()
	t.Setenv("OPENV_SMTP_FROM", "ops@example.com")
	MailerFromEnv()
	if !strings.Contains(log.String(), `msg="email: SMTP delivery enabled" host=email-smtp.eu-west-1.amazonaws.com port=587 from=ops@example.com`+"\n") {
		t.Errorf("want the boot log to show OPENV_SMTP_FROM, got:\n%s", log.String())
	}
}

// TestVAPIDFromEnvKeepsThePrivateKeyExactlyAsSet: the VAPID private key is a
// credential, used exactly as set (#379, question 24), where it used to be
// trimmed, and named once in the log when a line break follows it. One of
// only spaces still counts as missing, as it always did, so push stays off.
func TestVAPIDFromEnvKeepsThePrivateKeyExactlyAsSet(t *testing.T) {
	log := captureSlog(t)
	t.Setenv("OPENV_VAPID_PUBLIC_KEY", "pub")
	t.Setenv("OPENV_VAPID_SUBJECT", "mailto:ops@example.com")
	key := freshCredential("", "vapid_do_not_log", "\n")
	t.Setenv("OPENV_VAPID_PRIVATE_KEY", key)
	c := VAPIDFromEnv()
	if c.PrivateKey != key || !c.Enabled() {
		t.Errorf("private key %q, enabled %v: want it exactly as set (%q), and push on", c.PrivateKey, c.Enabled(), key)
	}
	if strings.Count(log.String(), credentialWarning("OPENV_VAPID_PRIVATE_KEY")+"\n") != 1 {
		t.Errorf("want one warning naming OPENV_VAPID_PRIVATE_KEY, got:\n%s", log.String())
	}

	log.Reset()
	t.Setenv("OPENV_VAPID_PRIVATE_KEY", "   ")
	c = VAPIDFromEnv()
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
