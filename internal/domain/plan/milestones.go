// The plan's stored milestones and their criteria (plan.go has the types).

package plan

import (
	"github.com/google/uuid"
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/targetreleases"
)

// GetMilestone returns one stored milestone.
func (s *DefaultService) GetMilestone(id string) (*Milestone, error) { return s.repo.GetMilestone(id) }

// CreateMilestone adds a custom milestone.
func (s *DefaultService) CreateMilestone(projectID string, req MilestoneRequest) (*Milestone, error) {
	now := s.now()
	m := &Milestone{ID: uuid.New().String(), ProjectID: projectID, Criteria: []Criterion{}, CreatedAt: now, UpdatedAt: now}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return nil, ErrNameRequired
	}
	if err := applyMilestone(m, req); err != nil {
		return nil, err
	}
	if err := s.repo.CreateMilestone(m); err != nil {
		return nil, err
	}
	return m, nil
}

func applyMilestone(m *Milestone, req MilestoneRequest) error {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return ErrNameRequired
		}
		m.Name = name
	}
	if req.Description != nil {
		m.Description = strings.TrimSpace(*req.Description)
	}
	if req.TargetDate != nil {
		d, err := cleanDate(*req.TargetDate)
		if err != nil {
			return err
		}
		m.TargetDate = d
	}
	if req.Criteria != nil {
		c, err := CleanCriteria(*req.Criteria)
		if err != nil {
			return err
		}
		m.Criteria = c
	}
	return nil
}

