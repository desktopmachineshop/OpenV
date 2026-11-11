// The order of a task among its siblings in the outline.

package plan

import "time"

// reorder puts a task straight after one of its siblings (after), first
// among them (an empty after) or last (nil), and numbers every sibling so
// the outline keeps that order. A place of 0 is none.
func (s *DefaultService) reorder(projectID, workItemID string, after *string) error {
	in, _, _, err := s.inputs(projectID)
	if err != nil {
		return err
	}
	p := Build(in, time.Now())
	var moved *Task
	for _, t := range p.Tasks {
		if t.ID == workItemID {
			moved = t
		}
	}
	if moved == nil {
		return ErrTaskNotFound
	}
	same := func(t *Task) bool {
		if moved.ParentID == nil || t.ParentID == nil {
			return moved.ParentID == nil && t.ParentID == nil
		}
		return *moved.ParentID == *t.ParentID
	}
	list := []string{}
	at := -1
	for _, t := range p.Tasks {
		if t.ID == workItemID || !same(t) {
			continue
		}
		list = append(list, t.ID)
		if after != nil && t.ID == *after {
			at = len(list)
		}
	}
	switch {
	case after == nil:
		// Siblings nobody has ordered stay in date order, the task among
		// them: it only loses the place it had under its old parent.
		ordered := false
		for _, sc := range in.Schedules {
			for _, id := range list {
				ordered = ordered || (sc.WorkItemID == id && sc.SortOrder != nil)
			}
		}
		if !ordered {
			return s.repo.SetOrder(projectID, map[string]int{workItemID: 0})
		}
		at = len(list)
	case *after == "":
		at = 0
	case at < 0:
		return ErrPosition
	}
	order := map[string]int{}
	list = append(list[:at], append([]string{workItemID}, list[at:]...)...)
	for i, id := range list {
		order[id] = i + 1
	}
	return s.repo.SetOrder(projectID, order)
}
