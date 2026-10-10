package main

import (
	"log/slog"
	"os"

	"github.com/openv/requirements-platform/internal/config"
)

// initLogging installs the process-wide slog default: a text handler on
// stderr with the level cfg reads from OPENV_LOG_LEVEL (debug|info|warn|error,
// default info).
func initLogging(cfg *config.Config) {
	level, unrecognized := cfg.LogLevel()
	if unrecognized {
		// Unknown value: keep info, but say so once, naming the variable,
		// never the value: a secret pasted into the wrong variable must not
		// reach the log (#379, bug 222).
		defer slog.Warn("unrecognized OPENV_LOG_LEVEL, using info", "want", "debug, info, warn or error (any case)")
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// fatal logs a boot-blocking error and exits.
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
