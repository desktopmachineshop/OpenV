package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/interviews"
)

// The interview repository's round trip (refactor plan X13's characterization
// for interview_repository.go, in the manner of X13a's), pinned as found:
// every column of an interview, an invite, a session and a message read back
// through each read that scans it, the columns that may be NULL, the scan
// errors that name a column's place, what a read answers for a row no one
// has and for an empty list, and what each write puts in the row. The
// existing tests (interview_repository_test.go, agent_suite_utc_stamps_test.go,
// timestamptz_test.go and malformed_parent_list_test.go) pin the participant
// name backfill, the latest-session read, the stamps a service writes and the
// malformed-id lists, and read back none of the other columns through any
// read; none pins a column's place in a scan. X13 gives the interview, the
// invite and the session each a columns const and a scan function in place of
// the copies of each; these tests are what holds the repository still while it
// does. The repository has no transaction, so there is none to pin. The
// helpers are in repository_roundtrip_helpers_test.go.

// rtIVWorld is a fresh database with one project and a repository over it.
type rtIVWorld struct {
	t       *testing.T
	db      *sql.DB
	repo    *InterviewRepository
	project string
}

func newRTIVWorld(t *testing.T) *rtIVWorld {
	t.Helper()
	db := rtDB(t)
	w := &rtIVWorld{t: t, db: db, repo: NewInterviewRepository(db), project: uuid.New().String()}
	seedProjects(t, db, w.project)
	return w
}

// interview saves a bare interview of the project (project "" is the world's)
// created at the minute and answers it.
func (w *rtIVWorld) interview(project, name string, minute int) *interviews.Interview {
	w.t.Helper()
	if project == "" {
		project = w.project
	}
	seedProjects(w.t, w.db, project)
	iv := &interviews.Interview{ID: uuid.New().String(), ProjectID: project, Name: name, Status: interviews.InterviewStatusOpen,
		CreatedAt: rtAt(minute), UpdatedAt: rtAt(minute)}
	if err := w.repo.SaveInterview(iv); err != nil {
		w.t.Fatalf("SaveInterview %q: %v", name, err)
	}
	return iv
}

// invite saves an invite of the interview, created at the minute.
func (w *rtIVWorld) invite(interview, label string, minute int) *interviews.Invite {
	w.t.Helper()
	inv := &interviews.Invite{ID: uuid.New().String(), InterviewID: interview, TokenHash: "hash-" + uuid.New().String(),
		InviteeLabel: label, CreatedAt: rtAt(minute)}
	if err := w.repo.SaveInvite(inv); err != nil {
		w.t.Fatalf("SaveInvite %q: %v", label, err)
	}
	return inv
}

// session saves a session of the invite and interview, started at the minute.
func (w *rtIVWorld) session(interview, invite, status string, minute int) *interviews.Session {
	w.t.Helper()
	s := &interviews.Session{ID: uuid.New().String(), InterviewID: interview, InviteID: invite, Status: status, StartedAt: rtAt(minute)}
	if err := w.repo.SaveSession(s); err != nil {
		w.t.Fatalf("SaveSession: %v", err)
	}
	return s
}

func rtInterviewSansTimes(i *interviews.Interview) interviews.Interview {
	c := *i
	c.ExpiresAt, c.CreatedAt, c.UpdatedAt = nil, time.Time{}, time.Time{}
	return c
}

// rtWantInterview fails unless got is sent as an interview reads back: every
// field but the times equal, and each time the TIMESTAMP column's wall clock
// of the time sent.
func rtWantInterview(t *testing.T, what string, got, sent *interviews.Interview) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtInterviewSansTimes(got), rtInterviewSansTimes(sent))
	rtWantOptionalTimestamp(t, what+" expires_at", got.ExpiresAt, sent.ExpiresAt)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
}

func rtInviteSansTimes(i *interviews.Invite) interviews.Invite {
	c := *i
	c.ExpiresAt, c.CreatedAt = nil, time.Time{}
	return c
}

// rtWantInvite fails unless got is sent as an invite reads back: every field
// but the times equal, expires_at the instant sent in time.UTC (a TIMESTAMPTZ
// read through inUTC), created_at the TIMESTAMP column's wall clock.
func rtWantInvite(t *testing.T, what string, got, sent *interviews.Invite) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtInviteSansTimes(got), rtInviteSansTimes(sent))
	rtWantOptionalTimestamptz(t, what+" expires_at", got.ExpiresAt, sent.ExpiresAt)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
}

// rtWantOptionalTimestamptz fails unless got is nil where sent is, and else
// sent's instant, in time.UTC.
func rtWantOptionalTimestamptz(t *testing.T, what string, got, sent *time.Time) {
	t.Helper()
	if sent == nil {
		if got != nil {
			t.Errorf("%s read back as %v, want nil", what, got)
		}
		return
	}
	rtWantTimestamptz(t, what, got, *sent)
}

// rtWantOptionalInstant fails unless got is nil where sent is, and else sent's
// instant, rounded to the microsecond, in any zone: a TIMESTAMPTZ column read
// raw, outside the repository's inUTC.
func rtWantOptionalInstant(t *testing.T, what string, got, sent *time.Time) {
	t.Helper()
	if (got == nil) != (sent == nil) || (got != nil && !got.Equal(sent.Round(time.Microsecond))) {
		t.Errorf("%s stored %v, want %v", what, got, sent)
	}
}

func rtSessionSansTimes(s *interviews.Session) interviews.Session {
	c := *s
	c.StartedAt, c.EndedAt = time.Time{}, nil
	return c
}

// rtWantSession fails unless got is sent as a session reads back: every field
// but the times equal, and each time the TIMESTAMP column's wall clock of the
// time sent.
func rtWantSession(t *testing.T, what string, got, sent *interviews.Session) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	rtWantSame(t, what, rtSessionSansTimes(got), rtSessionSansTimes(sent))
	rtWantTimestamp(t, what+" started_at", got.StartedAt, sent.StartedAt)
	rtWantOptionalTimestamp(t, what+" ended_at", got.EndedAt, sent.EndedAt)
}

func rtWantMessage(t *testing.T, what string, got, sent *interviews.Message) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	stamps := func(m interviews.Message) interviews.Message { m.CreatedAt = time.Time{}; return m }
	rtWantSame(t, what, stamps(*got), stamps(*sent))
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
}

// rtWantIVList fails unless list holds exactly the items of want, in the order
// given, each read back whole.
func rtWantIVList[T any](t *testing.T, what string, list []*T, err error, id func(*T) string,
	same func(*testing.T, string, *T, *T), want ...*T) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var got, wantIDs []string
	for _, x := range list {
		got = append(got, id(x))
	}
	for _, x := range want {
		wantIDs = append(wantIDs, id(x))
	}
	if strings.Join(got, "|") != strings.Join(wantIDs, "|") {
		t.Errorf("%s listed %q, want %q", what, got, wantIDs)
		return
	}
	for i := range list {
		same(t, fmt.Sprintf("%s item %d", what, i), list[i], want[i])
	}
}

func rtWantInterviews(t *testing.T, what string, list []*interviews.Interview, err error, want ...*interviews.Interview) {
	t.Helper()
	rtWantIVList(t, what, list, err, func(i *interviews.Interview) string { return i.ID }, rtWantInterview, want...)
}

func rtWantInvites(t *testing.T, what string, list []*interviews.Invite, err error, want ...*interviews.Invite) {
	t.Helper()
	rtWantIVList(t, what, list, err, func(i *interviews.Invite) string { return i.ID }, rtWantInvite, want...)
}

func rtWantSessions(t *testing.T, what string, list []*interviews.Session, err error, want ...*interviews.Session) {
	t.Helper()
	rtWantIVList(t, what, list, err, func(s *interviews.Session) string { return s.ID }, rtWantSession, want...)
}

