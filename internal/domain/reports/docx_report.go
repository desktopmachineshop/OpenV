package reports

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
)

// buildReportDOCX renders a project spec document as a WordprocessingML (.docx)
// file. It consumes the same report model the PDF renderer uses (artifact tree,
// title lookup, and traceability link groups) and emits a minimal but
// well-formed OOXML package: [Content_Types].xml, _rels/.rels, word/styles.xml,
// and word/document.xml, zipped together. No third-party dependency is used;
// the document XML is assembled directly and the container is an archive/zip.
func buildReportDOCX(data *exports.ProjectExport, baselineName string) ([]byte, error) {
	model := buildReportModel(data, baselineName)

	var body strings.Builder

	// Title: project name (H1).
	body.WriteString(docxHeading(data.ProjectName, 1))

	if data.ProjectDesc != "" {
		body.WriteString(docxParagraph(stripMarkdown(data.ProjectDesc), ""))
	}

	// Metadata line(s).
	if baselineName != "" {
		body.WriteString(docxParagraph("Baseline: "+baselineName, "Subtle"))
	}
	body.WriteString(docxParagraph("Generated: "+time.Now().Format("2006-01-02 15:04"), "Subtle"))

	// Product definition section (mirrors the PDF report).
	body.WriteString(docxProductDefinition(data.ProductProfile))

	// Artifact tree. Project is H1, so artifacts start at H2 and deepen per
	// level (capped at H6, the deepest heading style we define).
	for _, node := range model.roots {
		docxRenderArtifactNode(&body, node, 0, model)
	}

	document := docxDocumentXML(body.String())

	// Assemble the zip container.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	parts := []struct {
		name    string
		content string
	}{
		{"[Content_Types].xml", docxContentTypesXML},
		{"_rels/.rels", docxRootRelsXML},
		{"word/_rels/document.xml.rels", docxDocumentRelsXML},
		{"word/styles.xml", docxStylesXML},
		{"word/document.xml", document},
	}
	for _, part := range parts {
		w, err := zw.Create(part.name)
		if err != nil {
			return nil, fmt.Errorf("docx: create %s: %w", part.name, err)
		}
		if _, err := w.Write([]byte(part.content)); err != nil {
			return nil, fmt.Errorf("docx: write %s: %w", part.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("docx: finalize: %w", err)
	}

	return buf.Bytes(), nil
}

// docxRenderArtifactNode writes an artifact and its descendants. depth 0 maps to
// heading level 2 (the project itself is level 1).
func docxRenderArtifactNode(body *strings.Builder, node *artifactNode, depth int, model *reportModel) {
	artifact := node.artifact
	level := depth + 2
	if level > 6 {
		level = 6
	}

	// Headings and descriptions carry no traceability table; other artifact
	// types (requirements, tests, etc.) get a title heading plus a details and
	// traceability table.
	switch artifact.Type {
	case "description":
		// Description artifacts render only their body as prose.
		if artifact.Body != "" {
			body.WriteString(docxParagraph(stripMarkdown(artifact.Body), ""))
		}
	case "heading":
		body.WriteString(docxHeading(model.title(artifact), level))
		if artifact.Body != "" {
			body.WriteString(docxParagraph(stripMarkdown(artifact.Body), ""))
		}
	default:
		body.WriteString(docxHeading(model.title(artifact), level))
		if artifact.Body != "" {
			body.WriteString(docxParagraph(stripMarkdown(artifact.Body), ""))
		}
		body.WriteString(docxArtifactDetailsTable(artifact.Ref, artifact.Type, artifact.Version))
		body.WriteString(docxTraceabilityTable(node, model))
	}

	for _, child := range node.children {
		docxRenderArtifactNode(body, child, depth+1, model)
	}
}

// docxArtifactDetailsTable renders the Type/Version metadata as a two-column
// table.
func docxArtifactDetailsTable(ref, artifactType string, version int) string {
	rows := [][2]string{}
	// The reference is the first thing a reader needs when citing this
	// artifact elsewhere, so it leads the table.
	if ref != "" {
		rows = append(rows, [2]string{"Reference", ref})
	}
	rows = append(rows,
		[2]string{"Type", artifactType},
		[2]string{"Version", fmt.Sprintf("v%d", version)},
	)
	var b strings.Builder
	b.WriteString(docxTableOpen())
	for _, row := range rows {
		b.WriteString(docxTableRow(
			docxTableCell(2500, docxParagraph(row[0], "TableLabel")),
			docxTableCell(6500, docxParagraph(row[1], "")),
		))
	}
	b.WriteString(docxTableClose())
	return b.String()
}

// docxTraceabilityTable renders incoming/outgoing links for an artifact as a
// three-column table (Direction, Relationship, Target). Nothing is emitted when
// the artifact has no links.
func docxTraceabilityTable(node *artifactNode, model *reportModel) string {
	groups, ok := model.linkGroupsByArtifact[node.artifact.ID]
	if !ok {
		return ""
	}
	if len(groups.incoming) == 0 && len(groups.outgoing) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(docxParagraph("Traceability", "TableLabel"))
	b.WriteString(docxTableOpen())
	// Header row.
	b.WriteString(docxTableRow(
		docxTableCell(2000, docxParagraph("Direction", "TableLabel")),
		docxTableCell(3500, docxParagraph("Relationship", "TableLabel")),
		docxTableCell(3500, docxParagraph("Target", "TableLabel")),
	))

	writeGroup := func(direction string, linksByType map[string][]*linksdomain.Link, isIncoming bool) {
		for linkType, items := range linksByType {
			label := linkTypeLabelForDirection(linkType, isIncoming)
			for _, link := range items {
				targetID := link.ToID
				if isIncoming {
					targetID = link.FromID
				}
				title := model.artifactTitles[targetID]
				if title == "" {
					title = targetID
				}
				b.WriteString(docxTableRow(
					docxTableCell(2000, docxParagraph(direction, "")),
					docxTableCell(3500, docxParagraph(label, "")),
					docxTableCell(3500, docxParagraph(title, "")),
				))
			}
		}
	}
	writeGroup("Incoming", groups.incoming, true)
	writeGroup("Outgoing", groups.outgoing, false)

	b.WriteString(docxTableClose())
	return b.String()
}

// docxProductDefinition renders the product profile section, if present.
func docxProductDefinition(profile *products.ProductProfile) string {
	if !productProfileHasContent(profile) {
		return ""
	}
	var b strings.Builder
	b.WriteString(docxHeading("Product Definition", 2))

	writeField := func(label, text string) {
		if text == "" {
			return
		}
		b.WriteString(docxParagraph(label, "TableLabel"))
		b.WriteString(docxParagraph(stripMarkdown(text), ""))
	}
	writeField("Vision", profile.Vision)
	writeField("Problem Statement", profile.ProblemStatement)
	writeField("Target Users", profile.TargetUsers)

	if len(profile.SuccessMetrics) > 0 {
		b.WriteString(docxParagraph("Success Metrics", "TableLabel"))
		for _, metric := range profile.SuccessMetrics {
			name := mapStringValue(metric, "name", "title", "metric")
			if name == "" {
				name = "(unnamed metric)"
			}
			line := name
			if target := mapStringValue(metric, "target"); target != "" {
				line += ": " + target
			}
			if current := mapStringValue(metric, "current"); current != "" {
				line += fmt.Sprintf(" (%s)", current)
			}
			b.WriteString(docxParagraph(stripMarkdown(line), "ListBullet"))
		}
	}

	if len(profile.Constraints) > 0 {
		b.WriteString(docxParagraph("Constraints", "TableLabel"))
		for _, constraint := range profile.Constraints {
			text := mapStringValue(constraint, "text", "description", "name", "title")
			if text == "" {
				continue
			}
			b.WriteString(docxParagraph(stripMarkdown(text), "ListBullet"))
		}
	}

	return b.String()
}

// --- OOXML fragment builders -------------------------------------------------

// docxHeading emits a heading paragraph at the given level (1-6).
func docxHeading(text string, level int) string {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	return docxParagraph(text, fmt.Sprintf("Heading%d", level))
}

// docxParagraph emits a paragraph, optionally with a named style. Empty text
// still produces a paragraph (Word tolerates empty runs) so callers can rely on
// consistent spacing.
func docxParagraph(text, style string) string {
	var b strings.Builder
	b.WriteString("<w:p>")
	if style != "" {
		b.WriteString("<w:pPr><w:pStyle w:val=\"")
		b.WriteString(style)
		b.WriteString("\"/></w:pPr>")
	}
	// Split on newlines so multi-line body text keeps its line breaks.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteString("<w:r><w:br/></w:r>")
		}
		b.WriteString("<w:r><w:t xml:space=\"preserve\">")
		b.WriteString(docxEscape(line))
		b.WriteString("</w:t></w:r>")
	}
	b.WriteString("</w:p>")
	return b.String()
}

