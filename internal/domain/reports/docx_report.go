package reports

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/reports/doc"
)

// The Word document.
//
// buildReportDOCX renders the same report model as the PDF as a
// WordprocessingML package assembled directly: document, styles, numbering,
// settings, footer, relationships, media parts and core properties. There is
// no third-party dependency; what Word needs is a well-formed package, and the
// parts below are the minimum that gives it real paragraphs and runs, lists
// with bullets and numbers, tables that keep their rows and repeat their
// header, hyperlinks and bookmarks, inline pictures, a table of contents, a
// footer with page numbers, and document properties.

const (
	docxPageW      = 11906 // A4 in twentieths of a point
	docxPageH      = 16838
	docxMargin     = 1134 // 2 cm
	docxTextW      = docxPageW - 2*docxMargin
	emuPerTwip     = 635
	docxMaxImageW  = docxTextW * emuPerTwip       // full text width
	docxMaxImageH  = (docxPageH / 2) * emuPerTwip // half a page
	docxBulletsNum = 1                            // w:numId of the bullet definition
)

type docxRenderer struct {
	m    *reportModel
	body strings.Builder
	// rels accumulate the document's relationships: hyperlinks and images.
	rels   []docxRel
	media  []docxMedia
	nextID int
	// nums holds the numbering instances: one bullet list shared by all
	// bulleted lists, plus one per ordered list so each restarts at its
	// own start number.
	nums     []docxNum
	imageIDs int
}

type docxRel struct {
	ID, Type, Target string
	External         bool
}

type docxMedia struct {
	Name string
	Data []byte
}

type docxNum struct {
	ID    int
	Start int
}

func buildReportDOCX(data *exports.ProjectExport, opts RenderOptions) ([]byte, error) {
	m := buildReportModel(data, opts)
	r := &docxRenderer{m: m, nextID: 10}
	r.nums = append(r.nums, docxNum{ID: docxBulletsNum})

	r.cover()
	if opts.Content.TOC {
		r.toc()
	}
	if fields := profileFields(data.ProductProfile); len(fields) > 0 {
		r.pageBreak()
		r.heading("product-definition", "Product definition", 1)
		for _, f := range fields {
			r.para(docxRunText(f.Label, docxRun{Bold: true, Color: "3C3C3C"}), "", "keepNext")
			r.blocks(f.Blocks, 0)
		}
	}
	if len(m.order) > 0 {
		r.pageBreak()
		for _, n := range m.order {
			r.artifact(n)
		}
	}
	if opts.Content.VVStatus && m.coverage != nil {
		r.pageBreak()
		r.vvSummary()
	}
	if opts.Content.TestResults {
		r.pageBreak()
		r.testResults()
	}
	return r.pack()
}

// --- Cover, contents, sections ----------------------------------------------

func (r *docxRenderer) cover() {
	m := r.m
	if logo, ok := logoFigure(m.opts.Workspace); ok {
		wEmu, hEmu := docxFit(logo.Width, logo.Height, 60*36000, 24*36000) // 60×24 mm in EMU
		r.body.WriteString("<w:p>" + r.drawing(logo, wEmu, hEmu, "Workspace logo") + "</w:p>")
	}
	if m.opts.Workspace.Name != "" {
		r.para(docxRunText(m.opts.Workspace.Name, docxRun{Color: "6E6E6E"}), "Subtle", "")
	}
	r.para(docxRunText(m.data.ProjectName, docxRun{}), "Title", "")
	r.para(docxRunText("Specification", docxRun{Color: "505050", Size: 24}), "Subtitle", "")
	if strings.TrimSpace(m.data.ProjectDesc) != "" {
		r.blocks(doc.Parse(m.data.ProjectDesc), 0)
	}

	kind := "Live project"
	if m.opts.Snapshot.IsBaseline() {
		kind = "Baseline snapshot"
	}
	r.body.WriteString(docxTableOpen(docxTextW, false))
	r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
	r.body.WriteString(docxCell(docxTextW, "F3F5F8",
		docxParaXML(docxRunText(kind, docxRun{Bold: true, Color: "3C3C3C", Size: 18}), "", "")+
			docxParaXML(docxRunText(m.opts.Snapshot.Statement(), docxRun{}), "", "")))
	r.body.WriteString("</w:tr></w:tbl>")
	r.para("", "", "")

	r.body.WriteString(docxTableOpen(docxTextW, false))
	for _, line := range m.coverLines() {
		r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
		r.body.WriteString(docxCell(2200, "", docxParaXML(docxRunText(line.Label, docxRun{Bold: true, Color: "5A5A5A", Size: 18}), "Compact", "")))
		r.body.WriteString(docxCell(docxTextW-2200, "", docxParaXML(docxRunText(line.Value, docxRun{Size: 19}), "Compact", "")))
		r.body.WriteString("</w:tr>")
	}
	r.body.WriteString("</w:tbl>")
	r.para("", "", "")
}

