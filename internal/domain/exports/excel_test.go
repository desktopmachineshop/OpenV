package exports

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// excelFixture is a small project with two artifact types, a heading to number,
// a multi-line body and a suspect link — enough to exercise every sheet.
func excelFixture() ([]*artifacts.Artifact, []*links.Link) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	list := []*artifacts.Artifact{
		{
			ID: "h1", Ref: "SEC-1", Type: artifacts.TypeHeading, Title: "Requirements",
			Version: 1, CreatedAt: created, UpdatedAt: updated,
		},
		{
			ID: "a1", Ref: "REQ-1", Type: artifacts.TypeRequirement, ParentID: strPtr("h1"),
			Title:  "The pump shall stop on overheat",
			Body:   "Line one\nLine two",
			Status: "approved",
			// A stale attribute mirror must lose to the real column.
			Attributes: map[string]interface{}{"status": "draft"},
			Version:    3, CreatedAt: created, UpdatedAt: updated,
		},
		{
			ID: "t1", Ref: "TC-1", Type: artifacts.TypeTestCase, ParentID: strPtr("h1"),
			Title: "Verify overheat cutoff", Version: 1, CreatedAt: created, UpdatedAt: updated,
		},
	}
	linkList := []*links.Link{
		{ID: "l1", FromID: "t1", ToID: "a1", Type: "verifies", Suspect: true},
	}
	return list, linkList
}

func openExcel(t *testing.T, data []byte) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("exported workbook does not open: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func cell(t *testing.T, f *excelize.File, sheet, ref string) string {
	t.Helper()
	value, err := f.GetCellValue(sheet, ref)
	if err != nil {
		t.Fatalf("reading %s!%s: %v", sheet, ref, err)
	}
	return value
}

func TestExportProjectExcel(t *testing.T) {
	artifactList, linkList := excelFixture()
	svc := newTestService("Cooling System", artifactList, linkList)

	data, filename, err := svc.ExportProject("p1", FormatExcel)
	if err != nil {
		t.Fatalf("ExportProject(excel) returned error: %v", err)
	}
	if !strings.HasPrefix(filename, "project_Cooling System_") || !strings.HasSuffix(filename, ".xlsx") {
		t.Errorf("unexpected filename: %q", filename)
	}

	f := openExcel(t, data)

	// One sheet per type present, in catalog order, between the cover and the
	// traceability table.
	want := []string{"Project", "Headings", "Requirements", "Test Cases", "Links"}
	if got := f.GetSheetList(); len(got) != len(want) {
		t.Fatalf("sheets = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("sheets = %v, want %v", got, want)
			}
		}
	}

	// The cover names the project and counts what is in the file.
	if got := cell(t, f, "Project", "B1"); got != "Cooling System" {
		t.Errorf("Project!B1 = %q, want the project name", got)
	}
	if got := cell(t, f, "Project", "B4"); got != "3" {
		t.Errorf("artifact count = %q, want 3", got)
	}
	if got := cell(t, f, "Project", "B5"); got != "1" {
		t.Errorf("link count = %q, want 1", got)
	}

	// The header row is the CSV's columns with ref and section in front.
	header, err := f.GetRows("Requirements")
	if err != nil {
		t.Fatalf("reading the Requirements sheet: %v", err)
	}
	if len(header) != 2 {
		t.Fatalf("Requirements rows = %d, want header + 1 artifact", len(header))
	}
	for i, want := range excelArtifactHeader {
		if header[0][i] != want {
			t.Errorf("header column %d = %q, want %q", i, header[0][i], want)
		}
	}

	// One artifact, cell by cell.
	row := header[1]
	for i, want := range []string{
		"REQ-1", "", "a1", "requirement", "The pump shall stop on overheat",
		"Line one\nLine two", "approved", "3", "h1", "",
		"2026-01-02T03:04:05Z", "2026-02-03T04:05:06Z",
	} {
		if row[i] != want {
			t.Errorf("%s = %q, want %q", excelArtifactHeader[i], row[i], want)
		}
	}

	// The heading carries its derived section number, as text.
	if got := cell(t, f, "Headings", "B2"); got != "1" {
		t.Errorf("heading section number = %q, want 1", got)
	}

	// The traceability sheet names both ends and flags the suspect link.
	linkRows, err := f.GetRows("Links")
	if err != nil {
		t.Fatalf("reading the Links sheet: %v", err)
	}
	if len(linkRows) != 2 {
		t.Fatalf("Links rows = %d, want header + 1 link", len(linkRows))
	}
	for i, want := range excelLinkHeader {
		if linkRows[0][i] != want {
			t.Errorf("links header column %d = %q, want %q", i, linkRows[0][i], want)
		}
	}
	wantLink := []string{"TC-1", "Verify overheat cutoff", "verifies", "REQ-1", "The pump shall stop on overheat", "yes"}
	for i, want := range wantLink {
		if linkRows[1][i] != want {
			t.Errorf("links %s = %q, want %q", excelLinkHeader[i], linkRows[1][i], want)
		}
	}

	// The artifact's own links column keeps the CSV's "type:targetId" form.
	tests, err := f.GetRows("Test Cases")
	if err != nil {
		t.Fatalf("reading the Test Cases sheet: %v", err)
	}
	if got := tests[1][excelColumn(excelArtifactHeader, "links")]; got != "verifies:a1" {
		t.Errorf("test case links column = %q, want verifies:a1", got)
	}
}

