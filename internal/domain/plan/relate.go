package plan

import (
	"strings"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

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

// Relate makes a relationship between two tasks from either end, with an
// offset of lagDays working days. The pair has one relationship, so whatever it had, either way round, is replaced.
func (s *DefaultService) Relate(taskID, otherID, kind string, drives bool, lagDays int, lagUnit string) error {
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
	if err := s.notParents(a.ProjectID, a.ID, b.ID); err != nil {
		return err
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
	want := []Relationship{{TaskID: driver, Type: kind, LagDays: lagDays, LagUnit: lagUnit}}
	for _, d := range without(rest, driven, driver) {
		want = append(want, Relationship{TaskID: d.DependsOnID, Type: d.Type, LagDays: d.LagDays, LagUnit: d.LagUnit})
	}
	next, err := checkDependencies(driven, want, rest, map[string]bool{a.ID: true, b.ID: true, driver: true}, true)
	if err != nil {
		return err
	}
	// The old relationship ran the other way: take it off the old driven task.
	if err := s.repo.SetDependencies(a.ProjectID, driver, without(rest, driver, driven)); err != nil {
		return err
	}
	if err := s.repo.SetDependencies(a.ProjectID, driven, next); err != nil {
		return err
	}
	return s.autoSchedule(a.ProjectID, "")
}

// notParents refuses a relationship to or from a task with sub-tasks. Such
// a task has no work of its own: its dates are its sub-tasks', so it is one
// of them that another task waits for, and linking the parent only hides
// which.
func (s *DefaultService) notParents(projectID string, ids ...string) error {
	schedules, err := s.repo.ListSchedules(projectID)
	if err != nil {
		return err
	}
	for _, sc := range schedules {
		for _, id := range ids {
			if sc.ParentID != nil && *sc.ParentID == id {
				return ErrParentRelationship
			}
		}
	}
	return nil
}

// noNewParentLinks refuses the relationships of a task that are not among
// those it had and that have a task with sub-tasks at either end. A
// relationship an older plan already carries is left alone.
func noNewParentLinks(want, existing []Dependency, parents map[string]string) error {
	isParent := map[string]bool{}
	for _, p := range parents {
		isParent[p] = true
	}
	had := map[string]bool{}
	for _, d := range existing {
		had[d.WorkItemID+">"+d.DependsOnID] = true
	}
	for _, d := range want {
		if !had[d.WorkItemID+">"+d.DependsOnID] && (isParent[d.WorkItemID] || isParent[d.DependsOnID]) {
			return ErrParentRelationship
		}
	}
	return nil
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
	if err := s.repo.SetDependencies(a.ProjectID, b.ID, without(deps, b.ID, a.ID)); err != nil {
		return err
	}
	return s.autoSchedule(a.ProjectID, "")
}

// MaxLagDays bounds a relationship's offset, either way.
const MaxLagDays = 3650

// The units of a relationship's offset.
const (
	LagWorkingDays = "d"
	LagElapsedDays = "ed"
	LagPercent     = "%"
)

// ValidLagUnit reports whether u names an offset's unit; the empty unit is
// working days.
func ValidLagUnit(u string) bool {
	return u == "" || u == LagWorkingDays || u == LagElapsedDays || u == LagPercent
}

func lagUnit(u string) string {
	if u == "" {
		return LagWorkingDays
	}
	return u
}

// wantedRelationships reads the relationships a request asks for: its
// Relationships, or its DependsOn with each task keeping the type it has now.
func wantedRelationships(id string, req ScheduleRequest, existing []Dependency) []Relationship {
	if req.Relationships != nil {
		return *req.Relationships
	}
	has := map[string]Dependency{}
	for _, d := range existing {
		if d.WorkItemID == id {
			has[d.DependsOnID] = d
		}
	}
	out := make([]Relationship, 0, len(*req.DependsOn))
	for _, dep := range *req.DependsOn {
		dep = strings.TrimSpace(dep)
		out = append(out, Relationship{TaskID: dep, Type: has[dep].Type, LagDays: has[dep].LagDays, LagUnit: has[dep].LagUnit})
	}
	return out
}

// checkDependencies checks the relationships id is to have: each to a task
// on the project's board, none to id itself, each of a known type, and none
// to a task that id already drives, whatever the types along the way. A task
// named twice keeps its first entry.
func checkDependencies(id string, want []Relationship, existing []Dependency, onBoard map[string]bool, known bool) ([]Dependency, error) {
	waitsOn := map[string][]string{}
	for _, d := range existing {
		if d.WorkItemID != id {
			waitsOn[d.WorkItemID] = append(waitsOn[d.WorkItemID], d.DependsOnID)
		}
	}
	out := []Dependency{}
	seen := map[string]bool{}
	for _, rel := range want {
		dep, kind := strings.TrimSpace(rel.TaskID), strings.ToUpper(strings.TrimSpace(rel.Type))
		if dep == "" || seen[dep] {
			continue
		}
		if kind == "" {
			kind = RelFinishToStart
		}
		switch {
		case dep == id:
			return nil, ErrDependsOnSelf
		case !known && !onBoard[dep]:
			return nil, ErrOtherProject
		case !ValidRelationshipType(kind):
			return nil, ErrRelationshipType
		case rel.LagDays > MaxLagDays || rel.LagDays < -MaxLagDays || !ValidLagUnit(rel.LagUnit):
			return nil, ErrInvalidLag
		}
		seen[dep] = true
		out = append(out, Dependency{WorkItemID: id, DependsOnID: dep, Type: kind, LagDays: rel.LagDays, LagUnit: lagUnit(rel.LagUnit)})
	}
	visited := map[string]bool{}
	stack := make([]string, 0, len(out))
	for _, d := range out {
		stack = append(stack, d.DependsOnID)
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == id {
			return nil, ErrDependencyCycle
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		stack = append(stack, waitsOn[cur]...)
	}
	return out, nil
}
