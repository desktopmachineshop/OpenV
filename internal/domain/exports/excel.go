package exports

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// The Excel export is the CSV a reader can actually work in.
//
// A CSV is one flat table, so a project full of requirements, test cases and
// hazards arrives as a single sheet the reader has to sort and filter by hand.
// The workbook produced here says the same thing in the shape a spreadsheet is
// good at: a sheet per artifact type, the traceability links on their own
// sheet, and a cover sheet naming the project and the snapshot it came from.
//
// It renders from the same narrowed snapshot as every other download (see the
// downloads package), so "the requirements sections, no headings" means here
// exactly what it means in the PDF, and an artifact's shared columns carry
// exactly what the CSV export carries — they are literally the CSV's row
// (csvRow in export.go), written after the ref and section columns.
//
// The section column is where the row sits in the document: a heading's own
// number, and for everything else the number of the heading it lives under, so
// a requirement on a flat sheet can still be placed in the specification.

const (
	// excelSheetNameLimit is Excel's hard limit on a sheet name.
	excelSheetNameLimit = 31
	// excelCellLimit is Excel's hard limit on the characters in one cell.
	excelCellLimit = 32767
	// Column widths are derived from content and then clamped, so a body
	// column stays readable instead of running off the screen.
	excelMinColWidth = 10.0
	excelMaxColWidth = 60.0
)

// The two fixed sheets: the cover, and the traceability table. Everything
// between them is one artifact type.
const (
	excelSheetProject = "Project"
	excelSheetLinks   = "Links"
)

// excelArtifactHeader is an artifact sheet's column layout: the CSV export's
// columns, with the stable ref and the derived section number in front — the
// two things a reader navigates a specification by. A heading's section number
// is its own; every other row carries the section it sits in.
var excelArtifactHeader = append([]string{"ref", "section"}, csvHeader...)

// excelLinkHeader is the traceability sheet's column layout. Endpoints are
// shown by ref and title, because a link is only reviewable when you can see
// what sits at both ends of it.
var excelLinkHeader = []string{"from_ref", "from_title", "type", "to_ref", "to_title", "suspect"}

// excelTypeSheetNames names the sheet each catalog type gets. Plural, because
// a sheet holds many of them.
var excelTypeSheetNames = map[string]string{
	artifacts.TypeHeading:     "Headings",
	artifacts.TypeDescription: "Descriptions",
	artifacts.TypePersona:     "Personas",
	artifacts.TypeUserNeed:    "User Needs",
	artifacts.TypeRequirement: "Requirements",
	artifacts.TypeDesignItem:  "Design Items",
	artifacts.TypeTestCase:    "Test Cases",
	artifacts.TypeHazard:      "Hazards",
	artifacts.TypeOther:       "Other",
}

// excelStyles are the few cell styles a workbook uses, created once per file.
type excelStyles struct {
	header int
	wrap   int
}

// exportExcel renders the snapshot as an .xlsx workbook.
func (s *DefaultService) exportExcel(data *ProjectExport) ([]byte, string, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	styles, err := newExcelStyles(f)
	if err != nil {
		return nil, "", err
	}

	// NewFile starts with one sheet; the cover takes its place so the workbook
	// opens on the project rather than on an empty grid.
	if err := f.SetSheetName(f.GetSheetName(0), excelSheetProject); err != nil {
		return nil, "", fmt.Errorf("failed to name the project sheet: %w", err)
	}
	if err := writeExcelProjectSheet(f, data, styles); err != nil {
		return nil, "", err
	}
	if err := writeExcelArtifactSheets(f, data, styles); err != nil {
		return nil, "", err
	}
	if err := writeExcelLinksSheet(f, data, styles); err != nil {
		return nil, "", err
	}

	f.SetActiveSheet(0)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", fmt.Errorf("failed to write Excel workbook: %w", err)
	}
	return buf.Bytes(), exportFilename(data.ProjectName, "xlsx"), nil
}

// newExcelStyles creates the workbook's styles: a bold header row, and a
// wrapped, top-aligned style for the columns that hold prose.
func newExcelStyles(f *excelize.File) (excelStyles, error) {
	header, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return excelStyles{}, fmt.Errorf("failed to create the header style: %w", err)
	}
	wrap, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"},
	})
	if err != nil {
		return excelStyles{}, fmt.Errorf("failed to create the body style: %w", err)
	}
	return excelStyles{header: header, wrap: wrap}, nil
}

