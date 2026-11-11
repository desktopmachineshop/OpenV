// The critical path of a plan and each task's float (build.go calls it).

package plan

// criticalPath gives each scheduled task its float and marks the critical
// path. The plan's dates are the team's, so this is the float of the plan as
// it stands: a task can slip until the plan's last day, or until it would
// push a task it drives, whichever comes first.
//
//	finish to start:  the driven task's start, less a day, bounds the end
//	start to start:   the driven task's start bounds the start
//	finish to finish: the driven task's end bounds the end
//
// The critical path is the chain of tasks that decides when the plan ends.
// It starts at the tasks with no float and runs back from each task on it
// to the task that drives it most tightly, so it is shown whole even where
// the dates leave a gap between two of its tasks: FloatUntil says how far
// each task's end can slip before it pushes the next. A task on the path
// that is done, or has sub-tasks, is not marked. A broken relationship
// leaves no float.
func (b *builder) criticalPath(p *Plan) {
	var last *string
	for _, t := range p.Tasks {
		last = maxDate(last, t.SpanEnd)
	}
	if last == nil {
		return
	}
	// A plan with a finish date must end by it: float is counted to that
	// day, and goes negative for a task that runs past it.
	if b.in.Scheduling.ProjectFinish != nil {
		last = b.in.Scheduling.ProjectFinish
	}
	drives, drivers := map[string][]drive{}, map[string][]drive{}
	for _, d := range p.Dependencies {
		t, on := b.byID[d.WorkItemID], b.byID[d.DependsOnID]
		if t != nil && on != nil && t.SpanStart != nil && on.SpanStart != nil {
			drives[on.ID] = append(drives[on.ID], drive{t, d.Type, d.LagDays, d.LagUnit})
			drivers[t.ID] = append(drivers[t.ID], drive{on, d.Type, d.LagDays, d.LagUnit})
		}
	}
	float := map[string]int{}
	visiting := map[string]bool{}
	var slack func(t *Task) int
	slack = func(t *Task) int {
		if f, ok := float[t.ID]; ok {
			return f
		}
		f := b.cal.Between(*t.SpanEnd, *last)
		if visiting[t.ID] {
			return f
		}
		// With a path per chain, a task nothing waits for ends its own.
		if b.in.Scheduling.MultipleCriticalPaths && len(drives[t.ID]) == 0 && b.in.Scheduling.ProjectFinish == nil {
			f = 0
		}
		if limit := b.latest(t); limit != nil && *limit < f {
			f = *limit
		}
		visiting[t.ID] = true
		for _, d := range drives[t.ID] {
			if g := b.gap(t, d) + slack(d.task); g < f {
				f = g
			}
		}
		// A task that is done has nothing left to slip, or to make up.
		if t.Done {
			f = 0
		}
		visiting[t.ID] = false
		float[t.ID] = f
		return f
	}
	var path []*Task
	for _, t := range p.Tasks {
		if t.SpanStart == nil {
			continue
		}
		f := slack(t)
		t.FloatDays = &f
		t.FloatUntil = b.floatUntil(t, drives[t.ID], f)
		b.earlyLate(t, drives[t.ID], drivers[t.ID], f)
		if f <= b.in.CriticalSlackDays {
			path = append(path, t)
		}
	}
	b.markPath(path, drivers)
	// A folder knows when the path runs through it, at any depth.
	for i := len(p.Tasks) - 1; i >= 0; i-- {
		t := p.Tasks[i]
		if (t.Critical || t.CriticalSubtasks) && t.ParentID != nil {
			b.byID[*t.ParentID].CriticalSubtasks = true
		}
	}
}

// latest is how many working days a task can slip before it breaks its own
// deadline or its constraint (start or finish no later than, must start or
// finish on); nil when it has neither. Negative: it is already past it.
func (b *builder) latest(t *Task) *int {
	var out *int
	bound := func(from, to string) {
		n := b.cal.Between(from, to)
		if out == nil || n < *out {
			out = &n
		}
	}
	if t.Deadline != nil {
		bound(*t.SpanEnd, *t.Deadline)
	}
	if t.ConstraintDate != nil {
		switch t.ConstraintType {
		case ConstraintFNLT, ConstraintMFO:
			bound(*t.SpanEnd, *t.ConstraintDate)
		case ConstraintSNLT, ConstraintMSO:
			bound(*t.SpanStart, *t.ConstraintDate)
		}
	}
	return out
}

