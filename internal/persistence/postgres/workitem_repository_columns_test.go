package postgres

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// The work item repository's columns and scans (refactor plan X13's
// characterization for workitem_repository.go, in the manner of X13a's),
// pinned as found: every column of a work item read back through each read
// that scans it, the columns that may be NULL, the scan errors that name a
// column's place, what a read answers for a row no one has, and what the
// feed and the artifacts read as when the column holds SQL NULL. The existing
// tests (workitem_repository_roundtrip_test.go and
// workitem_repository_test.go) read a work item back whole only through
// FindByID, and the lists only by title; none pins a column's place in a
// scan or the reads of a NULL column through a list. X13 gives the work item
// a columns const and a scan function in place of the three copies of the
// list, and the activity entry the same in place of its inline scan; these
// tests are what holds the repository still while it does. The repository has
// no transaction (Save, Update, Delete and SaveActivity each send one
// statement), so there is none to pin. The helpers are in
// repository_roundtrip_helpers_test.go.

// rtWIWorld is a fresh database with one project and a repository over it.
type rtWIWorld struct {
	t       *testing.T
	db      *sql.DB
	repo    *WorkItemRepository
	project string
}

func newRTWIWorld(t *testing.T) *rtWIWorld {
	t.Helper()
	db := rtDB(t)
	w := &rtWIWorld{t: t, db: db, repo: NewWorkItemRepository(db), project: uuid.New().String()}
	seedProjects(t, db, w.project)
	return w
}

// save saves the item, failing the test on an error.
func (w *rtWIWorld) save(item *workitems.WorkItem) *workitems.WorkItem {
	w.t.Helper()
	if err := w.repo.Save(item); err != nil {
		w.t.Fatalf("Save %q: %v", item.Title, err)
	}
	return item
}

// bare saves an item of the project (project "" is the world's) with nothing
// optional, in the column, stamped at the minute.
func (w *rtWIWorld) bare(project, title, column string, minute int) *workitems.WorkItem {
	w.t.Helper()
	if project == "" {
		project = w.project
	}
	seedProjects(w.t, w.db, project)
	return w.save(&workitems.WorkItem{ID: uuid.New().String(), ProjectID: project, Title: title, Column: column,
		AssigneeType: workitems.AssigneeUser, CreatedAt: rtAt(minute), UpdatedAt: rtAt(minute)})
}

// rtWantWorkItem fails unless got is sent as a work item reads back: every
// field but the times equal (no artifacts reading back as an empty list), the
// two stamps the TIMESTAMP columns' wall clock of the times sent, and the due
// date, where there is one, the instant sent in time.UTC.
func rtWantWorkItem(t *testing.T, what string, got, sent *workitems.WorkItem) {
	t.Helper()
	if got == nil {
		t.Errorf("%s read back nothing, want %s", what, rtJSON(sent))
		return
	}
	want := rtItemSansTimes(sent)
	if want.ArtifactIDs == nil {
		want.ArtifactIDs = []string{}
	}
	rtWantSame(t, what, rtItemSansTimes(got), want)
	rtWantTimestamp(t, what+" created_at", got.CreatedAt, sent.CreatedAt)
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
	switch {
	case sent.DueDate == nil && got.DueDate != nil:
		t.Errorf("%s due_date read back as %v, want nil", what, got.DueDate)
	case sent.DueDate != nil:
		rtWantTimestamptz(t, what+" due_date", got.DueDate, *sent.DueDate)
	}
}

// rtWantWorkItems fails unless list holds exactly the items of want, in the
// order given, each read back whole.
func rtWantWorkItems(t *testing.T, what string, list []*workitems.WorkItem, err error, want ...*workitems.WorkItem) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	var got, wantIDs []string
	for _, item := range list {
		got = append(got, item.ID)
	}
	for _, item := range want {
		wantIDs = append(wantIDs, item.ID)
	}
	if strings.Join(got, "|") != strings.Join(wantIDs, "|") {
		t.Errorf("%s listed %q, want %q", what, rtItemTitles(list), rtItemTitles(want))
		return
	}
	for i := range list {
		rtWantWorkItem(t, fmt.Sprintf("%s item %d", what, i), list[i], want[i])
	}
}

