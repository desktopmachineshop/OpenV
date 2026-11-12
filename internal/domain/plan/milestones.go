// The plan's stored milestones and their criteria (plan.go has the types).

package plan

import (
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/targetreleases"
)

// GetMilestone returns one stored milestone.
func (s *DefaultService) GetMilestone(id string) (*Milestone, error) { return s.repo.GetMilestone(id) }

// Milestone is a stored milestone: a custom one, or the criteria of a
// release's (ReleaseID set; Name and TargetDate are then the release's and
// are not stored here).
type Milestone struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	ReleaseID   *string     `json:"release_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	TargetDate  *string     `json:"target_date"`
	Criteria    []Criterion `json:"criteria"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	// DateMode is how the milestone's date is set: DateFixed, the target
	// date typed, or DateByPlan, the day the last task it waits for
	// finishes. WaitsFor are the ids of those tasks.
	DateMode string   `json:"date_mode"`
	WaitsFor []string `json:"waits_for"`
}

// MilestoneRequest creates or changes a custom milestone. On an update a
// nil field is left as it is; an empty TargetDate clears the date.
type MilestoneRequest struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	TargetDate  *string      `json:"target_date"`
	Criteria    *[]Criterion `json:"criteria"`
	// DateMode is "fixed" or "plan"; WaitsFor replaces the tasks the
	// milestone waits for.
	DateMode *string   `json:"date_mode"`
	WaitsFor *[]string `json:"waits_for"`
}

