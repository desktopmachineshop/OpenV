package plan

import (
	"sort"
	"time"

	"github.com/openv/requirements-platform/internal/domain/targetreleases"
	"github.com/openv/requirements-platform/internal/domain/todos"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// Milestone kinds.
const (
	KindRelease = "release"
	KindCustom  = "custom"
)

// TaskTodo is a to-do on a task, as the plan shows it.
type TaskTodo struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
	// DueDate is the to-do's one date, its deadline (YYYY-MM-DD): a to-do
	// is a small job with no start or duration of its own.
	DueDate *string `json:"due_date"`
}

// TaskRelationship is one relationship of a task, as the plan shows it.
type TaskRelationship struct {
	TaskID string `json:"task_id"`
	Type   string `json:"type"`
	// Conflict: the dates break the relationship. Finish to start: this
	// task starts on or before the day the other ends. Start to start: it
	// starts before the other starts. Finish to finish: it ends before the
	// other ends. A relationship to a task that is done is never broken.
	Conflict bool `json:"conflict"`
}

// Task is one row of the plan: a work item with its schedule and progress.
type Task struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Column       string  `json:"column"`
	AssigneeType string  `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	ParentID     *string `json:"parent_id"`
	// Depth is how many tasks sit above this one; WBS its outline number
	// ("2.1").
	Depth int    `json:"depth"`
	WBS   string `json:"wbs"`
	// Start and End are the task's own dates; End is the work item's due
	// date.
	Start *string `json:"start"`
	End   *string `json:"end"`
	// SpanStart and SpanEnd are what the chart draws: the task's own dates,
	// widened to cover its sub-tasks, and each standing in for the other
	// when only one is known. Both nil: the task is not scheduled.
	SpanStart *string `json:"span_start"`
	SpanEnd   *string `json:"span_end"`
	// DurationDays is the task's length, counting its first and last day:
	// what its span covers, or the duration it keeps while not scheduled.
	DurationDays *int `json:"duration_days"`
	// EffortDays is the work in the task, in person-days.
	EffortDays *float64 `json:"effort_days"`
	// FloatDays is how many days the task can slip before the plan's last
	// task ends later; nil for a task with no dates. Critical: it is 0 and
	// the task is not done, so any slip moves the plan's end.
	FloatDays *int `json:"float_days"`
	Critical  bool `json:"critical"`
	// Progress is a whole percentage: 100 for a task in Done, else the
	// share of its to-dos ticked off and its sub-tasks' progress.
	Progress      int        `json:"progress"`
	Done          bool       `json:"done"`
	Late          bool       `json:"late"`
	TodosDone     int        `json:"todos_done"`
	TodosTotal    int        `json:"todos_total"`
	SubtasksDone  int        `json:"subtasks_done"`
	SubtasksTotal int        `json:"subtasks_total"`
	Todos         []TaskTodo `json:"todos"`
	DependsOn     []string   `json:"depends_on"`
	// Relationships are the tasks that drive this one, each with its type
	// and whether the dates break it; DependsOn is the same tasks' ids.
	Relationships []TaskRelationship `json:"relationships"`
	// Conflicts are the tasks this one depends on that finish on or after
	// the day it starts.
	Conflicts []string `json:"conflicts"`

	fraction float64
}

// CriterionResult is one criterion measured.
type CriterionResult struct {
	Metric  string `json:"metric"`
	Label   string `json:"label"`
	Target  int    `json:"target"`
	Actual  int    `json:"actual"`
	Count   int    `json:"count"`
	Total   int    `json:"total"`
	Met     bool   `json:"met"`
	Percent int    `json:"percent"`
}

// MilestoneProgress is a milestone on the plan with its criteria measured.
type MilestoneProgress struct {
	// Key names the milestone on the plan: its id, or "release:<id>" for a
	// release's.
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// ID is the stored milestone's id: empty for a release milestone whose
	// criteria are still the defaults.
	ID          string  `json:"id"`
	ReleaseID   *string `json:"release_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Date        *string `json:"date"`
	// ReleaseStatus is the release's status, for a release milestone.
	ReleaseStatus string `json:"release_status,omitempty"`
	// DefaultCriteria says the criteria are the defaults, not ones set.
	DefaultCriteria bool              `json:"default_criteria"`
	Criteria        []CriterionResult `json:"criteria"`
	// Progress is the mean of each criterion's way to its target, as a whole
	// percentage; 0 with no criteria. Met: every criterion reached its
	// target (and there is at least one), or the release is released.
	Progress int  `json:"progress"`
	Met      bool `json:"met"`
	Late     bool `json:"late"`
}

