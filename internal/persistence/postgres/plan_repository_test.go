package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/plan"
	"github.com/openv/requirements-platform/internal/domain/todos"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// TestPlanRepositoryRoundTrip pins the plan repository: a schedule read
// back as written and replaced by a second save, relationships with their
// types replaced as a set (a row written without one is finish to start), a to-do moved between tasks, a milestone's fields and criteria, the
// not-found answers, and a deleted work item, release or project taking its
// rows with it.
func TestPlanRepositoryRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	org := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Plan Org', $2)`, org, "plan-org-"+org[:8]); err != nil {
		t.Fatal(err)
	}
	project := uuid.New().String()
	seedProjects(t, db, project)
	repo := NewPlanRepository(db)
	board := workitems.NewDefaultService(NewWorkItemRepository(db), nil)
	card := func(title string) string {
		t.Helper()
		it, err := board.Create(workitems.CreateWorkItemRequest{ProjectID: project, Title: title}, nil, "tester")
		if err != nil {
			t.Fatal(err)
		}
		return it.ID
	}
	a, b, c := card("A"), card("B"), card("C")

	if list, err := repo.ListSchedules(project); err != nil || len(list) != 0 {
		t.Fatalf("empty schedules: %v %v", list, err)
	}
	start := "2027-03-01"
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: b, ProjectID: project, StartDate: &start, ParentID: &a}); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListSchedules(project)
	if err != nil || len(list) != 1 || *list[0].StartDate != start || *list[0].ParentID != a {
		t.Fatalf("schedule: %+v %v", list, err)
	}
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: b, ProjectID: project}); err != nil {
		t.Fatal(err)
	}
	if list, _ = repo.ListSchedules(project); len(list) != 1 || list[0].StartDate != nil || list[0].ParentID != nil {
		t.Fatalf("a second save replaces the first: %+v", list[0])
	}

	if err := repo.SetDependencies(project, c, []plan.Dependency{{DependsOnID: a, Type: plan.RelStartToStart}, {DependsOnID: b}}); err != nil {
		t.Fatal(err)
	}
	if deps, _ := repo.ListDependencies(project); len(deps) != 2 {
		t.Fatalf("a task has several relationships: %+v", deps)
	}
	if err := repo.SetDependencies(project, c, []plan.Dependency{{DependsOnID: b, Type: plan.RelFinishToFinish}}); err != nil {
		t.Fatal(err)
	}
	deps, err := repo.ListDependencies(project)
	if err != nil || len(deps) != 1 || deps[0] != (plan.Dependency{WorkItemID: c, DependsOnID: b, Type: plan.RelFinishToFinish}) {
		t.Fatalf("dependencies: %+v %v", deps, err)
	}

	todo, err := todos.NewDefaultService(NewTodoRepository(db), nil).Create(todos.CreateRequest{OrgID: org, ProjectID: &project, Text: "Crimp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachTodo(project, todo.ID, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachTodo(project, todo.ID, c); err != nil {
		t.Fatal(err)
	}
	if on, err := repo.ListTodoTasks(project); err != nil || len(on) != 1 || on[todo.ID] != c {
		t.Fatalf("a to-do is on one task: %v %v", on, err)
	}

	if _, err := repo.GetMilestone(uuid.New().String()); !errors.Is(err, plan.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := repo.GetMilestone("not-a-uuid"); !errors.Is(err, plan.ErrNotFound) {
		t.Fatalf("malformed id: %v", err)
	}
	release := uuid.New().String()
	if _, err := db.Exec(`INSERT INTO target_releases (id, project_id, name) VALUES ($1, $2, 'MVP')`, release, project); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	date := "2027-02-01"
	custom := &plan.Milestone{ID: uuid.New().String(), ProjectID: project, Name: "Definition complete", Description: "Needs done",
		TargetDate: &date, Criteria: []plan.Criterion{{Metric: plan.MetricNeedsAddressed, Target: 100}}, CreatedAt: now, UpdatedAt: now}
	forRelease := &plan.Milestone{ID: uuid.New().String(), ProjectID: project, ReleaseID: &release, CreatedAt: now.Add(time.Hour), UpdatedAt: now}
	for _, m := range []*plan.Milestone{custom, forRelease} {
		if err := repo.CreateMilestone(m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetMilestone(custom.ID)
	if err != nil || got.Name != custom.Name || *got.TargetDate != date || got.ReleaseID != nil ||
		len(got.Criteria) != 1 || got.Criteria[0] != custom.Criteria[0] || !got.CreatedAt.Equal(now) {
		t.Fatalf("milestone: %+v %v", got, err)
	}
	got.TargetDate, got.Criteria = nil, nil
	if err := repo.UpdateMilestone(got); err != nil {
		t.Fatal(err)
	}
	ms, err := repo.ListMilestones(project)
	if err != nil || len(ms) != 2 || ms[0].ID != custom.ID || ms[0].TargetDate != nil || ms[0].Criteria == nil || len(ms[0].Criteria) != 0 ||
		*ms[1].ReleaseID != release {
		t.Fatalf("milestones, oldest first: %+v %v", ms, err)
	}
	if err := repo.CreateMilestone(&plan.Milestone{ID: uuid.New().String(), ProjectID: project, ReleaseID: &release, CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("a release has one milestone")
	}
	if err := repo.DeleteMilestone(custom.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteMilestone(custom.ID); !errors.Is(err, plan.ErrNotFound) {
		t.Fatalf("deleted twice: %v", err)
	}
	if err := repo.UpdateMilestone(custom); !errors.Is(err, plan.ErrNotFound) {
		t.Fatalf("update of a deleted one: %v", err)
	}

	// The rows follow what they hang off.
	if _, err := db.Exec(`DELETE FROM target_releases WHERE id = $1`, release); err != nil {
		t.Fatal(err)
	}
	if ms, _ = repo.ListMilestones(project); len(ms) != 0 {
		t.Fatalf("a release takes its milestone: %+v", ms)
	}
	if _, err := db.Exec(`DELETE FROM work_items WHERE id = $1`, b); err != nil {
		t.Fatal(err)
	}
	if deps, _ = repo.ListDependencies(project); len(deps) != 0 {
		t.Fatalf("a work item takes its dependencies: %+v", deps)
	}
	if list, _ = repo.ListSchedules(project); len(list) != 0 {
		t.Fatalf("a work item takes its schedule: %+v", list)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE id = $1`, project); err != nil {
		t.Fatal(err)
	}
	if on, _ := repo.ListTodoTasks(project); len(on) != 0 {
		t.Fatalf("a project takes its plan: %v", on)
	}
}
