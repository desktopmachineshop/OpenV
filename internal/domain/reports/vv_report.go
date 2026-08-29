package reports

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/phpdave11/gofpdf"

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

// rollupDisplayOrder controls the summary/legend ordering of rollup states.
var rollupDisplayOrder = []string{
	vv.RollupPass,
	vv.RollupFail,
	vv.RollupBlocked,
	vv.RollupUnrun,
	vv.RollupUncovered,
	vv.RollupVerifiedManually,
	vv.RollupMethodMissing,
}

// rollupColor returns the fill color for a rollup cell.
// pass #27ae60, fail #e74c3c, blocked #f39c12, everything else gray.
func rollupColor(rollup string) (r, g, b int) {
	switch rollup {
	case vv.RollupPass:
		return 39, 174, 96
	case vv.RollupFail:
		return 231, 76, 60
	case vv.RollupBlocked:
		return 243, 156, 18
	default:
		return 150, 150, 150
	}
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
	pdf.SetFont("Arial", "B", 13)
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
	pdf.AddPage()

	tr := pdf.UnicodeTranslatorFromDescriptor("")

	// Title header.
	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, 8, tr(fmt.Sprintf("V&V Status Report - %s", data.ProjectName)), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(120, 120, 120)
	if baselineName != "" {
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("Baseline: %s", baselineName)), "", 1, "C", false, 0, "")
	}
	pdf.CellFormat(0, 5, fmt.Sprintf("Generated: %s", time.Now().Format("2006-01-02 15:04")), "", 1, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	// Resolve artifact IDs to titles for gap lists.
	artifactTitles := map[string]string{}
	for _, artifact := range data.Artifacts {
		artifactTitles[artifact.ID] = artifact.Title
	}

	// Summary block: counts per rollup.
	vvSectionHeader(pdf, tr, "Summary")
	pdf.SetFont("Arial", "", 10)
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
		titleColWidth  = 100.0
		methodColWidth = 35.0
		rollupColWidth = 35.0
		vvRowHeight    = 6.0
	)

	drawCoverageHeader := func() {
		pdf.SetFont("Arial", "B", 9)
		pdf.SetFillColor(230, 230, 230)
		pdf.SetTextColor(60, 60, 60)
		pdf.SetDrawColor(200, 200, 200)
		pdf.CellFormat(titleColWidth, vvRowHeight, "Requirement", "1", 0, "L", true, 0, "")
		pdf.CellFormat(methodColWidth, vvRowHeight, "Method", "1", 0, "L", true, 0, "")
		pdf.CellFormat(rollupColWidth, vvRowHeight, "Rollup", "1", 1, "L", true, 0, "")
		pdf.SetTextColor(0, 0, 0)
	}

	drawCoverageHeader()
	if len(coverage.Entries) == 0 {
		pdf.SetFont("Arial", "I", 9)
		pdf.CellFormat(titleColWidth+methodColWidth+rollupColWidth, vvRowHeight, "No requirements found.", "1", 1, "L", false, 0, "")
	}
	for _, entry := range coverage.Entries {
		_, pageH := pdf.GetPageSize()
		if pdf.GetY()+vvRowHeight > pageH-15 {
			pdf.AddPage()
			drawCoverageHeader()
		}

		pdf.SetFont("Arial", "", 9)
		title := tr(stripMarkdown(entry.Title))
		title = truncateToWidth(pdf, title, titleColWidth-4)
		method := entry.VerificationMethod
		if method == "" {
			method = "-"
		}

		pdf.SetDrawColor(200, 200, 200)
		pdf.CellFormat(titleColWidth, vvRowHeight, title, "1", 0, "L", false, 0, "")
		pdf.CellFormat(methodColWidth, vvRowHeight, tr(method), "1", 0, "L", false, 0, "")

		r, g, b := rollupColor(entry.Rollup)
		pdf.SetFillColor(r, g, b)
		pdf.SetTextColor(255, 255, 255)
		pdf.SetFont("Arial", "B", 9)
		pdf.CellFormat(rollupColWidth, vvRowHeight, tr(entry.Rollup), "1", 1, "C", true, 0, "")
		pdf.SetTextColor(0, 0, 0)
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
		pdf.SetFont("Arial", "B", 10)
		pdf.SetTextColor(60, 60, 60)
		pdf.CellFormat(0, 6, tr(fmt.Sprintf("%s (%d)", section.label, len(section.ids))), "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 9)
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
		pdf.SetFont("Arial", "I", 10)
		pdf.CellFormat(0, 6, "No gaps detected.", "", 1, "L", false, 0, "")
	}
	pdf.Ln(4)

	// Test runs appendix.
	vvSectionHeader(pdf, tr, "Test Runs")
	if len(runs) == 0 {
		pdf.SetFont("Arial", "I", 10)
		pdf.CellFormat(0, 6, "No test runs recorded.", "", 1, "L", false, 0, "")
	}
	for _, run := range runs {
		if run == nil {
			continue
		}
		ensureSpace(pdf, 14)
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(0, 6, tr(run.Name), "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 9)
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
