package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openv/requirements-platform/internal/config"
)

// TestEnvSecretKeepsACredentialExactlyAsSet: every credential the server
// reads goes through internal/config's credential helpers (env_vars.txt's
// read column says which), here those of stage config, and
// by the maintainer's decision on #379's question 24 it is used exactly as
// set, not trimmed as the other settings are: a WORKER_API_KEY with a line
// break after it, or a DB_PASSWORD with a space in front, reaches its use
// with them, and the log names each once, never printing it. Only an unset
// or empty variable falls back, so DB_PASSWORD's default applies to nothing
// else.
//
// internal/envparse names a variable once per value for the life of the
// process, so each run of the test (go test -count=2) sets values no earlier
// run set.
func TestEnvSecretKeepsACredentialExactlyAsSet(t *testing.T) {
	run := envSecretRuns.Add(1)
	workerKey := fmt.Sprintf("wk-do-not-log-%d\n", run)
	password := fmt.Sprintf(" pw-do-not-log-%d", run)
	spaces := strings.Repeat(" ", 2+int(run))
	var log bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Setenv("WORKER_API_KEY", workerKey)
	t.Setenv("DB_PASSWORD", password)
	for range 2 {
		if got := config.Load(os.LookupEnv).WorkerAPIKey(); got != workerKey {
			t.Errorf("WORKER_API_KEY read as %q, want it exactly as set", got)
		}
	}
	if got := config.Load(os.LookupEnv).DatabaseParts().Password; got != password {
		t.Errorf("DB_PASSWORD read as %q, want it exactly as set", got)
	}
	t.Setenv("DB_PASSWORD", "")
	if got := config.Load(os.LookupEnv).DatabaseParts().Password; got != "postgres" {
		t.Errorf("an empty DB_PASSWORD read as %q, want the default", got)
	}
	t.Setenv("DB_PASSWORD", spaces)
	if got := config.Load(os.LookupEnv).DatabaseParts().Password; got != spaces {
		t.Errorf("a DB_PASSWORD of spaces read as %q, want it exactly as set, not the default", got)
	}

	const warning = `level=WARN msg="a credential setting has spaces or a line break around it; it is used exactly as set" var=`
	want := warning + "WORKER_API_KEY\n" + warning + "DB_PASSWORD\n" + warning + "DB_PASSWORD\n"
	if log.String() != want {
		t.Errorf("log:\n%s\nwant:\n%s", log.String(), want)
	}
	if strings.Contains(log.String(), "do-not-log") {
		t.Errorf("the log printed a credential:\n%s", log.String())
	}
}

// envSecretRuns numbers the runs of TestEnvSecretKeepsACredentialExactlyAsSet
// in this process.
var envSecretRuns atomic.Int64