// Every column Save writes reads back through FindByID, ListByProject and
// ListBySourceChatterIDs, whole: the project, the title, the description, the
// board column, the sort order, the assignee's type and id, the run, the
// artifacts (in the order saved), the due date (an instant, read back in
// time.UTC whatever zone it was sent in), the note it was raised from, its
// author and the two stamps (the wall clock sent, its offset dropped, to the
// microsecond). Each column holds a value no other does, so a swap of two in a
// scan shows. A second item leaves every optional column NULL (nil pointers,
// an empty description and no artifacts, which read back as an empty list,
// not nil). The project's list answers the board's order and only the
// project's; the notes' list answers oldest created_at first, across
// projects, and leaves out an item raised from no note.
func TestWorkItemRepositoryListsReadBackEveryColumn(t *testing.T) {
	w := newRTWIWorld(t)
	assignee, run, noteA, noteB, author := uuid.New().String(), uuid.New().String(), uuid.New().String(),
		uuid.New().String(), uuid.New().String()
	due := time.Date(2026, 4, 1, 17, 30, 0, 987654321, rtCEST)
	full := w.save(&workitems.WorkItem{ID: uuid.New().String(), ProjectID: w.project, Title: "Trace REQ-23",
		Description: "Link the handoff tests", Column: workitems.ColumnReview, SortOrder: 7,
		AssigneeType: workitems.AssigneeAgent, AssigneeID: &assignee, AgentRunID: &run,
		ArtifactIDs: []string{"b-artifact", "a-artifact", "c-artifact"}, DueDate: &due, SourceChatterID: &noteA, CreatedBy: &author,
		CreatedAt: rtAt(10), UpdatedAt: rtAt(13).In(rtCEST)})
	bare := w.bare("", "Bare", workitems.ColumnBacklog, 20)

	// Another project's, raised from the other note, created sooner than the
	// full item, and with its own assignee type.
	foreignProject, foreignAssignee := uuid.New().String(), uuid.New().String()
	seedProjects(t, w.db, foreignProject)
	foreign := w.save(&workitems.WorkItem{ID: uuid.New().String(), ProjectID: foreignProject, Title: "Elsewhere",
		Description: "Not this board", Column: workitems.ColumnDone, SortOrder: 2, AssigneeType: workitems.AssigneeTeam,
		AssigneeID: &foreignAssignee, ArtifactIDs: []string{"z-artifact"}, SourceChatterID: &noteB,
		CreatedAt: rtAt(5), UpdatedAt: rtAt(6)})

	for name, item := range map[string]*workitems.WorkItem{"full": full, "bare": bare, "foreign": foreign} {
		got, err := w.repo.FindByID(item.ID)
		if err != nil {
			t.Fatalf("FindByID %s: %v", name, err)
		}
		rtWantWorkItem(t, "FindByID "+name, got, item)
	}

	list, err := w.repo.ListByProject(w.project)
	rtWantWorkItems(t, "ListByProject", list, err, bare, full)
	list, err = w.repo.ListByProject(foreignProject)
	rtWantWorkItems(t, "ListByProject of the other project", list, err, foreign)

	list, err = w.repo.ListBySourceChatterIDs([]string{noteA, noteB})
	rtWantWorkItems(t, "ListBySourceChatterIDs of both notes", list, err, foreign, full)
	list, err = w.repo.ListBySourceChatterIDs([]string{noteA})
	rtWantWorkItems(t, "ListBySourceChatterIDs of note A", list, err, full)
	list, err = w.repo.ListBySourceChatterIDs([]string{noteB})
	rtWantWorkItems(t, "ListBySourceChatterIDs of note B", list, err, foreign)
}

// The reads of a row no one has, or an id no row can have: FindByID answers
// no item and workitems.ErrNotFound itself for an id no row has, a malformed
// one and an empty one; every list answers nil (JSON null), never [], with no
// error, for an id nothing matches, a malformed one and an empty one, and so
// do the lists of ids that are all unusable.
func TestWorkItemRepositoryAnswersNoRow(t *testing.T) {
	w := newRTWIWorld(t)
	item := w.bare("", "Card", workitems.ColumnTodo, 0)
	if err := w.repo.SaveActivity(&workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID,
		Kind: workitems.KindComment, Actor: "user:dana", CreatedAt: rtAt(1)}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{uuid.New().String(), malformed, ""} {
		if got, err := w.repo.FindByID(id); got != nil || err != workitems.ErrNotFound {
			t.Errorf("FindByID(%q): %v, %v; want nil and workitems.ErrNotFound", id, got, err)
		}
		list, err := w.repo.ListByProject(id)
		rtWantNil(t, fmt.Sprintf("ListByProject(%q)", id), list, err)
		list, err = w.repo.ListBySourceChatterIDs([]string{id})
		rtWantNil(t, fmt.Sprintf("ListBySourceChatterIDs([%q])", id), list, err)
		activity, err := w.repo.ListActivity(id)
		rtWantNil(t, fmt.Sprintf("ListActivity(%q)", id), activity, err)
	}
	list, err := w.repo.ListBySourceChatterIDs([]string{malformed, ""})
	rtWantNil(t, "ListBySourceChatterIDs of ids no note has", list, err)
}

