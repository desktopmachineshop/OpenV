// Tracking a plan against what was planned: baselines and the variance
// from one, actual dates and a percentage complete typed by hand, the
// status date, and earned value.

package plan

import (
	"time"
)

// MaxBaselines bounds the baselines a plan keeps.
const MaxBaselines = 11

// BaselineTask is a task as a baseline recorded it.
type BaselineTask struct {
	ID           string   `json:"id"`
	Start        *string  `json:"start"`
	End          *string  `json:"end"`
	DurationDays *int     `json:"duration_days"`
	EffortDays   *float64 `json:"effort_days"`
	Cost         float64  `json:"cost"`
}

// Baseline is a snapshot of a plan's tasks: what was planned on the day it
// was saved.
type Baseline struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Name      string         `json:"name"`
	CreatedAt time.Time      `json:"created_at"`
	Tasks     []BaselineTask `json:"tasks,omitempty"`
	// BaselineID is the project baseline this one belongs to, nil for one
	// saved from the plan alone; Moment where the plan stood, nil for a
	// baseline saved before that was kept.
	BaselineID *string `json:"baseline_id,omitempty"`
	Moment     *Moment `json:"moment,omitempty"`
}

// BaselineInfo names a baseline on the plan, without its tasks.
type BaselineInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Tasks     int       `json:"tasks"`
	// Project: the baseline belongs to a project baseline, which records
	// the definition with it and is deleted from the Baselines page.
	Project bool `json:"project"`
}

// Tracking is what a plan is tracked against: the day its progress is
// reported as of (today when not set) and the baseline it is compared with
// (the newest when not set).
type Tracking struct {
	StatusDate       *string `json:"status_date"`
	ActiveBaselineID *string `json:"active_baseline_id"`
}

// EarnedValue is the plan's, or a task's, earned value at the status date.
type EarnedValue struct {
	// BAC is the budget at completion: the baseline's cost, or today's cost
	// with no baseline. PV is what should have been spent by the status
	// date, EV what the work done is worth, AC what it has cost.
	BAC float64 `json:"bac"`
	PV  float64 `json:"pv"`
	EV  float64 `json:"ev"`
	AC  float64 `json:"ac"`
	// SV and CV are the schedule and cost variances (EV - PV, EV - AC); SPI
	// and CPI the indices (EV / PV, EV / AC), 0 when undefined; EAC the
	// estimate at completion (BAC / CPI).
	SV  float64 `json:"sv"`
	CV  float64 `json:"cv"`
	SPI float64 `json:"spi"`
	CPI float64 `json:"cpi"`
	EAC float64 `json:"eac"`
}

func (e *EarnedValue) derive() {
	e.BAC, e.PV, e.EV, e.AC = round2(e.BAC), round2(e.PV), round2(e.EV), round2(e.AC)
	e.SV, e.CV = round2(e.EV-e.PV), round2(e.EV-e.AC)
	e.SPI, e.CPI, e.EAC = 0, 0, e.BAC
	if e.PV > 0 {
		e.SPI = round2(e.EV / e.PV)
	}
	if e.AC > 0 {
		e.CPI = round2(e.EV / e.AC)
	}
	if e.CPI > 0 {
		e.EAC = round2(e.BAC / e.CPI)
	}
}

// SaveBaseline records the plan as it stands as a new baseline of the plan
// alone.
func (s *DefaultService) SaveBaseline(projectID, name string, now time.Time) (*Baseline, error) {
	in, _, _, err := s.inputs(projectID)
	if err != nil {
		return nil, err
	}
	return s.SaveBaselineOf(Build(in, now), name, nil, now)
}

// DeleteBaseline removes a baseline of the project that was saved from the
// plan alone; one that belongs to a project baseline is ErrJoinedBaseline.
func (s *DefaultService) DeleteBaseline(projectID, id string) error {
	return s.repo.DeleteBaseline(projectID, id)
}

