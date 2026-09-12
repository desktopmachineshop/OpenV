package reports

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	"github.com/phpdave11/gofpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/reports/doc"
)

// The PDF specification.
//
// Layout rules the renderer keeps to, because the assessment found each one
// broken before:
//
//   - nothing is measured ahead and drawn as one box; every table row is
//     measured and drawn on its own, with a page break between rows and the
//     header row repeated, so a border never crosses another artifact;
//   - a body is rendered from doc blocks with gofpdf's flowing writer, which
//     wraps and breaks pages by itself, so a long description is never dropped;
//   - the fonts are the Go TrueType family embedded as UTF-8, so text is
//     drawn as written rather than translated to Windows-1252;
//   - a figure that cannot be embedded becomes a sentence, never an error.

const (
	pdfMarginL   = 18.0
	pdfMarginR   = 18.0
	pdfMarginT   = 18.0
	pdfMarginB   = 20.0
	pdfBodySize  = 10.0
	pdfBodyLine  = 5.0
	pdfSmallSize = 8.5
	pdfLabelW    = 42.0
	pdfCellPad   = 1.5
	pdfTOCLine   = 6.0
)

// pdfTextBlue is the colour of links.
var pdfTextBlue = [3]int{20, 90, 170}

type pdfRenderer struct {
	m   *reportModel
	pdf *gofpdf.Fpdf
	// linkIDs holds one internal link per artifact, bound where the artifact
	// is drawn.
	linkIDs map[string]int
	// headingPages records the page each outline entry landed on, for the
	// table of contents.
	headingPages map[string]int
	// tocPages is the number of pages the table of contents occupies in this
	// pass (0 on the measuring pass), added to every recorded page number.
	tocPages int
	// tocKnown is the page map from the measuring pass.
	tocKnown map[string]int
}

// tocEntry is one line of the table of contents.
type tocEntry struct {
	key   string
	title string
	level int
}

func buildReportPDF(data *exports.ProjectExport, opts RenderOptions) ([]byte, error) {
	m := buildReportModel(data, opts)
	entries := m.tocEntries()

	// Pass one measures where every heading lands; pass two prints the
	// table of contents with those numbers shifted by its own length. Body
	// pagination is identical in both passes because the contents occupy
	// whole pages of their own.
	first := newPDFRenderer(m)
	out, err := first.render(entries)
	if err != nil {
		return nil, err
	}
	if !opts.Content.TOC || len(entries) == 0 {
		return out, nil
	}
	second := newPDFRenderer(m)
	second.tocKnown = first.headingPages
	second.tocPages = tocPageCount(len(entries))
	return second.render(entries)
}

// tocPageCount is how many pages a contents list of n entries takes: a title
// line, then one line per entry.
func tocPageCount(n int) int {
	usable := 297.0 - pdfMarginT - pdfMarginB - 14
	perPage := int(usable / pdfTOCLine)
	if perPage < 1 {
		perPage = 1
	}
	pages := int(math.Ceil(float64(n) / float64(perPage)))
	if pages < 1 {
		pages = 1
	}
	return pages
}

func newPDFRenderer(m *reportModel) *pdfRenderer {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMarginL, pdfMarginT, pdfMarginR)
	pdf.SetAutoPageBreak(true, pdfMarginB)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Go", "B", gobold.TTF)
	pdf.AddUTF8FontFromBytes("Go", "I", goitalic.TTF)
	pdf.AddUTF8FontFromBytes("Go", "BI", gobolditalic.TTF)
	pdf.AddUTF8FontFromBytes("GoMono", "", gomono.TTF)
	pdf.AddUTF8FontFromBytes("GoMono", "B", gomonobold.TTF)
	pdf.SetCellMargin(0)
	pdf.AliasNbPages("{nb}")

	r := &pdfRenderer{m: m, pdf: pdf, linkIDs: map[string]int{}, headingPages: map[string]int{}}
	return r
}