func rtWantMessages(t *testing.T, what string, list []*interviews.Message, err error, want ...*interviews.Message) {
	t.Helper()
	rtWantIVList(t, what, list, err, func(m *interviews.Message) string { return m.ID }, rtWantMessage, want...)
}

// Every column SaveInterview writes reads back through FindInterviewByID and
// ListInterviewsByProject, whole: the project, the three optional ids, the
// name, the brief, the status, the optional expiry, the creator and the two
// stamps (the wall clock sent, its offset dropped, to the microsecond). Each
// column holds a value no other does, so a swap of two in a scan shows, and a
// second interview leaves every optional column NULL (nil pointers, an empty
// brief). The list answers newest created_at first and only the project's.
func TestInterviewRepositoryReadsBackEveryInterviewColumn(t *testing.T) {
	w := newRTIVWorld(t)
	guided, persona, agent, creator := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	expires := rtAt(900).In(rtCEST)
	full := &interviews.Interview{ID: uuid.New().String(), ProjectID: w.project, GuidedSessionID: &guided, PersonaArtifactID: &persona,
		Name: "Machinists", Brief: "How they zero the vice", AgentID: &agent, Status: interviews.InterviewStatusClosed,
		ExpiresAt: &expires, CreatedBy: &creator, CreatedAt: rtAt(10), UpdatedAt: rtAt(13).In(rtCEST)}
	if err := w.repo.SaveInterview(full); err != nil {
		t.Fatalf("SaveInterview: %v", err)
	}
	bare := &interviews.Interview{ID: uuid.New().String(), ProjectID: w.project, Name: "Estimators", Status: interviews.InterviewStatusOpen,
		CreatedAt: rtAt(20), UpdatedAt: rtAt(22)}
	if err := w.repo.SaveInterview(bare); err != nil {
		t.Fatalf("SaveInterview: %v", err)
	}
	elsewhere := w.interview(uuid.New().String(), "Elsewhere", 30)

	for name, iv := range map[string]*interviews.Interview{"full": full, "bare": bare, "elsewhere": elsewhere} {
		got, err := w.repo.FindInterviewByID(iv.ID)
		if err != nil {
			t.Fatalf("FindInterviewByID %s: %v", name, err)
		}
		rtWantInterview(t, "FindInterviewByID "+name, got, iv)
	}
	list, err := w.repo.ListInterviewsByProject(w.project)
	rtWantInterviews(t, "ListInterviewsByProject", list, err, bare, full)
	list, err = w.repo.ListInterviewsByProject(elsewhere.ProjectID)
	rtWantInterviews(t, "ListInterviewsByProject of the other project", list, err, elsewhere)
}

// Every column SaveInvite writes reads back through FindInviteByID,
// FindInviteByTokenHash and ListInvitesByInterview, whole: the interview, the
// token hash, the label, the optional expiry (an instant, read back in
// time.UTC whatever zone it was sent in), the revoked flag and created_at (the
// wall clock sent). A second invite leaves the expiry NULL and the label
// empty. The list answers newest created_at first and only the interview's.
func TestInterviewRepositoryReadsBackEveryInviteColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv, other := w.interview("", "Machinists", 0), w.interview("", "Estimators", 1)
	expires := rtAt(500).In(rtCEST)
	full := &interviews.Invite{ID: uuid.New().String(), InterviewID: iv.ID, TokenHash: "hash-full", InviteeLabel: "Shop floor",
		ExpiresAt: &expires, Revoked: true, CreatedAt: rtAt(10)}
	if err := w.repo.SaveInvite(full); err != nil {
		t.Fatalf("SaveInvite: %v", err)
	}
	bare := &interviews.Invite{ID: uuid.New().String(), InterviewID: iv.ID, TokenHash: "hash-bare", CreatedAt: rtAt(20)}
	if err := w.repo.SaveInvite(bare); err != nil {
		t.Fatalf("SaveInvite: %v", err)
	}
	foreign := w.invite(other.ID, "Foreign", 30)

	for name, inv := range map[string]*interviews.Invite{"full": full, "bare": bare, "foreign": foreign} {
		got, err := w.repo.FindInviteByID(inv.ID)
		if err != nil {
			t.Fatalf("FindInviteByID %s: %v", name, err)
		}
		rtWantInvite(t, "FindInviteByID "+name, got, inv)
		got, err = w.repo.FindInviteByTokenHash(inv.TokenHash)
		if err != nil {
			t.Fatalf("FindInviteByTokenHash %s: %v", name, err)
		}
		rtWantInvite(t, "FindInviteByTokenHash "+name, got, inv)
	}
	list, err := w.repo.ListInvitesByInterview(iv.ID)
	rtWantInvites(t, "ListInvitesByInterview", list, err, bare, full)
	list, err = w.repo.ListInvitesByInterview(other.ID)
	rtWantInvites(t, "ListInvitesByInterview of the other interview", list, err, foreign)
}