// Summary counts the plan's tasks.
type Summary struct {
	Tasks       int `json:"tasks"`
	Done        int `json:"done"`
	Late        int `json:"late"`
	Unscheduled int `json:"unscheduled"`
	// Progress is the mean progress of the top-level tasks.
	Progress int `json:"progress"`
	// Critical counts the tasks on the critical path.
	Critical int `json:"critical"`
	// EffortDays is the effort of every task, added up.
	EffortDays float64 `json:"effort_days"`
}

// Plan is a project's plan as computed for a reader.
type Plan struct {
	ProjectID string `json:"project_id"`
	// Today is the day the plan was computed for (UTC), which decides late.
	Today string `json:"today"`
	// Start and End span every dated task and milestone.
	Start        *string             `json:"start"`
	End          *string             `json:"end"`
	Tasks        []*Task             `json:"tasks"`
	Milestones   []MilestoneProgress `json:"milestones"`
	Dependencies []Dependency        `json:"dependencies"`
	// Metrics are every metric measured across the whole project, for the
	// criteria chooser.
	Metrics   []MetricValue `json:"metrics"`
	Catalogue []Metric      `json:"metric_catalogue"`
	Summary   Summary       `json:"summary"`
}

// Inputs is everything a plan is computed from.
type Inputs struct {
	ProjectID    string
	Items        []*workitems.WorkItem
	Todos        []*todos.Todo
	Schedules    []*TaskSchedule
	Dependencies []Dependency
	// TodoTasks maps an attached to-do to its task.
	TodoTasks  map[string]string
	Milestones []*Milestone
	Releases   []*targetreleases.Release
	Evidence   Evidence
}

// Plan loads the project's board, to-dos, schedule and milestones and
// computes its plan against the evidence.
func (s *DefaultService) Plan(projectID string, ev Evidence, now time.Time) (*Plan, error) {
	in := Inputs{ProjectID: projectID, Evidence: ev}
	var err error
	if in.Items, err = s.board.ListByProject(projectID); err != nil {
		return nil, err
	}
	if s.todos != nil {
		if in.Todos, err = s.todos.ListByProject(projectID); err != nil {
			return nil, err
		}
	}
	if in.Schedules, err = s.repo.ListSchedules(projectID); err != nil {
		return nil, err
	}
	if in.Dependencies, err = s.repo.ListDependencies(projectID); err != nil {
		return nil, err
	}
	if in.TodoTasks, err = s.repo.ListTodoTasks(projectID); err != nil {
		return nil, err
	}
	if in.Milestones, err = s.repo.ListMilestones(projectID); err != nil {
		return nil, err
	}
	if s.releases != nil {
		if in.Releases, err = s.releases.List(projectID); err != nil {
			return nil, err
		}
	}
	return Build(in, now), nil
}

func minDate(a, b *string) *string {
	if a == nil || (b != nil && *b < *a) {
		return b
	}
	return a
}

func maxDate(a, b *string) *string {
	if a == nil || (b != nil && *b > *a) {
		return b
	}
	return a
}

func percentOf(f float64) int {
	p := int(f*100 + 0.5)
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	return p
}

// Build computes a plan. It reads nothing but its inputs.
func Build(in Inputs, now time.Time) *Plan {
	today := now.UTC().Format(DateLayout)
	p := &Plan{ProjectID: in.ProjectID, Today: today, Tasks: []*Task{}, Milestones: []MilestoneProgress{},
		Dependencies: []Dependency{}, Catalogue: Metrics}
	b := &builder{in: in, today: today, byID: map[string]*Task{}, created: map[string]time.Time{}, children: map[string][]*Task{}}
	b.tasks()
	b.todos()
	for _, t := range b.byID {
		if t.ParentID != nil {
			b.children[*t.ParentID] = append(b.children[*t.ParentID], t)
		} else {
			b.roots = append(b.roots, t)
		}
	}
	for _, r := range b.roots {
		b.settle(r)
	}
	b.outline(p, b.roots, 0, "")
	b.dependencies(p)
	b.criticalPath(p)
	b.summary(p)

	p.Milestones = milestones(in, p.Tasks, today)
	for _, m := range p.Milestones {
		p.Start, p.End = minDate(p.Start, m.Date), maxDate(p.End, m.Date)
	}
	whole := indexEvidence(in.Evidence).measure(nil)
	whole[MetricTasksDone] = tasksDone(p.Tasks, nil)
	p.Metrics = make([]MetricValue, 0, len(Metrics))
	for _, m := range Metrics {
		p.Metrics = append(p.Metrics, whole[m.Key])
	}
	return p
}

