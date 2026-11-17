package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/todos"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// memTodos is an in-memory todos.Repository.
type memTodos struct{ rows map[string]*todos.Todo }

func (m *memTodos) Save(t *todos.Todo) error { c := *t; m.rows[t.ID] = &c; return nil }
func (m *memTodos) Update(t *todos.Todo) error {
	c := *t
	m.rows[t.ID] = &c
	return nil
}
func (m *memTodos) FindByID(id string) (*todos.Todo, error) {
	if t, ok := m.rows[id]; ok {
		c := *t
		return &c, nil
	}
	return nil, todos.ErrNotFound
}
func (m *memTodos) filter(keep func(*todos.Todo) bool) []*todos.Todo {
	var out []*todos.Todo
	for _, t := range m.rows {
		if keep(t) {
			c := *t
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	return out
}
func (m *memTodos) ListByProject(projectID string) ([]*todos.Todo, error) {
	return m.filter(func(t *todos.Todo) bool { return t.ProjectID != nil && *t.ProjectID == projectID }), nil
}
func (m *memTodos) ListForUser(orgID, userID string) ([]*todos.Todo, error) {
	return m.filter(func(t *todos.Todo) bool {
		if t.OrgID != orgID {
			return false
		}
		mine := t.AssigneeID != nil && *t.AssigneeID == userID
		personal := t.ProjectID == nil && t.CreatedBy != nil && *t.CreatedBy == userID
		return mine || personal
	}), nil
}
func (m *memTodos) ListBySourceChatterIDs(ids []string) ([]*todos.Todo, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	return m.filter(func(t *todos.Todo) bool { return t.SourceChatterID != nil && want[*t.SourceChatterID] }), nil
}
func (m *memTodos) Delete(id string) error { delete(m.rows, id); return nil }

// todoBoard is payloadWorkItems that also lists by source note.
type todoBoard struct{ *payloadWorkItems }

func (b todoBoard) ListBySourceChatterIDs(ids []string) ([]*workitems.WorkItem, error) {
	var out []*workitems.WorkItem
	for _, id := range ids {
		for _, item := range b.items {
			if item.SourceChatterID != nil && *item.SourceChatterID == id {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

// noteChatter answers one note on artifact "art-p" of project P.
type noteChatter struct{ fakeChatterService }

func (noteChatter) GetEntry(id string) (*chatter.ChatterEntry, error) {
	if id == "note-p" {
		return &chatter.ChatterEntry{ID: id, ArtifactID: "art-p", Message: "@sam check the wording"}, nil
	}
	return nil, chatter.ErrNotFound
}

// todoFixture is workspace W with project P, where "editor" edits and
// "sam" reads, and "outsider" is a member of W but not of P.
func todoFixture() (*Handler, *memTodos, *payloadWorkItems) {
	repo := &memTodos{rows: map[string]*todos.Todo{}}
	board := &payloadWorkItems{items: map[string]*workitems.WorkItem{}}
	boardSvc := workitems.NewDefaultService(todoBoard{board}, nil)
	h := NewHandler(HandlerDeps{
		WorkItemService: boardSvc,
		TodoService:     todos.NewDefaultService(repo, boardSvc),
		ChatterService:  &noteChatter{},
		ArtifactService: &fakeArtifactService{byID: map[string]*artifacts.Artifact{"art-p": {ID: "art-p", ProjectID: "proj-p"}}},
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-p": {ID: "proj-p", OrgID: "org-w"},
		}},
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-w": {"editor": orgs.RoleMember, "sam": orgs.RoleMember, "outsider": orgs.RoleMember},
		}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-p": {"editor": members.RoleEditor, "sam": members.RoleViewer},
		}},
	})
	return h, repo, board
}

func todoReq(method, userID, id, body string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/x", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-w")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": id})
}

func decodeTodo(t *testing.T, w *httptest.ResponseRecorder) *todos.Todo {
	t.Helper()
	var out todos.Todo
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return &out
}

