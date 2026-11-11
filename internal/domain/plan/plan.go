// Package plan implements a project's plan: the Gantt over the board.
//
// A plan's tasks are the board's work items (internal/domain/workitems), so
// a task has one title, one assignee and one due date wherever it is read.
// The plan adds what a schedule needs and the board does not hold: a start
// date, a parent task (a sub-task), finish-to-start dependencies between
// tasks, and which task a to-do (internal/domain/todos) belongs to.
//
// Its milestones are of two kinds. A release milestone is a target release
// (internal/domain/targetreleases) seen on the plan: every release of the
// project is one, with the release's own name, date and status, so the plan
// and the Releases page never disagree. A custom milestone is one the team
// adds ("Definition complete"). Either kind carries completion criteria,
// each a metric read from the project's artifacts with a target percentage
// ("user needs addressed: 100%"); see metrics.go.
//
// Progress is computed, never typed in: a task's from its to-dos and its
// sub-tasks, a milestone's from its criteria (build.go).
package plan

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/targetreleases"
	"github.com/openv/requirements-platform/internal/domain/todos"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// DateLayout is how the plan writes a day.
const DateLayout = "2006-01-02"

// MaxCriteria is the most criteria one milestone may carry.
const MaxCriteria = 12

// Errors.
var (
	ErrNotFound          = errors.New("milestone not found")
	ErrTaskNotFound      = errors.New("task not found")
	ErrNameRequired      = errors.New("milestone name is required")
	ErrInvalidDate       = errors.New("a date must be written YYYY-MM-DD")
	ErrDateOrder         = errors.New("a task cannot start after it is due")
	ErrParentSelf        = errors.New("a task cannot be its own sub-task")
	ErrParentCycle       = errors.New("a task cannot be a sub-task of one of its own sub-tasks")
	ErrOtherProject      = errors.New("the task is not on this project's board")
	ErrDependsOnSelf     = errors.New("a task cannot depend on itself")
	ErrDependencyCycle   = errors.New("these dependencies would make a loop")
	ErrInvalidDuration   = errors.New("a duration is a whole number of days, 1 or more")
	ErrInvalidEffort     = errors.New("effort is a number of days, 0 or more")
	ErrWorkingDays       = errors.New("working days are weekday numbers from 1 (Monday) to 7 (Sunday), at least one")
	ErrInvalidLag        = errors.New("a relationship's offset is a whole number, at most 3650 either way, in working days (d), elapsed days (ed) or percent of the driving task's duration (%)")
	ErrHoliday           = errors.New("a holiday is a date written YYYY-MM-DD with a name of at most 80 characters, and a plan has at most 366")
	ErrSchedulingMode    = errors.New("a scheduling mode is manual or auto, and a plan is scheduled from its start or its finish")
	ErrConstraint        = errors.New("a constraint is asap, alap, snet, snlt, fnet, fnlt, mso or mfo, and every one but asap and alap needs a date")
	ErrCriticalSlack     = errors.New("the critical threshold is a whole number of working days from 0 to 365")
	ErrMilestone         = errors.New("the milestone is not one of this project's")
	ErrParentDates       = errors.New("a task with sub-tasks takes its dates, duration and effort from them")
	ErrRelationshipType  = errors.New("a relationship is FS (finish to start), SS (start to start), FF (finish to finish) or SF (start to finish)")
	ErrUnknownMetric     = errors.New("unknown criterion metric")
	ErrInvalidTarget     = errors.New("a criterion's target is a percentage from 1 to 100")
	ErrTooManyCriteria   = errors.New("a milestone carries at most 12 criteria")
	ErrDuplicateMetric   = errors.New("a milestone names each metric once")
	ErrReleaseMilestone  = errors.New("a release milestone's name and date are its release's: edit the release")
	ErrUnsupportedFormat = errors.New("unsupported plan export format")
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// TaskSchedule is what the plan stores about one work item.
type TaskSchedule struct {
	WorkItemID string
	ProjectID  string
	// StartDate is YYYY-MM-DD, or nil when the task has no start yet.
	StartDate *string
	// ParentID is the task this one is a sub-task of.
	ParentID *string
	// DurationDays is how long the task runs, counting its first and last
	// day; nil when it has never been set or derived.
	DurationDays *int
	// EffortDays is the work in the task, in person-days.
	EffortDays *float64
	// Elapsed: the duration counts calendar days, weekends and holidays
	// included, as for a cure or a delivery.
	Elapsed bool
	// Deadline is the day the task should be finished by, YYYY-MM-DD; it
	// moves nothing, and the plan flags a task that misses it.
	Deadline *string
	// Mode is the task's own scheduling mode, ModeManual or ModeAuto; empty
	// takes the plan's.
	Mode string
	// ConstraintType and ConstraintDate hold the task to a date: see the
	// Constraint constants.
	ConstraintType string
	ConstraintDate *string
	// MilestoneKey is the milestone the task is assigned to, by the key the
	// plan gives it: a stored milestone's id, or "release:<id>".
	MilestoneKey *string
}

// The types of a relationship between two tasks, as a Gantt names them.
const (
	// RelFinishToStart: the task starts after the other finishes.
	RelFinishToStart = "FS"
	// RelStartToStart: the task starts no earlier than the other starts.
	RelStartToStart = "SS"
	// RelFinishToFinish: the task finishes no earlier than the other finishes.
	RelFinishToFinish = "FF"
	// RelStartToFinish: the task finishes no earlier than the other starts.
	RelStartToFinish = "SF"
)

// RelationshipTypes lists the types in the order a chooser shows them.
var RelationshipTypes = []string{RelFinishToStart, RelStartToStart, RelFinishToFinish, RelStartToFinish}

// ValidRelationshipType reports whether t names a relationship type.
func ValidRelationshipType(t string) bool {
	return t == RelFinishToStart || t == RelStartToStart || t == RelFinishToFinish || t == RelStartToFinish
}

// Dependency is a relationship between two tasks: WorkItemID is driven by
// DependsOnID in the way Type says. A task may have several, so two tasks
// can together drive when a third starts.
type Dependency struct {
	WorkItemID  string `json:"work_item_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
	// LagDays is the offset: what the driven task keeps after what the
	// type asks for, or before it when negative, in LagUnit.
	LagDays int `json:"lag_days"`
	// LagUnit is the offset's unit: working days ("d", the default),
	// elapsed days ("ed") or percent of the driving task's duration ("%").
	LagUnit string `json:"lag_unit"`
}

// Relationship names, in a schedule request, a task this one is driven by
// and how. An empty Type is finish to start.
type Relationship struct {
	TaskID string `json:"task_id"`
	Type   string `json:"type"`
	// LagDays is the relationship's offset; negative leads. LagUnit is its
	// unit: "d" (working days, the default), "ed" or "%".
	LagDays int    `json:"lag_days"`
	LagUnit string `json:"lag_unit"`
}

// Criterion is one completion criterion of a milestone: a metric and the
// percentage it must reach.
type Criterion struct {
	Metric string `json:"metric"`
	Target int    `json:"target"`
}

// Milestone is a stored milestone: a custom one, or the criteria of a
// release's (ReleaseID set; Name and TargetDate are then the release's and
// are not stored here).
type Milestone struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	ReleaseID   *string     `json:"release_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	TargetDate  *string     `json:"target_date"`
	Criteria    []Criterion `json:"criteria"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// MilestoneRequest creates or changes a custom milestone. On an update a
// nil field is left as it is; an empty TargetDate clears the date.
type MilestoneRequest struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	TargetDate  *string      `json:"target_date"`
	Criteria    *[]Criterion `json:"criteria"`
}

// ScheduleRequest changes a task's place on the plan. A nil field is left
// as it is; an empty StartDate, DueDate or ParentID clears it.
// Relationships replaces the task's relationships. DependsOn is the earlier
// form of the same list, without types: a task it names keeps the type it
// has, and a new one is finish to start. Relationships wins when both are
// sent.
//
// A task keeps its duration: a StartDate sent alone moves the due date with
// it, a DueDate sent alone moves the start, and a DurationDays sent alone
// moves the due date (or the start, for a task with only a due date). Both
// dates sent together set the duration to what they span. A DurationDays or
// EffortDays of 0 clears it.
type ScheduleRequest struct {
	StartDate    *string  `json:"start_date"`
	DueDate      *string  `json:"due_date"`
	DurationDays *int     `json:"duration_days"`
	EffortDays   *float64 `json:"effort_days"`
	ParentID     *string  `json:"parent_id"`
	// Milestone is the key of the milestone the task is assigned to; empty
	// takes it off its milestone.
	Milestone *string `json:"milestone"`
	// Elapsed makes the duration count calendar days.
	Elapsed *bool `json:"elapsed"`
	// Deadline is YYYY-MM-DD; empty clears it.
	Deadline *string `json:"deadline"`
	// Mode is "manual", "auto" or empty for the plan's.
	Mode *string `json:"mode"`
	// ConstraintType is one of the Constraint constants; ConstraintDate is
	// YYYY-MM-DD, needed by every type but asap and alap.
	ConstraintType *string         `json:"constraint_type"`
	ConstraintDate *string         `json:"constraint_date"`
	DependsOn      *[]string       `json:"depends_on"`
	Relationships  *[]Relationship `json:"relationships"`
}

// Repository stores what the plan adds to the board.
type Repository interface {
	ListSchedules(projectID string) ([]*TaskSchedule, error)
	// SaveSchedule inserts or replaces a task's schedule.
	SaveSchedule(s *TaskSchedule) error
	ListDependencies(projectID string) ([]Dependency, error)
	// Settings returns the plan's calendar, critical threshold and
	// scheduling settings; WorkingDays is nil for a project that never set
	// them. SaveSettings stores them whole.
	Settings(projectID string) (Settings, error)
	SaveSettings(projectID string, s Settings) error
	// SetDependencies replaces a task's relationships; each entry's
	// DependsOnID and Type are read.
	SetDependencies(projectID, workItemID string, relationships []Dependency) error
	// ListTodoTasks maps each of the project's attached to-dos to its task.
	ListTodoTasks(projectID string) (map[string]string, error)
	AttachTodo(projectID, todoID, workItemID string) error
	ListMilestones(projectID string) ([]*Milestone, error)
	// GetMilestone returns ErrNotFound when no milestone has the id.
	GetMilestone(id string) (*Milestone, error)
	CreateMilestone(m *Milestone) error
	UpdateMilestone(m *Milestone) error
	DeleteMilestone(id string) error
}

// Board is the part of the work item service the plan uses.
type Board interface {
	Get(id string) (*workitems.WorkItem, error)
	ListByProject(projectID string) ([]*workitems.WorkItem, error)
	Update(id string, req workitems.UpdateWorkItemRequest, actor string) (*workitems.WorkItem, error)
}

// TodoStore is the part of the to-do service the plan uses.
type TodoStore interface {
	ListByProject(projectID string) ([]*todos.Todo, error)
	Create(req todos.CreateRequest, createdBy *string) (*todos.Todo, error)
}

// Releases is the part of the target release service the plan uses.
type Releases interface {
	List(projectID string) ([]*targetreleases.Release, error)
	Get(id string) (*targetreleases.Release, error)
}

// Service is the plan's domain logic.
type Service interface {
	// Plan computes the project's plan from the board, the to-dos, the
	// releases and the evidence the caller read from the project.
	Plan(projectID string, ev Evidence, now time.Time) (*Plan, error)
	// Schedule changes a task's dates, parent and dependencies.
	Schedule(workItemID string, req ScheduleRequest, actor string) error
	// SetWorkingDays sets which days of the week the plan works.
	SetWorkingDays(projectID string, days []int) ([]int, error)
	// SetSettings changes the plan's calendar and critical threshold: the
	// fields the request names.
	SetSettings(projectID string, req SettingsRequest) (Settings, error)
	// Relate makes a relationship between two tasks of one project, from
	// either end: drives says the task drives the other one (else it is
	// driven by it). Any relationship the pair already has is replaced.
	Relate(taskID, otherID, kind string, drives bool, lagDays int, lagUnit string) error
	// Recalculate moves each task that another drives to the dates its
	// relationships ask for, and answers how many tasks it moved.
	Recalculate(projectID, actor string, onlyLater bool, now time.Time) (int, error)
	// Unrelate removes the relationship between two tasks, whichever way
	// it runs.
	Unrelate(taskID, otherID string) error
	// AddTodo raises a to-do on a task.
	AddTodo(workItemID, orgID, text string, due *time.Time, createdBy *string) (*todos.Todo, error)
	// TaskTodos lists the to-dos on a task, open ones first.
	TaskTodos(workItemID string) ([]*todos.Todo, error)
	GetMilestone(id string) (*Milestone, error)
	CreateMilestone(projectID string, req MilestoneRequest) (*Milestone, error)
	UpdateMilestone(id string, req MilestoneRequest) (*Milestone, error)
	DeleteMilestone(id string) error
	// SetReleaseCriteria sets the completion criteria of a release's
	// milestone.
	SetReleaseCriteria(projectID, releaseID string, criteria []Criterion) (*Milestone, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo     Repository
	board    Board
	todos    TodoStore
	releases Releases
	now      func() time.Time
}

// NewDefaultService builds the service. todoStore and releases may be nil:
// the plan then has no to-dos, or no release milestones.
func NewDefaultService(repo Repository, board Board, todoStore TodoStore, releases Releases) *DefaultService {
	return &DefaultService{repo: repo, board: board, todos: todoStore, releases: releases,
		now: func() time.Time { return time.Now().UTC() }}
}

// cleanDate checks a day: nil for an empty one.
func cleanDate(d string) (*string, error) {
	v := strings.TrimSpace(d)
	if v == "" {
		return nil, nil
	}
	if !datePattern.MatchString(v) {
		return nil, ErrInvalidDate
	}
	if _, err := time.Parse(DateLayout, v); err != nil {
		return nil, ErrInvalidDate
	}
	return &v, nil
}

// CleanCriteria checks a milestone's criteria and returns them normalised.
func CleanCriteria(in []Criterion) ([]Criterion, error) {
	if len(in) > MaxCriteria {
		return nil, ErrTooManyCriteria
	}
	out := make([]Criterion, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		if !ValidMetric(c.Metric) {
			return nil, ErrUnknownMetric
		}
		if c.Target < 1 || c.Target > 100 {
			return nil, ErrInvalidTarget
		}
		if seen[c.Metric] {
			return nil, ErrDuplicateMetric
		}
		seen[c.Metric] = true
		out = append(out, c)
	}
	return out, nil
}

// dayOf is the UTC day of a time, as the plan writes it.
func dayOf(t *time.Time) *string {
	if t == nil {
		return nil
	}
	d := t.UTC().Format(DateLayout)
	return &d
}

// Schedule changes a task's dates, parent and dependencies, then places the
// plan's automatic tasks. The due date is the work item's own, so the board
// shows the same day.
func (s *DefaultService) Schedule(workItemID string, req ScheduleRequest, actor string) error {
	item, err := s.board.Get(workItemID)
	if err != nil || item == nil {
		return ErrTaskNotFound
	}
	if err := s.schedule(workItemID, req, actor); err != nil {
		return err
	}
	return s.autoSchedule(item.ProjectID, actor)
}

// schedule is Schedule without the automatic placing that follows it.
func (s *DefaultService) schedule(workItemID string, req ScheduleRequest, actor string) error {
	item, err := s.board.Get(workItemID)
	if err != nil || item == nil {
		return ErrTaskNotFound
	}
	schedules, err := s.repo.ListSchedules(item.ProjectID)
	if err != nil {
		return err
	}
	current := &TaskSchedule{WorkItemID: item.ID, ProjectID: item.ProjectID}
	parents := map[string]string{}
	for _, sc := range schedules {
		if sc.WorkItemID == item.ID {
			current = sc
		}
		if sc.ParentID != nil {
			parents[sc.WorkItemID] = *sc.ParentID
		}
	}
	onBoard := map[string]bool{}
	if req.ParentID != nil || req.DependsOn != nil || req.Relationships != nil {
		items, err := s.board.ListByProject(item.ProjectID)
		if err != nil {
			return err
		}
		for _, it := range items {
			onBoard[it.ID] = true
		}
	}

	start, due, duration, effort, err := s.placeTask(item, current, schedules, req)
	if err != nil {
		return err
	}
	was := dayOf(item.DueDate)
	dueChanged := (due == nil) != (was == nil) || (due != nil && *due != *was)

	parent := current.ParentID
	if req.ParentID != nil {
		if parent, err = checkParent(item.ID, *req.ParentID, parents, onBoard); err != nil {
			return err
		}
	}

	var relationships []Dependency
	setRelationships := req.DependsOn != nil || req.Relationships != nil
	if setRelationships {
		deps, err := s.repo.ListDependencies(item.ProjectID)
		if err != nil {
			return err
		}
		if relationships, err = checkDependencies(item.ID, wantedRelationships(item.ID, req, deps), deps, onBoard, false); err != nil {
			return err
		}
	}

	milestone, err := s.milestoneKey(item.ProjectID, current.MilestoneKey, req.Milestone)
	if err != nil {
		return err
	}
	next, err := s.taskSettings(item.ProjectID, current, req, start)
	if err != nil {
		return err
	}
	next.StartDate, next.ParentID, next.DurationDays, next.EffortDays, next.MilestoneKey = start, parent, duration, effort, milestone
	if err := s.repo.SaveSchedule(next); err != nil {
		return err
	}
	if setRelationships {
		if err := s.repo.SetDependencies(item.ProjectID, item.ID, relationships); err != nil {
			return err
		}
	}
	if dueChanged {
		var dueAt *time.Time
		if due != nil {
			t, _ := time.Parse(DateLayout, *due)
			dueAt = &t
		}
		_, err := s.board.Update(item.ID, workitems.UpdateWorkItemRequest{
			Title:        item.Title,
			Description:  item.Description,
			AssigneeType: item.AssigneeType,
			AssigneeID:   item.AssigneeID,
			ArtifactIDs:  item.ArtifactIDs,
			DueDate:      dueAt,
		}, actor)
		if err != nil {
			return err
		}
	}
	return nil
}

// milestoneKey is the milestone a schedule request leaves a task on: the one
// it had when the request names none, none when it names the empty key, and
// otherwise the one named, which must be a milestone or a release of the
// task's project.
func (s *DefaultService) milestoneKey(projectID string, current, want *string) (*string, error) {
	if want == nil {
		return current, nil
	}
	key := strings.TrimSpace(*want)
	if key == "" {
		return nil, nil
	}
	if id, ok := strings.CutPrefix(key, releaseKeyPrefix); ok {
		if s.releases == nil {
			return nil, ErrMilestone
		}
		rel, err := s.releases.Get(id)
		if err != nil || rel == nil || rel.ProjectID != projectID {
			return nil, ErrMilestone
		}
		return &key, nil
	}
	m, err := s.repo.GetMilestone(key)
	if err != nil || m == nil || m.ProjectID != projectID || m.ReleaseID != nil {
		return nil, ErrMilestone
	}
	return &key, nil
}

// releaseKeyPrefix begins the plan's key of a release's milestone.
const releaseKeyPrefix = "release:"

// placeTask works out a task's start, due date, duration and effort from a
// request. A task with sub-tasks is a folder for them: its dates, duration
// and effort are theirs, so none can be set on it.
func (s *DefaultService) placeTask(item *workitems.WorkItem, current *TaskSchedule, schedules []*TaskSchedule,
	req ScheduleRequest) (start, due *string, duration *int, effort *float64, err error) {
	hasSubtasks := false
	for _, sc := range schedules {
		hasSubtasks = hasSubtasks || (sc.ParentID != nil && *sc.ParentID == item.ID)
	}
	if hasSubtasks && (req.StartDate != nil || req.DueDate != nil || req.DurationDays != nil || req.EffortDays != nil) {
		return nil, nil, nil, nil, ErrParentDates
	}
	cal, _, err := s.calendar(item.ProjectID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// A task whose duration is elapsed time counts every day.
	if elapsed := current.Elapsed; (req.Elapsed != nil && *req.Elapsed) || (req.Elapsed == nil && elapsed) {
		cal = elapsedCalendar
	}
	if start, due, duration, err = placeDates(cal, current, dayOf(item.DueDate), req); err != nil {
		return nil, nil, nil, nil, err
	}
	effort = current.EffortDays
	if req.EffortDays != nil {
		switch {
		case *req.EffortDays < 0:
			return nil, nil, nil, nil, ErrInvalidEffort
		case *req.EffortDays == 0:
			effort = nil
		default:
			effort = req.EffortDays
		}
	}
	return start, due, duration, effort, nil
}

// spanDays is a task's duration from start to end in the calendar's
// working days, counting both ends.
func spanDays(cal Calendar, start, end *string) *int {
	if start == nil || end == nil {
		return nil
	}
	n := cal.Span(*start, *end)
	return &n
}

// placeDates works out a task's start, due date and duration from what it
// has and what a request sends. The duration is what the task keeps: one
// date sent alone drags the other along, and a duration sent alone moves
// the due date, or the start of a task that has only a due date. Both dates
// sent together decide the duration.
func placeDates(cal Calendar, current *TaskSchedule, was *string, req ScheduleRequest) (start, due *string, duration *int, err error) {
	start, due = current.StartDate, was
	duration = current.DurationDays
	if d := spanDays(cal, start, due); d != nil {
		duration = d
	}
	if req.StartDate != nil {
		if start, err = cleanDate(*req.StartDate); err != nil {
			return nil, nil, nil, err
		}
	}
	if req.DueDate != nil {
		if due, err = cleanDate(*req.DueDate); err != nil {
			return nil, nil, nil, err
		}
	}
	if req.DurationDays != nil {
		switch {
		case *req.DurationDays < 0:
			return nil, nil, nil, ErrInvalidDuration
		case *req.DurationDays == 0:
			duration = nil
		default:
			duration = req.DurationDays
		}
	}
	switch {
	case req.StartDate != nil && req.DueDate != nil:
		// Both ends are given: they decide the length.
	case duration == nil:
	case req.DueDate != nil && due != nil:
		v := cal.Start(*due, *duration)
		start = &v
	case start != nil && (req.StartDate != nil || req.DurationDays != nil):
		v := cal.End(*start, *duration)
		due = &v
	case due != nil && req.DurationDays != nil:
		v := cal.Start(*due, *duration)
		start = &v
	}
	if start != nil && due != nil {
		if *start > *due {
			return nil, nil, nil, ErrDateOrder
		}
		duration = spanDays(cal, start, due)
	}
	return start, due, duration, nil
}

// checkParent checks the task id is to become a sub-task of: on the
// project's board, not id itself and not one of id's own sub-tasks. An empty
// one clears the parent.
func checkParent(id, want string, parents map[string]string, onBoard map[string]bool) (*string, error) {
	p := strings.TrimSpace(want)
	switch {
	case p == "":
		return nil, nil
	case p == id:
		return nil, ErrParentSelf
	case !onBoard[p]:
		return nil, ErrOtherProject
	}
	seen := map[string]bool{}
	for up := p; up != "" && !seen[up]; up = parents[up] {
		if up == id {
			return nil, ErrParentCycle
		}
		seen[up] = true
	}
	return &p, nil
}

// TaskTodos lists the to-dos on a task, in the project list's order: open
// ones first.
func (s *DefaultService) TaskTodos(workItemID string) ([]*todos.Todo, error) {
	item, err := s.board.Get(workItemID)
	if err != nil || item == nil {
		return nil, ErrTaskNotFound
	}
	out := []*todos.Todo{}
	if s.todos == nil {
		return out, nil
	}
	on, err := s.repo.ListTodoTasks(item.ProjectID)
	if err != nil {
		return nil, err
	}
	all, err := s.todos.ListByProject(item.ProjectID)
	if err != nil {
		return nil, err
	}
	for _, t := range all {
		if t != nil && on[t.ID] == item.ID {
			out = append(out, t)
		}
	}
	return out, nil
}

// AddTodo raises a to-do in the task's project and attaches it to the task.
func (s *DefaultService) AddTodo(workItemID, orgID, text string, due *time.Time, createdBy *string) (*todos.Todo, error) {
	item, err := s.board.Get(workItemID)
	if err != nil || item == nil {
		return nil, ErrTaskNotFound
	}
	if s.todos == nil {
		return nil, errors.New("to-dos are not available")
	}
	t, err := s.todos.Create(todos.CreateRequest{OrgID: orgID, ProjectID: &item.ProjectID, Text: text, DueDate: due}, createdBy)
	if err != nil {
		return nil, err
	}
	if err := s.repo.AttachTodo(item.ProjectID, t.ID, item.ID); err != nil {
		return nil, err
	}
	return t, nil
}
