package users

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sessionCeilingRuns numbers the runs of TestSessionPolicyFromEnvAtTheCeiling
// within one test binary: a lifetime is warned about once per variable and
// value for the life of the process, so each run (go test -count=2) spells
// its values above the ceiling differently.
var sessionCeilingRuns atomic.Int64

// TestSessionPolicyFromEnvAtTheCeiling pins the session lifetime's clamp at
// its ceilings, a characterization pin for refactor step X10b (#379, plan
// X10): a value even a nanosecond above 720h, or 168h, clamps to the ceiling
// with a warning naming the variable, and the ceiling itself, or a value
// below it, is kept with none. The other variable, unset, keeps its default
// with no warning. The warning's wording is not pinned. Each value above the
// ceiling is one unit more with each run in the process (the first run's is
// the nanosecond, second or minute above), so that it is new to the warn-once
// cache every time.
func TestSessionPolicyFromEnvAtTheCeiling(t *testing.T) {
	run := sessionCeilingRuns.Add(1)
	var logged bytes.Buffer
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	cases := []struct {
		name, value string
		want        time.Duration
		warns       bool
	}{
		{envSessionMaxAge, fmt.Sprintf("720h0m%ds", run), DefaultSessionMaxAge, true},
		{envSessionMaxAge, fmt.Sprintf(" 720h%dm0s ", run), DefaultSessionMaxAge, true},
		{envSessionMaxAge, fmt.Sprintf("720h%dns", run), DefaultSessionMaxAge, true},
		{envSessionMaxAge, "720h", DefaultSessionMaxAge, false},
		{envSessionMaxAge, "43200m", DefaultSessionMaxAge, false},
		{envSessionMaxAge, "719h59m59s", 719*time.Hour + 59*time.Minute + 59*time.Second, false},
		{envSessionIdle, fmt.Sprintf("168h0m%ds", run), DefaultSessionIdle, true},
		{envSessionIdle, fmt.Sprintf("168h%dns", run), DefaultSessionIdle, true},
		{envSessionIdle, "168h", DefaultSessionIdle, false},
		{envSessionIdle, "167h59m59s", 167*time.Hour + 59*time.Minute + 59*time.Second, false},
	}
	for _, tc := range cases {
		t.Setenv(envSessionMaxAge, "")
		t.Setenv(envSessionIdle, "")
		t.Setenv(tc.name, tc.value)
		logged.Reset()
		p := SessionPolicyFromEnv()
		got, other, otherName, otherWant := p.MaxAge, p.Idle, envSessionIdle, DefaultSessionIdle
		if tc.name == envSessionIdle {
			got, other, otherName, otherWant = p.Idle, p.MaxAge, envSessionMaxAge, DefaultSessionMaxAge
		}
		if got != tc.want || other != otherWant {
			t.Errorf("%s=%q: SessionPolicyFromEnv() = %+v, want %s %v and %s %v", tc.name, tc.value, p, tc.name, tc.want, otherName, otherWant)
		}
		warned := map[string]bool{}
		for _, line := range strings.Split(logged.String(), "\n") {
			for _, name := range []string{envSessionMaxAge, envSessionIdle} {
				if strings.Contains(line, "level=WARN") && strings.Contains(line, name) {
					warned[name] = true
				}
			}
		}
		if warned[tc.name] != tc.warns || warned[otherName] {
			t.Errorf("%s=%q: warnings name %s: %v, %s: %v; want %v and false; log:\n%s",
				tc.name, tc.value, tc.name, warned[tc.name], otherName, warned[otherName], tc.warns, logged.String())
		}
	}
}