// toc inserts a table-of-contents field. Word fills it in when the document
// is opened (settings.xml asks it to update fields), so the entries and page
// numbers are Word's own rather than a guess.
func (r *docxRenderer) toc() {
	r.pageBreak()
	r.para(docxRunText("Contents", docxRun{}), "TOCHeading", "")
	r.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="TOC1"/></w:pPr>` +
		`<w:r><w:fldChar w:fldCharType="begin" w:dirty="true"/></w:r>` +
		`<w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>Right-click and choose Update Field to build the table of contents.</w:t></w:r>` +
		`<w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
}

// heading writes an outline heading with a bookmark a hyperlink can target.
func (r *docxRenderer) heading(key, title string, level int) {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	r.body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="Heading%d"/></w:pPr>`, level))
	r.body.WriteString(r.bookmarkStart(key))
	r.body.WriteString(docxRunText(title, docxRun{}))
	r.body.WriteString(r.bookmarkEnd())
	r.body.WriteString("</w:p>")
}

func (r *docxRenderer) pageBreak() {
	r.body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
}

// --- Artifacts ---------------------------------------------------------------

func (r *docxRenderer) artifact(n *artifactNode) {
	m := r.m
	a := n.artifact
	level := m.headingLevel(n)
	switch {
	case a.Type == artifacts.TypeHeading:
		r.heading(a.ID, m.title(a), level)
		r.blocks(m.bodies[a.ID], 0)
		r.figures(a.ID)
	case a.Type == artifacts.TypeDescription:
		r.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Subtle"/><w:keepNext/></w:pPr>`)
		r.body.WriteString(r.bookmarkStart(a.ID))
		r.body.WriteString(docxRunText(m.title(a), docxRun{}))
		r.body.WriteString(r.bookmarkEnd())
		r.body.WriteString("</w:p>")
		r.blocks(m.bodies[a.ID], 0)
		r.figures(a.ID)
	default:
		// An artifact title is an outline entry (Word's navigation pane finds
		// it) but sits below the levels the contents list, so the table of
		// contents names sections rather than every requirement.
		if level < 4 {
			level = 4
		}
		r.heading(a.ID, m.title(a), level)
		r.fieldsTable(m.fieldRows(a))
		r.blocks(m.bodies[a.ID], 0)
		r.figures(a.ID)
		if rows := m.links[a.ID]; len(rows) > 0 {
			r.traceabilityTable(rows)
		}
	}
}

func (r *docxRenderer) fieldsTable(rows []fieldRow) {
	r.body.WriteString(docxTableOpen(docxTextW, true))
	for _, row := range rows {
		r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
		r.body.WriteString(docxCell(2400, "F7F7F7", docxParaXML(docxRunText(row.Label, docxRun{Bold: true, Color: "3C3C3C", Size: 18}), "Compact", "")))
		value := docxRunText(row.Value, docxRun{Size: 18})
		if row.Rollup != "" {
			cr, cg, cb := rollupColor(row.Rollup)
			value = docxRunText("■ ", docxRun{Color: fmt.Sprintf("%02X%02X%02X", cr, cg, cb), Size: 18}) + value
		}
		r.body.WriteString(docxCell(docxTextW-2400, "", docxParaXML(value, "Compact", "")))
		r.body.WriteString("</w:tr>")
	}
	r.body.WriteString("</w:tbl>")
	r.para("", "Compact", "")
}

func (r *docxRenderer) traceabilityTable(rows []linkRow) {
	r.para(docxRunText("Traceability", docxRun{Bold: true, Color: "3C3C3C"}), "Compact", "keepNext")
	widths := []int{1700, 3000, docxTextW - 4700}
	r.body.WriteString(docxTableOpen(docxTextW, true))
	r.body.WriteString(`<w:tr><w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>`)
	for i, h := range []string{"Direction", "Relationship", "Artifact"} {
		r.body.WriteString(docxCell(widths[i], "EBEDF0", docxParaXML(docxRunText(h, docxRun{Bold: true, Color: "3C3C3C", Size: 18}), "Compact", "")))
	}
	r.body.WriteString("</w:tr>")
	for _, row := range rows {
		r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
		r.body.WriteString(docxCell(widths[0], "", docxParaXML(docxRunText(row.Direction, docxRun{Size: 18}), "Compact", "")))
		r.body.WriteString(docxCell(widths[1], "", docxParaXML(docxRunText(row.Relationship, docxRun{Size: 18}), "Compact", "")))
		target := row.TargetTitle
		if row.Suspect {
			target += " (suspect)"
		}
		r.body.WriteString(docxCell(widths[2], "", docxParaXML(r.internalLink(row.TargetID, target, 18), "Compact", "")))
		r.body.WriteString("</w:tr>")
	}
	r.body.WriteString("</w:tbl>")
	r.para("", "Compact", "")
}

func (r *docxRenderer) figures(artifactID string) {
	for _, f := range r.m.figures[artifactID] {
		if !f.Renderable() {
			r.para(docxRunText(f.Placeholder, docxRun{Italic: true, Color: "6E6E6E"}), "", "")
			continue
		}
		wEmu, hEmu := docxFit(f.Width, f.Height, docxMaxImageW, docxMaxImageH)
		// Small images keep their natural size at 150 dpi rather than being
		// blown up to the column.
		natural := int64(float64(f.Width) / 150 * 914400)
		if natural < int64(wEmu) {
			wEmu, hEmu = docxFit(f.Width, f.Height, int(natural), docxMaxImageH)
		}
		r.body.WriteString(`<w:p><w:pPr><w:keepNext/><w:jc w:val="center"/></w:pPr>` + r.drawing(f, wEmu, hEmu, f.Caption) + "</w:p>")
		r.para(docxRunText(f.Caption, docxRun{}), "Caption", "")
	}
}

