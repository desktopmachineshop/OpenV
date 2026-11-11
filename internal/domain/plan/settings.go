// A plan's settings: its calendar, its critical threshold and how it is
// scheduled; and a task's own scheduling mode and date constraint.

package plan

import (
	"strings"
)

// The scheduling modes: a manual task sits where it was put, an automatic
// one is placed by the platform from its relationships and constraints.
const (
	ModeManual = "manual"
	ModeAuto   = "auto"
)

// What a plan is scheduled from: its start, tasks as soon as possible, or
// its finish, tasks as late as possible.
const (
	FromStart  = "start"
	FromFinish = "finish"
)

// The date constraints a task can carry.
const (
	ConstraintASAP = "asap" // as soon as possible: none
	ConstraintALAP = "alap" // as late as possible
	ConstraintSNET = "snet" // start no earlier than
	ConstraintSNLT = "snlt" // start no later than
	ConstraintFNET = "fnet" // finish no earlier than
	ConstraintFNLT = "fnlt" // finish no later than
	ConstraintMSO  = "mso"  // must start on
	ConstraintMFO  = "mfo"  // must finish on
)

// Constraints lists the constraint types in the order a chooser shows them.
var Constraints = []string{ConstraintASAP, ConstraintALAP, ConstraintSNET, ConstraintSNLT, ConstraintFNET, ConstraintFNLT, ConstraintMSO, ConstraintMFO}

// Scheduling is how a plan is scheduled.
type Scheduling struct {
	// Mode is the mode of every task that has none of its own.
	Mode string `json:"scheduling_mode"`
	// ProjectStart is the day the plan starts: an automatic task starts no
	// earlier. ProjectFinish is the day it must finish by, which bounds
	// float, and from which a plan scheduled from its finish works back.
	ProjectStart  *string `json:"project_start"`
	ProjectFinish *string `json:"project_finish"`
	ScheduleFrom  string  `json:"schedule_from"`
	// MultipleCriticalPaths: each chain of tasks has a critical path of its
	// own, to its own last task, rather than one path to the plan's end.
	MultipleCriticalPaths bool `json:"multiple_critical_paths"`
}

// Settings are a plan's calendar, its critical threshold and its
// scheduling.
type Settings struct {
	WorkingDays []int     `json:"working_days"`
	Holidays    []Holiday `json:"holidays"`
	// CriticalSlackDays: a task with that much float or less is critical.
	CriticalSlackDays int `json:"critical_slack_days"`
	Scheduling
	// Currency names the money costs are in, as up to three letters; Budget
	// is what the plan may cost, nil when it has none.
	Currency string   `json:"currency"`
	Budget   *float64 `json:"budget"`
}

// SettingsRequest changes the settings it names. An empty ProjectStart or
// ProjectFinish clears it.
type SettingsRequest struct {
	WorkingDays           *[]int     `json:"working_days"`
	Holidays              *[]Holiday `json:"holidays"`
	CriticalSlackDays     *int       `json:"critical_slack_days"`
	SchedulingMode        *string    `json:"scheduling_mode"`
	ProjectStart          *string    `json:"project_start"`
	ProjectFinish         *string    `json:"project_finish"`
	ScheduleFrom          *string    `json:"schedule_from"`
	MultipleCriticalPaths *bool      `json:"multiple_critical_paths"`
	Currency              *string    `json:"currency"`
	// Budget of 0 clears it.
	Budget *float64 `json:"budget"`
}

// MaxCriticalSlackDays bounds the critical threshold.
const MaxCriticalSlackDays = 365

// settings loads the project's settings with the defaults filled in.
func (s *DefaultService) settings(projectID string) (Settings, error) {
	set, err := s.repo.Settings(projectID)
	if err != nil {
		return set, err
	}
	if set.WorkingDays == nil {
		set.WorkingDays = DefaultWorkingDays
	}
	if set.Holidays == nil {
		set.Holidays = []Holiday{}
	}
	if set.Mode == "" {
		set.Mode = ModeManual
	}
	if set.ScheduleFrom == "" {
		set.ScheduleFrom = FromStart
	}
	return set, nil
}

// calendar is the project's calendar, its working days less its holidays,
// with the settings it came from.
func (s *DefaultService) calendar(projectID string) (Calendar, Settings, error) {
	set, err := s.settings(projectID)
	if err != nil {
		return Calendar{}, set, err
	}
	return NewCalendar(set.WorkingDays).WithHolidays(set.Holidays), set, nil
}

// SetWorkingDays sets which days of the week the plan works.
func (s *DefaultService) SetWorkingDays(projectID string, days []int) ([]int, error) {
	set, err := s.SetSettings(projectID, SettingsRequest{WorkingDays: &days})
	return set.WorkingDays, err
}

// SetSettings changes the settings the request names and answers them all.
// A change can move the plan's automatic tasks.
func (s *DefaultService) SetSettings(projectID string, req SettingsRequest) (Settings, error) {
	set, err := s.settings(projectID)
	if err != nil {
		return set, err
	}
	if err := applySettings(&set, req); err != nil {
		return set, err
	}
	if err := s.repo.SaveSettings(projectID, set); err != nil {
		return set, err
	}
	return set, s.autoSchedule(projectID, "")
}

