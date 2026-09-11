package guided

import (
	"errors"
	"testing"
)

// fakeRepo is a one-session guided repository: enough to drive Commit and
// Abandon, which touch nothing else when the session has no drafts.
type fakeRepo struct {
	session *Session
	// nudge is the parked wizard nudge, and nudgeWrites records every
	// SetPendingNudge call (nil included, which is the clear).
	nudge       *PendingNudge
	nudgeWrites int
	nudgeErr    error
}

func (f *fakeRepo) Save(s *Session) error { f.session = s; return nil }

func (f *fakeRepo) Update(s *Session) error { f.session = s; return nil }

func (f *fakeRepo) FindByID(id string) (*Session, error) {
	if f.session == nil || f.session.ID != id {
		return nil, ErrSessionNotFound
	}
	return f.session, nil
}

func (f *fakeRepo) ListByProject(projectID string) ([]*Session, error) { return nil, nil }

func (f *fakeRepo) SaveChatMessage(m *ChatMessage) error { return nil }

func (f *fakeRepo) ListChatMessages(sessionID string) ([]*ChatMessage, error) { return nil, nil }

func (f *fakeRepo) SetPendingNudge(sessionID string, nudge *PendingNudge) error {
	f.nudgeWrites++
	if f.nudgeErr != nil {
		return f.nudgeErr
	}
	f.nudge = nudge
	return nil
}

func (f *fakeRepo) TakePendingNudge(sessionID string) (*PendingNudge, error) {
	nudge := f.nudge
	f.nudge = nil
	return nudge, nil
}

func newNudgeFixture() (*DefaultService, *fakeRepo) {
	repo := &fakeRepo{
		session: &Session{
			ID:               "gs-1",
			ProjectID:        "proj-1",
			Status:           StatusInProgress,
			Answers:          map[string]interface{}{},
			DraftArtifactIDs: []string{},
		},
		nudge: &PendingNudge{Step: 4, Event: "saved step 4"},
	}
	return NewDefaultService(repo, nil, nil, nil, nil, nil), repo
}

// Closing a session must take its parked nudge with it: a committed or
// abandoned wizard has nobody watching its chat, and the nudge is the one
// thing that could still launch a copilot turn into it.
func TestCommitClearsTheParkedNudge(t *testing.T) {
	svc, repo := newNudgeFixture()

	session, err := svc.Commit("gs-1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if session.Status != StatusCommitted {
		t.Fatalf("status = %s, want committed", session.Status)
	}
	if repo.nudge != nil {
		t.Fatalf("a nudge is still parked on a committed session: %+v", repo.nudge)
	}
	if session.PendingNudge != nil {
		t.Fatalf("the returned session still carries %+v", session.PendingNudge)
	}
	if repo.nudgeWrites != 1 {
		t.Fatalf("SetPendingNudge calls = %d, want the one clear", repo.nudgeWrites)
	}
}

func TestAbandonClearsTheParkedNudge(t *testing.T) {
	svc, repo := newNudgeFixture()

	session, err := svc.Abandon("gs-1")
	if err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	if session.Status != StatusAbandoned {
		t.Fatalf("status = %s, want abandoned", session.Status)
	}
	if repo.nudge != nil {
		t.Fatalf("a nudge is still parked on an abandoned session: %+v", repo.nudge)
	}
}

// The session is already closed by the time the nudge is cleared, so a
// failure to clear it must not fail the close — the hooks refuse a closed
// session's nudge in any case.
func TestClosingSurvivesAFailedNudgeClear(t *testing.T) {
	svc, repo := newNudgeFixture()
	repo.nudgeErr = errors.New("connection reset")

	if _, err := svc.Abandon("gs-1"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	if repo.session.Status != StatusAbandoned {
		t.Fatalf("status = %s, want abandoned despite the failed clear", repo.session.Status)
	}
}