// drawing embeds an image as an inline picture and registers its media
// part and relationship.
func (r *docxRenderer) drawing(f figure, wEmu, hEmu int, alt string) string {
	r.imageIDs++
	ext, data := "png", f.PNG
	if len(f.JPEG) > 0 {
		ext, data = "jpeg", f.JPEG
	}
	name := fmt.Sprintf("image%d.%s", r.imageIDs, ext)
	r.media = append(r.media, docxMedia{Name: name, Data: data})
	rid := r.addRel("http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", "media/"+name, false)
	id := r.imageIDs
	return fmt.Sprintf(`<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">`+
		`<wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="%s" descr="%s"/>`+
		`<a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">`+
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">`+
		`<pic:nvPicPr><pic:cNvPr id="%d" name="%s"/><pic:cNvPicPr/></pic:nvPicPr>`+
		`<pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>`+
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`,
		wEmu, hEmu, id, docxEscape(name), docxEscape(alt), id, docxEscape(name), rid, wEmu, hEmu)
}

// docxFit scales pixel dimensions into a box in EMU.
func docxFit(px, py, maxW, maxH int) (int, int) {
	if px <= 0 || py <= 0 {
		return maxW, maxW / 2
	}
	w := maxW
	h := int(float64(w) * float64(py) / float64(px))
	if h > maxH {
		h = maxH
		w = int(float64(h) * float64(px) / float64(py))
	}
	return w, h
}

// --- Blocks ------------------------------------------------------------------

func (r *docxRenderer) blocks(blocks []doc.Block, listLevel int) {
	for _, b := range blocks {
		r.block(b, listLevel)
	}
}

func (r *docxRenderer) block(b doc.Block, listLevel int) {
	switch v := b.(type) {
	case doc.Paragraph:
		r.para(r.inlines(v.Inlines, 0), "", "")
	case doc.Heading:
		// A heading inside a body is emphasis, not outline: it must not
		// appear in the table of contents.
		size := 26 - (v.Level-1)*2
		if size < 21 {
			size = 21
		}
		r.para(r.inlines(v.Inlines, size), "BodyHeading", "keepNext")
	case doc.List:
		r.list(v, listLevel)
	case doc.Table:
		r.table(v)
	case doc.CodeBlock:
		text := strings.TrimRight(v.Text, "\n")
		var runs strings.Builder
		for i, line := range strings.Split(text, "\n") {
			if i > 0 {
				runs.WriteString("<w:r><w:br/></w:r>")
			}
			runs.WriteString(docxRunText(strings.ReplaceAll(line, "\t", "    "), docxRun{Mono: true, Size: 17}))
		}
		r.para(runs.String(), "Code", "")
	case doc.Quote:
		for _, inner := range v.Blocks {
			switch iv := inner.(type) {
			case doc.Paragraph:
				r.para(r.inlines(iv.Inlines, 0), "Quote", "")
			default:
				r.block(inner, listLevel)
			}
		}
	case doc.Rule:
		r.body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="BEBEBE"/></w:pBdr></w:pPr></w:p>`)
	}
}

