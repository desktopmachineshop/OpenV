// Recalculating a plan: moving the tasks that others drive to the dates
// their relationships ask for.

package plan

import (
	"time"
)

// Recalculate moves each task that another task drives to the dates its
// relationships, with their offsets, ask for, and answers how many tasks it
// moved. A task keeps its duration; a task nothing drives stays where it
// is, and so does one that is done. A driven task with sub-tasks moves them
// by as many working days as it has to move. The plan's dates are still the
// team's: this is the one place the platform sets them, and only when asked.
func (s *DefaultService) Recalculate(projectID, actor string, now time.Time) (int, error) {
	items, err := s.board.ListByProject(projectID)
	if err != nil {
		return 0, err
	}
	schedules, err := s.repo.ListSchedules(projectID)
	if err != nil {
		return 0, err
	}
	deps, err := s.repo.ListDependencies(projectID)
	if err != nil {
		return 0, err
	}
	days, err := s.repo.WorkingDays(projectID)
	if err != nil {
		return 0, err
	}
	p := Build(Inputs{ProjectID: projectID, Items: items, Schedules: schedules, Dependencies: deps, WorkingDays: days}, now)
	r := newRecalc(p, NewCalendar(days))
	// A move can push what the moved task drives, so go round until a pass
	// moves nothing: at most once per task along the longest chain.
	for pass := 0; pass <= len(p.Tasks) && r.pass(); pass++ {
	}
	moved := 0
	for _, t := range p.Tasks {
		was, is := r.was[t.ID], r.at[t.ID]
		if is == nil || (was != nil && *was == *is) {
			continue
		}
		if err := s.Schedule(t.ID, ScheduleRequest{StartDate: &is.start, DueDate: &is.end}, actor); err != nil {
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
	byID     map[string]*Task
	children map[string][]*Task
	drivers  map[string][]Dependency
	// at is where each task with no sub-tasks sits now; was, where it sat.
	at, was map[string]*placed
}

func newRecalc(p *Plan, cal Calendar) *recalc {
	r := &recalc{plan: p, cal: cal, byID: map[string]*Task{}, children: map[string][]*Task{}, drivers: map[string][]Dependency{},
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

// duration is how many working days a task runs: what it keeps, what its
// dates span, or a day.
func (r *recalc) duration(t *Task) int {
	if at := r.at[t.ID]; at != nil {
		return r.cal.Span(at.start, at.end)
	}
	if t.DurationDays != nil && *t.DurationDays > 0 {
		return *t.DurationDays
	}
	return 1
}

// wanted is the start a task's relationships ask of it: the latest of what
// each asks, or "" when no task that drives it has dates.
func (r *recalc) wanted(t *Task, duration int) string {
	want := ""
	for _, d := range r.drivers[t.ID] {
		on := r.span(d.DependsOnID)
		if on == nil {
			continue
		}
		var start string
		switch d.Type {
		case RelStartToStart:
			start = r.cal.Add(on.start, d.LagDays)
		case RelFinishToFinish:
			start = r.cal.Start(r.cal.Add(on.end, d.LagDays), duration)
		default:
			start = r.cal.Add(r.cal.Add(on.end, 1), d.LagDays)
		}
		if start > want {
			want = start
		}
	}
	if want == "" {
		return ""
	}
	// The first working day on or after it.
	return r.cal.End(want, 1)
}

// pass moves every driven task once, and reports whether any moved.
func (r *recalc) pass() bool {
	moved := false
	for _, t := range r.plan.Tasks {
		if t.Done || len(r.drivers[t.ID]) == 0 {
			continue
		}
		if len(r.children[t.ID]) == 0 {
			duration := r.duration(t)
			start := r.wanted(t, duration)
			if start == "" || (r.at[t.ID] != nil && r.at[t.ID].start == start) {
				continue
			}
			r.at[t.ID] = &placed{start, r.cal.End(start, duration)}
			moved = true
			continue
		}
		span := r.span(t.ID)
		if span == nil {
			continue
		}
		start := r.wanted(t, r.cal.Span(span.start, span.end))
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
		start := r.cal.End(r.cal.Add(at.start, n), 1)
		r.at[k.ID] = &placed{start, r.cal.End(start, r.cal.Span(at.start, at.end))}
		moved = true
	}
	return moved
}
