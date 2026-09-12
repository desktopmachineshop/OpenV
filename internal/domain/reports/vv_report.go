package reports

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// GenerateVVReport builds a V&V status PDF for a project or baseline.
// latest is the latest test result per test case and runs the project's test
// runs; both are plain DTOs supplied by the caller so the report engine makes
// no service calls beyond the export/baseline sourcing used by all reports.
func (s *DefaultService) GenerateVVReport(projectID string, baselineID string, latest map[string]*vv.TestResult, runs []*vv.TestRun) ([]byte, string, error) {
	if projectID == "" {
		return nil, "", errors.New("project_id is required")
	}

	var data exports.ProjectExport
	var baselineName string

	if baselineID != "" && baselineID != "live" {
		// Scoped load: a baseline from another project is baselines.ErrNotFound,
		// so a foreign baseline ID cannot pull another project's snapshot into
		// this project's report.
		baseline, err := s.baselineService.GetProjectBaseline(projectID, baselineID)
		if err != nil {
			return nil, "", err
		}
		baselineName = baseline.Name
		if err := json.Unmarshal(baseline.Snapshot, &data); err != nil {
			return nil, "", fmt.Errorf("failed to parse baseline snapshot: %w", err)
		}
	} else {
		jsonData, _, err := s.exportService.ExportProject(projectID, exports.FormatJSON)
		if err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return nil, "", fmt.Errorf("failed to parse export data: %w", err)
		}
	}

	coverage := vv.ComputeCoverage(&data, latest)
	gaps := vv.GapAnalysis(&data, coverage)

	pdf, err := buildVVReportPDF(&data, baselineName, coverage, gaps, runs)
	if err != nil {
		return nil, "", err
	}

	filename := fmt.Sprintf("vv-report-%s-%s.pdf", sanitizeFilename(data.ProjectName), time.Now().Format("20060102"))
	return pdf, filename, nil
}

// truncateToWidth shortens text so it fits within width mm at the current font.
func truncateToWidth(pdf *gofpdf.Fpdf, text string, width float64) string {
	if pdf.GetStringWidth(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "..."
		if pdf.GetStringWidth(candidate) <= width {
			return candidate
		}
	}
	return "..."
}

func vvSectionHeader(pdf *gofpdf.Fpdf, tr func(string) string, title string) {
	ensureSpace(pdf, 14)
	pdf.SetFont("Go", "B", 13)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 8, tr(title), "", 1, "L", false, 0, "")
	pdf.Ln(1)
}

