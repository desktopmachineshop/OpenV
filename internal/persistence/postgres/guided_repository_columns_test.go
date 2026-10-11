package postgres

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/guided"
)

// The guided session repository's columns and scans (refactor plan X13's
// characterization for guided_repository.go, in the manner of X13a's), pinned
// as found: every column of a session read back through each read that scans
// it, the columns that may be NULL, the scan errors that name a column's
// place, the JSON columns that cannot be decoded, what a read answers for a
// row no one has, and what each write puts in the row, for the session and for
// the copilot's chat message. The existing tests (partial_text_test.go,
// agent_suite_utc_stamps_test.go and malformed_parent_list_test.go) pin the
// parked nudge's set and take, the stamps a service writes and the
// malformed-id lists; none reads back the other columns of a session through
// FindByID or ListByProject, pins a column's place in a scan, or reads a chat
// message back whole. X13 gives the session a columns const and the two reads
// that scan it a shared list in place of their copies; these tests are what
// holds the repository still while it does. The repository has no
// transaction (every method sends one statement), so there is none to pin.
// The helpers are in repository_roundtrip_helpers_test.go.

// rtGSWorld is a fresh database with one project, which has a workspace, and
// a repository over it.
type rtGSWorld struct {
	t       *testing.T
	db      *sql.DB
	repo    *GuidedRepository
	org     string
	project string
}

func newRTGSWorld(t *testing.T) *rtGSWorld {
	t.Helper()
	db := rtDB(t)
	w := &rtGSWorld{t: t, db: db, repo: NewGuidedRepository(db), org: uuid.New().String()}
	rtSeedOrg(t, db, w.org)
	w.project = w.newProject()
	return w
}

// newProject seeds another project of the workspace and answers its id.
func (w *rtGSWorld) newProject() string {
	w.t.Helper()
	id := uuid.New().String()
	rtSeed(w.t, w.db, `INSERT INTO projects (id, org_id, name) VALUES ($1, $2, 'Guided')`, id, w.org)
	return id
}

// save saves the session, failing the test on an error.
func (w *rtGSWorld) save(s *guided.Session) *guided.Session {
	w.t.Helper()
	if err := w.repo.Save(s); err != nil {
		w.t.Fatalf("Save %s: %v", s.ID, err)
	}
	return s
}

// bare saves a session of the project (project "" is the world's) with
// nothing optional, created and updated at the minute.
func (w *rtGSWorld) bare(project string, step, minute int) *guided.Session {
	w.t.Helper()
	if project == "" {
		project = w.project
	}
	return w.save(&guided.Session{ID: uuid.New().String(), ProjectID: project, Status: guided.StatusInProgress,
		CurrentStep: step, CreatedAt: rtAt(minute), UpdatedAt: rtAt(minute)})
}

// message saves a chat message of the session, created at the minute.
func (w *rtGSWorld) message(session, role, content string, minute int) *guided.ChatMessage {
	w.t.Helper()
	m := &guided.ChatMessage{ID: uuid.New().String(), SessionID: session, Role: role, Content: content, CreatedAt: rtAt(minute)}
	if err := w.repo.SaveChatMessage(m); err != nil {
		w.t.Fatalf("SaveChatMessage %q: %v", content, err)
	}
	return m
}

// rtGSSansTimes is the session with its two stamps zeroed.
func rtGSSansTimes(s *guided.Session) guided.Session {
	c := *s
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	return c
}

// rtWantGuidedSession fails unless got is sent as a session reads back: every
// field but the times equal (no answers reading back as an empty map, no
// drafts as an empty list), and the two stamps the TIMESTAMP columns' wall
// clock of the times sent.
func rtWantGuidedSession(t *testing.T, what string, got, sent *guided.Session) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	want := rtGSSansTimes(sent)
	if want.Answers == nil {
		want.Answers = map[string]interface{}{}
	}
	if want.DraftArtifactIDs == nil {
		want.DraftArtifactIDs = []string{}
	}
	rtWantSame(t, what, rtGSSansTimes(got), want)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
}

// rtWantGuidedSessions fails unless list holds exactly the sessions of want,
// in the order given, each read back whole.
func rtWantGuidedSessions(t *testing.T, what string, list []*guided.Session, err error, want ...*guided.Session) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var got, wantIDs []string
	for _, s := range list {
		got = append(got, s.ID)
	}
	for _, s := range want {
		wantIDs = append(wantIDs, s.ID)
	}
	if strings.Join(got, "|") != strings.Join(wantIDs, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantIDs)
		return
	}
	for i := range list {
		rtWantGuidedSession(t, fmt.Sprintf("%s session %d", what, i), list[i], want[i])
	}
}

// rtWantGuidedMessage fails unless got is sent as a message reads back: every
// field equal, created_at the wall clock sent.
func rtWantGuidedMessage(t *testing.T, what string, got, sent *guided.ChatMessage) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	g, s := *got, *sent
	g.CreatedAt, s.CreatedAt = time.Time{}, time.Time{}
	rtWantSame(t, what, g, s)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
}

// rtWantGuidedMessages fails unless list holds exactly the messages of want,
// in the order given, each read back whole.
func rtWantGuidedMessages(t *testing.T, what string, list []*guided.ChatMessage, err error, want ...*guided.ChatMessage) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	if list == nil {
		t.Errorf("%s answered a nil list, want a non-nil one of %d", what, len(want))
	}
	var got, wantIDs []string
	for _, m := range list {
		got = append(got, m.ID)
	}
	for _, m := range want {
		wantIDs = append(wantIDs, m.ID)
	}
	if strings.Join(got, "|") != strings.Join(wantIDs, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantIDs)
		return
	}
	for i := range list {
		rtWantGuidedMessage(t, fmt.Sprintf("%s message %d", what, i), list[i], want[i])
	}
}

