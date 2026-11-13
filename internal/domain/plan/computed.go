// The plan as it is computed for a reader: a task, its to-dos and
// relationships, the plan's figures, and what it is computed from.

package plan

import (
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
	// Order is the task's place among its siblings in the outline, from 1.
	Order int `json:"order"`
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
