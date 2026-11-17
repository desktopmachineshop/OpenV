// The plan as Microsoft Project reads and writes it: the MSPDI XML format.
// Export writes the tasks with their outline, dates, durations, progress,
// constraints, deadlines and relationships, the milestones as zero-length
// tasks, and the people with their assignments. Import reads the tasks of
// such a file onto the board and the plan.

package plan

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// FormatMSProject is the export format Microsoft Project opens.
const FormatMSProject = "msproject"

// MaxImportTasks bounds the tasks one import adds.
const MaxImportTasks = 2000

const mspNamespace = "http://schemas.microsoft.com/project"

type mspProject struct {
	XMLName           xml.Name        `xml:"Project"`
	Xmlns             string          `xml:"xmlns,attr,omitempty"`
	Name              string          `xml:"Name"`
	Title             string          `xml:"Title"`
	ScheduleFromStart int             `xml:"ScheduleFromStart"`
	StartDate         string          `xml:"StartDate,omitempty"`
	FinishDate        string          `xml:"FinishDate,omitempty"`
	CurrencyCode      string          `xml:"CurrencyCode,omitempty"`
	MinutesPerDay     int             `xml:"MinutesPerDay"`
	MinutesPerWeek    int             `xml:"MinutesPerWeek"`
	DaysPerMonth      int             `xml:"DaysPerMonth"`
	StatusDate        string          `xml:"StatusDate,omitempty"`
	Tasks             []mspTask       `xml:"Tasks>Task"`
	Resources         []mspResource   `xml:"Resources>Resource"`
	Assignments       []mspAssignment `xml:"Assignments>Assignment"`
}

type mspTask struct {
	UID             int       `xml:"UID"`
	ID              int       `xml:"ID"`
	Name            string    `xml:"Name"`
	Manual          int       `xml:"Manual"`
	OutlineNumber   string    `xml:"OutlineNumber"`
	OutlineLevel    int       `xml:"OutlineLevel"`
	Start           string    `xml:"Start,omitempty"`
	Finish          string    `xml:"Finish,omitempty"`
	Duration        string    `xml:"Duration,omitempty"`
	DurationFormat  int       `xml:"DurationFormat"`
	Work            string    `xml:"Work,omitempty"`
	Milestone       int       `xml:"Milestone"`
	Summary         int       `xml:"Summary"`
	Critical        int       `xml:"Critical"`
	PercentComplete int       `xml:"PercentComplete"`
	FixedCost       float64   `xml:"FixedCost,omitempty"`
	ActualStart     string    `xml:"ActualStart,omitempty"`
	ActualFinish    string    `xml:"ActualFinish,omitempty"`
	ConstraintType  int       `xml:"ConstraintType"`
	ConstraintDate  string    `xml:"ConstraintDate,omitempty"`
	Deadline        string    `xml:"Deadline,omitempty"`
	Notes           string    `xml:"Notes,omitempty"`
	Links           []mspLink `xml:"PredecessorLink"`
}

type mspLink struct {
	PredecessorUID int `xml:"PredecessorUID"`
	Type           int `xml:"Type"`
	LinkLag        int `xml:"LinkLag"`
	LagFormat      int `xml:"LagFormat"`
}

type mspResource struct {
	UID          int     `xml:"UID"`
	ID           int     `xml:"ID"`
	Name         string  `xml:"Name"`
	Type         int     `xml:"Type"`
	MaxUnits     float64 `xml:"MaxUnits"`
	StandardRate float64 `xml:"StandardRate"`
}

type mspAssignment struct {
	UID         int     `xml:"UID"`
	TaskUID     int     `xml:"TaskUID"`
	ResourceUID int     `xml:"ResourceUID"`
	Units       float64 `xml:"Units"`
}

// Microsoft Project's numbers for a relationship's type and a constraint.
var (
	mspLinkTypes   = map[string]int{RelFinishToFinish: 0, RelFinishToStart: 1, RelStartToFinish: 2, RelStartToStart: 3}
	mspConstraints = map[string]int{ConstraintASAP: 0, ConstraintALAP: 1, ConstraintMSO: 2, ConstraintMFO: 3, ConstraintSNET: 4, ConstraintSNLT: 5, ConstraintFNET: 6, ConstraintFNLT: 7}
)

// The lag formats of the three units an offset has here: days, elapsed
// days and percent. A lag is in tenths of a minute, of an eight-hour day
// or a whole one; a percentage is itself.
const (
	mspLagDays    = 7
	mspLagElapsed = 8
	mspLagPercent = 19
)

func mspStart(day *string) string {
	if day == nil {
		return ""
	}
	return *day + "T08:00:00"
}

func mspFinish(day *string) string {
	if day == nil {
		return ""
	}
	return *day + "T17:00:00"
}

