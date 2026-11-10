package plan

import (
	"bytes"
	"errors"
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
	if p.Summary != (Summary{Tasks: 5, Done: 1, Late: 1, Unscheduled: 1, Progress: 26}) {
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
	schedules  map[string]*TaskSchedule
	deps       map[string][]string
	todoTasks  map[string]string
	milestones map[string]*Milestone
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{schedules: map[string]*TaskSchedule{}, deps: map[string][]string{}, todoTasks: map[string]string{}, milestones: map[string]*Milestone{}}
}

func (r *fakeRepo) ListSchedules(string) ([]*TaskSchedule, error) {
	out := []*TaskSchedule{}
	for _, s := range r.schedules {
		out = append(out, s)
	}
	return out, nil
}
func (r *fakeRepo) SaveSchedule(s *TaskSchedule) error { r.schedules[s.WorkItemID] = s; return nil }
func (r *fakeRepo) ListDependencies(string) ([]Dependency, error) {
	out := []Dependency{}
	for id, on := range r.deps {
		for _, d := range on {
			out = append(out, Dependency{WorkItemID: id, DependsOnID: d})
		}
	}
	return out, nil
}
func (r *fakeRepo) SetDependencies(_, id string, on []string) error { r.deps[id] = on; return nil }
func (r *fakeRepo) ListTodoTasks(string) (map[string]string, error) { return r.todoTasks, nil }
func (r *fakeRepo) AttachTodo(_, todoID, id string) error           { r.todoTasks[todoID] = id; return nil }
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
	if got := repo.deps["b"]; len(got) != 1 || got[0] != "c" {
		t.Fatalf("dependencies are deduplicated: %v", got)
	}
	ok("c", ScheduleRequest{ParentID: sp("b")})
	fails("a loop of parents", ScheduleRequest{ParentID: sp("c")}, "a", ErrParentCycle)
	fails("a loop of dependencies", ScheduleRequest{DependsOn: &[]string{"b"}}, "c", ErrDependencyCycle)

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
	todo, err := svc.AddTodo("a", "org", "  Crimp the connectors ", sp("me"))
	if err != nil {
		t.Fatal(err)
	}
	if todo.Text != "Crimp the connectors" || *todo.ProjectID != "p" || repo.todoTasks[todo.ID] != "a" {
		t.Fatalf("%+v on %v", todo, repo.todoTasks)
	}
	if _, err := svc.AddTodo("a", "org", " ", nil); !errors.Is(err, todos.ErrTextRequired) || len(td.made) != 1 {
		t.Fatalf("an empty to-do: %v", err)
	}
	if _, err := svc.AddTodo("nope", "org", "x", nil); !errors.Is(err, ErrTaskNotFound) {
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
		`1,"'=Frame, ""welded""",Done,,2027-03-01,2027-03-20,100,`,
		"1.1,Wiring,In Progress,Sam,2027-03-06,2027-03-20,0,0,0,0,0,1,false",
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
