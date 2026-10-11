package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// X13's literal equality for the work item repository: every statement it
// builds from workItemColumns and activityColumns is the literal it replaced,
// byte for byte (spacing, line breaks and tabs included). The statements are
// read off the wire by the recorder in pgkit_test.go, so the test needs no
// server and checks what each method sends, not a copy of it. The repository
// has no transaction: each method sends its one statement and nothing else.
// The statements X13 leaves as they were (Save, Update, Delete and
// MaxSortOrder) are not rebuilt and are not in this file.

// The statements X13 assembles, as the base (ae184f4) wrote them, byte for
// byte.
const (
	// oldWorkItemFindByIDSelect, FindByID
	oldWorkItemFindByIDSelect = `
		SELECT id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, source_chatter_id, created_by, created_at, updated_at
		FROM work_items
		WHERE id = $1
	`
	// oldWorkItemListByProjectSelect, ListByProject
	oldWorkItemListByProjectSelect = `
		SELECT id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, source_chatter_id, created_by, created_at, updated_at
		FROM work_items
		WHERE project_id = $1
		ORDER BY array_position($2::text[], board_column::text) ASC, sort_order ASC, created_at ASC
	`
	// oldWorkItemListBySourceChatterIDsSelect, ListBySourceChatterIDs
	oldWorkItemListBySourceChatterIDsSelect = `
		SELECT id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, source_chatter_id, created_by, created_at, updated_at
		FROM work_items
		WHERE source_chatter_id = ANY($1)
		ORDER BY created_at ASC, id ASC
	`
	// oldWorkItemSaveActivityInsert, SaveActivity
	oldWorkItemSaveActivityInsert = `
		INSERT INTO work_item_activity (id, work_item_id, kind, actor, content, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	// oldWorkItemListActivitySelect, ListActivity
	oldWorkItemListActivitySelect = `
		SELECT id, work_item_id, kind, actor, content, payload, created_at
		FROM work_item_activity
		WHERE work_item_id = $1
		ORDER BY created_at ASC
	`
)

// The lists the base wrote out, on one line each.
const (
	oldWorkItemColumns = `id, project_id, title, description, board_column, sort_order, assignee_type, assignee_id, agent_run_id, artifact_ids, due_date, source_chatter_id, created_by, created_at, updated_at`
	oldActivityColumns = `id, work_item_id, kind, actor, content, payload, created_at`
)

// The columns are the old lists.
func TestWorkItemColumnsAreTheOldLists(t *testing.T) {
	for _, c := range []struct{ name, got, want string }{
		{"workItemColumns", workItemColumns, oldWorkItemColumns},
		{"activityColumns", activityColumns, oldActivityColumns},
	} {
		if c.got != c.want {
			t.Errorf("%s =\n  %q\nwant\n  %q", c.name, c.got, c.want)
		}
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: FindByID no item and workitems.ErrNotFound, and each list nil.
func TestWorkItemReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewWorkItemRepository(db)
	id := uuid.New().String()

	if got, err := repo.FindByID(id); got != nil || err != workitems.ErrNotFound {
		t.Errorf("FindByID with no row: %v, %v; want nil and workitems.ErrNotFound", got, err)
	}
	wantEvents(t, "FindByID", rec.take(), "QUERY "+oldWorkItemFindByIDSelect)

	for _, r := range []struct {
		name  string
		read  func() (interface{}, error)
		query string
	}{
		{"ListByProject", func() (interface{}, error) { return repo.ListByProject(id) }, oldWorkItemListByProjectSelect},
		{"ListBySourceChatterIDs", func() (interface{}, error) { return repo.ListBySourceChatterIDs([]string{id}) }, oldWorkItemListBySourceChatterIDsSelect},
		{"ListActivity", func() (interface{}, error) { return repo.ListActivity(id) }, oldWorkItemListActivitySelect},
	} {
		got, err := r.read()
		if err != nil || !strings.Contains(fmt.Sprintf("%#v", got), "(nil)") {
			t.Errorf("%s with no rows: %#v, %v; want nil and no error", r.name, got, err)
		}
		wantEvents(t, r.name, rec.take(), "QUERY "+r.query)
	}

	// No usable note id sends nothing and answers nil.
	if got, err := repo.ListBySourceChatterIDs([]string{"not-a-uuid"}); got != nil || err != nil {
		t.Errorf("ListBySourceChatterIDs of no usable id: %v, %v; want nil, nil", got, err)
	}
	wantEvents(t, "ListBySourceChatterIDs of no usable id", rec.take())
}

// SaveActivity sends its old literal, as one statement with no transaction
// around it.
func TestWorkItemSaveActivitySendsItsOldLiteral(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewWorkItemRepository(db)
	id := uuid.New().String()

	if err := repo.SaveActivity(&workitems.Activity{ID: id, WorkItemID: id, Kind: workitems.KindComment, Actor: "system"}); err != nil {
		t.Fatalf("SaveActivity: %v", err)
	}
	wantEvents(t, "SaveActivity", rec.take(), "EXEC "+oldWorkItemSaveActivityInsert)
}
