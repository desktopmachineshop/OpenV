// The people and agents who do a plan's work: who each task is assigned to
// and for what share of their time, what the work costs, how loaded each
// one is day by day, and levelling, which delays tasks until nobody is
// asked for more than their capacity.

package plan

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// Assignment puts a person or an agent on a task for a share of their time.
type Assignment struct {
	WorkItemID string `json:"-"`
	// Type is "user" or "agent"; ID is theirs.
	Type string `json:"type"`
	ID   string `json:"id"`
	// Units is the percentage of their working time the task takes.
	Units int `json:"units"`
}

// Resource is what a project keeps about a person or an agent who does its
// work: what a day of theirs costs, and the percentage of a working day
// they can give the project.
type Resource struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	DailyRate float64 `json:"daily_rate"`
	Capacity  int     `json:"capacity"`
}

// Bounds on assignments and resources.
const (
	MaxAssignments = 20
	MaxUnits       = 1000
	// loadHorizon bounds, in days, the tasks whose load is counted day by
	// day: a task longer than that is costed but not levelled.
	loadHorizon = 1500
)

// Person names someone a task can be assigned to. The plan's reader fills
// the list in; the plan itself knows only ids.
type Person struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Overallocation is a run of working days on which a resource is asked for
// more than their capacity; Units is the most asked on any of them.
type Overallocation struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Units int    `json:"units"`
}

// ResourceLoad is a resource with the plan's work on them.
type ResourceLoad struct {
	Resource
	Name string `json:"name"`
	// Tasks counts the tasks assigned; WorkDays is their work, in days of
	// this resource's time; Cost is that work at the daily rate.
	Tasks    int     `json:"tasks"`
	WorkDays float64 `json:"work_days"`
	Cost     float64 `json:"cost"`
	// Peak is the most asked of them on any day, a percentage.
	Peak            int              `json:"peak"`
	Overallocations []Overallocation `json:"overallocations"`
	// TimeOff is the days they are away, from the workspace's calendar.
	TimeOff []TimeOff `json:"time_off"`
}

// CleanAssignments checks who a task is to be assigned to: each a user or
// an agent, once, for 1 to 1000 percent of their time (100 when left out).
func CleanAssignments(in []Assignment) ([]Assignment, error) {
	seen := map[string]bool{}
	out := []Assignment{}
	for _, a := range in {
		a.Type, a.ID = strings.ToLower(strings.TrimSpace(a.Type)), strings.TrimSpace(a.ID)
		if a.Units == 0 {
			a.Units = 100
		}
		if (a.Type != "user" && a.Type != "agent") || a.ID == "" || a.Units < 1 || a.Units > MaxUnits {
			return nil, ErrAssignment
		}
		if key := a.Type + ":" + a.ID; !seen[key] {
			seen[key] = true
			out = append(out, Assignment{Type: a.Type, ID: a.ID, Units: a.Units})
		}
	}
	if len(out) > MaxAssignments {
		return nil, ErrAssignment
	}
	return out, nil
}

// SetResource stores a person's or an agent's daily rate and capacity.
func (s *DefaultService) SetResource(projectID string, res Resource) (Resource, error) {
	res.Type = strings.ToLower(strings.TrimSpace(res.Type))
	if res.Capacity == 0 {
		res.Capacity = 100
	}
	if (res.Type != "user" && res.Type != "agent") || res.ID == "" || res.DailyRate < 0 || res.DailyRate > 1e9 ||
		res.Capacity < 1 || res.Capacity > MaxUnits {
		return res, ErrResource
	}
	return res, s.repo.SaveResource(projectID, res)
}