// Every column SaveSession writes reads back through FindSessionByID, the
// active and latest reads of an invite, and the lists of an interview and of
// a project, whole: the interview, the invite, the participant, the status,
// the summary and the two stamps. Each column holds a value no other does; a
// second session leaves the optional end NULL. The lists answer newest
// started_at first; ListSessionsByProject reaches every interview of the
// project but no other, and stops at its limit. The active read answers the
// newest session of the invite whose status is active, the latest read the
// newest whatever its status.
func TestInterviewRepositoryReadsBackEverySessionColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv, second := w.interview("", "Machinists", 0), w.interview("", "Estimators", 1)
	foreignIV := w.interview(uuid.New().String(), "Elsewhere", 2)
	invite := w.invite(iv.ID, "Shop floor", 3)
	ended := rtAt(70).In(rtCEST)
	done := &interviews.Session{ID: uuid.New().String(), InterviewID: iv.ID, InviteID: invite.ID, ParticipantName: "Ada",
		Status: interviews.SessionStatusCompleted, Summary: "Zeroes the vice first.", StartedAt: rtAt(10), EndedAt: &ended}
	if err := w.repo.SaveSession(done); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	open := &interviews.Session{ID: uuid.New().String(), InterviewID: iv.ID, InviteID: invite.ID,
		Status: interviews.SessionStatusActive, StartedAt: rtAt(20)}
	if err := w.repo.SaveSession(open); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	otherInvite := uuid.New().String()
	inSecond := w.session(second.ID, otherInvite, interviews.SessionStatusActive, 30)
	inForeign := w.session(foreignIV.ID, uuid.New().String(), interviews.SessionStatusActive, 40)

	for name, s := range map[string]*interviews.Session{"completed": done, "active": open, "second": inSecond, "foreign": inForeign} {
		got, err := w.repo.FindSessionByID(s.ID)
		if err != nil {
			t.Fatalf("FindSessionByID %s: %v", name, err)
		}
		rtWantSession(t, "FindSessionByID "+name, got, s)
	}

	// The invite's sessions: the active one is the newest, so both reads
	// answer it.
	got, err := w.repo.FindActiveSessionByInvite(invite.ID)
	if err != nil {
		t.Fatalf("FindActiveSessionByInvite: %v", err)
	}
	rtWantSession(t, "FindActiveSessionByInvite", got, open)
	got, err = w.repo.FindLatestSessionByInvite(invite.ID)
	if err != nil {
		t.Fatalf("FindLatestSessionByInvite: %v", err)
	}
	rtWantSession(t, "FindLatestSessionByInvite", got, open)
	got, err = w.repo.FindActiveSessionByInvite(otherInvite)
	if err != nil {
		t.Fatalf("FindActiveSessionByInvite of another invite: %v", err)
	}
	rtWantSession(t, "FindActiveSessionByInvite of another invite", got, inSecond)

	// A completed session started later is the latest but not the active one.
	closedLater := w.session(iv.ID, invite.ID, interviews.SessionStatusCompleted, 50)
	got, err = w.repo.FindActiveSessionByInvite(invite.ID)
	if err != nil {
		t.Fatalf("FindActiveSessionByInvite: %v", err)
	}
	rtWantSession(t, "FindActiveSessionByInvite beside a newer completed session", got, open)
	got, err = w.repo.FindLatestSessionByInvite(invite.ID)
	if err != nil {
		t.Fatalf("FindLatestSessionByInvite: %v", err)
	}
	rtWantSession(t, "FindLatestSessionByInvite beside a newer completed session", got, closedLater)
	// An older active session than the active one is never the answer.
	older := w.session(iv.ID, invite.ID, interviews.SessionStatusActive, 5)
	got, err = w.repo.FindActiveSessionByInvite(invite.ID)
	if err != nil {
		t.Fatalf("FindActiveSessionByInvite: %v", err)
	}
	rtWantSession(t, "FindActiveSessionByInvite beside an older active session", got, open)

	list, err := w.repo.ListSessionsByInterview(iv.ID)
	if err != nil || len(list) != 4 {
		t.Fatalf("ListSessionsByInterview listed %s, %v; want four", rtJSON(list), err)
	}
	for i, want := range []*interviews.Session{closedLater, open, done, older} {
		rtWantSession(t, fmt.Sprintf("ListSessionsByInterview item %d", i), list[i], want)
	}
	list, err = w.repo.ListSessionsByInterview(second.ID)
	rtWantSessions(t, "ListSessionsByInterview of the other interview", list, err, inSecond)

	// The project's sessions across its interviews, newest first.
	list, err = w.repo.ListSessionsByProject(w.project, 100)
	if err != nil || len(list) != 5 {
		t.Fatalf("ListSessionsByProject listed %s, %v; want five", rtJSON(list), err)
	}
	for i, want := range []*interviews.Session{closedLater, inSecond, open, done, older} {
		rtWantSession(t, fmt.Sprintf("ListSessionsByProject item %d", i), list[i], want)
	}
	list, err = w.repo.ListSessionsByProject(w.project, 2)
	rtWantSessions(t, "ListSessionsByProject with a limit of 2", list, err, closedLater, inSecond)
	list, err = w.repo.ListSessionsByProject(foreignIV.ProjectID, 10)
	rtWantSessions(t, "ListSessionsByProject of the other project", list, err, inForeign)
	list, err = w.repo.ListSessionsByProject(w.project, 0)
	rtWantNil(t, "ListSessionsByProject with a limit of 0", list, err)
	list, err = w.repo.ListSessionsByProject(w.project, -1)
	if list != nil {
		t.Errorf("ListSessionsByProject with a negative limit answered %s", rtJSON(list))
	}
	rtWantPQ(t, "ListSessionsByProject with a negative limit", err, "2201W", "")
}

// Every column SaveMessage writes reads back through ListMessagesBySession,
// whole: the id, the session, the role, the content and created_at (the wall
// clock sent), each a value no other holds. The transcript is oldest
// created_at first and only the session's.
func TestInterviewRepositoryReadsBackEveryMessageColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	invite := w.invite(iv.ID, "Shop floor", 1)
	session := w.session(iv.ID, invite.ID, interviews.SessionStatusActive, 2)
	other := w.session(iv.ID, invite.ID, interviews.SessionStatusActive, 3)

	late := &interviews.Message{ID: uuid.New().String(), SessionID: session.ID, Role: interviews.RoleParticipant,
		Content: "I zero the vice first.", CreatedAt: rtAt(30)}
	early := &interviews.Message{ID: uuid.New().String(), SessionID: session.ID, Role: interviews.RoleAssistant,
		Content: "What do you do first?", CreatedAt: rtAt(10)}
	middle := &interviews.Message{ID: uuid.New().String(), SessionID: session.ID, Role: interviews.RoleSystem,
		Content: "", CreatedAt: rtAt(20)}
	foreign := &interviews.Message{ID: uuid.New().String(), SessionID: other.ID, Role: interviews.RoleParticipant,
		Content: "Not this transcript", CreatedAt: rtAt(15)}
	for _, m := range []*interviews.Message{late, early, middle, foreign} {
		if err := w.repo.SaveMessage(m); err != nil {
			t.Fatalf("SaveMessage: %v", err)
		}
	}

	list, err := w.repo.ListMessagesBySession(session.ID)
	rtWantMessages(t, "ListMessagesBySession", list, err, early, middle, late)
	list, err = w.repo.ListMessagesBySession(other.ID)
	rtWantMessages(t, "ListMessagesBySession of the other session", list, err, foreign)
}

// rtIVRead is one read of the repository that answers rows: the ids of what it
// answered, nil for no row and for a nil list, and the error as it came.
type rtIVRead struct {
	name string
	run  func() ([]string, error)
}

func rtIVOne[T any](id func(*T) string, x *T, err error) ([]string, error) {
	if x == nil {
		return nil, err
	}
	return []string{id(x)}, err
}

func rtIVMany[T any](id func(*T) string, xs []*T, err error) ([]string, error) {
	if xs == nil {
		return nil, err
	}
	ids := make([]string, 0, len(xs))
	for _, x := range xs {
		ids = append(ids, id(x))
	}
	return ids, err
}

// rtIVScanCase is a column the schema forbids NULL, here one a test has let
// take it, and what that does to each read of the table.
type rtIVScanCase struct {
	column  string
	index   int
	message string
	// misses names the reads that cannot match the row.
	misses []string
}

const (
	rtIVStr  = "converting NULL to string is unsupported"
	rtIVBool = "sql/driver: couldn't convert <nil> (<nil>) into type bool"
	rtIVTime = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
)