// UpdateMilestone changes a milestone. A release's milestone takes only
// criteria: its name and date are the release's.
func (s *DefaultService) UpdateMilestone(id string, req MilestoneRequest) (*Milestone, error) {
	m, err := s.repo.GetMilestone(id)
	if err != nil {
		return nil, err
	}
	if m.ReleaseID != nil && (req.Name != nil || req.TargetDate != nil || req.Description != nil) {
		return nil, ErrReleaseMilestone
	}
	if err := applyMilestone(m, req); err != nil {
		return nil, err
	}
	m.UpdatedAt = s.now()
	if err := s.repo.UpdateMilestone(m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteMilestone removes a stored milestone. Removing a release's resets
// its criteria to the defaults; the release itself stays.
func (s *DefaultService) DeleteMilestone(id string) error { return s.repo.DeleteMilestone(id) }

// SetReleaseCriteria sets the criteria of a release's milestone, storing
// the milestone the first time.
func (s *DefaultService) SetReleaseCriteria(projectID, releaseID string, criteria []Criterion) (*Milestone, error) {
	clean, err := CleanCriteria(criteria)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.ListMilestones(projectID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, m := range stored {
		if m.ReleaseID != nil && *m.ReleaseID == releaseID {
			m.Criteria = clean
			m.UpdatedAt = now
			return m, s.repo.UpdateMilestone(m)
		}
	}
	m := &Milestone{ID: uuid.New().String(), ProjectID: projectID, ReleaseID: &releaseID, Criteria: clean, CreatedAt: now, UpdatedAt: now}
	return m, s.repo.CreateMilestone(m)
}

// milestones measures every milestone: each release of the project, then
// the custom ones, in date order with the undated last.
func milestones(in Inputs, tasks []*Task, counted map[string][]*Task, today string) []MilestoneProgress {
	ix := indexEvidence(in.Evidence)
	whole := ix.measure(nil)
	order := targetreleases.NewOrder(in.Releases)
	ranges := targetreleases.Resolve(in.Evidence.Artifacts, order)

	stored := map[string]*Milestone{}
	out := []MilestoneProgress{}
	for _, m := range in.Milestones {
		if m.ReleaseID != nil {
			stored[*m.ReleaseID] = m
		}
	}
	position := map[string]int{}

	measure := func(mp *MilestoneProgress, criteria []Criterion, values map[string]MetricValue) {
		measureMilestone(mp, criteria, values, tasks, counted[mp.Key])
	}

	for _, rel := range in.Releases {
		id := rel.ID
		mp := MilestoneProgress{Key: "release:" + id, Kind: KindRelease, ReleaseID: &id, Name: rel.Name,
			Description: rel.Description, Date: rel.TargetDate, ReleaseStatus: rel.Status}
		criteria := DefaultReleaseCriteria()
		mp.DefaultCriteria = true
		if m := stored[id]; m != nil {
			mp.ID, mp.DefaultCriteria, criteria = m.ID, false, m.Criteria
		}
		scope := map[string]bool{}
		idx := order[id]
		for artifactID, r := range ranges {
			if r.Contains(order, idx) {
				scope[artifactID] = true
			}
		}
		measure(&mp, criteria, ix.measure(scope))
		if rel.Status == targetreleases.StatusReleased {
			mp.Met, mp.Progress = true, 100
		}
		position[mp.Key] = idx
		out = append(out, mp)
	}
	for _, m := range in.Milestones {
		if m.ReleaseID != nil {
			continue
		}
		mp := MilestoneProgress{Key: m.ID, Kind: KindCustom, ID: m.ID, Name: m.Name, Description: m.Description, Date: m.TargetDate}
		measure(&mp, m.Criteria, whole)
		out = append(out, mp)
	}
	for i := range out {
		out[i].Late = !out[i].Met && out[i].Date != nil && *out[i].Date < today
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.Date != nil && b.Date == nil:
			return true
		case a.Date == nil && b.Date != nil:
			return false
		case a.Date != nil && *a.Date != *b.Date:
			return *a.Date < *b.Date
		}
		if a.Kind != b.Kind {
			return a.Kind == KindRelease
		}
		if a.Kind == KindRelease {
			return position[a.Key] < position[b.Key]
		}
		return a.Name < b.Name
	})
	return out
}

// measureMilestone measures a milestone's criteria against values,
// MetricTasksDone against the tasks due by its date and
// MetricMilestoneTasks against the tasks that count toward it. A milestone
// with tasks counts them whether or not its criteria say so: finishing them
// is part of reaching it.
func measureMilestone(mp *MilestoneProgress, criteria []Criterion, values map[string]MetricValue, tasks, counted []*Task) {
	mp.Criteria, mp.Tasks = []CriterionResult{}, []string{}
	named := false
	for _, c := range criteria {
		named = named || c.Metric == MetricMilestoneTasks
	}
	for _, t := range counted {
		mp.Tasks = append(mp.Tasks, t.ID)
	}
	if len(counted) > 0 && !named {
		criteria = append(append([]Criterion{}, criteria...), Criterion{MetricMilestoneTasks, 100})
	}
	var sum float64
	met := len(criteria) > 0
	for _, c := range criteria {
		v, ok := values[c.Metric]
		switch c.Metric {
		case MetricTasksDone:
			v, ok = tasksDone(tasks, mp.Date), true
		case MetricMilestoneTasks:
			v, ok = milestoneTasks(counted), true
		}
		if !ok {
			continue
		}
		r := CriterionResult{Metric: c.Metric, Label: v.Label, Target: c.Target, Actual: v.Percent, Count: v.Count, Total: v.Total}
		// Count against the target without the rounding of Percent, so
		// 199 of 200 is not 100%.
		r.Met = v.Total > 0 && v.Count*100 >= c.Target*v.Total
		way := 0.0
		if v.Total > 0 {
			way = float64(v.Count) * 100 / float64(v.Total) / float64(c.Target)
		}
		if way > 1 {
			way = 1
		}
		r.Percent = percentOf(way)
		if !r.Met && r.Percent == 100 {
			r.Percent = 99
		}
		sum += way
		met = met && r.Met
		mp.Criteria = append(mp.Criteria, r)
	}
	if len(mp.Criteria) > 0 {
		mp.Progress = percentOf(sum / float64(len(mp.Criteria)))
		if !met && mp.Progress == 100 {
			mp.Progress = 99
		}
	}
	mp.Met = met
}