// Every column Save writes reads back through FindByID and ListByProject,
// whole: the project, the status, the step, the answers, the drafts (in the
// order saved), the optional run, the creator and the two stamps (the wall
// clock sent, its offset dropped, to the microsecond); the parked nudge, which
// Save does not write, is set on its own and reads back through both. Each
// column holds a value no other does, so a swap of two in a scan shows. A
// second session leaves every optional column NULL (nil pointers and no
// nudge) and holds no answers and no drafts, which read back as an empty map
// and an empty list, not nil. The project's list answers newest created_at
// first and only the project's.
func TestGuidedRepositoryListsReadBackEveryColumn(t *testing.T) {
	w := newRTGSWorld(t)
	run, creator := uuid.New().String(), uuid.New().String()
	full := w.save(&guided.Session{ID: uuid.New().String(), ProjectID: w.project, Status: guided.StatusCommitted, CurrentStep: 6,
		Answers: map[string]interface{}{"step_1": "Mill aluminium", "step_2": []interface{}{"vice", float64(2)},
			"step_3": map[string]interface{}{"done": true, "note": nil}},
		DraftArtifactIDs: []string{"b-draft", "a-draft", "c-draft"}, AgentRunID: &run, CreatedBy: &creator,
		CreatedAt: rtAt(10), UpdatedAt: rtAt(13).In(rtCEST)})
	nudge := &guided.PendingNudge{Step: 4, State: map[string]interface{}{"step_4": "requirements", "count": float64(3)}, Event: "saved step 4"}
	if err := w.repo.SetPendingNudge(full.ID, nudge); err != nil {
		t.Fatal(err)
	}
	full.PendingNudge = nudge
	bare := w.bare("", 0, 20)

	// Another project's, created sooner than the full session.
	foreignProject, foreignRun, foreignCreator := w.newProject(), uuid.New().String(), uuid.New().String()
	foreign := w.save(&guided.Session{ID: uuid.New().String(), ProjectID: foreignProject, Status: guided.StatusAbandoned, CurrentStep: 2,
		Answers: map[string]interface{}{"elsewhere": "yes"}, DraftArtifactIDs: []string{"z-draft"}, AgentRunID: &foreignRun,
		CreatedBy: &foreignCreator, CreatedAt: rtAt(5), UpdatedAt: rtAt(6)})

	for name, s := range map[string]*guided.Session{"full": full, "bare": bare, "foreign": foreign} {
		got, err := w.repo.FindByID(s.ID)
		if err != nil {
			t.Fatalf("FindByID %s: %v", name, err)
		}
		rtWantGuidedSession(t, "FindByID "+name, got, s)
	}

	list, err := w.repo.ListByProject(w.project)
	rtWantGuidedSessions(t, "ListByProject", list, err, bare, full)
	list, err = w.repo.ListByProject(foreignProject)
	rtWantGuidedSessions(t, "ListByProject of the other project", list, err, foreign)
}

// Every column of a chat message reads back through ListChatMessages: the
// session, the role, the content (empty included) and created_at (the wall
// clock sent), each holding a value no other does. The list answers oldest
// created_at first and only the session's.
func TestGuidedRepositoryReadsBackEveryChatMessageColumn(t *testing.T) {
	w := newRTGSWorld(t)
	session, other := w.bare("", 1, 0), w.bare("", 2, 1)

	late := &guided.ChatMessage{ID: uuid.New().String(), SessionID: session.ID, Role: guided.ChatRoleAssistant,
		Content: "The spindle needs 4 kW.", CreatedAt: rtAt(30).In(rtCEST)}
	early := &guided.ChatMessage{ID: uuid.New().String(), SessionID: session.ID, Role: guided.ChatRoleUser,
		Content: "What does the spindle need?", CreatedAt: rtAt(10)}
	middle := &guided.ChatMessage{ID: uuid.New().String(), SessionID: session.ID, Role: guided.ChatRoleSystem,
		Content: "", CreatedAt: rtAt(20)}
	foreign := &guided.ChatMessage{ID: uuid.New().String(), SessionID: other.ID, Role: guided.ChatRoleUser,
		Content: "Not this conversation", CreatedAt: rtAt(15)}
	for _, m := range []*guided.ChatMessage{late, early, middle, foreign} {
		if err := w.repo.SaveChatMessage(m); err != nil {
			t.Fatalf("SaveChatMessage: %v", err)
		}
	}

	list, err := w.repo.ListChatMessages(session.ID)
	rtWantGuidedMessages(t, "ListChatMessages", list, err, early, middle, late)
	list, err = w.repo.ListChatMessages(other.ID)
	rtWantGuidedMessages(t, "ListChatMessages of the other session", list, err, foreign)
}

// rtGSRead is one read of the repository that answers rows: the ids of what it
// answered, nil for no row and for a nil list, and the error as it came.
type rtGSRead struct {
	name string
	run  func() ([]string, error)
}

func rtGSSession(s *guided.Session, err error) ([]string, error) {
	if s == nil {
		return nil, err
	}
	return []string{s.ID}, err
}

func rtGSSessions(list []*guided.Session, err error) ([]string, error) {
	if list == nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	return ids, err
}

func rtGSMessages(list []*guided.ChatMessage, err error) ([]string, error) {
	if list == nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	return ids, err
}

