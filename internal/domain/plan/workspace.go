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

// AddTimeOff records days a person or an agent is away.
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
	if err := s.repo.AddTimeOff(orgID, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteTimeOff removes a time-off entry of the workspace.
func (s *DefaultService) DeleteTimeOff(orgID, id string) error {
	return s.repo.DeleteTimeOff(orgID, id)
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
