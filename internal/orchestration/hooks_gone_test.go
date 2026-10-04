package orchestration

import (
	"bytes"
	"errors"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// A card or conversation a run names that is no longer there is nothing for
// the hooks to do (#379 bug 149). A project's delete takes its board and
// sessions with it, and a card can be deleted on its own, while the runs
// that named them stay; the board hook logged an ERROR "failed to move
// card" every time such a run changed status, which a project's delete
// makes it do when it announces the runs it cancelled.

// hgCaptureLog sends slog's default logger to a buffer until the test ends.
// slog.SetDefault also points the log package at it, and setting the old
// default back does not undo that, so both are restored.
func hgCaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return buf
}

// A run whose card is gone changes status with nothing logged as an error
// and no activity written for the card, whatever the status.
func TestStatusChangeOfARunWhoseCardIsGoneLogsNoError(t *testing.T) {
	for _, status := range []string{agentruns.StatusQueued, agentruns.StatusRunning, agentruns.StatusAwaitingApproval,
		agentruns.StatusSucceeded, agentruns.StatusFailed, agentruns.StatusCancelled} {
		t.Run(status, func(t *testing.T) {
			f := newFixture()
			f.workItems.moveErr = workitems.ErrNotFound
			logged := hgCaptureLog(t)

			f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: status, CancelRequested: true, WorkItemID: strptr("wi-gone")})

			if strings.Contains(logged.String(), "level=ERROR") {
				t.Errorf("a status change of a run whose card is gone logged an error:\n%s", logged)
			}
			if len(f.workItems.activities) != 0 {
				t.Errorf("activity written for a card that is gone: %+v", f.workItems.activities)
			}
		})
	}
}

// A card the hook could not move for any other reason is still an error.
func TestStatusChangeStillLogsACardItCouldNotMove(t *testing.T) {
	f := newFixture()
	f.workItems.moveErr = errors.New("connection refused")
	logged := hgCaptureLog(t)

	f.hooks.RunStatusChanged(&agentruns.Run{ID: "r1", Status: agentruns.StatusCancelled, WorkItemID: strptr("wi-9")})

	if !strings.Contains(logged.String(), "level=ERROR") || !strings.Contains(logged.String(), "failed to move card") {
		t.Errorf("a card that could not be moved logged %q, want the ERROR", logged)
	}
}

// A reply for an interview or guided session that is gone is dropped with
// nothing logged as an error; one that fails otherwise is still an error.
func TestReplyToAConversationThatIsGoneLogsNoError(t *testing.T) {
	for _, c := range []struct {
		name    string
		run     *agentruns.Run
		fail    func(f *fixture, err error)
		goneErr error
	}{
		{"interview", &agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, InterviewSessionID: strptr("iv-gone"), FinalText: "hi"},
			func(f *fixture, err error) { f.interviews.appendErr = err }, interviews.ErrSessionNotFound},
		{"guided session", &agentruns.Run{ID: "r1", Status: agentruns.StatusSucceeded, GuidedSessionID: strptr("gs-gone"), FinalText: "hi"},
			func(f *fixture, err error) { f.guided.appendErr = err }, guided.ErrSessionNotFound},
	} {
		t.Run(c.name+" gone", func(t *testing.T) {
			f := newFixture()
			c.fail(f, c.goneErr)
			logged := hgCaptureLog(t)
			run := *c.run
			f.hooks.RunStatusChanged(&run)
			if strings.Contains(logged.String(), "level=ERROR") {
				t.Errorf("a reply to the %s that is gone logged an error:\n%s", c.name, logged)
			}
		})
		t.Run(c.name+" failing", func(t *testing.T) {
			f := newFixture()
			c.fail(f, errors.New("connection refused"))
			logged := hgCaptureLog(t)
			run := *c.run
			f.hooks.RunStatusChanged(&run)
			if !strings.Contains(logged.String(), "level=ERROR") {
				t.Errorf("a reply to the %s that failed logged %q, want an ERROR", c.name, logged)
			}
		})
	}
}