// rtGSScanCase is a column the schema forbids NULL, here one a test has let
// take it, and what that does to each read of the table.
type rtGSScanCase struct {
	column  string
	index   int
	message string
	// misses names the reads that cannot match the row.
	misses []string
}

const (
	rtGSStr  = "converting NULL to string is unsupported"
	rtGSInt  = "converting NULL to int is unsupported"
	rtGSTime = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
)

// rtGSScanErrors drives one table's scan errors: for each case it inserts a
// row whose column is NULL (the others from base, a SQL literal each), runs
// every read and expects the error naming the column's place and no row and
// no list, or, where the read cannot match the row, no error and a list of
// the survivors the read names (the good rows the test seeded beside it).
func rtGSScanErrors(t *testing.T, db *sql.DB, table string, base map[string]string, cases []rtGSScanCase,
	survivors map[string][]string, reads func(id string) []rtGSRead) {
	t.Helper()
	for _, c := range cases {
		rtSeed(t, db, `ALTER TABLE `+table+` ALTER COLUMN `+c.column+` DROP NOT NULL`)
	}
	var columns []string
	for column := range base {
		columns = append(columns, column)
	}
	sort.Strings(columns)

	for _, c := range cases {
		id := uuid.New().String()
		var values []string
		for _, column := range columns {
			if column == c.column {
				values = append(values, "NULL")
			} else {
				values = append(values, base[column])
			}
		}
		rtSeed(t, db, fmt.Sprintf(`INSERT INTO %s (id, %s) VALUES ($1, %s)`, table, strings.Join(columns, ", "), strings.Join(values, ", ")), id)

	reads:
		for _, read := range reads(id) {
			got, err := read.run()
			for _, miss := range c.misses {
				if read.name == miss {
					if err != nil || strings.Join(got, "|") != strings.Join(survivors[read.name], "|") {
						t.Errorf("%s over a row with a NULL %s: %v, %v; want %v and no error", read.name, c.column, got, err, survivors[read.name])
					}
					continue reads
				}
			}
			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
			if got != nil || err == nil || err.Error() != want {
				t.Errorf("%s over a row with a NULL %s: %v, %v; want nil and %s", read.name, c.column, got, err, want)
			}
		}
		rtSeed(t, db, `DELETE FROM `+table+` WHERE id = $1`, id)
	}
}

// A column of a session the schema forbids NULL fails the reads that scan it,
// naming the column and its place among the eleven scanned (id 0, project_id
// 1, status 2, current_step 3, answers 4, draft_artifact_ids 5, agent_run_id
// 6, pending_nudge 7, created_by 8, created_at 9, updated_at 10), unwrapped,
// with no session and no list (nil, not []). One such row fails the whole
// list it is in, the good session beside it, read before it, with it: the
// list does not go around it. (The id is the primary key; it takes no NULL.
// project_id takes one without an error, and answers and draft_artifact_ids,
// scanned as bytes, take one without a scan error:
// TestGuidedRepositoryReadsNullColumns.)
func TestGuidedRepositorySessionScanErrorsNameTheColumn(t *testing.T) {
	w := newRTGSWorld(t)
	// A good session of the same project, newer than the bad rows (the list is
	// newest first, so it has been read when it meets one).
	good := w.bare("", 1, 0)
	rtGSScanErrors(t, w.db, "guided_sessions", map[string]string{
		"project_id": "'" + w.project + "'", "status": "'in-progress'", "current_step": "3",
		"created_at": "'2000-01-01 00:00:00'", "updated_at": "'2000-01-01 00:00:00'",
	}, []rtGSScanCase{
		{"status", 2, rtGSStr, nil},
		{"current_step", 3, rtGSInt, nil},
		{"created_at", 9, rtGSTime, nil},
		{"updated_at", 10, rtGSTime, nil},
	}, nil, func(id string) []rtGSRead {
		return []rtGSRead{
			{"FindByID", func() ([]string, error) { return rtGSSession(w.repo.FindByID(id)) }},
			{"ListByProject", func() ([]string, error) { return rtGSSessions(w.repo.ListByProject(w.project)) }},
		}
	})
	if got, err := w.repo.FindByID(good.ID); err != nil || got == nil {
		t.Errorf("the good session beside the bad rows: %v, %v; want it read", got, err)
	}
}

// A column of a chat message the schema forbids NULL fails the transcript
// read, naming the column and its place among the five scanned (id 0,
// session_id 1, role 2, content 3, created_at 4), unwrapped, with no list
// (nil, not []). A message with no session is in no transcript, which then
// lists the good message alone. (The id is the primary key.)
func TestGuidedRepositoryMessageScanErrorsNameTheColumn(t *testing.T) {
	w := newRTGSWorld(t)
	session := w.bare("", 1, 0)
	good := w.message(session.ID, guided.ChatRoleUser, "Good", 0)
	rtGSScanErrors(t, w.db, "guided_session_messages", map[string]string{
		"session_id": "'" + session.ID + "'", "role": "'user'", "content": "'Bad'", "created_at": "'2000-01-01 00:00:00'",
	}, []rtGSScanCase{
		{"session_id", 1, rtGSStr, []string{"ListChatMessages"}},
		{"role", 2, rtGSStr, nil},
		{"content", 3, rtGSStr, nil},
		{"created_at", 4, rtGSTime, nil},
	}, map[string][]string{"ListChatMessages": {good.ID}}, func(string) []rtGSRead {
		return []rtGSRead{{"ListChatMessages", func() ([]string, error) {
			return rtGSMessages(w.repo.ListChatMessages(session.ID))
		}}}
	})
}

