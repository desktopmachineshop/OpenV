// The lines between related tasks on the PDF chart (export_pdf.go).

package plan

import (
	"fmt"

	"github.com/phpdave11/gofpdf"
)

// pdfBar is where a task's bar sits on the page: its edges and its middle.
type pdfBar struct {
	left, right, y float64
	critical       bool
	// page is the chart page the bar is on, and wbs the task's number.
	page int
	wbs  string
}

// links draws, over the page just filled, a line with an arrowhead for each
// relationship whose two tasks are both on it, as the chart in the app
// does: finish to start from the driving task's end to the driven task's
// start, start to start between the two starts, finish to finish between
// the two ends, start to finish from the driver's start to the driven
// task's end. A broken one is red and dashed; one between two critical
// tasks is amber. A relationship to a task on another page runs to the
// page's edge and names the task and its page. It then forgets the page's
// bars.
func (c *pdfChart) links() {
	pdf := c.pdf
	for _, d := range c.doc.Plan.Dependencies {
		on, ok1 := c.rows[d.DependsOnID]
		t, ok2 := c.rows[d.WorkItemID]
		if !ok1 && !ok2 {
			continue
		}
		if !ok1 {
			on, ok1 = c.placed[d.DependsOnID]
		}
		if !ok2 {
			t, ok2 = c.placed[d.WorkItemID]
		}
		if !ok1 || !ok2 {
			continue
		}
		c.linkStyle(d, on, t)
		var pts []gofpdf.PointType
		dir, tipped := 1.0, true
		switch page := pdf.PageNo(); {
		case on.page == page && t.page == page:
			pts, dir = linkPoints(d.Type, on, t)
		case on.page == page:
			pts, tipped = c.linkOut(d.Type, on, t), false
		default:
			pts, dir = c.linkIn(d.Type, on, t)
		}
		for i := 1; i < len(pts); i++ {
			pdf.Line(pts[i-1].X, pts[i-1].Y, pts[i].X, pts[i].Y)
		}
		pdf.SetDashPattern([]float64{}, 0)
		if tipped {
			tip := pts[len(pts)-1]
			pdf.Polygon([]gofpdf.PointType{{X: tip.X, Y: tip.Y}, {X: tip.X - dir*1.1, Y: tip.Y - 0.6}, {X: tip.X - dir*1.1, Y: tip.Y + 0.6}}, "F")
		}
	}
	pdf.SetLineWidth(0.2)
	c.rows = nil
}

// linkStyle sets the pen for a relationship's line: grey, amber between two
// critical tasks, red and dashed when the dates break it.
func (c *pdfChart) linkStyle(d Dependency, on, t pdfBar) {
	pdf := c.pdf
	color, width := pdfMuted, 0.2
	if on.critical && t.critical {
		color, width = pdfWarning, 0.35
	}
	for _, task := range c.doc.Plan.Tasks {
		if task.ID != d.WorkItemID {
			continue
		}
		for _, r := range task.Relationships {
			if r.TaskID == d.DependsOnID && r.Conflict {
				color = pdfDanger
				pdf.SetDashPattern([]float64{0.8, 0.6}, 0)
			}
		}
	}
	rgb(pdf.SetDrawColor, color)
	rgb(pdf.SetFillColor, color)
	pdf.SetLineWidth(width)
}

// edge is where a line to a task on another page leaves this one: the
// bottom of the rows for a later page, the top for an earlier one. It
// writes the other task's number and page beside it.
func (c *pdfChart) edge(x float64, other pdfBar, arrow string) float64 {
	pdf := c.pdf
	y, text := float64(pdfLastRow), pdfLastRow-0.6
	if other.page < pdf.PageNo() {
		y, text = pdfFirstRow, pdfFirstRow+1.9
	}
	pdf.SetFont("Go", "", pdfTextSize-2)
	rgb(pdf.SetTextColor, pdfMuted)
	pdf.Text(x+0.6, text, fmt.Sprintf("%s %s p.%d", arrow, other.wbs, other.page))
	return y
}

// linkOut is the line from a driving task to the edge of its page, toward a
// driven task on another.
func (c *pdfChart) linkOut(kind string, on, t pdfBar) []gofpdf.PointType {
	x, out := on.right, on.right+1.2
	if kind == RelStartToStart || kind == RelStartToFinish {
		x, out = on.left, on.left-1.5
	}
	y := c.edge(out, t, "→")
	return []gofpdf.PointType{{X: x, Y: on.y}, {X: out, Y: on.y}, {X: out, Y: y}}
}

// linkIn is the line from the edge of the page to a driven task whose
// driving task is on another, and the way its arrowhead points.
func (c *pdfChart) linkIn(kind string, on, t pdfBar) ([]gofpdf.PointType, float64) {
	x, in, dir := t.left, t.left-1.5, 1.0
	if kind == RelFinishToFinish || kind == RelStartToFinish {
		x, in, dir = t.right, t.right+1.5, -1.0
	}
	y := c.edge(in, on, "←")
	return []gofpdf.PointType{{X: in, Y: y}, {X: in, Y: t.y}, {X: x, Y: t.y}}, dir
}

// linkPoints is the path of a relationship's line from the driving bar to
// the driven one, and the way its arrowhead points: 1 to the right, -1 to
// the left.
func linkPoints(kind string, on, t pdfBar) ([]gofpdf.PointType, float64) {
	pt := func(x, y float64) gofpdf.PointType { return gofpdf.PointType{X: x, Y: y} }
	switch kind {
	case RelStartToStart:
		gutter := on.left
		if t.left < gutter {
			gutter = t.left
		}
		gutter -= 1.5
		return []gofpdf.PointType{pt(on.left, on.y), pt(gutter, on.y), pt(gutter, t.y), pt(t.left, t.y)}, 1
	case RelStartToFinish:
		// Out of the driver's start, round to the driven task's end.
		gutter := on.left
		if t.right+3 < gutter {
			return []gofpdf.PointType{pt(on.left, on.y), pt(t.right+1.5, on.y), pt(t.right+1.5, t.y), pt(t.right, t.y)}, -1
		}
		gutter -= 1.5
		mid := on.y + pdfRowH/2
		if t.y < on.y {
			mid = on.y - pdfRowH/2
		}
		return []gofpdf.PointType{pt(on.left, on.y), pt(gutter, on.y), pt(gutter, mid), pt(t.right+1.5, mid), pt(t.right+1.5, t.y), pt(t.right, t.y)}, -1
	case RelFinishToFinish:
		gutter := on.right
		if t.right > gutter {
			gutter = t.right
		}
		gutter += 1.5
		return []gofpdf.PointType{pt(on.right, on.y), pt(gutter, on.y), pt(gutter, t.y), pt(t.right, t.y)}, -1
	}
	if t.left >= on.right+2.5 {
		return []gofpdf.PointType{pt(on.right, on.y), pt(on.right+1.2, on.y), pt(on.right+1.2, t.y), pt(t.left, t.y)}, 1
	}
	// The driven task starts before the driver ends: go round between the rows.
	mid := on.y + pdfRowH/2
	if t.y < on.y {
		mid = on.y - pdfRowH/2
	}
	return []gofpdf.PointType{pt(on.right, on.y), pt(on.right+1.2, on.y), pt(on.right+1.2, mid), pt(t.left-1.5, mid), pt(t.left-1.5, t.y), pt(t.left, t.y)}, 1
}
