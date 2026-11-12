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
	// LagDays is the relationship's offset: the driven task keeps that much
	// after what the type asks for, or before when negative, in LagUnit
	// ("d" working days, "ed" elapsed days, "%" of the driver's duration).
	LagDays int    `json:"lag_days"`
	LagUnit string `json:"lag_unit"`
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
	// Elapsed: the duration counts calendar days, not working days.
	Elapsed bool `json:"elapsed"`
	// Deadline is the day the task should be finished by; DeadlineMissed
	// says it is not done and ends, or has run, past it.
	Deadline       *string `json:"deadline"`
	DeadlineMissed bool    `json:"deadline_missed"`
	// ModeOwn is the task's own scheduling mode, empty when it takes the
	// plan's; Mode is the one in force, "manual" or "auto".
	ModeOwn string `json:"mode_own"`
	Mode    string `json:"mode"`
	// ConstraintType and ConstraintDate hold the task to a date;
	// ConstraintViolated says its dates do not keep to it.
	ConstraintType     string  `json:"constraint_type"`
	ConstraintDate     *string `json:"constraint_date"`
	ConstraintViolated bool    `json:"constraint_violated"`
	// EarlyStart and EarlyFinish are the earliest the task's relationships,
	// its constraint and the plan's start allow; LateStart and LateFinish
	// the latest before the plan would end later (its dates plus its
	// float). FreeFloatDays is how far it can slip before it moves a task
	// it drives. All nil for a task with no dates.
	EarlyStart    *string `json:"early_start"`
	EarlyFinish   *string `json:"early_finish"`
	LateStart     *string `json:"late_start"`
	LateFinish    *string `json:"late_finish"`
	FreeFloatDays *int    `json:"free_float_days"`
	// Assignments are who does the task and for what share of their time:
	// the ones set on the plan, or the board's assignee at 100%.
	Assignments []Assignment `json:"assignments"`
	// FixedCost is the task's cost beyond people's time; Cost is the whole:
	// its people's work at their rates, its fixed cost, and its sub-tasks'.
	FixedCost float64 `json:"fixed_cost"`
	Cost      float64 `json:"cost"`
	// EffortDriven: effort and the people on it decide the duration.
	EffortDriven bool `json:"effort_driven"`
	// Overallocated: someone on the task is asked for more than their
	// capacity while it runs.
	Overallocated bool `json:"overallocated"`
	// ActualStart and ActualFinish are when the task really started and
	// finished; ActualCost what it has cost. PercentComplete is the
	// percentage typed by hand, nil when progress is the computed one.
	ActualStart     *string  `json:"actual_start"`
	ActualFinish    *string  `json:"actual_finish"`
	PercentComplete *int     `json:"percent_complete"`
	ActualCost      *float64 `json:"actual_cost"`
	// Baseline is the task as the plan's active baseline has it, nil when
	// it is not in it. The variances are the working days its start and its
	// end have moved since: positive is later.
	Baseline           *BaselineTask `json:"baseline"`
	StartVarianceDays  *int          `json:"start_variance_days"`
	FinishVarianceDays *int          `json:"finish_variance_days"`
	// EarnedValue is the task's at the status date; nil for a task with
	// sub-tasks. BehindSchedule: it has earned less than was planned by then.
	EarnedValue    *EarnedValue `json:"earned_value"`
	BehindSchedule bool         `json:"behind_schedule"`
	// FloatUntil is the last day the task's end can slip to before it
	// pushes a task it drives; nil when it drives none or has no room.
	FloatUntil *string `json:"float_until"`
	// CriticalSubtasks: the critical path runs through this task's
	// sub-tasks, at any depth; a folded row shows it for them.
	CriticalSubtasks bool `json:"critical_subtasks"`
	// Progress is a whole percentage: 100 for a task in Done, else the
	// share of its to-dos ticked off and its sub-tasks' progress.
	Progress     int  `json:"progress"`
	Done         bool `json:"done"`
	Late         bool `json:"late"`
	TodosDone    int  `json:"todos_done"`
	TodosTotal   int  `json:"todos_total"`
	SubtasksDone int  `json:"subtasks_done"`
	// Milestone is the key of the milestone the task counts toward: the one
	// it is assigned to, or (MilestoneInherited) the one a task above it is.
	Milestone          *string    `json:"milestone"`
	MilestoneInherited bool       `json:"milestone_inherited"`
	SubtasksTotal      int        `json:"subtasks_total"`
	Todos              []TaskTodo `json:"todos"`
	DependsOn          []string   `json:"depends_on"`
	// Relationships are the tasks that drive this one, each with its type
	// and whether the dates break it; DependsOn is the same tasks' ids.
	Relationships []TaskRelationship `json:"relationships"`
	// Drives are the same relationships seen from the other end: the tasks
	// this one drives, each with its type and whether the dates break it.
	Drives []TaskRelationship `json:"drives"`
	// Conflicts are the tasks this one depends on that finish on or after
	// the day it starts.
	Conflicts []string `json:"conflicts"`
	// ExternalLinks are the task's relationships to tasks of other projects.
	ExternalLinks []ExternalLink `json:"external_links"`

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
	// Cost is what the plan costs: every task's, added up.
	Cost float64 `json:"cost"`
	// Overallocated counts the people and agents asked for more than
	// their capacity on some day.
	Overallocated int `json:"overallocated"`
}