// Columns that are NULL, or hold JSON null, read without a scan error.
// project_id NULL reads as an empty project id through FindByID and is in no
// project's list. answers and draft_artifact_ids that are SQL NULL (which the
// schema forbids and a column let take NULL would hold) read as an empty map
// and an empty list, not nil, where JSON null reads as nil. pending_nudge
// that is SQL NULL is no nudge, and JSON null reads as an empty one, not nil.
func TestGuidedRepositoryReadsNullColumns(t *testing.T) {
	w := newRTGSWorld(t)
	rtSeed(t, w.db, `ALTER TABLE guided_sessions ALTER COLUMN answers DROP NOT NULL`)
	rtSeed(t, w.db, `ALTER TABLE guided_sessions ALTER COLUMN draft_artifact_ids DROP NOT NULL`)

	// No project.
	orphan := uuid.New().String()
	rtSeed(t, w.db, `INSERT INTO guided_sessions (id, project_id, status, current_step, created_at, updated_at)
		VALUES ($1, NULL, 'abandoned', 9, '2026-03-14 09:26:53.123456', '2026-03-14 09:27:53.123456')`, orphan)
	got, err := w.repo.FindByID(orphan)
	if err != nil || got == nil || got.ProjectID != "" || got.Status != guided.StatusAbandoned || got.CurrentStep != 9 ||
		got.PendingNudge != nil || got.AgentRunID != nil || got.CreatedBy != nil {
		t.Errorf("FindByID of a session with no project: %s, %v; want it read with an empty project id", rtJSON(got), err)
	}
	list, err := w.repo.ListByProject(w.project)
	rtWantNil(t, "ListByProject of a project whose only session has none", list, err)

	// SQL NULL JSON columns.
	nulls := w.bare("", 1, 0)
	rtSeed(t, w.db, `UPDATE guided_sessions SET answers = NULL, draft_artifact_ids = NULL WHERE id = $1`, nulls.ID)
	got, err = w.repo.FindByID(nulls.ID)
	if err != nil || got == nil || got.Answers == nil || len(got.Answers) != 0 || got.DraftArtifactIDs == nil ||
		len(got.DraftArtifactIDs) != 0 || got.PendingNudge != nil {
		t.Errorf("FindByID of a session whose JSON columns are NULL: %s, %v; want an empty map, an empty list and no nudge", rtJSON(got), err)
	}
	list, err = w.repo.ListByProject(w.project)
	if err != nil || len(list) != 1 || list[0].Answers == nil || len(list[0].Answers) != 0 || list[0].DraftArtifactIDs == nil ||
		len(list[0].DraftArtifactIDs) != 0 {
		t.Errorf("ListByProject of a session whose JSON columns are NULL: %s, %v; want an empty map and an empty list", rtJSON(list), err)
	}

	// JSON null.
	rtSeed(t, w.db, `UPDATE guided_sessions SET answers = 'null', draft_artifact_ids = 'null', pending_nudge = 'null' WHERE id = $1`, nulls.ID)
	got, err = w.repo.FindByID(nulls.ID)
	if err != nil || got == nil || got.Answers != nil || got.DraftArtifactIDs != nil ||
		rtJSON(got.PendingNudge) != `{"step":0,"event":""}` {
		t.Errorf("FindByID of a session whose JSON columns are JSON null: %s, %v; want nil answers, nil drafts and an empty nudge", rtJSON(got), err)
	}
	list, err = w.repo.ListByProject(w.project)
	if err != nil || len(list) != 1 || list[0].Answers != nil || list[0].DraftArtifactIDs != nil ||
		rtJSON(list[0].PendingNudge) != `{"step":0,"event":""}` {
		t.Errorf("ListByProject of a session whose JSON columns are JSON null: %s, %v", rtJSON(list), err)
	}
}

// A pending nudge that cannot be decoded is no nudge, never a failed read: the
// session reads whole through FindByID and ListByProject with no nudge, for a
// value of another shape (a string, an array, a number, an object whose step
// is a string), and the take that finds one clears it and reports it.
func TestGuidedRepositoryUnreadableNudgeReadsAsNone(t *testing.T) {
	w := newRTGSWorld(t)
	s := w.bare("", 3, 0)
	for _, payload := range []string{`"saved step 3"`, `[1,2]`, `7`, `{"step":"three"}`} {
		rtSeed(t, w.db, `UPDATE guided_sessions SET pending_nudge = $2 WHERE id = $1`, s.ID, payload)
		got, err := w.repo.FindByID(s.ID)
		if err != nil || got == nil || got.PendingNudge != nil || got.CurrentStep != 3 {
			t.Errorf("FindByID with the nudge %s: %s, %v; want the session read, with no nudge", payload, rtJSON(got), err)
		}
		list, err := w.repo.ListByProject(w.project)
		if err != nil || len(list) != 1 || list[0].ID != s.ID || list[0].PendingNudge != nil {
			t.Errorf("ListByProject with the nudge %s: %s, %v; want the session listed, with no nudge", payload, rtJSON(list), err)
		}
	}
}