// writeExcelProjectSheet writes the cover: what this file is and what it was
// taken from. It is a label/value list rather than a table — there is nothing
// here to filter — so it carries no header row, and the labels are the bold
// column instead.
func writeExcelProjectSheet(f *excelize.File, data *ProjectExport, styles excelStyles) error {
	rows := [][2]string{
		{"Project", data.ProjectName},
		{"Description", data.ProjectDesc},
		{"Exported at", data.ExportedAt.UTC().Format(time.RFC3339)},
	}
	if data.BaselineName != "" {
		rows = append(rows, [2]string{"Baseline", data.BaselineName})
	}
	rows = append(rows,
		[2]string{"Artifacts", strconv.Itoa(countExcelArtifacts(data))},
		[2]string{"Links", strconv.Itoa(countExcelLinks(data))},
	)

	labelWidth, valueWidth := excelMinColWidth, excelMinColWidth
	for i, row := range rows {
		label, value := excelCellText(row[0]), excelCellText(row[1])
		if err := f.SetCellStr(excelSheetProject, fmt.Sprintf("A%d", i+1), label); err != nil {
			return fmt.Errorf("failed to write the project sheet: %w", err)
		}
		if err := f.SetCellStr(excelSheetProject, fmt.Sprintf("B%d", i+1), value); err != nil {
			return fmt.Errorf("failed to write the project sheet: %w", err)
		}
		labelWidth = max(labelWidth, excelWidth(label))
		valueWidth = max(valueWidth, excelWidth(value))
	}

	if err := f.SetCellStyle(excelSheetProject, "A1", fmt.Sprintf("A%d", len(rows)), styles.header); err != nil {
		return fmt.Errorf("failed to style the project sheet: %w", err)
	}
	if err := f.SetCellStyle(excelSheetProject, "B1", fmt.Sprintf("B%d", len(rows)), styles.wrap); err != nil {
		return fmt.Errorf("failed to style the project sheet: %w", err)
	}
	if err := f.SetColWidth(excelSheetProject, "A", "A", labelWidth); err != nil {
		return fmt.Errorf("failed to size the project sheet: %w", err)
	}
	return f.SetColWidth(excelSheetProject, "B", "B", valueWidth)
}

// writeExcelArtifactSheets writes one sheet per artifact type present, in the
// catalog's order so a workbook always reads needs, then requirements, then
// tests; a type outside the catalog follows them alphabetically.
func writeExcelArtifactSheets(f *excelize.File, data *ProjectExport, styles excelStyles) error {
	byType := map[string][]*artifacts.Artifact{}
	var order []string
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		if _, seen := byType[a.Type]; !seen {
			order = append(order, a.Type)
		}
		byType[a.Type] = append(byType[a.Type], a)
	}
	sortExcelTypes(order)

	sections := excelSections(data.Artifacts)
	linkColumn := linksByFrom(data.Links)
	used := map[string]bool{
		strings.ToLower(excelSheetProject): true,
		strings.ToLower(excelSheetLinks):   true,
	}

	for _, artifactType := range order {
		name := excelSheetName(excelTypeSheetName(artifactType), used)
		if _, err := f.NewSheet(name); err != nil {
			return fmt.Errorf("failed to create sheet %q: %w", name, err)
		}
		rows := make([][]string, 0, len(byType[artifactType]))
		for _, a := range byType[artifactType] {
			rows = append(rows, excelArtifactRow(a, sections, linkColumn))
		}
		table := excelTable{
			header: excelArtifactHeader,
			rows:   rows,
			// Only the version is a number. Refs and section numbers are
			// addresses, and a number would misread them ("1.10" is not 1.1).
			numeric:    map[int]bool{excelColumn(excelArtifactHeader, "version"): true},
			wrap:       map[int]bool{excelColumn(excelArtifactHeader, "body"): true},
			autoFilter: true,
		}
		if err := writeExcelTable(f, name, table, styles); err != nil {
			return err
		}
	}
	return nil
}

// writeExcelLinksSheet writes the traceability table. A link whose endpoint is
// not in this download still gets a row, showing the raw id, so nothing is
// silently dropped.
func writeExcelLinksSheet(f *excelize.File, data *ProjectExport, styles excelStyles) error {
	if _, err := f.NewSheet(excelSheetLinks); err != nil {
		return fmt.Errorf("failed to create the links sheet: %w", err)
	}

	byID := map[string]*artifacts.Artifact{}
	for _, a := range data.Artifacts {
		if a != nil {
			byID[a.ID] = a
		}
	}
	ref := func(id string) string {
		if a := byID[id]; a != nil && a.Ref != "" {
			return a.Ref
		}
		return id
	}
	title := func(id string) string {
		if a := byID[id]; a != nil {
			return a.Title
		}
		return ""
	}

	rows := make([][]string, 0, len(data.Links))
	for _, link := range data.Links {
		if link == nil {
			continue
		}
		suspect := "no"
		if link.Suspect {
			suspect = "yes"
		}
		rows = append(rows, []string{
			ref(link.FromID), title(link.FromID), link.Type,
			ref(link.ToID), title(link.ToID), suspect,
		})
	}

	return writeExcelTable(f, excelSheetLinks, excelTable{
		header:     excelLinkHeader,
		rows:       rows,
		autoFilter: true,
	}, styles)
}