func buildVVReportPDF(
	data *exports.ProjectExport,
	baselineName string,
	coverage *vv.CoverageReport,
	gaps *vv.GapReport,
	runs []*vv.TestRun,
) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddUTF8FontFromBytes("Go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("Go", "B", gobold.TTF)
	pdf.AddUTF8FontFromBytes("Go", "I", goitalic.TTF)
	pdf.AddPage()

	// The Go fonts are embedded as UTF-8, so text needs no translation.
	tr := func(s string) string { return s }

	// Title header.
	pdf.SetFont("Go", "B", 16)
	pdf.CellFormat(0, 8, tr(fmt.Sprintf("V&V Status Report - %s", data.ProjectName)), "", 1, "C", false, 0, "")

	pdf.SetFont("Go", "", 9)
	pdf.SetTextColor(120, 120, 120)
	if baselineName != "" {
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("Baseline: %s", baselineName)), "", 1, "C", false, 0, "")
	}
	pdf.CellFormat(0, 5, fmt.Sprintf("Generated: %s", time.Now().Format("2006-01-02 15:04")), "", 1, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	// Resolve artifact IDs to titles for gap lists.
	sectionNumbers := artifacts.SectionNumbers(data.Artifacts)
	artifactTitles := map[string]string{}
	refs := map[string]string{}
	for _, artifact := range data.Artifacts {
		artifactTitles[artifact.ID] = qualifiedTitle(artifact, sectionNumbers)
		refs[artifact.ID] = artifact.Ref
	}

	// Summary block: counts per rollup.
	vvSectionHeader(pdf, tr, "Summary")
	pdf.SetFont("Go", "", 10)
	total := 0
	for _, count := range coverage.Summary {
		total += count
	}
	pdf.CellFormat(0, 5, tr(fmt.Sprintf("Requirements: %d", total)), "", 1, "L", false, 0, "")
	for _, rollup := range rollupDisplayOrder {
		count, ok := coverage.Summary[rollup]
		if !ok || count == 0 {
			continue
		}
		ensureSpace(pdf, 6)
		r, g, b := rollupColor(rollup)
		y := pdf.GetY()
		pdf.SetFillColor(r, g, b)
		pdf.Rect(15, y+1, 3, 3, "F")
		pdf.SetX(20)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("%s: %d", rollup, count)), "", 1, "L", false, 0, "")
	}
	pdf.Ln(4)

	// Coverage table.
	vvSectionHeader(pdf, tr, "Requirement Coverage")
	const (
		refColWidth    = 22.0
		titleColWidth  = 86.0
		methodColWidth = 34.0
		rollupColWidth = 38.0
		vvRowHeight    = 6.0
	)

	drawCoverageHeader := func() {
		pdf.SetFont("Go", "B", 9)
		pdf.SetFillColor(230, 230, 230)
		pdf.SetTextColor(60, 60, 60)
		pdf.SetDrawColor(200, 200, 200)
		pdf.CellFormat(refColWidth, vvRowHeight, "Ref", "1", 0, "L", true, 0, "")
		pdf.CellFormat(titleColWidth, vvRowHeight, "Requirement", "1", 0, "L", true, 0, "")
		pdf.CellFormat(methodColWidth, vvRowHeight, "Method", "1", 0, "L", true, 0, "")
		pdf.CellFormat(rollupColWidth, vvRowHeight, "Rollup", "1", 1, "L", true, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}

	drawCoverageHeader()
	if len(coverage.Entries) == 0 {
		pdf.SetFont("Go", "I", 9)
		pdf.CellFormat(refColWidth+titleColWidth+methodColWidth+rollupColWidth, vvRowHeight, "No requirements found.", "1", 1, "L", false, 0, "")
	}
	for _, entry := range coverage.Entries {
		pdf.SetFont("Go", "", 9)
		title := entry.Title
		lines := pdf.SplitText(title, titleColWidth-2)
		rowH := float64(len(lines)) * 4.6
		if rowH < vvRowHeight {
			rowH = vvRowHeight
		}
		_, pageH := pdf.GetPageSize()
		if pdf.GetY()+rowH > pageH-15 {
			pdf.AddPage()
			drawCoverageHeader()
		}
		method := entry.VerificationMethod
		if method == "" {
			method = "-"
		}
		y := pdf.GetY()
		pdf.SetDrawColor(200, 200, 200)
		x := 15.0
		for _, w := range []float64{refColWidth, titleColWidth, methodColWidth} {
			pdf.Rect(x, y, w, rowH, "D")
			x += w
		}
		pdf.SetXY(16, y+0.8)
		pdf.CellFormat(refColWidth-2, 4.6, refs[entry.RequirementID], "", 0, "L", false, 0, "")
		for i, line := range lines {
			pdf.SetXY(15+refColWidth+1, y+0.8+float64(i)*4.6)
			pdf.CellFormat(titleColWidth-2, 4.6, line, "", 0, "L", false, 0, "")
		}
		pdf.SetXY(15+refColWidth+titleColWidth+1, y+0.8)
		pdf.CellFormat(methodColWidth-2, 4.6, tr(method), "", 0, "L", false, 0, "")

		r, g, b := rollupColor(entry.Rollup)
		pdf.SetFillColor(r, g, b)
		pdf.Rect(15+refColWidth+titleColWidth+methodColWidth, y, rollupColWidth, rowH, "FD")
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Go", "B", 9)
		pdf.SetXY(15+refColWidth+titleColWidth+methodColWidth, y+0.8)
		pdf.CellFormat(rollupColWidth, 4.6, tr(entry.Rollup), "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetY(y + rowH)
	}
	pdf.Ln(6)

	// Gap analysis.
	vvSectionHeader(pdf, tr, "Gap Analysis")
	gapSections := []struct {
		label string
		ids   []string
	}{
		{"Requirements without a verification method", gaps.RequirementsWithoutMethod},
		{"Requirements without a test case", gaps.RequirementsWithoutTestCase},
		{"Unverified (demonstration, analysis, inspection)", gaps.RequirementsUnverified},
		{"Requirements with failing tests", gaps.RequirementsFailing},
		{"Orphan test cases (verify nothing)", gaps.OrphanTestCases},
		{"User needs without a derived requirement", gaps.NeedsWithoutRequirement},
		{"Unmitigated hazards", gaps.HazardsUnmitigated},
	}
	anyGaps := false
	for _, section := range gapSections {
		if len(section.ids) == 0 {
			continue
		}
		anyGaps = true
		ensureSpace(pdf, 10)
		pdf.SetFont("Go", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(0, 6, tr(fmt.Sprintf("%s (%d)", section.label, len(section.ids))), "", 1, "L", false, 0, "")
		pdf.SetFont("Go", "", 9)
		pdf.SetTextColor(0, 0, 0)
		for _, id := range section.ids {
			title := artifactTitles[id]
			if title == "" {
				title = id
			}
			ensureSpace(pdf, 5)
			pdf.SetX(19)
			pdf.MultiCell(0, 5, tr("- "+stripMarkdown(title)), "", "L", false)
		}
		pdf.Ln(2)
	}
	if !anyGaps {
		pdf.SetFont("Go", "I", 10)
		pdf.CellFormat(0, 6, "No gaps detected.", "", 1, "L", false, 0, "")
	}
	pdf.Ln(4)

	// Test runs appendix.
	vvSectionHeader(pdf, tr, "Test Runs")
	if len(runs) == 0 {
		pdf.SetFont("Go", "I", 10)
		pdf.CellFormat(0, 6, "No test runs recorded.", "", 1, "L", false, 0, "")
	}
	for _, run := range runs {
		if run == nil {
			continue
		}
		ensureSpace(pdf, 14)
		pdf.SetFont("Go", "B", 10)
		pdf.CellFormat(0, 6, tr(run.Name), "", 1, "L", false, 0, "")
		pdf.SetFont("Go", "", 9)
		pdf.SetTextColor(90, 90, 90)
		details := []string{
			fmt.Sprintf("Status: %s", run.Status),
			fmt.Sprintf("Started: %s", run.StartedAt.Format("2006-01-02 15:04")),
		}
		if run.CompletedAt != nil {
			details = append(details, fmt.Sprintf("Completed: %s", run.CompletedAt.Format("2006-01-02 15:04")))
		}
		pdf.SetX(19)
		pdf.CellFormat(0, 5, tr(strings.Join(details, "    ")), "", 1, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.Ln(1)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ensureSpace starts a new page when fewer than needed millimetres remain.
func ensureSpace(pdf *gofpdf.Fpdf, needed float64) {
	_, pageH := pdf.GetPageSize()
	if pdf.GetY()+needed > pageH-15 {
		pdf.AddPage()
	}
}
