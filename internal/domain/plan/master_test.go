package plan

import (
	"reflect"
	"testing"
)

// The master plan cuts each project's plan at a level: level 1 is the
// top-level tasks, and each level further replaces a task that has
// sub-tasks with them, so a task and its sub-tasks are never both drawn.
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
		{2, []string{"frame", "wire", "ship"}},
		{3, []string{"weld", "paint", "wire", "ship"}},
		{9, []string{"weld", "paint", "wire", "ship"}},
		{MasterAllLevels, []string{"weld", "paint", "wire", "ship"}},
	} {
		m := MasterPlan{}
		m.Add("P", p, tc.levels)
		got := []string{}
		for _, task := range m.Projects[0].Tasks {
			got = append(got, task.ID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("levels %d: got %v, want %v", tc.levels, got, tc.want)
		}
	}
	m := MasterPlan{}
	m.Add("P", p, 2)
	if d := m.Projects[0].Tasks[0].Depth; d != 1 {
		t.Errorf("a sub-task carries its depth: got %d", d)
	}
}