// excelArtifactRow is one artifact as a sheet row, in excelArtifactHeader's
// order: the stable ref and the section number it sits in, then the CSV
// export's own row (export.go), unchanged.
func excelArtifactRow(a *artifacts.Artifact, sections map[string]string, linkColumn map[string][]string) []string {
	return append([]string{a.Ref, sections[a.ID]}, csvRow(a, linkColumn)...)
}

// excelSections is the section number each artifact's row shows.
//
// artifacts.SectionNumbers numbers headings only — a leaf is cited by its ref,
// not by a clause number that churns on every edit. A flat sheet has no
// hierarchy to show that with, though, so a requirement whose section column
// were left empty would arrive unplaceable: sorting or filtering the sheet
// tells the reader nothing about where in the specification the row belongs.
// So a non-heading is filed under its nearest heading ancestor's number, which
// is the same section the PDF nests it inside; an artifact with no heading
// above it has no number, and its column stays empty.
func excelSections(list []*artifacts.Artifact) map[string]string {
	numbers := artifacts.SectionNumbers(list)

	byID := make(map[string]*artifacts.Artifact, len(list))
	for _, a := range list {
		if a != nil {
			byID[a.ID] = a
		}
	}

	sections := make(map[string]string, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		if number, ok := numbers[a.ID]; ok {
			sections[a.ID] = number
			continue
		}
		// Walk up the parent chain to the first numbered heading. seen stops a
		// parent cycle, which SectionNumbers tolerates and so must this.
		seen := map[string]bool{a.ID: true}
		for node := a; node.ParentID != nil && *node.ParentID != "" && !seen[*node.ParentID]; {
			seen[*node.ParentID] = true
			parent := byID[*node.ParentID]
			if parent == nil {
				break
			}
			if number, ok := numbers[parent.ID]; ok {
				sections[a.ID] = number
				break
			}
			node = parent
		}
	}
	return sections
}

// excelTable is one sheet's worth of data: a header, its rows, and which
// columns are numbers and which hold prose.
type excelTable struct {
	header     []string
	rows       [][]string
	numeric    map[int]bool
	wrap       map[int]bool
	autoFilter bool
}

// writeExcelTable writes a table into a sheet and finishes it the way a reader
// expects a spreadsheet to arrive: bold header, header row frozen, an
// autofilter across it, and columns wide enough to read but not absurd.
func writeExcelTable(f *excelize.File, sheet string, table excelTable, styles excelStyles) error {
	widths := make([]float64, len(table.header))
	for i, h := range table.header {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return err
		}
		if err := f.SetCellStr(sheet, cell, h); err != nil {
			return fmt.Errorf("failed to write the %s header: %w", sheet, err)
		}
		widths[i] = excelWidth(h)
	}

	for r, row := range table.rows {
		for i := range table.header {
			value := ""
			if i < len(row) {
				value = excelCellText(row[i])
			}
			if value == "" {
				continue
			}
			cell, err := excelize.CoordinatesToCellName(i+1, r+2)
			if err != nil {
				return err
			}
			written := false
			if table.numeric[i] {
				if n, err := strconv.ParseFloat(value, 64); err == nil {
					if err := f.SetCellFloat(sheet, cell, n, -1, 64); err != nil {
						return fmt.Errorf("failed to write %s!%s: %w", sheet, cell, err)
					}
					written = true
				}
			}
			// Everything else is written as text on purpose: a ref, a section
			// number and a date must survive as themselves rather than be
			// coerced into numbers or Excel serial dates.
			if !written {
				if err := f.SetCellStr(sheet, cell, value); err != nil {
					return fmt.Errorf("failed to write %s!%s: %w", sheet, cell, err)
				}
			}
			widths[i] = max(widths[i], excelWidth(value))
		}
	}

	return finishExcelSheet(f, sheet, table, widths, styles)
}