// rtWIRead is one read of the repository that answers rows: the ids of what it
// answered, nil for no row and for a nil list, and the error as it came.
type rtWIRead struct {
	name string
	run  func() ([]string, error)
}

func rtWIItem(item *workitems.WorkItem, err error) ([]string, error) {
	if item == nil {
		return nil, err
	}
	return []string{item.ID}, err
}

func rtWIItems(items []*workitems.WorkItem, err error) ([]string, error) {
	if items == nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids, err
}

// rtWIScanCase is a column the schema forbids NULL, here one a test has let
// take it, and what that does to each read of the table.
type rtWIScanCase struct {
	column  string
	index   int
	message string
	// misses names the reads that cannot match the row.
	misses []string
}

const (
	rtWIStr  = "converting NULL to string is unsupported"
	rtWIInt  = "converting NULL to int is unsupported"
	rtWITime = "unsupported Scan, storing driver.Value type <nil> into type *time.Time"
)

// rtWIScanErrors drives one table's scan errors: for each case it inserts a
// row whose column is NULL (the others from base, a SQL literal each), runs
// every read and expects the error naming the column's place and no row and
// no list, or, where the read cannot match the row, no error and a list of
// the survivors the read names (the good rows the test seeded beside it).
func rtWIScanErrors(t *testing.T, db *sql.DB, table string, base map[string]string, cases []rtWIScanCase,
	survivors map[string][]string, reads func(id string) []rtWIRead) {
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

// A column of a work item the schema forbids NULL fails the reads that scan
// it, naming the column and its place among the fifteen scanned (id 0,
// project_id 1, title 2, description 3, board_column 4, sort_order 5,
// assignee_type 6, assignee_id 7, agent_run_id 8, artifact_ids 9, due_date 10,
// source_chatter_id 11, created_by 12, created_at 13, updated_at 14),
// unwrapped, with no item and no list (nil, not []). One such row fails the
// whole list it is in, the good item beside it with it: the board does not
// list around it. An item with no project is out of its project's list,
// which then lists the good item alone.
// (The id is the primary key; it takes no NULL, and artifact_ids, scanned as
// bytes, takes one without a scan error: TestWorkItemRepositoryReadsSQLNull.)
func TestWorkItemRepositoryScanErrorsNameTheColumn(t *testing.T) {
	w := newRTWIWorld(t)
	note := uuid.New().String()
	// A good item of the same project and note: the list that meets the bad
	// row has read it already.
	good := w.save(&workitems.WorkItem{ID: uuid.New().String(), ProjectID: w.project, Title: "Good", Column: workitems.ColumnBacklog,
		AssigneeType: workitems.AssigneeUser, SourceChatterID: &note, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)})
	rtWIScanErrors(t, w.db, "work_items", map[string]string{
		"project_id": "'" + w.project + "'", "title": "'Bad'", "description": "'Description'", "board_column": "'todo'",
		"sort_order": "4", "assignee_type": "'agent'", "source_chatter_id": "'" + note + "'",
		"created_at": "NOW()", "updated_at": "NOW()",
	}, []rtWIScanCase{
		{"project_id", 1, rtWIStr, []string{"ListByProject"}},
		{"title", 2, rtWIStr, nil},
		{"description", 3, rtWIStr, nil},
		{"board_column", 4, rtWIStr, nil},
		{"sort_order", 5, rtWIInt, nil},
		{"assignee_type", 6, rtWIStr, nil},
		{"created_at", 13, rtWITime, nil},
		{"updated_at", 14, rtWITime, nil},
	}, map[string][]string{"ListByProject": {good.ID}}, func(id string) []rtWIRead {
		return []rtWIRead{
			{"FindByID", func() ([]string, error) { return rtWIItem(w.repo.FindByID(id)) }},
			{"ListByProject", func() ([]string, error) { return rtWIItems(w.repo.ListByProject(w.project)) }},
			{"ListBySourceChatterIDs", func() ([]string, error) { return rtWIItems(w.repo.ListBySourceChatterIDs([]string{note})) }},
		}
	})
	if got, err := w.repo.FindByID(good.ID); err != nil || got == nil {
		t.Errorf("the good item beside the bad rows: %v, %v; want it read", got, err)
	}
}