// builder holds a plan's tasks while Build works them out.
type builder struct {
	in       Inputs
	today    string
	byID     map[string]*Task
	created  map[string]time.Time
	children map[string][]*Task
	roots    []*Task
}

// tasks makes a task of each work item and gives it its schedule.
func (b *builder) tasks() {
	for _, it := range b.in.Items {
		if it == nil {
			continue
		}
		b.byID[it.ID] = &Task{ID: it.ID, Title: it.Title, Column: it.Column, AssigneeType: it.AssigneeType, AssigneeID: it.AssigneeID,
			End: dayOf(it.DueDate), Done: it.Column == workitems.ColumnDone,
			Todos: []TaskTodo{}, DependsOn: []string{}, Relationships: []TaskRelationship{}, Conflicts: []string{}}
		b.created[it.ID] = it.CreatedAt
	}
	for _, sc := range b.in.Schedules {
		t := b.byID[sc.WorkItemID]
		if t == nil {
			continue
		}
		t.Start = sc.StartDate
		t.DurationDays, t.EffortDays = sc.DurationDays, sc.EffortDays
		if sc.ParentID != nil && b.byID[*sc.ParentID] != nil && *sc.ParentID != t.ID {
			t.ParentID = sc.ParentID
		}
	}
	// A parent chain that loops (it cannot be written, but a row may have
	// been) is cut at the task that closes it.
	for _, t := range b.byID {
		seen := map[string]bool{t.ID: true}
		for cur := t; cur.ParentID != nil; cur = b.byID[*cur.ParentID] {
			if seen[*cur.ParentID] {
				cur.ParentID = nil
				break
			}
			seen[*cur.ParentID] = true
		}
	}
}

// todos puts each attached to-do on its task, the open ones first.
func (b *builder) todos() {
	for _, td := range b.in.Todos {
		if td == nil {
			continue
		}
		t := b.byID[b.in.TodoTasks[td.ID]]
		if t == nil {
			continue
		}
		t.Todos = append(t.Todos, TaskTodo{ID: td.ID, Text: td.Text, Done: td.Done, DueDate: dayOf(td.DueDate)})
		t.TodosTotal++
		if td.Done {
			t.TodosDone++
		}
	}
	for _, t := range b.byID {
		sort.SliceStable(t.Todos, func(i, j int) bool { return !t.Todos[i].Done && t.Todos[j].Done })
	}
}

// settle works out a task's span and progress, its sub-tasks' first.
func (b *builder) settle(t *Task) {
	start, end := t.Start, t.End
	units, sum := float64(t.TodosTotal), float64(t.TodosDone)
	for _, c := range b.children[t.ID] {
		b.settle(c)
		start, end = minDate(start, c.SpanStart), maxDate(end, c.SpanEnd)
		t.SubtasksTotal++
		if c.Done {
			t.SubtasksDone++
		}
		units++
		sum += c.fraction
	}
	if start == nil {
		start = end
	}
	if end == nil {
		end = start
	}
	t.SpanStart, t.SpanEnd = start, end
	if d := spanDays(start, end); d != nil {
		t.DurationDays = d
	}
	switch {
	case t.Done:
		t.fraction = 1
	case units > 0:
		t.fraction = sum / units
	}
	t.Progress = percentOf(t.fraction)
	t.Late = !t.Done && t.SpanEnd != nil && *t.SpanEnd < b.today
}

// outline lists the tasks as a tree: by start, the unscheduled last, then
// by age.
func (b *builder) outline(p *Plan, list []*Task, depth int, prefix string) {
	sort.SliceStable(list, func(i, j int) bool {
		x, y := list[i], list[j]
		switch {
		case x.SpanStart != nil && y.SpanStart == nil:
			return true
		case x.SpanStart == nil && y.SpanStart != nil:
			return false
		case x.SpanStart != nil && *x.SpanStart != *y.SpanStart:
			return *x.SpanStart < *y.SpanStart
		}
		if !b.created[x.ID].Equal(b.created[y.ID]) {
			return b.created[x.ID].Before(b.created[y.ID])
		}
		return x.ID < y.ID
	})
	for i, t := range list {
		t.Depth = depth
		t.WBS = prefix + itoa(i+1)
		p.Tasks = append(p.Tasks, t)
		b.outline(p, b.children[t.ID], depth+1, t.WBS+".")
	}
}

