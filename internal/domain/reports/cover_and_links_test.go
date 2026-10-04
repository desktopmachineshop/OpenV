package reports

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/reports/doc"
)

// foreignID is a requirement of another project, named by its id alone in
// the links and described by the export's linked_artifacts.
const foreignID = "7c0f3a52-0000-4000-8000-000000000121"

// refinedFixture is a project whose requirement another project's
// requirement refines (REQ-145).
func refinedFixture() *exports.ProjectExport {
	return &exports.ProjectExport{
		ProjectName: "Parent",
		Artifacts: []*artifacts.Artifact{
			{ID: "req1", Type: artifacts.TypeRequirement, Ref: "REQ-1", Title: "Answer in time", Version: 1},
		},
		Links: []*linksdomain.Link{
			{ID: "l1", FromID: foreignID, ToID: "req1", Type: linksdomain.TypeRefines},
		},
		LinkedArtifacts: []*exports.LinkedArtifact{
			{ID: foreignID, ProjectID: "child", ProjectName: "Supplier", Ref: "REQ-7", Type: artifacts.TypeRequirement,
				Title: "Cache hits", Status: artifacts.StatusApproved},
		},
	}
}

// TestTraceabilityNamesARequirementOfAnotherProject: a requirement refined
// from another project names the refining requirement by its project, ref
// and title in the PDF and Word traceability tables, as the export's
// linked_artifacts describe it, not by its raw id (#379 bug 67).
func TestTraceabilityNamesARequirementOfAnotherProject(t *testing.T) {
	const want = "Supplier / REQ-7 Cache hits"
	data := refinedFixture()
	m := buildReportModel(data, defaultRenderOptions(Snapshot{}))
	rows := m.links["req1"]
	if len(rows) != 1 {
		t.Fatalf("traceability rows of REQ-1 = %+v, want the one refinement", rows)
	}
	if rows[0].TargetTitle != want || rows[0].Relationship != "refined by" {
		t.Errorf("REQ-1's row = %q %q, want \"refined by\" %q", rows[0].Relationship, rows[0].TargetTitle, want)
	}

	out, err := buildReportDOCX(data, defaultRenderOptions(Snapshot{}))
	if err != nil {
		t.Fatalf("docx: %v", err)
	}
	text := docxText(readZipPart(t, out, "word/document.xml"))
	if !strings.Contains(text, want) {
		t.Errorf("the Word traceability table does not name %q", want)
	}
	if strings.Contains(text, foreignID) {
		t.Errorf("the Word document shows the raw id %s", foreignID)
	}
}

// coverDescription is a project description with text in angle brackets,
// an ampersand, Markdown markup and two paragraphs, the second of two
// lines.
const coverDescription = "Covers the <pump> & its <valve>, **as typed**.\n\nLine one\nline two"

// TestCoverShowsTheDescriptionAsTyped: the PDF and Word covers show a
// project's description as the app shows it, as typed, so text in angle
// brackets is kept rather than read as Markdown's raw HTML and dropped
// (#379 bug 68).
func TestCoverShowsTheDescriptionAsTyped(t *testing.T) {
	want := []string{"Covers the <pump> & its <valve>, **as typed**.", "Line one\nline two"}
	var got []string
	for _, b := range plainBlocks(coverDescription) {
		p, ok := b.(doc.Paragraph)
		if !ok {
			t.Fatalf("the cover's description holds a %T, want paragraphs only", b)
		}
		got = append(got, doc.InlineText(p.Inlines))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the cover's description reads %q, want it as typed: %q", got, want)
	}

	data := &exports.ProjectExport{ProjectName: "Pumps", ProjectDesc: coverDescription}
	out, err := buildReportDOCX(data, defaultRenderOptions(Snapshot{}))
	if err != nil {
		t.Fatalf("docx: %v", err)
	}
	text := docxText(readZipPart(t, out, "word/document.xml"))
	for _, want := range []string{"Covers the <pump> & its <valve>, **as typed**.", "Line one", "line two"} {
		if !strings.Contains(text, want) {
			t.Errorf("the Word cover does not show %q", want)
		}
	}
}
