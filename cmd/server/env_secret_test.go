package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestEnvSecretKeepsACredentialExactlyAsSet: every credential the server
// reads goes through envSecret (env_vars.txt's read column says which), and
// by the maintainer's decision on #379's question 24 it is used exactly as
// set, not trimmed as the other settings are: a WORKER_API_KEY with a line
// break after it, or a DB_PASSWORD with a space in front, reaches its use
// with them, and the log names each once, never printing it. Only an unset
// or empty variable falls back, so DB_PASSWORD's default applies to nothing
// else.
func TestEnvSecretKeepsACredentialExactlyAsSet(t *testing.T) {
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

	t.Setenv("WORKER_API_KEY", "wk-do-not-log\n")
	t.Setenv("DB_PASSWORD", " pw-do-not-log")
	for range 2 {
		if got := envSecret("WORKER_API_KEY", ""); got != "wk-do-not-log\n" {
			t.Errorf("WORKER_API_KEY read as %q, want it exactly as set", got)
		}
	}
	if got := envSecret("DB_PASSWORD", "postgres"); got != " pw-do-not-log" {
		t.Errorf("DB_PASSWORD read as %q, want it exactly as set", got)
	}
	t.Setenv("DB_PASSWORD", "")
	if got := envSecret("DB_PASSWORD", "postgres"); got != "postgres" {
		t.Errorf("an empty DB_PASSWORD read as %q, want the default", got)
	}
	t.Setenv("DB_PASSWORD", "   ")
	if got := envSecret("DB_PASSWORD", "postgres"); got != "   " {
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