// drivenDuration is the duration an effort-driven task's effort gives it:
// the effort shared among the people on it, each at their share of a day,
// rounded up to whole days.
func drivenDuration(effort float64, assignments []Assignment) int {
	units := 0
	for _, a := range assignments {
		units += a.Units
	}
	if units == 0 {
		units = 100
	}
	days := int(math.Ceil(effort*100/float64(units) - 1e-9))
	if days < 1 {
		days = 1
	}
	return days
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// resources gives each task its assignments and its cost, and the plan its
// resources with their work, cost and load.
func (b *builder) resources(p *Plan) {
	loads := map[string]*ResourceLoad{}
	days := map[string]map[string]int{}
	get := func(kind, id string) *ResourceLoad {
		key := kind + ":" + id
		if loads[key] == nil {
			loads[key] = &ResourceLoad{Resource: Resource{Type: kind, ID: id, Capacity: 100}, Overallocations: []Overallocation{}, TimeOff: []TimeOff{}}
			days[key] = map[string]int{}
		}
		return loads[key]
	}
	for _, res := range b.in.Resources {
		l := get(res.Type, res.ID)
		l.DailyRate, l.Capacity = res.DailyRate, res.Capacity
	}
	for _, t := range p.Tasks {
		t.Assignments = b.who(t)
		if t.Assignments == nil {
			t.Assignments = []Assignment{}
		}
		t.Cost = t.FixedCost
		if len(b.children[t.ID]) > 0 {
			continue
		}
		b.work(t, get, days)
	}
	// A task with sub-tasks costs what they cost, with its own fixed cost.
	for i := len(p.Tasks) - 1; i >= 0; i-- {
		t := p.Tasks[i]
		t.Cost = round2(t.Cost)
		if t.ParentID != nil {
			b.byID[*t.ParentID].Cost += t.Cost
		}
		if t.Depth == 0 {
			p.Summary.Cost += t.Cost
		}
	}
	p.Summary.Cost = round2(p.Summary.Cost)
	p.Resources = make([]ResourceLoad, 0, len(loads))
	over := map[string]map[string]bool{}
	for key, l := range loads {
		l.WorkDays, l.Cost = round2(l.WorkDays), round2(l.Cost)
		l.Overallocations, over[key] = b.overallocations(days[key], l.Capacity)
		for _, t := range b.in.TimeOff {
			if t.Type+":"+t.AssigneeID == key {
				l.TimeOff = append(l.TimeOff, t)
			}
		}
		for _, n := range days[key] {
			if n > l.Peak {
				l.Peak = n
			}
		}
		p.Resources = append(p.Resources, *l)
	}
	sort.Slice(p.Resources, func(i, j int) bool {
		x, y := p.Resources[i], p.Resources[j]
		if x.Type != y.Type {
			return x.Type > y.Type // users before agents
		}
		return x.ID < y.ID
	})
	for _, t := range p.Tasks {
		if t.SpanStart == nil || len(b.children[t.ID]) > 0 || t.Done {
			continue
		}
		for _, a := range t.Assignments {
			for day := range over[a.Type+":"+a.ID] {
				if day >= *t.SpanStart && day <= *t.SpanEnd {
					t.Overallocated = true
				}
			}
		}
	}
}

// work puts a task's work and cost on the people assigned to it, and its
// units on each working day it runs.
func (b *builder) work(t *Task, get func(kind, id string) *ResourceLoad, days map[string]map[string]int) {
	units := 0
	for _, a := range t.Assignments {
		units += a.Units
	}
	if units == 0 {
		return
	}
	cal := taskCalendar(b.cal, t.Elapsed, t.Assignments, b.off)
	duration := 0
	if t.SpanStart != nil {
		duration = cal.Span(*t.SpanStart, *t.SpanEnd)
	} else if t.DurationDays != nil {
		duration = *t.DurationDays
	}
	for _, a := range t.Assignments {
		l := get(a.Type, a.ID)
		l.Tasks++
		// The task's effort shared by units, or else each person's share
		// of every day it runs.
		work := float64(duration) * float64(a.Units) / 100
		if t.EffortDays != nil {
			work = *t.EffortDays * float64(a.Units) / float64(units)
		}
		l.WorkDays += work
		l.Cost += work * l.DailyRate
		t.Cost += work * l.DailyRate
		if t.SpanStart == nil || t.Done || daysApart(*t.SpanStart, *t.SpanEnd) > loadHorizon {
			continue
		}
		key := a.Type + ":" + a.ID
		for d := parseDay(*t.SpanStart); !d.After(parseDay(*t.SpanEnd)); d = d.AddDate(0, 0, 1) {
			// A day they are away asks nothing of them.
			if day := d.Format(DateLayout); cal.works(d) && !b.off[key][day] {
				days[key][day] += a.Units
			}
		}
	}
}

// overallocations turns a resource's load by day into the runs of days on
// which more than their capacity is asked, and the set of those days.
func (b *builder) overallocations(load map[string]int, capacity int) ([]Overallocation, map[string]bool) {
	over := map[string]bool{}
	list := []string{}
	for day, n := range load {
		if n > capacity {
			over[day] = true
			list = append(list, day)
		}
	}
	sort.Strings(list)
	out := []Overallocation{}
	for _, day := range list {
		// A run goes on while the next over-loaded day is the next day the
		// plan works.
		if n := len(out); n > 0 && b.cal.Add(out[n-1].End, 1) >= day {
			out[n-1].End = day
			if load[day] > out[n-1].Units {
				out[n-1].Units = load[day]
			}
			continue
		}
		out = append(out, Overallocation{Start: day, End: day, Units: load[day]})
	}
	return out, over
}

// Level delays tasks until no person or agent is asked for more than their
// capacity, and answers how many tasks it moved. Where someone is
// over-allocated it leaves in place the task with the least float and
// delays another, the one with the most, until after the first of the rest
// ends; the tasks those drive then follow. It never moves a task earlier,
// or one that is done.
func (s *DefaultService) Level(projectID, actor string, now time.Time) (int, error) {
	in, cal, set, err := s.inputs(projectID)
	if err != nil {
		return 0, err
	}
	p := Build(in, now)
	r := newRecalc(p, cal, set.Scheduling, placing{onlyLater: true})
	for i := 0; i < 20*len(p.Tasks)+20; i++ {
		cur := Build(r.inputs(in), now)
		if !r.delayOne(cur) {
			break
		}
		for pass := 0; pass <= len(p.Tasks) && r.pass(); pass++ {
		}
	}
	return s.write(r, actor)
}

// delayOne finds the first day on which a resource is over-allocated and
// delays one of the tasks that load it; false when nobody is.
func (r *recalc) delayOne(cur *Plan) bool {
	var first *Overallocation
	var who ResourceLoad
	for _, res := range cur.Resources {
		for i := range res.Overallocations {
			if o := &res.Overallocations[i]; first == nil || o.Start < first.Start {
				first, who = o, res
			}
		}
	}
	if first == nil {
		return false
	}
	var on []*Task
	for _, t := range cur.Tasks {
		if t.SpanStart == nil || t.Done || t.SubtasksTotal > 0 || *t.SpanStart > first.Start || *t.SpanEnd < first.Start {
			continue
		}
		for _, a := range t.Assignments {
			if a.Type == who.Type && a.ID == who.ID {
				on = append(on, t)
			}
		}
	}
	if len(on) < 2 {
		return false
	}
	// Delay the task with the most float; of equals, the one that starts
	// later, then the one lower in the outline.
	sort.SliceStable(on, func(i, j int) bool {
		fi, fj := *on[i].FloatDays, *on[j].FloatDays
		if fi != fj {
			return fi > fj
		}
		return *on[i].SpanStart > *on[j].SpanStart
	})
	move := on[0]
	until := *on[1].SpanEnd
	for _, t := range on[2:] {
		if *t.SpanEnd < until {
			until = *t.SpanEnd
		}
	}
	old := r.byID[move.ID]
	at := r.at[move.ID]
	if old == nil || at == nil {
		return false
	}
	start := r.cal.Add(r.cal.OnOrBefore(until), 1)
	if start <= at.start {
		start = r.cal.Add(r.cal.OnOrAfter(at.start), 1)
	}
	r.at[move.ID] = &placed{start, r.own(old).End(start, r.own(old).Span(at.start, at.end))}
	return true
}

// assign stores the assignments a schedule request names, and gives an
// effort-driven task the duration its effort and the people on it decide:
// the due date then follows the start. It answers the task's due date and
// duration as they now are.
func (s *DefaultService) assign(item *workitems.WorkItem, next *TaskSchedule, req ScheduleRequest, start, due *string, duration *int,
	effort *float64) (*string, *int, error) {
	var assignments []Assignment
	if req.Assignments != nil {
		clean, err := CleanAssignments(*req.Assignments)
		if err != nil {
			return nil, nil, err
		}
		if err := s.repo.SetAssignments(item.ProjectID, item.ID, clean); err != nil {
			return nil, nil, err
		}
		assignments = clean
	}
	if !next.EffortDriven || effort == nil {
		return due, duration, nil
	}
	if req.Assignments == nil {
		all, err := s.repo.ListAssignments(item.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		for _, a := range all {
			if a.WorkItemID == item.ID {
				assignments = append(assignments, a)
			}
		}
	}
	days := drivenDuration(*effort, assignments)
	duration = &days
	if start != nil {
		base, _, err := s.calendar(item.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		_, cal, err := s.taskCalendars(item, base, next.Elapsed, ScheduleRequest{})
		if err != nil {
			return nil, nil, err
		}
		end := cal.End(*start, days)
		due = &end
	}
	return due, duration, nil
}