func applySettings(set *Settings, req SettingsRequest) error {
	var err error
	if req.WorkingDays != nil {
		if set.WorkingDays, err = CleanWorkingDays(*req.WorkingDays); err != nil {
			return err
		}
	}
	if req.Holidays != nil {
		if set.Holidays, err = CleanHolidays(*req.Holidays); err != nil {
			return err
		}
	}
	if req.CriticalSlackDays != nil {
		if *req.CriticalSlackDays < 0 || *req.CriticalSlackDays > MaxCriticalSlackDays {
			return ErrCriticalSlack
		}
		set.CriticalSlackDays = *req.CriticalSlackDays
	}
	if req.SchedulingMode != nil {
		if *req.SchedulingMode != ModeManual && *req.SchedulingMode != ModeAuto {
			return ErrSchedulingMode
		}
		set.Mode = *req.SchedulingMode
	}
	if req.ScheduleFrom != nil {
		if *req.ScheduleFrom != FromStart && *req.ScheduleFrom != FromFinish {
			return ErrSchedulingMode
		}
		set.ScheduleFrom = *req.ScheduleFrom
	}
	if req.ProjectStart != nil {
		if set.ProjectStart, err = cleanDate(*req.ProjectStart); err != nil {
			return err
		}
	}
	if req.ProjectFinish != nil {
		if set.ProjectFinish, err = cleanDate(*req.ProjectFinish); err != nil {
			return err
		}
	}
	if req.MultipleCriticalPaths != nil {
		set.MultipleCriticalPaths = *req.MultipleCriticalPaths
	}
	if req.Currency != nil {
		c := strings.ToUpper(strings.TrimSpace(*req.Currency))
		if len(c) > 3 {
			return ErrInvalidCost
		}
		set.Currency = c
	}
	if req.Budget != nil {
		switch {
		case *req.Budget < 0 || *req.Budget > 1e12:
			return ErrInvalidCost
		case *req.Budget == 0:
			set.Budget = nil
		default:
			set.Budget = req.Budget
		}
	}
	return nil
}

// constraintOf is the scheduling mode and the constraint a schedule request
// leaves a task with.
func constraintOf(current *TaskSchedule, req ScheduleRequest) (mode, kind string, date *string, err error) {
	mode, kind, date = current.Mode, current.ConstraintType, current.ConstraintDate
	if req.Mode != nil {
		mode = strings.ToLower(strings.TrimSpace(*req.Mode))
		if mode != "" && mode != ModeManual && mode != ModeAuto {
			return "", "", nil, ErrSchedulingMode
		}
	}
	if req.ConstraintType != nil {
		kind = strings.ToLower(strings.TrimSpace(*req.ConstraintType))
	}
	if req.ConstraintDate != nil {
		if date, err = cleanDate(*req.ConstraintDate); err != nil {
			return "", "", nil, err
		}
	}
	if kind == "" {
		kind = ConstraintASAP
	}
	known := false
	for _, c := range Constraints {
		known = known || c == kind
	}
	dateless := kind == ConstraintASAP || kind == ConstraintALAP
	if !known || (!dateless && date == nil) {
		return "", "", nil, ErrConstraint
	}
	if dateless {
		date = nil
	}
	return mode, kind, date, nil
}

// taskSettings is a task's schedule row with what a request leaves of its
// scheduling mode, constraint, elapsed duration and deadline; the caller
// fills in its dates. A start typed on an automatic task with no constraint
// holds it there: it becomes "start no earlier than" that day, or the
// placing that follows would put it straight back.
func (s *DefaultService) taskSettings(projectID string, current *TaskSchedule, req ScheduleRequest, start *string) (*TaskSchedule, error) {
	mode, constraint, constraintDate, err := constraintOf(current, req)
	if err != nil {
		return nil, err
	}
	if req.StartDate != nil && start != nil && req.ConstraintType == nil && constraint == ConstraintASAP {
		if set, err := s.settings(projectID); err == nil && (mode == ModeAuto || (mode == "" && set.Mode == ModeAuto)) {
			constraint, constraintDate = ConstraintSNET, start
		}
	}
	out := &TaskSchedule{WorkItemID: current.WorkItemID, ProjectID: projectID, Elapsed: current.Elapsed, Deadline: current.Deadline,
		Mode: mode, ConstraintType: constraint, ConstraintDate: constraintDate, FixedCost: current.FixedCost, EffortDriven: current.EffortDriven}
	if req.FixedCost != nil {
		if *req.FixedCost < 0 || *req.FixedCost > 1e12 {
			return nil, ErrInvalidCost
		}
		out.FixedCost = *req.FixedCost
	}
	if req.EffortDriven != nil {
		out.EffortDriven = *req.EffortDriven
	}
	if req.Elapsed != nil {
		out.Elapsed = *req.Elapsed
	}
	if req.Deadline != nil {
		if out.Deadline, err = cleanDate(*req.Deadline); err != nil {
			return nil, err
		}
	}
	return out, nil
}