func (r *docxRenderer) list(l doc.List, level int) {
	numID := docxBulletsNum
	if l.Ordered {
		numID = len(r.nums) + 1
		r.nums = append(r.nums, docxNum{ID: numID, Start: l.Start})
	}
	if level > 8 {
		level = 8
	}
	for _, item := range l.Items {
		first := true
		for _, b := range item.Blocks {
			switch v := b.(type) {
			case doc.Paragraph:
				runs := r.inlines(v.Inlines, 0)
				if item.Task != nil {
					box := "[ ] "
					if *item.Task {
						box = "[x] "
					}
					runs = docxRunText(box, docxRun{}) + runs
				}
				if first {
					r.body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr></w:pPr>%s</w:p>`, level, numID, runs))
				} else {
					r.body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:ind w:left="%d"/></w:pPr>%s</w:p>`, 720+level*360, runs))
				}
			case doc.List:
				r.list(v, level+1)
			default:
				r.block(b, level+1)
			}
			first = false
		}
		if len(item.Blocks) == 0 {
			r.body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr></w:pPr></w:p>`, level, numID))
		}
	}
}

func (r *docxRenderer) table(t doc.Table) {
	ncols := len(t.Header)
	for _, row := range t.Rows {
		if len(row) > ncols {
			ncols = len(row)
		}
	}
	if ncols == 0 {
		return
	}
	weights := make([]int, ncols)
	measure := func(cells []doc.Cell) {
		for i, c := range cells {
			if i >= ncols {
				break
			}
			n := len([]rune(doc.InlineText(c.Inlines)))
			if n < 4 {
				n = 4
			}
			if n > 40 {
				n = 40
			}
			if n > weights[i] {
				weights[i] = n
			}
		}
	}
	measure(t.Header)
	for _, row := range t.Rows {
		measure(row)
	}
	total := 0
	for _, w := range weights {
		total += w
	}
	widths := make([]int, ncols)
	for i := range widths {
		widths[i] = docxTextW * weights[i] / total
	}
	align := func(i int) string {
		if i < len(t.Align) {
			switch t.Align[i] {
			case "center":
				return "center"
			case "right":
				return "right"
			}
		}
		return ""
	}
	r.body.WriteString(docxTableOpen(docxTextW, true))
	r.body.WriteString(`<w:tr><w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>`)
	for i := 0; i < ncols; i++ {
		var runs string
		if i < len(t.Header) {
			runs = r.inlinesStyled(t.Header[i].Inlines, 18, true)
		}
		r.body.WriteString(docxCell(widths[i], "EBEDF0", docxParaXML(runs, "Compact", align(i))))
	}
	r.body.WriteString("</w:tr>")
	for _, row := range t.Rows {
		r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
		for i := 0; i < ncols; i++ {
			var runs string
			if i < len(row) {
				runs = r.inlinesStyled(row[i].Inlines, 18, false)
			}
			r.body.WriteString(docxCell(widths[i], "", docxParaXML(runs, "Compact", align(i))))
		}
		r.body.WriteString("</w:tr>")
	}
	r.body.WriteString("</w:tbl>")
	r.para("", "Compact", "")
}

// --- Inline runs -------------------------------------------------------------

// docxRun is the styling of one run.
type docxRun struct {
	Bold, Italic, Strike, Mono bool
	Color                      string
	// Size is in half-points; 0 keeps the style's size.
	Size int
}

func (r *docxRenderer) inlines(inlines []doc.Inline, size int) string {
	return r.inlinesStyled(inlines, size, false)
}

func (r *docxRenderer) inlinesStyled(inlines []doc.Inline, size int, forceBold bool) string {
	var b strings.Builder
	for _, in := range inlines {
		if in.Break {
			b.WriteString("<w:r><w:br/></w:r>")
			continue
		}
		if in.Text == "" {
			continue
		}
		run := docxRun{Bold: in.Bold || forceBold, Italic: in.Italic, Strike: in.Strike, Mono: in.Code, Size: size}
		switch {
		case in.URL != "":
			rid := r.addRel("http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink", in.URL, true)
			run.Color = "145AAA"
			b.WriteString(fmt.Sprintf(`<w:hyperlink r:id="%s">%s</w:hyperlink>`, rid, docxRunTextStyle(in.Text, run, "Hyperlink")))
		case in.Ref != "":
			if target := r.refTarget(in.Ref); target != "" {
				run.Color = "145AAA"
				b.WriteString(fmt.Sprintf(`<w:hyperlink w:anchor="%s">%s</w:hyperlink>`, docxBookmarkName(target), docxRunTextStyle(in.Text, run, "Hyperlink")))
			} else {
				b.WriteString(docxRunText(in.Text, run))
			}
		default:
			b.WriteString(docxRunText(in.Text, run))
		}
	}
	return b.String()
}

// refTarget resolves a citation to the artifact id whose bookmark it links.
func (r *docxRenderer) refTarget(ref string) string {
	if id, ok := r.m.refIndex[ref]; ok {
		return id
	}
	if att, ok := r.m.figureIndex[ref]; ok {
		return att.ArtifactID
	}
	return ""
}

// internalLink writes a hyperlink to an artifact's bookmark, or plain text
// when the artifact is not in the document.
func (r *docxRenderer) internalLink(artifactID, text string, size int) string {
	if _, ok := r.m.byID[artifactID]; !ok {
		return docxRunText(text, docxRun{Size: size})
	}
	return fmt.Sprintf(`<w:hyperlink w:anchor="%s">%s</w:hyperlink>`, docxBookmarkName(artifactID), docxRunTextStyle(text, docxRun{Color: "145AAA", Size: size}, "Hyperlink"))
}

func docxRunText(text string, run docxRun) string {
	return docxRunTextStyle(text, run, "")
}

func docxRunTextStyle(text string, run docxRun, charStyle string) string {
	var p strings.Builder
	if charStyle != "" {
		p.WriteString(`<w:rStyle w:val="` + charStyle + `"/>`)
	}
	if run.Mono {
		p.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`)
	}
	if run.Bold {
		p.WriteString("<w:b/>")
	}
	if run.Italic {
		p.WriteString("<w:i/>")
	}
	if run.Strike {
		p.WriteString("<w:strike/>")
	}
	if run.Color != "" {
		p.WriteString(`<w:color w:val="` + run.Color + `"/>`)
	}
	if run.Size > 0 {
		p.WriteString(fmt.Sprintf(`<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, run.Size, run.Size))
	}
	if run.Mono {
		p.WriteString(`<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`)
	}
	props := ""
	if p.Len() > 0 {
		props = "<w:rPr>" + p.String() + "</w:rPr>"
	}
	return `<w:r>` + props + `<w:t xml:space="preserve">` + docxEscape(text) + `</w:t></w:r>`
}

// para writes a paragraph. extra is "keepNext" to bind it to the next
// paragraph.
func (r *docxRenderer) para(runs, style, extra string) {
	r.body.WriteString(docxParaXML(runs, style, extra))
}

func docxParaXML(runs, style, extra string) string {
	var p strings.Builder
	if style != "" {
		p.WriteString(`<w:pStyle w:val="` + style + `"/>`)
	}
	switch extra {
	case "keepNext":
		p.WriteString("<w:keepNext/>")
	case "center":
		p.WriteString(`<w:jc w:val="center"/>`)
	case "right":
		p.WriteString(`<w:jc w:val="right"/>`)
	}
	props := ""
	if p.Len() > 0 {
		props = "<w:pPr>" + p.String() + "</w:pPr>"
	}
	return "<w:p>" + props + runs + "</w:p>"
}

// --- Bookmarks and relationships --------------------------------------------

var bookmarkCounter int

func (r *docxRenderer) bookmarkStart(key string) string {
	r.nextID++
	return fmt.Sprintf(`<w:bookmarkStart w:id="%d" w:name="%s"/>`, r.nextID, docxBookmarkName(key))
}

func (r *docxRenderer) bookmarkEnd() string {
	return fmt.Sprintf(`<w:bookmarkEnd w:id="%d"/>`, r.nextID)
}

// docxBookmarkName makes a legal bookmark name (letters, digits and
// underscores, starting with a letter, at most 40 characters) from a key.
func docxBookmarkName(key string) string {
	var b strings.Builder
	b.WriteString("a_")
	for _, ch := range key {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 40 {
			break
		}
	}
	return b.String()
}

func (r *docxRenderer) addRel(relType, target string, external bool) string {
	for _, rel := range r.rels {
		if rel.Type == relType && rel.Target == target && rel.External == external {
			return rel.ID
		}
	}
	id := fmt.Sprintf("rId%d", 100+len(r.rels))
	r.rels = append(r.rels, docxRel{ID: id, Type: relType, Target: target, External: external})
	return id
}

// --- Verification sections ---------------------------------------------------

func (r *docxRenderer) vvSummary() {
	m := r.m
	r.heading("vv-summary", "Verification summary", 1)
	total := 0
	for _, c := range m.coverage.Summary {
		total += c
	}
	r.para(docxRunText(fmt.Sprintf("Requirements: %d", total), docxRun{}), "", "")
	for _, rollup := range rollupDisplayOrder {
		count, ok := m.coverage.Summary[rollup]
		if !ok || count == 0 {
			continue
		}
		cr, cg, cb := rollupColor(rollup)
		r.para(docxRunText("■ ", docxRun{Color: fmt.Sprintf("%02X%02X%02X", cr, cg, cb)})+
			docxRunText(fmt.Sprintf("%s: %d", rollupLabel(rollup), count), docxRun{}), "Compact", "")
	}
	r.para("", "Compact", "")

	r.para(docxRunText("Requirement coverage", docxRun{Bold: true}), "Heading2", "")
	widths := []int{1500, docxTextW - 1500 - 2000 - 2200, 2000, 2200}
	r.body.WriteString(docxTableOpen(docxTextW, true))
	r.body.WriteString(`<w:tr><w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>`)
	for i, h := range []string{"Ref", "Requirement", "Method", "Rollup"} {
		r.body.WriteString(docxCell(widths[i], "EBEDF0", docxParaXML(docxRunText(h, docxRun{Bold: true, Color: "3C3C3C", Size: 18}), "Compact", "")))
	}
	r.body.WriteString("</w:tr>")
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
		cr, cg, cb := rollupColor(e.Rollup)
		fill := fmt.Sprintf("%02X%02X%02X", cr, cg, cb)
		r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
		r.body.WriteString(docxCell(widths[0], "", docxParaXML(r.internalLink(e.RequirementID, ref, 18), "Compact", "")))
		r.body.WriteString(docxCell(widths[1], "", docxParaXML(docxRunText(title, docxRun{Size: 18}), "Compact", "")))
		r.body.WriteString(docxCell(widths[2], "", docxParaXML(docxRunText(method, docxRun{Size: 18}), "Compact", "")))
		r.body.WriteString(docxCell(widths[3], fill, docxParaXML(docxRunText(rollupLabel(e.Rollup), docxRun{Bold: true, Color: "FFFFFF", Size: 17}), "Compact", "center")))
		r.body.WriteString("</w:tr>")
	}
	r.body.WriteString("</w:tbl>")
	r.para("", "Compact", "")

	any := false
	for _, section := range gapSections(m.gaps) {
		if len(section.IDs) == 0 {
			continue
		}
		if !any {
			r.para(docxRunText("Gaps", docxRun{Bold: true}), "Heading2", "")
			any = true
		}
		r.para(docxRunText(fmt.Sprintf("%s (%d)", section.Label, len(section.IDs)), docxRun{Bold: true, Color: "3C3C3C"}), "Compact", "keepNext")
		for _, id := range section.IDs {
			title := m.artifactTitles[id]
			if title == "" {
				title = id
			}
			r.body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="%d"/></w:numPr></w:pPr>%s</w:p>`, docxBulletsNum, r.internalLink(id, title, 0)))
		}
	}
	if !any {
		r.para(docxRunText("No gaps detected.", docxRun{Italic: true}), "", "")
	}
}

