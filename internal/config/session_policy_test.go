package config

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// The session lifetime's tests of internal/domain/users, as they read
// OPENV_SESSION_MAX_AGE and OPENV_SESSION_IDLE through users.SessionPolicyFromEnv,
// held here against SessionPolicy, which reads them once refactor step X10b
// takes that helper out of production code. Each reads the process environment, set with t.Setenv,
// through a Config loaded from it. This package's session lifetimes warn
// once per variable and value for the life of the test binary, which the
// other tests here share, so the values these set are their own.

const (
	envSessionMaxAge = "OPENV_SESSION_MAX_AGE"
	envSessionIdle   = "OPENV_SESSION_IDLE"
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
	above := fmt.Sprintf("%dh", 200000+run)
	t.Setenv(envSessionMaxAge, unusable)
	t.Setenv(envSessionIdle, above)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for range 3 {
		if p := Load(os.LookupEnv).SessionPolicy(); p.MaxAge != users.DefaultSessionMaxAge || p.Idle != users.DefaultSessionIdle {
			t.Errorf("policy = %v/%v, want the defaults %v/%v", p.MaxAge, p.Idle, users.DefaultSessionMaxAge, users.DefaultSessionIdle)
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

func TestSessionPolicyClampsAndDefaults(t *testing.T) {
	quietLog(t)
	cases := []struct {
		name             string
		maxAge, idle     string
		wantMax, wantIdl time.Duration
	}{
		{"unset uses the defaults", "", "", users.DefaultSessionMaxAge, users.DefaultSessionIdle},
		{"an operator may shorten", "12h", "45m", 12 * time.Hour, 45 * time.Minute},
		{"above the ceiling is clamped", "8760h", "8760h", users.DefaultSessionMaxAge, users.DefaultSessionIdle},
		{"unparseable falls back", "30d", "week", users.DefaultSessionMaxAge, users.DefaultSessionIdle},
		{"non-positive falls back", "0s", "-4h", users.DefaultSessionMaxAge, users.DefaultSessionIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envSessionMaxAge, tc.maxAge)
			t.Setenv(envSessionIdle, tc.idle)

			p := Load(os.LookupEnv).SessionPolicy()
			if p.MaxAge != tc.wantMax || p.Idle != tc.wantIdl {
				t.Errorf("policy = %v/%v, want %v/%v", p.MaxAge, p.Idle, tc.wantMax, tc.wantIdl)
			}
		})
	}
}

// sessionCeilingRuns numbers the runs of TestSessionPolicyAtTheCeiling
// within one test binary: a lifetime is warned about once per variable and
// value for the life of the process, so each run (go test -count=2) spells
// its values above the ceiling differently.
var sessionCeilingRuns atomic.Int64

// sessionCeilingOffset keeps the values TestSessionPolicyAtTheCeiling sets
// apart from those the other tests of this package set.
const sessionCeilingOffset = 100

// TestSessionPolicyAtTheCeiling pins the session lifetime's clamp at its
// ceilings, the characterization pin refactor step X10b's pin pull request
// (#570) added beside users.SessionPolicyFromEnv: a value even a nanosecond
// above 720h, or 168h, clamps to the ceiling with a warning naming the
// variable, and the ceiling itself, or a value below it, is kept with none.
// The other variable, unset, keeps its default with no warning. The
// warning's wording is not pinned. Each value above the ceiling is one unit
// more with each run in the process, past sessionCeilingOffset units above
// it, so that it is new to the warn-once cache every time.
func TestSessionPolicyAtTheCeiling(t *testing.T) {
	run := sessionCeilingRuns.Add(1) + sessionCeilingOffset
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
		{envSessionMaxAge, fmt.Sprintf("720h0m%ds", run), users.DefaultSessionMaxAge, true},
		{envSessionMaxAge, fmt.Sprintf(" 720h%dm0s ", run), users.DefaultSessionMaxAge, true},
		{envSessionMaxAge, fmt.Sprintf("720h%dns", run), users.DefaultSessionMaxAge, true},
		{envSessionMaxAge, "720h", users.DefaultSessionMaxAge, false},
		{envSessionMaxAge, "43200m", users.DefaultSessionMaxAge, false},
		{envSessionMaxAge, "719h59m59s", 719*time.Hour + 59*time.Minute + 59*time.Second, false},
		{envSessionIdle, fmt.Sprintf("168h0m%ds", run), users.DefaultSessionIdle, true},
		{envSessionIdle, fmt.Sprintf("168h%dns", run), users.DefaultSessionIdle, true},
		{envSessionIdle, "168h", users.DefaultSessionIdle, false},
		{envSessionIdle, "167h59m59s", 167*time.Hour + 59*time.Minute + 59*time.Second, false},
	}
	for _, tc := range cases {
		t.Setenv(envSessionMaxAge, "")
		t.Setenv(envSessionIdle, "")
		t.Setenv(tc.name, tc.value)
		logged.Reset()
		p := Load(os.LookupEnv).SessionPolicy()
		got, other, otherName, otherWant := p.MaxAge, p.Idle, envSessionIdle, users.DefaultSessionIdle
		if tc.name == envSessionIdle {
			got, other, otherName, otherWant = p.Idle, p.MaxAge, envSessionMaxAge, users.DefaultSessionMaxAge
		}
		if got != tc.want || other != otherWant {
			t.Errorf("%s=%q: SessionPolicy() = %+v, want %s %v and %s %v", tc.name, tc.value, p, tc.name, tc.want, otherName, otherWant)
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