func docxTableOpen() string {
	return `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/>` +
		`<w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
		`<w:left w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
		`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
		`<w:right w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
		`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
		`<w:insideV w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/>` +
		`</w:tblBorders></w:tblPr>`
}

func docxTableClose() string {
	return "</w:tbl>"
}

func docxTableRow(cells ...string) string {
	return "<w:tr>" + strings.Join(cells, "") + "</w:tr>"
}

// docxTableCell emits a table cell of the given width (in twentieths of a point,
// "dxa") wrapping the supplied paragraph fragment(s).
func docxTableCell(widthDxa int, content string) string {
	return fmt.Sprintf("<w:tc><w:tcPr><w:tcW w:w=\"%d\" w:type=\"dxa\"/></w:tcPr>%s</w:tc>", widthDxa, content)
}

// docxEscape escapes text for inclusion in XML character data / attributes and
// strips XML-1.0-illegal control characters so the document stays well-formed.
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
			// Drop control characters that are illegal in XML 1.0.
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func docxDocumentXML(body string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="720" w:footer="720" w:gutter="0"/>` +
		`</w:sectPr>` +
		`</w:body></w:document>`
}

// --- Static OOXML parts ------------------------------------------------------

const docxContentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`</Types>`

const docxRootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`</Relationships>`

const docxDocumentRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
	`</Relationships>`

// docxStylesXML defines the styles referenced by the document: Normal, six
// heading levels, a bullet list, and the internal label/table styles. Heading
// styles carry outlineLvl so Word builds a navigable document map / TOC.
const docxStylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:pPr><w:spacing w:after="120"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="180" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="160" w:after="80"/><w:outlineLvl w:val="3"/></w:pPr><w:rPr><w:b/><w:i/><w:sz w:val="24"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading5"><w:name w:val="heading 5"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="140" w:after="80"/><w:outlineLvl w:val="4"/></w:pPr><w:rPr><w:b/><w:sz w:val="22"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading6"><w:name w:val="heading 6"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="120" w:after="80"/><w:outlineLvl w:val="5"/></w:pPr><w:rPr><w:i/><w:sz w:val="22"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Subtle"><w:name w:val="Subtle"/><w:basedOn w:val="Normal"/><w:rPr><w:color w:val="808080"/><w:sz w:val="18"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="TableLabel"><w:name w:val="Table Label"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="40"/></w:pPr><w:rPr><w:b/><w:color w:val="3C3C3C"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListBullet"><w:name w:val="List Bullet"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="360" w:hanging="360"/></w:pPr></w:style>` +
	`<w:style w:type="table" w:default="1" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:left w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:right w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="CCCCCC"/></w:tblBorders></w:tblPr></w:style>` +
	`</w:styles>`