// Answers or drafts stored as JSON of another shape fail the read: the
// decoder's error, unwrapped, with no session and no list (nil, not []); the
// good session beside the bad row, read before it, fails the list with it.
func TestGuidedRepositoryUnreadableJSONFailsTheRead(t *testing.T) {
	w := newRTGSWorld(t)
	good := w.bare("", 1, 5)
	for _, c := range []struct{ column, stored, message string }{
		{"answers", `[1]`, "json: cannot unmarshal array into Go value of type map[string]interface {}"},
		{"answers", `"text"`, "json: cannot unmarshal string into Go value of type map[string]interface {}"},
		{"draft_artifact_ids", `{"a":1}`, "json: cannot unmarshal object into Go value of type []string"},
		{"draft_artifact_ids", `[1]`, "json: cannot unmarshal number into Go value of type string"},
	} {
		bad := w.bare("", 2, 0)
		rtSeed(t, w.db, `UPDATE guided_sessions SET `+c.column+` = $2 WHERE id = $1`, bad.ID, c.stored)
		what := fmt.Sprintf("%s stored as %s", c.column, c.stored)

		got, err := w.repo.FindByID(bad.ID)
		var typeErr *json.UnmarshalTypeError
		if got != nil || !errors.As(err, &typeErr) || err.Error() != c.message {
			t.Errorf("FindByID with %s: %v, %v; want nil and the decoder's %q", what, got, err, c.message)
		}
		list, err := w.repo.ListByProject(w.project)
		if list != nil || !errors.As(err, &typeErr) || err.Error() != c.message {
			t.Errorf("ListByProject with %s: %v, %v; want nil and the decoder's %q", what, list, err, c.message)
		}
		rtSeed(t, w.db, `DELETE FROM guided_sessions WHERE id = $1`, bad.ID)
	}
	if got, err := w.repo.FindByID(good.ID); err != nil || got == nil {
		t.Errorf("the good session beside the bad rows: %v, %v; want it read", got, err)
	}
}

