package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/todos"
)

// TestTodoRepositoryRoundTrip pins the to-do repository: every field read
// back as written (the due date in UTC), the project list's order (open
// first, soonest due first, then done, most recently done first), My to-dos
// (assigned to me, and my personal ones, in one workspace only), the notes'
// lookup, not-found, and a deleted project taking its to-dos with it.
func TestTodoRepositoryRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	org, other := uuid.New().String(), uuid.New().String()
	for i, id := range []string{org, other} {
		if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Todo Org', $2)`, id, "todo-org-"+id[:8]+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	me, sam := uuid.New().String(), uuid.New().String()
	for _, u := range []string{me, sam} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Member')`, u, u+"@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	project := uuid.New().String()
	seedProjects(t, db, project)
	repo := NewTodoRepository(db)
	svc := todos.NewDefaultService(repo, nil)

	if list, err := repo.ListByProject(project); err != nil || len(list) != 0 {
		t.Fatalf("empty list: %v %v", list, err)
	}
	if _, err := repo.FindByID(uuid.New().String()); !errors.Is(err, todos.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := repo.FindByID("not-a-uuid"); !errors.Is(err, todos.ErrNotFound) {
		t.Fatalf("malformed id: %v", err)
	}

	soon := time.Date(2027, 1, 2, 0, 0, 0, 0, time.FixedZone("x", 3600))
	later := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	note := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO chatter (id, artifact_id, message) VALUES ($1, $2, 'note')`, note, uuid.New().String()); err != nil {
		t.Fatal(err)
	}
	art := uuid.New().String()
	undated, err := svc.Create(todos.CreateRequest{OrgID: org, ProjectID: &project, Text: "undated", AssigneeID: &sam,
		SourceChatterID: &note, SourceArtifactID: &art}, &me)
	if err != nil {
		t.Fatal(err)
	}
	late, _ := svc.Create(todos.CreateRequest{OrgID: org, ProjectID: &project, Text: "later", DueDate: &later}, &me)
	early, _ := svc.Create(todos.CreateRequest{OrgID: org, ProjectID: &project, Text: "soon", DueDate: &soon, AssigneeID: &me}, &me)
	done, _ := svc.Create(todos.CreateRequest{OrgID: org, ProjectID: &project, Text: "done"}, &me)
	if _, err := svc.SetDone(done.ID, true); err != nil {
		t.Fatal(err)
	}
	personal, _ := svc.Create(todos.CreateRequest{OrgID: org, Text: "mine"}, &me)
	if _, err := svc.Create(todos.CreateRequest{OrgID: other, Text: "elsewhere"}, &me); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByID(undated.ID)
	if err != nil || got.Text != "undated" || got.SourceChatterID == nil || *got.SourceChatterID != note ||
		got.SourceArtifactID == nil || *got.SourceArtifactID != art || *got.AssigneeID != sam || *got.CreatedBy != me || got.OrgID != org {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if got, _ := repo.FindByID(early.ID); got.DueDate == nil || !got.DueDate.Equal(soon) || got.DueDate.Location() != time.UTC {
		t.Fatalf("due date: %+v", got.DueDate)
	}
	if got, _ := repo.FindByID(done.ID); !got.Done || got.DoneAt == nil {
		t.Fatalf("done: %+v", got)
	}

	list, _ := repo.ListByProject(project)
	order := []string{}
	for _, t := range list {
		order = append(order, t.Text)
	}
	if want := []string{"soon", "later", "undated", "done"}; len(order) != 4 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
		t.Fatalf("order = %v, want %v", order, want)
	}

	mine, _ := repo.ListForUser(org, me)
	if len(mine) != 2 || mine[0].ID != early.ID || mine[1].ID != personal.ID {
		t.Fatalf("my to-dos = %+v", mine)
	}
	byNote, _ := repo.ListBySourceChatterIDs([]string{note, "not-a-uuid"})
	if len(byNote) != 1 || byNote[0].ID != undated.ID {
		t.Fatalf("by note = %+v", byNote)
	}

	if err := repo.Update(&todos.Todo{ID: uuid.New().String(), Text: "x"}); !errors.Is(err, todos.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if err := repo.Delete(late.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE id = $1`, project); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.ListByProject(project); len(list) != 0 {
		t.Fatalf("a deleted project's to-dos remain: %+v", list)
	}
	if _, err := repo.FindByID(personal.ID); err != nil {
		t.Fatalf("the personal to-do went with the project: %v", err)
	}
}
