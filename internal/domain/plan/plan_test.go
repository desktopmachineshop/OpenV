package plan

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/targetreleases"
	"github.com/openv/requirements-platform/internal/domain/todos"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

func sp(s string) *string { return &s }

// everyDay is a calendar with no days off, for the tests that count days.
var everyDay = []int{1, 2, 3, 4, 5, 6, 7}

func day(s string) *time.Time {
	t, _ := time.Parse(DateLayout, s)
	return &t
}

var now = time.Date(2027, 3, 10, 12, 0, 0, 0, time.UTC)

func item(id, title, column string, due *time.Time, age int) *workitems.WorkItem {
	return &workitems.WorkItem{ID: id, ProjectID: "p", Title: title, Column: column, DueDate: due,
		AssigneeType: workitems.AssigneeUser, CreatedAt: now.AddDate(0, 0, -100+age)}
}

func task(p *Plan, id string) *Task {
	for _, t := range p.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func TestTaskProgressComesFromToDosAndSubTasks(t *testing.T) {
	in := Inputs{
		ProjectID: "p",
		Items: []*workitems.WorkItem{
			item("parent", "Build the rig", workitems.ColumnInProgress, nil, 0),
			item("a", "Frame", workitems.ColumnDone, day("2027-03-05"), 1),
			item("b", "Wiring", workitems.ColumnInProgress, day("2027-03-20"), 2),
			item("lone", "Order parts", workitems.ColumnTodo, day("2027-03-01"), 3),
			item("free", "Someday", workitems.ColumnBacklog, nil, 4),
		},
		Schedules: []*TaskSchedule{
			{WorkItemID: "a", ProjectID: "p", StartDate: sp("2027-03-01"), ParentID: sp("parent")},
			{WorkItemID: "b", ProjectID: "p", StartDate: sp("2027-03-04"), ParentID: sp("parent")},
		},
		Todos: []*todos.Todo{
			{ID: "t1", Text: "Crimp", Done: true}, {ID: "t2", Text: "Label", Done: false},
			{ID: "t3", Text: "Test", Done: false}, {ID: "t4", Text: "Loose to-do", Done: true},
			{ID: "t5", Text: "Sign off", Done: true},
		},
		TodoTasks:    map[string]string{"t1": "b", "t2": "b", "t3": "b", "t5": "parent"},
		Dependencies: []Dependency{{WorkItemID: "b", DependsOnID: "a"}, {WorkItemID: "lone", DependsOnID: "b"}},
	}
	p := Build(in, now)

	b := task(p, "b")
	if b.Progress != 33 || b.TodosDone != 1 || b.TodosTotal != 3 {
		t.Fatalf("wiring: %d%% %d/%d, want 33%% 1/3", b.Progress, b.TodosDone, b.TodosTotal)
	}
	if b.Todos[0].ID != "t2" || b.Todos[2].ID != "t1" {
		t.Fatalf("open to-dos come first: %+v", b.Todos)
	}
	parent := task(p, "parent")
	// One to-do done, one sub-task done, one a third done: (1 + 1 + 1/3) / 3.
	if parent.Progress != 78 || parent.SubtasksDone != 1 || parent.SubtasksTotal != 2 {
		t.Fatalf("parent: %d%% %d/%d, want 78%% 1/2", parent.Progress, parent.SubtasksDone, parent.SubtasksTotal)
	}
	if *parent.SpanStart != "2027-03-01" || *parent.SpanEnd != "2027-03-20" || parent.Start != nil {
		t.Fatalf("a parent spans its sub-tasks: %v-%v", *parent.SpanStart, *parent.SpanEnd)
	}
	if a := task(p, "a"); a.Progress != 100 || a.Late {
		t.Fatalf("a done task is 100%% and never late: %+v", a)
	}
	lone := task(p, "lone")
	if !lone.Late || *lone.SpanStart != "2027-03-01" || lone.Progress != 0 {
		t.Fatalf("an open task past its due date is late, and a due date alone is its span: %+v", lone)
	}
	if len(lone.Conflicts) != 1 || lone.Conflicts[0] != "b" {
		t.Fatalf("lone starts before what it depends on ends: %v", lone.Conflicts)
	}
	if len(b.Conflicts) != 0 {
		t.Fatalf("a finished predecessor is no conflict: %v", b.Conflicts)
	}
	got := []string{}
	for _, tk := range p.Tasks {
		got = append(got, tk.WBS+" "+tk.ID)
	}
	if want := "1 parent,1.1 a,1.2 b,2 lone,3 free"; strings.Join(got, ",") != want {
		t.Fatalf("outline %v, want %s", got, want)
	}
	if p.Summary != (Summary{Tasks: 5, Done: 1, Late: 1, Unscheduled: 1, Progress: 26, Critical: 1}) {
		t.Fatalf("summary %+v", p.Summary)
	}
	if *p.Start != "2027-03-01" || *p.End != "2027-03-20" {
		t.Fatalf("span %s-%s", *p.Start, *p.End)
	}
}

func evidence() Evidence {
	art := func(id, typ, status string, attrs map[string]interface{}) *artifacts.Artifact {
		return &artifacts.Artifact{ID: id, Type: typ, Status: status, Attributes: attrs}
	}
	return Evidence{
		Artifacts: []*artifacts.Artifact{
			art("n1", artifacts.TypeUserNeed, "", nil), art("n2", artifacts.TypeUserNeed, "", nil),
			art("r1", artifacts.TypeRequirement, artifacts.StatusApproved, map[string]interface{}{targetreleases.AttrFrom: "mvp"}),
			art("r2", artifacts.TypeRequirement, "draft", map[string]interface{}{targetreleases.AttrFrom: "v1"}),
			art("r3", artifacts.TypeRequirement, artifacts.StatusApproved, nil),
			art("tc1", artifacts.TypeTestCase, "", nil), art("tc2", artifacts.TypeTestCase, "", nil),
		},
		Links: []*links.Link{
			{FromID: "r1", ToID: "n1", Type: "derives-from"},
			{FromID: "r2", ToID: "n1", Type: "derives-from"},
			{FromID: "tc1", ToID: "r1", Type: "verifies"},
		},
		Coverage: &vv.CoverageReport{Entries: []vv.CoverageEntry{
			{RequirementID: "r1", Rollup: vv.RollupPass, TestCaseIDs: []string{"tc1"}},
			{RequirementID: "r2", Rollup: vv.RollupUncovered},
		}},
		Latest: map[string]string{"tc1": vv.ResultPass},
	}
}

func TestMilestonesMeasureTheirCriteria(t *testing.T) {
	rel := func(id, name, date, status string, pos int) *targetreleases.Release {
		return &targetreleases.Release{ID: id, Name: name, TargetDate: sp(date), Status: status, Position: pos}
	}
	in := Inputs{
		ProjectID: "p",
		Evidence:  evidence(),
		Releases: []*targetreleases.Release{
			rel("mvp", "MVP", "2027-04-01", targetreleases.StatusInProgress, 0),
			rel("v1", "V1.0", "2027-09-01", targetreleases.StatusPlanned, 1),
		},
		Milestones: []*Milestone{
			{ID: "def", Name: "Definition complete", TargetDate: sp("2027-02-01"),
				Criteria: []Criterion{{MetricNeedsAddressed, 100}, {MetricRequirementsApproved, 50}}},
			{ID: "v1row", ReleaseID: sp("v1"), Criteria: []Criterion{{MetricTestsPassing, 100}}},
			{ID: "bare", Name: "Kick-off"},
		},
	}
	p := Build(in, now)
	keys := []string{}
	by := map[string]MilestoneProgress{}
	for _, m := range p.Milestones {
		keys = append(keys, m.Key)
		by[m.Key] = m
	}
	if want := "def,release:mvp,release:v1,bare"; strings.Join(keys, ",") != want {
		t.Fatalf("order %v, want %s", keys, want)
	}

	def := by["def"]
	// One of two needs addressed (50% of the way to 100), two of three
	// requirements approved (past its target of 50).
	if def.Progress != 75 || def.Met || !def.Late {
		t.Fatalf("definition: %d%% met=%v late=%v, want 75%% unmet late", def.Progress, def.Met, def.Late)
	}
	if c := def.Criteria[0]; c.Actual != 50 || c.Count != 1 || c.Total != 2 || c.Met || c.Label != "User needs addressed" {
		t.Fatalf("needs addressed: %+v", c)
	}
	if c := def.Criteria[1]; !c.Met || c.Percent != 100 || c.Actual != 66 {
		t.Fatalf("requirements approved: %+v", c)
	}

	// MVP holds r1 only: approved and verified, so its default criteria are met.
	mvp := by["release:mvp"]
	if !mvp.DefaultCriteria || mvp.ID != "" || !mvp.Met || mvp.Progress != 100 || mvp.Kind != KindRelease {
		t.Fatalf("mvp: %+v", mvp)
	}
	if c := mvp.Criteria[0]; c.Total != 1 || c.Count != 1 {
		t.Fatalf("a release's criteria count its own requirements: %+v", c)
	}
	// V1.0 carries its own criterion: one of two test cases passes.
	v1 := by["release:v1"]
	if v1.DefaultCriteria || v1.ID != "v1row" || v1.Progress != 50 || v1.Met || v1.Late {
		t.Fatalf("v1: %+v", v1)
	}
	if bare := by["bare"]; bare.Met || bare.Progress != 0 || len(bare.Criteria) != 0 {
		t.Fatalf("a milestone with no criteria is not met: %+v", bare)
	}
	if len(p.Metrics) != len(Metrics) || p.Metrics[0].Percent != 50 {
		t.Fatalf("project metrics: %+v", p.Metrics)
	}
	if *p.Start != "2027-02-01" || *p.End != "2027-09-01" {
		t.Fatalf("milestones widen the span: %s-%s", *p.Start, *p.End)
	}

	in.Releases[1].Status = targetreleases.StatusReleased
	if m := Build(in, now).Milestones[2]; !m.Met || m.Progress != 100 {
		t.Fatalf("a released release's milestone is met: %+v", m)
	}
}

func TestACriterionIsNotMetByRounding(t *testing.T) {
	ev := Evidence{}
	for i := 0; i < 200; i++ {
		status := artifacts.StatusApproved
		if i == 0 {
			status = "draft"
		}
		ev.Artifacts = append(ev.Artifacts, &artifacts.Artifact{ID: itoa(i), Type: artifacts.TypeRequirement, Status: status})
	}
	p := Build(Inputs{Evidence: ev, Milestones: []*Milestone{{ID: "m", Name: "M", Criteria: []Criterion{{MetricRequirementsApproved, 100}}}}}, now)
	if m := p.Milestones[0]; m.Met || m.Progress != 99 || m.Criteria[0].Percent != 99 {
		t.Fatalf("199 of 200 is not 100%%: %+v", m)
	}
}

// fakeRepo is a plan.Repository in memory.
type fakeRepo struct {
	schedules   map[string]*TaskSchedule
	deps        map[string][]Dependency
	days        []int
	holidays    []Holiday
	slack       int
	set         Settings
	assignments map[string][]Assignment
	resources   []Resource
	baselines   []*Baseline
	external    []ExternalLink
	todoTasks   map[string]string
	milestones  map[string]*Milestone
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{days: everyDay, schedules: map[string]*TaskSchedule{}, deps: map[string][]Dependency{}, todoTasks: map[string]string{}, milestones: map[string]*Milestone{}}
}

func (r *fakeRepo) ListSchedules(string) ([]*TaskSchedule, error) {
	out := []*TaskSchedule{}
	for _, s := range r.schedules {
		out = append(out, s)
	}
	return out, nil
}
func (r *fakeRepo) Settings(string) (Settings, error) {
	set := r.set
	set.WorkingDays, set.Holidays, set.CriticalSlackDays = r.days, r.holidays, r.slack
	return set, nil
}
func (r *fakeRepo) SaveSettings(_ string, s Settings) error {
	r.set, r.days, r.holidays, r.slack = s, s.WorkingDays, s.Holidays, s.CriticalSlackDays
	return nil
}
func (r *fakeRepo) ListAssignments(string) ([]Assignment, error) {
	out := []Assignment{}
	for id, list := range r.assignments {
		for _, a := range list {
			a.WorkItemID = id
			out = append(out, a)
		}
	}
	return out, nil
}
func (r *fakeRepo) SetAssignments(_, id string, a []Assignment) error {
	if r.assignments == nil {
		r.assignments = map[string][]Assignment{}
	}
	r.assignments[id] = a
	return nil
}
func (r *fakeRepo) ListResources(string) ([]Resource, error) { return r.resources, nil }
func (r *fakeRepo) SaveResource(_ string, res Resource) error {
	r.resources = append(r.resources, res)
	return nil
}
func (r *fakeRepo) SetOrder(_ string, order map[string]int) error {
	for id, n := range order {
		n := n
		if r.schedules[id] == nil {
			r.schedules[id] = &TaskSchedule{WorkItemID: id, ProjectID: "p"}
		}
		r.schedules[id].SortOrder = &n
		if n == 0 {
			r.schedules[id].SortOrder = nil
		}
	}
	return nil
}
func (r *fakeRepo) ListExternalLinks(string) ([]ExternalLink, error) { return r.external, nil }
func (r *fakeRepo) SaveExternalLink(taskID, on, kind string, lag int, unit string) error {
	r.external = append(r.external, ExternalLink{LocalID: taskID, TaskID: on, ProjectID: "other", ProjectName: "Other", Title: "Elsewhere",
		Start: sp("2027-03-22"), End: sp("2027-03-24"), Type: kind, LagDays: lag, LagUnit: unit, Direction: ExternalDrivenBy})
	return nil
}
func (r *fakeRepo) DeleteExternalLink(taskID, other string) error {
	for i, l := range r.external {
		if l.LocalID == taskID && l.TaskID == other {
			r.external = append(r.external[:i], r.external[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func (r *fakeRepo) ListBaselines(string) ([]*Baseline, error) { return r.baselines, nil }
func (r *fakeRepo) CreateBaseline(b *Baseline) error {
	r.baselines = append(r.baselines, b)
	return nil
}
func (r *fakeRepo) DeleteBaseline(_, id string) error {
	for i, b := range r.baselines {
		if b.ID == id {
			r.baselines = append(r.baselines[:i], r.baselines[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
func (r *fakeRepo) SaveSchedule(s *TaskSchedule) error {
	// As the table does, a save leaves the task's place alone.
	if old := r.schedules[s.WorkItemID]; old != nil && old != s {
		s.SortOrder = old.SortOrder
	}
	r.schedules[s.WorkItemID] = s
	return nil
}
func (r *fakeRepo) ListDependencies(string) ([]Dependency, error) {
	out := []Dependency{}
	for _, on := range r.deps {
		out = append(out, on...)
	}
	return out, nil
}
func (r *fakeRepo) SetDependencies(_, id string, on []Dependency) error { r.deps[id] = on; return nil }
func (r *fakeRepo) ListTodoTasks(string) (map[string]string, error)     { return r.todoTasks, nil }
func (r *fakeRepo) AttachTodo(_, todoID, id string) error               { r.todoTasks[todoID] = id; return nil }
func (r *fakeRepo) ListMilestones(string) ([]*Milestone, error) {
	out := []*Milestone{}
	for _, m := range r.milestones {
		out = append(out, m)
	}
	return out, nil
}
func (r *fakeRepo) GetMilestone(id string) (*Milestone, error) {
	if m := r.milestones[id]; m != nil {
		return m, nil
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) CreateMilestone(m *Milestone) error { r.milestones[m.ID] = m; return nil }
func (r *fakeRepo) UpdateMilestone(m *Milestone) error { r.milestones[m.ID] = m; return nil }
func (r *fakeRepo) DeleteMilestone(id string) error    { delete(r.milestones, id); return nil }

// fakeBoard is the board in memory; a task of another project is "x".
type fakeBoard struct {
	items   map[string]*workitems.WorkItem
	updates int
}

func (b *fakeBoard) Get(id string) (*workitems.WorkItem, error) {
	if it := b.items[id]; it != nil {
		return it, nil
	}
	return nil, workitems.ErrNotFound
}
func (b *fakeBoard) ListByProject(projectID string) ([]*workitems.WorkItem, error) {
	out := []*workitems.WorkItem{}
	for _, it := range b.items {
		if it.ProjectID == projectID {
			out = append(out, it)
		}
	}
	return out, nil
}
func (b *fakeBoard) Update(id string, req workitems.UpdateWorkItemRequest, _ string) (*workitems.WorkItem, error) {
	b.updates++
	it := b.items[id]
	it.Title, it.DueDate = req.Title, req.DueDate
	return it, nil
}

func (b *fakeBoard) Create(req workitems.CreateWorkItemRequest, _ *string, _ string) (*workitems.WorkItem, error) {
	it := &workitems.WorkItem{ID: fmt.Sprintf("new%d", len(b.items)), ProjectID: req.ProjectID, Title: req.Title, Column: req.Column,
		AssigneeType: workitems.AssigneeUser, CreatedAt: now.Add(time.Duration(len(b.items)) * time.Second)}
	b.items[it.ID] = it
	return it, nil
}

type fakeTodos struct{ made []*todos.Todo }

func (f *fakeTodos) ListByProject(string) ([]*todos.Todo, error) { return f.made, nil }
func (f *fakeTodos) Create(req todos.CreateRequest, _ *string) (*todos.Todo, error) {
	text, err := todos.NormalizeText(req.Text)
	if err != nil {
		return nil, err
	}
	t := &todos.Todo{ID: "todo" + itoa(len(f.made)+1), OrgID: req.OrgID, ProjectID: req.ProjectID, Text: text}
	f.made = append(f.made, t)
	return t, nil
}

func newService() (*DefaultService, *fakeRepo, *fakeBoard, *fakeTodos) {
	repo := newFakeRepo()
	board := &fakeBoard{items: map[string]*workitems.WorkItem{
		"a": item("a", "A", workitems.ColumnTodo, day("2027-03-10"), 0),
		"b": item("b", "B", workitems.ColumnTodo, nil, 1),
		"c": item("c", "C", workitems.ColumnTodo, nil, 2),
		"x": {ID: "x", ProjectID: "other", Title: "Elsewhere"},
	}}
	td := &fakeTodos{}
	return NewDefaultService(repo, board, td, nil), repo, board, td
}

func TestScheduleChecksDatesParentsAndDependencies(t *testing.T) {
	svc, repo, board, _ := newService()
	fails := func(name string, req ScheduleRequest, id string, want error) {
		t.Helper()
		if err := svc.Schedule(id, req, "tester"); !errors.Is(err, want) {
			t.Fatalf("%s: %v, want %v", name, err, want)
		}
	}
	fails("unknown task", ScheduleRequest{}, "nope", ErrTaskNotFound)
	fails("bad date", ScheduleRequest{StartDate: sp("10/03/2027")}, "a", ErrInvalidDate)
	fails("no such day", ScheduleRequest{StartDate: sp("2027-02-30")}, "a", ErrInvalidDate)
	fails("start after due", ScheduleRequest{StartDate: sp("2027-03-11")}, "a", ErrDateOrder)
	fails("own parent", ScheduleRequest{ParentID: sp("a")}, "a", ErrParentSelf)
	fails("parent elsewhere", ScheduleRequest{ParentID: sp("x")}, "a", ErrOtherProject)
	fails("depends on itself", ScheduleRequest{DependsOn: &[]string{"a"}}, "a", ErrDependsOnSelf)
	fails("dependency elsewhere", ScheduleRequest{DependsOn: &[]string{"x"}}, "a", ErrOtherProject)

	ok := func(id string, req ScheduleRequest) {
		t.Helper()
		if err := svc.Schedule(id, req, "tester"); err != nil {
			t.Fatal(err)
		}
	}
	ok("a", ScheduleRequest{StartDate: sp("2027-03-01")})
	if board.updates != 0 || *repo.schedules["a"].StartDate != "2027-03-01" {
		t.Fatalf("a start date alone leaves the work item alone: %d updates", board.updates)
	}
	ok("b", ScheduleRequest{ParentID: sp("a"), DueDate: sp("2027-03-08"), DependsOn: &[]string{"c", "c", " "}})
	if board.updates != 1 || board.items["b"].DueDate.Format(DateLayout) != "2027-03-08" || board.items["b"].Title != "B" {
		t.Fatalf("the due date is the work item's: %+v", board.items["b"])
	}
	if got := repo.deps["b"]; len(got) != 1 || got[0] != (Dependency{WorkItemID: "b", DependsOnID: "c", Type: RelFinishToStart, LagUnit: LagWorkingDays}) {
		t.Fatalf("dependencies are deduplicated: %v", got)
	}
	ok("c", ScheduleRequest{ParentID: sp("b")})
	fails("a loop of parents", ScheduleRequest{ParentID: sp("c")}, "a", ErrParentCycle)
	fails("a loop of dependencies", ScheduleRequest{DependsOn: &[]string{"b"}}, "c", ErrDependencyCycle)

	// b has a sub-task now, so it takes its dates from it and none can be set on it.
	fails("dates on a task with sub-tasks", ScheduleRequest{StartDate: sp("2027-03-02")}, "b", ErrParentDates)
	ok("c", ScheduleRequest{ParentID: sp("")})

	// An omitted field is left as it is; an empty one clears it.
	ok("b", ScheduleRequest{StartDate: sp("2027-03-02")})
	if s := repo.schedules["b"]; s.ParentID == nil || *s.ParentID != "a" || len(repo.deps["b"]) != 1 || board.updates != 1 {
		t.Fatalf("an omitted field changed: %+v", s)
	}
	ok("b", ScheduleRequest{ParentID: sp(""), DueDate: sp(""), DependsOn: &[]string{}})
	if s := repo.schedules["b"]; s.ParentID != nil || board.items["b"].DueDate != nil || len(repo.deps["b"]) != 0 {
		t.Fatalf("an empty field did not clear: %+v", s)
	}
}

func TestAddTodoAttachesItToTheTask(t *testing.T) {
	svc, repo, _, td := newService()
	todo, err := svc.AddTodo("a", "org", "  Crimp the connectors ", nil, sp("me"))
	if err != nil {
		t.Fatal(err)
	}
	if todo.Text != "Crimp the connectors" || *todo.ProjectID != "p" || repo.todoTasks[todo.ID] != "a" {
		t.Fatalf("%+v on %v", todo, repo.todoTasks)
	}
	if _, err := svc.AddTodo("a", "org", " ", nil, nil); !errors.Is(err, todos.ErrTextRequired) || len(td.made) != 1 {
		t.Fatalf("an empty to-do: %v", err)
	}
	if _, err := svc.AddTodo("nope", "org", "x", nil, nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	p, err := svc.Plan("p", Evidence{}, now)
	if err != nil || task(p, "a").TodosTotal != 1 {
		t.Fatalf("the plan shows the to-do: %v", err)
	}
}

func TestMilestoneRules(t *testing.T) {
	svc, repo, _, _ := newService()
	if _, err := svc.CreateMilestone("p", MilestoneRequest{Name: sp(" ")}); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("no name: %v", err)
	}
	bad := map[error][]Criterion{
		ErrUnknownMetric:   {{"velocity", 100}},
		ErrInvalidTarget:   {{MetricNeedsAddressed, 0}},
		ErrDuplicateMetric: {{MetricNeedsAddressed, 100}, {MetricNeedsAddressed, 50}},
	}
	for want, criteria := range bad {
		c := criteria
		if _, err := svc.CreateMilestone("p", MilestoneRequest{Name: sp("M"), Criteria: &c}); !errors.Is(err, want) {
			t.Fatalf("%v: got %v", want, err)
		}
	}
	m, err := svc.CreateMilestone("p", MilestoneRequest{Name: sp(" Definition complete "), TargetDate: sp("2027-02-01"),
		Criteria: &[]Criterion{{MetricNeedsAddressed, 100}}})
	if err != nil || m.Name != "Definition complete" || *m.TargetDate != "2027-02-01" {
		t.Fatalf("%+v %v", m, err)
	}
	if m, err = svc.UpdateMilestone(m.ID, MilestoneRequest{TargetDate: sp("")}); err != nil || m.TargetDate != nil || len(m.Criteria) != 1 {
		t.Fatalf("clearing the date keeps the rest: %+v %v", m, err)
	}

	rm, err := svc.SetReleaseCriteria("p", "rel", []Criterion{{MetricTestsPassing, 90}})
	if err != nil || *rm.ReleaseID != "rel" {
		t.Fatal(err)
	}
	again, _ := svc.SetReleaseCriteria("p", "rel", []Criterion{})
	if again.ID != rm.ID || len(repo.milestones) != 2 || len(again.Criteria) != 0 {
		t.Fatalf("a release keeps one milestone: %+v", again)
	}
	if _, err := svc.UpdateMilestone(rm.ID, MilestoneRequest{Name: sp("Renamed")}); !errors.Is(err, ErrReleaseMilestone) {
		t.Fatalf("a release milestone's name is the release's: %v", err)
	}
}

func TestRenderEveryFormat(t *testing.T) {
	in := Inputs{
		ProjectID: "p",
		Items: []*workitems.WorkItem{
			item("a", "=Frame, \"welded\"", workitems.ColumnDone, day("2027-03-05"), 0),
			item("b", "Wiring", workitems.ColumnInProgress, day("2027-03-20"), 1),
			item("c", "Unscheduled", workitems.ColumnBacklog, nil, 2),
		},
		Schedules:    []*TaskSchedule{{WorkItemID: "a", StartDate: sp("2027-03-01")}, {WorkItemID: "b", StartDate: sp("2027-03-06"), ParentID: sp("a")}},
		Dependencies: []Dependency{{WorkItemID: "b", DependsOnID: "a"}},
		Milestones:   []*Milestone{{ID: "m", Name: "Definition complete", TargetDate: sp("2027-03-15"), Criteria: []Criterion{{MetricNeedsAddressed, 100}}}, {ID: "n", Name: "Kick-off"}},
		Evidence:     evidence(),
		WorkingDays:  everyDay,
	}
	in.Items[1].AssigneeID = sp("u1")
	doc := Document{ProjectName: `Rig "Mk/2"`, ExportedAt: now, Plan: Build(in, now), Assignees: map[string]string{"u1": "Sam"}}

	magic := map[string]string{FormatPDF: "%PDF-", FormatExcel: "PK", FormatCSV: "wbs,task,", FormatJSON: "{"}
	for _, f := range Formats {
		out, err := Render(doc, f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !bytes.HasPrefix(out.Data, []byte(magic[f])) || out.ContentType == "" {
			t.Fatalf("%s: starts %q", f, out.Data[:8])
		}
		if !strings.HasPrefix(out.Filename, "plan-Rig Mk2-2027-03-10.") {
			t.Fatalf("%s: filename %q", f, out.Filename)
		}
	}
	if _, err := Render(doc, "reqif"); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("reqif: %v", err)
	}

	csvOut, _ := Render(doc, FormatCSV)
	text := string(csvOut.Data)
	for _, want := range []string{
		`1,"'=Frame, ""welded""",Done,,2027-03-06,2027-03-20,15,,0,false,100,`,
		"1.1,Wiring,In Progress,Sam,2027-03-06,2027-03-20,15,,0,true,0,0,0,0,0,1FS,false",
		"\n\nmilestone,kind,date,",
		"Definition complete,custom,2027-03-15,50,false,User needs addressed,100,50,1,2,false",
		"Kick-off,custom,,0,false,,,,,,",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("csv lacks %q:\n%s", want, text)
		}
	}

	// A plan with nothing in it still renders.
	for _, f := range Formats {
		if _, err := Render(Document{ProjectName: "Empty", ExportedAt: now, Plan: Build(Inputs{}, now)}, f); err != nil {
			t.Fatalf("empty %s: %v", f, err)
		}
	}
}

func TestRelationshipsHaveATypeAndATaskMayHaveSeveral(t *testing.T) {
	svc, repo, _, _ := newService()
	rels := func(r ...Relationship) ScheduleRequest { return ScheduleRequest{Relationships: &r} }
	if err := svc.Schedule("a", rels(Relationship{TaskID: "b", Type: "XX"}), "tester"); !errors.Is(err, ErrRelationshipType) {
		t.Fatalf("an unknown type: %v", err)
	}
	// Two tasks drive c: it starts with a and after b finishes.
	if err := svc.Schedule("c", rels(Relationship{TaskID: "a", Type: "ss"}, Relationship{TaskID: "b", Type: ""}, Relationship{TaskID: "a", Type: "FF"}), "tester"); err != nil {
		t.Fatal(err)
	}
	want := []Dependency{{WorkItemID: "c", DependsOnID: "a", Type: RelStartToStart, LagUnit: LagWorkingDays}, {WorkItemID: "c", DependsOnID: "b", Type: RelFinishToStart, LagUnit: LagWorkingDays}}
	if got := repo.deps["c"]; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("relationships %+v, want %+v (a pair has one relationship, the first named)", got, want)
	}
	// A loop is a loop whatever the types along it.
	if err := svc.Schedule("a", rels(Relationship{TaskID: "c", Type: "FF"}), "tester"); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("a loop through SS and FF: %v", err)
	}
	// The earlier depends_on form keeps the type a task has, and a new one is FS.
	if err := svc.Schedule("c", ScheduleRequest{DependsOn: &[]string{"a"}}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := repo.deps["c"]; len(got) != 1 || got[0].Type != RelStartToStart {
		t.Fatalf("depends_on changed a type: %+v", got)
	}
}

func TestEachRelationshipTypeIsBrokenByItsOwnDates(t *testing.T) {
	in := Inputs{
		Items: []*workitems.WorkItem{
			item("on", "Driver", workitems.ColumnTodo, day("2027-03-10"), 0),
			item("t", "Driven", workitems.ColumnTodo, day("2027-03-08"), 1),
		},
		Schedules: []*TaskSchedule{{WorkItemID: "on", StartDate: sp("2027-03-05")}, {WorkItemID: "t", StartDate: sp("2027-03-04")}},
	}
	// Driven runs 4 to 8 March, the driver 5 to 10 March: it starts before
	// the driver starts, ends before it ends, and starts before it ends. It
	// does end after the driver starts, so start to finish holds.
	in.Dependencies = []Dependency{{WorkItemID: "t", DependsOnID: "on", Type: RelStartToFinish}}
	if got := task(Build(in, now), "t"); got.Relationships[0].Conflict {
		t.Fatalf("SF holds while the driven task ends on or after the driver's start: %+v", got.Relationships)
	}
	for _, kind := range []string{RelFinishToStart, RelStartToStart, RelFinishToFinish} {
		in.Dependencies = []Dependency{{WorkItemID: "t", DependsOnID: "on", Type: kind}}
		got := task(Build(in, now), "t")
		if len(got.Relationships) != 1 || got.Relationships[0] != (TaskRelationship{TaskID: "on", Type: kind, LagUnit: LagWorkingDays, Conflict: true}) || len(got.Conflicts) != 1 {
			t.Fatalf("%s: %+v", kind, got.Relationships)
		}
	}
	cases := []struct{ kind, start, end string }{
		{RelStartToStart, "2027-03-05", "2027-03-06"},   // starts with the driver, ends before it
		{RelFinishToFinish, "2027-03-01", "2027-03-10"}, // starts before it, ends with it
		{RelFinishToStart, "2027-03-11", "2027-03-12"},  // starts the day after it ends
	}
	for _, c := range cases {
		in.Dependencies = []Dependency{{WorkItemID: "t", DependsOnID: "on", Type: c.kind}}
		in.Items[1].DueDate, in.Schedules[1].StartDate = day(c.end), sp(c.start)
		if got := task(Build(in, now), "t"); got.Relationships[0].Conflict || len(got.Conflicts) != 0 {
			t.Fatalf("%s kept from %s to %s is not broken: %+v", c.kind, c.start, c.end, got.Relationships)
		}
	}
	// A stored row with no type reads as finish to start.
	in.Dependencies = []Dependency{{WorkItemID: "t", DependsOnID: "on"}}
	if got := task(Build(in, now), "t"); got.Relationships[0].Type != RelFinishToStart {
		t.Fatalf("an untyped row: %+v", got.Relationships)
	}
}

func TestTaskTodosListsOnlyThatTasks(t *testing.T) {
	svc, _, _, _ := newService()
	first, _ := svc.AddTodo("a", "org", "Crimp", nil, nil)
	if _, err := svc.AddTodo("b", "org", "Label", nil, nil); err != nil {
		t.Fatal(err)
	}
	list, err := svc.TaskTodos("a")
	if err != nil || len(list) != 1 || list[0].ID != first.ID {
		t.Fatalf("%+v %v", list, err)
	}
	if list, _ = svc.TaskTodos("c"); list == nil || len(list) != 0 {
		t.Fatalf("a task with none answers an empty list: %v", list)
	}
	if _, err := svc.TaskTodos("nope"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
}

func TestATaskKeepsItsDuration(t *testing.T) {
	svc, repo, board, _ := newService()
	dates := func(id string) (string, string, int) {
		t.Helper()
		sc := repo.schedules[id]
		start, due, dur := "", "", 0
		if sc != nil && sc.StartDate != nil {
			start = *sc.StartDate
		}
		if d := board.items[id].DueDate; d != nil {
			due = d.Format(DateLayout)
		}
		if sc != nil && sc.DurationDays != nil {
			dur = *sc.DurationDays
		}
		return start, due, dur
	}
	must := func(id string, req ScheduleRequest, start, due string, dur int) {
		t.Helper()
		if err := svc.Schedule(id, req, "tester"); err != nil {
			t.Fatal(err)
		}
		if s, d, n := dates(id); s != start || d != due || n != dur {
			t.Fatalf("%s: %s to %s, %d days; want %s to %s, %d days", id, s, d, n, start, due, dur)
		}
	}
	n := func(i int) *int { return &i }
	// b has no dates: a duration alone is kept for later.
	must("b", ScheduleRequest{DurationDays: n(5)}, "", "", 5)
	// A start then gives it its due date, counting both ends.
	must("b", ScheduleRequest{StartDate: sp("2027-03-01")}, "2027-03-01", "2027-03-05", 5)
	// Moving the start moves the end; moving the end moves the start.
	must("b", ScheduleRequest{StartDate: sp("2027-03-08")}, "2027-03-08", "2027-03-12", 5)
	must("b", ScheduleRequest{DueDate: sp("2027-03-20")}, "2027-03-16", "2027-03-20", 5)
	// A new duration moves the end, the start staying put.
	must("b", ScheduleRequest{DurationDays: n(10)}, "2027-03-16", "2027-03-25", 10)
	// Both dates together (a bar resized) decide the duration.
	must("b", ScheduleRequest{StartDate: sp("2027-03-16"), DueDate: sp("2027-03-18")}, "2027-03-16", "2027-03-18", 3)
	// a has only a due date: a duration places its start before it.
	must("a", ScheduleRequest{DurationDays: n(4)}, "2027-03-07", "2027-03-10", 4)
	// The board moved a's due date behind the plan's back: the dates win.
	later := time.Date(2027, 3, 12, 0, 0, 0, 0, time.UTC)
	board.items["a"].DueDate = &later
	must("a", ScheduleRequest{StartDate: sp("2027-03-08")}, "2027-03-08", "2027-03-13", 6)

	if err := svc.Schedule("a", ScheduleRequest{DurationDays: n(-1)}, "tester"); !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("a negative duration: %v", err)
	}
	f := func(v float64) *float64 { return &v }
	if err := svc.Schedule("a", ScheduleRequest{EffortDays: f(-2)}, "tester"); !errors.Is(err, ErrInvalidEffort) {
		t.Fatalf("negative effort: %v", err)
	}
	if err := svc.Schedule("a", ScheduleRequest{EffortDays: f(7.5)}, "tester"); err != nil || *repo.schedules["a"].EffortDays != 7.5 {
		t.Fatalf("effort: %v", err)
	}
	// Effort is its own number: it survives a move, and 0 clears it.
	must("a", ScheduleRequest{StartDate: sp("2027-03-09")}, "2027-03-09", "2027-03-14", 6)
	if *repo.schedules["a"].EffortDays != 7.5 {
		t.Fatal("a move dropped the effort")
	}
	if err := svc.Schedule("a", ScheduleRequest{EffortDays: f(0)}, "tester"); err != nil || repo.schedules["a"].EffortDays != nil {
		t.Fatalf("clearing the effort: %v", err)
	}
	p, _ := svc.Plan("p", Evidence{}, now)
	if d := task(p, "b").DurationDays; d == nil || *d != 3 {
		t.Fatalf("the plan reports the duration: %v", d)
	}
}

func TestCriticalPathIsTheChainThatDecidesThePlansEnd(t *testing.T) {
	// design 1-5, build 6-12 after design (FS, tight), test 13-20 after build
	// (tight) and the last task; docs 6-8 after design, with room to slip;
	// paint 14-16 must finish with test (FF) and has 4 days; old is done.
	mk := func(id, start, end, col string, age int) (*workitems.WorkItem, *TaskSchedule) {
		return item(id, id, col, day("2027-03-"+end), age), &TaskSchedule{WorkItemID: id, StartDate: sp("2027-03-" + start)}
	}
	in := Inputs{WorkingDays: everyDay}
	for i, r := range [][4]string{
		{"design", "01", "05", workitems.ColumnTodo}, {"build", "06", "12", workitems.ColumnTodo},
		{"test", "13", "20", workitems.ColumnTodo}, {"docs", "06", "08", workitems.ColumnTodo},
		{"paint", "14", "16", workitems.ColumnTodo}, {"old", "18", "20", workitems.ColumnDone},
	} {
		it, sc := mk(r[0], r[1], r[2], r[3], i)
		in.Items, in.Schedules = append(in.Items, it), append(in.Schedules, sc)
	}
	in.Items = append(in.Items, item("someday", "someday", workitems.ColumnBacklog, nil, 9))
	effort := 6.5
	in.Schedules[1].EffortDays = &effort
	in.Dependencies = []Dependency{
		{WorkItemID: "build", DependsOnID: "design", Type: RelFinishToStart},
		{WorkItemID: "test", DependsOnID: "build", Type: RelFinishToStart},
		{WorkItemID: "docs", DependsOnID: "design", Type: RelFinishToStart},
		{WorkItemID: "paint", DependsOnID: "test", Type: RelStartToStart},
		{WorkItemID: "test", DependsOnID: "paint", Type: RelFinishToFinish},
	}
	p := Build(in, now)
	want := map[string]struct {
		float    int
		critical bool
	}{
		"design": {0, true}, "build": {0, true}, "test": {0, true},
		"docs":  {12, false}, // nothing waits for it: it can run to the plan's last day
		"paint": {4, false},  // test must not finish before it, and test finishes 4 days after
		"old":   {0, false},  // done: nothing left to slip
	}
	for id, w := range want {
		got := task(p, id)
		if got.FloatDays == nil || *got.FloatDays != w.float || got.Critical != w.critical {
			t.Errorf("%s: float %v critical %v, want %d %v", id, got.FloatDays, got.Critical, w.float, w.critical)
		}
	}
	if s := task(p, "someday"); s.FloatDays != nil || s.Critical {
		t.Errorf("an unscheduled task has no float: %+v", s)
	}
	if p.Summary.Critical != 3 || p.Summary.EffortDays != 6.5 {
		t.Errorf("summary: %+v", p.Summary)
	}
	if d := task(p, "build").DurationDays; d == nil || *d != 7 {
		t.Errorf("build runs 7 days: %v", d)
	}
	// Give build a day's gap before test and it gains a day of float, and so
	// does design, which is tight against build. The path still runs through
	// both, since they are what test waits for, and build says how far its
	// end can slip before it pushes test.
	in.Items[1].DueDate = day("2027-03-11")
	p = Build(in, now)
	build, design := task(p, "build"), task(p, "design")
	if *build.FloatDays != 1 || *design.FloatDays != 1 || !build.Critical || !design.Critical || !task(p, "test").Critical {
		t.Errorf("a gap is float, and the path still shows: build %d %v design %d %v", *build.FloatDays, build.Critical, *design.FloatDays, design.Critical)
	}
	if build.FloatUntil == nil || *build.FloatUntil != "2027-03-12" || design.FloatUntil != nil {
		t.Errorf("build can slip to the day before test starts, design has no room: %v %v", build.FloatUntil, design.FloatUntil)
	}
	if task(p, "docs").Critical || task(p, "paint").Critical {
		t.Errorf("tasks off the path stay off it")
	}
	// Nothing waits for docs: its float runs to the plan's last day.
	if u := task(p, "docs").FloatUntil; u == nil || *u != "2027-03-20" {
		t.Errorf("docs can slip to the end of the plan: %v", u)
	}
}

func TestATodoHasOneDateItsDeadline(t *testing.T) {
	due := time.Date(2027, 3, 12, 15, 0, 0, 0, time.UTC)
	p := Build(Inputs{
		Items:     []*workitems.WorkItem{item("a", "A", workitems.ColumnTodo, nil, 0)},
		Todos:     []*todos.Todo{{ID: "t1", Text: "Crimp", DueDate: &due}, {ID: "t2", Text: "Label"}},
		TodoTasks: map[string]string{"t1": "a", "t2": "a"},
	}, now)
	got := task(p, "a").Todos
	if got[0].DueDate == nil || *got[0].DueDate != "2027-03-12" || got[1].DueDate != nil {
		t.Fatalf("%+v", got)
	}
}

func TestWorkingDaysDecideDurationAndFloat(t *testing.T) {
	// March 2027: the 1st is a Monday, the 6th and 7th a weekend.
	week := NewCalendar(nil)
	if week.Span("2027-03-01", "2027-03-12") != 10 || week.Span("2027-03-05", "2027-03-08") != 2 || week.Span("2027-03-06", "2027-03-07") != 1 {
		t.Fatal("a duration counts the working days from start to end, and is at least 1")
	}
	if week.End("2027-03-04", 3) != "2027-03-08" || week.End("2027-03-06", 1) != "2027-03-08" || week.Start("2027-03-08", 3) != "2027-03-04" || week.Start("2027-03-07", 1) != "2027-03-05" {
		t.Fatal("a task's other date skips the days off")
	}
	if week.Between("2027-03-05", "2027-03-08") != 1 || week.Between("2027-03-08", "2027-03-05") != -1 || week.Between("2027-03-01", "2027-03-29") != 20 {
		t.Fatal("slip is counted in working days")
	}
	if NewCalendar(everyDay).Span("2027-03-05", "2027-03-08") != 4 {
		t.Fatal("a plan that works every day counts every day")
	}
	if _, err := CleanWorkingDays([]int{}); !errors.Is(err, ErrWorkingDays) {
		t.Fatalf("no working days: %v", err)
	}
	if _, err := CleanWorkingDays([]int{0}); !errors.Is(err, ErrWorkingDays) {
		t.Fatalf("day 0: %v", err)
	}
	if d, _ := CleanWorkingDays([]int{7, 1, 1}); len(d) != 2 || d[0] != 1 || d[1] != 7 {
		t.Fatalf("sorted, each once: %v", d)
	}

	svc, repo, board, _ := newService()
	if _, err := svc.SetWorkingDays("p", []int{9}); !errors.Is(err, ErrWorkingDays) {
		t.Fatalf("a bad day: %v", err)
	}
	if days, err := svc.SetWorkingDays("p", []int{5, 4, 3, 2, 1}); err != nil || len(days) != 5 || days[0] != 1 {
		t.Fatalf("%v %v", days, err)
	}
	n := 3
	// b: three working days from Thursday the 4th end on Monday the 8th.
	if err := svc.Schedule("b", ScheduleRequest{StartDate: sp("2027-03-04"), DurationDays: &n}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := board.items["b"].DueDate.Format(DateLayout); got != "2027-03-08" || *repo.schedules["b"].DurationDays != 3 {
		t.Fatalf("due %s", got)
	}
	// Moved to start on Friday, it still takes three working days.
	if err := svc.Schedule("b", ScheduleRequest{StartDate: sp("2027-03-05")}, "tester"); err != nil || board.items["b"].DueDate.Format(DateLayout) != "2027-03-09" {
		t.Fatalf("%v %v", err, board.items["b"].DueDate)
	}
	p, _ := svc.Plan("p", Evidence{}, now)
	if len(p.WorkingDays) != 5 || *task(p, "b").DurationDays != 3 {
		t.Fatalf("the plan reports its working days and counts in them: %v", p.WorkingDays)
	}
	// a is due Wednesday the 10th, a day after b: one working day of float for b.
	if f := task(p, "b").FloatDays; f == nil || *f != 1 {
		t.Fatalf("float in working days: %v", f)
	}
}

func TestATaskWithSubTasksTakesItsDatesFromThem(t *testing.T) {
	effort := func(v float64) *float64 { return &v }
	in := Inputs{
		WorkingDays: everyDay,
		Items: []*workitems.WorkItem{
			// The folder has dates of its own from before it had sub-tasks: they no longer count.
			item("folder", "Folder", workitems.ColumnTodo, day("2027-06-30"), 0),
			item("x", "X", workitems.ColumnTodo, day("2027-03-05"), 1),
			item("y", "Y", workitems.ColumnTodo, day("2027-03-20"), 2),
			item("after", "After", workitems.ColumnTodo, day("2027-03-25"), 3),
			item("empty", "Empty folder", workitems.ColumnTodo, day("2027-03-01"), 4),
			item("unplaced", "Unplaced", workitems.ColumnTodo, nil, 5),
		},
		Schedules: []*TaskSchedule{
			{WorkItemID: "folder", StartDate: sp("2027-01-01"), EffortDays: effort(99)},
			{WorkItemID: "x", StartDate: sp("2027-03-01"), ParentID: sp("folder"), EffortDays: effort(2)},
			{WorkItemID: "y", StartDate: sp("2027-03-06"), ParentID: sp("folder"), EffortDays: effort(3.5)},
			{WorkItemID: "after", StartDate: sp("2027-03-21")},
			{WorkItemID: "unplaced", ParentID: sp("empty")},
		},
		Dependencies: []Dependency{
			{WorkItemID: "y", DependsOnID: "x", Type: RelFinishToStart},
			{WorkItemID: "after", DependsOnID: "y", Type: RelFinishToStart},
		},
	}
	p := Build(in, now)
	f := task(p, "folder")
	if f.Start != nil || f.End != nil || *f.SpanStart != "2027-03-01" || *f.SpanEnd != "2027-03-20" || *f.DurationDays != 20 || *f.EffortDays != 5.5 {
		t.Fatalf("a folder spans its sub-tasks and adds up their effort: %+v", f)
	}
	if e := task(p, "empty"); e.SpanStart != nil || e.DurationDays != nil {
		t.Fatalf("a folder whose sub-tasks have no dates has none: %+v", e)
	}
	// The path runs x, y, after: through the folder, which is not itself on it.
	if f.Critical || !f.CriticalSubtasks || !task(p, "x").Critical || !task(p, "y").Critical || !task(p, "after").Critical {
		t.Fatalf("the critical path runs through the folder's sub-tasks: %+v", f)
	}
	if task(p, "empty").CriticalSubtasks || p.Summary.Critical != 3 || p.Summary.EffortDays != 5.5 {
		t.Fatalf("summary counts the tasks themselves, once: %+v", p.Summary)
	}
	// Each relationship is seen from both ends.
	if d := task(p, "y").Drives; len(d) != 1 || d[0] != (TaskRelationship{TaskID: "after", Type: RelFinishToStart, LagUnit: LagWorkingDays}) {
		t.Fatalf("y drives after: %+v", d)
	}
	if d := task(p, "after").Drives; len(d) != 0 {
		t.Fatalf("after drives nothing: %+v", d)
	}
}

func TestARelationshipIsMadeFromEitherEnd(t *testing.T) {
	svc, repo, _, _ := newService()
	has := func(driven, driver, kind string) bool {
		for _, d := range repo.deps[driven] {
			if d.DependsOnID == driver && d.Type == kind {
				return true
			}
		}
		return false
	}
	// From a: a drives b, finish to start. It is stored on b, the driven task.
	if err := svc.Relate("a", "b", "fs", true, 0, ""); err != nil || !has("b", "a", RelFinishToStart) {
		t.Fatalf("a drives b: %v %+v", err, repo.deps)
	}
	// From b the same relationship is changed to start to start.
	if err := svc.Relate("b", "a", RelStartToStart, false, 0, ""); err != nil || !has("b", "a", RelStartToStart) || len(repo.deps["b"]) != 1 {
		t.Fatalf("retyped from the other end: %v %+v", err, repo.deps)
	}
	// Turned round from a: now b drives a, and the old one is gone.
	if err := svc.Relate("a", "b", RelFinishToFinish, false, 0, ""); err != nil || !has("a", "b", RelFinishToFinish) || len(repo.deps["b"]) != 0 {
		t.Fatalf("turned round: %v %+v", err, repo.deps)
	}
	// c drives b, so b -> a and c -> b: a driving c would close a loop.
	if err := svc.Relate("c", "b", "", true, 0, ""); err != nil || !has("b", "c", RelFinishToStart) {
		t.Fatal(err)
	}
	if err := svc.Relate("a", "c", RelFinishToStart, true, 0, ""); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("a loop: %v", err)
	}
	for want, call := range map[error]func() error{
		ErrDependsOnSelf:    func() error { return svc.Relate("a", "a", "FS", true, 0, "") },
		ErrOtherProject:     func() error { return svc.Relate("a", "x", "FS", true, 0, "") },
		ErrTaskNotFound:     func() error { return svc.Relate("nope", "a", "FS", true, 0, "") },
		ErrRelationshipType: func() error { return svc.Relate("a", "c", "XX", true, 0, "") },
	} {
		if err := call(); !errors.Is(err, want) {
			t.Fatalf("want %v, got %v", want, err)
		}
	}
	// Removed from either end, whichever way it runs.
	if err := svc.Unrelate("a", "b"); err != nil || len(repo.deps["a"]) != 0 || !has("b", "c", RelFinishToStart) {
		t.Fatalf("unrelate: %v %+v", err, repo.deps)
	}
	if err := svc.Unrelate("c", "b"); err != nil || len(repo.deps["b"]) != 0 {
		t.Fatalf("unrelate from the driver: %v %+v", err, repo.deps)
	}
}

func TestATaskIsAssignedToAMilestoneAndCountsTowardIt(t *testing.T) {
	in := Inputs{
		WorkingDays: everyDay,
		Items: []*workitems.WorkItem{
			item("design", "Design", workitems.ColumnTodo, nil, 0),
			item("d1", "Drive", workitems.ColumnDone, day("2027-03-05"), 1),
			item("d2", "Guard", workitems.ColumnTodo, day("2027-03-20"), 2),
			item("own", "Own", workitems.ColumnDone, day("2027-03-25"), 3),
			item("free", "Free", workitems.ColumnTodo, day("2027-03-25"), 4),
			item("lost", "Lost", workitems.ColumnTodo, day("2027-03-25"), 5),
		},
		Schedules: []*TaskSchedule{
			// A folder on a milestone puts its sub-tasks on it; one of them is on another.
			{WorkItemID: "design", MilestoneKey: sp("m1")},
			{WorkItemID: "d1", ParentID: sp("design")},
			{WorkItemID: "d2", ParentID: sp("design")},
			{WorkItemID: "own", ParentID: sp("design"), MilestoneKey: sp("m2")},
			// A milestone that has gone is no milestone.
			{WorkItemID: "lost", MilestoneKey: sp("deleted")},
		},
		Milestones: []*Milestone{
			{ID: "m1", ProjectID: "p", Name: "Review", TargetDate: sp("2027-03-21"), Criteria: []Criterion{{MetricNeedsAddressed, 100}}},
			{ID: "m2", ProjectID: "p", Name: "Ship", TargetDate: sp("2027-04-01"), Criteria: []Criterion{{MetricMilestoneTasks, 50}}},
			{ID: "m3", ProjectID: "p", Name: "Later", Criteria: []Criterion{}},
		},
	}
	p := Build(in, now)
	ms := map[string]MilestoneProgress{}
	for _, m := range p.Milestones {
		ms[m.Key] = m
	}
	if got := ms["m1"].Tasks; len(got) != 2 || got[0] != "d1" || got[1] != "d2" {
		t.Fatalf("m1 counts the folder's sub-tasks left on it, not the folder: %v", got)
	}
	// m1's tasks join its criteria unasked: one of two done.
	c := ms["m1"].Criteria
	if len(c) != 2 || c[1].Metric != MetricMilestoneTasks || c[1].Count != 1 || c[1].Total != 2 || c[1].Target != 100 || ms["m1"].Met {
		t.Fatalf("m1 criteria: %+v", c)
	}
	// m2 names the metric itself, with its own target.
	if c := ms["m2"].Criteria; len(c) != 1 || c[0].Target != 50 || c[0].Count != 1 || c[0].Total != 1 || !ms["m2"].Met || ms["m2"].Progress != 100 {
		t.Fatalf("m2 criteria: %+v", c)
	}
	if m := ms["m3"]; len(m.Criteria) != 0 || len(m.Tasks) != 0 {
		t.Fatalf("a milestone with no tasks gains no criterion: %+v", m)
	}
	if d := task(p, "d2"); d.Milestone == nil || *d.Milestone != "m1" || !d.MilestoneInherited {
		t.Fatalf("a sub-task takes its folder's milestone: %+v", d)
	}
	if o := task(p, "own"); *o.Milestone != "m2" || o.MilestoneInherited {
		t.Fatalf("its own milestone wins: %+v", o)
	}
	if task(p, "free").Milestone != nil || task(p, "lost").Milestone != nil {
		t.Fatalf("no milestone, and one that has gone, are both none")
	}
	for _, v := range p.Metrics {
		if v.Metric == MetricMilestoneTasks && (v.Count != 2 || v.Total != 3) {
			t.Fatalf("across the plan: %+v", v)
		}
	}
}

func TestScheduleAssignsAMilestoneOfTheSameProject(t *testing.T) {
	svc, repo, _, _ := newService()
	repo.milestones["m1"] = &Milestone{ID: "m1", ProjectID: "p", Name: "Review"}
	repo.milestones["far"] = &Milestone{ID: "far", ProjectID: "other", Name: "Elsewhere"}
	if err := svc.Schedule("a", ScheduleRequest{Milestone: sp("m1")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := repo.schedules["a"].MilestoneKey; got == nil || *got != "m1" {
		t.Fatalf("assigned: %v", got)
	}
	// A request that names no milestone leaves it.
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-08")}, "tester"); err != nil || repo.schedules["a"].MilestoneKey == nil {
		t.Fatalf("kept: %v %v", err, repo.schedules["a"].MilestoneKey)
	}
	for _, key := range []string{"far", "nowhere", "release:none"} {
		if err := svc.Schedule("a", ScheduleRequest{Milestone: sp(key)}, "tester"); !errors.Is(err, ErrMilestone) {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if err := svc.Schedule("a", ScheduleRequest{Milestone: sp("")}, "tester"); err != nil || repo.schedules["a"].MilestoneKey != nil {
		t.Fatalf("cleared: %v %v", err, repo.schedules["a"].MilestoneKey)
	}
}

func TestARelationshipCarriesAnOffset(t *testing.T) {
	week := []int{1, 2, 3, 4, 5}
	in := Inputs{
		WorkingDays: week,
		Items: []*workitems.WorkItem{
			// 1 March 2027 is a Monday.
			item("a", "A", workitems.ColumnTodo, day("2027-03-05"), 0),
			item("b", "B", workitems.ColumnTodo, day("2027-03-12"), 1),
			item("c", "C", workitems.ColumnTodo, day("2027-03-19"), 2),
		},
		Schedules: []*TaskSchedule{
			{WorkItemID: "a", StartDate: sp("2027-03-01")},
			{WorkItemID: "b", StartDate: sp("2027-03-08")},
			{WorkItemID: "c", StartDate: sp("2027-03-10")},
		},
		Dependencies: []Dependency{
			// B starts 5 working days after A starts: 8 March exactly.
			{WorkItemID: "b", DependsOnID: "a", Type: RelStartToStart, LagDays: 5},
			// C starts 3 working days after B finishes: it starts too early.
			{WorkItemID: "c", DependsOnID: "b", Type: RelFinishToStart, LagDays: 3},
		},
	}
	p := Build(in, now)
	if r := task(p, "b").Relationships[0]; r.LagDays != 5 || r.Conflict {
		t.Fatalf("b keeps its 5 days: %+v", r)
	}
	if r := task(p, "c").Relationships[0]; r.LagDays != 3 || !r.Conflict {
		t.Fatalf("c starts before its offset allows: %+v", r)
	}
	// C starts before its offset allows, by so many working days: that is
	// negative float for C's drivers, all the way back to A.
	if fa, fb := task(p, "a").FloatDays, task(p, "b").FloatDays; fa == nil || *fa >= 0 || *fa != *fb {
		t.Fatalf("a broken offset is negative float along the chain: %v %v", fa, fb)
	}
}

func TestRecalculateMovesDrivenTasksToWhatTheirRelationshipsAsk(t *testing.T) {
	svc, repo, board, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	board.items["a"].DueDate = day("2027-03-05")
	board.items["b"].DueDate = day("2027-03-03")
	board.items["c"].DueDate = day("2027-03-02")
	board.items["d"] = item("d", "D", workitems.ColumnDone, day("2027-03-02"), 3)
	repo.schedules["a"] = &TaskSchedule{WorkItemID: "a", ProjectID: "p", StartDate: sp("2027-03-01")}
	repo.schedules["b"] = &TaskSchedule{WorkItemID: "b", ProjectID: "p", StartDate: sp("2027-03-02")}
	repo.schedules["c"] = &TaskSchedule{WorkItemID: "c", ProjectID: "p", StartDate: sp("2027-03-01")}
	repo.schedules["d"] = &TaskSchedule{WorkItemID: "d", ProjectID: "p", StartDate: sp("2027-03-01")}
	repo.deps["b"] = []Dependency{{WorkItemID: "b", DependsOnID: "a", Type: RelStartToStart, LagDays: 5}}
	repo.deps["c"] = []Dependency{{WorkItemID: "c", DependsOnID: "b", Type: RelFinishToStart}}
	repo.deps["d"] = []Dependency{{WorkItemID: "d", DependsOnID: "a", Type: RelFinishToStart}}
	moved, err := svc.Recalculate("p", "tester", false, now)
	if err != nil || moved != 2 {
		t.Fatalf("moved %d: %v", moved, err)
	}
	at := func(id string) string {
		return *repo.schedules[id].StartDate + ".." + board.items[id].DueDate.Format(DateLayout)
	}
	// A is not driven: it stays. B starts 5 working days after A starts and
	// keeps its 2 days; C follows B's new end and keeps its 2; D is done.
	if at("a") != "2027-03-01..2027-03-05" || at("b") != "2027-03-08..2027-03-09" || at("c") != "2027-03-10..2027-03-11" || at("d") != "2027-03-01..2027-03-02" {
		t.Fatalf("a %s b %s c %s d %s", at("a"), at("b"), at("c"), at("d"))
	}
	if again, err := svc.Recalculate("p", "tester", false, now); err != nil || again != 0 {
		t.Fatalf("a plan that agrees with its relationships does not move: %d %v", again, err)
	}
}

func TestHolidaysElapsedTimeDeadlinesAndTheCriticalThreshold(t *testing.T) {
	week := []int{1, 2, 3, 4, 5}
	in := Inputs{
		WorkingDays: week,
		// Wednesday 3 March 2027 is a holiday.
		Holidays: []Holiday{{Date: "2027-03-03", Name: "Founders' day"}},
		Items: []*workitems.WorkItem{
			item("a", "A", workitems.ColumnTodo, day("2027-03-05"), 0),
			item("cure", "Cure", workitems.ColumnTodo, day("2027-03-14"), 1),
			item("b", "B", workitems.ColumnTodo, day("2027-03-19"), 2),
		},
		Schedules: []*TaskSchedule{
			{WorkItemID: "a", StartDate: sp("2027-03-01"), Deadline: sp("2027-03-04")},
			{WorkItemID: "cure", StartDate: sp("2027-03-06"), Elapsed: true},
			{WorkItemID: "b", StartDate: sp("2027-03-16"), Deadline: sp("2027-03-31")},
		},
		Dependencies: []Dependency{{WorkItemID: "b", DependsOnID: "cure", Type: RelFinishToStart}},
	}
	p := Build(in, now)
	// Monday to Friday less the holiday: four working days.
	if d := task(p, "a").DurationDays; *d != 4 {
		t.Fatalf("a holiday is not a working day: %d", *d)
	}
	// Saturday 6 to Sunday 14 March, counted as it elapses: nine days.
	if c := task(p, "cure"); *c.DurationDays != 9 || !c.Elapsed {
		t.Fatalf("an elapsed duration counts every day: %+v", *c.DurationDays)
	}
	if a, b := task(p, "a"), task(p, "b"); !a.DeadlineMissed || b.DeadlineMissed || *a.Deadline != "2027-03-04" {
		t.Fatalf("a ends after its deadline, b before: %v %v", a.DeadlineMissed, b.DeadlineMissed)
	}
	// Cure ends Sunday 14; B could start Monday 15 and starts Tuesday 16: a
	// day of float. It is on the path all the same, being what B waits for.
	if c := task(p, "cure"); *c.FloatDays != 1 || !c.Critical {
		t.Fatalf("cure has a day of float: %d %v", *c.FloatDays, c.Critical)
	}
	// A ends a day after its deadline: a day of negative float, which makes
	// it critical.
	if a := task(p, "a"); *a.FloatDays != -1 || !a.Critical {
		t.Fatalf("a missed deadline is negative float: %d %v", *a.FloatDays, a.Critical)
	}
	// Without the deadline nothing waits for A, which ends ten working days
	// before the plan does: critical only once the threshold reaches that.
	in.Schedules[0].Deadline = nil
	if a := task(Build(in, now), "a"); *a.FloatDays != 10 || a.Critical {
		t.Fatalf("a: %d %v", *a.FloatDays, a.Critical)
	}
	in.CriticalSlackDays = 10
	if a := task(Build(in, now), "a"); !a.Critical {
		t.Fatalf("a threshold of ten days makes it critical")
	}
	if len(p.Holidays) != 1 || p.Holidays[0].Name != "Founders' day" {
		t.Fatalf("the plan reports its holidays: %+v", p.Holidays)
	}
}

func TestOffsetUnitsAndStartToFinish(t *testing.T) {
	cal := NewCalendar([]int{1, 2, 3, 4, 5})
	// The driver runs Monday 1 to Friday 12 March 2027: ten working days.
	ask := func(kind string, lag int, unit string) string {
		return askedDate(cal, kind, lag, unit, "2027-03-01", "2027-03-12", 10)
	}
	for _, c := range []struct {
		kind string
		lag  int
		unit string
		want string
	}{
		{RelFinishToStart, 0, LagWorkingDays, "2027-03-15"},
		{RelFinishToStart, 2, LagWorkingDays, "2027-03-17"},
		// Two calendar days after Monday 15 is Wednesday 17; five is Saturday, so Monday 22.
		{RelFinishToStart, 5, LagElapsedDays, "2027-03-22"},
		// Half the driver's ten days after it starts.
		{RelStartToStart, 50, LagPercent, "2027-03-08"},
		{RelStartToStart, -1, LagWorkingDays, "2027-02-26"},
		{RelFinishToFinish, 0, LagWorkingDays, "2027-03-12"},
		// Start to finish: the driven task ends no earlier than the driver starts.
		{RelStartToFinish, 0, LagWorkingDays, "2027-03-01"},
		{RelStartToFinish, 3, LagWorkingDays, "2027-03-04"},
	} {
		if got := ask(c.kind, c.lag, c.unit); got != c.want {
			t.Errorf("%s %+d%s: %s, want %s", c.kind, c.lag, c.unit, got, c.want)
		}
	}
	svc, _, _, _ := newService()
	if err := svc.Relate("a", "b", RelStartToFinish, true, 5, "weeks"); !errors.Is(err, ErrInvalidLag) {
		t.Fatalf("an unknown unit: %v", err)
	}
	if err := svc.Relate("a", "b", RelStartToFinish, true, 25, LagPercent); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSettings("p", SettingsRequest{Holidays: &[]Holiday{{Date: "03/03/2027"}}}); !errors.Is(err, ErrHoliday) {
		t.Fatalf("a holiday not written as a date: %v", err)
	}
	slack := 400
	if _, err := svc.SetSettings("p", SettingsRequest{CriticalSlackDays: &slack}); !errors.Is(err, ErrCriticalSlack) {
		t.Fatalf("a threshold over a year: %v", err)
	}
	set, err := svc.SetSettings("p", SettingsRequest{Holidays: &[]Holiday{{Date: "2027-12-27", Name: "Shutdown"}, {Date: "2027-01-01"}}})
	if err != nil || len(set.Holidays) != 2 || set.Holidays[0].Date != "2027-01-01" || len(set.WorkingDays) == 0 {
		t.Fatalf("holidays come back in date order with the rest: %+v %v", set, err)
	}
}

func TestEarlyAndLateDatesConstraintsAndPathsPerChain(t *testing.T) {
	week := []int{1, 2, 3, 4, 5}
	in := Inputs{
		WorkingDays: week,
		Items: []*workitems.WorkItem{
			// 1 March 2027 is a Monday.
			item("a", "A", workitems.ColumnTodo, day("2027-03-05"), 0),
			item("b", "B", workitems.ColumnTodo, day("2027-03-12"), 1),
			item("solo", "Solo", workitems.ColumnTodo, day("2027-03-03"), 2),
			item("late", "Late", workitems.ColumnTodo, day("2027-03-19"), 3),
		},
		Schedules: []*TaskSchedule{
			{WorkItemID: "a", StartDate: sp("2027-03-01")},
			// B starts Wednesday 10: two working days after it could.
			{WorkItemID: "b", StartDate: sp("2027-03-10")},
			{WorkItemID: "solo", StartDate: sp("2027-03-01"), ConstraintType: ConstraintFNLT, ConstraintDate: sp("2027-03-02")},
			{WorkItemID: "late", StartDate: sp("2027-03-15"), ConstraintType: ConstraintSNET, ConstraintDate: sp("2027-03-17")},
		},
		Dependencies: []Dependency{{WorkItemID: "b", DependsOnID: "a", Type: RelFinishToStart}},
	}
	p := Build(in, now)
	b := task(p, "b")
	// B could start Monday 8 and finish Wednesday 10; the plan ends Friday
	// 19, five working days after B does.
	if *b.EarlyStart != "2027-03-08" || *b.EarlyFinish != "2027-03-10" || *b.LateFinish != "2027-03-19" || *b.FloatDays != 5 {
		t.Fatalf("b: early %s..%s late ..%s float %d", *b.EarlyStart, *b.EarlyFinish, *b.LateFinish, *b.FloatDays)
	}
	// A can slip two days before it pushes B, seven before the plan ends later.
	if a := task(p, "a"); *a.FreeFloatDays != 2 || *a.FloatDays != 7 || *a.LateStart != "2027-03-10" {
		t.Fatalf("a: free %d total %d late start %s", *a.FreeFloatDays, *a.FloatDays, *a.LateStart)
	}
	// Solo finishes a day after its "finish no later than": negative float, and broken.
	if s := task(p, "solo"); *s.FloatDays != -1 || !s.ConstraintViolated || !s.Critical {
		t.Fatalf("solo: %d %v %v", *s.FloatDays, s.ConstraintViolated, s.Critical)
	}
	if l := task(p, "late"); !l.ConstraintViolated || *l.EarlyStart != "2027-03-17" {
		t.Fatalf("late starts before its no-earlier-than date: %v %s", l.ConstraintViolated, *l.EarlyStart)
	}
	if p.Mode != ModeManual || p.ScheduleFrom != FromStart || b.Mode != ModeManual || b.ConstraintType != ConstraintASAP {
		t.Fatalf("defaults: %s %s %s %s", p.Mode, p.ScheduleFrom, b.Mode, b.ConstraintType)
	}
	// With a path per chain, B ends its own chain: no float, and A has only its gap.
	in.Scheduling.MultipleCriticalPaths = true
	p = Build(in, now)
	if b, a := task(p, "b"), task(p, "a"); *b.FloatDays != 0 || !b.Critical || *a.FloatDays != 2 {
		t.Fatalf("a path per chain: b %d a %d", *b.FloatDays, *a.FloatDays)
	}
	// A plan that must finish by Wednesday 17 is two days over.
	in.Scheduling = Scheduling{ProjectFinish: sp("2027-03-17")}
	if l := task(Build(in, now), "late"); *l.FloatDays != -2 {
		t.Fatalf("past the plan's finish date: %d", *l.FloatDays)
	}
}

func TestAutomaticTasksArePlacedAfterEveryChange(t *testing.T) {
	svc, repo, board, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	board.items["a"].DueDate = day("2027-03-05")
	repo.schedules["a"] = &TaskSchedule{WorkItemID: "a", ProjectID: "p", StartDate: sp("2027-03-01")}
	at := func(id string) string {
		sc := repo.schedules[id]
		if sc == nil || sc.StartDate == nil || board.items[id].DueDate == nil {
			return "unscheduled"
		}
		return *sc.StartDate + ".." + board.items[id].DueDate.Format(DateLayout)
	}
	// A manual plan: relating two tasks moves nothing.
	if err := svc.Relate("b", "a", RelFinishToStart, false, 0, ""); err != nil {
		t.Fatal(err)
	}
	if at("b") != "unscheduled" {
		t.Fatalf("a manual task stays where it is: %s", at("b"))
	}
	// Make the plan automatic: B, three days long, goes straight after A.
	three := 3
	if err := svc.Schedule("b", ScheduleRequest{DurationDays: &three}, "tester"); err != nil {
		t.Fatal(err)
	}
	auto := ModeAuto
	if _, err := svc.SetSettings("p", SettingsRequest{SchedulingMode: &auto}); err != nil {
		t.Fatal(err)
	}
	if at("b") != "2027-03-08..2027-03-10" {
		t.Fatalf("b follows a: %s", at("b"))
	}
	// Moving A moves B with it. A, typed to a date, is held there.
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-03")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if at("a") != "2027-03-03..2027-03-09" || at("b") != "2027-03-10..2027-03-12" || repo.schedules["a"].ConstraintType != ConstraintSNET {
		t.Fatalf("a %s b %s (%s)", at("a"), at("b"), repo.schedules["a"].ConstraintType)
	}
	// A task of its own mode: C is manual, and stays put though A drives it.
	manual := ModeManual
	if err := svc.Schedule("c", ScheduleRequest{StartDate: sp("2027-03-01"), DueDate: sp("2027-03-02"), Mode: &manual}, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Relate("c", "a", RelFinishToStart, false, 0, ""); err != nil {
		t.Fatal(err)
	}
	if at("c") != "2027-03-01..2027-03-02" {
		t.Fatalf("a manual task in an automatic plan stays: %s", at("c"))
	}
	// A must-start-on constraint decides B alone.
	mso := ConstraintMSO
	if err := svc.Schedule("b", ScheduleRequest{ConstraintType: &mso, ConstraintDate: sp("2027-03-22")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if at("b") != "2027-03-22..2027-03-24" {
		t.Fatalf("must start on: %s", at("b"))
	}
	bad := "sometime"
	if err := svc.Schedule("b", ScheduleRequest{ConstraintType: &bad}, "tester"); !errors.Is(err, ErrConstraint) {
		t.Fatalf("an unknown constraint: %v", err)
	}
	snet := ConstraintSNET
	if err := svc.Schedule("b", ScheduleRequest{ConstraintType: &snet, ConstraintDate: sp("")}, "tester"); !errors.Is(err, ErrConstraint) {
		t.Fatalf("a constraint without its date: %v", err)
	}
}

func TestRecalculateOnlyLaterAndAsLateAsPossible(t *testing.T) {
	svc, repo, board, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	board.items["a"].DueDate = day("2027-03-05")
	board.items["b"].DueDate = day("2027-03-17")
	board.items["c"].DueDate = day("2027-03-26")
	repo.schedules["a"] = &TaskSchedule{WorkItemID: "a", ProjectID: "p", StartDate: sp("2027-03-01")}
	// B already starts a week after A ends; C ends the plan.
	repo.schedules["b"] = &TaskSchedule{WorkItemID: "b", ProjectID: "p", StartDate: sp("2027-03-15")}
	repo.schedules["c"] = &TaskSchedule{WorkItemID: "c", ProjectID: "p", StartDate: sp("2027-03-22")}
	repo.deps["b"] = []Dependency{{WorkItemID: "b", DependsOnID: "a", Type: RelFinishToStart}}
	at := func(id string) string {
		return *repo.schedules[id].StartDate + ".." + board.items[id].DueDate.Format(DateLayout)
	}
	// Relationships as minimums: B is already late enough, and stays.
	if moved, err := svc.Recalculate("p", "tester", true, now); err != nil || moved != 0 || at("b") != "2027-03-15..2027-03-17" {
		t.Fatalf("only later: moved %d, b %s, %v", moved, at("b"), err)
	}
	// As late as possible: A slides up against B, which it drives.
	alap := ConstraintALAP
	if err := svc.Schedule("a", ScheduleRequest{ConstraintType: &alap}, "tester"); err != nil {
		t.Fatal(err)
	}
	if moved, err := svc.Recalculate("p", "tester", true, now); err != nil || moved != 1 || at("a") != "2027-03-08..2027-03-12" {
		t.Fatalf("as late as possible: moved %d, a %s, %v", moved, at("a"), err)
	}
	// Without "only later", B snaps back against A.
	if moved, err := svc.Recalculate("p", "tester", false, now); err != nil || at("b") != "2027-03-15..2027-03-17" {
		t.Fatalf("snap: moved %d, b %s, %v", moved, at("b"), err)
	}
}

func TestAssignmentsCostAndOverallocation(t *testing.T) {
	week := []int{1, 2, 3, 4, 5}
	ten := 10.0
	in := Inputs{
		WorkingDays: week,
		Items: []*workitems.WorkItem{
			// 1 March 2027 is a Monday.
			item("a", "A", workitems.ColumnTodo, day("2027-03-05"), 0),
			item("b", "B", workitems.ColumnTodo, day("2027-03-10"), 1),
			item("c", "C", workitems.ColumnTodo, day("2027-03-19"), 2),
		},
		Schedules: []*TaskSchedule{
			{WorkItemID: "a", StartDate: sp("2027-03-01"), EffortDays: &ten, FixedCost: 250},
			{WorkItemID: "b", StartDate: sp("2027-03-04")},
			{WorkItemID: "c", StartDate: sp("2027-03-15")},
		},
		Assignments: []Assignment{
			// Ann and Bob share A's ten days of effort three to one.
			{WorkItemID: "a", Type: "user", ID: "ann", Units: 75},
			{WorkItemID: "a", Type: "user", ID: "bob", Units: 25},
			// Ann is also on B at 50%: over her capacity while the two overlap.
			{WorkItemID: "b", Type: "user", ID: "ann", Units: 50},
		},
		Resources: []Resource{{Type: "user", ID: "ann", DailyRate: 400, Capacity: 100}, {Type: "user", ID: "bob", DailyRate: 200, Capacity: 100}},
		Currency:  "GBP",
	}
	uid := "cat"
	in.Items[2].AssigneeID = &uid
	p := Build(in, now)
	// A: Ann 7.5 days at 400, Bob 2.5 at 200, and 250 fixed.
	if a := task(p, "a"); a.Cost != 3750 || len(a.Assignments) != 2 || !a.Overallocated {
		t.Fatalf("a: cost %v, %+v, overallocated %v", a.Cost, a.Assignments, a.Overallocated)
	}
	// B runs five working days, half of Ann's: 2.5 days at 400.
	if b := task(p, "b"); b.Cost != 1000 || !b.Overallocated {
		t.Fatalf("b: %v %v", b.Cost, b.Overallocated)
	}
	// C has nobody set on the plan: the board's assignee does it, at no rate.
	if c := task(p, "c"); len(c.Assignments) != 1 || c.Assignments[0].ID != "cat" || c.Assignments[0].Units != 100 || c.Cost != 0 || c.Overallocated {
		t.Fatalf("c: %+v %v", c.Assignments, c.Cost)
	}
	var ann ResourceLoad
	for _, res := range p.Resources {
		if res.ID == "ann" {
			ann = res
		}
	}
	// Ann is at 125% on Thursday 4 and Friday 5 March.
	if ann.Peak != 125 || len(ann.Overallocations) != 1 || ann.Overallocations[0] != (Overallocation{Start: "2027-03-04", End: "2027-03-05", Units: 125}) || ann.WorkDays != 10 || ann.Cost != 4000 {
		t.Fatalf("ann: %+v", ann)
	}
	if p.Summary.Cost != 4750 || p.Summary.Overallocated != 1 || p.Currency != "GBP" || len(p.Resources) != 3 {
		t.Fatalf("summary: %+v, %d resources", p.Summary, len(p.Resources))
	}
	if _, err := CleanAssignments([]Assignment{{Type: "team", ID: "x"}}); !errors.Is(err, ErrAssignment) {
		t.Fatalf("an unknown kind of assignee: %v", err)
	}
	if got, _ := CleanAssignments([]Assignment{{Type: "User", ID: " ann "}, {Type: "user", ID: "ann", Units: 50}}); len(got) != 1 || got[0].Units != 100 {
		t.Fatalf("each assignee once, at 100%% unless said: %+v", got)
	}
}

func TestEffortDrivesDurationAndLevellingClearsOverallocation(t *testing.T) {
	svc, repo, board, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	at := func(id string) string {
		return *repo.schedules[id].StartDate + ".." + board.items[id].DueDate.Format(DateLayout)
	}
	// Six days of effort for two people at full time: three days.
	six, yes := 6.0, true
	two := []Assignment{{Type: "user", ID: "ann"}, {Type: "user", ID: "bob"}}
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-01"), EffortDays: &six, EffortDriven: &yes, Assignments: &two}, "tester"); err != nil {
		t.Fatal(err)
	}
	if at("a") != "2027-03-01..2027-03-03" || *repo.schedules["a"].DurationDays != 3 {
		t.Fatalf("effort driven: %s", at("a"))
	}
	// Bob leaves: the same effort now takes Ann six days.
	one := []Assignment{{Type: "user", ID: "ann"}}
	if err := svc.Schedule("a", ScheduleRequest{Assignments: &one}, "tester"); err != nil {
		t.Fatal(err)
	}
	if at("a") != "2027-03-01..2027-03-08" {
		t.Fatalf("one person: %s", at("a"))
	}
	// Ann is also on B, which overlaps A; C waits for B.
	if err := svc.Schedule("b", ScheduleRequest{StartDate: sp("2027-03-02"), DueDate: sp("2027-03-04"), Assignments: &one}, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Schedule("c", ScheduleRequest{StartDate: sp("2027-03-05"), DueDate: sp("2027-03-05")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Relate("c", "b", RelFinishToStart, false, 0, ""); err != nil {
		t.Fatal(err)
	}
	moved, err := svc.Level("p", "tester", now)
	if err != nil || moved != 2 {
		t.Fatalf("levelled %d: %v", moved, err)
	}
	// A, which ends the plan, has no float and stays; B goes after it and
	// takes C, which it drives, with it.
	if at("a") != "2027-03-01..2027-03-08" || at("b") != "2027-03-09..2027-03-11" || at("c") != "2027-03-12..2027-03-12" {
		t.Fatalf("a %s b %s c %s", at("a"), at("b"), at("c"))
	}
	if again, _ := svc.Level("p", "tester", now); again != 0 {
		t.Fatalf("a levelled plan stays: %d", again)
	}
	if _, err := svc.SetResource("p", Resource{Type: "user", ID: "ann", DailyRate: -1}); !errors.Is(err, ErrResource) {
		t.Fatalf("a negative rate: %v", err)
	}
	budget, money := 5000.0, "gbp"
	if set, err := svc.SetSettings("p", SettingsRequest{Budget: &budget, Currency: &money}); err != nil || set.Currency != "GBP" || *set.Budget != 5000 {
		t.Fatalf("budget and currency: %+v %v", set, err)
	}
}

func TestBaselinesVarianceActualsAndEarnedValue(t *testing.T) {
	svc, repo, board, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	ann := []Assignment{{Type: "user", ID: "ann"}}
	repo.resources = []Resource{{Type: "user", ID: "ann", DailyRate: 100, Capacity: 100}}
	// A runs Monday 1 to Friday 12 March 2027: ten days of Ann at 100 a day.
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-01"), DueDate: sp("2027-03-12"), Assignments: &ann}, "tester"); err != nil {
		t.Fatal(err)
	}
	b, err := svc.SaveBaseline("p", "", now)
	if err != nil || b.Name != "Baseline 2027-03-10" || len(b.Tasks) != 3 {
		t.Fatalf("baseline: %+v %v", b, err)
	}
	// A then slips two working days, and is reported 30% complete by hand.
	thirty := 30
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-03"), PercentComplete: &thirty, ActualStart: sp("2027-03-03")}, "tester"); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Plan("p", Evidence{}, now)
	if err != nil {
		t.Fatal(err)
	}
	a := task(p, "a")
	if a.Progress != 30 || a.PercentComplete == nil || *a.ActualStart != "2027-03-03" {
		t.Fatalf("a percentage typed by hand is the progress: %d %v", a.Progress, a.PercentComplete)
	}
	if a.Baseline == nil || *a.Baseline.Start != "2027-03-01" || *a.StartVarianceDays != 2 || *a.FinishVarianceDays != 2 {
		t.Fatalf("variance from the baseline: %+v %v %v", a.Baseline, a.StartVarianceDays, a.FinishVarianceDays)
	}
	// As of Wednesday 10 March the baseline had A 8 of 10 days in: 800
	// planned, 300 earned, 300 spent.
	ev := p.EarnedValue
	if ev.BAC != 1000 || ev.PV != 800 || ev.EV != 300 || ev.AC != 300 || ev.SV != -500 || ev.SPI != 0.38 || ev.CPI != 1 || !a.BehindSchedule {
		t.Fatalf("earned value: %+v behind %v", ev, a.BehindSchedule)
	}
	if p.StatusDay != "2027-03-10" || p.BaselineID == nil || *p.BaselineID != b.ID || len(p.Baselines) != 1 || p.Baselines[0].Tasks != 3 {
		t.Fatalf("the plan names its status day and baseline: %s %v %+v", p.StatusDay, p.BaselineID, p.Baselines)
	}
	// What it has really cost, and a status date of its own.
	cost := 450.0
	if err := svc.Schedule("a", ScheduleRequest{ActualCost: &cost}, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSettings("p", SettingsRequest{StatusDate: sp("2027-03-05")}); err != nil {
		t.Fatal(err)
	}
	p, _ = svc.Plan("p", Evidence{}, now)
	if ev := p.EarnedValue; ev.PV != 500 || ev.AC != 450 || ev.CV != -150 || ev.CPI != 0.67 || ev.EAC != 1492.54 {
		t.Fatalf("at the status date, with an actual cost: %+v", ev)
	}
	// Back to the computed progress.
	back, over := -1, 140
	if err := svc.Schedule("a", ScheduleRequest{PercentComplete: &back}, "tester"); err != nil || repo.schedules["a"].PercentComplete != nil {
		t.Fatalf("cleared: %v", err)
	}
	if err := svc.Schedule("a", ScheduleRequest{PercentComplete: &over}, "tester"); !errors.Is(err, ErrPercent) {
		t.Fatalf("140%%: %v", err)
	}
	if err := svc.Schedule("a", ScheduleRequest{ActualStart: sp("2027-03-09"), ActualFinish: sp("2027-03-08")}, "tester"); !errors.Is(err, ErrDateOrder) {
		t.Fatalf("finished before it started: %v", err)
	}
	// B should have started on Monday 1 March and has not: it goes to the
	// status date, Friday 5.
	if err := svc.Schedule("b", ScheduleRequest{StartDate: sp("2027-03-01"), DueDate: sp("2027-03-02")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if moved, err := svc.RescheduleRemaining("p", "tester", now); err != nil || moved != 1 || *repo.schedules["b"].StartDate != "2027-03-05" ||
		board.items["b"].DueDate.Format(DateLayout) != "2027-03-08" {
		t.Fatalf("rescheduled %d: %v, b %s", moved, err, *repo.schedules["b"].StartDate)
	}
	if err := svc.DeleteBaseline("p", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a baseline it has not: %v", err)
	}
	if err := svc.DeleteBaseline("p", b.ID); err != nil || len(repo.baselines) != 0 {
		t.Fatalf("deleted: %v", err)
	}
}

func TestTasksKeepTheOrderTheyArePutIn(t *testing.T) {
	svc, repo, _, _ := newService()
	outline := func() string {
		p, err := svc.Plan("p", Evidence{}, now)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, task := range p.Tasks {
			out += task.WBS + task.ID + " "
		}
		return out
	}
	// By start, then by age: A has dates, B and C none.
	if got := outline(); got != "1a 2b 3c " {
		t.Fatalf("to begin with: %s", got)
	}
	if err := svc.Schedule("c", ScheduleRequest{AfterID: sp("")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := outline(); got != "1c 2a 3b " {
		t.Fatalf("C first: %s", got)
	}
	if err := svc.Schedule("a", ScheduleRequest{AfterID: sp("b")}, "tester"); err != nil {
		t.Fatal(err)
	}
	// A new start date does not move a task once its siblings are ordered.
	if err := svc.Schedule("b", ScheduleRequest{StartDate: sp("2020-01-01"), DueDate: sp("2020-01-02")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := outline(); got != "1c 2b 3a " {
		t.Fatalf("A after B: %s", got)
	}
	if err := svc.Schedule("a", ScheduleRequest{AfterID: sp("nope")}, "tester"); !errors.Is(err, ErrPosition) {
		t.Fatalf("after a task that is not a sibling: %v", err)
	}
	// Indented under C, A loses its place among the top-level tasks; B
	// follows it there and goes after it.
	if err := svc.Schedule("a", ScheduleRequest{ParentID: sp("c")}, "tester"); err != nil || repo.schedules["a"].SortOrder != nil {
		t.Fatalf("indented: %v %v", err, repo.schedules["a"].SortOrder)
	}
	if err := svc.Schedule("b", ScheduleRequest{ParentID: sp("c"), AfterID: sp("a")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := outline(); got != "1c 1.1a 1.2b " {
		t.Fatalf("both under C: %s", got)
	}
	// Outdented again, after C.
	if err := svc.Schedule("a", ScheduleRequest{ParentID: sp(""), AfterID: sp("c")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if got := outline(); got != "1c 1.1b 2a " {
		t.Fatalf("A outdented: %s", got)
	}
}

func TestPDFLinksCrossPages(t *testing.T) {
	// Sixty tasks, each driven by the one before and every tenth by the
	// first: more than a page, so some lines leave one page for another.
	in := Inputs{ProjectID: "p", WorkingDays: everyDay}
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("t%02d", i)
		start := now.AddDate(0, 0, i*2)
		due := start.AddDate(0, 0, 3)
		s := start.Format(DateLayout)
		in.Items = append(in.Items, item(id, "Task "+id, workitems.ColumnTodo, &due, i))
		in.Schedules = append(in.Schedules, &TaskSchedule{WorkItemID: id, ProjectID: "p", StartDate: &s})
		if i > 0 {
			in.Dependencies = append(in.Dependencies, Dependency{WorkItemID: id, DependsOnID: fmt.Sprintf("t%02d", i-1), Type: RelFinishToStart})
		}
		if i > 0 && i%10 == 0 {
			in.Dependencies = append(in.Dependencies, Dependency{WorkItemID: id, DependsOnID: "t00", Type: RelStartToStart})
		}
	}
	doc := Document{ProjectName: "Long", ExportedAt: now, Plan: Build(in, now)}
	out, err := renderPDF(doc)
	if err != nil || len(out) < 1000 {
		t.Fatalf("render: %d bytes, %v", len(out), err)
	}
	if path := os.Getenv("PLAN_PDF_OUT"); path != "" {
		_ = os.WriteFile(path, out, 0o644)
	}
}

func TestMicrosoftProjectXMLRoundTrip(t *testing.T) {
	svc, repo, _, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-01"), DueDate: sp("2027-03-05")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Schedule("b", ScheduleRequest{StartDate: sp("2027-03-08"), DueDate: sp("2027-03-10"), ParentID: sp("c"),
		Relationships: &[]Relationship{{TaskID: "a", Type: RelStartToStart, LagDays: 5}}}, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMilestone("p", MilestoneRequest{Name: sp("Gate"), TargetDate: sp("2027-03-12")}); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Plan("p", Evidence{}, now)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Render(Document{ProjectName: "Rig", ExportedAt: now, Plan: p}, FormatMSProject)
	if err != nil || out.Filename[len(out.Filename)-4:] != ".xml" {
		t.Fatalf("export: %v %+v", err, out)
	}
	xmlText := string(out.Data)
	for _, want := range []string{`<Project xmlns="http://schemas.microsoft.com/project">`, "<Name>A</Name>", "<Start>2027-03-01T08:00:00</Start>", "<Duration>PT40H0M0S</Duration>",
		"<OutlineNumber>2.1</OutlineNumber>", "<Type>3</Type>", "<LinkLag>24000</LinkLag>", "<Milestone>1</Milestone>", "<Summary>1</Summary>"} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("the export lacks %s:\n%s", want, xmlText)
		}
	}
	// Read back into an empty project: the same outline, dates and link.
	svc2, repo2, board2, _ := newService()
	repo2.days = []int{1, 2, 3, 4, 5}
	board2.items = map[string]*workitems.WorkItem{}
	res, err := svc2.ImportMSProject("p", out.Data, nil, "tester")
	if err != nil || res != (ImportResult{Tasks: 3, Milestones: 1, Relationships: 1}) {
		t.Fatalf("import: %+v %v", res, err)
	}
	got, _ := svc2.Plan("p", Evidence{}, now)
	line := ""
	for _, task := range got.Tasks {
		line += task.WBS + " " + task.Title + " "
		if task.SpanStart != nil {
			line += *task.SpanStart + ".." + *task.SpanEnd + " "
		}
		for _, r := range task.Relationships {
			line += fmt.Sprintf("%s+%d%s ", r.Type, r.LagDays, r.LagUnit)
		}
	}
	if line != "1 A 2027-03-01..2027-03-05 2 C 2027-03-08..2027-03-10 2.1 B 2027-03-08..2027-03-10 SS+5d " || len(got.Milestones) != 1 || got.Milestones[0].Name != "Gate" {
		t.Fatalf("read back: %q %+v", line, got.Milestones)
	}
	if _, err := svc2.ImportMSProject("p", []byte("<Project></Project>"), nil, "tester"); !errors.Is(err, ErrImport) {
		t.Fatalf("a file with no tasks: %v", err)
	}
	if _, err := svc2.ImportMSProject("p", []byte("not xml"), nil, "tester"); !errors.Is(err, ErrImport) {
		t.Fatalf("not XML: %v", err)
	}
}

func TestATaskOfAnotherProjectDrivesOne(t *testing.T) {
	svc, repo, _, _ := newService()
	repo.days = []int{1, 2, 3, 4, 5}
	if err := svc.Schedule("a", ScheduleRequest{StartDate: sp("2027-03-08"), DueDate: sp("2027-03-10")}, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RelateExternal("a", "b", "", 0, "", "tester"); !errors.Is(err, ErrSameProject) {
		t.Fatalf("two tasks of one project: %v", err)
	}
	if err := svc.RelateExternal("a", "nope", "", 0, "", "tester"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("a task that is not: %v", err)
	}
	// X, of another project, runs 22 to 24 March: A, which starts on the
	// 8th, breaks a finish to start from it until the plan is recalculated.
	if err := svc.RelateExternal("a", "x", "fs", 1, "", "tester"); err != nil {
		t.Fatal(err)
	}
	p, _ := svc.Plan("p", Evidence{}, now)
	a := task(p, "a")
	if len(a.ExternalLinks) != 1 || !a.ExternalLinks[0].Conflict || a.ExternalLinks[0].ProjectName != "Other" || a.ExternalLinks[0].Type != RelFinishToStart || len(a.Relationships) != 0 {
		t.Fatalf("the link on A: %+v", a.ExternalLinks)
	}
	if moved, err := svc.Recalculate("p", "tester", false, now); err != nil || moved != 1 || *repo.schedules["a"].StartDate != "2027-03-26" {
		t.Fatalf("recalculated %d: %v, a starts %s", moved, err, *repo.schedules["a"].StartDate)
	}
	p, _ = svc.Plan("p", Evidence{}, now)
	if task(p, "a").ExternalLinks[0].Conflict {
		t.Fatal("kept once A follows it")
	}
	if err := svc.UnrelateExternal("a", "x"); err != nil || len(repo.external) != 0 {
		t.Fatalf("removed: %v", err)
	}
	if err := svc.UnrelateExternal("a", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed twice: %v", err)
	}
}