// A read for a row no one has: FindByID answers no session and
// guided.ErrSessionNotFound itself for an id no row has, a malformed one and
// an empty one; ListByProject answers nil (JSON null), never [], for a project
// nothing matches, a malformed id and an empty one, and for a project with no
// session; ListChatMessages answers an empty, non-nil list (JSON []) for each
// of those and for a session with no message. TakePendingNudge answers no
// nudge and no error for an id no row has, a malformed one, an empty one and a
// session with nothing parked. SetPendingNudge of an id no row has is no
// error and writes nothing; of a malformed or empty one it is Postgres's
// refusal as it came.
func TestGuidedRepositoryAnswersNoRow(t *testing.T) {
	w := newRTGSWorld(t)
	session := w.bare("", 1, 0)
	emptyProject := w.newProject()

	for _, id := range []string{uuid.New().String(), malformed, ""} {
		if got, err := w.repo.FindByID(id); got != nil || err != guided.ErrSessionNotFound {
			t.Errorf("FindByID(%q): %v, %v; want nil and guided.ErrSessionNotFound", id, got, err)
		}
		list, err := w.repo.ListByProject(id)
		rtWantNil(t, fmt.Sprintf("ListByProject(%q)", id), list, err)
		messages, err := w.repo.ListChatMessages(id)
		rtWantEmpty(t, fmt.Sprintf("ListChatMessages(%q)", id), messages, err)
		if nudge, err := w.repo.TakePendingNudge(id); nudge != nil || err != nil {
			t.Errorf("TakePendingNudge(%q): %v, %v; want nil, nil", id, nudge, err)
		}
	}
	list, err := w.repo.ListByProject(emptyProject)
	rtWantNil(t, "ListByProject of a project with none", list, err)
	messages, err := w.repo.ListChatMessages(session.ID)
	rtWantEmpty(t, "ListChatMessages of a session with none", messages, err)
	if nudge, err := w.repo.TakePendingNudge(session.ID); nudge != nil || err != nil {
		t.Errorf("TakePendingNudge of a session with nothing parked: %v, %v; want nil, nil", nudge, err)
	}

	nudge := &guided.PendingNudge{Step: 1, Event: "saved step 1"}
	if err := w.repo.SetPendingNudge(uuid.New().String(), nudge); err != nil {
		t.Errorf("SetPendingNudge of a session no row has: %v, want no error", err)
	}
	if err := w.repo.SetPendingNudge(uuid.New().String(), nil); err != nil {
		t.Errorf("SetPendingNudge(nil) of a session no row has: %v, want no error", err)
	}
	for _, id := range []string{malformed, ""} {
		rtWantUnwrappedPQ(t, fmt.Sprintf("SetPendingNudge(%q)", id), w.repo.SetPendingNudge(id, nudge), "22P02")
		rtWantUnwrappedPQ(t, fmt.Sprintf("SetPendingNudge(%q, nil)", id), w.repo.SetPendingNudge(id, nil), "22P02")
	}
	var parked int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM guided_sessions WHERE pending_nudge IS NOT NULL`).Scan(&parked); err != nil || parked != 0 {
		t.Errorf("%d sessions hold a nudge after the refused and unmatched SetPendingNudge calls (%v), want none", parked, err)
	}
}

// rtGSRow is the raw columns of a session row, as stored.
type rtGSRow struct {
	ID, Status                          string
	Project, Org, Run, Nudge, CreatedBy sql.NullString
	Step                                int
	Answers, Drafts                     string
	CreatedAt, UpdatedAt                time.Time
}

func rtReadGuidedRow(t *testing.T, db *sql.DB, id string) *rtGSRow {
	t.Helper()
	r := new(rtGSRow)
	err := db.QueryRow(`SELECT id::text, project_id::text, org_id::text, status, current_step, answers::text,
		draft_artifact_ids::text, agent_run_id::text, pending_nudge::text, created_by::text, created_at, updated_at
		FROM guided_sessions WHERE id = $1`, id).
		Scan(&r.ID, &r.Project, &r.Org, &r.Status, &r.Step, &r.Answers, &r.Drafts, &r.Run, &r.Nudge, &r.CreatedBy,
			&r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read guided session %s: %v", id, err)
	}
	return r
}

func rtReadGuidedMessage(t *testing.T, db *sql.DB, id string) *guided.ChatMessage {
	t.Helper()
	m := new(guided.ChatMessage)
	err := db.QueryRow(`SELECT id::text, session_id::text, role, content, created_at FROM guided_session_messages WHERE id = $1`, id).
		Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read guided message %s: %v", id, err)
	}
	return m
}

// rtWantStoredJSON fails unless the jsonb column's text holds the same JSON as
// want, whatever its spacing.
func rtWantStoredJSON(t *testing.T, what, stored string, want interface{}) {
	t.Helper()
	var got interface{}
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Errorf("%s stored %q, which is not JSON: %v", what, stored, err)
		return
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var wantAny interface{}
	if err := json.Unmarshal(wantJSON, &wantAny); err != nil {
		t.Fatal(err)
	}
	rtWantSame(t, what+" stored", got, wantAny)
}

// Save writes the project, the status, the step, the answers, the drafts, the
// optional run, the creator and the two stamps as the session holds them, and
// the workspace of the project; it does not write the parked nudge, which a
// session it is given holds or not. Nil answers are stored as {} and nil
// drafts as [], a nil run and creator as NULL. Saving an id a session has is
// Postgres's refusal as it came, unwrapped; so is a project no row has (a
// foreign key) and a malformed one (the uuid's refusal); answers JSON cannot
// hold fail before a statement is sent; the refused writes leave no row.
func TestGuidedRepositorySaveWritesEveryColumn(t *testing.T) {
	w := newRTGSWorld(t)
	run, creator := uuid.New().String(), uuid.New().String()
	s := &guided.Session{ID: uuid.New().String(), ProjectID: w.project, Status: guided.StatusCommitted, CurrentStep: 5,
		Answers: map[string]interface{}{"step_1": "Mill", "step_2": []interface{}{"a", float64(2)}}, DraftArtifactIDs: []string{"b", "a"},
		AgentRunID: &run, CreatedBy: &creator, PendingNudge: &guided.PendingNudge{Step: 2, Event: "ignored"},
		CreatedAt: rtAt(10).In(rtCEST), UpdatedAt: rtAt(11).In(rtCEST)}
	w.save(s)
	row := rtReadGuidedRow(t, w.db, s.ID)
	if row == nil {
		t.Fatal("Save wrote no row")
	}
	if row.ID != s.ID || row.Project.String != w.project || row.Org.String != w.org || row.Status != guided.StatusCommitted ||
		row.Step != 5 || row.Run.String != run || row.CreatedBy.String != creator {
		t.Errorf("Save wrote %+v, want %s in the workspace %s", row, rtJSON(s), w.org)
	}
	rtWantStoredJSON(t, "Save answers", row.Answers, s.Answers)
	rtWantStoredJSON(t, "Save draft_artifact_ids", row.Drafts, s.DraftArtifactIDs)
	if row.Nudge.Valid {
		t.Errorf("Save wrote the pending nudge %s, want NULL", row.Nudge.String)
	}
	rtWantTimestamp(t, "Save created_at", row.CreatedAt, s.CreatedAt)
	rtWantTimestamp(t, "Save updated_at", row.UpdatedAt, s.UpdatedAt)

	bare := &guided.Session{ID: uuid.New().String(), ProjectID: w.project, Status: guided.StatusInProgress, CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	w.save(bare)
	if row := rtReadGuidedRow(t, w.db, bare.ID); row.Answers != "{}" || row.Drafts != "[]" || row.Run.Valid || row.CreatedBy.Valid ||
		row.Nudge.Valid || row.Step != 0 {
		t.Errorf("Save of a bare session wrote %+v, want {}, [], step 0 and NULL run, creator and nudge", row)
	}

	rtWantUnwrappedPQ(t, "Save of an id a session has", w.repo.Save(s), "23505")
	rtWantPQ(t, "Save of an id a session has", w.repo.Save(s), "23505", "guided_sessions_pkey")
	noProject := &guided.Session{ID: uuid.New().String(), ProjectID: uuid.New().String(), Status: guided.StatusInProgress,
		CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	rtWantUnwrappedPQ(t, "Save of a project no row has", w.repo.Save(noProject), "23503")
	badProject := &guided.Session{ID: uuid.New().String(), ProjectID: malformed, Status: guided.StatusInProgress,
		CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	rtWantRefused(t, "Save of a malformed project", w.repo.Save(badProject))
	unencodable := &guided.Session{ID: uuid.New().String(), ProjectID: w.project, Status: guided.StatusInProgress,
		Answers: rtUnencodable, CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	rtWantUnencodable(t, "Save", w.repo.Save(unencodable))
	for _, id := range []string{noProject.ID, badProject.ID, unencodable.ID} {
		if rtReadGuidedRow(t, w.db, id) != nil {
			t.Error("a refused Save wrote a row")
		}
	}
	if got := rtReadGuidedRow(t, w.db, s.ID); got.Status != guided.StatusCommitted || got.Step != 5 {
		t.Errorf("the refused Save changed the row: %+v", got)
	}
}

// Update rewrites the status, the step, the answers, the drafts, the run and
// updated_at, and nothing else: not the project, the workspace, the creator,
// created_at nor the parked nudge, whatever the session it is given holds. Nil
// answers become {} and nil drafts [], a nil run clears the column. It is no
// error for a session no row has, and writes nothing; a malformed id is
// Postgres's refusal as it came, and answers JSON cannot hold fail before a
// statement is sent.
func TestGuidedRepositoryUpdateWritesItsSixColumns(t *testing.T) {
	w := newRTGSWorld(t)
	run, creator := uuid.New().String(), uuid.New().String()
	s := w.save(&guided.Session{ID: uuid.New().String(), ProjectID: w.project, Status: guided.StatusInProgress, CurrentStep: 2,
		Answers: map[string]interface{}{"old": "answer"}, DraftArtifactIDs: []string{"old-draft"}, AgentRunID: &run, CreatedBy: &creator,
		CreatedAt: rtAt(10), UpdatedAt: rtAt(11)})
	if err := w.repo.SetPendingNudge(s.ID, &guided.PendingNudge{Step: 2, Event: "parked"}); err != nil {
		t.Fatal(err)
	}
	before := rtReadGuidedRow(t, w.db, s.ID)

	newRun, otherCreator := uuid.New().String(), uuid.New().String()
	update := &guided.Session{ID: s.ID, ProjectID: w.newProject(), Status: guided.StatusAbandoned, CurrentStep: 7,
		Answers: map[string]interface{}{"new": "answer"}, DraftArtifactIDs: []string{"z", "y"}, AgentRunID: &newRun,
		CreatedBy: &otherCreator, PendingNudge: &guided.PendingNudge{Step: 9}, CreatedAt: rtAt(900), UpdatedAt: rtAt(20).In(rtCEST)}
	if err := w.repo.Update(update); err != nil {
		t.Fatalf("Update: %v", err)
	}
	row := rtReadGuidedRow(t, w.db, s.ID)
	if row.Status != guided.StatusAbandoned || row.Step != 7 || row.Run.String != newRun {
		t.Errorf("Update wrote %+v, want status abandoned, step 7 and the new run", row)
	}
	rtWantStoredJSON(t, "Update answers", row.Answers, update.Answers)
	rtWantStoredJSON(t, "Update draft_artifact_ids", row.Drafts, update.DraftArtifactIDs)
	rtWantTimestamp(t, "Update updated_at", row.UpdatedAt, update.UpdatedAt)
	if row.Project != before.Project || row.Org != before.Org || row.CreatedBy != before.CreatedBy || !row.CreatedAt.Equal(before.CreatedAt) ||
		row.Nudge != before.Nudge || row.ID != before.ID {
		t.Errorf("Update changed %+v, want the project, workspace, creator, created_at and nudge as they were (%+v)", row, before)
	}

	cleared := &guided.Session{ID: s.ID, Status: guided.StatusInProgress, UpdatedAt: rtAt(30)}
	if err := w.repo.Update(cleared); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if row := rtReadGuidedRow(t, w.db, s.ID); row.Answers != "{}" || row.Drafts != "[]" || row.Run.Valid || row.Step != 0 {
		t.Errorf("Update of a bare session wrote %+v, want {}, [], step 0 and a NULL run", row)
	}

	if err := w.repo.Update(&guided.Session{ID: uuid.New().String(), Status: guided.StatusInProgress, UpdatedAt: rtAt(1)}); err != nil {
		t.Errorf("Update of a session no row has: %v, want no error", err)
	}
	rtWantRefused(t, "Update of a malformed id", w.repo.Update(&guided.Session{ID: malformed, Status: guided.StatusInProgress, UpdatedAt: rtAt(1)}))
	rtWantUnencodable(t, "Update", w.repo.Update(&guided.Session{ID: s.ID, Status: guided.StatusCommitted, Answers: rtUnencodable, UpdatedAt: rtAt(1)}))
	if got := rtReadGuidedRow(t, w.db, s.ID); got.Status != guided.StatusInProgress {
		t.Errorf("the refused Update changed the row: %+v", got)
	}
	var count int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM guided_sessions`).Scan(&count); err != nil || count != 1 {
		t.Errorf("%d sessions after the unmatched Update calls (%v), want 1", count, err)
	}
}