// rtIVScanErrors drives one table's scan errors: for each case it inserts a row
// whose column is NULL (the others from base, a SQL literal each), runs every
// read and expects the error naming the column's place and no row and no list,
// or no row, no list and no error where the read cannot match the row.
func rtIVScanErrors(t *testing.T, db *sql.DB, table string, base map[string]string, cases []rtIVScanCase,
	reads func(id string) []rtIVRead) {
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
					rtWantNil(t, fmt.Sprintf("%s over a row with a NULL %s", read.name, c.column), got, err)
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

// A column of an interview the schema forbids NULL fails the reads that scan
// it, naming the column and its place among the twelve scanned (id 0,
// project_id 1, guided_session_id 2, persona_artifact_id 3, name 4, brief 5,
// agent_id 6, status 7, expires_at 8, created_by 9, created_at 10, updated_at
// 11), unwrapped, with no interview and no list (nil, not []). An interview
// with no project is out of its project's list. (The id is the primary key;
// it takes no NULL.)
func TestInterviewRepositoryInterviewScanErrorsNameTheColumn(t *testing.T) {
	w := newRTIVWorld(t)
	rtIVScanErrors(t, w.db, "interviews", map[string]string{
		"project_id": "'" + w.project + "'", "name": "'Machinists'", "brief": "'Brief'", "status": "'open'",
		"created_at": "NOW()", "updated_at": "NOW()",
	}, []rtIVScanCase{
		{"project_id", 1, rtIVStr, []string{"ListInterviewsByProject"}},
		{"name", 4, rtIVStr, nil},
		{"brief", 5, rtIVStr, nil},
		{"status", 7, rtIVStr, nil},
		{"created_at", 10, rtIVTime, nil},
		{"updated_at", 11, rtIVTime, nil},
	}, func(id string) []rtIVRead {
		return []rtIVRead{
			{"FindInterviewByID", func() ([]string, error) {
				iv, err := w.repo.FindInterviewByID(id)
				return rtIVOne(func(i *interviews.Interview) string { return i.ID }, iv, err)
			}},
			{"ListInterviewsByProject", func() ([]string, error) {
				list, err := w.repo.ListInterviewsByProject(w.project)
				return rtIVMany(func(i *interviews.Interview) string { return i.ID }, list, err)
			}},
		}
	})
}

// A column of an invite the schema forbids NULL fails the reads that scan it,
// naming the column and its place among the seven scanned (id 0, interview_id
// 1, token_hash 2, invitee_label 3, expires_at 4, revoked 5, created_at 6),
// unwrapped, with no invite and no list. An invite with no interview is out of
// the interview's list, and one with no token hash cannot be found by a hash.
func TestInterviewRepositoryInviteScanErrorsNameTheColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	rtIVScanErrors(t, w.db, "interview_invites", map[string]string{
		"interview_id": "'" + iv.ID + "'", "token_hash": "'hash-null'", "invitee_label": "'Shop floor'", "revoked": "FALSE",
		"created_at": "NOW()",
	}, []rtIVScanCase{
		{"interview_id", 1, rtIVStr, []string{"ListInvitesByInterview"}},
		{"token_hash", 2, rtIVStr, []string{"FindInviteByTokenHash"}},
		{"invitee_label", 3, rtIVStr, nil},
		{"revoked", 5, rtIVBool, nil},
		{"created_at", 6, rtIVTime, nil},
	}, func(id string) []rtIVRead {
		inviteID := func(i *interviews.Invite) string { return i.ID }
		return []rtIVRead{
			{"FindInviteByID", func() ([]string, error) {
				inv, err := w.repo.FindInviteByID(id)
				return rtIVOne(inviteID, inv, err)
			}},
			{"FindInviteByTokenHash", func() ([]string, error) {
				inv, err := w.repo.FindInviteByTokenHash("hash-null")
				return rtIVOne(inviteID, inv, err)
			}},
			{"ListInvitesByInterview", func() ([]string, error) {
				list, err := w.repo.ListInvitesByInterview(iv.ID)
				return rtIVMany(inviteID, list, err)
			}},
		}
	})
}

// A column of a session the schema forbids NULL fails the reads that scan it,
// naming the column and its place among the eight scanned (id 0, interview_id
// 1, invite_id 2, participant_name 3, status 4, summary 5, started_at 6,
// ended_at 7), unwrapped, with no session and no list. A session with no
// interview is out of the interview's list and the project's; one with no
// invite is out of the two reads of an invite; one with no status is not an
// active session.
func TestInterviewRepositorySessionScanErrorsNameTheColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	invite := uuid.New().String()
	rtIVScanErrors(t, w.db, "interview_sessions", map[string]string{
		"interview_id": "'" + iv.ID + "'", "invite_id": "'" + invite + "'", "participant_name": "'Ada'", "status": "'active'",
		"summary": "'Summary'", "started_at": "NOW()",
	}, []rtIVScanCase{
		{"interview_id", 1, rtIVStr, []string{"ListSessionsByInterview", "ListSessionsByProject"}},
		{"invite_id", 2, rtIVStr, []string{"FindActiveSessionByInvite", "FindLatestSessionByInvite"}},
		{"participant_name", 3, rtIVStr, nil},
		{"status", 4, rtIVStr, []string{"FindActiveSessionByInvite"}},
		{"summary", 5, rtIVStr, nil},
		{"started_at", 6, rtIVTime, nil},
	}, func(id string) []rtIVRead {
		sessionID := func(s *interviews.Session) string { return s.ID }
		return []rtIVRead{
			{"FindSessionByID", func() ([]string, error) {
				s, err := w.repo.FindSessionByID(id)
				return rtIVOne(sessionID, s, err)
			}},
			{"FindActiveSessionByInvite", func() ([]string, error) {
				s, err := w.repo.FindActiveSessionByInvite(invite)
				return rtIVOne(sessionID, s, err)
			}},
			{"FindLatestSessionByInvite", func() ([]string, error) {
				s, err := w.repo.FindLatestSessionByInvite(invite)
				return rtIVOne(sessionID, s, err)
			}},
			{"ListSessionsByInterview", func() ([]string, error) {
				list, err := w.repo.ListSessionsByInterview(iv.ID)
				return rtIVMany(sessionID, list, err)
			}},
			{"ListSessionsByProject", func() ([]string, error) {
				list, err := w.repo.ListSessionsByProject(w.project, 10)
				return rtIVMany(sessionID, list, err)
			}},
		}
	})
}

// A column of a message the schema forbids NULL fails the transcript read,
// naming the column and its place among the five scanned (id 0, session_id 1,
// role 2, content 3, created_at 4), unwrapped, with no list. A message with no
// session is in no transcript.
func TestInterviewRepositoryMessageScanErrorsNameTheColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	session := w.session(iv.ID, uuid.New().String(), interviews.SessionStatusActive, 1)
	rtIVScanErrors(t, w.db, "interview_messages", map[string]string{
		"session_id": "'" + session.ID + "'", "role": "'participant'", "content": "'Content'", "created_at": "NOW()",
	}, []rtIVScanCase{
		{"session_id", 1, rtIVStr, []string{"ListMessagesBySession"}},
		{"role", 2, rtIVStr, nil},
		{"content", 3, rtIVStr, nil},
		{"created_at", 4, rtIVTime, nil},
	}, func(string) []rtIVRead {
		return []rtIVRead{{"ListMessagesBySession", func() ([]string, error) {
			list, err := w.repo.ListMessagesBySession(session.ID)
			return rtIVMany(func(m *interviews.Message) string { return m.ID }, list, err)
		}}}
	})
}

