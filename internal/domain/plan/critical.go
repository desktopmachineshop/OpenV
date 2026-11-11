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
	drives, drivers := map[string][]drive{}, map[string][]drive{}
	for _, d := range p.Dependencies {
		t, on := b.byID[d.WorkItemID], b.byID[d.DependsOnID]
		if t != nil && on != nil && t.SpanStart != nil && on.SpanStart != nil {
			drives[on.ID] = append(drives[on.ID], drive{t, d.Type, d.LagDays})
			drivers[t.ID] = append(drivers[t.ID], drive{on, d.Type, d.LagDays})
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
		visiting[t.ID] = true
		for _, d := range drives[t.ID] {
			if g := b.gap(t, d) + slack(d.task); g < f {
				f = g
			}
		}
		if f < 0 {
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
		t.FloatUntil = b.floatUntil(t, drives[t.ID])
		if f == 0 {
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

// drive is one end of a relationship seen from the other: the task driven,
// or the task that drives, and the relationship's type.
type drive struct {
	task *Task
	kind string
	lag  int
}

// gap is how many working days the driving task t can slip before it pushes
// the task it drives.
func (b *builder) gap(t *Task, d drive) int {
	switch d.kind {
	case RelStartToStart:
		return b.cal.Between(*t.SpanStart, *d.task.SpanStart) - d.lag
	case RelFinishToFinish:
		return b.cal.Between(*t.SpanEnd, *d.task.SpanEnd) - d.lag
	}
	// The working days strictly between its end and that start.
	gap := b.cal.Between(*t.SpanEnd, *d.task.SpanStart)
	if b.cal.Works(*d.task.SpanStart) {
		gap--
	}
	return gap - d.lag
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
			if g := b.gap(d.task, drive{t, d.kind, d.lag}); least < 0 || g < least {
				least = g
			}
		}
		for _, d := range drivers[t.ID] {
			if b.gap(d.task, drive{t, d.kind, d.lag}) == least {
				path = append(path, d.task)
			}
		}
	}
}

// floatUntil is the last day a task's end can slip to before it pushes a
// task it drives: nil when it drives none, or has no room at all.
func (b *builder) floatUntil(t *Task, drives []drive) *string {
	var until *string
	for _, d := range drives {
		var day string
		switch d.kind {
		case RelStartToStart:
			day = addDays(*t.SpanEnd, daysApart(*t.SpanStart, *d.task.SpanStart))
		case RelFinishToFinish:
			day = *d.task.SpanEnd
		default:
			day = addDays(*d.task.SpanStart, -1)
		}
		day = b.cal.Add(day, -d.lag)
		if until == nil || day < *until {
			until = &day
		}
	}
	if until == nil || *until <= *t.SpanEnd {
		return nil
	}
	return until
}

func addDays(day string, n int) string {
	return parseDay(day).AddDate(0, 0, n).Format(DateLayout)
}

func daysApart(a, b string) int {
	return int(parseDay(b).Sub(parseDay(a)).Hours() / 24)
}