// earlyLate gives a task its early and late dates and its free float. The
// late dates are its dates moved by its float. The early start is the
// latest of what the tasks that drive it ask, the plan's start and its own
// "no earlier than" constraint; with none of those it is the task's start.
func (b *builder) earlyLate(t *Task, drives, drivers []drive, float int) {
	start, end := b.cal.OnOrAfter(*t.SpanStart), b.cal.OnOrBefore(*t.SpanEnd)
	ls, lf := b.cal.Add(start, float), b.cal.Add(end, float)
	t.LateStart, t.LateFinish = &ls, &lf
	free := float
	for i, d := range drives {
		if g := b.gap(t, d); i == 0 || g < free {
			free = g
		}
	}
	t.FreeFloatDays = &free

	own := b.cal
	if t.Elapsed && len(b.children[t.ID]) == 0 {
		own = elapsedCalendar
	}
	duration := own.Span(*t.SpanStart, *t.SpanEnd)
	early := ""
	bound := func(day string) {
		if day > early {
			early = day
		}
	}
	for _, d := range drivers {
		on := d.task
		asked := askedDate(b.cal, d.kind, d.lag, d.unit, *on.SpanStart, *on.SpanEnd, b.cal.Span(*on.SpanStart, *on.SpanEnd))
		if endDriven(d.kind) {
			asked = own.Start(asked, duration)
		}
		bound(asked)
	}
	if b.in.Scheduling.ProjectStart != nil {
		bound(*b.in.Scheduling.ProjectStart)
	}
	if t.ConstraintDate != nil {
		switch t.ConstraintType {
		case ConstraintSNET, ConstraintMSO:
			bound(*t.ConstraintDate)
		case ConstraintFNET, ConstraintMFO:
			bound(own.Start(*t.ConstraintDate, duration))
		}
	}
	if early == "" {
		early = *t.SpanStart
	}
	es := b.cal.OnOrAfter(early)
	ef := own.End(es, duration)
	t.EarlyStart, t.EarlyFinish = &es, &ef
}

// drive is one end of a relationship seen from the other: the task driven,
// or the task that drives, and the relationship's type.
type drive struct {
	task *Task
	kind string
	lag  int
	unit string
}

// gap is how many working days the driving task t can slip before it pushes
// the task it drives: the working days from the day the relationship asks
// of the driven task to the day that task has. Negative: the dates break
// the relationship by that much.
func (b *builder) gap(t *Task, d drive) int {
	duration := b.cal.Span(*t.SpanStart, *t.SpanEnd)
	want := askedDate(b.cal, d.kind, d.lag, d.unit, *t.SpanStart, *t.SpanEnd, duration)
	if endDriven(d.kind) {
		return b.cal.Between(want, b.cal.OnOrBefore(*d.task.SpanEnd))
	}
	return b.cal.Between(want, b.cal.OnOrAfter(*d.task.SpanStart))
}

// endDriven reports whether a relationship of the type places the driven
// task's end (finish to finish, start to finish) rather than its start.
func endDriven(kind string) bool {
	return kind == RelFinishToFinish || kind == RelStartToFinish
}

// askedDate is the day a relationship asks of the task it drives, given
// where the driving task sits: of its start for finish to start (the
// working day after the driver ends) and start to start (the day the driver
// starts); of its end for finish to finish (the day the driver ends) and
// start to finish (the day the driver starts). The offset then moves it:
// by working days, by calendar days, or by a percentage of the driver's
// duration in working days.
func askedDate(cal Calendar, kind string, lag int, unit, onStart, onEnd string, onDuration int) string {
	var day string
	switch kind {
	case RelStartToStart, RelStartToFinish:
		day = onStart
	case RelFinishToFinish:
		day = onEnd
	default:
		day = cal.Add(cal.OnOrBefore(onEnd), 1)
	}
	snap := cal.OnOrAfter
	if endDriven(kind) {
		snap = cal.OnOrBefore
	}
	switch unit {
	case LagElapsedDays:
		return snap(addDays(day, lag))
	case LagPercent:
		// Half a day rounds away from zero.
		n := onDuration * lag
		if n >= 0 {
			n = (n + 50) / 100
		} else {
			n = (n - 50) / 100
		}
		return cal.Add(snap(day), n)
	}
	return cal.Add(snap(day), lag)
}

// markPath walks the critical path back from the tasks with no float: from
// each task on it to the driver, or drivers, with the least room before
// they push it.
func (b *builder) markPath(path []*Task, drivers map[string][]drive) {
	seen := map[string]bool{}
	for len(path) > 0 {
		t := path[0]
		path = path[1:]
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		// A task with sub-tasks is not itself on the path: its sub-tasks are.
		t.Critical = !t.Done && len(b.children[t.ID]) == 0
		least := -1
		for _, d := range drivers[t.ID] {
			if g := b.gap(d.task, drive{t, d.kind, d.lag, d.unit}); least < 0 || g < least {
				least = g
			}
		}
		for _, d := range drivers[t.ID] {
			if b.gap(d.task, drive{t, d.kind, d.lag, d.unit}) == least {
				path = append(path, d.task)
			}
		}
	}
}

// floatUntil is the last day a task's end can slip to: before it pushes a
// task it drives, or, for a task that drives none, before the plan would
// end later. Nil when it has no room at all.
func (b *builder) floatUntil(t *Task, drives []drive, float int) *string {
	room := float
	for i, d := range drives {
		if g := b.gap(t, d); i == 0 || g < room {
			room = g
		}
	}
	if room <= 0 {
		return nil
	}
	until := b.cal.Add(b.cal.OnOrBefore(*t.SpanEnd), room)
	return &until
}

func addDays(day string, n int) string {
	return parseDay(day).AddDate(0, 0, n).Format(DateLayout)
}

func daysApart(a, b string) int {
	return int(parseDay(b).Sub(parseDay(a)).Hours() / 24)
}
