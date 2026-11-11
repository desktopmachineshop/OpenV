package plan

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// The formats a plan exports as: the ones a project download offers that
// suit a schedule. ReqIF exchanges requirements and Word is a document of
// them; neither has a place for dates and bars, so a plan leaves them out.
const (
	FormatJSON  = "json"
	FormatCSV   = "csv"
	FormatExcel = "excel"
	FormatPDF   = "pdf"
)

// Formats lists the export formats in the order a chooser shows them.
var Formats = []string{FormatPDF, FormatExcel, FormatCSV, FormatJSON}

// Document is a plan ready to export: the plan, the project's name and the
// display name of each assignee by id.
type Document struct {
	ProjectName string            `json:"project_name"`
	ExportedAt  time.Time         `json:"exported_at"`
	Plan        *Plan             `json:"plan"`
	Assignees   map[string]string `json:"assignees"`
}

// Rendered is an exported plan.
type Rendered struct {
	Data        []byte
	Filename    string
	ContentType string
}

// Render exports the plan in a format of Formats.
func Render(doc Document, format string) (*Rendered, error) {
	if doc.Plan == nil {
		doc.Plan = &Plan{}
	}
	var (
		body []byte
		err  error
		ext  string
		mime string
	)
	switch format {
	case FormatJSON:
		ext, mime = "json", "application/json"
		body, err = json.MarshalIndent(doc, "", "  ")
	case FormatCSV:
		ext, mime = "csv", "text/csv; charset=utf-8"
		body, err = renderCSV(doc)
	case FormatExcel:
		ext, mime = "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		body, err = renderExcel(doc)
	case FormatPDF:
		ext, mime = "pdf", "application/pdf"
		body, err = renderPDF(doc)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	if err != nil {
		return nil, err
	}
	return &Rendered{Data: body, Filename: filename(doc, ext), ContentType: mime}, nil
}

// filename names the download; the project's name is stripped of what a
// quoted Content-Disposition filename cannot hold.
func filename(doc Document, ext string) string {
	var b strings.Builder
	for _, r := range doc.ProjectName {
		switch {
		case r < 0x20 || r == 0x7f, r == '"' || r == '\\' || r == '/':
		default:
			b.WriteRune(r)
		}
	}
	name := strings.TrimSpace(b.String())
	if name == "" {
		name = "project"
	}
	return fmt.Sprintf("plan-%s-%s.%s", name, doc.ExportedAt.UTC().Format(DateLayout), ext)
}

func optInt(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// optNumber writes a number of days without trailing zeros: 2.5, 3.
func optNumber(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// columnLabel is a board column as the board writes it.
func columnLabel(column string) string {
	switch column {
	case "backlog":
		return "Backlog"
	case "todo":
		return "To Do"
	case "in-progress":
		return "In Progress"
	case "review":
		return "Review"
	case "done":
		return "Done"
	}
	return column
}

func (d Document) assignee(t *Task) string {
	if t.AssigneeID == nil {
		return ""
	}
	if n := d.Assignees[*t.AssigneeID]; n != "" {
		return n
	}
	return *t.AssigneeID
}

var taskHeader = []string{"wbs", "task", "status", "assignee", "start", "end", "duration_days", "effort_days", "float_days", "critical", "progress_percent", "todos_done", "todos_total", "subtasks_done", "subtasks_total", "relationships", "late"}

// taskRows is the task table every tabular format writes: one row per task
// in outline order, the title indented by its depth, what it depends on by
// outline number.
func taskRows(doc Document, indent bool) [][]string {
	wbs := map[string]string{}
	for _, t := range doc.Plan.Tasks {
		wbs[t.ID] = t.WBS
	}
	rows := [][]string{}
	for _, t := range doc.Plan.Tasks {
		// The outline number with the type after it, as a Gantt writes a
		// predecessor: "1.1FS, 2SS".
		deps := make([]string, 0, len(t.Relationships))
		for _, r := range t.Relationships {
			deps = append(deps, wbs[r.TaskID]+r.Type)
		}
		title := t.Title
		if indent {
			title = strings.Repeat("  ", t.Depth) + title
		}
		rows = append(rows, []string{
			t.WBS, title, columnLabel(t.Column), doc.assignee(t), str(t.SpanStart), str(t.SpanEnd),
			optInt(t.DurationDays), optNumber(t.EffortDays), optInt(t.FloatDays), strconv.FormatBool(t.Critical),
			strconv.Itoa(t.Progress), strconv.Itoa(t.TodosDone), strconv.Itoa(t.TodosTotal),
			strconv.Itoa(t.SubtasksDone), strconv.Itoa(t.SubtasksTotal), strings.Join(deps, ", "),
			strconv.FormatBool(t.Late),
		})
	}
	return rows
}

var milestoneHeader = []string{"milestone", "kind", "date", "progress_percent", "met", "criterion", "target_percent", "actual_percent", "count", "total", "criterion_met"}

// milestoneRows writes one row per criterion, and one for a milestone that
// has none.
func milestoneRows(doc Document) [][]string {
	rows := [][]string{}
	for _, m := range doc.Plan.Milestones {
		head := []string{m.Name, m.Kind, str(m.Date), strconv.Itoa(m.Progress), strconv.FormatBool(m.Met)}
		if len(m.Criteria) == 0 {
			rows = append(rows, append(head, "", "", "", "", "", ""))
			continue
		}
		for _, c := range m.Criteria {
			rows = append(rows, append(append([]string{}, head...), c.Label, strconv.Itoa(c.Target), strconv.Itoa(c.Actual),
				strconv.Itoa(c.Count), strconv.Itoa(c.Total), strconv.FormatBool(c.Met)))
		}
	}
	return rows
}

// csvCell keeps a spreadsheet from reading a cell as a formula.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// renderCSV writes the task table, a blank line, then the milestones.
func renderCSV(doc Document) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	write := func(rows [][]string) error {
		for _, row := range rows {
			safe := make([]string, len(row))
			for i, v := range row {
				safe[i] = csvCell(v)
			}
			if err := w.Write(safe); err != nil {
				return err
			}
		}
		return nil
	}
	if err := write(append([][]string{taskHeader}, taskRows(doc, false)...)); err != nil {
		return nil, err
	}
	if len(doc.Plan.Milestones) > 0 {
		w.Flush()
		buf.WriteString("\n")
		if err := write(append([][]string{milestoneHeader}, milestoneRows(doc)...)); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// renderExcel writes a workbook: the tasks with a drawn timeline of weeks
// beside them, then the milestones and their criteria.
func renderExcel(doc Document) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const tasksSheet, milestonesSheet = "Plan", "Milestones"
	if err := f.SetSheetName(f.GetSheetName(0), tasksSheet); err != nil {
		return nil, err
	}
	if _, err := f.NewSheet(milestonesSheet); err != nil {
		return nil, err
	}
	header, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Vertical: "center"}})
	if err != nil {
		return nil, err
	}
	fill := func(color string) (int, error) {
		return f.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{color}}})
	}
	barStyle, err := fill("3498DB")
	if err != nil {
		return nil, err
	}
	doneStyle, err := fill("27AE60")
	if err != nil {
		return nil, err
	}
	lateStyle, err := fill("E74C3C")
	if err != nil {
		return nil, err
	}
	criticalStyle, err := fill("F39C12")
	if err != nil {
		return nil, err
	}

	writeTable := func(sheet string, head []string, rows [][]string) error {
		for i, row := range append([][]string{head}, rows...) {
			cells := make([]interface{}, len(row))
			for j, v := range row {
				if n, err := strconv.Atoi(v); err == nil && i > 0 && head[j] != "wbs" {
					cells[j] = n
				} else {
					cells[j] = csvCell(v)
				}
			}
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			if err := f.SetSheetRow(sheet, cell, &cells); err != nil {
				return err
			}
		}
		last, _ := excelize.CoordinatesToCellName(len(head), 1)
		if err := f.SetCellStyle(sheet, "A1", last, header); err != nil {
			return err
		}
		return f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	}
	if err := writeTable(tasksSheet, taskHeader, taskRows(doc, true)); err != nil {
		return nil, err
	}
	_ = f.SetColWidth(tasksSheet, "B", "B", 48)
	_ = f.SetColWidth(tasksSheet, "C", "F", 14)
	if err := writeTable(milestonesSheet, milestoneHeader, milestoneRows(doc)); err != nil {
		return nil, err
	}
	_ = f.SetColWidth(milestonesSheet, "A", "A", 32)
	_ = f.SetColWidth(milestonesSheet, "F", "F", 34)

	excelTimeline(f, tasksSheet, doc.Plan, header, [4]int{barStyle, doneStyle, lateStyle, criticalStyle})
	f.SetActiveSheet(0)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("failed to write the plan workbook: %w", err)
	}
	return buf.Bytes(), nil
}

