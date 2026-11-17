package plan

import (
	"reflect"
	"testing"
)

// The master plan cuts each project's plan at a level: level 1 is the
// top-level tasks, and each level further adds the sub-tasks of the tasks
// above, which stay and are marked as summaries.
func TestMasterPlanLevels(t *testing.T) {
	id := func(s string) *string { return &s }
	p := &Plan{ProjectID: "p", Today: "2026-11-13", Tasks: []*Task{
		{ID: "build", Title: "Build"},
		{ID: "frame", Title: "Frame", ParentID: id("build"), Depth: 1},
		{ID: "weld", Title: "Weld", ParentID: id("frame"), Depth: 2},
		{ID: "paint", Title: "Paint", ParentID: id("frame"), Depth: 2},
		{ID: "wire", Title: "Wire", ParentID: id("build"), Depth: 1},
		{ID: "ship", Title: "Ship"},
	}}
	for _, tc := range []struct {
		levels int
		want   []string
	}{
		{1, []string{"build", "ship"}},
		{2, []string{"build", "frame", "wire", "ship"}},
		{3, []string{"build", "frame", "weld", "paint", "wire", "ship"}},
		{9, []string{"build", "frame", "weld", "paint", "wire", "ship"}},
		{MasterAllLevels, []string{"build", "frame", "weld", "paint", "wire", "ship"}},
	} {
		m := MasterPlan{}
		m.Add("P", "", p, tc.levels)
		got := []string{}
		for _, task := range m.Projects[0].Tasks {
			got = append(got, task.ID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("levels %d: got %v, want %v", tc.levels, got, tc.want)
		}
	}
	m := MasterPlan{}
	m.Add("P", "parent", p, 2)
	if m.Projects[0].ParentProjectID != "parent" {
		t.Errorf("a line says which project it belongs under: %+v", m.Projects[0])
	}
	tasks := m.Projects[0].Tasks
	if !tasks[0].Summary || !tasks[1].Summary || tasks[2].Summary || tasks[3].Summary {
		t.Errorf("a task with sub-tasks is a summary, shown or not: %+v", tasks)
	}
	if tasks[1].Depth != 1 {
		t.Errorf("a sub-task carries its depth: got %d", tasks[1].Depth)
	}
}

// Two moments of a plan compared task by task: what was added, removed,
// finished, moved or advanced, and nothing that reads the same.
func TestTaskChangesBetweenTwoMoments(t *testing.T) {
	d := func(s string) *string { return &s }
	before := []BaselineTask{
		{ID: "same", Title: "Same", Start: d("2027-03-01"), End: d("2027-03-05")},
		{ID: "slips", Title: "Slips", Start: d("2027-03-01"), End: d("2027-03-05")},
		{ID: "done", Title: "Done", Start: d("2027-03-01"), End: d("2027-03-05"), Progress: 50},
		{ID: "gone", Title: "Gone", Start: d("2027-03-01"), End: d("2027-03-05")},
		{ID: "ahead", Title: "Ahead", Progress: 10},
	}
	after := []BaselineTask{
		{ID: "same", Title: "Same", Start: d("2027-03-01"), End: d("2027-03-05")},
		{ID: "slips", Title: "Slips", Start: d("2027-03-03"), End: d("2027-03-12")},
		{ID: "done", Title: "Done", Start: d("2027-03-01"), End: d("2027-03-08"), Progress: 100, Done: true},
		{ID: "new", Title: "New"},
		{ID: "ahead", Title: "Ahead", Progress: 40},
	}
	got := TaskChanges(before, after)
	kinds := []string{}
	for _, c := range got {
		kinds = append(kinds, c.ID+":"+c.Change)
	}
	want := []string{"slips:moved", "done:finished", "new:added", "ahead:progress", "gone:removed"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("got %v, want %v", kinds, want)
	}
	if got[0].StartShiftDays != 2 || got[0].FinishShiftDays != 7 {
		t.Errorf("a task that slips says by how many days: %+v", got[0])
	}
	if got[2].Before != nil || got[4].After != nil || got[4].Title != "Gone" {
		t.Errorf("an added task has no before, a removed one no after and keeps its title: %+v %+v", got[2], got[4])
	}
	if len(TaskChanges(before, before)) != 0 {
		t.Error("a plan compared with itself has no changes")
	}
}

// A person's tasks across plans: the ones assigned to them or that they
// are a resource of, a parent left to its sub-tasks.
func TestMyTasksAcrossPlans(t *testing.T) {
	id := func(s string) *string { return &s }
	p := &Plan{ProjectID: "p", Today: "2026-11-13", Tasks: []*Task{
		{ID: "build", Title: "Build", AssigneeType: "user", AssigneeID: id("ann")},
		{ID: "frame", Title: "Frame", ParentID: id("build"), AssigneeType: "user", AssigneeID: id("ann")},
		{ID: "wire", Title: "Wire", ParentID: id("build"), Assignments: []Assignment{{Type: "user", ID: "ann"}, {Type: "user", ID: "bob"}}},
		{ID: "paint", Title: "Paint", AssigneeType: "user", AssigneeID: id("bob")},
		{ID: "draft", Title: "Draft", AssigneeType: "agent", AssigneeID: id("ann")},
	}}
	m := MyTasks{Tasks: []MyTask{}}
	m.Add("Rig", p, "ann")
	got := []string{}
	for _, task := range m.Tasks {
		got = append(got, task.ID)
	}
	if want := []string{"frame", "wire"}; !reflect.DeepEqual(got, want) || m.Tasks[0].ProjectName != "Rig" || m.Today != "2026-11-13" {
		t.Fatalf("got %v %+v, want %v", got, m, want)
	}
}
