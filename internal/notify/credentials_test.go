package notify

import (
	"bytes"
	"log/slog"
	"strings"
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

// TestNewMailerKeepsTheCredentialsExactlyAsSet: the SMTP user and password
// are credentials, used exactly as given (#379, question 24; cmd/server
// reads them exactly as set, internal/config's SMTP), and the mailer's log
// line prints neither.
func TestNewMailerKeepsTheCredentialsExactlyAsSet(t *testing.T) {
	log := captureSlog(t)
	user, pass := " apikey", "sk_smtp_do_not_log\n"
	m := NewMailer(SMTPSettings{Host: "smtp.example.com", Port: "587", User: user, Password: pass, From: "ops@example.com"})
	if m.user != user || m.pass != pass {
		t.Errorf("user %q, password %q: want both exactly as given (%q, %q)", m.user, m.pass, user, pass)
	}
	if strings.Contains(log.String(), "sk_smtp") || strings.Contains(log.String(), "apikey") {
		t.Errorf("the log printed a credential:\n%s", log.String())
	}
}

// TestNewMailerKeepsTheSMTPUserOutOfTheLogAsTheFromAddress: with
// OPENV_SMTP_FROM unset, mail goes out from OPENV_SMTP_USER, a credential,
// which the boot log names as the From address rather than printing (#379,
// question 24), as an access-key-style SMTP user would otherwise be.
func TestNewMailerKeepsTheSMTPUserOutOfTheLogAsTheFromAddress(t *testing.T) {
	log := captureSlog(t)
	settings := SMTPSettings{Host: "email-smtp.eu-west-1.amazonaws.com", Port: "587", User: " AKIAEXAMPLEDONOTLOG"}
	m := NewMailer(settings)
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
	settings.From = "ops@example.com"
	NewMailer(settings)
	if !strings.Contains(log.String(), `msg="email: SMTP delivery enabled" host=email-smtp.eu-west-1.amazonaws.com port=587 from=ops@example.com`+"\n") {
		t.Errorf("want the boot log to show OPENV_SMTP_FROM, got:\n%s", log.String())
	}
}
