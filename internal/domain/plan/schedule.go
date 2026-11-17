// Scheduling a task: the change a ScheduleRequest asks for, checked and
// stored, and the dates the plan then gives the task.

package plan

import (
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

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
	// A task given a new parent goes last among its new siblings unless
	// the request says where.
	if req.AfterID != nil || req.ParentID != nil {
		if err := s.reorder(item.ProjectID, workItemID, req.AfterID); err != nil {
			return err
		}
	}
	if req.Order != nil && req.AfterID == nil {
		if err := s.placeAt(item.ProjectID, workItemID, *req.Order); err != nil {
			return err
		}
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
	current := s.unscheduled(item.ID, item.ProjectID)
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
		if err = noNewParentLinks(relationships, deps, parents); err != nil {
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
	if due, duration, err = s.assign(item, next, req, start, due, duration, effort); err != nil {
		return err
	}
	dueChanged = (due == nil) != (was == nil) || (due != nil && *due != *was)
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
	// A task counts the days it is worked on: every day for elapsed time,
	// otherwise the plan's days less those its people are all away.
	was, now, err := s.taskCalendars(item, cal, current.Elapsed, req)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	// New people and no new dates: the task keeps its days of work, and
	// its end moves to fit them.
	if req.Assignments != nil && req.Elapsed == nil && req.StartDate == nil && req.DueDate == nil && req.DurationDays == nil {
		if d := spanDays(was, current.StartDate, dayOf(item.DueDate)); d != nil && *d > 0 {
			req.DurationDays = d
		}
	}
	if start, due, duration, err = placeDates(now, current, dayOf(item.DueDate), req); err != nil {
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