func (r *docxRenderer) testResults() {
	m := r.m
	r.heading("test-results", "Test results", 1)
	if len(m.evidence) == 0 {
		r.para(docxRunText("No test cases in this selection.", docxRun{Italic: true}), "", "")
	} else {
		widths := []int{1500, docxTextW - 1500 - 1500 - 2000 - 2600, 1500, 2000, 2600}
		r.body.WriteString(docxTableOpen(docxTextW, true))
		r.body.WriteString(`<w:tr><w:trPr><w:cantSplit/><w:tblHeader/></w:trPr>`)
		for i, h := range []string{"Ref", "Test case", "Result", "Executed", "Run"} {
			r.body.WriteString(docxCell(widths[i], "EBEDF0", docxParaXML(docxRunText(h, docxRun{Bold: true, Color: "3C3C3C", Size: 18}), "Compact", "")))
		}
		r.body.WriteString("</w:tr>")
		for _, e := range m.evidence {
			cr, cg, cb := rollupColor(e.Status)
			fill := fmt.Sprintf("%02X%02X%02X", cr, cg, cb)
			r.body.WriteString("<w:tr><w:trPr><w:cantSplit/></w:trPr>")
			r.body.WriteString(docxCell(widths[0], "", docxParaXML(r.internalLink(e.TestCaseID, e.Ref, 18), "Compact", "")))
			r.body.WriteString(docxCell(widths[1], "", docxParaXML(docxRunText(e.Title, docxRun{Size: 18}), "Compact", "")))
			r.body.WriteString(docxCell(widths[2], fill, docxParaXML(docxRunText(e.Status, docxRun{Bold: true, Color: "FFFFFF", Size: 17}), "Compact", "center")))
			r.body.WriteString(docxCell(widths[3], "", docxParaXML(docxRunText(e.ExecutedAt, docxRun{Size: 17}), "Compact", "")))
			r.body.WriteString(docxCell(widths[4], "", docxParaXML(docxRunText(e.RunName, docxRun{Size: 17}), "Compact", "")))
			r.body.WriteString("</w:tr>")
		}
		r.body.WriteString("</w:tbl>")
		r.para("", "Compact", "")
	}

	r.para(docxRunText("Test runs", docxRun{Bold: true}), "Heading2", "")
	if len(m.runs) == 0 {
		r.para(docxRunText("No test runs recorded.", docxRun{Italic: true}), "", "")
	}
	for _, run := range m.runs {
		if run == nil {
			continue
		}
		r.para(docxRunText(run.Name, docxRun{Bold: true}), "Compact", "keepNext")
		details := []string{"Status: " + run.Status, "Started: " + run.StartedAt.UTC().Format("2006-01-02 15:04")}
		if run.CompletedAt != nil {
			details = append(details, "Completed: "+run.CompletedAt.UTC().Format("2006-01-02 15:04"))
		}
		r.para(docxRunText(strings.Join(details, "    "), docxRun{Color: "5A5A5A", Size: 18}), "Compact", "")
		if strings.TrimSpace(run.Description) != "" {
			r.para(docxRunText(run.Description, docxRun{Size: 18}), "", "")
		} else {
			r.para("", "Compact", "")
		}
	}
}

