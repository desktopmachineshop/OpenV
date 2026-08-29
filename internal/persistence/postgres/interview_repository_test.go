package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/interviews"
)

// seedInterviewSession inserts an interview and one active, still-anonymous
// session for it, returning the repository and the session id.
func seedInterviewSession(t *testing.T) (*InterviewRepository, string) {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)

	interviewID := uuid.New().String()
	if _, err := db.Exec(
		`INSERT INTO interviews (id, project_id, name) VALUES ($1, $2, 'Test Interview')`,
		interviewID, uuid.New().String(),
	); err != nil {
		t.Fatalf("seed interview: %v", err)
	}

	repo := NewInterviewRepository(db)
	session := &interviews.Session{
		ID:              uuid.New().String(),
		InterviewID:     interviewID,
		InviteID:        uuid.New().String(),
		ParticipantName: "",
		Status:          interviews.SessionStatusActive,
		StartedAt:       time.Now(),
	}
	if err := repo.SaveSession(session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return repo, session.ID
}

// TestSetParticipantNamePreservesCompletion is the #212 regression: the
// participant-name backfill must be a targeted single-column write, so a
// CompleteSession that lands first (setting status/summary/ended_at) is not
// reverted when the backfill then fills the still-blank name.
func TestSetParticipantNamePreservesCompletion(t *testing.T) {
	repo, sessionID := seedInterviewSession(t)

	// A concurrent CompleteSession wins the race and finalizes the session.
	ended := time.Now().Truncate(time.Millisecond)
	completed := &interviews.Session{
		ID:              sessionID,
		Status:          interviews.SessionStatusCompleted,
		Summary:         "the reviewer's summary",
		ParticipantName: "", // completion does not know the name
		EndedAt:         &ended,
	}
	if err := repo.UpdateSession(completed); err != nil {
		t.Fatalf("UpdateSession (simulated CompleteSession): %v", err)
	}

	// The name backfill runs afterwards from its own (stale) view of the
	// session. The targeted write must fill the name without touching the
	// completion fields.
	if err := repo.SetParticipantName(sessionID, "Ada"); err != nil {
		t.Fatalf("SetParticipantName: %v", err)
	}

	got, err := repo.FindSessionByID(sessionID)
	if err != nil {
		t.Fatalf("FindSessionByID: %v", err)
	}
	if got.ParticipantName != "Ada" {
		t.Errorf("participant_name = %q, want %q", got.ParticipantName, "Ada")
	}
	if got.Status != interviews.SessionStatusCompleted {
		t.Errorf("status = %q, want %q (backfill must not revert completion)", got.Status, interviews.SessionStatusCompleted)
	}
	if got.Summary != "the reviewer's summary" {
		t.Errorf("summary = %q, want it preserved", got.Summary)
	}
	if got.EndedAt == nil {
		t.Error("ended_at was reverted to NULL by the backfill")
	}
}

// TestSetParticipantNameNeverOverwrites verifies the WHERE guard: a session
// that already carries a name is left untouched by a later backfill.
func TestSetParticipantNameNeverOverwrites(t *testing.T) {
	repo, sessionID := seedInterviewSession(t)

	if err := repo.SetParticipantName(sessionID, "Grace"); err != nil {
		t.Fatalf("SetParticipantName (first): %v", err)
	}
	if err := repo.SetParticipantName(sessionID, "Ada"); err != nil {
		t.Fatalf("SetParticipantName (second): %v", err)
	}

	got, err := repo.FindSessionByID(sessionID)
	if err != nil {
		t.Fatalf("FindSessionByID: %v", err)
	}
	if got.ParticipantName != "Grace" {
		t.Errorf("participant_name = %q, want it left as %q", got.ParticipantName, "Grace")
	}
}
