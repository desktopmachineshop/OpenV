// A plan as a project baseline records it: its figures and its milestones
// on the day the baseline was taken, beside the tasks a plan baseline keeps.

package plan

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// MomentMilestone is a milestone as a baseline recorded it.
type MomentMilestone struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Date     *string `json:"date"`
	Progress int     `json:"progress"`
	Met      bool    `json:"met"`
}

// Moment is where a plan stood: how far along it was, how many of its
// tasks were done and late, the days it ran from and to, and its
// milestones.
type Moment struct {
	Progress   int               `json:"progress"`
	Tasks      int               `json:"tasks"`
	Done       int               `json:"done"`
	Late       int               `json:"late"`
	Start      *string           `json:"start"`
	End        *string           `json:"end"`
	Milestones []MomentMilestone `json:"milestones"`
}

// MomentOf reads where a computed plan stands.
func MomentOf(p *Plan) *Moment {
	m := &Moment{Progress: p.Summary.Progress, Tasks: p.Summary.Tasks, Done: p.Summary.Done, Late: p.Summary.Late, Start: p.Start, End: p.End, Milestones: []MomentMilestone{}}
	for _, ms := range p.Milestones {
		m.Milestones = append(m.Milestones, MomentMilestone{Key: ms.Key, Name: ms.Name, Kind: ms.Kind, Date: ms.Date, Progress: ms.Progress, Met: ms.Met})
	}
	return m
}

// SaveBaselineOf records a computed plan as a new baseline: its tasks, and
// where it stood. With joined, the id of a project baseline, the plan
// baseline belongs to it and does not count toward MaxBaselines, which
// bounds the ones saved from the plan alone.
func (s *DefaultService) SaveBaselineOf(p *Plan, name string, joined *string, now time.Time) (*Baseline, error) {
	if joined == nil {
		existing, err := s.repo.ListBaselines(p.ProjectID)
		if err != nil {
			return nil, err
		}
		alone := 0
		for _, b := range existing {
			if b.BaselineID == nil {
				alone++
			}
		}
		if alone >= MaxBaselines {
			return nil, ErrTooManyBaselines
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Baseline " + now.UTC().Format(DateLayout)
	}
	if len(name) > 120 {
		return nil, ErrNameRequired
	}
	b := &Baseline{ID: uuid.NewString(), ProjectID: p.ProjectID, Name: name, CreatedAt: now.UTC(), Tasks: []BaselineTask{}, BaselineID: joined, Moment: MomentOf(p)}
	b.Tasks = TasksOf(p)
	return b, s.repo.CreateBaseline(b)
}

// TasksOf reads a computed plan's tasks as a baseline records them.
func TasksOf(p *Plan) []BaselineTask {
	out := []BaselineTask{}
	for _, t := range p.Tasks {
		out = append(out, BaselineTask{ID: t.ID, Start: t.SpanStart, End: t.SpanEnd, DurationDays: t.DurationDays, EffortDays: t.EffortDays, Cost: t.Cost,
			Title: t.Title, Progress: t.Progress, Done: t.Done})
	}
	return out
}

// JoinedBaselines answers the plan baseline taken with each project
// baseline that recorded the plan, by the project baseline's id.
func (s *DefaultService) JoinedBaselines(projectID string) (map[string]*Baseline, error) {
	list, err := s.repo.ListBaselines(projectID)
	if err != nil {
		return nil, err
	}
	out := map[string]*Baseline{}
	for _, b := range list {
		if b.BaselineID != nil {
			out[*b.BaselineID] = b
		}
	}
	return out, nil
}

// Kinds of change to a task between two moments of a plan.
const (
	TaskAdded    = "added"
	TaskRemoved  = "removed"
	TaskFinished = "finished"
	TaskReopened = "reopened"
	TaskMoved    = "moved"
	TaskProgress = "progress"
)

// TaskChange is what happened to one task between two moments of a plan.
type TaskChange struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Change is the kind, the most telling one when several apply: added
	// or removed; finished or reopened; moved, when its dates changed;
	// progress, when only how far along it is did.
	Change string `json:"change"`
	// Before and After are the task on each side, nil where it was not there.
	Before *BaselineTask `json:"before"`
	After  *BaselineTask `json:"after"`
	// StartShiftDays and FinishShiftDays are how many calendar days later
	// (negative: earlier) the task starts and ends, 0 when a date did not
	// move or either side has none.
	StartShiftDays  int `json:"start_shift_days"`
	FinishShiftDays int `json:"finish_shift_days"`
}

// TaskChanges lists the tasks that differ between two moments of a plan,
// in the order of after, then the ones only before has. A task that reads
// the same on both sides is left out.
func TaskChanges(before, after []BaselineTask) []TaskChange {
	was := map[string]*BaselineTask{}
	for i := range before {
		was[before[i].ID] = &before[i]
	}
	out := []TaskChange{}
	seen := map[string]bool{}
	for i := range after {
		now := &after[i]
		seen[now.ID] = true
		old := was[now.ID]
		if old == nil {
			out = append(out, TaskChange{ID: now.ID, Title: now.Title, Change: TaskAdded, After: now})
			continue
		}
		c := TaskChange{ID: now.ID, Title: now.Title, Before: old, After: now,
			StartShiftDays: dayShift(old.Start, now.Start), FinishShiftDays: dayShift(old.End, now.End)}
		switch {
		case !old.Done && now.Done:
			c.Change = TaskFinished
		case old.Done && !now.Done:
			c.Change = TaskReopened
		case !sameDate(old.Start, now.Start) || !sameDate(old.End, now.End):
			c.Change = TaskMoved
		case old.Progress != now.Progress:
			c.Change = TaskProgress
		default:
			continue
		}
		out = append(out, c)
	}
	for i := range before {
		if old := &before[i]; !seen[old.ID] {
			out = append(out, TaskChange{ID: old.ID, Title: old.Title, Change: TaskRemoved, Before: old})
		}
	}
	return out
}

func sameDate(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// dayShift is the calendar days from one date to another, 0 when either is
// missing or unreadable.
func dayShift(from, to *string) int {
	if from == nil || to == nil {
		return 0
	}
	a, err1 := time.Parse(DateLayout, *from)
	b, err2 := time.Parse(DateLayout, *to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}

// BaselineMoments answers where the plan stood at each project baseline
// that recorded it, by the project baseline's id.
func (s *DefaultService) BaselineMoments(projectID string) (map[string]*Moment, error) {
	joined, err := s.JoinedBaselines(projectID)
	if err != nil {
		return nil, err
	}
	out := map[string]*Moment{}
	for id, b := range joined {
		if b.Moment != nil {
			out[id] = b.Moment
		}
	}
	return out, nil
}