// --- Tables ------------------------------------------------------------------

func docxTableOpen(width int, bordered bool) string {
	borders := ""
	if bordered {
		borders = `<w:tblBorders>` +
			`<w:top w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
			`<w:left w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
			`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
			`<w:right w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
			`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
			`<w:insideV w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
			`</w:tblBorders>`
	} else {
		borders = `<w:tblBorders><w:top w:val="none" w:sz="0" w:space="0" w:color="auto"/><w:left w:val="none" w:sz="0" w:space="0" w:color="auto"/><w:bottom w:val="none" w:sz="0" w:space="0" w:color="auto"/><w:right w:val="none" w:sz="0" w:space="0" w:color="auto"/><w:insideH w:val="none" w:sz="0" w:space="0" w:color="auto"/><w:insideV w:val="none" w:sz="0" w:space="0" w:color="auto"/></w:tblBorders>`
	}
	return fmt.Sprintf(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="%d" w:type="dxa"/><w:tblLayout w:type="fixed"/>%s`+
		`<w:tblCellMar><w:top w:w="40" w:type="dxa"/><w:left w:w="90" w:type="dxa"/><w:bottom w:w="40" w:type="dxa"/><w:right w:w="90" w:type="dxa"/></w:tblCellMar>`+
		`<w:tblLook w:val="04A0" w:firstRow="1" w:lastRow="0" w:firstColumn="0" w:lastColumn="0" w:noHBand="0" w:noVBand="1"/></w:tblPr>`, width, borders)
}

// docxCell emits a cell of the given width, optionally shaded, wrapping the
// supplied paragraph fragment(s).
func docxCell(widthDxa int, fill string, content string) string {
	shade := ""
	if fill != "" {
		shade = `<w:shd w:val="clear" w:color="auto" w:fill="` + fill + `"/>`
	}
	if content == "" {
		content = "<w:p/>"
	}
	return fmt.Sprintf(`<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>%s</w:tcPr>%s</w:tc>`, widthDxa, shade, content)
}

// --- Package -----------------------------------------------------------------

// docxEscape escapes text for XML character data and attributes and strips
// control characters illegal in XML 1.0.
func docxEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '\t', '\n', '\r':
			b.WriteRune(r)
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

const docxNamespaces = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`

func (r *docxRenderer) documentXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document ` + docxNamespaces + `><w:body>` + r.body.String() +
		fmt.Sprintf(`<w:sectPr><w:footerReference w:type="default" r:id="rIdFooter"/><w:pgSz w:w="%d" w:h="%d"/>`, docxPageW, docxPageH) +
		fmt.Sprintf(`<w:pgMar w:top="%d" w:right="%d" w:bottom="%d" w:left="%d" w:header="708" w:footer="708" w:gutter="0"/>`, docxMargin, docxMargin, docxMargin, docxMargin) +
		`</w:sectPr></w:body></w:document>`
}

func (r *docxRenderer) footerXML() string {
	m := r.m
	left := m.data.ProjectName + " · " + m.opts.Snapshot.Label()
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:ftr ` + docxNamespaces + `>` +
		`<w:p><w:pPr><w:pStyle w:val="Footer"/><w:tabs><w:tab w:val="right" w:pos="` + fmt.Sprint(docxTextW) + `"/></w:tabs></w:pPr>` +
		docxRunText(left, docxRun{Color: "787878", Size: 16}) +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:tab/><w:t xml:space="preserve">Page </w:t></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:instrText xml:space="preserve"> PAGE </w:instrText></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:t>1</w:t></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="end"/></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:t xml:space="preserve"> of </w:t></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:instrText xml:space="preserve"> NUMPAGES </w:instrText></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:t>1</w:t></w:r>` +
		`<w:r><w:rPr><w:color w:val="787878"/><w:sz w:val="16"/></w:rPr><w:fldChar w:fldCharType="end"/></w:r>` +
		`</w:p></w:ftr>`
}

func (r *docxRenderer) numberingXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	// Abstract 1: bullets at nine levels. Abstract 2: decimal at nine levels.
	b.WriteString(`<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>`)
	bullets := []string{"•", "◦", "▪"}
	for lvl := 0; lvl < 9; lvl++ {
		b.WriteString(fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="%s"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			lvl, bullets[lvl%3], 720+lvl*360))
	}
	b.WriteString(`</w:abstractNum>`)
	b.WriteString(`<w:abstractNum w:abstractNumId="2"><w:multiLevelType w:val="hybridMultilevel"/>`)
	for lvl := 0; lvl < 9; lvl++ {
		fmtName := "decimal"
		switch lvl % 3 {
		case 1:
			fmtName = "lowerLetter"
		case 2:
			fmtName = "lowerRoman"
		}
		b.WriteString(fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="1"/><w:numFmt w:val="%s"/><w:lvlText w:val="%%%d."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="%d" w:hanging="360"/></w:pPr></w:lvl>`,
			lvl, fmtName, lvl+1, 720+lvl*360))
	}
	b.WriteString(`</w:abstractNum>`)
	for _, n := range r.nums {
		if n.ID == docxBulletsNum {
			b.WriteString(fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="1"/></w:num>`, n.ID))
			continue
		}
		start := n.Start
		if start < 1 {
			start = 1
		}
		b.WriteString(fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="2"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="%d"/></w:lvlOverride></w:num>`, n.ID, start))
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}