// RescheduleRemaining moves the work that should have started by the status
// date and has not: each task that is not done, has no progress and starts
// before the status date goes to that day, keeping its duration, and the
// tasks it drives follow. It answers how many tasks moved.
func (s *DefaultService) RescheduleRemaining(projectID, actor string, now time.Time) (int, error) {
	in, cal, set, err := s.inputs(projectID)
	if err != nil {
		return 0, err
	}
	status := now.UTC().Format(DateLayout)
	if set.StatusDate != nil {
		status = *set.StatusDate
	}
	p := Build(in, now)
	r := newRecalc(p, cal, set.Scheduling, placing{onlyLater: true})
	first := cal.OnOrAfter(status)
	for _, t := range p.Tasks {
		at := r.at[t.ID]
		if at == nil || t.Done || t.Progress > 0 || t.ActualStart != nil || at.start >= status {
			continue
		}
		r.at[t.ID] = &placed{first, r.own(t).End(first, r.own(t).Span(at.start, at.end))}
	}
	for pass := 0; pass <= len(p.Tasks) && r.pass(); pass++ {
	}
	return s.write(r, actor)
}

// tracking compares the plan with its active baseline and measures its
// earned value at the status date.
func (b *builder) tracking(p *Plan) {
	p.Tracking = b.in.Tracking
	p.Baselines = []BaselineInfo{}
	var active *Baseline
	for _, bl := range b.in.Baselines {
		p.Baselines = append(p.Baselines, BaselineInfo{ID: bl.ID, Name: bl.Name, CreatedAt: bl.CreatedAt, Tasks: len(bl.Tasks), Project: bl.BaselineID != nil})
		if b.in.Tracking.ActiveBaselineID != nil && *b.in.Tracking.ActiveBaselineID == bl.ID {
			active = bl
		}
	}
	// With none chosen, the newest: baselines come oldest first.
	if active == nil && len(b.in.Baselines) > 0 {
		active = b.in.Baselines[len(b.in.Baselines)-1]
	}
	status := b.today
	if b.in.Tracking.StatusDate != nil {
		status = *b.in.Tracking.StatusDate
	}
	p.StatusDay = status
	planned := map[string]*BaselineTask{}
	if active != nil {
		p.BaselineID = &active.ID
		for i := range active.Tasks {
			planned[active.Tasks[i].ID] = &active.Tasks[i]
		}
	}
	for _, t := range p.Tasks {
		base := planned[t.ID]
		t.Baseline = base
		if base != nil && base.Start != nil && t.SpanStart != nil {
			sv, fv := b.cal.Between(*base.Start, *t.SpanStart), b.cal.Between(*base.End, *t.SpanEnd)
			t.StartVarianceDays, t.FinishVarianceDays = &sv, &fv
		}
		if len(b.children[t.ID]) > 0 {
			continue
		}
		ev := b.earned(t, base, status)
		t.EarnedValue = &ev
		p.EarnedValue.BAC += ev.BAC
		p.EarnedValue.PV += ev.PV
		p.EarnedValue.EV += ev.EV
		p.EarnedValue.AC += ev.AC
		t.BehindSchedule = !t.Done && ev.BAC > 0 && ev.EV < ev.PV
		if ev.BAC == 0 && !t.Done && t.SpanStart != nil {
			// With no money on it, by time: the share of its days gone by.
			t.BehindSchedule = t.fraction < b.share(taskCalendar(b.cal, t.Elapsed, t.Assignments, b.off), *t.SpanStart, *t.SpanEnd, status)
		}
	}
	p.EarnedValue.derive()
}

// share is the part of the working days from start to end that has passed
// by the end of day.
func (b *builder) share(cal Calendar, start, end, day string) float64 {
	switch {
	case day < start:
		return 0
	case day >= end:
		return 1
	}
	return float64(cal.Span(start, day)) / float64(cal.Span(start, end))
}

// earned is a task's earned value at the status date: its budget is the
// baseline's cost for it, or its cost now with no baseline, spread evenly
// over its planned days.
func (b *builder) earned(t *Task, base *BaselineTask, status string) EarnedValue {
	ev := EarnedValue{BAC: t.Cost}
	start, end := t.SpanStart, t.SpanEnd
	if base != nil {
		ev.BAC, start, end = base.Cost, base.Start, base.End
	}
	if start != nil && end != nil {
		// Planned work goes at the pace the task's people are there for.
		ev.PV = ev.BAC * b.share(taskCalendar(b.cal, t.Elapsed, t.Assignments, b.off), *start, *end, status)
	}
	ev.EV = ev.BAC * t.fraction
	ev.AC = t.Cost * t.fraction
	if t.ActualCost != nil {
		ev.AC = *t.ActualCost
	}
	ev.derive()
	return ev
}
