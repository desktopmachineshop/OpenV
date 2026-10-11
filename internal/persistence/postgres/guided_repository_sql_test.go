package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/guided"
)

// X13's literal equality for the guided session repository: every statement it
// builds from guidedSessionColumns and guidedChatMessageColumns is the literal
// it replaced, byte for byte (spacing, line breaks and tabs included). The
// statements are read off the wire by the recorder in pgkit_test.go, so the
// test needs no server and checks what each method sends, not a copy of it.
// The repository has no transaction: each method sends its one statement and
// nothing else. The statements X13 leaves as they were (Save, Update, the
// nudge's set and take) are not rebuilt and are not in this file.

// The statements X13 assembles, as the base (3ca5675) wrote them, byte for
// byte.
const (
	// oldGuidedFindByIDSelect, FindByID
	oldGuidedFindByIDSelect = `
		SELECT id, project_id, status, current_step, answers, draft_artifact_ids, agent_run_id, pending_nudge, created_by, created_at, updated_at
		FROM guided_sessions
		WHERE id = $1
	`
	// oldGuidedListByProjectSelect, ListByProject
	oldGuidedListByProjectSelect = `
		SELECT id, project_id, status, current_step, answers, draft_artifact_ids, agent_run_id, pending_nudge, created_by, created_at, updated_at
		FROM guided_sessions
		WHERE project_id = $1
		ORDER BY created_at DESC
	`
	// oldGuidedListChatMessagesSelect, ListChatMessages
	oldGuidedListChatMessagesSelect = `
		SELECT id, session_id, role, content, created_at
		FROM guided_session_messages
		WHERE session_id = $1
		ORDER BY created_at
	`
)

// The lists the base wrote out, on one line each.
const (
	oldGuidedSessionColumns     = `id, project_id, status, current_step, answers, draft_artifact_ids, agent_run_id, pending_nudge, created_by, created_at, updated_at`
	oldGuidedChatMessageColumns = `id, session_id, role, content, created_at`
)

// The columns are the old lists.
func TestGuidedColumnsAreTheOldLists(t *testing.T) {
	for _, c := range []struct{ name, got, want string }{
		{"guidedSessionColumns", guidedSessionColumns, oldGuidedSessionColumns},
		{"guidedChatMessageColumns", guidedChatMessageColumns, oldGuidedChatMessageColumns},
	} {
		if c.got != c.want {
			t.Errorf("%s =\n  %q\nwant\n  %q", c.name, c.got, c.want)
		}
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: FindByID no session and guided.ErrSessionNotFound, ListByProject
// nil, and ListChatMessages an empty, non-nil list.
func TestGuidedReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewGuidedRepository(db)
	id := uuid.New().String()

	if got, err := repo.FindByID(id); got != nil || err != guided.ErrSessionNotFound {
		t.Errorf("FindByID with no row: %v, %v; want nil and guided.ErrSessionNotFound", got, err)
	}
	wantEvents(t, "FindByID", rec.take(), "QUERY "+oldGuidedFindByIDSelect)

	got, err := repo.ListByProject(id)
	if err != nil || !strings.Contains(fmt.Sprintf("%#v", got), "(nil)") {
		t.Errorf("ListByProject with no rows: %#v, %v; want nil and no error", got, err)
	}
	wantEvents(t, "ListByProject", rec.take(), "QUERY "+oldGuidedListByProjectSelect)

	messages, err := repo.ListChatMessages(id)
	if err != nil || messages == nil || len(messages) != 0 {
		t.Errorf("ListChatMessages with no rows: %#v, %v; want an empty, non-nil list and no error", messages, err)
	}
	wantEvents(t, "ListChatMessages", rec.take(), "QUERY "+oldGuidedListChatMessagesSelect)
}
