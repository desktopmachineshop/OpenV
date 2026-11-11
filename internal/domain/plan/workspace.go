// The workspace's plan calendar: the days of the week every plan of the
// workspace works, and the time off of its people and agents, which is the
// same on every project.

package plan

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxTimeOff bounds a workspace's time-off entries, and MaxTimeOffDays one
// entry's length.
const (
	MaxTimeOff     = 2000
	MaxTimeOffDays = 366
)

// TimeOff is a run of days a person or an agent is away: they have no
// capacity on them, whatever the plan's calendar says.
type TimeOff struct {
	ID string `json:"id"`
	// Type is "user" or "agent"; AssigneeID their id.
	Type       string `json:"type"`
	AssigneeID string `json:"assignee_id"`
	// Start and End are YYYY-MM-DD, both counted.
	Start string `json:"start"`
	End   string `json:"end"`
	Name  string `json:"name"`
}

// WorkspaceCalendar is a workspace's plan calendar.
type WorkspaceCalendar struct {
	WorkingDays []int     `json:"working_days"`
	TimeOff     []TimeOff `json:"time_off"`
}

// WorkspaceCalendar reads the workspace's working days (Monday to Friday
// until it sets them) and its time off, in date order.
func (s *DefaultService) WorkspaceCalendar(orgID string) (WorkspaceCalendar, error) {
	days, off, err := s.repo.WorkspaceCalendar(orgID)
	if err != nil {
		return WorkspaceCalendar{}, err
	}
	if days == nil {
		days = DefaultWorkingDays
	}
	if off == nil {
		off = []TimeOff{}
	}
	return WorkspaceCalendar{WorkingDays: days, TimeOff: off}, nil
}

// SetWorkspaceWorkingDays sets the days of the week every plan of the
// workspace works.
func (s *DefaultService) SetWorkspaceWorkingDays(orgID string, days []int) ([]int, error) {
	clean, err := CleanWorkingDays(days)
	if err != nil {
		return nil, err
	}
	return clean, s.repo.SaveWorkspaceWorkingDays(orgID, clean)
}

// AddTimeOff records days a person or an agent is away. The tasks they
// are on stretch over those days.
func (s *DefaultService) AddTimeOff(orgID string, t TimeOff) (*TimeOff, error) {
	if t.Type == "" {
		t.Type = "user"
	}
	start, err1 := time.Parse(DateLayout, t.Start)
	end, err2 := time.Parse(DateLayout, t.End)
	switch {
	case t.Type != "user" && t.Type != "agent", strings.TrimSpace(t.AssigneeID) == "":
		return nil, ErrResource
	case err1 != nil || err2 != nil:
		return nil, ErrInvalidDate
	case end.Before(start):
		return nil, ErrDateOrder
	case end.Sub(start) > MaxTimeOffDays*24*time.Hour, len([]rune(t.Name)) > 80:
		return nil, ErrTimeOff
	}
	_, existing, err := s.repo.WorkspaceCalendar(orgID)
	if err != nil {
		return nil, err
	}
	if len(existing) >= MaxTimeOff {
		return nil, ErrTimeOff
	}
	t.ID, t.Name = uuid.NewString(), strings.TrimSpace(t.Name)
	if err := s.restretch(orgID, func() error { return s.repo.AddTimeOff(orgID, &t) }); err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteTimeOff removes a time-off entry of the workspace.
func (s *DefaultService) DeleteTimeOff(orgID, id string) error {
	return s.restretch(orgID, func() error { return s.repo.DeleteTimeOff(orgID, id) })
}

// away is the set of days each resource ("type:id") is off.
func away(off []TimeOff) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, t := range off {
		start, err1 := time.Parse(DateLayout, t.Start)
		end, err2 := time.Parse(DateLayout, t.End)
		if err1 != nil || err2 != nil || end.Sub(start) > MaxTimeOffDays*24*time.Hour {
			continue
		}
		key := t.Type + ":" + t.AssigneeID
		if out[key] == nil {
			out[key] = map[string]bool{}
		}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			out[key][d.Format(DateLayout)] = true
		}
	}
	return out
}

// taskCalendar is the calendar a task's duration counts: every day for
// elapsed time; otherwise the plan's, less the days on which everyone
// assigned to the task is away, since nobody can work on it then. A task
// stretches over those days: its duration is the days it is worked on.
func taskCalendar(base Calendar, elapsed bool, who []Assignment, off map[string]map[string]bool) Calendar {
	if elapsed {
		return elapsedCalendar
	}
	if len(who) == 0 || len(off) == 0 {
		return base
	}
	var all map[string]bool
	for i, a := range who {
		days := off[a.Type+":"+a.ID]
		if len(days) == 0 {
			return base
		}
		if i == 0 {
			all = make(map[string]bool, len(days))
			for d := range days {
				all[d] = true
			}
			continue
		}
		for d := range all {
			if !days[d] {
				delete(all, d)
			}
		}
	}
	return base.Without(all)
}

// stretched is a task as it sat before a change to who is away.
type stretched struct {
	id, start, end string
	days           int
}

// restretch makes a change to the workspace's time off, then moves the end
// of every task of its projects that is not done so that the task keeps
// the days of work it had: a task whose people are away for a week ends a
// week later. The tasks those drive follow on automatic plans.
func (s *DefaultService) restretch(orgID string, change func() error) error {
	projects, err := s.repo.OrgProjects(orgID)
	if err != nil {
		return err
	}
	kept := map[string][]stretched{}
	for _, pid := range projects {
		in, cal, _, err := s.inputs(pid)
		if err != nil {
			return err
		}
		p := Build(in, time.Now())
		for _, t := range p.Tasks {
			if t.Done || t.SubtasksTotal > 0 || t.Elapsed || t.SpanStart == nil || t.SpanEnd == nil || len(t.Assignments) == 0 {
				continue
			}
			was := taskCalendar(cal, false, t.Assignments, p.off)
			kept[pid] = append(kept[pid], stretched{t.ID, *t.SpanStart, *t.SpanEnd, was.Span(*t.SpanStart, *t.SpanEnd)})
		}
	}
	if err := change(); err != nil {
		return err
	}
	for _, pid := range projects {
		in, cal, _, err := s.inputs(pid)
		if err != nil {
			return err
		}
		p := Build(in, time.Now())
		moved := false
		for _, k := range kept[pid] {
			t := p.task(k.id)
			if t == nil || k.days < 1 {
				continue
			}
			end := taskCalendar(cal, false, t.Assignments, p.off).End(k.start, k.days)
			if end == k.end {
				continue
			}
			if err := s.schedule(k.id, ScheduleRequest{StartDate: &k.start, DueDate: &end}, ""); err != nil {
				return err
			}
			moved = true
		}
		if moved {
			if err := s.autoSchedule(pid, ""); err != nil {
				return err
			}
		}
	}
	return nil
}