// dependencies gives each task what it waits on, and which of those end too
// late for it.
func (b *builder) dependencies(p *Plan) {
	for _, d := range b.in.Dependencies {
		t, on := b.byID[d.WorkItemID], b.byID[d.DependsOnID]
		if t == nil || on == nil {
			continue
		}
		if !ValidRelationshipType(d.Type) {
			d.Type = RelFinishToStart
		}
		conflict := relationshipBroken(d.Type, t, on)
		t.DependsOn = append(t.DependsOn, on.ID)
		t.Relationships = append(t.Relationships, TaskRelationship{TaskID: on.ID, Type: d.Type, Conflict: conflict})
		p.Dependencies = append(p.Dependencies, d)
		if conflict {
			t.Conflicts = append(t.Conflicts, on.ID)
		}
	}
	// In outline order, so the answer does not depend on the ids.
	row := make(map[string]int, len(p.Tasks))
	for i, t := range p.Tasks {
		row[t.ID] = i
	}
	sort.SliceStable(p.Dependencies, func(i, j int) bool {
		x, y := p.Dependencies[i], p.Dependencies[j]
		if x.WorkItemID != y.WorkItemID {
			return row[x.WorkItemID] < row[y.WorkItemID]
		}
		return row[x.DependsOnID] < row[y.DependsOnID]
	})
	byRow := func(ids []string) {
		sort.SliceStable(ids, func(i, j int) bool { return row[ids[i]] < row[ids[j]] })
	}
	for _, t := range p.Tasks {
		byRow(t.DependsOn)
		byRow(t.Conflicts)
		rels := t.Relationships
		sort.SliceStable(rels, func(i, j int) bool { return row[rels[i].TaskID] < row[rels[j].TaskID] })
	}
}

// relationshipBroken reports whether t's dates break its relationship of
// the given type to on. Unscheduled ends break nothing, and neither does a
// task that is done.
func relationshipBroken(kind string, t, on *Task) bool {
	if on.Done || t.SpanStart == nil || on.SpanStart == nil {
		return false
	}
	switch kind {
	case RelStartToStart:
		return *t.SpanStart < *on.SpanStart
	case RelFinishToFinish:
		return *t.SpanEnd < *on.SpanEnd
	}
	return *t.SpanStart <= *on.SpanEnd
}

// criticalPath gives each scheduled task its float and marks the critical
// ones. The plan's dates are the team's, so this is the float of the plan as
// it stands: a task can slip until the plan's last day, or until it would
// push a task it drives, whichever comes first.
//
//	finish to start:  the driven task's start, less a day, bounds the end
//	start to start:   the driven task's start bounds the start
//	finish to finish: the driven task's end bounds the end
//
// A task with no float left, which is not done, is critical: any slip moves
// the end of the plan. A broken relationship leaves no float.
func (b *builder) criticalPath(p *Plan) {
	var last *string
	for _, t := range p.Tasks {
		last = maxDate(last, t.SpanEnd)
	}
	if last == nil {
		return
	}
	type drive struct {
		task *Task
		kind string
	}
	drives := map[string][]drive{}
	for _, d := range p.Dependencies {
		if t := b.byID[d.WorkItemID]; t != nil && t.SpanStart != nil {
			drives[d.DependsOnID] = append(drives[d.DependsOnID], drive{t, d.Type})
		}
	}
	float := map[string]int{}
	visiting := map[string]bool{}
	var slack func(t *Task) int
	slack = func(t *Task) int {
		if f, ok := float[t.ID]; ok {
			return f
		}
		f := daysBetween(*t.SpanEnd, *last)
		if visiting[t.ID] {
			return f
		}
		visiting[t.ID] = true
		for _, d := range drives[t.ID] {
			var gap int
			switch d.kind {
			case RelStartToStart:
				gap = daysBetween(*t.SpanStart, *d.task.SpanStart)
			case RelFinishToFinish:
				gap = daysBetween(*t.SpanEnd, *d.task.SpanEnd)
			default:
				gap = daysBetween(*t.SpanEnd, *d.task.SpanStart) - 1
			}
			if g := gap + slack(d.task); g < f {
				f = g
			}
		}
		if f < 0 {
			f = 0
		}
		visiting[t.ID] = false
		float[t.ID] = f
		return f
	}
	for _, t := range p.Tasks {
		if t.SpanStart == nil {
			continue
		}
		f := slack(t)
		t.FloatDays = &f
		t.Critical = f == 0 && !t.Done
	}
}