// MilestoneProgress is a milestone on the plan with its criteria measured.
type MilestoneProgress struct {
	// Key names the milestone on the plan: its id, or "release:<id>" for a
	// release's.
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// ID is the stored milestone's id: empty for a release milestone whose
	// criteria are still the defaults.
	ID          string  `json:"id"`
	ReleaseID   *string `json:"release_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Date        *string `json:"date"`
	// ReleaseStatus is the release's status, for a release milestone.
	ReleaseStatus string `json:"release_status,omitempty"`
	// DefaultCriteria says the criteria are the defaults, not ones set.
	DefaultCriteria bool              `json:"default_criteria"`
	Criteria        []CriterionResult `json:"criteria"`
	// Progress is the mean of each criterion's way to its target, as a whole
	// percentage; 0 with no criteria. Met: every criterion reached its
	// target (and there is at least one), or the release is released.
	Progress int  `json:"progress"`
	Met      bool `json:"met"`
	Late     bool `json:"late"`
	// DateMode is how the date is set: "fixed", the date typed (FixedDate,
	// which a milestone dated by the plan keeps for when it has nothing
	// scheduled to wait for), or "plan", the day the last task it waits
	// for finishes. WaitsFor are those tasks' ids. Conflict: its date is
	// fixed and one of them finishes after it.
	DateMode  string   `json:"date_mode"`
	FixedDate *string  `json:"fixed_date"`
	WaitsFor  []string `json:"waits_for"`
	Conflict  bool     `json:"conflict"`
	// Tasks are the ids of the tasks that count toward the milestone, in
	// outline order: those assigned to it, or under a task that is, that
	// have no sub-tasks.
	Tasks []string `json:"tasks"`
}

// How a milestone's date is set, and the most tasks one waits for.
const (
	DateFixed        = "fixed"
	DateByPlan       = "plan"
	MaxMilestoneWait = 50
)

// waitsFor checks the tasks a milestone is to wait for: each a task of the
// project, with no sub-tasks (it is a sub-task that is waited for), once.
func (s *DefaultService) waitsFor(projectID string, ids []string) ([]string, error) {
	items, err := s.board.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	onBoard := map[string]bool{}
	for _, it := range items {
		onBoard[it.ID] = true
	}
	schedules, err := s.repo.ListSchedules(projectID)
	if err != nil {
		return nil, err
	}
	parent := map[string]bool{}
	for _, sc := range schedules {
		if sc.ParentID != nil {
			parent[*sc.ParentID] = true
		}
	}
	out, seen := []string{}, map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !onBoard[id] || parent[id] {
			return nil, ErrMilestoneLink
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > MaxMilestoneWait {
		return nil, ErrMilestoneLink
	}
	return out, nil
}

// linkMilestone applies the date mode and the tasks waited for of a request.
func (s *DefaultService) linkMilestone(m *Milestone, req MilestoneRequest) error {
	if req.DateMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*req.DateMode))
		if mode != DateFixed && mode != DateByPlan {
			return ErrMilestoneLink
		}
		m.DateMode = mode
	}
	if req.WaitsFor != nil {
		ids, err := s.waitsFor(m.ProjectID, *req.WaitsFor)
		if err != nil {
			return err
		}
		m.WaitsFor = ids
	}
	return nil
}

// CreateMilestone adds a custom milestone.
func (s *DefaultService) CreateMilestone(projectID string, req MilestoneRequest) (*Milestone, error) {
	now := s.now()
	m := &Milestone{ID: uuid.New().String(), ProjectID: projectID, Criteria: []Criterion{}, CreatedAt: now, UpdatedAt: now, DateMode: DateFixed, WaitsFor: []string{}}
	if req.Name == nil || strings.TrimSpace(*req.Name) == "" {
		return nil, ErrNameRequired
	}
	if err := applyMilestone(m, req); err != nil {
		return nil, err
	}
	if err := s.linkMilestone(m, req); err != nil {
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
	if m.ReleaseID != nil && (req.Name != nil || req.TargetDate != nil || req.Description != nil || req.DateMode != nil) {
		return nil, ErrReleaseMilestone
	}
	if err := applyMilestone(m, req); err != nil {
		return nil, err
	}
	if err := s.linkMilestone(m, req); err != nil {
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

// SetReleaseWaitsFor sets the tasks a release's milestone waits for,
// storing the milestone, with the default criteria, the first time.
func (s *DefaultService) SetReleaseWaitsFor(projectID, releaseID string, taskIDs []string) (*Milestone, error) {
	ids, err := s.waitsFor(projectID, taskIDs)
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
			m.WaitsFor, m.UpdatedAt = ids, now
			return m, s.repo.UpdateMilestone(m)
		}
	}
	m := &Milestone{ID: uuid.New().String(), ProjectID: projectID, ReleaseID: &releaseID, Criteria: DefaultReleaseCriteria(), DateMode: DateFixed, WaitsFor: ids, CreatedAt: now, UpdatedAt: now}
	return m, s.repo.CreateMilestone(m)
}

// awaited is what the tasks a milestone waits for say about its date: the
// ids of those the plan still has, and the day the last of them finishes
// (nil when none is scheduled).
func awaited(ids []string, byID map[string]*Task) ([]string, *string) {
	kept := []string{}
	var last *string
	for _, id := range ids {
		if t := byID[id]; t != nil {
			kept = append(kept, id)
			last = maxDate(last, t.SpanEnd)
		}
	}
	return kept, last
}

// dated gives a milestone the tasks it waits for and its date: by the plan,
// the day the last of them finishes (its own date until one is scheduled);
// fixed, its own, in conflict when one of them finishes after it.
func (mp *MilestoneProgress) dated(stored *Milestone, byID map[string]*Task) {
	mp.DateMode, mp.WaitsFor, mp.FixedDate = DateFixed, []string{}, mp.Date
	if stored == nil {
		return
	}
	var last *string
	mp.WaitsFor, last = awaited(stored.WaitsFor, byID)
	if stored.DateMode == DateByPlan && stored.ReleaseID == nil {
		mp.DateMode = DateByPlan
		if last != nil {
			mp.Date = last
		}
		return
	}
	mp.Conflict = last != nil && mp.Date != nil && *last > *mp.Date
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
	byID := map[string]*Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}

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
		mp.dated(stored[id], byID)
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
		mp.dated(m, byID)
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
