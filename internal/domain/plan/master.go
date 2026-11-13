// The master plan: every project's plan in a workspace, each cut down to
// what a line of a portfolio chart shows.

package plan

// MasterTask is a task of a project on the master plan, at the level the
// reader asked for.
type MasterTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	WBS   string `json:"wbs"`
	// Depth is how many tasks sit above this one: 0 for a top-level task.
	Depth    int     `json:"depth"`
	Start    *string `json:"start"`
	End      *string `json:"end"`
	Progress int     `json:"progress"`
	Done     bool    `json:"done"`
	Late     bool    `json:"late"`
	// Critical: the task, or one beneath it, is on its project's critical path.
	Critical bool `json:"critical"`
}

// MasterMilestone is a dated milestone of a project on the master plan.
type MasterMilestone struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Progress int    `json:"progress"`
	Met      bool   `json:"met"`
	Late     bool   `json:"late"`
}

// MasterLink is a relationship between tasks of two projects, from the
// driving project to the driven one.
type MasterLink struct {
	FromProjectID string `json:"from_project_id"`
	FromTitle     string `json:"from_title"`
	ToProjectID   string `json:"to_project_id"`
	ToTitle       string `json:"to_title"`
	Type          string `json:"type"`
	Conflict      bool   `json:"conflict"`
}

// MasterProject is one project's line of the master plan.
type MasterProject struct {
	ProjectID  string            `json:"project_id"`
	Name       string            `json:"name"`
	Start      *string           `json:"start"`
	End        *string           `json:"end"`
	Summary    Summary           `json:"summary"`
	Tasks      []MasterTask      `json:"tasks"`
	Milestones []MasterMilestone `json:"milestones"`
}

// MasterPlan is the plans of the projects a reader can see.
type MasterPlan struct {
	Today    string          `json:"today"`
	Start    *string         `json:"start"`
	End      *string         `json:"end"`
	Projects []MasterProject `json:"projects"`
	Links    []MasterLink    `json:"links"`
}

// MasterAllLevels asks Add for every level of each plan.
const MasterAllLevels = 0

// Add puts a project's plan on the master plan: its span, its figures,
// its tasks down to a level, its dated milestones, and the links its tasks
// have from tasks of other projects. Level 1 is the top-level tasks; each
// level further replaces a task that has sub-tasks with those sub-tasks,
// so a line never draws a task and its sub-tasks over each other.
// MasterAllLevels goes all the way down.
func (m *MasterPlan) Add(name string, p *Plan, levels int) {
	parent := map[string]bool{}
	for _, t := range p.Tasks {
		if t.ParentID != nil {
			parent[*t.ParentID] = true
		}
	}
	line := MasterProject{ProjectID: p.ProjectID, Name: name, Start: p.Start, End: p.End, Summary: p.Summary, Tasks: []MasterTask{}, Milestones: []MasterMilestone{}}
	for _, t := range p.Tasks {
		if shown := !parent[t.ID] || t.Depth == levels-1; shown && (levels == MasterAllLevels || t.Depth < levels) {
			line.Tasks = append(line.Tasks, MasterTask{ID: t.ID, Title: t.Title, WBS: t.WBS, Depth: t.Depth, Start: t.SpanStart, End: t.SpanEnd, Progress: t.Progress,
				Done: t.Done, Late: t.Late, Critical: t.Critical || t.CriticalSubtasks})
		}
		for _, l := range t.ExternalLinks {
			if l.Direction == ExternalDrivenBy {
				m.Links = append(m.Links, MasterLink{FromProjectID: l.ProjectID, FromTitle: l.Title, ToProjectID: p.ProjectID, ToTitle: t.Title, Type: l.Type, Conflict: l.Conflict})
			}
		}
	}
	for _, ms := range p.Milestones {
		if ms.Date != nil {
			line.Milestones = append(line.Milestones, MasterMilestone{Key: ms.Key, Name: ms.Name, Date: *ms.Date, Progress: ms.Progress, Met: ms.Met, Late: ms.Late})
		}
	}
	m.Start, m.End = minDate(m.Start, p.Start), maxDate(m.End, p.End)
	m.Today = p.Today
	m.Projects = append(m.Projects, line)
}