// summary counts the tasks and spans their dates.
func (b *builder) summary(p *Plan) {
	var rootSum float64
	for _, t := range p.Tasks {
		p.Summary.Tasks++
		if t.Done {
			p.Summary.Done++
		}
		if t.Late {
			p.Summary.Late++
		}
		if t.Critical {
			p.Summary.Critical++
		}
		if t.EffortDays != nil {
			p.Summary.EffortDays += *t.EffortDays
		}
		if t.SpanStart == nil {
			p.Summary.Unscheduled++
		}
		if t.Depth == 0 {
			rootSum += t.fraction
		}
		p.Start, p.End = minDate(p.Start, t.SpanStart), maxDate(p.End, t.SpanEnd)
	}
	if len(b.roots) > 0 {
		p.Summary.Progress = percentOf(rootSum / float64(len(b.roots)))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

// milestones measures every milestone: each release of the project, then
// the custom ones, in date order with the undated last.
func milestones(in Inputs, tasks []*Task, today string) []MilestoneProgress {
	ix := indexEvidence(in.Evidence)
	whole := ix.measure(nil)
	order := targetreleases.NewOrder(in.Releases)
	ranges := targetreleases.Resolve(in.Evidence.Artifacts, order)

	stored := map[string]*Milestone{}
	out := []MilestoneProgress{}
	for _, m := range in.Milestones {
		if m.ReleaseID != nil {
			stored[*m.ReleaseID] = m
		}
	}
	position := map[string]int{}

	measure := func(mp *MilestoneProgress, criteria []Criterion, values map[string]MetricValue) {
		measureMilestone(mp, criteria, values, tasks)
	}

	for _, rel := range in.Releases {
		id := rel.ID
		mp := MilestoneProgress{Key: "release:" + id, Kind: KindRelease, ReleaseID: &id, Name: rel.Name,
			Description: rel.Description, Date: rel.TargetDate, ReleaseStatus: rel.Status}
		criteria := DefaultReleaseCriteria()
		mp.DefaultCriteria = true
		if m := stored[id]; m != nil {
			mp.ID, mp.DefaultCriteria, criteria = m.ID, false, m.Criteria
		}
		scope := map[string]bool{}
		idx := order[id]
		for artifactID, r := range ranges {
			if r.Contains(order, idx) {
				scope[artifactID] = true
			}
		}
		measure(&mp, criteria, ix.measure(scope))
		if rel.Status == targetreleases.StatusReleased {
			mp.Met, mp.Progress = true, 100
		}
		position[mp.Key] = idx
		out = append(out, mp)
	}
	for _, m := range in.Milestones {
		if m.ReleaseID != nil {
			continue
		}
		mp := MilestoneProgress{Key: m.ID, Kind: KindCustom, ID: m.ID, Name: m.Name, Description: m.Description, Date: m.TargetDate}
		measure(&mp, m.Criteria, whole)
		out = append(out, mp)
	}
	for i := range out {
		out[i].Late = !out[i].Met && out[i].Date != nil && *out[i].Date < today
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Date != nil && b.Date == nil:
			return true
		case a.Date == nil && b.Date != nil:
			return false
		case a.Date != nil && *a.Date != *b.Date:
			return *a.Date < *b.Date
		}
		if a.Kind != b.Kind {
			return a.Kind == KindRelease
		}
		if a.Kind == KindRelease {
			return position[a.Key] < position[b.Key]
		}
		return a.Name < b.Name
	})
	return out
}

// measureMilestone measures a milestone's criteria against values, and
// MetricTasksDone against the tasks due by its date.
func measureMilestone(mp *MilestoneProgress, criteria []Criterion, values map[string]MetricValue, tasks []*Task) {
	mp.Criteria = []CriterionResult{}
	var sum float64
	met := len(criteria) > 0
	for _, c := range criteria {
		v, ok := values[c.Metric]
		if c.Metric == MetricTasksDone {
			v, ok = tasksDone(tasks, mp.Date), true
		}
		if !ok {
			continue
		}
		r := CriterionResult{Metric: c.Metric, Label: v.Label, Target: c.Target, Actual: v.Percent, Count: v.Count, Total: v.Total}
		// Count against the target without the rounding of Percent, so
		// 199 of 200 is not 100%.
		r.Met = v.Total > 0 && v.Count*100 >= c.Target*v.Total
		way := 0.0
		if v.Total > 0 {
			way = float64(v.Count) * 100 / float64(v.Total) / float64(c.Target)
		}
		if way > 1 {
			way = 1
		}
		r.Percent = percentOf(way)
		if !r.Met && r.Percent == 100 {
			r.Percent = 99
		}
		sum += way
		met = met && r.Met
		mp.Criteria = append(mp.Criteria, r)
	}
	if len(mp.Criteria) > 0 {
		mp.Progress = percentOf(sum / float64(len(mp.Criteria)))
		if !met && mp.Progress == 100 {
			mp.Progress = 99
		}
	}
	mp.Met = met
}