// SetPendingNudge writes the one column: the nudge as JSON (the state left
// out when empty), and nil clears it; a later nudge overwrites the earlier.
// TakePendingNudge answers it once and clears it, and writes nothing else. A
// nudge JSON cannot hold fails before a statement is sent and leaves the
// parked one.
func TestGuidedRepositoryNudgeWritesOneColumn(t *testing.T) {
	w := newRTGSWorld(t)
	s := w.bare("", 3, 0)
	before := rtReadGuidedRow(t, w.db, s.ID)

	if err := w.repo.SetPendingNudge(s.ID, &guided.PendingNudge{Step: 3, Event: "saved step 3"}); err != nil {
		t.Fatal(err)
	}
	row := rtReadGuidedRow(t, w.db, s.ID)
	rtWantStoredJSON(t, "SetPendingNudge", row.Nudge.String, map[string]interface{}{"step": float64(3), "event": "saved step 3"})
	if row.Status != before.Status || row.Answers != before.Answers || !row.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("SetPendingNudge changed %+v, want only the nudge (%+v)", row, before)
	}

	err := w.repo.SetPendingNudge(s.ID, &guided.PendingNudge{Step: 1, State: rtUnencodable, Event: "x"})
	rtWantUnencodable(t, "SetPendingNudge", err)
	if got := rtReadGuidedRow(t, w.db, s.ID); !strings.Contains(got.Nudge.String, "saved step 3") {
		t.Errorf("the refused SetPendingNudge changed the nudge to %+v", got.Nudge)
	}

	if err := w.repo.SetPendingNudge(s.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := rtReadGuidedRow(t, w.db, s.ID); got.Nudge.Valid {
		t.Errorf("SetPendingNudge(nil) left the nudge %s", got.Nudge.String)
	}

	if err := w.repo.SetPendingNudge(s.ID, &guided.PendingNudge{Step: 6, State: map[string]interface{}{"k": "v"}, Event: "saved step 6"}); err != nil {
		t.Fatal(err)
	}
	took, err := w.repo.TakePendingNudge(s.ID)
	if err != nil || took == nil || took.Step != 6 || took.Event != "saved step 6" || took.State["k"] != "v" {
		t.Errorf("TakePendingNudge: %s, %v; want the nudge parked", rtJSON(took), err)
	}
	after := rtReadGuidedRow(t, w.db, s.ID)
	if after.Nudge.Valid || after.Status != before.Status || after.Step != before.Step || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("TakePendingNudge left %+v, want only the nudge cleared (%+v)", after, before)
	}
}

