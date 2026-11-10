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

	"github.com/google/uuid"

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
}

// Dependency says WorkItemID cannot start until DependsOnID has finished.
type Dependency struct {
	WorkItemID  string `json:"work_item_id"`
	DependsOnID string `json:"depends_on_id"`
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
type ScheduleRequest struct {
	StartDate *string   `json:"start_date"`
	DueDate   *string   `json:"due_date"`
	ParentID  *string   `json:"parent_id"`
	DependsOn *[]string `json:"depends_on"`
}

// Repository stores what the plan adds to the board.
type Repository interface {
	ListSchedules(projectID string) ([]*TaskSchedule, error)
	// SaveSchedule inserts or replaces a task's schedule.
	SaveSchedule(s *TaskSchedule) error
	ListDependencies(projectID string) ([]Dependency, error)
	// SetDependencies replaces what a task depends on.
	SetDependencies(projectID, workItemID string, dependsOn []string) error
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
	// AddTodo raises a to-do on a task.
	AddTodo(workItemID, orgID, text string, createdBy *string) (*todos.Todo, error)
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

// Schedule changes a task's dates, parent and dependencies. The due date is
// the work item's own, so the board shows the same day.
func (s *DefaultService) Schedule(workItemID string, req ScheduleRequest, actor string) error {
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
	if req.ParentID != nil || req.DependsOn != nil {
		items, err := s.board.ListByProject(item.ProjectID)
		if err != nil {
			return err
		}
		for _, it := range items {
			onBoard[it.ID] = true
		}
	}

	start, due := current.StartDate, dayOf(item.DueDate)
	if req.StartDate != nil {
		if start, err = cleanDate(*req.StartDate); err != nil {
			return err
		}
	}
	dueChanged := false
	if req.DueDate != nil {
		next, err := cleanDate(*req.DueDate)
		if err != nil {
			return err
		}
		dueChanged = (next == nil) != (due == nil) || (next != nil && *next != *due)
		due = next
	}
	if start != nil && due != nil && *start > *due {
		return ErrDateOrder
	}

	parent := current.ParentID
	if req.ParentID != nil {
		if parent, err = checkParent(item.ID, *req.ParentID, parents, onBoard); err != nil {
			return err
		}
	}

	var dependsOn []string
	if req.DependsOn != nil {
		deps, err := s.repo.ListDependencies(item.ProjectID)
		if err != nil {
			return err
		}
		if dependsOn, err = checkDependencies(item.ID, *req.DependsOn, deps, onBoard); err != nil {
			return err
		}
	}

	if err := s.repo.SaveSchedule(&TaskSchedule{WorkItemID: item.ID, ProjectID: item.ProjectID, StartDate: start, ParentID: parent}); err != nil {
		return err
	}
	if req.DependsOn != nil {
		if err := s.repo.SetDependencies(item.ProjectID, item.ID, dependsOn); err != nil {
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

// checkDependencies checks the tasks id is to depend on: each on the
// project's board, none id itself, and none that already waits on id.
func checkDependencies(id string, want []string, existing []Dependency, onBoard map[string]bool) ([]string, error) {
	waitsOn := map[string][]string{}
	for _, d := range existing {
		if d.WorkItemID != id {
			waitsOn[d.WorkItemID] = append(waitsOn[d.WorkItemID], d.DependsOnID)
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, dep := range want {
		dep = strings.TrimSpace(dep)
		if dep == "" || seen[dep] {
			continue
		}
		if dep == id {
			return nil, ErrDependsOnSelf
		}
		if !onBoard[dep] {
			return nil, ErrOtherProject
		}
		seen[dep] = true
		out = append(out, dep)
	}
	visited := map[string]bool{}
	stack := append([]string(nil), out...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == id {
			return nil, ErrDependencyCycle
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		stack = append(stack, waitsOn[cur]...)
	}
	return out, nil
}

// AddTodo raises a to-do in the task's project and attaches it to the task.
func (s *DefaultService) AddTodo(workItemID, orgID, text string, createdBy *string) (*todos.Todo, error) {
	item, err := s.board.Get(workItemID)
	if err != nil || item == nil {
		return nil, ErrTaskNotFound
	}
	if s.todos == nil {
		return nil, errors.New("to-dos are not available")
	}
	t, err := s.todos.Create(todos.CreateRequest{OrgID: orgID, ProjectID: &item.ProjectID, Text: text}, createdBy)
	if err != nil {
		return nil, err
	}
	if err := s.repo.AttachTodo(item.ProjectID, t.ID, item.ID); err != nil {
		return nil, err
	}
	return t, nil
}

// GetMilestone returns one stored milestone.
func (s *DefaultService) GetMilestone(id string) (*Milestone, error) { return s.repo.GetMilestone(id) }

// CreateMilestone adds a custom milestone.
func (s *DefaultService) CreateMilestone(projectID string, req MilestoneRequest) (*Milestone, error) {
	now := s.now()
	m := &Milestone{ID: uuid.New().String(), ProjectID: projectID, Criteria: []Criterion{}, CreatedAt: now, UpdatedAt: now}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return nil, ErrNameRequired
	}
	if err := applyMilestone(m, req); err != nil {
		return nil, err
	}
	if err := s.repo.CreateMilestone(m); err != nil {
		return nil, err
	}
	return m, nil
}

func applyMilestone(m *Milestone, req MilestoneRequest) error {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return ErrNameRequired
		}
		m.Name = name
	}
	if req.Description != nil {
		m.Description = strings.TrimSpace(*req.Description)
	}
	if req.TargetDate != nil {
		d, err := cleanDate(*req.TargetDate)
		if err != nil {
			return err
		}
		m.TargetDate = d
	}
	if req.Criteria != nil {
		c, err := CleanCriteria(*req.Criteria)
		if err != nil {
			return err
		}
		m.Criteria = c
	}
	return nil
}

// UpdateMilestone changes a milestone. A release's milestone takes only
// criteria: its name and date are the release's.
func (s *DefaultService) UpdateMilestone(id string, req MilestoneRequest) (*Milestone, error) {
	m, err := s.repo.GetMilestone(id)
	if err != nil {
		return nil, err
	}
	if m.ReleaseID != nil && (req.Name != nil || req.TargetDate != nil || req.Description != nil) {
		return nil, ErrReleaseMilestone
	}
	if err := applyMilestone(m, req); err != nil {
		return nil, err
	}
	m.UpdatedAt = s.now()
	if err := s.repo.UpdateMilestone(m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteMilestone removes a stored milestone. Removing a release's resets
// its criteria to the defaults; the release itself stays.
func (s *DefaultService) DeleteMilestone(id string) error { return s.repo.DeleteMilestone(id) }

// SetReleaseCriteria sets the criteria of a release's milestone, storing
// the milestone the first time.
func (s *DefaultService) SetReleaseCriteria(projectID, releaseID string, criteria []Criterion) (*Milestone, error) {
	clean, err := CleanCriteria(criteria)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.ListMilestones(projectID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, m := range stored {
		if m.ReleaseID != nil && *m.ReleaseID == releaseID {
			m.Criteria = clean
			m.UpdatedAt = now
			return m, s.repo.UpdateMilestone(m)
		}
	}
	m := &Milestone{ID: uuid.New().String(), ProjectID: projectID, ReleaseID: &releaseID, Criteria: clean, CreatedAt: now, UpdatedAt: now}
	return m, s.repo.CreateMilestone(m)
}
