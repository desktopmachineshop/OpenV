package api

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// An OPENV_REGISTRATION the server does not know leaves registration open
// and says so, naming the variable and never the value (#379, bug 222): a
// secret pasted into the wrong variable must not reach the log.
func TestAnUnknownRegistrationPolicyIsNamedNotQuoted(t *testing.T) {
	const value = "invite-only-sk_live_bug222"
	t.Setenv(envRegistration, value)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if got := RegistrationPolicyFromEnv(); got != RegistrationOpen {
		t.Errorf("OPENV_REGISTRATION=%q: policy %q, want %q", value, got, RegistrationOpen)
	}
	logged := buf.String()
	if n := strings.Count(logged, "level=WARN"); n != 1 || !strings.Contains(logged, envRegistration) {
		t.Errorf("OPENV_REGISTRATION=%q: want one warning naming the variable, got:\n%s", value, logged)
	}
	if strings.Contains(logged, value) {
		t.Errorf("the warning printed the value of OPENV_REGISTRATION:\n%s", logged)
	}
}
