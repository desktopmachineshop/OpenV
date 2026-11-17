// The calendar a task counts its duration in, for the service's writes.

package plan

import "github.com/openv/requirements-platform/internal/domain/workitems"

// taskCalendars answers the calendar a task counted its duration in before
// a request, and the one it counts in after it: who is on the task, and
// whether its duration is elapsed time, can both change.
func (s *DefaultService) taskCalendars(item *workitems.WorkItem, cal Calendar, elapsed bool, req ScheduleRequest) (Calendar, Calendar, error) {
	off, err := s.repo.ListTimeOff(item.ProjectID)
	if err != nil {
		return cal, cal, err
	}
	all, err := s.repo.ListAssignments(item.ProjectID)
	if err != nil {
		return cal, cal, err
	}
	var who []Assignment
	for _, a := range all {
		if a.WorkItemID == item.ID {
			who = append(who, a)
		}
	}
	board := func(list []Assignment) []Assignment {
		if len(list) == 0 && item.AssigneeID != nil && *item.AssigneeID != "" {
			return []Assignment{{Type: item.AssigneeType, ID: *item.AssigneeID, Units: 100}}
		}
		return list
	}
	next, after := who, elapsed
	if req.Assignments != nil {
		next = *req.Assignments
	}
	if req.Elapsed != nil {
		after = *req.Elapsed
	}
	away := away(off)
	return taskCalendar(cal, elapsed, board(who), away), taskCalendar(cal, after, board(next), away), nil
}
