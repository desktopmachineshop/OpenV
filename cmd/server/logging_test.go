package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/config"
)

// initLoggingInto runs initLogging with OPENV_LOG_LEVEL set to raw and its
// stderr a file, and returns what it wrote there and the logger it
// installed. slog.SetDefault also points the log package at the new
// handler, and setting the old one back does not undo that, so the test
// restores both, and stderr.
func initLoggingInto(t *testing.T, raw string) (string, *slog.Logger) {
	t.Helper()
	t.Setenv("OPENV_LOG_LEVEL", raw)
	out, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	prevStderr := os.Stderr
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	os.Stderr = out
	initLogging(config.Load(os.LookupEnv))
	os.Stderr = prevStderr
	installed := slog.Default()
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data), installed
}

// An OPENV_LOG_LEVEL the server does not know keeps info and says so once,
// naming the variable and never the value (#379, bug 222): a secret pasted
// into the wrong variable must not reach the log. A level it knows says
// nothing.
func TestAnUnknownLogLevelIsNamedNotQuoted(t *testing.T) {
	const value = "verbose-sk_live_bug222"
	logged, logger := initLoggingInto(t, value)
	ctx := context.Background()
	if !logger.Enabled(ctx, slog.LevelInfo) || logger.Enabled(ctx, slog.LevelDebug) {
		t.Errorf("OPENV_LOG_LEVEL=%q did not keep the level at info", value)
	}
	if n := strings.Count(logged, "level=WARN"); n != 1 || !strings.Contains(logged, "OPENV_LOG_LEVEL") {
		t.Errorf("OPENV_LOG_LEVEL=%q: want one warning naming the variable, got:\n%s", value, logged)
	}
	if strings.Contains(logged, value) {
		t.Errorf("the warning printed the value of OPENV_LOG_LEVEL:\n%s", logged)
	}

	logged, logger = initLoggingInto(t, " DEBUG ")
	if !logger.Enabled(ctx, slog.LevelDebug) {
		t.Error(`OPENV_LOG_LEVEL=" DEBUG " did not set the level to debug`)
	}
	if logged != "" {
		t.Errorf(`OPENV_LOG_LEVEL=" DEBUG " logged:\n%s`, logged)
	}
}