// excelTimeline draws the timeline right of the task table: one narrow
// column a week, each task's weeks filled in its state's colour (styles:
// open, done, late, critical).
func excelTimeline(f *excelize.File, sheet string, p *Plan, header int, styles [4]int) {
	if weeks := weeksOf(p); len(weeks) > 0 {
		first := len(taskHeader) + 2
		for i, w := range weeks {
			cell, _ := excelize.CoordinatesToCellName(first+i, 1)
			_ = f.SetCellValue(sheet, cell, w.Format("02 Jan"))
		}
		from, _ := excelize.ColumnNumberToName(first)
		to, _ := excelize.ColumnNumberToName(first + len(weeks) - 1)
		_ = f.SetColWidth(sheet, from, to, 7)
		_ = f.SetCellStyle(sheet, from+"1", to+"1", header)
		for row, t := range p.Tasks {
			if t.SpanStart == nil || t.SpanEnd == nil {
				continue
			}
			a, b := weekIndex(weeks, *t.SpanStart), weekIndex(weeks, *t.SpanEnd)
			style := styles[0]
			if t.Critical {
				style = styles[3]
			}
			if t.Done {
				style = styles[1]
			} else if t.Late {
				style = styles[2]
			}
			c1, _ := excelize.CoordinatesToCellName(first+a, row+2)
			c2, _ := excelize.CoordinatesToCellName(first+b, row+2)
			_ = f.SetCellStyle(sheet, c1, c2, style)
		}
	}
}

// maxWeeks caps the drawn timeline of a workbook: five years.
const maxWeeks = 260

// weeksOf is the Monday of each week the plan spans.
func weeksOf(p *Plan) []time.Time {
	if p.Start == nil || p.End == nil {
		return nil
	}
	start, err1 := time.Parse(DateLayout, *p.Start)
	end, err2 := time.Parse(DateLayout, *p.End)
	if err1 != nil || err2 != nil {
		return nil
	}
	start = start.AddDate(0, 0, -((int(start.Weekday()) + 6) % 7))
	weeks := []time.Time{}
	for d := start; !d.After(end) && len(weeks) < maxWeeks; d = d.AddDate(0, 0, 7) {
		weeks = append(weeks, d)
	}
	return weeks
}

func weekIndex(weeks []time.Time, day string) int {
	d, err := time.Parse(DateLayout, day)
	if err != nil {
		return 0
	}
	i := int(d.Sub(weeks[0]).Hours() / 24 / 7)
	if i < 0 {
		i = 0
	}
	if i >= len(weeks) {
		i = len(weeks) - 1
	}
	return i
}
