package plan

import (
	"strings"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// SetWorkingDays sets which days of the week the plan works.
func (s *DefaultService) SetWorkingDays(projectID string, days []int) ([]int, error) {
	clean, err := CleanWorkingDays(days)
	if err != nil {
		return nil, err
	}
	return clean, s.repo.SetWorkingDays(projectID, clean)
}

// pair loads two tasks of one project that are not the same task.
func (s *DefaultService) pair(taskID, otherID string) (*workitems.WorkItem, *workitems.WorkItem, error) {
	a, err := s.board.Get(taskID)
	if err != nil || a == nil {
		return nil, nil, ErrTaskNotFound
	}
	if taskID == otherID {
		return nil, nil, ErrDependsOnSelf
	}
	b, err := s.board.Get(otherID)
	if err != nil || b == nil || b.ProjectID != a.ProjectID {
		return nil, nil, ErrOtherProject
	}
	return a, b, nil
}

// without is a task's relationships with the one to other left out.
func without(deps []Dependency, taskID, other string) []Dependency {
	out := []Dependency{}
	for _, d := range deps {
		if d.WorkItemID == taskID && d.DependsOnID != other {
			out = append(out, d)
		}
	}
	return out
}

// Relate makes a relationship between two tasks from either end. The pair
// has one relationship, so whatever it had, either way round, is replaced.
func (s *DefaultService) Relate(taskID, otherID, kind string, drives bool) error {
	a, b, err := s.pair(taskID, otherID)
	if err != nil {
		return err
	}
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if kind == "" {
		kind = RelFinishToStart
	}
	if !ValidRelationshipType(kind) {
		return ErrRelationshipType
	}
	driver, driven := b.ID, a.ID
	if drives {
		driver, driven = a.ID, b.ID
	}
	deps, err := s.repo.ListDependencies(a.ProjectID)
	if err != nil {
		return err
	}
	// The plan without this pair, to look for a loop the new one would close.
	rest := []Dependency{}
	for _, d := range deps {
		between := (d.WorkItemID == a.ID && d.DependsOnID == b.ID) || (d.WorkItemID == b.ID && d.DependsOnID == a.ID)
		if !between {
			rest = append(rest, d)
		}
	}
	want := []Relationship{{TaskID: driver, Type: kind}}
	for _, d := range without(rest, driven, driver) {
		want = append(want, Relationship{TaskID: d.DependsOnID, Type: d.Type})
	}
	next, err := checkDependencies(driven, want, rest, map[string]bool{a.ID: true, b.ID: true, driver: true}, true)
	if err != nil {
		return err
	}
	// The old relationship ran the other way: take it off the old driven task.
	if err := s.repo.SetDependencies(a.ProjectID, driver, without(rest, driver, driven)); err != nil {
		return err
	}
	return s.repo.SetDependencies(a.ProjectID, driven, next)
}

// Unrelate removes the relationship between two tasks, whichever way round.
func (s *DefaultService) Unrelate(taskID, otherID string) error {
	a, b, err := s.pair(taskID, otherID)
	if err != nil {
		return err
	}
	deps, err := s.repo.ListDependencies(a.ProjectID)
	if err != nil {
		return err
	}
	if err := s.repo.SetDependencies(a.ProjectID, a.ID, without(deps, a.ID, b.ID)); err != nil {
		return err
	}
	return s.repo.SetDependencies(a.ProjectID, b.ID, without(deps, b.ID, a.ID))
}
