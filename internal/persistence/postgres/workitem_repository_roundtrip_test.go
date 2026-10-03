package postgres

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// The work item repository's round trip (refactor plan S15b, OpenV REQ-23):
// a board card and its activity, pinned as found. The board's column order
// is TestWorkItemsListInTheBoardsFlowOrder's (workitem_repository_test.go);
// this file pins the rest. The helpers are in
// repository_roundtrip_helpers_test.go.

func rtItemSansTimes(item *workitems.WorkItem) workitems.WorkItem {
	c := *item
	c.CreatedAt, c.UpdatedAt, c.DueDate = time.Time{}, time.Time{}, nil
	return c
}

func rtItemTitles(list []*workitems.WorkItem) []string {
	var titles []string
	for _, item := range list {
		titles = append(titles, item.Title)
	}
	return titles
}

// A work item reads back every field as saved: created_at and updated_at as
// TIMESTAMP columns keep them (the wall clock, to the microsecond, in
// lib/pq's zone at offset 0), due_date as a TIMESTAMPTZ keeps the instant
// and answers in time.UTC, and no artifacts are stored as [] and read back
// as an empty list. Update rewrites title, description, column, sort order,
// assignee, run, artifacts, due date and updated_at, never the project, the
// note it was raised from, its author or created_at, and an id no row has is
// no error. An item no row has, and a malformed id, is
// workitems.ErrNotFound from FindByID, where a malformed id in Update or
// Delete is Postgres's refusal. Nothing checks the project: an item for a
// project no row has is stored. Delete takes the item's activity.
func TestWorkItemRepositoryRoundTrip(t *testing.T) {
	db := rtDB(t)
	repo := NewWorkItemRepository(db)
	projectID, assignee, run, note, author := uuid.New().String(), uuid.New().String(), uuid.New().String(),
		uuid.New().String(), uuid.New().String()
	due := time.Date(2026, 4, 1, 17, 30, 0, 987654321, rtCEST)

	saved := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: projectID, Title: "Trace REQ-23",
		Description: "Link the handoff tests", Column: workitems.ColumnTodo, SortOrder: 3,
		AssigneeType: workitems.AssigneeAgent, AssigneeID: &assignee, AgentRunID: &run,
		ArtifactIDs: []string{"b-artifact", "a-artifact"}, DueDate: &due, SourceChatterID: &note, CreatedBy: &author,
		CreatedAt: rtAt(0), UpdatedAt: rtAt(1).In(rtCEST)}
	if err := repo.Save(saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(saved.ID)
	if err != nil || got == nil {
		t.Fatalf("FindByID: %v, %v", got, err)
	}
	rtWantTimestamp(t, "a work item's created_at", got.CreatedAt, saved.CreatedAt)
	rtWantTimestamp(t, "a work item's updated_at, sent at +02:00", got.UpdatedAt, saved.UpdatedAt)
	rtWantTimestamptz(t, "a work item's due_date, sent at +02:00", got.DueDate, due)
	rtWantSame(t, "a work item", rtItemSansTimes(got), rtItemSansTimes(saved))

	bare := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: projectID, Title: "Bare",
		Column: workitems.ColumnBacklog, AssigneeType: workitems.AssigneeUser, CreatedAt: rtAt(2), UpdatedAt: rtAt(2)}
	if err := repo.Save(bare); err != nil {
		t.Fatalf("Save with no artifacts: %v", err)
	}
	var stored string
	if err := db.QueryRow(`SELECT artifact_ids::text FROM work_items WHERE id = $1`, bare.ID).Scan(&stored); err != nil || stored != "[]" {
		t.Errorf("no artifacts stored as %q (%v), want []", stored, err)
	}
	if got, err := repo.FindByID(bare.ID); err != nil || got == nil {
		t.Errorf("FindByID(bare): %v, %v", got, err)
	} else {
		want := rtItemSansTimes(bare)
		want.ArtifactIDs = []string{}
		rtWantSame(t, "a work item with nothing optional", rtItemSansTimes(got), want)
		if got.DueDate != nil {
			t.Errorf("no due date read back as %v, want nil", got.DueDate)
		}
	}

	t.Run("update", func(t *testing.T) {
		newRun := uuid.New().String()
		changed := *got
		changed.ProjectID, changed.SourceChatterID, changed.CreatedBy = uuid.New().String(), nil, nil // not written
		changed.Title, changed.Description, changed.Column, changed.SortOrder = "Traced", "", workitems.ColumnDone, 0
		changed.AssigneeType, changed.AssigneeID, changed.AgentRunID = workitems.AssigneeTeam, nil, &newRun
		changed.ArtifactIDs, changed.DueDate = nil, nil
		changed.CreatedAt, changed.UpdatedAt = rtAt(50), rtAt(60) // created_at is not written either
		if err := repo.Update(&changed); err != nil {
			t.Fatalf("Update: %v", err)
		}
		after, err := repo.FindByID(saved.ID)
		if err != nil || after == nil {
			t.Fatalf("FindByID after update: %v, %v", after, err)
		}
		rtWantTimestamp(t, "created_at after an update", after.CreatedAt, saved.CreatedAt)
		rtWantTimestamp(t, "updated_at after an update", after.UpdatedAt, rtAt(60))
		if after.DueDate != nil {
			t.Errorf("a due date updated to none read back as %v, want nil", after.DueDate)
		}
		want := rtItemSansTimes(&changed)
		want.ProjectID, want.SourceChatterID, want.CreatedBy, want.ArtifactIDs = projectID, &note, &author, []string{}
		rtWantSame(t, "an updated work item", rtItemSansTimes(after), want)

		if err := repo.Update(&workitems.WorkItem{ID: uuid.New().String(), Title: "Ghost"}); err != nil {
			t.Errorf("Update of an item no row has: %v, want no error", err)
		}
		rtWantRefused(t, "Update of a malformed id", repo.Update(&workitems.WorkItem{ID: malformed, Title: "x"}))
	})

	t.Run("saves", func(t *testing.T) {
		rtWantPQ(t, "Save of an id an item has", repo.Save(&workitems.WorkItem{ID: saved.ID, ProjectID: projectID,
			Title: "Again", Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser}), "23505", "work_items_pkey")
		rtWantRefused(t, "Save for a malformed project id", repo.Save(&workitems.WorkItem{ID: uuid.New().String(),
			ProjectID: malformed, Title: "x", Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser}))
		orphan := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: uuid.New().String(), Title: "Orphan",
			Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.Save(orphan); err != nil {
			t.Errorf("Save for a project no row has: %v, want it stored", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		for _, id := range append([]string{uuid.New().String()}, malformedIDs...) {
			if found, err := repo.FindByID(id); found != nil || err != workitems.ErrNotFound {
				t.Errorf("FindByID(%q) = %v, %v; want nil, workitems.ErrNotFound", id, found, err)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := repo.SaveActivity(&workitems.Activity{ID: uuid.New().String(), WorkItemID: saved.ID,
			Kind: workitems.KindComment, Actor: "user:dana", CreatedAt: rtAt(3)}); err != nil {
			t.Fatal(err)
		}
		if err := repo.Delete(saved.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if found, err := repo.FindByID(saved.ID); found != nil || err != workitems.ErrNotFound {
			t.Errorf("FindByID after Delete: %v, %v; want workitems.ErrNotFound", found, err)
		}
		var left int
		if err := db.QueryRow(`SELECT COUNT(*) FROM work_item_activity WHERE work_item_id = $1`, saved.ID).Scan(&left); err != nil || left != 0 {
			t.Errorf("a deleted item's activity: %d rows (%v), want none", left, err)
		}
		if err := repo.Delete(saved.ID); err != nil {
			t.Errorf("Delete of an item no row has: %v, want no error", err)
		}
		rtWantRefused(t, "Delete of a malformed id", repo.Delete(malformed))
		if found, err := repo.FindByID(bare.ID); found == nil || err != nil {
			t.Errorf("Delete removed another item: %v, %v", found, err)
		}
	})
}

// ListByProject breaks a tie on column and sort order by created_at, and
// items that tie on all three come in no set order; a project with none, or
// no row, lists nil, and a malformed project id is Postgres's refusal.
// ListBySourceChatterIDs has no ORDER BY at all: it lists every project's
// items raised from the notes, in no set order; no notes is nil with no
// query, no match is nil, and one malformed note id among good ones refuses
// the whole list. MaxSortOrder is a column's highest sort order, -1 for an
// empty column or one the board does not have, and 0 with Postgres's
// refusal for a malformed project id.
func TestWorkItemRepositoryLists(t *testing.T) {
	db := rtDB(t)
	repo := NewWorkItemRepository(db)
	project, other := uuid.New().String(), uuid.New().String()
	noteA, noteB, noteC := uuid.New().String(), uuid.New().String(), uuid.New().String()

	for _, c := range []struct {
		project, title, column string
		sortOrder, at          int
		note                   *string
	}{
		{project, "backlog 1", workitems.ColumnBacklog, 1, 0, nil},
		{project, "backlog 0, later", workitems.ColumnBacklog, 0, 5, &noteA},
		{project, "tie a", workitems.ColumnDone, 4, 3, nil},
		{project, "backlog 0, sooner", workitems.ColumnBacklog, 0, 2, &noteB},
		{project, "todo -2", workitems.ColumnTodo, -2, 1, &noteA},
		{project, "tie b", workitems.ColumnDone, 4, 3, nil},
		{other, "another project's", workitems.ColumnTodo, 7, 0, &noteB},
	} {
		if err := repo.Save(&workitems.WorkItem{ID: uuid.New().String(), ProjectID: c.project, Title: c.title,
			Column: c.column, SortOrder: c.sortOrder, AssigneeType: workitems.AssigneeUser, SourceChatterID: c.note,
			CreatedAt: rtAt(c.at), UpdatedAt: rtAt(c.at)}); err != nil {
			t.Fatalf("Save %q: %v", c.title, err)
		}
	}

	t.Run("by project", func(t *testing.T) {
		items, err := repo.ListByProject(project)
		if err != nil {
			t.Fatalf("ListByProject: %v", err)
		}
		rtWantOrder(t, "a project's items", rtItemTitles(items), []string{"backlog 0, sooner"}, []string{"backlog 0, later"},
			[]string{"backlog 1"}, []string{"todo -2"}, []string{"tie a", "tie b"})

		list, err := repo.ListByProject(uuid.New().String())
		rtWantNil(t, "the items of a project with none", list, err)
		list, err = repo.ListByProject(malformed)
		if list != nil {
			t.Errorf("ListByProject of a malformed id listed %v", list)
		}
		rtWantRefused(t, "ListByProject of a malformed id", err)
	})

	t.Run("by note", func(t *testing.T) {
		items, err := repo.ListBySourceChatterIDs([]string{noteB, noteA, uuid.New().String()})
		if err != nil {
			t.Fatalf("ListBySourceChatterIDs: %v", err)
		}
		rtWantOrder(t, "the items raised from two notes", rtItemTitles(items),
			[]string{"backlog 0, later", "backlog 0, sooner", "todo -2", "another project's"})

		list, err := repo.ListBySourceChatterIDs(nil)
		rtWantNil(t, "the items of no notes", list, err)
		list, err = repo.ListBySourceChatterIDs([]string{})
		rtWantNil(t, "the items of an empty list of notes", list, err)
		list, err = repo.ListBySourceChatterIDs([]string{noteC})
		rtWantNil(t, "the items of a note with none", list, err)
		list, err = repo.ListBySourceChatterIDs([]string{noteA, malformed})
		if list != nil {
			t.Errorf("ListBySourceChatterIDs with a malformed id listed %v", list)
		}
		rtWantRefused(t, "ListBySourceChatterIDs with a malformed id", err)
	})

	t.Run("max sort order", func(t *testing.T) {
		for _, c := range []struct {
			project, column string
			want            int
		}{
			{project, workitems.ColumnBacklog, 1},
			{project, workitems.ColumnTodo, -2},
			{project, workitems.ColumnDone, 4},
			{project, workitems.ColumnReview, -1},
			{project, "archived", -1},
			{other, workitems.ColumnTodo, 7},
			{uuid.New().String(), workitems.ColumnTodo, -1},
		} {
			if got, err := repo.MaxSortOrder(c.project, c.column); got != c.want || err != nil {
				t.Errorf("MaxSortOrder(%s, %q) = %d, %v; want %d", c.project, c.column, got, err, c.want)
			}
		}
		got, err := repo.MaxSortOrder(malformed, workitems.ColumnTodo)
		if got != 0 {
			t.Errorf("MaxSortOrder of a malformed project id = %d, want 0", got)
		}
		rtWantRefused(t, "MaxSortOrder of a malformed project id", err)
	})
}

// An activity entry reads back as saved, a missing payload stored as {} and
// read back as an empty map. The feed lists by created_at alone, oldest
// first (a tie in no set order); an item with none, or no row, lists nil,
// and a malformed id is Postgres's refusal. An entry for an item no row has
// is the foreign key's refusal.
func TestWorkItemRepositoryActivity(t *testing.T) {
	db := rtDB(t)
	repo := NewWorkItemRepository(db)
	item := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: uuid.New().String(), Title: "Card",
		Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := repo.Save(item); err != nil {
		t.Fatal(err)
	}

	moved := &workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID, Kind: workitems.KindMoved,
		Actor: "user:dana", Content: "Moved to done",
		Payload:   map[string]interface{}{"from": "todo", "to": "done", "position": 2.0, "flags": []interface{}{"fast"}},
		CreatedAt: rtAt(4).In(rtCEST)}
	comment := &workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID, Kind: workitems.KindComment,
		Actor: "agent:run-1", Content: "Looks right", CreatedAt: rtAt(1)}
	tieA := &workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID, Kind: workitems.KindAssigned,
		Actor: "system", Content: "tie a", Payload: map[string]interface{}{}, CreatedAt: rtAt(2)}
	tieB := &workitems.Activity{ID: uuid.New().String(), WorkItemID: item.ID, Kind: workitems.KindRunStarted,
		Actor: "system", Content: "tie b", Payload: map[string]interface{}{}, CreatedAt: rtAt(2)}
	for _, a := range []*workitems.Activity{moved, tieB, comment, tieA} {
		if err := repo.SaveActivity(a); err != nil {
			t.Fatalf("SaveActivity %q: %v", a.Content, err)
		}
	}

	feed, err := repo.ListActivity(item.ID)
	if err != nil {
		t.Fatalf("ListActivity: %v", err)
	}
	var contents []string
	byID := map[string]*workitems.Activity{}
	for _, a := range feed {
		contents = append(contents, a.Content)
		byID[a.ID] = a
	}
	// The move was stamped last but one, at +02:00: its wall clock, two
	// hours on, is what it sorts by.
	rtWantOrder(t, "an item's activity", contents, []string{"Looks right"}, []string{"tie a", "tie b"}, []string{"Moved to done"})
	for _, sent := range []*workitems.Activity{moved, comment} {
		got := byID[sent.ID]
		if got == nil {
			continue
		}
		rtWantTimestamp(t, "an activity entry's created_at", got.CreatedAt, sent.CreatedAt)
		gotCopy, want := *got, *sent
		gotCopy.CreatedAt, want.CreatedAt = time.Time{}, time.Time{}
		if want.Payload == nil {
			want.Payload = map[string]interface{}{}
		}
		rtWantSame(t, "an activity entry", gotCopy, want)
	}

	other := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: item.ProjectID, Title: "Quiet",
		Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
	if err := repo.Save(other); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListActivity(other.ID)
	rtWantNil(t, "the activity of an item with none", list, err)
	list, err = repo.ListActivity(uuid.New().String())
	rtWantNil(t, "the activity of an item no row has", list, err)
	list, err = repo.ListActivity(malformed)
	if list != nil {
		t.Errorf("ListActivity of a malformed id listed %v", list)
	}
	rtWantRefused(t, "ListActivity of a malformed id", err)

	rtWantPQ(t, "SaveActivity for an item no row has", repo.SaveActivity(&workitems.Activity{ID: uuid.New().String(),
		WorkItemID: uuid.New().String(), Kind: workitems.KindComment, Actor: "system"}), "23503", "work_item_activity_work_item_id_fkey")
	rtWantPQ(t, "SaveActivity of an id an entry has", repo.SaveActivity(&workitems.Activity{ID: moved.ID,
		WorkItemID: item.ID, Kind: workitems.KindComment, Actor: "system"}), "23505", "work_item_activity_pkey")
}

// What a work item's artifacts and an activity entry's payload read back as
// when the JSON stored is not what the repository writes: JSON null reads as
// a nil list or map (null in the API), and JSON of another shape fails the
// read with the decoder's error, a single item's and the whole list's alike,
// where the team and agent repositories fall back to an empty value.
func TestWorkItemRepositoryReadsJSONOfAnotherShape(t *testing.T) {
	db := rtDB(t)
	repo := NewWorkItemRepository(db)
	projectID := uuid.New().String()
	newItem := func(title string) *workitems.WorkItem {
		item := &workitems.WorkItem{ID: uuid.New().String(), ProjectID: projectID, Title: title,
			Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser, CreatedAt: rtAt(0), UpdatedAt: rtAt(0)}
		if err := repo.Save(item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	nullItem, objectItem := newItem("null artifacts"), newItem("object artifacts")
	nullEntry := &workitems.Activity{ID: uuid.New().String(), WorkItemID: nullItem.ID, Kind: workitems.KindComment,
		Actor: "system", CreatedAt: rtAt(0)}
	arrayEntry := &workitems.Activity{ID: uuid.New().String(), WorkItemID: objectItem.ID, Kind: workitems.KindComment,
		Actor: "system", CreatedAt: rtAt(0)}
	for _, a := range []*workitems.Activity{nullEntry, arrayEntry} {
		if err := repo.SaveActivity(a); err != nil {
			t.Fatal(err)
		}
	}
	rtSeed(t, db, `UPDATE work_items SET artifact_ids = 'null' WHERE id = $1`, nullItem.ID)
	rtSeed(t, db, `UPDATE work_items SET artifact_ids = '{"id": "x"}' WHERE id = $1`, objectItem.ID)
	rtSeed(t, db, `UPDATE work_item_activity SET payload = 'null' WHERE id = $1`, nullEntry.ID)
	rtSeed(t, db, `UPDATE work_item_activity SET payload = '[1]' WHERE id = $1`, arrayEntry.ID)

	if got, err := repo.FindByID(nullItem.ID); err != nil || got == nil || got.ArtifactIDs != nil {
		t.Errorf("artifacts stored as JSON null: %v, %v; want a nil list", got, err)
	}
	var typeErr *json.UnmarshalTypeError
	if got, err := repo.FindByID(objectItem.ID); got != nil || !errors.As(err, &typeErr) {
		t.Errorf("artifacts stored as an object: %v, %v; want the decoder's type error", got, err)
	}
	if list, err := repo.ListByProject(projectID); list != nil || !errors.As(err, &typeErr) {
		t.Errorf("a project with an item whose artifacts are an object: %v, %v; want no list and the decoder's type error", list, err)
	}
	if list, err := repo.ListActivity(nullItem.ID); err != nil || len(list) != 1 || list[0].Payload != nil {
		t.Errorf("a payload stored as JSON null: %v, %v; want a nil map", list, err)
	}
	if list, err := repo.ListActivity(objectItem.ID); list != nil || !errors.As(err, &typeErr) {
		t.Errorf("a payload stored as an array: %v, %v; want no list and the decoder's type error", list, err)
	}
}

// A failure that is not the id's, here a database that fails every
// statement, is handed back as it came by every method: FindByID answers no
// item with that error, never workitems.ErrNotFound; a list answers nil;
// MaxSortOrder answers 0. ListBySourceChatterIDs of no notes runs no
// statement, so it answers nil and no error even then. A payload JSON
// cannot encode fails SaveActivity with the encoder's error before any
// statement runs.
func TestWorkItemRepositoryHandsBackAFailure(t *testing.T) {
	repo := NewWorkItemRepository(rtClosedDB(t))
	id := uuid.New().String()
	item := &workitems.WorkItem{ID: id, ProjectID: id, Title: "x", Column: workitems.ColumnTodo, AssigneeType: workitems.AssigneeUser}
	rtWantClosed(t, "Save", repo.Save(item), "")
	rtWantClosed(t, "Update", repo.Update(item), "")
	rtWantClosed(t, "Delete", repo.Delete(id), "")
	rtWantClosed(t, "SaveActivity", repo.SaveActivity(&workitems.Activity{ID: id, WorkItemID: id, Kind: workitems.KindComment}), "")

	found, err := repo.FindByID(id)
	if found != nil {
		t.Errorf("FindByID on a failing database read %v", found)
	}
	rtWantClosed(t, "FindByID", err, "")
	items, err := repo.ListByProject(id)
	if items != nil {
		t.Errorf("ListByProject on a failing database listed %v", items)
	}
	rtWantClosed(t, "ListByProject", err, "")
	items, err = repo.ListBySourceChatterIDs([]string{id})
	if items != nil {
		t.Errorf("ListBySourceChatterIDs on a failing database listed %v", items)
	}
	rtWantClosed(t, "ListBySourceChatterIDs", err, "")
	items, err = repo.ListBySourceChatterIDs(nil)
	rtWantNil(t, "ListBySourceChatterIDs of no notes on a failing database", items, err)
	activity, err := repo.ListActivity(id)
	if activity != nil {
		t.Errorf("ListActivity on a failing database listed %v", activity)
	}
	rtWantClosed(t, "ListActivity", err, "")
	highest, err := repo.MaxSortOrder(id, workitems.ColumnTodo)
	if highest != 0 {
		t.Errorf("MaxSortOrder on a failing database = %d, want 0", highest)
	}
	rtWantClosed(t, "MaxSortOrder", err, "")

	rtWantUnencodable(t, "SaveActivity", repo.SaveActivity(&workitems.Activity{ID: id, WorkItemID: id,
		Kind: workitems.KindComment, Payload: rtUnencodable}))
}