// Plan is a project's plan as computed for a reader.
type Plan struct {
	// off is the days each resource is away, for the placing that follows.
	off map[string]map[string]bool

	ProjectID string `json:"project_id"`
	// WorkingDays are the days of the week the plan works, as ISO weekday
	// numbers (Monday is 1, Sunday 7): the days durations and float count.
	WorkingDays []int `json:"working_days"`
	// Holidays are the days the plan does not work whatever the weekday.
	Holidays []Holiday `json:"holidays"`
	// CriticalSlackDays: a task with that much float or less is critical.
	CriticalSlackDays int `json:"critical_slack_days"`
	// Scheduling is how the plan is scheduled: its mode, its start and
	// finish, and whether each chain has its own critical path.
	Scheduling
	// Resources are the people and agents with work on the plan, or a rate
	// or capacity set, with their load. People lists who a task can be
	// assigned to, filled in by whoever serves the plan.
	Resources []ResourceLoad `json:"resources"`
	People    []Person       `json:"people"`
	Currency  string         `json:"currency"`
	Budget    *float64       `json:"budget"`
	// Tracking is the status date and the baseline chosen; StatusDay the
	// day progress is reported as of (the status date, or today);
	// BaselineID the baseline the tasks are compared with; Baselines every
	// one the plan keeps; EarnedValue the plan's at the status day.
	Tracking
	StatusDay   string         `json:"status_day"`
	BaselineID  *string        `json:"baseline_id"`
	Baselines   []BaselineInfo `json:"baselines"`
	EarnedValue EarnedValue    `json:"earned_value"`
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
	// External are the project's links to tasks of other projects.
	External []ExternalLink
	// TodoTasks maps an attached to-do to its task.
	TodoTasks map[string]string
	// WorkingDays are the plan's working days as ISO weekday numbers; nil
	// for the default, Monday to Friday.
	WorkingDays []int
	// Holidays are days off whatever the weekday; CriticalSlackDays is the
	// float at or below which a task is critical.
	Holidays          []Holiday
	CriticalSlackDays int
	// Scheduling is how the plan is scheduled; the zero value is manual,
	// from its start.
	Scheduling Scheduling
	// Assignments are who each task is assigned to; Resources the rates and
	// capacities the project keeps; Currency and Budget its money.
	Assignments []Assignment
	Resources   []Resource
	// TimeOff is the days the workspace's people and agents are away.
	TimeOff  []TimeOff
	Currency string
	Budget   *float64
	// Baselines are the plan's snapshots, oldest first; Tracking says which
	// one it is compared with, and as of which day.
	Baselines  []*Baseline
	Tracking   Tracking
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
	set, err := s.settings(projectID)
	if err != nil {
		return nil, err
	}
	in.WorkingDays, in.Holidays, in.CriticalSlackDays, in.Scheduling = set.WorkingDays, set.Holidays, set.CriticalSlackDays, set.Scheduling
	in.Currency, in.Budget, in.Tracking = set.Currency, set.Budget, set.Tracking
	if in.Baselines, err = s.repo.ListBaselines(projectID); err != nil {
		return nil, err
	}
	if in.External, err = s.repo.ListExternalLinks(projectID); err != nil {
		return nil, err
	}
	if in.TimeOff, err = s.repo.ListTimeOff(projectID); err != nil {
		return nil, err
	}
	if in.Assignments, err = s.repo.ListAssignments(projectID); err != nil {
		return nil, err
	}
	if in.Resources, err = s.repo.ListResources(projectID); err != nil {
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
	b := &builder{in: in, today: today, cal: NewCalendar(in.WorkingDays).WithHolidays(in.Holidays), byID: map[string]*Task{}, created: map[string]time.Time{}, order: map[string]int{}, children: map[string][]*Task{}, off: away(in.TimeOff), own: map[string][]Assignment{}}
	for _, a := range in.Assignments {
		b.own[a.WorkItemID] = append(b.own[a.WorkItemID], Assignment{Type: a.Type, ID: a.ID, Units: a.Units})
	}
	p.off = b.off
	p.WorkingDays, p.Holidays, p.CriticalSlackDays = DefaultWorkingDays, []Holiday{}, in.CriticalSlackDays
	p.Scheduling = in.Scheduling
	if p.Mode == "" {
		p.Mode = ModeManual
	}
	if p.ScheduleFrom == "" {
		p.ScheduleFrom = FromStart
	}
	if in.Holidays != nil {
		p.Holidays = in.Holidays
	}
	if clean, err := CleanWorkingDays(in.WorkingDays); err == nil {
		p.WorkingDays = clean
	}
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
	b.external()
	b.criticalPath(p)
	b.summary(p)
	b.resources(p)
	for _, res := range p.Resources {
		if len(res.Overallocations) > 0 {
			p.Summary.Overallocated++
		}
	}
	p.Currency, p.Budget, p.People = in.Currency, in.Budget, []Person{}
	b.tracking(p)

	own := map[string]string{}
	for _, sc := range in.Schedules {
		if sc.MilestoneKey != nil {
			own[sc.WorkItemID] = *sc.MilestoneKey
		}
	}
	counted := assignMilestones(p.Tasks, own)
	p.Milestones = milestones(in, p.Tasks, counted, today)
	onPlan := map[string]bool{}
	for _, m := range p.Milestones {
		onPlan[m.Key] = true
	}
	var assigned []*Task
	for _, t := range p.Tasks {
		// A key whose milestone has gone is no milestone.
		if t.Milestone != nil && !onPlan[*t.Milestone] {
			t.Milestone, t.MilestoneInherited = nil, false
		}
		if t.Milestone != nil && t.SubtasksTotal == 0 {
			assigned = append(assigned, t)
		}
	}
	for _, m := range p.Milestones {
		p.Start, p.End = minDate(p.Start, m.Date), maxDate(p.End, m.Date)
	}
	whole := indexEvidence(in.Evidence).measure(nil)
	whole[MetricTasksDone] = tasksDone(p.Tasks, nil)
	whole[MetricMilestoneTasks] = milestoneTasks(assigned)
	p.Metrics = make([]MetricValue, 0, len(Metrics))
	for _, m := range Metrics {
		p.Metrics = append(p.Metrics, whole[m.Key])
	}
	return p
}

// builder holds a plan's tasks while Build works them out.
type builder struct {
	in      Inputs
	today   string
	cal     Calendar
	byID    map[string]*Task
	created map[string]time.Time
	// order is a task's place among its siblings, where one was moved.
	order    map[string]int
	children map[string][]*Task
	// off is the days each resource is away; own, the people set on each task.
	off   map[string]map[string]bool
	own   map[string][]Assignment
	roots []*Task
}

// tasks makes a task of each work item and gives it its schedule.
func (b *builder) tasks() {
	for _, it := range b.in.Items {
		if it == nil {
			continue
		}
		b.byID[it.ID] = &Task{ID: it.ID, Title: it.Title, Column: it.Column, AssigneeType: it.AssigneeType, AssigneeID: it.AssigneeID,
			End: dayOf(it.DueDate), Done: it.Column == workitems.ColumnDone,
			Todos: []TaskTodo{}, ExternalLinks: []ExternalLink{}, DependsOn: []string{}, Relationships: []TaskRelationship{}, Drives: []TaskRelationship{}, Conflicts: []string{}}
		b.created[it.ID] = it.CreatedAt
	}
	mode := b.in.Scheduling.Mode
	if mode == "" {
		mode = ModeManual
	}
	for _, t := range b.byID {
		t.Mode, t.ConstraintType = mode, ConstraintASAP
		// Until a task has a schedule of its own, the plan's default says
		// whether its effort decides its duration.
		t.EffortDriven = b.in.Scheduling.EffortDrivenDefault
	}
	for _, sc := range b.in.Schedules {
		t := b.byID[sc.WorkItemID]
		if t == nil {
			continue
		}
		t.Start = sc.StartDate
		if sc.SortOrder != nil {
			b.order[sc.WorkItemID] = *sc.SortOrder
		}
		t.DurationDays, t.EffortDays = sc.DurationDays, sc.EffortDays
		t.Elapsed, t.Deadline = sc.Elapsed, sc.Deadline
		t.ModeOwn, t.ConstraintType, t.ConstraintDate = sc.Mode, sc.ConstraintType, sc.ConstraintDate
		t.FixedCost, t.EffortDriven = sc.FixedCost, sc.EffortDriven
		t.ActualStart, t.ActualFinish, t.PercentComplete, t.ActualCost = sc.ActualStart, sc.ActualFinish, sc.PercentComplete, sc.ActualCost
		if t.ModeOwn != "" {
			t.Mode = t.ModeOwn
		}
		if t.ConstraintType == "" {
			t.ConstraintType = ConstraintASAP
		}
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

// who is the people on a task: those set on the plan, or else the board's
// assignee.
func (b *builder) who(t *Task) []Assignment {
	if own := b.own[t.ID]; len(own) > 0 {
		return own
	}
	if t.AssigneeID != nil && *t.AssigneeID != "" {
		return []Assignment{{Type: t.AssigneeType, ID: *t.AssigneeID, Units: 100}}
	}
	return nil
}

// task is the plan's task with the id, or nil.
func (p *Plan) task(id string) *Task {
	for _, t := range p.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// settle works out a task's span and progress, its sub-tasks' first.
func (b *builder) settle(t *Task) {
	start, end := t.Start, t.End
	// A task with sub-tasks is a folder for them: it has no dates, duration
	// or effort of its own, only what they add up to.
	if len(b.children[t.ID]) > 0 {
		start, end, t.Start, t.End, t.DurationDays, t.EffortDays = nil, nil, nil, nil, nil, nil
	}
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
		if c.EffortDays != nil {
			total := *c.EffortDays
			if t.EffortDays != nil {
				total += *t.EffortDays
			}
			t.EffortDays = &total
		}
	}
	if start == nil {
		start = end
	}
	if end == nil {
		end = start
	}
	t.SpanStart, t.SpanEnd = start, end
	cal := b.cal
	if len(b.children[t.ID]) == 0 {
		cal = taskCalendar(b.cal, t.Elapsed, b.who(t), b.off)
	}
	if d := spanDays(cal, start, end); d != nil {
		t.DurationDays = d
	}
	t.ConstraintViolated = constraintViolated(t.ConstraintType, t.ConstraintDate, start, end)
	t.DeadlineMissed = t.Deadline != nil && !t.Done && ((end != nil && *end > *t.Deadline) || b.today > *t.Deadline)
	switch {
	case t.Done:
		t.fraction = 1
	case t.PercentComplete != nil && len(b.children[t.ID]) == 0:
		// Typed by hand: it stands in for the to-dos.
		t.fraction = float64(*t.PercentComplete) / 100
	case units > 0:
		t.fraction = sum / units
	}
	t.Progress = percentOf(t.fraction)
	t.Late = !t.Done && t.SpanEnd != nil && *t.SpanEnd < b.today
}

// outline lists the tasks as a tree: siblings in the order they were put
// in, where one was moved; otherwise by start, the unscheduled last, then
// by age.
func (b *builder) outline(p *Plan, list []*Task, depth int, prefix string) {
	sort.SliceStable(list, func(i, j int) bool {
		x, y := list[i], list[j]
		ox, placedX := b.order[x.ID]
		oy, placedY := b.order[y.ID]
		switch {
		case placedX != placedY:
			return placedX
		case placedX && ox != oy:
			return ox < oy
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
		d.LagUnit = lagUnit(d.LagUnit)
		conflict := b.broken(d, t, on)
		t.DependsOn = append(t.DependsOn, on.ID)
		t.Relationships = append(t.Relationships, TaskRelationship{TaskID: on.ID, Type: d.Type, LagDays: d.LagDays, LagUnit: d.LagUnit, Conflict: conflict})
		on.Drives = append(on.Drives, TaskRelationship{TaskID: t.ID, Type: d.Type, LagDays: d.LagDays, LagUnit: d.LagUnit, Conflict: conflict})
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
		for _, rels := range [][]TaskRelationship{t.Relationships, t.Drives} {
			sort.SliceStable(rels, func(i, j int) bool { return row[rels[i].TaskID] < row[rels[j].TaskID] })
		}
	}
}

// broken reports whether t's dates break the relationship d to on: t
// starts, or ends, before the day the relationship asks for. Unscheduled
// ends break nothing, and neither does a driving task that is done.
func (b *builder) broken(d Dependency, t, on *Task) bool {
	if on.Done || t.SpanStart == nil || on.SpanStart == nil {
		return false
	}
	return b.gap(on, drive{t, d.Type, d.LagDays, d.LagUnit}) < 0
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
		if t.EffortDays != nil && t.SubtasksTotal == 0 {
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

// constraintViolated reports whether a task's dates break its constraint.
// A task with no dates breaks none.
func constraintViolated(kind string, date, start, end *string) bool {
	if date == nil || start == nil || end == nil {
		return false
	}
	switch kind {
	case ConstraintSNET:
		return *start < *date
	case ConstraintSNLT:
		return *start > *date
	case ConstraintFNET:
		return *end < *date
	case ConstraintFNLT:
		return *end > *date
	case ConstraintMSO:
		return *start != *date
	case ConstraintMFO:
		return *end != *date
	}
	return false
}
