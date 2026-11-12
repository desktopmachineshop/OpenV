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

	if set, err := repo.Settings(project); err != nil || set.WorkingDays != nil || len(set.Holidays) != 0 || set.Mode != plan.ModeManual || set.ScheduleFrom != plan.FromStart {
		t.Fatalf("a project that never set its plan's settings has the defaults, and no working days stored: %+v %v", set, err)
	}
	start0, finish0 := "2027-01-04", "2027-12-17"
	want := plan.Settings{
		WorkingDays:       []int{1, 7},
		Holidays:          []plan.Holiday{{Date: "2027-12-27", Name: "Shutdown"}, {Date: "2027-01-01"}},
		CriticalSlackDays: 3,
		Scheduling:        plan.Scheduling{Mode: plan.ModeAuto, ProjectStart: &start0, ProjectFinish: &finish0, ScheduleFrom: plan.FromFinish, MultipleCriticalPaths: true},
	}
	if err := repo.SaveSettings(project, want); err != nil {
		t.Fatal(err)
	}
	set, err := repo.Settings(project)
	if err != nil || len(set.WorkingDays) != 2 || set.WorkingDays[1] != 7 || set.CriticalSlackDays != 3 || set.Mode != plan.ModeAuto ||
		*set.ProjectStart != start0 || *set.ProjectFinish != finish0 || set.ScheduleFrom != plan.FromFinish || !set.MultipleCriticalPaths {
		t.Fatalf("settings: %+v %v", set, err)
	}
	if h := set.Holidays; len(h) != 2 || h[0].Date != "2027-01-01" || h[1].Name != "Shutdown" {
		t.Fatalf("holidays in date order: %+v", h)
	}
	// Saved again, they are replaced whole.
	if err := repo.SaveSettings(project, plan.Settings{WorkingDays: []int{1, 2, 3, 4, 5}, Scheduling: plan.Scheduling{Mode: plan.ModeManual, ScheduleFrom: plan.FromStart}}); err != nil {
		t.Fatal(err)
	}
	if set, _ := repo.Settings(project); len(set.Holidays) != 0 || set.ProjectStart != nil || set.Mode != plan.ModeManual || len(set.WorkingDays) != 5 {
		t.Fatalf("settings replaced: %+v", set)
	}
	ann := "11111111-1111-4111-8111-111111111111"
	if err := repo.SetAssignments(project, a, []plan.Assignment{{Type: "user", ID: ann, Units: 50}, {Type: "agent", ID: ann, Units: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAssignments(project, a, []plan.Assignment{{Type: "user", ID: ann, Units: 75}}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.ListAssignments(project); err != nil || len(got) != 1 || got[0] != (plan.Assignment{WorkItemID: a, Type: "user", ID: ann, Units: 75}) {
		t.Fatalf("assignments are replaced: %+v %v", got, err)
	}
	if err := repo.SaveResource(project, plan.Resource{Type: "user", ID: ann, DailyRate: 400, Capacity: 80}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveResource(project, plan.Resource{Type: "user", ID: ann, DailyRate: 425.5, Capacity: 100}); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.ListResources(project); err != nil || len(got) != 1 || got[0].DailyRate != 425.5 || got[0].Capacity != 100 {
		t.Fatalf("resources: %+v %v", got, err)
	}
	if err := repo.SaveResource(project, plan.Resource{Type: "user", ID: "nobody", DailyRate: 1, Capacity: 100}); err != plan.ErrResource {
		t.Fatalf("an id that is not one: %v", err)
	}
	// A planned resource is a row with a name: tasks are assigned to it,
	// staffing hands them to a person (adding to a share they already
	// have), and removing one takes it off its tasks.
	welder, painter := "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	for id, name := range map[string]string{welder: "Welder", painter: "Painter"} {
		if err := repo.SaveResource(project, plan.Resource{Type: plan.TypePlanned, ID: id, PlannedName: name, DailyRate: 300, Capacity: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repo.ListResources(project); err != nil || len(got) != 3 || got[0].Type != plan.TypePlanned || got[0].PlannedName != "Welder" || got[2].PlannedName != "" {
		t.Fatalf("planned resources keep their names: %+v %v", got, err)
	}
	if err := repo.SetAssignments(project, a, []plan.Assignment{{Type: "user", ID: ann, Units: 75}, {Type: plan.TypePlanned, ID: welder, Units: 50}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAssignments(project, b, []plan.Assignment{{Type: plan.TypePlanned, ID: welder, Units: 100}, {Type: plan.TypePlanned, ID: painter, Units: 100}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceAssignee(project, plan.Assignment{Type: plan.TypePlanned, ID: welder}, plan.Assignment{Type: "user", ID: ann}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveResource(project, plan.TypePlanned, painter); err != nil {
		t.Fatal(err)
	}
	units := map[string]int{}
	staffed, err := repo.ListAssignments(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, as := range staffed {
		units[as.WorkItemID+" "+as.Type] = as.Units
	}
	if len(staffed) != 2 || units[a+" user"] != 125 || units[b+" user"] != 100 {
		t.Fatalf("staffed and removed: %+v", staffed)
	}
	if got, _ := repo.ListResources(project); len(got) != 1 || got[0].Type != "user" {
		t.Fatalf("the placeholders are gone: %+v", got)
	}
	if err := repo.SetAssignments(project, a, []plan.Assignment{{Type: "user", ID: ann, Units: 75}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAssignments(project, b, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSettings(project, plan.Settings{WorkingDays: []int{1, 2, 3, 4, 5}, Scheduling: plan.Scheduling{Mode: plan.ModeManual, ScheduleFrom: plan.FromStart, EffortDrivenDefault: true}}); err != nil {
		t.Fatal(err)
	}
	if set, _ := repo.Settings(project); !set.EffortDrivenDefault {
		t.Fatalf("the effort-driven default: %+v", set)
	}
	budget := 25000.0
	if err := repo.SaveSettings(project, plan.Settings{WorkingDays: []int{1, 2, 3, 4, 5}, Currency: "GBP", Budget: &budget, Scheduling: plan.Scheduling{Mode: plan.ModeManual, ScheduleFrom: plan.FromStart}}); err != nil {
		t.Fatal(err)
	}
	if set, _ := repo.Settings(project); set.Currency != "GBP" || set.Budget == nil || *set.Budget != 25000 {
		t.Fatalf("currency and budget: %+v", set)
	}
	if err := repo.SetOrder(project, map[string]int{a: 2}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := repo.ListSchedules(project); len(rows) == 0 || rows[0].SortOrder == nil || *rows[0].SortOrder != 2 {
		t.Fatalf("a task's place: %+v", rows)
	}
	// The workspace's working days override a project's own, and its time
	// off is read through any of its projects.
	if _, err := db.Exec(`UPDATE projects SET org_id = $2 WHERE id = $1`, project, org); err != nil {
		t.Fatal(err)
	}
	if days, off, err := repo.WorkspaceCalendar(org); err != nil || days != nil || len(off) != 0 {
		t.Fatalf("a workspace that set nothing: %v %v %v", days, off, err)
	}
	if err := repo.SaveProjectWorkingDays(project, []int{1, 2, 3, 6}); err != nil {
		t.Fatal(err)
	}
	if set, _ := repo.Settings(project); len(set.WorkingDays) != 4 || set.WorkingDays[3] != 6 {
		t.Fatalf("the workspace's days are the plan's: %v", set.WorkingDays)
	}
	if err := repo.SaveWorkspaceWorkingDays(org, []int{1, 2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	away := &plan.TimeOff{ID: "44444444-4444-4444-8444-444444444444", Type: "user", AssigneeID: "11111111-1111-4111-8111-111111111111", Start: "2027-08-02", End: "2027-08-13", Name: "Summer"}
	if err := repo.AddTimeOff(org, away); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.ListTimeOff(project); err != nil || len(got) != 1 || got[0] != *away {
		t.Fatalf("time off through the project: %+v %v", got, err)
	}
	if days, off, _ := repo.WorkspaceCalendar(org); len(days) != 5 || len(off) != 1 {
		t.Fatalf("the workspace's calendar: %v %v", days, off)
	}
	if err := repo.DeleteTimeOff(org, away.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTimeOff(org, away.ID); err != plan.ErrNotFound {
		t.Fatalf("removed twice: %v", err)
	}
	// A link to a task of another project is listed from both ends.
	otherProject, otherTask := "33333333-3333-4333-8333-333333333333", ""
	seedProjects(t, db, otherProject)
	if err := db.QueryRow(`INSERT INTO work_items (id, project_id, title, due_date) VALUES (gen_random_uuid(), $1, 'Their task', '2027-04-02') RETURNING id`, otherProject).Scan(&otherTask); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveExternalLink(a, otherTask, "SS", 3, "d"); err != nil {
		t.Fatal(err)
	}
	mine, _ := repo.ListExternalLinks(project)
	theirs, _ := repo.ListExternalLinks(otherProject)
	if len(mine) != 1 || mine[0].LocalID != a || mine[0].TaskID != otherTask || mine[0].ProjectName == "" || mine[0].ProjectID != otherProject || mine[0].Direction != plan.ExternalDrivenBy ||
		mine[0].Type != "SS" || mine[0].LagDays != 3 || mine[0].End == nil || *mine[0].End != "2027-04-02" || *mine[0].Start != "2027-04-02" {
		t.Fatalf("the link from here: %+v", mine)
	}
	if len(theirs) != 1 || theirs[0].LocalID != otherTask || theirs[0].TaskID != a || theirs[0].Direction != plan.ExternalDrives {
		t.Fatalf("the link from there: %+v", theirs)
	}
	if err := repo.DeleteExternalLink(otherTask, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteExternalLink(a, otherTask); err != plan.ErrNotFound {
		t.Fatalf("removed twice: %v", err)
	}
	started, pct, spent := "2027-03-01", 40, 120.5
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: a, ProjectID: project, ActualStart: &started, PercentComplete: &pct, ActualCost: &spent}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := repo.ListSchedules(project); len(rows) == 0 || rows[0].ActualStart == nil || *rows[0].ActualStart != started ||
		rows[0].ActualFinish != nil || *rows[0].PercentComplete != 40 || *rows[0].ActualCost != 120.5 {
		t.Fatalf("actuals: %+v", rows)
	}
	start := "2027-03-01"
	base := &plan.Baseline{ID: "22222222-2222-4222-8222-222222222222", CreatedAt: time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC), ProjectID: project, Name: "As approved", Tasks: []plan.BaselineTask{{ID: a, Start: &start, Cost: 100}}}
	if err := repo.CreateBaseline(base); err != nil || base.ID == "" || base.CreatedAt.IsZero() {
		t.Fatalf("baseline: %+v %v", base, err)
	}
	if got, err := repo.ListBaselines(project); err != nil || len(got) != 1 || got[0].Name != "As approved" || len(got[0].Tasks) != 1 || *got[0].Tasks[0].Start != start {
		t.Fatalf("baselines: %+v %v", got, err)
	}
	status := "2027-03-10"
	if err := repo.SaveSettings(project, plan.Settings{WorkingDays: []int{1, 2, 3, 4, 5}, Tracking: plan.Tracking{StatusDate: &status, ActiveBaselineID: &base.ID}, Scheduling: plan.Scheduling{Mode: plan.ModeManual, ScheduleFrom: plan.FromStart}}); err != nil {
		t.Fatal(err)
	}
	if set, _ := repo.Settings(project); set.StatusDate == nil || *set.StatusDate != status || set.ActiveBaselineID == nil || *set.ActiveBaselineID != base.ID {
		t.Fatalf("tracking settings: %+v", set)
	}
	if err := repo.DeleteBaseline(project, "nope"); err != plan.ErrNotFound {
		t.Fatalf("a baseline id that is not one: %v", err)
	}
	if err := repo.DeleteBaseline(project, base.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteBaseline(project, base.ID); err != plan.ErrNotFound {
		t.Fatalf("deleted twice: %v", err)
	}
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: a, ProjectID: project, FixedCost: 250, EffortDriven: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSchedules(project); len(got) != 1 || got[0].FixedCost != 250 || !got[0].EffortDriven {
		t.Fatalf("fixed cost and effort driven: %+v", got)
	}
	constraint := "2027-03-15"
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: a, ProjectID: project, Mode: plan.ModeAuto, ConstraintType: plan.ConstraintSNET, ConstraintDate: &constraint}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSchedules(project); len(got) != 1 || got[0].Mode != plan.ModeAuto || got[0].ConstraintType != plan.ConstraintSNET || *got[0].ConstraintDate != constraint {
		t.Fatalf("mode and constraint: %+v", got)
	}
	deadline := "2027-06-30"
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: a, ProjectID: project, Elapsed: true, Deadline: &deadline}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSchedules(project); len(got) != 1 || !got[0].Elapsed || got[0].Deadline == nil || *got[0].Deadline != deadline {
		t.Fatalf("elapsed and deadline: %+v", got)
	}
	key := "release:11111111-1111-1111-1111-111111111111"
	if err := repo.SaveSchedule(&plan.TaskSchedule{WorkItemID: a, ProjectID: project, MilestoneKey: &key}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ListSchedules(project); len(got) != 1 || got[0].MilestoneKey == nil || *got[0].MilestoneKey != key {
		t.Fatalf("milestone key: %+v", got)
	}
	if _, err := db.Exec(`DELETE FROM plan_tasks WHERE work_item_id = $1`, a); err != nil {
		t.Fatal(err)
	}
	if list, err := repo.ListSchedules(project); err != nil || len(list) != 0 {
		t.Fatalf("empty schedules: %v %v", list, err)
	}
	start = "2027-03-01"
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

	if err := repo.SetDependencies(project, c, []plan.Dependency{{DependsOnID: a, Type: plan.RelStartToFinish, LagDays: 5, LagUnit: plan.LagElapsedDays}, {DependsOnID: b}}); err != nil {
		t.Fatal(err)
	}
	if deps, _ := repo.ListDependencies(project); len(deps) != 2 || deps[0].LagDays+deps[1].LagDays != 5 {
		t.Fatalf("a task has several relationships, one with its offset: %+v", deps)
	}
	if err := repo.SetDependencies(project, c, []plan.Dependency{{DependsOnID: b, Type: plan.RelFinishToFinish}}); err != nil {
		t.Fatal(err)
	}
	deps, err := repo.ListDependencies(project)
	if err != nil || len(deps) != 1 || deps[0] != (plan.Dependency{WorkItemID: c, DependsOnID: b, Type: plan.RelFinishToFinish, LagUnit: plan.LagWorkingDays}) {
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