// A read for a row no one has: FindInterviewByID answers no interview and
// ErrInterviewNotFound itself, FindSessionByID no session and
// ErrSessionNotFound, for an id no row has and for a malformed one; the invite
// reads and the two session reads of an invite answer nil, nil; every list
// answers nil, no error, for an id nothing matches, for a malformed one and for
// an empty string, never [].
func TestInterviewRepositoryAnswersNoRow(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	empty := w.interview("", "Estimators", 1)
	invite := w.invite(iv.ID, "Shop floor", 2)
	session := w.session(iv.ID, invite.ID, interviews.SessionStatusCompleted, 3)

	for _, id := range []string{uuid.New().String(), "not-a-uuid", ""} {
		if got, err := w.repo.FindInterviewByID(id); got != nil || err != interviews.ErrInterviewNotFound {
			t.Errorf("FindInterviewByID(%q): %v, %v; want nil and ErrInterviewNotFound", id, got, err)
		}
		if got, err := w.repo.FindSessionByID(id); got != nil || err != interviews.ErrSessionNotFound {
			t.Errorf("FindSessionByID(%q): %v, %v; want nil and ErrSessionNotFound", id, got, err)
		}
		if got, err := w.repo.FindInviteByID(id); got != nil || err != nil {
			t.Errorf("FindInviteByID(%q): %v, %v; want nil, nil", id, got, err)
		}
		if got, err := w.repo.FindInviteByTokenHash(id); got != nil || err != nil {
			t.Errorf("FindInviteByTokenHash(%q): %v, %v; want nil, nil", id, got, err)
		}
		if got, err := w.repo.FindActiveSessionByInvite(id); got != nil || err != nil {
			t.Errorf("FindActiveSessionByInvite(%q): %v, %v; want nil, nil", id, got, err)
		}
		if got, err := w.repo.FindLatestSessionByInvite(id); got != nil || err != nil {
			t.Errorf("FindLatestSessionByInvite(%q): %v, %v; want nil, nil", id, got, err)
		}
		interviewList, err := w.repo.ListInterviewsByProject(id)
		rtWantNil(t, fmt.Sprintf("ListInterviewsByProject(%q)", id), interviewList, err)
		inviteList, err := w.repo.ListInvitesByInterview(id)
		rtWantNil(t, fmt.Sprintf("ListInvitesByInterview(%q)", id), inviteList, err)
		sessionList, err := w.repo.ListSessionsByInterview(id)
		rtWantNil(t, fmt.Sprintf("ListSessionsByInterview(%q)", id), sessionList, err)
		sessionList, err = w.repo.ListSessionsByProject(id, 10)
		rtWantNil(t, fmt.Sprintf("ListSessionsByProject(%q)", id), sessionList, err)
		messageList, err := w.repo.ListMessagesBySession(id)
		rtWantNil(t, fmt.Sprintf("ListMessagesBySession(%q)", id), messageList, err)
	}

	// Rows that exist, for parents with nothing under them.
	inviteList, err := w.repo.ListInvitesByInterview(empty.ID)
	rtWantNil(t, "ListInvitesByInterview of an interview with none", inviteList, err)
	sessionList, err := w.repo.ListSessionsByInterview(empty.ID)
	rtWantNil(t, "ListSessionsByInterview of an interview with none", sessionList, err)
	messageList, err := w.repo.ListMessagesBySession(session.ID)
	rtWantNil(t, "ListMessagesBySession of a session with none", messageList, err)
	interviewList, err := w.repo.ListInterviewsByProject(uuid.New().String())
	rtWantNil(t, "ListInterviewsByProject of a project with none", interviewList, err)
	if got, err := w.repo.FindActiveSessionByInvite(invite.ID); got != nil || err != nil {
		t.Errorf("FindActiveSessionByInvite of an invite whose only session ended: %v, %v; want nil, nil", got, err)
	}
	if got, err := w.repo.FindInviteByTokenHash("hash-nobody"); got != nil || err != nil {
		t.Errorf("FindInviteByTokenHash of a hash no invite has: %v, %v; want nil, nil", got, err)
	}
}

// rtIVRow is the raw columns of an interview row, as stored.
type rtIVRow struct {
	ID, ProjectID, Name, Brief, Status string
	Guided, Persona, Agent, CreatedBy  sql.NullString
	ExpiresAt                          *time.Time
	CreatedAt, UpdatedAt               time.Time
}

func rtReadInterviewRow(t *testing.T, db *sql.DB, id string) *rtIVRow {
	t.Helper()
	r := new(rtIVRow)
	err := db.QueryRow(`SELECT id::text, project_id::text, guided_session_id::text, persona_artifact_id::text, name, brief,
		agent_id::text, status, expires_at, created_by::text, created_at, updated_at FROM interviews WHERE id = $1`, id).
		Scan(&r.ID, &r.ProjectID, &r.Guided, &r.Persona, &r.Name, &r.Brief, &r.Agent, &r.Status, &r.ExpiresAt, &r.CreatedBy,
			&r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read interview %s: %v", id, err)
	}
	return r
}

// rtIVInviteRow is the raw columns of an invite row, as stored.
type rtIVInviteRow struct {
	ID, InterviewID, TokenHash, Label string
	ExpiresAt                         *time.Time
	Revoked                           bool
	CreatedAt                         time.Time
}

func rtReadInviteRow(t *testing.T, db *sql.DB, id string) *rtIVInviteRow {
	t.Helper()
	r := new(rtIVInviteRow)
	err := db.QueryRow(`SELECT id::text, interview_id::text, token_hash, invitee_label, expires_at, revoked, created_at
		FROM interview_invites WHERE id = $1`, id).Scan(&r.ID, &r.InterviewID, &r.TokenHash, &r.Label, &r.ExpiresAt, &r.Revoked, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read invite %s: %v", id, err)
	}
	return r
}

// rtIVSessionRow is the raw columns of a session row, as stored.
type rtIVSessionRow struct {
	ID, InterviewID, InviteID, Name, Status, Summary string
	StartedAt                                        time.Time
	EndedAt                                          *time.Time
}

func rtReadSessionRow(t *testing.T, db *sql.DB, id string) *rtIVSessionRow {
	t.Helper()
	r := new(rtIVSessionRow)
	err := db.QueryRow(`SELECT id::text, interview_id::text, invite_id::text, participant_name, status, summary, started_at, ended_at
		FROM interview_sessions WHERE id = $1`, id).Scan(&r.ID, &r.InterviewID, &r.InviteID, &r.Name, &r.Status, &r.Summary,
		&r.StartedAt, &r.EndedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read session %s: %v", id, err)
	}
	return r
}

func rtReadMessageRow(t *testing.T, db *sql.DB, id string) *interviews.Message {
	t.Helper()
	m := new(interviews.Message)
	err := db.QueryRow(`SELECT id::text, session_id::text, role, content, created_at FROM interview_messages WHERE id = $1`, id).
		Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read message %s: %v", id, err)
	}
	return m
}

func rtWantNullString(t *testing.T, what string, got sql.NullString, want *string) {
	t.Helper()
	if want == nil {
		if got.Valid {
			t.Errorf("%s stored %q, want NULL", what, got.String)
		}
		return
	}
	if !got.Valid || got.String != *want {
		t.Errorf("%s stored %v, want %q", what, got, *want)
	}
}