// SaveChatMessage writes all five columns as the message holds them, and the
// empty content as ”. Saving an id a message has is Postgres's refusal as it
// came, unwrapped; so is a session no row has (a foreign key) and a malformed
// one (the uuid's refusal); the refused writes leave no row. Any role is
// stored: the repository does not check it.
func TestGuidedRepositorySaveChatMessageWritesEveryColumn(t *testing.T) {
	w := newRTGSWorld(t)
	session := w.bare("", 1, 0)
	m := &guided.ChatMessage{ID: uuid.New().String(), SessionID: session.ID, Role: guided.ChatRoleAssistant,
		Content: "The spindle needs 4 kW.", CreatedAt: rtAt(10).In(rtCEST)}
	if err := w.repo.SaveChatMessage(m); err != nil {
		t.Fatalf("SaveChatMessage: %v", err)
	}
	rtWantGuidedMessage(t, "SaveChatMessage", rtReadGuidedMessage(t, w.db, m.ID), m)

	empty := w.message(session.ID, "any-role", "", 1)
	if got := rtReadGuidedMessage(t, w.db, empty.ID); got == nil || got.Role != "any-role" || got.Content != "" {
		t.Errorf("SaveChatMessage of an empty message with a free role stored %+v", got)
	}

	rtWantUnwrappedPQ(t, "SaveChatMessage of an id a message has", w.repo.SaveChatMessage(m), "23505")
	rtWantPQ(t, "SaveChatMessage of an id a message has", w.repo.SaveChatMessage(m), "23505", "guided_session_messages_pkey")
	noSession := &guided.ChatMessage{ID: uuid.New().String(), SessionID: uuid.New().String(), Role: guided.ChatRoleUser, CreatedAt: rtAt(1)}
	rtWantUnwrappedPQ(t, "SaveChatMessage of a session no row has", w.repo.SaveChatMessage(noSession), "23503")
	badSession := &guided.ChatMessage{ID: uuid.New().String(), SessionID: malformed, Role: guided.ChatRoleUser, CreatedAt: rtAt(1)}
	rtWantRefused(t, "SaveChatMessage of a malformed session", w.repo.SaveChatMessage(badSession))
	for _, id := range []string{noSession.ID, badSession.ID} {
		if rtReadGuidedMessage(t, w.db, id) != nil {
			t.Error("a refused SaveChatMessage wrote a row")
		}
	}
	if got := rtReadGuidedMessage(t, w.db, m.ID); got.Content != "The spindle needs 4 kW." {
		t.Errorf("the refused SaveChatMessage changed the row: %+v", got)
	}
}

// Deleting a session takes its messages with it (the foreign key cascades), so
// the transcript of a session that is gone reads as an empty list.
func TestGuidedRepositoryMessagesGoWithTheirSession(t *testing.T) {
	w := newRTGSWorld(t)
	session := w.bare("", 1, 0)
	w.message(session.ID, guided.ChatRoleUser, "Hello", 1)
	rtSeed(t, w.db, `DELETE FROM guided_sessions WHERE id = $1`, session.ID)
	list, err := w.repo.ListChatMessages(session.ID)
	rtWantEmpty(t, "ListChatMessages of a deleted session", list, err)
	if got, err := w.repo.FindByID(session.ID); got != nil || err != guided.ErrSessionNotFound {
		t.Errorf("FindByID of a deleted session: %v, %v; want nil and guided.ErrSessionNotFound", got, err)
	}
}

// On a database that fails, every method hands the driver's error back as it
// came, unwrapped: the reads answer no row and no list.
func TestGuidedRepositoryOnAFailingDatabase(t *testing.T) {
	repo := NewGuidedRepository(rtClosedDB(t))
	id := uuid.New().String()

	for name, read := range map[string]func() (interface{}, error){
		"FindByID":         func() (interface{}, error) { return repo.FindByID(id) },
		"ListByProject":    func() (interface{}, error) { return repo.ListByProject(id) },
		"ListChatMessages": func() (interface{}, error) { return repo.ListChatMessages(id) },
		"TakePendingNudge": func() (interface{}, error) { return repo.TakePendingNudge(id) },
	} {
		got, err := read()
		rtWantClosed(t, name, err, "")
		if rv := fmt.Sprintf("%#v", got); !strings.Contains(rv, "(nil)") {
			t.Errorf("%s on a failing database answered %s, want nil", name, rv)
		}
	}
	rtWantClosed(t, "Save", repo.Save(&guided.Session{ID: id, ProjectID: id}), "")
	rtWantClosed(t, "Update", repo.Update(&guided.Session{ID: id}), "")
	rtWantClosed(t, "SetPendingNudge", repo.SetPendingNudge(id, &guided.PendingNudge{Step: 1}), "")
	rtWantClosed(t, "SetPendingNudge(nil)", repo.SetPendingNudge(id, nil), "")
	rtWantClosed(t, "SaveChatMessage", repo.SaveChatMessage(&guided.ChatMessage{ID: id, SessionID: id}), "")
}