func TestExportExcelSheetPresentation(t *testing.T) {
	artifactList, linkList := excelFixture()
	svc := newTestService("Cooling System", artifactList, linkList)

	data, _, err := svc.ExportProject("p1", FormatExcel)
	if err != nil {
		t.Fatalf("ExportProject(excel) returned error: %v", err)
	}
	f := openExcel(t, data)

	// The header row is bold.
	styleID, err := f.GetCellStyle("Requirements", "A1")
	if err != nil {
		t.Fatalf("reading the header style: %v", err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("reading style %d: %v", styleID, err)
	}
	if style.Font == nil || !style.Font.Bold {
		t.Errorf("header style is not bold: %+v", style.Font)
	}

	// It is frozen and filterable, so a long sheet stays navigable.
	panes, err := f.GetPanes("Requirements")
	if err != nil {
		t.Fatalf("reading panes: %v", err)
	}
	if !panes.Freeze || panes.YSplit != 1 {
		t.Errorf("panes = %+v, want the header row frozen", panes)
	}

	// Column widths are derived from content but capped.
	body, err := f.GetColWidth("Requirements", "F")
	if err != nil {
		t.Fatalf("reading the body column width: %v", err)
	}
	if body < excelMinColWidth || body > excelMaxColWidth {
		t.Errorf("body column width = %v, want between %v and %v", body, excelMinColWidth, excelMaxColWidth)
	}
}

func TestExportExcelEmptyProject(t *testing.T) {
	svc := newTestService("Empty", nil, nil)

	data, _, err := svc.ExportProject("p1", FormatExcel)
	if err != nil {
		t.Fatalf("ExportProject(excel) returned error: %v", err)
	}
	f := openExcel(t, data)

	// No artifacts means no type sheets, but the workbook is still a workbook.
	if got := f.GetSheetList(); len(got) != 2 || got[0] != "Project" || got[1] != "Links" {
		t.Fatalf("sheets = %v, want [Project Links]", got)
	}
	if got := cell(t, f, "Project", "B4"); got != "0" {
		t.Errorf("artifact count = %q, want 0", got)
	}
	if got := cell(t, f, "Links", "A1"); got != "from_ref" {
		t.Errorf("Links!A1 = %q, want the header row", got)
	}
}

func TestExportExcelBaselineNameOnCover(t *testing.T) {
	svc := newTestService("Cooling System", nil, nil)
	data, err := svc.PrepareExport("p1", false)
	if err != nil {
		t.Fatalf("PrepareExport returned error: %v", err)
	}
	data.BaselineName = "Release 1.0"

	body, _, err := svc.RenderExport(data, FormatExcel)
	if err != nil {
		t.Fatalf("RenderExport(excel) returned error: %v", err)
	}
	f := openExcel(t, body)
	if got := cell(t, f, "Project", "A4"); got != "Baseline" {
		t.Errorf("Project!A4 = %q, want Baseline", got)
	}
	if got := cell(t, f, "Project", "B4"); got != "Release 1.0" {
		t.Errorf("Project!B4 = %q, want the baseline name", got)
	}
}

func TestExcelSheetNameRules(t *testing.T) {
	used := map[string]bool{}
	long := strings.Repeat("Requirement", 5)

	for _, tc := range []struct {
		in   string
		want string
	}{
		{"Requirements", "Requirements"},
		{"Requirements", "Requirements 2"},
		{"Odd [type]: a/b\\c*d?e", "Odd (type)- a-b-c-de"},
		{long, long[:31]},
		{"", "Sheet"},
	} {
		got := excelSheetName(tc.in, used)
		if got != tc.want {
			t.Errorf("excelSheetName(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if len([]rune(got)) > excelSheetNameLimit {
			t.Errorf("excelSheetName(%q) = %q, longer than %d characters", tc.in, got, excelSheetNameLimit)
		}
		if strings.ContainsAny(got, `[]:*?/\`) {
			t.Errorf("excelSheetName(%q) = %q, still contains a forbidden character", tc.in, got)
		}
	}
}

func TestExcelTypeSheetNameFallsBackToTheType(t *testing.T) {
	if got := excelTypeSheetName(artifacts.TypeUserNeed); got != "User Needs" {
		t.Errorf("user-need sheet = %q, want User Needs", got)
	}
	if got := excelTypeSheetName("risk-control"); got != "Risk Controls" {
		t.Errorf("unknown type sheet = %q, want Risk Controls", got)
	}
	if got := excelTypeSheetName(""); got != "Untyped" {
		t.Errorf("empty type sheet = %q, want Untyped", got)
	}
}