// SaveInterview writes all twelve columns as the interview holds them, a nil
// optional id or expiry as NULL, and an empty brief as ”. Saving an id an
// interview has is Postgres's refusal as it came, unwrapped; so is a project
// no row has (a foreign key) and a malformed one (the uuid's refusal); the
// refused writes leave no row.
func TestInterviewRepositorySaveInterviewWritesEveryColumn(t *testing.T) {
	w := newRTIVWorld(t)
	guided, persona, agent, creator := uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String()
	expires := rtAt(900).In(rtCEST)
	iv := &interviews.Interview{ID: uuid.New().String(), ProjectID: w.project, GuidedSessionID: &guided, PersonaArtifactID: &persona,
		Name: "Machinists", Brief: "How they zero the vice", AgentID: &agent, Status: interviews.InterviewStatusClosed,
		ExpiresAt: &expires, CreatedBy: &creator, CreatedAt: rtAt(10).In(rtCEST), UpdatedAt: rtAt(13).In(rtCEST)}
	if err := w.repo.SaveInterview(iv); err != nil {
		t.Fatalf("SaveInterview: %v", err)
	}
	row := rtReadInterviewRow(t, w.db, iv.ID)
	if row == nil {
		t.Fatal("SaveInterview wrote no row")
	}
	if row.ID != iv.ID || row.ProjectID != w.project || row.Name != "Machinists" || row.Brief != "How they zero the vice" ||
		row.Status != "closed" {
		t.Errorf("SaveInterview wrote %+v, want %s", row, rtJSON(iv))
	}
	rtWantNullString(t, "SaveInterview guided_session_id", row.Guided, &guided)
	rtWantNullString(t, "SaveInterview persona_artifact_id", row.Persona, &persona)
	rtWantNullString(t, "SaveInterview agent_id", row.Agent, &agent)
	rtWantNullString(t, "SaveInterview created_by", row.CreatedBy, &creator)
	rtWantOptionalTimestamp(t, "SaveInterview expires_at", row.ExpiresAt, iv.ExpiresAt)
	rtWantTimestamp(t, "SaveInterview created_at", row.CreatedAt, iv.CreatedAt)
	rtWantTimestamp(t, "SaveInterview updated_at", row.UpdatedAt, iv.UpdatedAt)

	bare := &interviews.Interview{ID: uuid.New().String(), ProjectID: w.project, Name: "Estimators", Status: interviews.InterviewStatusOpen,
		CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	if err := w.repo.SaveInterview(bare); err != nil {
		t.Fatalf("SaveInterview of a bare interview: %v", err)
	}
	row = rtReadInterviewRow(t, w.db, bare.ID)
	if row.Brief != "" || row.Guided.Valid || row.Persona.Valid || row.Agent.Valid || row.CreatedBy.Valid || row.ExpiresAt != nil {
		t.Errorf("SaveInterview of a bare interview wrote %+v, want an empty brief and NULL optional columns", row)
	}

	again := *iv
	again.Name = "Other"
	err := w.repo.SaveInterview(&again)
	rtWantUnwrappedPQ(t, "SaveInterview of an id an interview has", err, "23505")
	rtWantPQ(t, "SaveInterview of an id an interview has", err, "23505", "interviews_pkey")
	if row := rtReadInterviewRow(t, w.db, iv.ID); row.Name != "Machinists" {
		t.Errorf("the refused SaveInterview changed the row: %+v", row)
	}

	noProject := &interviews.Interview{ID: uuid.New().String(), ProjectID: uuid.New().String(), Name: "Orphan", Status: "open",
		CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	err = w.repo.SaveInterview(noProject)
	rtWantUnwrappedPQ(t, "SaveInterview of a project no row has", err, "23503")
	malformed := &interviews.Interview{ID: uuid.New().String(), ProjectID: "not-a-uuid", Name: "Malformed", Status: "open",
		CreatedAt: rtAt(1), UpdatedAt: rtAt(1)}
	rtWantRefused(t, "SaveInterview of a malformed project", w.repo.SaveInterview(malformed))
	for _, id := range []string{noProject.ID, malformed.ID} {
		if rtReadInterviewRow(t, w.db, id) != nil {
			t.Error("a refused SaveInterview wrote a row")
		}
	}
}

// UpdateInterview rewrites the guided session, the persona, the name, the
// brief, the agent, the status, the expiry and updated_at, and nothing else:
// not the project, the creator nor created_at, whatever the interview it is
// given holds. A nil optional id or expiry clears the column to NULL. It is no
// error for an interview no row has (and writes none); a malformed id is
// Postgres's refusal as it came.
func TestInterviewRepositoryUpdateInterviewWritesItsEightColumns(t *testing.T) {
	w := newRTIVWorld(t)
	oldPersona, creator := uuid.New().String(), uuid.New().String()
	oldExpiry := rtAt(800)
	iv := &interviews.Interview{ID: uuid.New().String(), ProjectID: w.project, PersonaArtifactID: &oldPersona, Name: "Machinists",
		Brief: "Old brief", Status: interviews.InterviewStatusOpen, ExpiresAt: &oldExpiry, CreatedBy: &creator,
		CreatedAt: rtAt(1), UpdatedAt: rtAt(2)}
	if err := w.repo.SaveInterview(iv); err != nil {
		t.Fatal(err)
	}
	before := rtReadInterviewRow(t, w.db, iv.ID)

	guided, persona, agent := uuid.New().String(), uuid.New().String(), uuid.New().String()
	expires := rtAt(900).In(rtCEST)
	sent := &interviews.Interview{ID: iv.ID, ProjectID: uuid.New().String(), GuidedSessionID: &guided, PersonaArtifactID: &persona,
		Name: "Estimators", Brief: "New brief", AgentID: &agent, Status: interviews.InterviewStatusClosed, ExpiresAt: &expires,
		CreatedBy: &agent, CreatedAt: rtAt(-99), UpdatedAt: rtAt(60).In(rtCEST)}
	if err := w.repo.UpdateInterview(sent); err != nil {
		t.Fatalf("UpdateInterview: %v", err)
	}
	after := rtReadInterviewRow(t, w.db, iv.ID)
	if after.Name != "Estimators" || after.Brief != "New brief" || after.Status != "closed" {
		t.Errorf("UpdateInterview wrote %+v, want the name, brief and status of %s", after, rtJSON(sent))
	}
	rtWantNullString(t, "UpdateInterview guided_session_id", after.Guided, &guided)
	rtWantNullString(t, "UpdateInterview persona_artifact_id", after.Persona, &persona)
	rtWantNullString(t, "UpdateInterview agent_id", after.Agent, &agent)
	rtWantOptionalTimestamp(t, "UpdateInterview expires_at", after.ExpiresAt, sent.ExpiresAt)
	rtWantTimestamp(t, "UpdateInterview updated_at", after.UpdatedAt, sent.UpdatedAt)
	if after.ProjectID != before.ProjectID || after.CreatedBy != before.CreatedBy || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("UpdateInterview changed the columns it never writes: %+v, was %+v", after, before)
	}

	// Nil optional columns are written as NULL.
	cleared := &interviews.Interview{ID: iv.ID, Name: "Estimators", Status: interviews.InterviewStatusOpen, UpdatedAt: rtAt(61)}
	if err := w.repo.UpdateInterview(cleared); err != nil {
		t.Fatal(err)
	}
	after = rtReadInterviewRow(t, w.db, iv.ID)
	if after.Guided.Valid || after.Persona.Valid || after.Agent.Valid || after.ExpiresAt != nil || after.Brief != "" {
		t.Errorf("UpdateInterview with nil optionals wrote %+v, want NULL columns and an empty brief", after)
	}

	missing := &interviews.Interview{ID: uuid.New().String(), Name: "Nobody", Status: "open", UpdatedAt: rtAt(0)}
	if err := w.repo.UpdateInterview(missing); err != nil || rtReadInterviewRow(t, w.db, missing.ID) != nil {
		t.Errorf("UpdateInterview of an interview no row has: %v, row %+v; want no error and no row", err,
			rtReadInterviewRow(t, w.db, missing.ID))
	}
	rtWantRefused(t, "UpdateInterview of a malformed id", w.repo.UpdateInterview(&interviews.Interview{ID: "not-a-uuid", Name: "x", Status: "open"}))
}

// SaveInvite writes all seven columns as the invite holds them (a nil expiry
// as NULL, an empty label as ”). Saving an id an invite has, or a token hash
// one has, is Postgres's refusal as it came (the unique index on the hash for
// the second); an interview no row has is a foreign key's, a malformed one the
// uuid's; the refused writes leave no row. RevokeInvite sets the flag and
// nothing else, again changes nothing, is no error for an invite no row has,
// and a malformed id is Postgres's refusal as it came.
func TestInterviewRepositorySaveInviteWritesEveryColumnAndRevokeInvite(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	expires := rtAt(500).In(rtCEST)
	inv := &interviews.Invite{ID: uuid.New().String(), InterviewID: iv.ID, TokenHash: "hash-one", InviteeLabel: "Shop floor",
		ExpiresAt: &expires, Revoked: true, CreatedAt: rtAt(10).In(rtCEST)}
	if err := w.repo.SaveInvite(inv); err != nil {
		t.Fatalf("SaveInvite: %v", err)
	}
	row := rtReadInviteRow(t, w.db, inv.ID)
	if row == nil {
		t.Fatal("SaveInvite wrote no row")
	}
	if row.ID != inv.ID || row.InterviewID != iv.ID || row.TokenHash != "hash-one" || row.Label != "Shop floor" || !row.Revoked {
		t.Errorf("SaveInvite wrote %+v, want %s", row, rtJSON(inv))
	}
	rtWantOptionalInstant(t, "SaveInvite expires_at", row.ExpiresAt, inv.ExpiresAt)
	rtWantTimestamp(t, "SaveInvite created_at", row.CreatedAt, inv.CreatedAt)

	bare := &interviews.Invite{ID: uuid.New().String(), InterviewID: iv.ID, TokenHash: "hash-bare", CreatedAt: rtAt(1)}
	if err := w.repo.SaveInvite(bare); err != nil {
		t.Fatal(err)
	}
	if row := rtReadInviteRow(t, w.db, bare.ID); row.ExpiresAt != nil || row.Label != "" || row.Revoked {
		t.Errorf("SaveInvite of a bare invite wrote %+v, want NULL expiry, an empty label and not revoked", row)
	}

	again := *inv
	again.TokenHash = "hash-two"
	err := w.repo.SaveInvite(&again)
	rtWantUnwrappedPQ(t, "SaveInvite of an id an invite has", err, "23505")
	rtWantPQ(t, "SaveInvite of an id an invite has", err, "23505", "interview_invites_pkey")
	sameHash := &interviews.Invite{ID: uuid.New().String(), InterviewID: iv.ID, TokenHash: "hash-one", CreatedAt: rtAt(1)}
	err = w.repo.SaveInvite(sameHash)
	rtWantUnwrappedPQ(t, "SaveInvite of a hash an invite has", err, "23505")
	rtWantPQ(t, "SaveInvite of a hash an invite has", err, "23505", "idx_interview_invites_token")
	noInterview := &interviews.Invite{ID: uuid.New().String(), InterviewID: uuid.New().String(), TokenHash: "hash-three", CreatedAt: rtAt(1)}
	rtWantUnwrappedPQ(t, "SaveInvite of an interview no row has", w.repo.SaveInvite(noInterview), "23503")
	malformed := &interviews.Invite{ID: uuid.New().String(), InterviewID: "not-a-uuid", TokenHash: "hash-four", CreatedAt: rtAt(1)}
	rtWantRefused(t, "SaveInvite of a malformed interview", w.repo.SaveInvite(malformed))
	for _, id := range []string{sameHash.ID, noInterview.ID, malformed.ID} {
		if rtReadInviteRow(t, w.db, id) != nil {
			t.Error("a refused SaveInvite wrote a row")
		}
	}
	if got := rtReadInviteRow(t, w.db, inv.ID); got.TokenHash != "hash-one" {
		t.Errorf("the refused SaveInvite changed the row: %+v", got)
	}

	// RevokeInvite sets the flag and nothing else.
	open := w.invite(iv.ID, "To revoke", 5)
	if err := w.repo.RevokeInvite(open.ID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	revoked := rtReadInviteRow(t, w.db, open.ID)
	if !revoked.Revoked || revoked.TokenHash != open.TokenHash || revoked.Label != "To revoke" || revoked.InterviewID != iv.ID {
		t.Errorf("RevokeInvite left %+v, want the flag set and the rest as it was", revoked)
	}
	if err := w.repo.RevokeInvite(open.ID); err != nil {
		t.Fatalf("RevokeInvite again: %v", err)
	}
	rtWantSame(t, "the invite after a second RevokeInvite", rtReadInviteRow(t, w.db, open.ID), revoked)
	if err := w.repo.RevokeInvite(uuid.New().String()); err != nil {
		t.Errorf("RevokeInvite of an invite no row has: %v, want no error", err)
	}
	rtWantRefused(t, "RevokeInvite of a malformed id", w.repo.RevokeInvite("not-a-uuid"))
}

// SaveSession writes all eight columns as the session holds them (a nil end as
// NULL); the invite is not a foreign key. Saving an id a session has is
// Postgres's refusal as it came, an interview no row has a foreign key's, a
// malformed id the uuid's; the refused writes leave no row. UpdateSession
// rewrites the participant, the status, the summary and the end, and nothing
// else: not the interview, the invite nor started_at, whatever the session it
// is given holds. An empty participant overwrites a name, and a nil end clears
// the column. It is no error for a session no row has; a malformed id is
// Postgres's refusal as it came.
func TestInterviewRepositorySaveAndUpdateSessionWriteTheirColumns(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	invite := uuid.New().String()
	ended := rtAt(70).In(rtCEST)
	s := &interviews.Session{ID: uuid.New().String(), InterviewID: iv.ID, InviteID: invite, ParticipantName: "Ada",
		Status: interviews.SessionStatusCompleted, Summary: "Zeroes the vice first.", StartedAt: rtAt(10).In(rtCEST), EndedAt: &ended}
	if err := w.repo.SaveSession(s); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	row := rtReadSessionRow(t, w.db, s.ID)
	if row == nil {
		t.Fatal("SaveSession wrote no row")
	}
	if row.ID != s.ID || row.InterviewID != iv.ID || row.InviteID != invite || row.Name != "Ada" || row.Status != "completed" ||
		row.Summary != "Zeroes the vice first." {
		t.Errorf("SaveSession wrote %+v, want %s", row, rtJSON(s))
	}
	rtWantTimestamp(t, "SaveSession started_at", row.StartedAt, s.StartedAt)
	rtWantOptionalTimestamp(t, "SaveSession ended_at", row.EndedAt, s.EndedAt)

	bare := w.session(iv.ID, invite, interviews.SessionStatusActive, 1)
	if row := rtReadSessionRow(t, w.db, bare.ID); row.EndedAt != nil || row.Name != "" || row.Summary != "" {
		t.Errorf("SaveSession of a bare session wrote %+v, want NULL end, an empty participant and summary", row)
	}

	again := *s
	again.Summary = "Other"
	err := w.repo.SaveSession(&again)
	rtWantUnwrappedPQ(t, "SaveSession of an id a session has", err, "23505")
	rtWantPQ(t, "SaveSession of an id a session has", err, "23505", "interview_sessions_pkey")
	noInterview := &interviews.Session{ID: uuid.New().String(), InterviewID: uuid.New().String(), InviteID: invite,
		Status: "active", StartedAt: rtAt(1)}
	rtWantUnwrappedPQ(t, "SaveSession of an interview no row has", w.repo.SaveSession(noInterview), "23503")
	malformed := &interviews.Session{ID: uuid.New().String(), InterviewID: iv.ID, InviteID: "not-a-uuid", Status: "active", StartedAt: rtAt(1)}
	rtWantRefused(t, "SaveSession of a malformed invite", w.repo.SaveSession(malformed))
	for _, id := range []string{noInterview.ID, malformed.ID} {
		if rtReadSessionRow(t, w.db, id) != nil {
			t.Error("a refused SaveSession wrote a row")
		}
	}

	// UpdateSession.
	before := rtReadSessionRow(t, w.db, s.ID)
	newEnd := rtAt(90).In(rtCEST)
	sent := &interviews.Session{ID: s.ID, InterviewID: uuid.New().String(), InviteID: uuid.New().String(), ParticipantName: "Grace",
		Status: interviews.SessionStatusActive, Summary: "A new summary", StartedAt: rtAt(-99), EndedAt: &newEnd}
	if err := w.repo.UpdateSession(sent); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	after := rtReadSessionRow(t, w.db, s.ID)
	if after.Name != "Grace" || after.Status != "active" || after.Summary != "A new summary" {
		t.Errorf("UpdateSession wrote %+v, want the participant, status and summary of %s", after, rtJSON(sent))
	}
	rtWantOptionalTimestamp(t, "UpdateSession ended_at", after.EndedAt, sent.EndedAt)
	if after.InterviewID != before.InterviewID || after.InviteID != before.InviteID || !after.StartedAt.Equal(before.StartedAt) {
		t.Errorf("UpdateSession changed the columns it never writes: %+v, was %+v", after, before)
	}

	// An empty participant overwrites the name; a nil end clears the column.
	if err := w.repo.UpdateSession(&interviews.Session{ID: s.ID, Status: interviews.SessionStatusActive}); err != nil {
		t.Fatal(err)
	}
	after = rtReadSessionRow(t, w.db, s.ID)
	if after.Name != "" || after.Summary != "" || after.EndedAt != nil {
		t.Errorf("UpdateSession of empty fields wrote %+v, want an empty participant and summary and a NULL end", after)
	}

	missing := &interviews.Session{ID: uuid.New().String(), Status: "active"}
	if err := w.repo.UpdateSession(missing); err != nil || rtReadSessionRow(t, w.db, missing.ID) != nil {
		t.Errorf("UpdateSession of a session no row has: %v, row %+v; want no error and no row", err, rtReadSessionRow(t, w.db, missing.ID))
	}
	rtWantRefused(t, "UpdateSession of a malformed id", w.repo.UpdateSession(&interviews.Session{ID: "not-a-uuid", Status: "active"}))
}

// SetParticipantName fills a name only where the stored one is NULL or empty,
// and writes that one column; it is no error for a session no row has (and
// writes none), and a malformed id is Postgres's refusal as it came. (The
// participant column forbids NULL; a test lets it take one to show the guard
// reads it as blank. The completion fields are in interview_repository_test.go.)
func TestInterviewRepositorySetParticipantNameGuard(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	blank := w.session(iv.ID, uuid.New().String(), interviews.SessionStatusActive, 1)
	named := &interviews.Session{ID: uuid.New().String(), InterviewID: iv.ID, InviteID: uuid.New().String(), ParticipantName: "Grace",
		Status: interviews.SessionStatusActive, StartedAt: rtAt(2)}
	if err := w.repo.SaveSession(named); err != nil {
		t.Fatal(err)
	}

	if err := w.repo.SetParticipantName(blank.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := w.repo.SetParticipantName(named.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	if got := rtReadSessionRow(t, w.db, blank.ID); got.Name != "Ada" || got.Status != "active" || got.EndedAt != nil {
		t.Errorf("SetParticipantName of a blank name left %+v, want Ada alone changed", got)
	}
	if got := rtReadSessionRow(t, w.db, named.ID); got.Name != "Grace" {
		t.Errorf("SetParticipantName of a named session left %+v, want Grace", got)
	}

	rtSeed(t, w.db, `ALTER TABLE interview_sessions ALTER COLUMN participant_name DROP NOT NULL`)
	rtSeed(t, w.db, `UPDATE interview_sessions SET participant_name = NULL WHERE id = $1`, named.ID)
	if err := w.repo.SetParticipantName(named.ID, "Hopper"); err != nil {
		t.Fatal(err)
	}
	if got := rtReadSessionRow(t, w.db, named.ID); got.Name != "Hopper" {
		t.Errorf("SetParticipantName of a NULL name left %+v, want Hopper", got)
	}

	if err := w.repo.SetParticipantName(uuid.New().String(), "Nobody"); err != nil {
		t.Errorf("SetParticipantName of a session no row has: %v, want no error", err)
	}
	rtWantRefused(t, "SetParticipantName of a malformed id", w.repo.SetParticipantName("not-a-uuid", "Nobody"))
}

// SaveMessage writes all five columns as the message holds them. Saving an id a
// message has is Postgres's refusal as it came, a session no row has a foreign
// key's, a malformed id the uuid's; the refused writes leave no row.
func TestInterviewRepositorySaveMessageWritesEveryColumn(t *testing.T) {
	w := newRTIVWorld(t)
	iv := w.interview("", "Machinists", 0)
	session := w.session(iv.ID, uuid.New().String(), interviews.SessionStatusActive, 1)
	m := &interviews.Message{ID: uuid.New().String(), SessionID: session.ID, Role: interviews.RoleParticipant,
		Content: "I zero the vice first.", CreatedAt: rtAt(10).In(rtCEST)}
	if err := w.repo.SaveMessage(m); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	rtWantMessage(t, "SaveMessage", rtReadMessageRow(t, w.db, m.ID), m)

	again := *m
	again.Content = "Other"
	err := w.repo.SaveMessage(&again)
	rtWantUnwrappedPQ(t, "SaveMessage of an id a message has", err, "23505")
	rtWantPQ(t, "SaveMessage of an id a message has", err, "23505", "interview_messages_pkey")
	noSession := &interviews.Message{ID: uuid.New().String(), SessionID: uuid.New().String(), Role: "system", Content: "x", CreatedAt: rtAt(1)}
	rtWantUnwrappedPQ(t, "SaveMessage of a session no row has", w.repo.SaveMessage(noSession), "23503")
	malformed := &interviews.Message{ID: uuid.New().String(), SessionID: "not-a-uuid", Role: "system", Content: "x", CreatedAt: rtAt(1)}
	rtWantRefused(t, "SaveMessage of a malformed session", w.repo.SaveMessage(malformed))
	for _, id := range []string{noSession.ID, malformed.ID} {
		if rtReadMessageRow(t, w.db, id) != nil {
			t.Error("a refused SaveMessage wrote a row")
		}
	}
	if got := rtReadMessageRow(t, w.db, m.ID); got.Content != "I zero the vice first." {
		t.Errorf("the refused SaveMessage changed the row: %+v", got)
	}
}

// On a database that fails, every method hands the driver's error back as it
// came, unwrapped: the reads answer no row and no list.
func TestInterviewRepositoryOnAFailingDatabase(t *testing.T) {
	repo := NewInterviewRepository(rtClosedDB(t))
	id := uuid.New().String()

	for name, read := range map[string]func() (interface{}, error){
		"FindInterviewByID":         func() (interface{}, error) { return repo.FindInterviewByID(id) },
		"FindInviteByID":            func() (interface{}, error) { return repo.FindInviteByID(id) },
		"FindInviteByTokenHash":     func() (interface{}, error) { return repo.FindInviteByTokenHash(id) },
		"FindSessionByID":           func() (interface{}, error) { return repo.FindSessionByID(id) },
		"FindActiveSessionByInvite": func() (interface{}, error) { return repo.FindActiveSessionByInvite(id) },
		"FindLatestSessionByInvite": func() (interface{}, error) { return repo.FindLatestSessionByInvite(id) },
		"ListInterviewsByProject":   func() (interface{}, error) { return repo.ListInterviewsByProject(id) },
		"ListInvitesByInterview":    func() (interface{}, error) { return repo.ListInvitesByInterview(id) },
		"ListSessionsByInterview":   func() (interface{}, error) { return repo.ListSessionsByInterview(id) },
		"ListSessionsByProject":     func() (interface{}, error) { return repo.ListSessionsByProject(id, 10) },
		"ListMessagesBySession":     func() (interface{}, error) { return repo.ListMessagesBySession(id) },
	} {
		got, err := read()
		rtWantClosed(t, name, err, "")
		if rv := fmt.Sprintf("%#v", got); !strings.Contains(rv, "(nil)") {
			t.Errorf("%s on a failing database answered %s, want nil", name, rv)
		}
	}
	rtWantClosed(t, "SaveInterview", repo.SaveInterview(&interviews.Interview{ID: id, ProjectID: id}), "")
	rtWantClosed(t, "UpdateInterview", repo.UpdateInterview(&interviews.Interview{ID: id}), "")
	rtWantClosed(t, "SaveInvite", repo.SaveInvite(&interviews.Invite{ID: id, InterviewID: id}), "")
	rtWantClosed(t, "RevokeInvite", repo.RevokeInvite(id), "")
	rtWantClosed(t, "SaveSession", repo.SaveSession(&interviews.Session{ID: id, InterviewID: id, InviteID: id}), "")
	rtWantClosed(t, "UpdateSession", repo.UpdateSession(&interviews.Session{ID: id}), "")
	rtWantClosed(t, "SetParticipantName", repo.SetParticipantName(id, "Ada"), "")
	rtWantClosed(t, "SaveMessage", repo.SaveMessage(&interviews.Message{ID: id, SessionID: id}), "")
}
