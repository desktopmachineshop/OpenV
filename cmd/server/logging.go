package main

import (
	"log/slog"
	"os"
	"strings"
)

// initLogging installs the process-wide slog default: a text handler on
// stderr with the level taken from OPENV_LOG_LEVEL (debug|info|warn|error,
// default info).
func initLogging() {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENV_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	case "", "info":
		// default
	default:
		// Unknown value: keep info, but say so once.
		defer slog.Warn("unrecognized OPENV_LOG_LEVEL, using info", "value", os.Getenv("OPENV_LOG_LEVEL"))
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// fatal logs a boot-blocking error and exits.
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
