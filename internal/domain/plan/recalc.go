// Placing a plan's tasks: Recalculate, which moves the tasks that others
// drive when an editor asks, and automatic scheduling, which keeps a plan's
// automatic tasks where their relationships and constraints put them after
// every change.

package plan

import (
	"time"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// placing says which tasks a pass may move.
type placing struct {
	// auto: only the tasks whose scheduling mode is automatic. Otherwise
	// every task that a relationship or a constraint places.
	auto bool
	// onlyLater: a task is never moved earlier than it sits.
	onlyLater bool
}

// Recalculate moves each task that another task drives, or that a
// constraint holds to a date, to the dates those ask for, and answers how
// many tasks it moved. A task keeps its duration; a task nothing places
// stays where it is, and so does one that is done. A driven task with
// sub-tasks moves them by as many working days as it has to move. With
// onlyLater, relationships are minimums: a task that already starts on or
// after the day they ask stays put.
func (s *DefaultService) Recalculate(projectID, actor string, onlyLater bool, now time.Time) (int, error) {
	return s.place(projectID, actor, placing{onlyLater: onlyLater}, now)
}

// autoSchedule places the plan's automatic tasks. It does nothing for a
// plan that has none.
func (s *DefaultService) autoSchedule(projectID, actor string) error {
	_, err := s.place(projectID, actor, placing{auto: true}, time.Now())
	return err
}

func (s *DefaultService) place(projectID, actor string, how placing, now time.Time) (int, error) {
	if how.auto {
		schedules, err := s.repo.ListSchedules(projectID)
		if err != nil {
			return 0, err
		}
		set, err := s.settings(projectID)
		if err != nil {
			return 0, err
		}
		any := set.Mode == ModeAuto
		for _, sc := range schedules {
			any = any || sc.Mode == ModeAuto
		}
		if !any {
			return 0, nil
		}
	}
	in, cal, set, err := s.inputs(projectID)
	if err != nil {
		return 0, err
	}
	p := Build(in, now)
	r := newRecalc(p, cal, set.Scheduling, how)
	// A move can push what the moved task drives, so go round until a pass
	// moves nothing: at most once per task along the longest chain.
	for pass := 0; pass <= len(p.Tasks) && r.pass(); pass++ {
	}
	r.late(in, now)
	return s.write(r, actor)
}

// inputs loads what placing a plan's tasks reads: the board, the schedule,
// the relationships, the assignments and the settings, with the calendar
// those give.
func (s *DefaultService) inputs(projectID string) (Inputs, Calendar, Settings, error) {
	in := Inputs{ProjectID: projectID}
	cal, set, err := s.calendar(projectID)
	if err != nil {
		return in, cal, set, err
	}
	if in.Items, err = s.board.ListByProject(projectID); err != nil {
		return in, cal, set, err
	}
	if in.Schedules, err = s.repo.ListSchedules(projectID); err != nil {
		return in, cal, set, err
	}
	if in.Dependencies, err = s.repo.ListDependencies(projectID); err != nil {
		return in, cal, set, err
	}
	if in.Assignments, err = s.repo.ListAssignments(projectID); err != nil {
		return in, cal, set, err
	}
	if in.Resources, err = s.repo.ListResources(projectID); err != nil {
		return in, cal, set, err
	}
	in.WorkingDays, in.Holidays, in.CriticalSlackDays, in.Scheduling = set.WorkingDays, set.Holidays, set.CriticalSlackDays, set.Scheduling
	return in, cal, set, nil
}

// write stores the dates of every task a placing moved, and answers how
// many that was.
func (s *DefaultService) write(r *recalc, actor string) (int, error) {
	moved := 0
	for _, t := range r.plan.Tasks {
		was, is := r.was[t.ID], r.at[t.ID]
		if is == nil || (was != nil && *was == *is) {
			continue
		}
		if err := s.schedule(t.ID, ScheduleRequest{StartDate: &is.start, DueDate: &is.end}, actor); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// placed is where a task with no sub-tasks sits.
type placed struct{ start, end string }

// recalc holds a plan's tasks while they are moved.
type recalc struct {
	plan     *Plan
	cal      Calendar
	set      Scheduling
	how      placing
	byID     map[string]*Task
	children map[string][]*Task
	drivers  map[string][]Dependency
	// at is where each task with no sub-tasks sits now; was, where it sat.
	at, was map[string]*placed
}

func newRecalc(p *Plan, cal Calendar, set Scheduling, how placing) *recalc {
	r := &recalc{plan: p, cal: cal, set: set, how: how, byID: map[string]*Task{}, children: map[string][]*Task{}, drivers: map[string][]Dependency{},
		at: map[string]*placed{}, was: map[string]*placed{}}
	for _, t := range p.Tasks {
		r.byID[t.ID] = t
		if t.ParentID != nil {
			r.children[*t.ParentID] = append(r.children[*t.ParentID], t)
		}
	}
	for _, t := range p.Tasks {
		if len(r.children[t.ID]) == 0 && t.SpanStart != nil && t.SpanEnd != nil {
			r.was[t.ID] = &placed{*t.SpanStart, *t.SpanEnd}
			r.at[t.ID] = &placed{*t.SpanStart, *t.SpanEnd}
		}
	}
	for _, d := range p.Dependencies {
		r.drivers[d.WorkItemID] = append(r.drivers[d.WorkItemID], d)
	}
	return r
}

// span is where a task sits: its own place, or from the first start to the
// last end of the tasks beneath it.
func (r *recalc) span(id string) *placed {
	kids := r.children[id]
	if len(kids) == 0 {
		return r.at[id]
	}
	var out *placed
	for _, k := range kids {
		s := r.span(k.ID)
		switch {
		case s == nil:
		case out == nil:
			out = &placed{s.start, s.end}
		default:
			if s.start < out.start {
				out.start = s.start
			}
			if s.end > out.end {
				out.end = s.end
			}
		}
	}
	return out
}

// own is the calendar a task's duration counts: every day for a task whose
// duration is elapsed time, the plan's otherwise.
func (r *recalc) own(t *Task) Calendar {
	if t.Elapsed && len(r.children[t.ID]) == 0 {
		return elapsedCalendar
	}
	return r.cal
}

// duration is how many working days a task runs: what its dates span, what
// it keeps, or a day.
func (r *recalc) duration(t *Task) int {
	if at := r.at[t.ID]; at != nil {
		return r.own(t).Span(at.start, at.end)
	}
	if t.DurationDays != nil && *t.DurationDays > 0 {
		return *t.DurationDays
	}
	return 1
}

// moves reports whether the pass may move a task.
func (r *recalc) moves(t *Task) bool {
	if t.Done {
		return false
	}
	if r.how.auto {
		return t.Mode == ModeAuto
	}
	if len(r.drivers[t.ID]) > 0 {
		return true
	}
	switch t.ConstraintType {
	case ConstraintSNET, ConstraintFNET, ConstraintMSO, ConstraintMFO:
		return t.ConstraintDate != nil
	}
	return false
}

// wanted is the start a task is asked to have, or "" when nothing asks. A
// "must start on" or "must finish on" constraint decides it alone.
// Otherwise it is the latest of what the tasks that drive it ask, its "no
// earlier than" constraint and, for an automatic task, the plan's start. A
// task that only such bounds place, with no task driving it, is never
// pulled earlier than it sits, and neither is any task when only later
// moves are asked for.
func (r *recalc) wanted(t *Task, duration int, sits *string) string {
	own := r.own(t)
	if t.ConstraintDate != nil {
		switch t.ConstraintType {
		case ConstraintMSO:
			return r.cal.OnOrAfter(*t.ConstraintDate)
		case ConstraintMFO:
			return own.Start(*t.ConstraintDate, duration)
		}
	}
	want, driven := "", false
	bound := func(day string) {
		if day > want {
			want = day
		}
	}
	for _, d := range r.drivers[t.ID] {
		on := r.span(d.DependsOnID)
		if on == nil {
			continue
		}
		asked := askedDate(r.cal, d.Type, d.LagDays, d.LagUnit, on.start, on.end, r.cal.Span(on.start, on.end))
		if endDriven(d.Type) {
			asked = own.Start(asked, duration)
		}
		driven = true
		bound(asked)
	}
	if t.ConstraintDate != nil {
		switch t.ConstraintType {
		case ConstraintSNET:
			bound(*t.ConstraintDate)
		case ConstraintFNET:
			bound(own.Start(*t.ConstraintDate, duration))
		}
	}
	if r.how.auto && r.set.ProjectStart != nil {
		bound(*r.set.ProjectStart)
	}
	if want == "" {
		return ""
	}
	want = r.cal.OnOrAfter(want)
	if sits != nil && *sits > want && (!driven || r.how.onlyLater) {
		return *sits
	}
	return want
}

// pass moves every task it may once, and reports whether any moved.
func (r *recalc) pass() bool {
	moved := false
	for _, t := range r.plan.Tasks {
		if !r.moves(t) {
			continue
		}
		if len(r.children[t.ID]) == 0 {
			duration := r.duration(t)
			var sits *string
			if at := r.at[t.ID]; at != nil {
				sits = &at.start
			}
			start := r.wanted(t, duration, sits)
			if start == "" || (sits != nil && *sits == start) {
				continue
			}
			r.at[t.ID] = &placed{start, r.own(t).End(start, duration)}
			moved = true
			continue
		}
		span := r.span(t.ID)
		if span == nil {
			continue
		}
		start := r.wanted(t, r.cal.Span(span.start, span.end), &span.start)
		if start == "" || start == span.start {
			continue
		}
		moved = r.shift(t.ID, r.cal.Between(span.start, start)) || moved
	}
	return moved
}

// shift moves the tasks beneath a task by n working days: those with no
// sub-tasks that are not done and that no relationship of their own places.
func (r *recalc) shift(id string, n int) bool {
	moved := false
	for _, k := range r.children[id] {
		if len(r.children[k.ID]) > 0 {
			moved = r.shift(k.ID, n) || moved
			continue
		}
		at := r.at[k.ID]
		if at == nil || k.Done || len(r.drivers[k.ID]) > 0 || n == 0 {
			continue
		}
		start := r.cal.OnOrAfter(r.cal.Add(at.start, n))
		r.at[k.ID] = &placed{start, r.own(k).End(start, r.own(k).Span(at.start, at.end))}
		moved = true
	}
	return moved
}

// late moves, as late as their free float allows, the tasks that are to be
// as late as possible: those with that constraint, and every unconstrained
// task of a plan scheduled from its finish. A move frees room for the task
// before it, so it goes round until nothing moves.
func (r *recalc) late(in Inputs, now time.Time) {
	late := func(t *Task) bool {
		if !r.moves(t) && !(t.ConstraintType == ConstraintALAP && !t.Done && !r.how.auto) {
			return false
		}
		if len(r.children[t.ID]) > 0 || r.at[t.ID] == nil {
			return false
		}
		return t.ConstraintType == ConstraintALAP || (t.ConstraintType == ConstraintASAP && r.set.ScheduleFrom == FromFinish)
	}
	any := false
	for _, t := range r.plan.Tasks {
		any = any || late(t)
	}
	for pass := 0; any && pass <= len(r.plan.Tasks); pass++ {
		p := Build(r.inputs(in), now)
		moved := false
		for _, t := range p.Tasks {
			old := r.byID[t.ID]
			if old == nil || !late(old) || t.FreeFloatDays == nil || *t.FreeFloatDays <= 0 {
				continue
			}
			at := r.at[t.ID]
			start := r.cal.OnOrAfter(r.cal.Add(at.start, *t.FreeFloatDays))
			r.at[t.ID] = &placed{start, r.own(old).End(start, r.own(old).Span(at.start, at.end))}
			moved = true
		}
		if !moved {
			return
		}
	}
}

// inputs is the plan's inputs with each task where it now sits.
func (r *recalc) inputs(in Inputs) Inputs {
	out := in
	seen := map[string]bool{}
	out.Schedules = make([]*TaskSchedule, 0, len(in.Schedules))
	for _, sc := range in.Schedules {
		c := *sc
		if at := r.at[sc.WorkItemID]; at != nil {
			c.StartDate = &at.start
		}
		seen[sc.WorkItemID] = true
		out.Schedules = append(out.Schedules, &c)
	}
	out.Items = make([]*workitems.WorkItem, 0, len(in.Items))
	for _, it := range in.Items {
		if it == nil {
			continue
		}
		c := *it
		if at := r.at[it.ID]; at != nil {
			due, _ := time.Parse(DateLayout, at.end)
			c.DueDate = &due
			if !seen[it.ID] {
				out.Schedules = append(out.Schedules, &TaskSchedule{WorkItemID: it.ID, ProjectID: in.ProjectID, StartDate: &at.start})
			}
		}
		out.Items = append(out.Items, &c)
	}
	return out
}