// A column of an activity entry the schema forbids NULL fails the feed,
// naming the column and its place among the seven scanned (id 0, work_item_id
// 1, kind 2, actor 3, content 4, payload 5, created_at 6), unwrapped, with no
// list (nil, not []), and the good entry beside it fails with it. An entry
// with no work item is in no item's feed, which then lists the good entry
// alone. (The id is the primary key, and
// payload, scanned as bytes, takes NULL without a scan error:
// TestWorkItemRepositoryReadsSQLNull.)
func TestWorkItemRepositoryActivityScanErrorsNameTheColumn(t *testing.T) {
	w := newRTWIWorld(t)
	item := w.bare("", "Card", workitems.ColumnTodo, 0)
	good := &workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID, Kind: workitems.KindComment,
		Actor: "user:dana", Content: "Good", CreatedAt: rtAt(0)}
	if err := w.repo.SaveActivity(good); err != nil {
		t.Fatal(err)
	}
	rtWIScanErrors(t, w.db, "work_item_activity", map[string]string{
		"work_item_id": "'" + item.ID + "'", "kind": "'comment'", "actor": "'system'", "content": "'Content'", "created_at": "NOW()",
	}, []rtWIScanCase{
		{"work_item_id", 1, rtWIStr, []string{"ListActivity"}},
		{"kind", 2, rtWIStr, nil},
		{"actor", 3, rtWIStr, nil},
		{"content", 4, rtWIStr, nil},
		{"created_at", 6, rtWITime, nil},
	}, map[string][]string{"ListActivity": {good.ID}}, func(string) []rtWIRead {
		return []rtWIRead{{"ListActivity", func() ([]string, error) {
			list, err := w.repo.ListActivity(item.ID)
			if list == nil {
				return nil, err
			}
			ids := make([]string, 0, len(list))
			for _, a := range list {
				ids = append(ids, a.ID)
			}
			return ids, err
		}}}
	})
}

// Artifacts and a payload that are SQL NULL, which the schema forbids and a
// column let take NULL would hold, are not scan errors: both are scanned as
// bytes, so an item's artifacts read as an empty list (not nil), logged once
// for each read of the item as a value of any other shape is, by FindByID and
// by both lists, and an entry's payload reads as an empty map (not nil),
// where a payload stored as JSON null reads as nil.
func TestWorkItemRepositoryReadsSQLNull(t *testing.T) {
	w := newRTWIWorld(t)
	note := uuid.New().String()
	item := w.save(&workitems.WorkItem{ID: uuid.New().String(), ProjectID: w.project, Title: "Card", Column: workitems.ColumnTodo,
		AssigneeType: workitems.AssigneeUser, SourceChatterID: &note, ArtifactIDs: []string{"a-artifact"},
		CreatedAt: rtAt(0), UpdatedAt: rtAt(0)})
	entry := &workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID, Kind: workitems.KindComment, Actor: "system",
		Content: "Entry", Payload: map[string]interface{}{"k": "v"}, CreatedAt: rtAt(1)}
	if err := w.repo.SaveActivity(entry); err != nil {
		t.Fatal(err)
	}
	rtSeed(t, w.db, `ALTER TABLE work_items ALTER COLUMN artifact_ids DROP NOT NULL`)
	rtSeed(t, w.db, `ALTER TABLE work_item_activity ALTER COLUMN payload DROP NOT NULL`)
	rtSeed(t, w.db, `UPDATE work_items SET artifact_ids = NULL WHERE id = $1`, item.ID)
	rtSeed(t, w.db, `UPDATE work_item_activity SET payload = NULL WHERE id = $1`, entry.ID)

	logged := rtCaptureLog(t)
	got, err := w.repo.FindByID(item.ID)
	if err != nil || got == nil || got.ArtifactIDs == nil || len(got.ArtifactIDs) != 0 {
		t.Errorf("FindByID of an item whose artifacts are NULL: %v, %v; want the item, its artifacts an empty list", got, err)
	}
	rtWantLogged(t, "FindByID", logged, map[string]int{item.ID: 1})
	logged.Reset()
	for name, read := range map[string]func() ([]*workitems.WorkItem, error){
		"ListByProject":          func() ([]*workitems.WorkItem, error) { return w.repo.ListByProject(w.project) },
		"ListBySourceChatterIDs": func() ([]*workitems.WorkItem, error) { return w.repo.ListBySourceChatterIDs([]string{note}) },
	} {
		list, err := read()
		if err != nil || len(list) != 1 || list[0].ID != item.ID || list[0].ArtifactIDs == nil || len(list[0].ArtifactIDs) != 0 {
			t.Errorf("%s of an item whose artifacts are NULL: %s, %v; want the item, its artifacts an empty list", name, rtJSON(list), err)
		}
		rtWantLogged(t, name, logged, map[string]int{item.ID: 1})
		logged.Reset()
	}

	feed, err := w.repo.ListActivity(item.ID)
	if err != nil || len(feed) != 1 || feed[0].ID != entry.ID || feed[0].Payload == nil || len(feed[0].Payload) != 0 {
		t.Errorf("ListActivity of an entry whose payload is NULL: %s, %v; want the entry, its payload an empty map", rtJSON(feed), err)
	}
}
