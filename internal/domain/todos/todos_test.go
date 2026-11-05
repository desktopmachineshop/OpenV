package todos

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

type memRepo struct{ rows map[string]*Todo }

func newMemRepo() *memRepo { return &memRepo{rows: map[string]*Todo{}} }

func (m *memRepo) Save(t *Todo) error { c := *t; m.rows[t.ID] = &c; return nil }
func (m *memRepo) Update(t *Todo) error {
	if _, ok := m.rows[t.ID]; !ok {
		return ErrNotFound
	}
	c := *t
	m.rows[t.ID] = &c
	return nil
}
func (m *memRepo) FindByID(id string) (*Todo, error) {
	t, ok := m.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *t
	return &c, nil
}
func (m *memRepo) ListByProject(string) ([]*Todo, error)            { return nil, nil }
func (m *memRepo) ListForUser(string, string) ([]*Todo, error)      { return nil, nil }
func (m *memRepo) ListBySourceChatterIDs([]string) ([]*Todo, error) { return nil, nil }
func (m *memRepo) Delete(id string) error                           { delete(m.rows, id); return nil }

type fakeBoard struct {
	got *workitems.CreateWorkItemRequest
}

func (f *fakeBoard) Create(req workitems.CreateWorkItemRequest, _ *string, _ string) (*workitems.WorkItem, error) {
	f.got = &req
	return &workitems.WorkItem{ID: "wi-1", ProjectID: req.ProjectID, Title: req.Title, Column: req.Column}, nil
}

func ptr(s string) *string { return &s }

func TestNormalizeText(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  error
	}{
		{"  check the wording  ", "check the wording", nil},
		{"   ", "", ErrTextRequired},
		{"line one\nline two", "line one\nline two", nil},
		{"para one\n\npara two", "", ErrParagraphs},
		{"para one\r\n  \r\npara two", "", ErrParagraphs},
		{strings.Repeat("é", MaxTextLength), strings.Repeat("é", MaxTextLength), nil},
		{strings.Repeat("a", MaxTextLength+1), "", ErrTextTooLong},
	}
	for _, c := range cases {
		got, err := NormalizeText(c.in)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("NormalizeText(%q) = %q, %v; want %q, %v", c.in, got, err, c.want, c.err)
		}
	}
}

func TestCreatePersonalTodoIsTheWritersOwn(t *testing.T) {
	svc := NewDefaultService(newMemRepo(), nil)
	todo, err := svc.Create(CreateRequest{OrgID: "org", Text: "ring Sam", AssigneeID: ptr("someone-else")}, ptr("me"))
	if err != nil {
		t.Fatal(err)
	}
	if todo.ProjectID != nil || todo.AssigneeID == nil || *todo.AssigneeID != "me" {
		t.Fatalf("personal to-do = %+v, want project nil and assignee me", todo)
	}
	if todo.Status() != "open" {
		t.Fatalf("status = %q, want open", todo.Status())
	}
}

func TestCreateNeedsAWorkspace(t *testing.T) {
	svc := NewDefaultService(newMemRepo(), nil)
	if _, err := svc.Create(CreateRequest{Text: "x"}, nil); !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("err = %v, want ErrNoWorkspace", err)
	}
}

func TestSetDoneStampsAndClears(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo, nil)
	todo, _ := svc.Create(CreateRequest{OrgID: "org", ProjectID: ptr("p"), Text: "tick me"}, ptr("me"))
	done, err := svc.SetDone(todo.ID, true)
	if err != nil || !done.Done || done.DoneAt == nil || done.Status() != "done" {
		t.Fatalf("SetDone(true) = %+v, %v", done, err)
	}
	open, err := svc.SetDone(todo.ID, false)
	if err != nil || open.Done || open.DoneAt != nil {
		t.Fatalf("SetDone(false) = %+v, %v", open, err)
	}
}

func TestPromoteMakesABoardTaskAndTicksTheTodoOff(t *testing.T) {
	repo := newMemRepo()
	board := &fakeBoard{}
	svc := NewDefaultService(repo, board)
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	todo, _ := svc.Create(CreateRequest{
		OrgID: "org", ProjectID: ptr("p"), Text: "rewrite the braking section",
		AssigneeID: ptr("sam"), DueDate: &due, SourceArtifactID: ptr("a1"), SourceChatterID: ptr("c1"),
	}, ptr("me"))

	got, item, err := svc.Promote(todo.ID, ptr("me"), "user:me")
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "wi-1" || board.got.Column != workitems.ColumnTodo || board.got.Description != "rewrite the braking section" {
		t.Fatalf("work item request = %+v", board.got)
	}
	if *board.got.AssigneeID != "sam" || board.got.DueDate == nil || len(board.got.ArtifactIDs) != 1 || board.got.ArtifactIDs[0] != "a1" {
		t.Fatalf("work item request did not carry the to-do's fields: %+v", board.got)
	}
	if !got.Done || got.DoneNote != DoneNotePromoted || got.PromotedWorkItemID == nil || *got.PromotedWorkItemID != "wi-1" {
		t.Fatalf("promoted to-do = %+v", got)
	}
	if _, _, err := svc.Promote(todo.ID, ptr("me"), "user:me"); !errors.Is(err, ErrPromoted) {
		t.Fatalf("second promote err = %v, want ErrPromoted", err)
	}
}

func TestPromoteRefusesAPersonalTodo(t *testing.T) {
	svc := NewDefaultService(newMemRepo(), &fakeBoard{})
	todo, _ := svc.Create(CreateRequest{OrgID: "org", Text: "mine"}, ptr("me"))
	if _, _, err := svc.Promote(todo.ID, ptr("me"), "user:me"); !errors.Is(err, ErrNoProject) {
		t.Fatalf("err = %v, want ErrNoProject", err)
	}
}

func TestPromotedTitleCutsAtAWord(t *testing.T) {
	long := strings.Repeat("word ", 40)
	got := PromotedTitle(long)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) > promotedTitleLimit+1 || strings.Contains(got, "wor…") {
		t.Fatalf("PromotedTitle = %q", got)
	}
	if PromotedTitle("short") != "short" {
		t.Fatal("short text must stay whole")
	}
}
