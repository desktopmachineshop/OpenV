// The lines between related tasks on the PDF chart (export_pdf.go).

package plan

import (
	"github.com/phpdave11/gofpdf"
)

// pdfBar is where a task's bar sits on the page: its edges and its middle.
type pdfBar struct {
	left, right, y float64
	critical       bool
}

// links draws, over the page just filled, a line with an arrowhead for each
// relationship whose two tasks are both on it, as the chart in the app
// does: finish to start from the driving task's end to the driven task's
// start, start to start between the two starts, finish to finish between
// the two ends, start to finish from the driver's start to the driven
// task's end. A broken one is red and dashed; one between two critical
// tasks is amber. It then forgets the page's bars.
func (c *pdfChart) links() {
	pdf := c.pdf
	for _, d := range c.doc.Plan.Dependencies {
		on, ok1 := c.rows[d.DependsOnID]
		t, ok2 := c.rows[d.WorkItemID]
		if !ok1 || !ok2 {
			continue
		}
		color, width := pdfMuted, 0.2
		if on.critical && t.critical {
			color, width = pdfWarning, 0.35
		}
		broken := false
		for _, task := range c.doc.Plan.Tasks {
			if task.ID != d.WorkItemID {
				continue
			}
			for _, r := range task.Relationships {
				broken = broken || (r.TaskID == d.DependsOnID && r.Conflict)
			}
		}
		if broken {
			color = pdfDanger
			pdf.SetDashPattern([]float64{0.8, 0.6}, 0)
		}
		rgb(pdf.SetDrawColor, color)
		rgb(pdf.SetFillColor, color)
		pdf.SetLineWidth(width)
		pts, dir := linkPoints(d.Type, on, t)
		for i := 1; i < len(pts); i++ {
			pdf.Line(pts[i-1].X, pts[i-1].Y, pts[i].X, pts[i].Y)
		}
		pdf.SetDashPattern([]float64{}, 0)
		tip := pts[len(pts)-1]
		pdf.Polygon([]gofpdf.PointType{{X: tip.X, Y: tip.Y}, {X: tip.X - dir*1.1, Y: tip.Y - 0.6}, {X: tip.X - dir*1.1, Y: tip.Y + 0.6}}, "F")
	}
	pdf.SetLineWidth(0.2)
	c.rows = nil
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