// finishExcelSheet applies the presentation a finished sheet carries: styles,
// widths, the frozen header and its filter.
func finishExcelSheet(f *excelize.File, sheet string, table excelTable, widths []float64, styles excelStyles) error {
	lastCol, err := excelize.ColumnNumberToName(len(table.header))
	if err != nil {
		return err
	}

	if err := f.SetCellStyle(sheet, "A1", lastCol+"1", styles.header); err != nil {
		return fmt.Errorf("failed to style the %s header: %w", sheet, err)
	}

	for i := range table.header {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		if err := f.SetColWidth(sheet, col, col, widths[i]); err != nil {
			return fmt.Errorf("failed to size %s!%s: %w", sheet, col, err)
		}
		if table.wrap[i] && len(table.rows) > 0 {
			last := fmt.Sprintf("%s%d", col, len(table.rows)+1)
			if err := f.SetCellStyle(sheet, col+"2", last, styles.wrap); err != nil {
				return fmt.Errorf("failed to style %s!%s: %w", sheet, col, err)
			}
		}
	}

	if err := f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
		Selection:   []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}},
	}); err != nil {
		return fmt.Errorf("failed to freeze the %s header: %w", sheet, err)
	}

	if table.autoFilter {
		rangeRef := fmt.Sprintf("A1:%s%d", lastCol, len(table.rows)+1)
		if err := f.AutoFilter(sheet, rangeRef, nil); err != nil {
			return fmt.Errorf("failed to filter %s: %w", sheet, err)
		}
	}
	return nil
}

// excelCellText makes a value safe to store: control characters XML cannot
// carry are dropped (tabs and newlines survive, so a multi-line body stays
// multi-line), and an over-long value is truncated rather than rejected.
func excelCellText(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return truncateRunes(b.String(), excelCellLimit)
}

// excelWidth is the column width one value asks for, clamped to something a
// reader can live with. The longest line decides it, so a wrapped body column
// is sized by a line rather than by the whole paragraph.
func excelWidth(value string) float64 {
	longest := 0
	for _, line := range strings.Split(value, "\n") {
		if n := len([]rune(line)); n > longest {
			longest = n
		}
	}
	width := float64(longest) + 2
	if width < excelMinColWidth {
		return excelMinColWidth
	}
	if width > excelMaxColWidth {
		return excelMaxColWidth
	}
	return width
}

// excelColumn is the index of a named column in a header.
func excelColumn(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

// excelTypeSheetName is what a type's sheet is called: the catalog's label,
// pluralized, with a type outside the catalog falling back to its own value.
func excelTypeSheetName(artifactType string) string {
	if name, ok := excelTypeSheetNames[artifactType]; ok {
		return name
	}
	words := strings.FieldsFunc(artifactType, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, w := range words {
		// The first rune, not the first byte: a type like "évaluation"
		// would otherwise be cut mid-rune into an invalid UTF-8 sheet name.
		r, size := utf8.DecodeRuneInString(w)
		if size == 0 {
			continue
		}
		words[i] = string(unicode.ToUpper(r)) + w[size:]
	}
	name := strings.Join(words, " ")
	if name == "" {
		return "Untyped"
	}
	if !strings.HasSuffix(strings.ToLower(name), "s") {
		name += "s"
	}
	return name
}

// excelSheetName makes a name Excel accepts: none of []:*?/\, no more than 31
// characters, and unique within the workbook.
func excelSheetName(base string, used map[string]bool) string {
	replacer := strings.NewReplacer("[", "(", "]", ")", ":", "-", "*", "-", "?", "", "/", "-", "\\", "-")
	name := strings.TrimSpace(replacer.Replace(excelCellText(base)))
	name = strings.Trim(name, "'")
	if name == "" {
		name = "Sheet"
	}
	name = truncateRunes(name, excelSheetNameLimit)

	candidate := name
	for i := 2; used[strings.ToLower(candidate)]; i++ {
		suffix := fmt.Sprintf(" %d", i)
		candidate = truncateRunes(name, excelSheetNameLimit-len(suffix)) + suffix
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit]))
}

// sortExcelTypes orders artifact types the way the catalog does, with types
// outside the catalog after it in alphabetical order.
func sortExcelTypes(types []string) {
	rank := map[string]int{}
	for i, td := range artifacts.TypeCatalog() {
		rank[td.Value] = i
	}
	sort.SliceStable(types, func(i, j int) bool {
		ri, iKnown := rank[types[i]]
		rj, jKnown := rank[types[j]]
		switch {
		case iKnown && jKnown:
			return ri < rj
		case iKnown != jKnown:
			return iKnown
		default:
			return types[i] < types[j]
		}
	})
}

func countExcelArtifacts(data *ProjectExport) int {
	n := 0
	for _, a := range data.Artifacts {
		if a != nil {
			n++
		}
	}
	return n
}

func countExcelLinks(data *ProjectExport) int {
	n := 0
	for _, link := range data.Links {
		if link != nil {
			n++
		}
	}
	return n
}
