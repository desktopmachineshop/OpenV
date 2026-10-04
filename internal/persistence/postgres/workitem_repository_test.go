package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// TestWorkItemsListInTheBoardsFlowOrder pins OpenV REQ-23's board order: a
// project's work items list column by column in the order the board flows
// in (backlog, todo, in-progress, review, done), then by sort_order, then
// by created_at. The list used to sort board_column as text, so the columns
// came alphabetically: backlog, done, in-progress, review, todo. A column
// the board does not have, which no write stores, would list last.
func TestWorkItemsListInTheBoardsFlowOrder(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewWorkItemRepository(db)

	project, other := uuid.New().String(), uuid.New().String()
	seedProjects(t, db, project, other)
	start := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)
	// Saved out of order: the list's order is the query's, not the insert's.
	for i, c := range []struct {
		project, title, column string
		sortOrder              int
	}{
		{project, "done", workitems.ColumnDone, 0},
		{project, "retired", "archived", 0},
		{project, "review", workitems.ColumnReview, 0},
		{project, "todo 1", workitems.ColumnTodo, 1},
		{project, "todo 0", workitems.ColumnTodo, 0},
		{project, "in-progress", workitems.ColumnInProgress, 0},
		{other, "another project's", workitems.ColumnBacklog, 0},
		{project, "backlog, later", workitems.ColumnBacklog, 0},
		{project, "backlog, sooner", workitems.ColumnBacklog, 0},
	} {
		at := start.Add(time.Duration(i) * time.Minute)
		if strings.HasSuffix(c.title, "sooner") {
			at = start.Add(-time.Minute)
		}
		if err := repo.Save(&workitems.WorkItem{
			ID: uuid.New().String(), ProjectID: c.project, Title: c.title, Column: c.column,
			SortOrder: c.sortOrder, AssigneeType: workitems.AssigneeUser, ArtifactIDs: []string{},
			CreatedAt: at, UpdatedAt: at,
		}); err != nil {
			t.Fatalf("save %q: %v", c.title, err)
		}
	}

	items, err := repo.ListByProject(project)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	var got []string
	for _, item := range items {
		got = append(got, item.Title)
	}
	want := []string{"backlog, sooner", "backlog, later", "todo 0", "todo 1", "in-progress", "review", "done", "retired"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("listed\n  %s\nwant\n  %s", strings.Join(got, " | "), strings.Join(want, " | "))
	}
}
