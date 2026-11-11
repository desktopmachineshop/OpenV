// Relationships between tasks of two projects. The driven task's plan
// reads the driving task's dates as they stand and never moves it; each
// plan shows the other project's task by name on the task it is linked to,
// not as a row of its own.

package plan

import "strings"

// Which way an external link runs, seen from the task that carries it.
const (
	ExternalDrivenBy = "driven_by"
	ExternalDrives   = "drives"
)

// ExternalLink is a relationship to a task of another project, as one of
// the two tasks sees it.
type ExternalLink struct {
	// LocalID is the task of this project the link is on.
	LocalID string `json:"-"`
	// TaskID, ProjectID, ProjectName and Title name the other task.
	TaskID      string `json:"task_id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Title       string `json:"title"`
	// Start and End are the other task's dates; a task with only a due
	// date starts on it.
	Start *string `json:"start"`
	End   *string `json:"end"`
	Done  bool    `json:"done"`
	Type  string  `json:"type"`
	// LagDays and LagUnit are the offset, as on a relationship.
	LagDays int    `json:"lag_days"`
	LagUnit string `json:"lag_unit"`
	// Direction is ExternalDrivenBy when the other task drives this one.
	Direction string `json:"direction"`
	// Conflict: the driven task's dates break the relationship.
	Conflict bool `json:"conflict"`
}

// external gives each task its links to other projects' tasks, and marks
// the ones the dates break.
func (b *builder) external() {
	for _, l := range b.in.External {
		t := b.byID[l.LocalID]
		if t == nil {
			continue
		}
		if !ValidRelationshipType(l.Type) {
			l.Type = RelFinishToStart
		}
		l.LagUnit = lagUnit(l.LagUnit)
		if l.Start == nil {
			l.Start = l.End
		}
		if l.Start != nil && l.End != nil && t.SpanStart != nil {
			other := &Task{SpanStart: l.Start, SpanEnd: l.End}
			if l.Direction == ExternalDrives {
				l.Conflict = !t.Done && b.gap(t, drive{other, l.Type, l.LagDays, l.LagUnit}) < 0
			} else {
				l.Conflict = !l.Done && b.gap(other, drive{t, l.Type, l.LagDays, l.LagUnit}) < 0
			}
		}
		t.ExternalLinks = append(t.ExternalLinks, l)
	}
}

// RelateExternal makes a task driven by a task of another project. The
// pair has one such link, either way round.
func (s *DefaultService) RelateExternal(taskID, otherID, kind string, lagDays int, unit, actor string) error {
	a, err := s.board.Get(taskID)
	if err != nil || a == nil {
		return ErrTaskNotFound
	}
	b, err := s.board.Get(otherID)
	if err != nil || b == nil {
		return ErrTaskNotFound
	}
	if a.ProjectID == b.ProjectID {
		return ErrSameProject
	}
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if kind == "" {
		kind = RelFinishToStart
	}
	if !ValidRelationshipType(kind) {
		return ErrRelationshipType
	}
	if lagDays > MaxLagDays || lagDays < -MaxLagDays || !ValidLagUnit(unit) {
		return ErrInvalidLag
	}
	if err := s.repo.SaveExternalLink(a.ID, b.ID, kind, lagDays, lagUnit(unit)); err != nil {
		return err
	}
	return s.autoSchedule(a.ProjectID, actor)
}

// UnrelateExternal removes the link between two tasks of different
// projects, whichever way round it runs.
func (s *DefaultService) UnrelateExternal(taskID, otherID string) error {
	return s.repo.DeleteExternalLink(taskID, otherID)
}