// TestAProjectTodoIsRaisedTickedAndPromoted walks a to-do from a note to a
// board task: raised from a note with its artifact as the source, assigned
// to the person named, shown in their My to-dos, ticked off and opened
// again, then promoted to a work item that carries its text.
func TestAProjectTodoIsRaisedTickedAndPromoted(t *testing.T) {
	h, _, board := todoFixture()

	w := httptest.NewRecorder()
	h.CreateProjectTodo(w, todoReq("POST", "editor", "proj-p", `{"text":"check the wording","assignee_id":"sam","source_chatter_id":"note-p"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	todo := decodeTodo(t, w)
	if todo.SourceArtifactID == nil || *todo.SourceArtifactID != "art-p" || todo.OrgID != "org-w" {
		t.Fatalf("created = %+v", todo)
	}

	w = httptest.NewRecorder()
	h.ListMyTodos(w, todoReq("GET", "sam", "", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), todo.ID) {
		t.Fatalf("sam's to-dos: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.SetTodoDone(w, todoReq("POST", "editor", todo.ID, `{"done":true}`))
	if w.Code != http.StatusOK || !decodeTodo(t, w).Done {
		t.Fatalf("tick: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.SetTodoDone(w, todoReq("POST", "editor", todo.ID, `{"done":false}`))
	if w.Code != http.StatusOK || decodeTodo(t, w).Done {
		t.Fatalf("untick: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	h.PromoteTodo(w, todoReq("POST", "editor", todo.ID, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	var promoted struct {
		Todo     todos.Todo         `json:"todo"`
		WorkItem workitems.WorkItem `json:"work_item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &promoted); err != nil {
		t.Fatal(err)
	}
	if !promoted.Todo.Done || promoted.Todo.DoneNote != todos.DoneNotePromoted ||
		promoted.WorkItem.Title != "check the wording" || promoted.WorkItem.Column != workitems.ColumnTodo {
		t.Fatalf("promoted = %+v", promoted)
	}
	if _, ok := board.items[promoted.WorkItem.ID]; !ok {
		t.Fatal("the work item was not saved on the board")
	}
	w = httptest.NewRecorder()
	h.PromoteTodo(w, todoReq("POST", "editor", todo.ID, ""))
	if w.Code != http.StatusConflict {
		t.Fatalf("second promote: %d", w.Code)
	}
}

// TestProjectTodosAnswerAsTheProjectDoes: a reader may list but not add; a
// caller outside the project is answered as for an id no row has; a note of
// another project, an assignee who is not a member and a second paragraph
// are refused.
func TestProjectTodosAnswerAsTheProjectDoes(t *testing.T) {
	h, repo, _ := todoFixture()
	cases := []struct {
		user, body string
		want       int
	}{
		{"sam", `{"text":"x"}`, http.StatusForbidden},
		{"outsider", `{"text":"x"}`, http.StatusNotFound},
		{"editor", `{"text":"x","source_chatter_id":"note-elsewhere"}`, http.StatusBadRequest},
		{"editor", `{"text":"x","assignee_id":"outsider"}`, http.StatusBadRequest},
		{"editor", `{"text":"one\n\ntwo"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.CreateProjectTodo(w, todoReq("POST", c.user, "proj-p", c.body))
		if w.Code != c.want {
			t.Errorf("%s %s: %d %s, want %d", c.user, c.body, w.Code, w.Body.String(), c.want)
		}
	}
	if len(repo.rows) != 0 {
		t.Fatalf("refused creates stored %d rows", len(repo.rows))
	}

	w := httptest.NewRecorder()
	h.ListProjectTodos(w, todoReq("GET", "sam", "proj-p", ""))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("empty list: %d %q", w.Code, w.Body.String())
	}
}

// TestAPersonalTodoIsItsWritersAlone: a personal to-do is listed, read and
// ticked by its writer, does not exist for anyone else, and cannot go on a
// board.
func TestAPersonalTodoIsItsWritersAlone(t *testing.T) {
	h, _, _ := todoFixture()
	w := httptest.NewRecorder()
	h.CreatePersonalTodo(w, todoReq("POST", "sam", "", `{"text":"book the rig"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	todo := decodeTodo(t, w)
	if todo.ProjectID != nil || todo.AssigneeID == nil || *todo.AssigneeID != "sam" {
		t.Fatalf("personal = %+v", todo)
	}

	w = httptest.NewRecorder()
	h.GetTodo(w, todoReq("GET", "editor", todo.ID, ""))
	if w.Code != http.StatusNotFound {
		t.Fatalf("another member read it: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.SetTodoDone(w, todoReq("POST", "sam", todo.ID, `{"done":true}`))
	if w.Code != http.StatusOK {
		t.Fatalf("tick: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.PromoteTodo(w, todoReq("POST", "sam", todo.ID, ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("promote personal: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ListMyTodos(w, todoReq("GET", "editor", "", ""))
	if strings.Contains(w.Body.String(), todo.ID) {
		t.Fatal("another member's My to-dos lists it")
	}
}

// TestANoteShowsItsTodoAndStillItsOldCard: a note raised since to-dos have
// a table shows the to-do, open or done; a note raised before shows the
// work item it raised, as it always did.
func TestANoteShowsItsTodoAndStillItsOldCard(t *testing.T) {
	h, repo, board := todoFixture()
	pid, note, old := "proj-p", "note-p", "note-old"
	repo.rows["t1"] = &todos.Todo{ID: "t1", OrgID: "org-w", ProjectID: &pid, Text: "check", SourceChatterID: &note, Done: true}
	board.items["c1"] = &workitems.WorkItem{ID: "c1", ProjectID: pid, Title: "old card", Column: workitems.ColumnReview, SourceChatterID: &old, ArtifactIDs: []string{}}
	h.MemberService = nil // names are not under test here
	entries := []*chatter.ChatterEntry{{ID: note}, {ID: old}}
	h.decorateChatterEntries(pid, entries)
	if entries[0].Todo == nil || entries[0].Todo.TodoID != "t1" || entries[0].Todo.Status != "done" || entries[0].Todo.WorkItemID != "" {
		t.Fatalf("new note's to-do = %+v", entries[0].Todo)
	}
	if entries[1].Todo == nil || entries[1].Todo.WorkItemID != "c1" || entries[1].Todo.Status != workitems.ColumnReview {
		t.Fatalf("old note's to-do = %+v", entries[1].Todo)
	}
}