func mspHours(days float64, elapsed bool) string {
	hours := days * 8
	if elapsed {
		hours = days * 24
	}
	return fmt.Sprintf("PT%dH%dM0S", int(hours), int(hours*60)%60)
}

// renderMSProject writes the plan as MSPDI XML.
func renderMSProject(doc Document) ([]byte, error) {
	p := doc.Plan
	out := mspProject{Xmlns: mspNamespace, Name: doc.ProjectName, Title: doc.ProjectName, ScheduleFromStart: 1,
		StartDate: mspStart(p.Start), FinishDate: mspFinish(p.End), CurrencyCode: p.Currency,
		MinutesPerDay: 480, MinutesPerWeek: 480 * len(p.WorkingDays), DaysPerMonth: 20, Tasks: []mspTask{}}
	if p.ScheduleFrom == FromFinish {
		out.ScheduleFromStart = 0
	}
	if p.StatusDate != nil {
		out.StatusDate = mspFinish(p.StatusDate)
	}
	uid := map[string]int{}
	for i, t := range p.Tasks {
		uid[t.ID] = i + 1
	}
	for i, t := range p.Tasks {
		task := mspTask{UID: i + 1, ID: i + 1, Name: t.Title, OutlineNumber: t.WBS, OutlineLevel: t.Depth + 1,
			Start: mspStart(t.SpanStart), Finish: mspFinish(t.SpanEnd), DurationFormat: 7, PercentComplete: t.Progress,
			FixedCost: t.FixedCost, ActualStart: mspStart(t.ActualStart), ActualFinish: mspFinish(t.ActualFinish),
			ConstraintType: mspConstraints[t.ConstraintType], ConstraintDate: mspStart(t.ConstraintDate), Deadline: mspFinish(t.Deadline)}
		if t.Mode == ModeManual {
			task.Manual = 1
		}
		if t.SubtasksTotal > 0 {
			task.Summary = 1
		}
		if t.Critical {
			task.Critical = 1
		}
		if t.DurationDays != nil {
			task.Duration = mspHours(float64(*t.DurationDays), t.Elapsed)
			if t.Elapsed {
				task.DurationFormat = 8
			}
		}
		if t.EffortDays != nil {
			task.Work = mspHours(*t.EffortDays, false)
		}
		for _, r := range t.Relationships {
			link := mspLink{PredecessorUID: uid[r.TaskID], Type: mspLinkTypes[r.Type], LagFormat: mspLagDays, LinkLag: r.LagDays * 4800}
			switch r.LagUnit {
			case LagElapsedDays:
				link.LagFormat, link.LinkLag = mspLagElapsed, r.LagDays*14400
			case LagPercent:
				link.LagFormat, link.LinkLag = mspLagPercent, r.LagDays
			}
			task.Links = append(task.Links, link)
		}
		out.Tasks = append(out.Tasks, task)
	}
	for _, m := range p.Milestones {
		if m.Date == nil {
			continue
		}
		n := len(out.Tasks) + 1
		out.Tasks = append(out.Tasks, mspTask{UID: n, ID: n, Name: m.Name, Manual: 1, OutlineNumber: strconv.Itoa(n), OutlineLevel: 1,
			Start: mspStart(m.Date), Finish: mspStart(m.Date), Duration: "PT0H0M0S", DurationFormat: 7, Milestone: 1, PercentComplete: m.Progress,
			ConstraintType: mspConstraints[ConstraintMSO], ConstraintDate: mspStart(m.Date)})
	}
	who := map[string]int{}
	for i, r := range p.Resources {
		who[r.Type+":"+r.ID] = i + 1
		name := r.Name
		if name == "" {
			name = r.ID
		}
		out.Resources = append(out.Resources, mspResource{UID: i + 1, ID: i + 1, Name: name, Type: 1, MaxUnits: float64(r.Capacity) / 100, StandardRate: r.DailyRate / 8})
	}
	for _, t := range p.Tasks {
		for _, a := range t.Assignments {
			if res := who[a.Type+":"+a.ID]; res > 0 && t.SubtasksTotal == 0 {
				out.Assignments = append(out.Assignments, mspAssignment{UID: len(out.Assignments) + 1, TaskUID: uid[t.ID], ResourceUID: res, Units: float64(a.Units) / 100})
			}
		}
	}
	body, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

// ImportResult counts what an import added.
type ImportResult struct {
	Tasks         int `json:"tasks"`
	Milestones    int `json:"milestones"`
	Relationships int `json:"relationships"`
}

// mspDay is the day of an MSPDI date and time, or nil.
func mspDay(s string) *string {
	if len(s) < 10 {
		return nil
	}
	day := s[:10]
	if _, err := time.Parse(DateLayout, day); err != nil {
		return nil
	}
	return &day
}

// mspLag turns a link's lag into an offset and its unit.
func mspLag(l mspLink) (int, string) {
	switch l.LagFormat {
	case mspLagPercent, 20:
		return l.LinkLag, LagPercent
	case mspLagElapsed, 4, 6, 10, 12:
		return l.LinkLag / 14400, LagElapsedDays
	}
	return l.LinkLag / 4800, LagWorkingDays
}

// ImportMSProject adds the tasks of an MSPDI file to the project: each as
// a card on the board with its dates, its place in the outline, its
// progress, constraint and deadline, then the relationships between them.
// A task of no length that is marked a milestone becomes a milestone. The
// file's project summary row, its resources and its calendars are left.
func (s *DefaultService) ImportMSProject(projectID string, data []byte, createdBy *string, actor string) (ImportResult, error) {
	res := ImportResult{}
	var in mspProject
	if err := xml.Unmarshal(data, &in); err != nil {
		return res, fmt.Errorf("%w: %v", ErrImport, err)
	}
	if len(in.Tasks) == 0 {
		return res, fmt.Errorf("%w: it has no tasks", ErrImport)
	}
	if len(in.Tasks) > MaxImportTasks {
		return res, fmt.Errorf("%w: more than %d tasks", ErrImport, MaxImportTasks)
	}
	ids := map[int]string{}
	order, placed := map[string]int{}, map[string]int{}
	var path []string // the task at each outline level above the one being read
	for i, t := range in.Tasks {
		name := strings.TrimSpace(t.Name)
		if t.OutlineLevel < 1 || name == "" {
			continue
		}
		start, finish := mspDay(t.Start), mspDay(t.Finish)
		if t.Milestone == 1 && t.Summary == 0 {
			req := MilestoneRequest{Name: &name, TargetDate: finish}
			if _, err := s.CreateMilestone(projectID, req); err != nil {
				return res, err
			}
			res.Milestones++
			continue
		}
		column := workitems.ColumnTodo
		switch {
		case t.PercentComplete >= 100:
			column = workitems.ColumnDone
		case t.PercentComplete > 0:
			column = workitems.ColumnInProgress
		}
		item, err := s.board.Create(workitems.CreateWorkItemRequest{ProjectID: projectID, Title: name, Description: strings.TrimSpace(t.Notes), Column: column}, createdBy, actor)
		if err != nil {
			return res, err
		}
		ids[t.UID] = item.ID
		res.Tasks++
		if len(path) >= t.OutlineLevel {
			path = path[:t.OutlineLevel-1]
		}
		req := ScheduleRequest{}
		if len(path) > 0 {
			req.ParentID = &path[len(path)-1]
		}
		// The file's order is the outline's: number each task among its siblings.
		parent := ""
		if len(path) > 0 {
			parent = path[len(path)-1]
		}
		placed[parent]++
		order[item.ID] = placed[parent]
		path = append(path, item.ID)
		// A task with sub-tasks takes its dates from them.
		summary := i+1 < len(in.Tasks) && in.Tasks[i+1].OutlineLevel > t.OutlineLevel
		if !summary {
			req.StartDate, req.DueDate, req.Deadline = start, finish, mspDay(t.Deadline)
			if t.PercentComplete > 0 && t.PercentComplete < 100 {
				req.PercentComplete = &in.Tasks[i].PercentComplete
			}
			for kind, n := range mspConstraints {
				if n == t.ConstraintType && n > 1 && mspDay(t.ConstraintDate) != nil {
					kind := kind
					req.ConstraintType, req.ConstraintDate = &kind, mspDay(t.ConstraintDate)
				}
			}
		}
		if err := s.schedule(item.ID, req, actor); err != nil {
			return res, fmt.Errorf("%s: %w", name, err)
		}
	}
	res.Relationships = s.importLinks(in.Tasks, ids, actor)
	if err := s.repo.SetOrder(projectID, order); err != nil {
		return res, err
	}
	return res, s.autoSchedule(projectID, actor)
}

// importLinks gives the imported tasks their relationships, and answers
// how many it made.
func (s *DefaultService) importLinks(tasks []mspTask, ids map[int]string, actor string) int {
	n := 0
	for _, t := range tasks {
		id := ids[t.UID]
		rels := []Relationship{}
		for _, l := range t.Links {
			on := ids[l.PredecessorUID]
			if id == "" || on == "" {
				continue
			}
			rel := Relationship{TaskID: on, Type: RelFinishToStart}
			for kind, n := range mspLinkTypes {
				if n == l.Type {
					rel.Type = kind
				}
			}
			rel.LagDays, rel.LagUnit = mspLag(l)
			rels = append(rels, rel)
		}
		if len(rels) == 0 {
			continue
		}
		// A relationship the plan cannot hold, such as one that closes a
		// loop, is left out rather than failing the import.
		if err := s.schedule(id, ScheduleRequest{Relationships: &rels}, actor); err == nil {
			n += len(rels)
		}
	}
	return n
}