func (r *pdfRenderer) render(entries []tocEntry) ([]byte, error) {
	m, pdf := r.m, r.pdf
	pdf.SetTitle(m.data.ProjectName, true)
	author := m.opts.Author
	if author == "" {
		author = m.opts.Workspace.Name
	}
	if author != "" {
		pdf.SetAuthor(author, true)
	}
	pdf.SetCreator("OpenV", true)
	pdf.SetSubject(m.opts.Snapshot.Statement(), true)

	footerLeft := m.data.ProjectName + " · " + m.opts.Snapshot.Label()
	pdf.SetFooterFunc(func() {
		pdf.SetY(-13)
		pdf.SetFont("Go", "", 8)
		pdf.SetTextColor(120, 120, 120)
		pdf.SetDrawColor(210, 210, 210)
		pdf.SetLineWidth(0.2)
		pdf.Line(pdfMarginL, pdf.GetY()-1.5, 210-pdfMarginR, pdf.GetY()-1.5)
		pdf.CellFormat(120, 5, footerLeft, "", 0, "L", false, 0, "")
		pdf.CellFormat(0, 5, fmt.Sprintf("Page %d of {nb}", pdf.PageNo()), "", 0, "R", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})

	for _, n := range m.order {
		r.linkIDs[n.artifact.ID] = pdf.AddLink()
	}
	for _, e := range entries {
		if _, ok := r.linkIDs[e.key]; !ok {
			r.linkIDs[e.key] = pdf.AddLink()
		}
	}

	r.cover()
	if r.tocPages > 0 {
		r.contents(entries)
	}

	// Product definition.
	if fields := profileFields(m.data.ProductProfile); len(fields) > 0 {
		pdf.AddPage()
		r.sectionTitle("product-definition", "Product definition", 0)
		for _, f := range fields {
			r.ensureSpace(14)
			pdf.SetFont("Go", "B", 10.5)
			pdf.SetTextColor(60, 60, 60)
			pdf.CellFormat(0, 6, f.Label, "", 1, "L", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
			r.blocks(f.Blocks, pdfMarginL)
			pdf.Ln(1.5)
		}
	}

	if len(m.order) > 0 {
		pdf.AddPage()
		for _, n := range m.order {
			r.artifact(n)
		}
	}

	if m.opts.Content.VVStatus && m.coverage != nil {
		pdf.AddPage()
		r.vvSummary()
	}
	if m.opts.Content.TestResults {
		pdf.AddPage()
		r.testResults()
	}

	if pdf.Err() {
		return nil, pdf.Error()
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- Cover and contents ------------------------------------------------------

func (r *pdfRenderer) cover() {
	m, pdf := r.m, r.pdf
	pdf.AddPage()

	y := pdfMarginT
	if logo, ok := logoFigure(m.opts.Workspace); ok {
		w, h := fitBox(logo.Width, logo.Height, 60, 24)
		if r.placeImage("logo", logo, pdfMarginL, y, w, h) {
			y += h + 4
		}
	}
	if m.opts.Workspace.Name != "" {
		pdf.SetXY(pdfMarginL, y)
		pdf.SetFont("Go", "", 10)
		pdf.SetTextColor(110, 110, 110)
		pdf.CellFormat(0, 5, m.opts.Workspace.Name, "", 1, "L", false, 0, "")
		y = pdf.GetY() + 2
	}
	pdf.SetXY(pdfMarginL, y+6)
	pdf.SetFont("Go", "B", 24)
	pdf.SetTextColor(0, 0, 0)
	pdf.MultiCell(0, 10, m.data.ProjectName, "", "L", false)
	pdf.Ln(1)
	pdf.SetFont("Go", "", 12)
	pdf.SetTextColor(80, 80, 80)
	pdf.CellFormat(0, 6, "Specification", "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)
	if strings.TrimSpace(m.data.ProjectDesc) != "" {
		r.blocks(doc.Parse(m.data.ProjectDesc), pdfMarginL)
		pdf.Ln(3)
	}

	// The source statement, boxed so it is the first thing a reader checks.
	pdf.SetFont("Go", "", 10)
	stmt := m.opts.Snapshot.Statement()
	lines := pdf.SplitText(stmt, r.contentWidth()-6)
	boxH := float64(len(lines))*pdfBodyLine + 12
	r.ensureSpace(boxH)
	top := pdf.GetY()
	pdf.SetFillColor(243, 245, 248)
	pdf.SetDrawColor(200, 205, 212)
	pdf.SetLineWidth(0.3)
	pdf.Rect(pdfMarginL, top, r.contentWidth(), boxH, "FD")
	pdf.SetXY(pdfMarginL+3, top+2.5)
	pdf.SetFont("Go", "B", 9)
	pdf.SetTextColor(60, 60, 60)
	kind := "Live project"
	if m.opts.Snapshot.IsBaseline() {
		kind = "Baseline snapshot"
	}
	pdf.CellFormat(0, 5, kind, "", 1, "L", false, 0, "")
	pdf.SetX(pdfMarginL + 3)
	pdf.SetFont("Go", "", 10)
	pdf.SetTextColor(0, 0, 0)
	pdf.MultiCell(r.contentWidth()-6, pdfBodyLine, stmt, "", "L", false)
	pdf.SetY(top + boxH + 5)

	for _, line := range m.coverLines() {
		r.ensureSpace(8)
		pdf.SetFont("Go", "B", 9)
		pdf.SetTextColor(90, 90, 90)
		yy := pdf.GetY()
		pdf.SetXY(pdfMarginL, yy)
		pdf.CellFormat(30, pdfBodyLine, line.Label, "", 0, "L", false, 0, "")
		pdf.SetFont("Go", "", 9.5)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetXY(pdfMarginL+30, yy)
		pdf.MultiCell(r.contentWidth()-30, pdfBodyLine, line.Value, "", "L", false)
		pdf.Ln(0.8)
	}
}

// tocEntries lists what the contents page names: the product definition,
// every heading up to level 3, and the verification sections.
func (m *reportModel) tocEntries() []tocEntry {
	var out []tocEntry
	if len(profileFields(m.data.ProductProfile)) > 0 {
		out = append(out, tocEntry{key: "product-definition", title: "Product definition", level: 0})
	}
	for _, n := range m.order {
		if n.artifact.Type != artifacts.TypeHeading || n.depth > 2 {
			continue
		}
		out = append(out, tocEntry{key: n.artifact.ID, title: m.title(n.artifact), level: n.depth})
	}
	if m.opts.Content.VVStatus {
		out = append(out, tocEntry{key: "vv-summary", title: "Verification summary", level: 0})
	}
	if m.opts.Content.TestResults {
		out = append(out, tocEntry{key: "test-results", title: "Test results", level: 0})
	}
	return out
}

func (r *pdfRenderer) contents(entries []tocEntry) {
	pdf := r.pdf
	pdf.AddPage()
	pdf.SetFont("Go", "B", 16)
	pdf.CellFormat(0, 10, "Contents", "", 1, "L", false, 0, "")
	pdf.Ln(2)
	width := r.contentWidth()
	for _, e := range entries {
		if pdf.GetY()+pdfTOCLine > 297-pdfMarginB {
			pdf.AddPage()
		}
		page := r.tocKnown[e.key] + r.tocPages
		indent := float64(e.level) * 6
		style := ""
		if e.level == 0 {
			style = "B"
		}
		pdf.SetFont("Go", style, 10)
		pdf.SetTextColor(0, 0, 0)
		num := fmt.Sprintf("%d", page)
		numW := pdf.GetStringWidth(num) + 2
		titleW := width - indent - numW - 4
		title := r.truncate(e.title, titleW)
		y := pdf.GetY()
		pdf.SetXY(pdfMarginL+indent, y)
		pdf.CellFormat(titleW, pdfTOCLine, title, "", 0, "L", false, r.linkIDs[e.key], "")
		// Dotted leader from the end of the title to the number.
		pdf.SetFont("Go", "", 10)
		tw := pdf.GetStringWidth(title)
		pdf.SetDrawColor(170, 170, 170)
		pdf.SetLineWidth(0.2)
		pdf.SetDashPattern([]float64{0.4, 1.2}, 0)
		pdf.Line(pdfMarginL+indent+tw+1.5, y+pdfTOCLine-1.6, pdfMarginL+width-numW-1, y+pdfTOCLine-1.6)
		pdf.SetDashPattern([]float64{}, 0)
		pdf.SetXY(pdfMarginL+width-numW, y)
		pdf.CellFormat(numW, pdfTOCLine, num, "", 1, "R", false, r.linkIDs[e.key], "")
	}
	// Pad to the planned length so the body starts where pass one put it.
	for pdf.PageNo() < 1+r.tocPages+1 {
		pdf.AddPage()
	}
	// The cover is page 1 and the contents start on page 2; the loop above
	// leaves the cursor on the last contents page, so the body's AddPage
	// begins the page after it.
}

// --- Artifacts ---------------------------------------------------------------

func (r *pdfRenderer) artifact(n *artifactNode) {
	m, pdf := r.m, r.pdf
	a := n.artifact
	level := m.headingLevel(n)

	switch {
	case a.Type == artifacts.TypeHeading:
		size := 15.0 - float64(level-1)*1.5
		if size < 11 {
			size = 11
		}
		r.ensureSpace(size + 14)
		pdf.Ln(2)
		r.bindArtifact(a.ID, m.title(a), level-1)
		pdf.SetFont("Go", "B", size)
		pdf.SetTextColor(0, 0, 0)
		pdf.MultiCell(0, size*0.5, m.title(a), "", "L", false)
		pdf.Ln(1)
		r.blocks(m.bodies[a.ID], pdfMarginL)
		r.figures(a.ID)
		pdf.Ln(2)
	case a.Type == artifacts.TypeDescription:
		r.ensureSpace(16)
		r.bindArtifact(a.ID, m.title(a), level-1)
		if a.Ref != "" || a.Title != "" {
			pdf.SetFont("Go", "", 8)
			pdf.SetTextColor(130, 130, 130)
			pdf.CellFormat(0, 4, m.title(a), "", 1, "L", false, 0, "")
			pdf.SetTextColor(0, 0, 0)
		}
		r.blocks(m.bodies[a.ID], pdfMarginL)
		r.figures(a.ID)
		pdf.Ln(2)
	default:
		r.ensureSpace(24)
		pdf.Ln(2)
		r.bindArtifact(a.ID, m.title(a), level-1)
		pdf.SetFont("Go", "B", 11.5)
		pdf.SetTextColor(0, 0, 0)
		pdf.MultiCell(0, 6, m.title(a), "", "L", false)
		pdf.Ln(0.5)
		r.fieldsTable(m.fieldRows(a))
		if len(m.bodies[a.ID]) > 0 {
			pdf.Ln(1.5)
			r.blocks(m.bodies[a.ID], pdfMarginL)
		}
		r.figures(a.ID)
		if rows := m.links[a.ID]; len(rows) > 0 {
			r.traceabilityTable(rows)
		}
		pdf.Ln(3)
	}
}

// bindArtifact anchors an artifact's link target and outline entry at the
// current position and records its page.
func (r *pdfRenderer) bindArtifact(key, title string, outlineLevel int) {
	pdf := r.pdf
	if id, ok := r.linkIDs[key]; ok {
		pdf.SetLink(id, -1, -1)
	}
	pdf.Bookmark(title, outlineLevel, -1)
	r.headingPages[key] = pdf.PageNo()
}

// sectionTitle draws a top-level section that is not an artifact.
func (r *pdfRenderer) sectionTitle(key, title string, level int) {
	pdf := r.pdf
	r.bindArtifact(key, title, level)
	pdf.SetFont("Go", "B", 15)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 9, title, "", 1, "L", false, 0, "")
	pdf.Ln(2)
}

// fieldsTable draws the details rows, one at a time, each measured before
// it is drawn so a row that does not fit moves whole to the next page.
func (r *pdfRenderer) fieldsTable(rows []fieldRow) {
	pdf := r.pdf
	width := r.contentWidth()
	valueW := width - pdfLabelW
	pdf.SetDrawColor(205, 205, 205)
	pdf.SetLineWidth(0.2)
	for _, row := range rows {
		pdf.SetFont("Go", "", 9)
		lines := pdf.SplitText(row.Value, valueW-2*pdfCellPad)
		h := float64(maxInt(len(lines), 1))*4.4 + 2*pdfCellPad
		r.ensureSpace(h)
		y := pdf.GetY()
		pdf.SetFillColor(247, 247, 247)
		pdf.Rect(pdfMarginL, y, pdfLabelW, h, "FD")
		pdf.Rect(pdfMarginL+pdfLabelW, y, valueW, h, "D")
		pdf.SetXY(pdfMarginL+pdfCellPad, y+pdfCellPad)
		pdf.SetFont("Go", "B", 9)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(pdfLabelW-2*pdfCellPad, 4.4, row.Label, "", 0, "L", false, 0, "")
		x := pdfMarginL + pdfLabelW + pdfCellPad
		if row.Rollup != "" {
			cr, cg, cb := rollupColor(row.Rollup)
			pdf.SetFillColor(cr, cg, cb)
			pdf.Rect(x, y+pdfCellPad+0.9, 2.6, 2.6, "F")
			x += 4
		}
		pdf.SetXY(x, y+pdfCellPad)
		pdf.SetFont("Go", "", 9)
		pdf.SetTextColor(0, 0, 0)
		pdf.MultiCell(valueW-2*pdfCellPad-(x-pdfMarginL-pdfLabelW-pdfCellPad), 4.4, row.Value, "", "L", false)
		pdf.SetY(y + h)
	}
	pdf.SetDrawColor(0, 0, 0)
}

// traceabilityTable draws the links table with its header repeated on every
// page it continues onto, each target a link to its artifact.
func (r *pdfRenderer) traceabilityTable(rows []linkRow) {
	pdf := r.pdf
	width := r.contentWidth()
	cols := []float64{24, 46, width - 70}
	pdf.Ln(2)
	r.ensureSpace(14)
	pdf.SetFont("Go", "B", 9.5)
	pdf.SetTextColor(60, 60, 60)
	pdf.CellFormat(0, 5, "Traceability", "", 1, "L", false, 0, "")
	header := func() {
		y := pdf.GetY()
		pdf.SetFont("Go", "B", 8.5)
		pdf.SetFillColor(235, 237, 240)
		pdf.SetDrawColor(205, 205, 205)
		pdf.SetTextColor(60, 60, 60)
		x := pdfMarginL
		for i, title := range []string{"Direction", "Relationship", "Artifact"} {
			pdf.SetXY(x, y)
			pdf.CellFormat(cols[i], 5.5, " "+title, "1", 0, "L", true, 0, "")
			x += cols[i]
		}
		pdf.SetY(y + 5.5)
	}
	header()
	for _, row := range rows {
		pdf.SetFont("Go", "", 9)
		target := row.TargetTitle
		if row.Suspect {
			target += " (suspect)"
		}
		lines := pdf.SplitText(target, cols[2]-2*pdfCellPad)
		h := float64(maxInt(len(lines), 1))*4.4 + 2*pdfCellPad
		if pdf.GetY()+h > 297-pdfMarginB {
			pdf.AddPage()
			header()
		}
		y := pdf.GetY()
		pdf.SetDrawColor(205, 205, 205)
		x := pdfMarginL
		for _, w := range cols {
			pdf.Rect(x, y, w, h, "D")
			x += w
		}
		pdf.SetTextColor(0, 0, 0)
		pdf.SetXY(pdfMarginL+pdfCellPad, y+pdfCellPad)
		pdf.CellFormat(cols[0]-2*pdfCellPad, 4.4, row.Direction, "", 0, "L", false, 0, "")
		pdf.SetXY(pdfMarginL+cols[0]+pdfCellPad, y+pdfCellPad)
		pdf.CellFormat(cols[1]-2*pdfCellPad, 4.4, row.Relationship, "", 0, "L", false, 0, "")
		pdf.SetXY(pdfMarginL+cols[0]+cols[1]+pdfCellPad, y+pdfCellPad)
		link := r.linkIDs[row.TargetID]
		if link > 0 {
			pdf.SetTextColor(pdfTextBlue[0], pdfTextBlue[1], pdfTextBlue[2])
		}
		for i, line := range lines {
			pdf.SetX(pdfMarginL + cols[0] + cols[1] + pdfCellPad)
			pdf.CellFormat(cols[2]-2*pdfCellPad, 4.4, line, "", 1, "L", false, link, "")
			if i == len(lines)-1 {
				break
			}
		}
		pdf.SetTextColor(0, 0, 0)
		pdf.SetY(y + h)
	}
	pdf.SetDrawColor(0, 0, 0)
}

// figures draws an artifact's figures, each captioned, at the width of the
// text column and no taller than a third of the page.
func (r *pdfRenderer) figures(artifactID string) {
	pdf := r.pdf
	figs := r.m.figures[artifactID]
	if len(figs) == 0 {
		return
	}
	width := r.contentWidth()
	for i, f := range figs {
		pdf.Ln(2)
		if !f.Renderable() {
			r.ensureSpace(8)
			pdf.SetFont("Go", "I", 9)
			pdf.SetTextColor(110, 110, 110)
			pdf.MultiCell(0, 4.5, f.Placeholder, "", "L", false)
			pdf.SetTextColor(0, 0, 0)
			continue
		}
		w, h := figureBox(f.Width, f.Height, width, 110)
		r.ensureSpace(h + 8)
		name := fmt.Sprintf("fig-%s-%d", artifactID, i)
		y := pdf.GetY()
		x := pdfMarginL + (width-w)/2
		if !r.placeImage(name, f, x, y, w, h) {
			pdf.SetFont("Go", "I", 9)
			pdf.SetTextColor(110, 110, 110)
			pdf.MultiCell(0, 4.5, f.Caption+" could not be embedded.", "", "L", false)
			pdf.SetTextColor(0, 0, 0)
			continue
		}
		pdf.SetY(y + h + 1)
		pdf.SetFont("Go", "I", 8.5)
		pdf.SetTextColor(90, 90, 90)
		pdf.MultiCell(0, 4.2, f.Caption, "", "C", false)
		pdf.SetTextColor(0, 0, 0)
	}
}

// placeImage registers and draws an image, clearing gofpdf's latched error
// on failure so one bad file cannot poison the document.
func (r *pdfRenderer) placeImage(name string, f figure, x, y, w, h float64) bool {
	pdf := r.pdf
	opts := gofpdf.ImageOptions{ImageType: "PNG"}
	data := f.PNG
	if len(f.JPEG) > 0 {
		opts.ImageType = "JPG"
		data = f.JPEG
	}
	if info := pdf.RegisterImageOptionsReader(name, opts, bytes.NewReader(data)); info == nil || pdf.Err() {
		pdf.ClearError()
		return false
	}
	pdf.ImageOptions(name, x, y, w, h, false, opts, 0, "")
	if pdf.Err() {
		pdf.ClearError()
		return false
	}
	return true
}

// figureBox sizes an image: the column width for anything that wide, its
// natural size at 150 dpi for smaller ones, and never taller than maxH.
func figureBox(px, py int, maxW, maxH float64) (float64, float64) {
	if px <= 0 || py <= 0 {
		return maxW, maxW * 0.6
	}
	natural := float64(px) / 150 * 25.4
	w := math.Min(maxW, math.Max(natural, 60))
	h := w * float64(py) / float64(px)
	if h > maxH {
		h = maxH
		w = h * float64(px) / float64(py)
	}
	return w, h
}

// fitBox scales an image to fit inside a box.
func fitBox(px, py int, maxW, maxH float64) (float64, float64) {
	if px <= 0 || py <= 0 {
		return maxW, maxH
	}
	w := maxW
	h := w * float64(py) / float64(px)
	if h > maxH {
		h = maxH
		w = h * float64(px) / float64(py)
	}
	return w, h
}

// --- Blocks ------------------------------------------------------------------

// blocks renders body blocks starting at the given left edge.
func (r *pdfRenderer) blocks(blocks []doc.Block, left float64) {
	for _, b := range blocks {
		r.block(b, left)
	}
}

func (r *pdfRenderer) block(b doc.Block, left float64) {
	pdf := r.pdf
	switch v := b.(type) {
	case doc.Paragraph:
		r.ensureSpace(pdfBodyLine)
		r.paragraph(v.Inlines, left, pdfBodySize, pdfBodyLine, "")
		pdf.Ln(1.6)
	case doc.Heading:
		size := 12.0 - float64(v.Level-1)*0.7
		if size < 10 {
			size = 10
		}
		r.ensureSpace(size + 6)
		pdf.Ln(1)
		r.paragraph(v.Inlines, left, size, size*0.5, "B")
		pdf.Ln(1.2)
	case doc.List:
		r.list(v, left, 0)
		pdf.Ln(1)
	case doc.Table:
		r.table(v, left)
		pdf.Ln(2)
	case doc.CodeBlock:
		r.code(v, left)
		pdf.Ln(1.5)
	case doc.Quote:
		r.quote(v, left)
		pdf.Ln(1.5)
	case doc.Rule:
		r.ensureSpace(4)
		y := pdf.GetY() + 1.5
		pdf.SetDrawColor(190, 190, 190)
		pdf.SetLineWidth(0.3)
		pdf.Line(left, y, pdfMarginL+r.contentWidth(), y)
		pdf.SetY(y + 2)
	}
}

// paragraph writes inline runs with gofpdf's flowing writer, which wraps
// and breaks pages on its own. The left margin is moved for the duration so
// wrapped lines return to the block's edge.
func (r *pdfRenderer) paragraph(inlines []doc.Inline, left float64, size, lineH float64, baseStyle string) {
	pdf := r.pdf
	oldL, _, _, _ := pdf.GetMargins()
	pdf.SetLeftMargin(left)
	pdf.SetX(left)
	for _, in := range inlines {
		if in.Break {
			pdf.Ln(lineH)
			continue
		}
		if in.Text == "" {
			continue
		}
		style := baseStyle
		if in.Bold && !strings.Contains(style, "B") {
			style += "B"
		}
		if in.Italic {
			style += "I"
		}
		family := "Go"
		if in.Code {
			family = "GoMono"
			style = strings.ReplaceAll(style, "I", "")
		}
		pdf.SetFont(family, normaliseStyle(style), size)
		text := in.Text
		x0, y0 := pdf.GetX(), pdf.GetY()
		switch {
		case in.URL != "":
			pdf.SetTextColor(pdfTextBlue[0], pdfTextBlue[1], pdfTextBlue[2])
			pdf.WriteLinkString(lineH, text, in.URL)
		case in.Ref != "":
			if id := r.refLink(in.Ref); id > 0 {
				pdf.SetTextColor(pdfTextBlue[0], pdfTextBlue[1], pdfTextBlue[2])
				pdf.WriteLinkID(lineH, text, id)
			} else {
				pdf.SetTextColor(0, 0, 0)
				pdf.Write(lineH, text)
			}
		default:
			pdf.SetTextColor(0, 0, 0)
			pdf.Write(lineH, text)
		}
		if in.Strike && pdf.GetY() == y0 {
			// A run that stayed on one line gets a stroke through it; the
			// Go fonts have no combining overlay glyph to fake one with.
			pdf.SetDrawColor(60, 60, 60)
			pdf.SetLineWidth(0.3)
			pdf.Line(x0, y0+lineH*0.55, pdf.GetX(), y0+lineH*0.55)
		}
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(lineH)
	pdf.SetLeftMargin(oldL)
}

// refLink resolves a citation to an artifact's link id: an artifact by its
// reference, or a figure by the artifact that owns it.
func (r *pdfRenderer) refLink(ref string) int {
	if id, ok := r.m.refIndex[ref]; ok {
		return r.linkIDs[id]
	}
	if att, ok := r.m.figureIndex[ref]; ok {
		return r.linkIDs[att.ArtifactID]
	}
	return 0
}

func normaliseStyle(s string) string {
	b := strings.Contains(s, "B")
	i := strings.Contains(s, "I")
	switch {
	case b && i:
		return "BI"
	case b:
		return "B"
	case i:
		return "I"
	}
	return ""
}

func (r *pdfRenderer) list(l doc.List, left float64, depth int) {
	pdf := r.pdf
	markerW := 6.0
	if l.Ordered {
		markerW = 8.0
	}
	for i, item := range l.Items {
		r.ensureSpace(pdfBodyLine)
		marker := "•"
		if l.Ordered {
			marker = fmt.Sprintf("%d.", l.Start+i)
		}
		if item.Task != nil {
			if *item.Task {
				marker = "[x]"
			} else {
				marker = "[ ]"
			}
		}
		y := pdf.GetY()
		pdf.SetXY(left, y)
		pdf.SetFont("Go", "", pdfBodySize)
		pdf.SetTextColor(0, 0, 0)
		pdf.CellFormat(markerW, pdfBodyLine, marker, "", 0, "L", false, 0, "")
		inner := left + markerW
		first := true
		for _, b := range item.Blocks {
			switch v := b.(type) {
			case doc.Paragraph:
				if !first {
					pdf.SetX(inner)
				}
				r.paragraph(v.Inlines, inner, pdfBodySize, pdfBodyLine, "")
			case doc.List:
				r.list(v, inner, depth+1)
			default:
				pdf.SetX(inner)
				r.block(b, inner)
			}
			first = false
		}
		if len(item.Blocks) == 0 {
			pdf.Ln(pdfBodyLine)
		}
		pdf.SetX(left)
	}
}

// table draws a GFM table: columns weighted by their longest cell, rows
// measured and drawn one at a time, the header repeated after a page break.
func (r *pdfRenderer) table(t doc.Table, left float64) {
	pdf := r.pdf
	width := pdfMarginL + r.contentWidth() - left
	ncols := len(t.Header)
	for _, row := range t.Rows {
		if len(row) > ncols {
			ncols = len(row)
		}
	}
	if ncols == 0 {
		return
	}
	weights := make([]float64, ncols)
	measure := func(cells []doc.Cell) {
		for i, c := range cells {
			if i >= ncols {
				break
			}
			n := float64(len([]rune(doc.InlineText(c.Inlines))))
			n = math.Min(math.Max(n, 4), 40)
			if n > weights[i] {
				weights[i] = n
			}
		}
	}
	measure(t.Header)
	for _, row := range t.Rows {
		measure(row)
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	cols := make([]float64, ncols)
	for i := range cols {
		cols[i] = width * weights[i] / total
		if cols[i] < 14 {
			cols[i] = 14
		}
	}
	// Re-normalise after the minimum.
	sum := 0.0
	for _, c := range cols {
		sum += c
	}
	for i := range cols {
		cols[i] = cols[i] * width / sum
	}

	cellText := func(c doc.Cell) string { return doc.InlineText(c.Inlines) }
	rowHeight := func(cells []doc.Cell, size float64) ([]([]string), float64) {
		wrapped := make([][]string, ncols)
		maxLines := 1
		for i := 0; i < ncols; i++ {
			text := ""
			if i < len(cells) {
				text = cellText(cells[i])
			}
			pdf.SetFont("Go", "", size)
			wrapped[i] = pdf.SplitText(text, cols[i]-2*pdfCellPad)
			if len(wrapped[i]) > maxLines {
				maxLines = len(wrapped[i])
			}
		}
		return wrapped, float64(maxLines)*4.2 + 2*pdfCellPad
	}
	draw := func(cells []doc.Cell, header bool) {
		wrapped, h := rowHeight(cells, 9)
		y := pdf.GetY()
		x := left
		pdf.SetDrawColor(205, 205, 205)
		pdf.SetLineWidth(0.2)
		for i := 0; i < ncols; i++ {
			if header {
				pdf.SetFillColor(235, 237, 240)
				pdf.Rect(x, y, cols[i], h, "FD")
			} else {
				pdf.Rect(x, y, cols[i], h, "D")
			}
			align := "L"
			if i < len(t.Align) {
				switch t.Align[i] {
				case "center":
					align = "C"
				case "right":
					align = "R"
				}
			}
			style := ""
			if header || (i < len(cells) && allBold(cells[i].Inlines)) {
				style = "B"
			}
			pdf.SetFont("Go", style, 9)
			pdf.SetTextColor(0, 0, 0)
			if header {
				pdf.SetTextColor(60, 60, 60)
			}
			for j, line := range wrapped[i] {
				pdf.SetXY(x+pdfCellPad, y+pdfCellPad+float64(j)*4.2)
				pdf.CellFormat(cols[i]-2*pdfCellPad, 4.2, line, "", 0, align, false, 0, "")
			}
			x += cols[i]
		}
		pdf.SetY(y + h)
	}
	_, headH := rowHeight(t.Header, 9)
	r.ensureSpace(headH + 8)
	draw(t.Header, true)
	for _, row := range t.Rows {
		_, h := rowHeight(row, 9)
		if pdf.GetY()+h > 297-pdfMarginB {
			pdf.AddPage()
			draw(t.Header, true)
		}
		draw(row, false)
	}
	pdf.SetDrawColor(0, 0, 0)
}

func allBold(inlines []doc.Inline) bool {
	seen := false
	for _, in := range inlines {
		if in.Break || in.Text == "" {
			continue
		}
		seen = true
		if !in.Bold {
			return false
		}
	}
	return seen
}

// code draws a code block line by line on a grey ground, breaking pages
// between lines; indentation is preserved because the text is not reflowed.
func (r *pdfRenderer) code(c doc.CodeBlock, left float64) {
	pdf := r.pdf
	width := pdfMarginL + r.contentWidth() - left
	lineH := 4.2
	pdf.SetFont("GoMono", "", 8.5)
	text := strings.TrimRight(c.Text, "\n")
	var lines []string
	for _, raw := range strings.Split(text, "\n") {
		raw = strings.ReplaceAll(raw, "\t", "    ")
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, pdf.SplitText(raw, width-2*pdfCellPad)...)
	}
	if len(lines) == 0 {
		return
	}
	r.ensureSpace(lineH*2 + 2)
	pdf.SetFillColor(245, 245, 245)
	pdf.SetTextColor(30, 30, 30)
	for i, line := range lines {
		if pdf.GetY()+lineH > 297-pdfMarginB {
			pdf.AddPage()
		}
		y := pdf.GetY()
		pad := 0.0
		if i == 0 {
			pad = 1
		}
		pdf.Rect(left, y, width, lineH+pad, "F")
		pdf.SetXY(left+pdfCellPad, y+pad)
		pdf.CellFormat(width-2*pdfCellPad, lineH, line, "", 1, "L", false, 0, "")
		pdf.SetY(y + lineH + pad)
	}
	y := pdf.GetY()
	pdf.Rect(left, y, width, 1, "F")
	pdf.SetY(y + 1)
	pdf.SetTextColor(0, 0, 0)
}

// quote indents its blocks and draws a bar beside them where they stay on
// one page.
func (r *pdfRenderer) quote(q doc.Quote, left float64) {
	pdf := r.pdf
	startPage := pdf.PageNo()
	startY := pdf.GetY()
	inner := left + 5
	pdf.SetTextColor(70, 70, 70)
	r.blocks(q.Blocks, inner)
	pdf.SetTextColor(0, 0, 0)
	if pdf.PageNo() == startPage {
		pdf.SetFillColor(200, 200, 200)
		pdf.Rect(left+1, startY, 1.2, pdf.GetY()-startY-1, "F")
	}
}

// --- Verification sections ---------------------------------------------------

func (r *pdfRenderer) vvSummary() {
	m, pdf := r.m, r.pdf
	r.sectionTitle("vv-summary", "Verification summary", 0)

	total := 0
	for _, c := range m.coverage.Summary {
		total += c
	}
	pdf.SetFont("Go", "", 10)
	pdf.CellFormat(0, 5.5, fmt.Sprintf("Requirements: %d", total), "", 1, "L", false, 0, "")
	for _, rollup := range rollupDisplayOrder {
		count, ok := m.coverage.Summary[rollup]
		if !ok || count == 0 {
			continue
		}
		r.ensureSpace(6)
		cr, cg, cb := rollupColor(rollup)
		y := pdf.GetY()
		pdf.SetFillColor(cr, cg, cb)
		pdf.Rect(pdfMarginL, y+1.2, 3, 3, "F")
		pdf.SetX(pdfMarginL + 5)
		pdf.CellFormat(0, 5.5, fmt.Sprintf("%s: %d", rollupLabel(rollup), count), "", 1, "L", false, 0, "")
	}
	pdf.Ln(3)

	// Coverage table: Ref | Requirement | Method | Rollup, titles wrapped.
	width := r.contentWidth()
	cols := []float64{22, width - 22 - 32 - 34, 32, 34}
	header := func() {
		y := pdf.GetY()
		pdf.SetFont("Go", "B", 8.5)
		pdf.SetFillColor(235, 237, 240)
		pdf.SetDrawColor(205, 205, 205)
		pdf.SetTextColor(60, 60, 60)
		x := pdfMarginL
		for i, t := range []string{"Ref", "Requirement", "Method", "Rollup"} {
			pdf.SetXY(x, y)
			pdf.CellFormat(cols[i], 5.5, " "+t, "1", 0, "L", true, 0, "")
			x += cols[i]
		}
		pdf.SetY(y + 5.5)
	}
	r.ensureSpace(20)
	pdf.SetFont("Go", "B", 11)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 7, "Requirement coverage", "", 1, "L", false, 0, "")
	header()
	for _, e := range m.coverage.Entries {
		a := m.byID[e.RequirementID]
		ref, title := "", e.Title
		if a != nil {
			ref, title = a.Ref, a.Title
		}
		method := e.VerificationMethod
		if method == "" {
			method = "-"
		}
		pdf.SetFont("Go", "", 9)
		lines := pdf.SplitText(title, cols[1]-2*pdfCellPad)
		h := float64(maxInt(len(lines), 1))*4.4 + 2*pdfCellPad
		if pdf.GetY()+h > 297-pdfMarginB {
			pdf.AddPage()
			header()
		}
		y := pdf.GetY()
		pdf.SetDrawColor(205, 205, 205)
		x := pdfMarginL
		for _, w := range cols {
			pdf.Rect(x, y, w, h, "D")
			x += w
		}
		link := r.linkIDs[e.RequirementID]
		pdf.SetTextColor(pdfTextBlue[0], pdfTextBlue[1], pdfTextBlue[2])
		pdf.SetXY(pdfMarginL+pdfCellPad, y+pdfCellPad)
		pdf.CellFormat(cols[0]-2*pdfCellPad, 4.4, ref, "", 0, "L", false, link, "")
		pdf.SetTextColor(0, 0, 0)
		for j, line := range lines {
			pdf.SetXY(pdfMarginL+cols[0]+pdfCellPad, y+pdfCellPad+float64(j)*4.4)
			pdf.CellFormat(cols[1]-2*pdfCellPad, 4.4, line, "", 0, "L", false, 0, "")
		}
		pdf.SetXY(pdfMarginL+cols[0]+cols[1]+pdfCellPad, y+pdfCellPad)
		pdf.CellFormat(cols[2]-2*pdfCellPad, 4.4, method, "", 0, "L", false, 0, "")
		cr, cg, cb := rollupColor(e.Rollup)
		pdf.SetFillColor(cr, cg, cb)
		pdf.Rect(pdfMarginL+cols[0]+cols[1]+cols[2], y, cols[3], h, "F")
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Go", "B", 8.5)
		pdf.SetXY(pdfMarginL+cols[0]+cols[1]+cols[2], y+pdfCellPad)
		pdf.CellFormat(cols[3], 4.4, rollupLabel(e.Rollup), "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetY(y + h)
	}
	pdf.Ln(4)

	// Gaps.
	any := false
	for _, section := range gapSections(m.gaps) {
		if len(section.IDs) == 0 {
			continue
		}
		if !any {
			r.ensureSpace(16)
			pdf.SetFont("Go", "B", 11)
			pdf.CellFormat(0, 7, "Gaps", "", 1, "L", false, 0, "")
			any = true
		}
		r.ensureSpace(12)
		pdf.SetFont("Go", "B", 9.5)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(0, 6, fmt.Sprintf("%s (%d)", section.Label, len(section.IDs)), "", 1, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		for _, id := range section.IDs {
			title := m.artifactTitles[id]
			if title == "" {
				title = id
			}
			r.ensureSpace(5)
			pdf.SetFont("Go", "", 9)
			pdf.SetX(pdfMarginL + 4)
			pdf.SetTextColor(pdfTextBlue[0], pdfTextBlue[1], pdfTextBlue[2])
			pdf.MultiCell(0, 4.6, "• "+title, "", "L", false)
			pdf.SetTextColor(0, 0, 0)
			if link := r.linkIDs[id]; link > 0 {
				// A MultiCell has no link argument; cover its first line.
				pdf.Link(pdfMarginL+4, pdf.GetY()-4.6, r.contentWidth()-4, 4.6, link)
			}
		}
		pdf.Ln(1.5)
	}
	if !any {
		pdf.SetFont("Go", "I", 10)
		pdf.CellFormat(0, 6, "No gaps detected.", "", 1, "L", false, 0, "")
	}
}

func (r *pdfRenderer) testResults() {
	m, pdf := r.m, r.pdf
	r.sectionTitle("test-results", "Test results", 0)
	width := r.contentWidth()
	cols := []float64{22, width - 22 - 24 - 28 - 40, 24, 28, 40}
	header := func() {
		y := pdf.GetY()
		pdf.SetFont("Go", "B", 8.5)
		pdf.SetFillColor(235, 237, 240)
		pdf.SetDrawColor(205, 205, 205)
		pdf.SetTextColor(60, 60, 60)
		x := pdfMarginL
		for i, t := range []string{"Ref", "Test case", "Result", "Executed", "Run"} {
			pdf.SetXY(x, y)
			pdf.CellFormat(cols[i], 5.5, " "+t, "1", 0, "L", true, 0, "")
			x += cols[i]
		}
		pdf.SetY(y + 5.5)
	}
	if len(m.evidence) == 0 {
		pdf.SetFont("Go", "I", 10)
		pdf.CellFormat(0, 6, "No test cases in this selection.", "", 1, "L", false, 0, "")
	} else {
		header()
	}
	for _, e := range m.evidence {
		pdf.SetFont("Go", "", 9)
		lines := pdf.SplitText(e.Title, cols[1]-2*pdfCellPad)
		runLines := pdf.SplitText(e.RunName, cols[4]-2*pdfCellPad)
		h := float64(maxInt(maxInt(len(lines), len(runLines)), 1))*4.4 + 2*pdfCellPad
		if pdf.GetY()+h > 297-pdfMarginB {
			pdf.AddPage()
			header()
		}
		y := pdf.GetY()
		pdf.SetDrawColor(205, 205, 205)
		x := pdfMarginL
		for _, w := range cols {
			pdf.Rect(x, y, w, h, "D")
			x += w
		}
		link := r.linkIDs[e.TestCaseID]
		pdf.SetTextColor(pdfTextBlue[0], pdfTextBlue[1], pdfTextBlue[2])
		pdf.SetXY(pdfMarginL+pdfCellPad, y+pdfCellPad)
		pdf.CellFormat(cols[0]-2*pdfCellPad, 4.4, e.Ref, "", 0, "L", false, link, "")
		pdf.SetTextColor(0, 0, 0)
		for j, line := range lines {
			pdf.SetXY(pdfMarginL+cols[0]+pdfCellPad, y+pdfCellPad+float64(j)*4.4)
			pdf.CellFormat(cols[1]-2*pdfCellPad, 4.4, line, "", 0, "L", false, 0, "")
		}
		cr, cg, cb := rollupColor(e.Status)
		pdf.SetFillColor(cr, cg, cb)
		pdf.Rect(pdfMarginL+cols[0]+cols[1], y, cols[2], h, "F")
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Go", "B", 8.5)
		pdf.SetXY(pdfMarginL+cols[0]+cols[1], y+pdfCellPad)
		pdf.CellFormat(cols[2], 4.4, e.Status, "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Go", "", 8.5)
		pdf.SetXY(pdfMarginL+cols[0]+cols[1]+cols[2]+pdfCellPad, y+pdfCellPad)
		pdf.CellFormat(cols[3]-2*pdfCellPad, 4.4, e.ExecutedAt, "", 0, "L", false, 0, "")
		for j, line := range runLines {
			pdf.SetXY(pdfMarginL+cols[0]+cols[1]+cols[2]+cols[3]+pdfCellPad, y+pdfCellPad+float64(j)*4.4)
			pdf.CellFormat(cols[4]-2*pdfCellPad, 4.4, line, "", 0, "L", false, 0, "")
		}
		pdf.SetY(y + h)
	}
	pdf.Ln(4)

	r.ensureSpace(16)
	pdf.SetFont("Go", "B", 11)
	pdf.CellFormat(0, 7, "Test runs", "", 1, "L", false, 0, "")
	if len(m.runs) == 0 {
		pdf.SetFont("Go", "I", 10)
		pdf.CellFormat(0, 6, "No test runs recorded.", "", 1, "L", false, 0, "")
	}
	for _, run := range m.runs {
		if run == nil {
			continue
		}
		r.ensureSpace(14)
		pdf.SetFont("Go", "B", 10)
		pdf.MultiCell(0, 5.5, run.Name, "", "L", false)
		pdf.SetFont("Go", "", 9)
		pdf.SetTextColor(90, 90, 90)
		details := []string{
			"Status: " + run.Status,
			"Started: " + run.StartedAt.UTC().Format("2006-01-02 15:04"),
		}
		if run.CompletedAt != nil {
			details = append(details, "Completed: "+run.CompletedAt.UTC().Format("2006-01-02 15:04"))
		}
		pdf.SetX(pdfMarginL + 4)
		pdf.CellFormat(0, 5, strings.Join(details, "    "), "", 1, "L", false, 0, "")
		if strings.TrimSpace(run.Description) != "" {
			pdf.SetX(pdfMarginL + 4)
			pdf.MultiCell(0, 4.6, run.Description, "", "L", false)
		}
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(1.5)
	}
}

// --- Helpers -----------------------------------------------------------------

func (r *pdfRenderer) contentWidth() float64 {
	return 210 - pdfMarginL - pdfMarginR
}

// ensureSpace starts a new page when fewer than needed millimetres remain.
func (r *pdfRenderer) ensureSpace(needed float64) {
	if r.pdf.GetY()+needed > 297-pdfMarginB {
		r.pdf.AddPage()
	}
}

// truncate shortens a string to fit a width at the current font.
func (r *pdfRenderer) truncate(s string, w float64) string {
	pdf := r.pdf
	if pdf.GetStringWidth(s) <= w {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if pdf.GetStringWidth(candidate) <= w {
			return candidate
		}
	}
	return "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
