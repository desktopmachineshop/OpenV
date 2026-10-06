package notify

import (
	"errors"
	"strings"
	"sync/atomic"
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

// verificationSwitchRuns numbers the runs of the test below within one test
// binary: internal/envparse names a variable once per value for the life of
// the process, so each run (go test -count=2) spells its value differently.
var verificationSwitchRuns atomic.Int64

// Only off switches verification off (#379, bug 225). Any other value, false
// and 0 among them, keeps verification on where the mailer can send, as
// before, but is no longer passed over in silence: the log names the
// variable and the value it takes once, never the value it has. A blank
// value is silent.
func TestVerificationSwitchWarnsOnAValueItDoesNotTake(t *testing.T) {
	// false, then false with spaces after it on each later run, which the
	// switch trims.
	value := "false" + strings.Repeat(" ", int(verificationSwitchRuns.Add(1)-1))
	log := captureSlog(t)
	t.Setenv("OPENV_EMAIL_VERIFICATION", value)
	for range 3 {
		if !VerificationPolicyFromEnv(&captureMailer{enabled: true}).Required {
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
		if got := VerificationPolicyFromEnv(&captureMailer{enabled: true}).Required; got == off {
			t.Errorf("OPENV_EMAIL_VERIFICATION=%q: required %v, want %v", quiet, got, !off)
		}
		if strings.Contains(log.String(), "level=WARN") {
			t.Errorf("OPENV_EMAIL_VERIFICATION=%q warned:\n%s", quiet, log)
		}
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
