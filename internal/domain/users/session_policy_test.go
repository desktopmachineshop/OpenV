package users

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
)

// sessionWarningRuns numbers the runs of the test below within one test
// binary: a session lifetime warns once per variable and value for the life
// of the process, so each run (go test -count=2) sets values of its own.
var sessionWarningRuns atomic.Int64

// A session lifetime the server cannot use keeps its default, and one above
// the ceiling is clamped to it; either way the log says so once per
// variable and value, however often the policy is read, naming the variable
// and what applies, never the value (#379, bug 222): a secret pasted into
// the wrong variable must not reach the log.
func TestSessionLifetimeWarningsNameTheVariableOnce(t *testing.T) {
	run := sessionWarningRuns.Add(1)
	unusable := fmt.Sprintf("sk_live_bug222_run%d", run)
	above := fmt.Sprintf("%dh", 100000+run)
	t.Setenv(envSessionMaxAge, unusable)
	t.Setenv(envSessionIdle, above)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for range 3 {
		if p := SessionPolicyFromEnv(); p.MaxAge != DefaultSessionMaxAge || p.Idle != DefaultSessionIdle {
			t.Errorf("policy = %v/%v, want the defaults %v/%v", p.MaxAge, p.Idle, DefaultSessionMaxAge, DefaultSessionIdle)
		}
	}
	logged := buf.String()
	for _, name := range []string{envSessionMaxAge, envSessionIdle} {
		if n := strings.Count(logged, "level=WARN"); n != 2 || strings.Count(logged, "var="+name+" ") != 1 {
			t.Errorf("three reads: want one warning naming %s, and two in all, got:\n%s", name, logged)
		}
	}
	for _, value := range []string{unusable, above} {
		if strings.Contains(logged, value) {
			t.Errorf("a warning printed the value %q:\n%s", value, logged)
		}
	}
}
