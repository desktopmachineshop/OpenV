package plan

import (
	"bytes"
	"fmt"
	"time"

	"github.com/phpdave11/gofpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// The PDF is the chart on landscape A4: the outline on the left, the
// timeline on the right with a bar per task (its progress filled in), a
// diamond per milestone and a line at today, the rows repeating their header
// on each page; then the milestones with their criteria as a table.

const (
	pdfW, pdfH   = 297.0, 210.0
	pdfMargin    = 12.0
	pdfRowH      = 6.0
	pdfHeadH     = 9.0
	pdfOutlineW  = 96.0
	pdfDatesW    = 34.0
	pdfTitleSize = 13.0
	pdfTextSize  = 7.5
)

// The app's colours (frontend/src/theme.css, the light theme).
var (
	pdfText    = [3]int{44, 62, 80}
	pdfMuted   = [3]int{107, 119, 120}
	pdfBorder  = [3]int{220, 221, 225}
	pdfSoft    = [3]int{238, 238, 238}
	pdfAccent  = [3]int{52, 152, 219}
	pdfTrack   = [3]int{207, 229, 245}
	pdfSuccess = [3]int{39, 174, 96}
	pdfDanger  = [3]int{231, 76, 60}
	pdfPurple  = [3]int{142, 68, 173}
	pdfWarning = [3]int{243, 156, 18}
)

type pdfChart struct {
	pdf        *gofpdf.Fpdf
	doc        Document
	start, end time.Time
	dated      bool
	x0, x1     float64 // the timeline's left and right edges
	// rows is where each scheduled task's bar was drawn on the page being
	// filled, for the lines between related tasks.
	rows map[string]pdfBar
	// placed is where every scheduled task's bar goes, on whichever page,
	// so a line can say where a task on another page is.
	placed map[string]pdfBar
}

func rgb(set func(r, g, b int), c [3]int) { set(c[0], c[1], c[2]) }

func renderPDF(doc Document) ([]byte, error) {
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	pdf.SetAutoPageBreak(false, pdfMargin)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Go", "B", gobold.TTF)
	pdf.SetCellMargin(0)
	pdf.SetTitle(doc.ProjectName+" plan", true)
	pdf.SetCreator("OpenV", true)
	pdf.SetCreationDate(doc.ExportedAt.UTC())
	pdf.SetModificationDate(doc.ExportedAt.UTC())
	pdf.SetCatalogSort(true)

	c := &pdfChart{pdf: pdf, doc: doc, x0: pdfMargin + pdfOutlineW + pdfDatesW, x1: pdfW - pdfMargin}
	if doc.Plan.Start != nil && doc.Plan.End != nil {
		s, err1 := time.Parse(DateLayout, *doc.Plan.Start)
		e, err2 := time.Parse(DateLayout, *doc.Plan.End)
		if err1 == nil && err2 == nil {
			// A margin of a few days either side, so a bar or a diamond at
			// the edge is not cut.
			pad := int(e.Sub(s).Hours()/24)/30 + 2
			c.start, c.end, c.dated = s.AddDate(0, 0, -pad), e.AddDate(0, 0, pad+1), true
		}
	}

	c.placed = c.layout()
	y := c.newPage()
	for _, t := range doc.Plan.Tasks {
		if y+pdfRowH > pdfH-pdfMargin-4 {
			c.links()
			c.footer()
			y = c.newPage()
		}
		c.taskRow(t, y)
		y += pdfRowH
	}
	if len(doc.Plan.Tasks) == 0 {
		pdf.SetFont("Go", "", pdfTextSize+1)
		rgb(pdf.SetTextColor, pdfMuted)
		pdf.Text(pdfMargin, y+5, "No tasks on the board yet.")
	}
	c.links()
	c.footer()
	c.milestoneTable()
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("failed to draw the plan: %w", err)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("failed to write the plan: %w", err)
	}
	return buf.Bytes(), nil
}

