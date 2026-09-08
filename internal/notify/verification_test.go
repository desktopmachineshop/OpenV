package notify

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// slowMailer never answers, standing in for an SMTP server that hangs.
type slowMailer struct{ block chan struct{} }

func (m *slowMailer) Enabled() bool { return true }
func (m *slowMailer) Send(string, string, string) error {
	<-m.block
	return nil
}

// failMailer is enabled but every send fails.
type failMailer struct{}

func (failMailer) Enabled() bool                     { return true }
func (failMailer) Send(string, string, string) error { return errors.New("smtp down") }

func TestVerificationPolicyFromEnv(t *testing.T) {
	t.Setenv("OPENV_EMAIL_VERIFICATION", "")
	if VerificationPolicyFromEnv(&captureMailer{enabled: false}).Required {
		t.Error("a disabled mailer must not require verification")
	}
	if !VerificationPolicyFromEnv(&captureMailer{enabled: true}).Required {
		t.Error("an enabled mailer must require verification by default")
	}
	t.Setenv("OPENV_EMAIL_VERIFICATION", "OFF")
	if VerificationPolicyFromEnv(&captureMailer{enabled: true}).Required {
		t.Error("OPENV_EMAIL_VERIFICATION=off must switch verification off")
	}
	if VerificationPolicyFromEnv(nil).Required {
		t.Error("a nil mailer must not require verification")
	}
}

func TestVerificationLinkAndEmail(t *testing.T) {
	link := VerificationLink("https://app.example.com/", "ab c/d")
	if link != "https://app.example.com/verify-email?token=ab+c%2Fd" {
		t.Errorf("link = %q", link)
	}
	subject, body := RenderVerificationEmail("Sam", link, 24*time.Hour)
	if subject != "Verify your email for OpenV" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"Hi Sam,", link, "24 hours", "works once"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	_, body = RenderVerificationEmail("  ", link, time.Hour)
	if !strings.HasPrefix(body, "Hi,") {
		t.Errorf("anonymous greeting = %q", body[:8])
	}
}

func TestSendWithTimeout(t *testing.T) {
	m := &captureMailer{enabled: true}
	if err := SendWithTimeout(m, "a@example.com", "s", "b", time.Second); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(m.to) != 1 || m.to[0] != "a@example.com" {
		t.Errorf("recipients = %v", m.to)
	}
	if err := SendWithTimeout(failMailer{}, "a@example.com", "s", "b", time.Second); err == nil {
		t.Error("a failing mailer must surface its error")
	}
	slow := &slowMailer{block: make(chan struct{})}
	defer close(slow.block)
	if err := SendWithTimeout(slow, "a@example.com", "s", "b", 20*time.Millisecond); !errors.Is(err, ErrSendTimeout) {
		t.Errorf("hung send returned %v, want ErrSendTimeout", err)
	}
}
