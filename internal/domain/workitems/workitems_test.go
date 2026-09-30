package workitems

import (
	"errors"
	"testing"
)

// memRepo is the slice of Repository that Create and Update reach.
type memRepo struct {
	Repository
	items map[string]*WorkItem
}

func (m *memRepo) Save(item *WorkItem) error {
	m.items[item.ID] = item
	return nil
}

func (m *memRepo) Update(item *WorkItem) error {
	m.items[item.ID] = item
	return nil
}

func (m *memRepo) FindByID(id string) (*WorkItem, error) {
	if item, ok := m.items[id]; ok {
		copied := *item
		return &copied, nil
	}
	return nil, ErrNotFound
}

func (m *memRepo) SaveActivity(*Activity) error                       { return nil }
func (m *memRepo) MaxSortOrder(projectID, column string) (int, error) { return -1, nil }

// TestAnUnknownAssigneeTypeIsRefused pins OpenV REQ-23's assignee types: a
// person, an agent or a crew ("team"). Any other assignee_type used to be
// stored as sent, on a create and on an update; it is now refused with
// ErrInvalidAssigneeType, which the API answers 400, and nothing is stored.
// An empty assignee_type is still a person on a create and keeps the stored
// one on an update.
func TestAnUnknownAssigneeTypeIsRefused(t *testing.T) {
	for _, assigneeType := range []string{"robot", "Team", "users", " "} {
		t.Run("create "+assigneeType, func(t *testing.T) {
			repo := &memRepo{items: map[string]*WorkItem{}}
			_, err := NewDefaultService(repo, nil).Create(CreateWorkItemRequest{
				ProjectID: "p", Title: "Check the wording", AssigneeType: assigneeType,
			}, nil, "user:u")
			if !errors.Is(err, ErrInvalidAssigneeType) {
				t.Fatalf("Create with assignee_type %q: err = %v, want ErrInvalidAssigneeType", assigneeType, err)
			}
			if len(repo.items) != 0 {
				t.Fatalf("a refused create stored %d items", len(repo.items))
			}
		})
		t.Run("update "+assigneeType, func(t *testing.T) {
			repo := &memRepo{items: map[string]*WorkItem{
				"w": {ID: "w", ProjectID: "p", Title: "Old", AssigneeType: AssigneeUser},
			}}
			_, err := NewDefaultService(repo, nil).Update("w", UpdateWorkItemRequest{
				Title: "New", AssigneeType: assigneeType,
			}, "user:u")
			if !errors.Is(err, ErrInvalidAssigneeType) {
				t.Fatalf("Update with assignee_type %q: err = %v, want ErrInvalidAssigneeType", assigneeType, err)
			}
			if got := repo.items["w"]; got.Title != "Old" || got.AssigneeType != AssigneeUser {
				t.Fatalf("a refused update stored %+v", got)
			}
		})
	}
	for _, assigneeType := range []string{"", AssigneeUser, AssigneeAgent, AssigneeTeam} {
		t.Run("accepts "+assigneeType, func(t *testing.T) {
			repo := &memRepo{items: map[string]*WorkItem{}}
			svc := NewDefaultService(repo, nil)
			item, err := svc.Create(CreateWorkItemRequest{
				ProjectID: "p", Title: "Plan", AssigneeType: assigneeType,
			}, nil, "user:u")
			if err != nil {
				t.Fatalf("Create with assignee_type %q: %v", assigneeType, err)
			}
			want := assigneeType
			if want == "" {
				want = AssigneeUser
			}
			if item.AssigneeType != want {
				t.Fatalf("assignee_type = %q, want %q", item.AssigneeType, want)
			}
			updated, err := svc.Update(item.ID, UpdateWorkItemRequest{Title: "Plan", AssigneeType: assigneeType}, "user:u")
			if err != nil {
				t.Fatalf("Update with assignee_type %q: %v", assigneeType, err)
			}
			if updated.AssigneeType != want {
				t.Fatalf("updated assignee_type = %q, want %q", updated.AssigneeType, want)
			}
		})
	}
}

// TestColumnsFlowAsTheBoardDoes pins the order the board's columns flow in,
// which is the order ListByProject lists a project's items in (OpenV
// REQ-23), and that every column in it is one ValidColumn accepts.
func TestColumnsFlowAsTheBoardDoes(t *testing.T) {
	want := []string{"backlog", "todo", "in-progress", "review", "done"}
	if len(Columns) != len(want) {
		t.Fatalf("Columns = %v, want %v", Columns, want)
	}
	for i, c := range want {
		if Columns[i] != c {
			t.Fatalf("Columns = %v, want %v", Columns, want)
		}
		if !ValidColumn(c) {
			t.Fatalf("ValidColumn(%q) = false", c)
		}
	}
	if ValidColumn("doing") {
		t.Fatal(`ValidColumn("doing") = true`)
	}
}