// x is where a day falls on the timeline.
func (c *pdfChart) x(day string) (float64, bool) {
	d, err := time.Parse(DateLayout, day)
	if err != nil || !c.dated {
		return 0, false
	}
	span := c.end.Sub(c.start).Hours()
	return c.x0 + (c.x1-c.x0)*d.Sub(c.start).Hours()/span, true
}

// fit cuts text to a width, with an ellipsis.
func (c *pdfChart) fit(s string, w float64) string {
	if c.pdf.GetStringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 && c.pdf.GetStringWidth(string(r)+"…") > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// newPage starts a chart page: the title, the header row with the months,
// the milestones, and answers the y of the first task row.
func (c *pdfChart) newPage() float64 {
	pdf := c.pdf
	pdf.AddPage()
	rgb(pdf.SetTextColor, pdfText)
	pdf.SetFont("Go", "B", pdfTitleSize)
	pdf.Text(pdfMargin, pdfMargin+4, c.fit(c.doc.ProjectName+" · Plan", 180))
	pdf.SetFont("Go", "", pdfTextSize)
	rgb(pdf.SetTextColor, pdfMuted)
	s := c.doc.Plan.Summary
	line := fmt.Sprintf("%d tasks · %d done · %d late · %d critical (amber outline) · %d%% complete · as of %s",
		s.Tasks, s.Done, s.Late, s.Critical, s.Progress, c.doc.Plan.Today)
	pdf.Text(pdfW-pdfMargin-pdf.GetStringWidth(line), pdfMargin+4, line)

	top := pdfMargin + 8
	rgb(pdf.SetFillColor, [3]int{248, 249, 250})
	rgb(pdf.SetDrawColor, pdfBorder)
	pdf.SetLineWidth(0.2)
	pdf.Rect(pdfMargin, top, pdfW-2*pdfMargin, pdfHeadH, "FD")
	pdf.SetFont("Go", "B", pdfTextSize-0.5)
	pdf.Text(pdfMargin+2, top+5.8, "TASK")
	pdf.Text(pdfMargin+pdfOutlineW+1, top+5.8, "START – END")

	if c.dated {
		// A tick a month, or a year on a plan longer than two years; every
		// second or third label when they would collide.
		step, layout := 1, "Jan"
		first := time.Date(c.start.Year(), c.start.Month(), 1, 0, 0, 0, 0, time.UTC)
		if c.end.Sub(c.start).Hours() > 24*365*2 {
			step, layout = 12, "2006"
			first = time.Date(c.start.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		}
		ticks := 0
		for m := first; m.Before(c.end); m = m.AddDate(0, step, 0) {
			ticks++
		}
		every := ticks/14 + 1
		i := 0
		for m := first; m.Before(c.end); m = m.AddDate(0, step, 0) {
			x, _ := c.x(m.Format(DateLayout))
			if x < c.x0 {
				continue
			}
			rgb(pdf.SetDrawColor, pdfSoft)
			pdf.Line(x, top, x, pdfH-pdfMargin-4)
			if i%every == 0 && x < c.x1-10 {
				label := m.Format(layout)
				if step == 1 && (m.Month() == time.January || i == 0) {
					label = m.Format("Jan 2006")
				}
				pdf.Text(x+1, top+5.8, label)
			}
			i++
		}
		if x, ok := c.x(c.doc.Plan.Today); ok && x >= c.x0 && x <= c.x1 {
			rgb(pdf.SetDrawColor, pdfDanger)
			pdf.SetLineWidth(0.3)
			pdf.Line(x, top+pdfHeadH, x, pdfH-pdfMargin-4)
			pdf.SetLineWidth(0.2)
		}
	}

	y := top + pdfHeadH
	c.milestoneRow(y)
	return y + pdfRowH
}

// milestoneRow draws the chart's row of milestones at y: a diamond each.
func (c *pdfChart) milestoneRow(y float64) {
	pdf := c.pdf
	pdf.SetFont("Go", "B", pdfTextSize)
	rgb(pdf.SetTextColor, pdfText)
	pdf.Text(pdfMargin+2, y+4.2, "Milestones")
	pdf.SetFont("Go", "", pdfTextSize-1)
	// Each label runs from its diamond to the next one, cut to fit; where
	// two diamonds sit too close for a name, the table on the last pages
	// names them.
	type mark struct {
		m MilestoneProgress
		x float64
	}
	marks := []mark{}
	for _, m := range c.doc.Plan.Milestones {
		if m.Date == nil {
			continue
		}
		if x, ok := c.x(*m.Date); ok {
			marks = append(marks, mark{m, x})
		}
	}
	const r = 1.9
	for i, mk := range marks {
		m, x := mk.m, mk.x
		color := pdfAccent
		if m.Kind == KindRelease {
			color = pdfPurple
		}
		if m.Met {
			color = pdfSuccess
		} else if m.Late {
			color = pdfDanger
		}
		rgb(pdf.SetFillColor, color)
		cy := y + pdfRowH/2
		pdf.Polygon([]gofpdf.PointType{{X: x, Y: cy - r}, {X: x + r, Y: cy}, {X: x, Y: cy + r}, {X: x - r, Y: cy}}, "F")
		room := c.x1 - x - r - 1
		if i+1 < len(marks) {
			room = marks[i+1].x - x - 2*r - 2
		}
		if room < 9 {
			continue
		}
		rgb(pdf.SetTextColor, pdfMuted)
		pdf.Text(x+r+1, cy+1, c.fit(fmt.Sprintf("%s %d%%", m.Name, m.Progress), room))
	}
	rgb(pdf.SetDrawColor, pdfBorder)
	pdf.Line(pdfMargin, y+pdfRowH, pdfW-pdfMargin, y+pdfRowH)
}

func (c *pdfChart) footer() {
	pdf := c.pdf
	pdf.SetFont("Go", "", pdfTextSize-1)
	rgb(pdf.SetTextColor, pdfMuted)
	pdf.Text(pdfMargin, pdfH-pdfMargin+2, "OpenV · "+c.doc.ExportedAt.UTC().Format(DateLayout))
	page := fmt.Sprintf("Page %d", pdf.PageNo())
	pdf.Text(pdfW-pdfMargin-pdf.GetStringWidth(page), pdfH-pdfMargin+2, page)
	if c.dated && len(c.doc.Plan.Tasks) > 0 {
		c.legend(pdfMargin+42, pdfH-pdfMargin+2)
	}
}

func (c *pdfChart) taskRow(t *Task, y float64) {
	pdf := c.pdf
	rgb(pdf.SetDrawColor, pdfSoft)
	pdf.Line(pdfMargin, y+pdfRowH, pdfW-pdfMargin, y+pdfRowH)
	style := ""
	if t.SubtasksTotal > 0 {
		style = "B"
	}
	pdf.SetFont("Go", style, pdfTextSize)
	rgb(pdf.SetTextColor, pdfText)
	indent := pdfMargin + 2 + float64(t.Depth)*3
	pdf.Text(indent, y+4.2, c.fit(t.WBS+"  "+t.Title, pdfMargin+pdfOutlineW-indent-10))
	pdf.SetFont("Go", "", pdfTextSize-0.5)
	rgb(pdf.SetTextColor, pdfMuted)
	pct := fmt.Sprintf("%d%%", t.Progress)
	pdf.Text(pdfMargin+pdfOutlineW-2-pdf.GetStringWidth(pct), y+4.2, pct)
	if t.SpanStart != nil {
		dates := *t.SpanStart
		if *t.SpanEnd != *t.SpanStart {
			dates += " – " + (*t.SpanEnd)[5:]
		}
		pdf.Text(pdfMargin+pdfOutlineW+1, y+4.2, dates)
	} else {
		pdf.Text(pdfMargin+pdfOutlineW+1, y+4.2, "not scheduled")
	}
	if t.SpanStart == nil || !c.dated {
		return
	}
	a, b := c.span(t)
	top, h := y+1.4, pdfRowH-2.8
	if t.SubtasksTotal > 0 {
		top, h = y+2.2, pdfRowH-4.4
	}
	fill := pdfAccent
	if t.Done {
		fill = pdfSuccess
	} else if t.Late {
		fill = pdfDanger
	}
	rgb(pdf.SetFillColor, pdfTrack)
	pdf.RoundedRect(a, top, b-a, h, 0.8, "1234", "F")
	if t.Progress > 0 {
		rgb(pdf.SetFillColor, fill)
		pdf.RoundedRect(a, top, (b-a)*float64(t.Progress)/100, h, 0.8, "1234", "F")
	}
	// A critical task is outlined in amber, heavier than the rest.
	if t.Critical {
		rgb(pdf.SetDrawColor, pdfWarning)
		pdf.SetLineWidth(0.5)
	} else {
		rgb(pdf.SetDrawColor, fill)
	}
	pdf.RoundedRect(a, top, b-a, h, 0.8, "1234", "D")
	pdf.SetLineWidth(0.2)
	if c.rows == nil {
		c.rows = map[string]pdfBar{}
	}
	c.rows[t.ID] = pdfBar{left: a, right: b, y: y + pdfRowH/2, critical: t.Critical, page: pdf.PageNo(), wbs: t.WBS}
}

// milestoneTable writes the milestones and their criteria on pages of
// their own.
func (c *pdfChart) milestoneTable() {
	ms := c.doc.Plan.Milestones
	if len(ms) == 0 {
		return
	}
	pdf := c.pdf
	cols := []float64{pdfMargin, pdfMargin + 70, pdfMargin + 95, pdfMargin + 120, pdfMargin + 205, pdfMargin + 228, pdfMargin + 251}
	heads := []string{"MILESTONE", "DATE", "PROGRESS", "CRITERION", "TARGET", "ACTUAL", "MET"}
	var y float64
	page := func() {
		pdf.AddPage()
		rgb(pdf.SetTextColor, pdfText)
		pdf.SetFont("Go", "B", pdfTitleSize)
		pdf.Text(pdfMargin, pdfMargin+4, c.fit(c.doc.ProjectName+" · Milestones", 240))
		y = pdfMargin + 8
		rgb(pdf.SetFillColor, [3]int{248, 249, 250})
		rgb(pdf.SetDrawColor, pdfBorder)
		pdf.Rect(pdfMargin, y, pdfW-2*pdfMargin, pdfHeadH, "FD")
		pdf.SetFont("Go", "B", pdfTextSize-0.5)
		rgb(pdf.SetTextColor, pdfMuted)
		for i, h := range heads {
			pdf.Text(cols[i]+2, y+5.8, h)
		}
		y += pdfHeadH
	}
	page()
	row := func(cells []string, bold bool) {
		if y+pdfRowH > pdfH-pdfMargin-4 {
			c.footer()
			page()
		}
		for i, v := range cells {
			style := ""
			if bold && i == 0 {
				style = "B"
			}
			pdf.SetFont("Go", style, pdfTextSize)
			rgb(pdf.SetTextColor, pdfText)
			w := pdfW - pdfMargin - cols[i] - 4
			if i+1 < len(cols) {
				w = cols[i+1] - cols[i] - 4
			}
			pdf.Text(cols[i]+2, y+4.2, c.fit(v, w))
		}
		rgb(pdf.SetDrawColor, pdfSoft)
		pdf.Line(pdfMargin, y+pdfRowH, pdfW-pdfMargin, y+pdfRowH)
		y += pdfRowH
	}
	yes := func(b bool) string {
		if b {
			return "Yes"
		}
		return "No"
	}
	for _, m := range ms {
		name := m.Name
		if m.Kind == KindRelease {
			name += " (release)"
		}
		date := str(m.Date)
		if date == "" {
			date = "no date"
		}
		head := []string{name, date, fmt.Sprintf("%d%%", m.Progress)}
		if len(m.Criteria) == 0 {
			row(append(head, "No criteria set", "", "", yes(m.Met)), true)
			continue
		}
		for i, cr := range m.Criteria {
			cells := []string{"", "", ""}
			if i == 0 {
				cells = head
			}
			row(append(append([]string{}, cells...), cr.Label, fmt.Sprintf("%d%%", cr.Target),
				fmt.Sprintf("%d%% (%d of %d)", cr.Actual, cr.Count, cr.Total), yes(cr.Met)), i == 0)
		}
	}
	c.footer()
}

// span is the left and right edges of a scheduled task's bar.
func (c *pdfChart) span(t *Task) (float64, float64) {
	a, _ := c.x(*t.SpanStart)
	end, _ := time.Parse(DateLayout, *t.SpanEnd)
	b, _ := c.x(end.AddDate(0, 0, 1).Format(DateLayout))
	if b-a < 1.2 {
		b = a + 1.2
	}
	return a, b
}

// pdfFirstRow is the y of a chart page's first task row.
const pdfFirstRow = pdfMargin + 8 + pdfHeadH + pdfRowH

// pdfLastRow is where a chart page's rows end.
const pdfLastRow = pdfH - pdfMargin - 4

// layout works out, before anything is drawn, the page and place of every
// scheduled task's bar, as renderPDF fills the pages.
func (c *pdfChart) layout() map[string]pdfBar {
	placed := map[string]pdfBar{}
	page, y := 1, float64(pdfFirstRow)
	for _, t := range c.doc.Plan.Tasks {
		if y+pdfRowH > pdfLastRow {
			page, y = page+1, pdfFirstRow
		}
		if t.SpanStart != nil && c.dated {
			a, b := c.span(t)
			placed[t.ID] = pdfBar{left: a, right: b, y: y + pdfRowH/2, critical: t.Critical, page: page, wbs: t.WBS}
		}
		y += pdfRowH
	}
	return placed
}

// legend explains the chart's marks, on one line starting at x.
func (c *pdfChart) legend(x, y float64) {
	pdf := c.pdf
	pdf.SetFont("Go", "", pdfTextSize-1)
	label := func(s string) {
		rgb(pdf.SetTextColor, pdfMuted)
		pdf.Text(x, y, s)
		x += pdf.GetStringWidth(s) + 5
	}
	bar := func(fill [3]int, outline *[3]int, name string) {
		rgb(pdf.SetFillColor, fill)
		pdf.RoundedRect(x, y-2.2, 5, 2.4, 0.6, "1234", "F")
		if outline != nil {
			rgb(pdf.SetDrawColor, *outline)
			pdf.SetLineWidth(0.5)
			pdf.RoundedRect(x, y-2.2, 5, 2.4, 0.6, "1234", "D")
			pdf.SetLineWidth(0.2)
		}
		x += 6.2
		label(name)
	}
	line := func(color [3]int, dashed bool, name string) {
		rgb(pdf.SetDrawColor, color)
		rgb(pdf.SetFillColor, color)
		if dashed {
			pdf.SetDashPattern([]float64{0.8, 0.6}, 0)
		}
		pdf.Line(x, y-1, x+5, y-1)
		pdf.SetDashPattern([]float64{}, 0)
		pdf.Polygon([]gofpdf.PointType{{X: x + 5, Y: y - 1}, {X: x + 3.9, Y: y - 1.6}, {X: x + 3.9, Y: y - 0.4}}, "F")
		x += 6.2
		label(name)
	}
	bar(pdfAccent, nil, "In progress")
	bar(pdfSuccess, nil, "Done")
	bar(pdfDanger, nil, "Late")
	bar(pdfTrack, &pdfWarning, "Critical")
	line(pdfMuted, false, "Drives")
	line(pdfDanger, true, "Broken")
	rgb(pdf.SetDrawColor, pdfDanger)
	pdf.SetLineWidth(0.3)
	pdf.Line(x+0.5, y-2.4, x+0.5, y+0.2)
	pdf.SetLineWidth(0.2)
	x += 2.2
	label("Today")
	label("→ 2.1 p.2: continues to task 2.1 on page 2")
}
