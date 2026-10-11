package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/interviews"
)

// X13's literal equality for the interview repository: every statement it
// builds from interviewColumns, inviteColumns, interviewSessionColumns,
// messageColumns and prefixedInterviewSessionColumns is the literal it
// replaced, byte for byte (spacing, line breaks and tabs included). The
// statements are read off the wire by the recorder in pgkit_test.go, so the
// test needs no server and checks what each method sends, not a copy of it.
// The repository has no transaction: each method sends its one statement and
// nothing else. The statements X13 leaves as they were (UpdateInterview,
// RevokeInvite, UpdateSession and SetParticipantName) are not rebuilt and are
// not in this file.

// The statements X13 assembles, as the base (16d5b9b) wrote them, byte for
// byte.
const (
	// oldInterviewSaveInterviewInsert, SaveInterview
	oldInterviewSaveInterviewInsert = `
		INSERT INTO interviews (id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	// oldInterviewFindInterviewByIDSelect, FindInterviewByID
	oldInterviewFindInterviewByIDSelect = `
		SELECT id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at
		FROM interviews
		WHERE id = $1
	`
	// oldInterviewListInterviewsByProjectSelect, ListInterviewsByProject
	oldInterviewListInterviewsByProjectSelect = `
		SELECT id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at
		FROM interviews
		WHERE project_id = $1
		ORDER BY created_at DESC
	`
	// oldInterviewSaveInviteInsert, SaveInvite
	oldInterviewSaveInviteInsert = `
		INSERT INTO interview_invites (id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	// oldInterviewFindInviteByIDSelect, FindInviteByID
	oldInterviewFindInviteByIDSelect = `
		SELECT id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites
		WHERE id = $1
	`
	// oldInterviewFindInviteByTokenHashSelect, FindInviteByTokenHash
	oldInterviewFindInviteByTokenHashSelect = `
		SELECT id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites
		WHERE token_hash = $1
	`
	// oldInterviewListInvitesByInterviewSelect, ListInvitesByInterview
	oldInterviewListInvitesByInterviewSelect = `
		SELECT id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites
		WHERE interview_id = $1
		ORDER BY created_at DESC
	`
	// oldInterviewSaveSessionInsert, SaveSession
	oldInterviewSaveSessionInsert = `
		INSERT INTO interview_sessions (id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	// oldInterviewFindSessionByIDSelect, FindSessionByID
	oldInterviewFindSessionByIDSelect = `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE id = $1
	`
	// oldInterviewFindActiveSessionByInviteSelect, FindActiveSessionByInvite
	oldInterviewFindActiveSessionByInviteSelect = `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE invite_id = $1 AND status = 'active'
		ORDER BY started_at DESC
		LIMIT 1
	`
	// oldInterviewFindLatestSessionByInviteSelect, FindLatestSessionByInvite
	oldInterviewFindLatestSessionByInviteSelect = `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE invite_id = $1
		ORDER BY started_at DESC
		LIMIT 1
	`
	// oldInterviewListSessionsByInterviewSelect, ListSessionsByInterview
	oldInterviewListSessionsByInterviewSelect = `
		SELECT id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions
		WHERE interview_id = $1
		ORDER BY started_at DESC
	`
	// oldInterviewListSessionsByProjectSelect, ListSessionsByProject
	oldInterviewListSessionsByProjectSelect = `
		SELECT s.id, s.interview_id, s.invite_id, s.participant_name, s.status, s.summary, s.started_at, s.ended_at
		FROM interview_sessions s
		JOIN interviews i ON i.id = s.interview_id
		WHERE i.project_id = $1
		ORDER BY s.started_at DESC
		LIMIT $2
	`
	// oldInterviewSaveMessageInsert, SaveMessage
	oldInterviewSaveMessageInsert = `
		INSERT INTO interview_messages (id, session_id, role, content, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	// oldInterviewListMessagesBySessionSelect, ListMessagesBySession
	oldInterviewListMessagesBySessionSelect = `
		SELECT id, session_id, role, content, created_at
		FROM interview_messages
		WHERE session_id = $1
		ORDER BY created_at ASC
	`
)

// The lists the base wrote out, on one line each.
const (
	oldInterviewColumns        = `id, project_id, guided_session_id, persona_artifact_id, name, brief, agent_id, status, expires_at, created_by, created_at, updated_at`
	oldInviteColumns           = `id, interview_id, token_hash, invitee_label, expires_at, revoked, created_at`
	oldInterviewSessionColumns = `id, interview_id, invite_id, participant_name, status, summary, started_at, ended_at`
	oldMessageColumns          = `id, session_id, role, content, created_at`
	// oldPrefixedInterviewSessionColumns, ListSessionsByProject's list,
	// qualified by s
	oldPrefixedInterviewSessionColumns = `s.id, s.interview_id, s.invite_id, s.participant_name, s.status, s.summary, s.started_at, s.ended_at`
)

// The columns are the old lists, and the qualified list is the old one with
// s. before each column.
func TestInterviewColumnsAreTheOldLists(t *testing.T) {
	for _, c := range []struct{ name, got, want string }{
		{"interviewColumns", interviewColumns, oldInterviewColumns},
		{"inviteColumns", inviteColumns, oldInviteColumns},
		{"interviewSessionColumns", interviewSessionColumns, oldInterviewSessionColumns},
		{"messageColumns", messageColumns, oldMessageColumns},
		{`prefixedInterviewSessionColumns("s")`, prefixedInterviewSessionColumns("s"), oldPrefixedInterviewSessionColumns},
	} {
		if c.got != c.want {
			t.Errorf("%s =\n  %q\nwant\n  %q", c.name, c.got, c.want)
		}
	}
	if want := "s." + strings.ReplaceAll(oldInterviewSessionColumns, ", ", ", s."); oldPrefixedInterviewSessionColumns != want {
		t.Errorf("the old qualified list is not the bare one with s. before each column: %q", want)
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: the interview and session by id their sentinels, the invite reads
// and the two session reads of an invite nil, nil, and each list nil.
func TestInterviewReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewInterviewRepository(db)
	id := uuid.New().String()

	if got, err := repo.FindInterviewByID(id); got != nil || err != interviews.ErrInterviewNotFound {
		t.Errorf("FindInterviewByID with no row: %v, %v; want nil and ErrInterviewNotFound", got, err)
	}
	wantEvents(t, "FindInterviewByID", rec.take(), "QUERY "+oldInterviewFindInterviewByIDSelect)
	if got, err := repo.FindSessionByID(id); got != nil || err != interviews.ErrSessionNotFound {
		t.Errorf("FindSessionByID with no row: %v, %v; want nil and ErrSessionNotFound", got, err)
	}
	wantEvents(t, "FindSessionByID", rec.take(), "QUERY "+oldInterviewFindSessionByIDSelect)

	for _, r := range []struct {
		name  string
		read  func() (interface{}, error)
		query string
	}{
		{"FindInviteByID", func() (interface{}, error) { return repo.FindInviteByID(id) }, oldInterviewFindInviteByIDSelect},
		{"FindInviteByTokenHash", func() (interface{}, error) { return repo.FindInviteByTokenHash(id) }, oldInterviewFindInviteByTokenHashSelect},
		{"FindActiveSessionByInvite", func() (interface{}, error) { return repo.FindActiveSessionByInvite(id) }, oldInterviewFindActiveSessionByInviteSelect},
		{"FindLatestSessionByInvite", func() (interface{}, error) { return repo.FindLatestSessionByInvite(id) }, oldInterviewFindLatestSessionByInviteSelect},
		{"ListInterviewsByProject", func() (interface{}, error) { return repo.ListInterviewsByProject(id) }, oldInterviewListInterviewsByProjectSelect},
		{"ListInvitesByInterview", func() (interface{}, error) { return repo.ListInvitesByInterview(id) }, oldInterviewListInvitesByInterviewSelect},
		{"ListSessionsByInterview", func() (interface{}, error) { return repo.ListSessionsByInterview(id) }, oldInterviewListSessionsByInterviewSelect},
		{"ListSessionsByProject", func() (interface{}, error) { return repo.ListSessionsByProject(id, 10) }, oldInterviewListSessionsByProjectSelect},
		{"ListMessagesBySession", func() (interface{}, error) { return repo.ListMessagesBySession(id) }, oldInterviewListMessagesBySessionSelect},
	} {
		got, err := r.read()
		if err != nil || !strings.Contains(fmt.Sprintf("%#v", got), "(nil)") {
			t.Errorf("%s with no rows: %#v, %v; want nil and no error", r.name, got, err)
		}
		wantEvents(t, r.name, rec.take(), "QUERY "+r.query)
	}
}

// Each insert sends its old literal, as one statement with no transaction
// around it.
func TestInterviewSavesSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewInterviewRepository(db)
	id := uuid.New().String()

	for _, w := range []struct {
		name  string
		save  func() error
		query string
	}{
		{"SaveInterview", func() error {
			return repo.SaveInterview(&interviews.Interview{ID: id, ProjectID: id, Name: "Machinists", Status: interviews.InterviewStatusOpen})
		}, oldInterviewSaveInterviewInsert},
		{"SaveInvite", func() error {
			return repo.SaveInvite(&interviews.Invite{ID: id, InterviewID: id, TokenHash: "hash"})
		}, oldInterviewSaveInviteInsert},
		{"SaveSession", func() error {
			return repo.SaveSession(&interviews.Session{ID: id, InterviewID: id, InviteID: id, Status: interviews.SessionStatusActive})
		}, oldInterviewSaveSessionInsert},
		{"SaveMessage", func() error {
			return repo.SaveMessage(&interviews.Message{ID: id, SessionID: id, Role: interviews.RoleParticipant, Content: "Hello"})
		}, oldInterviewSaveMessageInsert},
	} {
		if err := w.save(); err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
		wantEvents(t, w.name, rec.take(), "EXEC "+w.query)
	}
}
