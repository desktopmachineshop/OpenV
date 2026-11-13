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
	for _, t := range p.Tasks {
		b.Tasks = append(b.Tasks, BaselineTask{ID: t.ID, Start: t.SpanStart, End: t.SpanEnd, DurationDays: t.DurationDays, EffortDays: t.EffortDays, Cost: t.Cost})
	}
	return b, s.repo.CreateBaseline(b)
}

// BaselineMoments answers where the plan stood at each project baseline
// that recorded it, by the project baseline's id.
func (s *DefaultService) BaselineMoments(projectID string) (map[string]*Moment, error) {
	list, err := s.repo.ListBaselines(projectID)
	if err != nil {
		return nil, err
	}
	out := map[string]*Moment{}
	for _, b := range list {
		if b.BaselineID != nil && b.Moment != nil {
			out[*b.BaselineID] = b.Moment
		}
	}
	return out, nil
}