func (r *docxRenderer) relsXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	b.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	b.WriteString(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`)
	b.WriteString(`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/>`)
	b.WriteString(`<Relationship Id="rIdFooter" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer" Target="footer1.xml"/>`)
	for _, rel := range r.rels {
		mode := ""
		if rel.External {
			mode = ` TargetMode="External"`
		}
		b.WriteString(fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"%s/>`, rel.ID, rel.Type, docxEscape(rel.Target), mode))
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func (r *docxRenderer) contentTypesXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Default Extension="png" ContentType="image/png"/>`)
	b.WriteString(`<Default Extension="jpeg" ContentType="image/jpeg"/>`)
	b.WriteString(`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`)
	b.WriteString(`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`)
	b.WriteString(`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>`)
	b.WriteString(`<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>`)
	b.WriteString(`<Override PartName="/word/footer1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml"/>`)
	b.WriteString(`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>`)
	b.WriteString(`<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>`)
	b.WriteString(`</Types>`)
	return b.String()
}

func (r *docxRenderer) coreXML() string {
	m := r.m
	now := m.opts.Snapshot.ExportedAt.UTC().Format(time.RFC3339)
	author := m.opts.Author
	if author == "" {
		author = m.opts.Workspace.Name
	}
	if author == "" {
		author = "OpenV"
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" ` +
		`xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		`<dc:title>` + docxEscape(m.data.ProjectName) + `</dc:title>` +
		`<dc:subject>` + docxEscape(m.opts.Snapshot.Statement()) + `</dc:subject>` +
		`<dc:creator>` + docxEscape(author) + `</dc:creator>` +
		`<cp:lastModifiedBy>OpenV</cp:lastModifiedBy>` +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created>` +
		`<dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified>` +
		`</cp:coreProperties>`
}

const docxAppXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` +
	`<Application>OpenV</Application></Properties>`

const docxRootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` +
	`</Relationships>`

// docxSettingsXML asks Word to refresh fields on open, which is what fills
// the table of contents and the page counts.
const docxSettingsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:updateFields w:val="true"/><w:defaultTabStop w:val="720"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat>` +
	`</w:settings>`

// docxStylesXML defines every style the document references.
const docxStylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="21"/><w:szCs w:val="21"/><w:lang w:val="en-GB"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="100" w:line="264" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:before="240" w:after="60"/></w:pPr><w:rPr><w:b/><w:sz w:val="52"/><w:szCs w:val="52"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="240"/></w:pPr><w:rPr><w:color w:val="505050"/><w:sz w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="34"/><w:szCs w:val="34"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="280" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="220" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="25"/><w:szCs w:val="25"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="200" w:after="60"/><w:outlineLvl w:val="3"/></w:pPr><w:rPr><w:b/><w:sz w:val="23"/><w:szCs w:val="23"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading5"><w:name w:val="heading 5"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="180" w:after="60"/><w:outlineLvl w:val="4"/></w:pPr><w:rPr><w:b/><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading6"><w:name w:val="heading 6"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="160" w:after="60"/><w:outlineLvl w:val="5"/></w:pPr><w:rPr><w:b/><w:i/><w:sz w:val="21"/><w:szCs w:val="21"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="BodyHeading"><w:name w:val="Body Heading"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="160" w:after="60"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Subtle"><w:name w:val="Subtle"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="40"/></w:pPr><w:rPr><w:color w:val="808080"/><w:sz w:val="17"/><w:szCs w:val="17"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Compact"><w:name w:val="Compact"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="0" w:after="0" w:line="240" w:lineRule="auto"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:before="40" w:after="200"/><w:jc w:val="center"/></w:pPr><w:rPr><w:i/><w:color w:val="5A5A5A"/><w:sz w:val="18"/><w:szCs w:val="18"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F5F5F5"/><w:spacing w:before="60" w:after="120" w:line="240" w:lineRule="auto"/><w:ind w:left="120" w:right="120"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="17"/><w:szCs w:val="17"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:pBdr><w:left w:val="single" w:sz="12" w:space="8" w:color="C8C8C8"/></w:pBdr><w:ind w:left="360"/></w:pPr><w:rPr><w:color w:val="464646"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="40"/><w:contextualSpacing/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOCHeading"><w:name w:val="TOC Heading"/><w:basedOn w:val="Heading1"/><w:next w:val="Normal"/><w:pPr><w:outlineLvl w:val="9"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOC1"><w:name w:val="toc 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="9638"/></w:tabs><w:spacing w:after="60"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOC2"><w:name w:val="toc 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="9638"/></w:tabs><w:spacing w:after="40"/><w:ind w:left="220"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TOC3"><w:name w:val="toc 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="9638"/></w:tabs><w:spacing w:after="40"/><w:ind w:left="440"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Footer"><w:name w:val="footer"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="0"/></w:pPr></w:style>` +
	`<w:style w:type="character" w:default="1" w:styleId="DefaultParagraphFont"><w:name w:val="Default Paragraph Font"/></w:style>` +
	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:basedOn w:val="DefaultParagraphFont"/><w:rPr><w:color w:val="145AAA"/><w:u w:val="single"/></w:rPr></w:style>` +
	`<w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/><w:tblPr><w:tblInd w:w="0" w:type="dxa"/><w:tblCellMar><w:top w:w="0" w:type="dxa"/><w:left w:w="108" w:type="dxa"/><w:bottom w:w="0" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:basedOn w:val="TableNormal"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:left w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:right w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/></w:tblBorders></w:tblPr></w:style>` +
	`</w:styles>`

func (r *docxRenderer) pack() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct {
		name    string
		content []byte
	}{
		{"[Content_Types].xml", []byte(r.contentTypesXML())},
		{"_rels/.rels", []byte(docxRootRelsXML)},
		{"docProps/core.xml", []byte(r.coreXML())},
		{"docProps/app.xml", []byte(docxAppXML)},
		{"word/_rels/document.xml.rels", []byte(r.relsXML())},
		{"word/styles.xml", []byte(docxStylesXML)},
		{"word/numbering.xml", []byte(r.numberingXML())},
		{"word/settings.xml", []byte(docxSettingsXML)},
		{"word/footer1.xml", []byte(r.footerXML())},
		{"word/document.xml", []byte(r.documentXML())},
	}
	for _, part := range parts {
		w, err := zw.Create(part.name)
		if err != nil {
			return nil, fmt.Errorf("docx: create %s: %w", part.name, err)
		}
		if _, err := w.Write(part.content); err != nil {
			return nil, fmt.Errorf("docx: write %s: %w", part.name, err)
		}
	}
	for _, media := range r.media {
		w, err := zw.Create("word/media/" + media.Name)
		if err != nil {
			return nil, fmt.Errorf("docx: create media %s: %w", media.Name, err)
		}
		if _, err := w.Write(media.Data); err != nil {
			return nil, fmt.Errorf("docx: write media %s: %w", media.Name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("docx: finalize: %w", err)
	}
	return buf.Bytes(), nil
}
