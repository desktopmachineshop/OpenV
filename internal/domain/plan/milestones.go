// The plan's stored milestones and their criteria (plan.go has the types).

package plan

import (
	"github.com/google/uuid"
	"strings"
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
