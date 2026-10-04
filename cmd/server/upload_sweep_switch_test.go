//go:build unix

package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// captureSweepLog sends slog's default logger to a buffer for the test.
func captureSweepLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// OPENV_UPLOAD_SWEEP=off skips the sweep (#379 question 55): no file goes,
// boot_tasks records nothing, and the log says the sweep is off. The first
// boot without the setting then sweeps, once.
func TestTheUploadSweepOffRemovesAndRecordsNothing(t *testing.T) {
	f := newSweepFixture(t)
	log := captureSweepLog(t)
	f.figure(f.file(storedName("named.png"), 2*time.Hour))
	orphans := []string{f.file(storedName("orphan.png"), 2*time.Hour), f.file(evidenceName(), 2*time.Hour)}

	sweepUnreferencedUploads(f.db, f.dir, f.began, false)
	f.wantKept("a file no row names, with the sweep off,", orphans...)
	if outcome, ok := f.recorded(); ok {
		t.Errorf("the sweep was recorded while off: %q", outcome)
	}
	if !strings.Contains(log.String(), `msg="upload sweep: off (OPENV_UPLOAD_SWEEP=off); nothing swept or recorded"`) {
		t.Errorf("the log does not say the sweep is off:\n%s", log)
	}

	f.sweep()
	f.wantGone("a file no row names, at the first boot with the sweep on,", orphans...)
	if _, ok := f.recorded(); !ok {
		t.Error("the sweep was not recorded once it ran")
	}
	later := f.file(storedName("after the sweep.png"), 2*time.Hour)
	f.sweep()
	f.wantKept("a file no row names, left after the sweep ran,", later)
}
