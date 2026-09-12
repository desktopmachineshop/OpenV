package reports

// The rendered-document fidelity suite (TC-66 in the OpenV Platform project).
//
// One fixture project carries every markdown feature the editor accepts, a
// body longer than a page, non-Latin text, figures in embeddable and
// non-embeddable formats, traceability and test evidence. Both documents are
// rendered from it and checked for the defects the 2026-09-12 assessment
// found: content dropped, figures missing, markdown flattened, links absent,
// characters substituted, tables that split, output that differs between
// renders.
//
// The PDF is checked at the byte level — object dictionaries (annotations,
// images, fonts, outlines) are plain text in a gofpdf file even though the
// content streams are compressed — and the DOCX through its XML parts.

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

const fidelityBody = "This body has **bold**, *italic*, `code`, ~~gone~~ and a [markdown link](https://example.com/spec#s3).\n" +
	"Second line cites #REQ-2 and its figure #REQ-1-FIG-1.\n\n" +
	"| Column A | Column B |\n|---|---|\n| one | two |\n| three | four |\n\n" +
	"1. first numbered\n2. second numbered\n   - nested bullet\n\n" +
	"- [x] done task\n- [ ] open task\n\n" +
	"```go\nfunc main() {\n    indented()\n}\n```\n\n" +
	"> quoted\n\n" +
	"Unicode probe: Ω ≤ 5 µm — café naïve — Привет\n"

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fidelityFixture builds the project: a section holding a requirement with
// the torture body and two figures, a description longer than a page, a
// test case verifying the requirement, and one link the other way.
func fidelityFixture(t *testing.T) (*exports.ProjectExport, RenderOptions) {
	t.Helper()
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "REQ-1-FIG-1.png")
	if err := os.WriteFile(pngPath, pngBytes(t, 400, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	svgPath := filepath.Join(dir, "REQ-1-FIG-2.svg")
	if err := os.WriteFile(svgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	bogusPath := filepath.Join(dir, "REQ-1-FIG-3.webp")
	if err := os.WriteFile(bogusPath, []byte("not an image at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	long := strings.Repeat("A description that runs well past one page so the renderer must carry it across page breaks rather than drop it. ", 120)
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	data := &exports.ProjectExport{
		ProjectName: "Fidelity Widget",
		ProjectDesc: "A **fixture** project.",
		Artifacts: []*artifacts.Artifact{
			{ID: "h1", Type: artifacts.TypeHeading, Ref: "HDG-1", Title: "Requirements"},
			{ID: "req1", ParentID: ptr("h1"), Type: artifacts.TypeRequirement, Ref: "REQ-1", Title: "Torture requirement", Body: fidelityBody, Version: 3,
				Attributes: map[string]interface{}{"priority": "must", "status": "approved", "verification_method": "test", "verification_status": "verified", "custom_owner": "Ada"}},
			{ID: "req2", ParentID: ptr("h1"), Type: artifacts.TypeRequirement, Ref: "REQ-2", Title: "Second requirement", Body: "Plain.", Version: 1,
				Attributes: map[string]interface{}{"priority": "should", "verification_method": "test"}},
			{ID: "dsc1", ParentID: ptr("h1"), Type: artifacts.TypeDescription, Ref: "DSC-1", Title: "Long prose", Body: long, Version: 1},
			{ID: "h2", Type: artifacts.TypeHeading, Ref: "HDG-2", Title: "Tests"},
			{ID: "tc1", ParentID: ptr("h2"), Type: artifacts.TypeTestCase, Ref: "TC-1", Title: "Torture test", Body: "Runs it.", Version: 2,
				Attributes: map[string]interface{}{"execution_method": "automated"}},
		},
		Links: []*linksdomain.Link{
			{ID: "l1", FromID: "tc1", ToID: "req1", Type: "verifies"},
			{ID: "l2", FromID: "req1", ToID: "req2", Type: "relates-to"},
			{ID: "l3", FromID: "req1", ToID: "dsc1", Type: "relates-to"},
		},
		Attachments: []*attachments.Attachment{
			{ID: "a1", ArtifactID: "req1", Filename: "REQ-1-FIG-1.png", OriginalFilename: "diagram.png", MimeType: "image/png", FilePath: pngPath, FigureRef: "REQ-1-FIG-1", FigureNum: 1, Version: 1},
			{ID: "a2", ArtifactID: "req1", Filename: "REQ-1-FIG-2.svg", OriginalFilename: "vector.svg", MimeType: "image/svg+xml", FilePath: svgPath, FigureRef: "REQ-1-FIG-2", FigureNum: 2, Version: 1},
			{ID: "a3", ArtifactID: "req1", Filename: "REQ-1-FIG-3.webp", OriginalFilename: "broken.webp", MimeType: "image/webp", FilePath: bogusPath, FigureRef: "REQ-1-FIG-3", FigureNum: 3, Version: 1},
			{ID: "a4", ArtifactID: "req2", Filename: "REQ-2-FIG-1.png", OriginalFilename: "missing.png", MimeType: "image/png", FilePath: filepath.Join(dir, "missing.png"), FigureRef: "REQ-2-FIG-1", FigureNum: 1, Version: 1},
		},
	}
	executed := now.Add(-time.Hour)
	opts := RenderOptions{
		Snapshot:  Snapshot{BaselineID: "base-1", BaselineName: "Release 1", CapturedAt: now.Add(-48 * time.Hour), ExportedAt: now},
		Content:   exports.DefaultContent(),
		Workspace: Workspace{Name: "Acme Devices", Logo: pngBytes(t, 300, 100), LogoMime: "image/png"},
		Latest:    map[string]*vv.TestResult{"tc1": {ID: "r1", RunID: "run-1", TestCaseID: "tc1", Status: "pass", ExecutedAt: &executed}},
		Runs:      []*vv.TestRun{{ID: "run-1", Name: "Nightly", Status: "completed", StartedAt: executed, CompletedAt: &now}},
	}
	opts.Content.TestResults = true
	opts.Content.VVStatus = true
	opts.Content.Template = "vv"
	return data, opts
}

// docxText flattens document XML to the text a reader sees.
func docxText(xml string) string {
	text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(xml, "")
	return strings.NewReplacer("&quot;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">", "&apos;", "'").Replace(text)
}

func docxPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	return ""
}

func TestFidelityDOCX(t *testing.T) {
	data, opts := fidelityFixture(t)
	out, err := buildReportDOCX(data, opts)
	if err != nil {
		t.Fatalf("docx: %v", err)
	}
	doc := docxPart(t, out, "word/document.xml")
	text := docxText(doc)

	// 1. Nothing dropped: the long description and every probe phrase.
	for _, probe := range []string{"carry it across page breaks", "first numbered", "second numbered", "nested bullet", "quoted", "Ω ≤ 5 µm", "Привет", "indented()", "Column A", "Torture requirement", "DSC-1 Long prose"} {
		if !strings.Contains(text, probe) {
			t.Errorf("document text missing %q", probe)
		}
	}
	// 2. Figures embedded and captioned; unrenderable ones become sentences.
	if n := strings.Count(doc, "<w:drawing>"); n != 2 { // logo + PNG figure
		t.Errorf("drawings = %d, want 2 (logo and the PNG figure)", n)
	}
	for _, part := range []string{"word/media/image1.png", "word/media/image2.png"} {
		if docxPart(t, out, part) == "" {
			t.Errorf("missing media part %s", part)
		}
	}
	for _, probe := range []string{"Figure REQ-1-FIG-1 — diagram.png", "Figure REQ-1-FIG-2 — vector.svg is an SVG drawing", "Figure REQ-1-FIG-3 — broken.webp is not an image", "Figure REQ-2-FIG-1 — missing.png could not be read"} {
		if !strings.Contains(text, probe) {
			t.Errorf("figure handling missing %q", probe)
		}
	}
	// 4. Markdown structure survives: a real table with a repeating header,
	//    numbered and bulleted lists through numbering.xml, a code style, the
	//    external link with its URL, bold and strike runs.
	if !strings.Contains(doc, "<w:tblHeader/>") || !strings.Contains(doc, `<w:t xml:space="preserve">Column A</w:t>`) {
		t.Error("the GFM table did not become a Word table with a header row")
	}
	if !strings.Contains(doc, "<w:numPr>") || docxPart(t, out, "word/numbering.xml") == "" {
		t.Error("lists carry no numbering definitions")
	}
	if !strings.Contains(doc, `<w:pStyle w:val="Code"/>`) {
		t.Error("code block lost its style")
	}
	rels := docxPart(t, out, "word/_rels/document.xml.rels")
	if !strings.Contains(rels, `Target="https://example.com/spec#s3" TargetMode="External"`) {
		t.Error("external link URL was not kept")
	}
	if !strings.Contains(doc, "<w:b/>") || !strings.Contains(doc, "<w:strike/>") {
		t.Error("emphasis runs lost")
	}
	// 5. Citations and traceability rows are hyperlinks to bookmarks,
	//    including one to a description artifact.
	for _, anchor := range []string{`w:anchor="a_req2"`, `w:anchor="a_req1"`, `w:anchor="a_dsc1"`} {
		if !strings.Contains(doc, anchor) {
			t.Errorf("no hyperlink %s", anchor)
		}
	}
	if !strings.Contains(doc, `w:name="a_dsc1"`) {
		t.Error("description artifact has no bookmark")
	}
	// 7. Pagination properties, footer, contents field, properties.
	if !strings.Contains(doc, "<w:cantSplit/>") {
		t.Error("table rows may split across pages")
	}
	if !strings.Contains(doc, `TOC \o "1-3"`) {
		t.Error("no table of contents field")
	}
	if footer := docxPart(t, out, "word/footer1.xml"); !strings.Contains(footer, " PAGE ") || !strings.Contains(footer, " NUMPAGES ") {
		t.Error("footer has no page number fields")
	}
	if core := docxPart(t, out, "docProps/core.xml"); !strings.Contains(core, "<dc:title>Fidelity Widget</dc:title>") || !strings.Contains(core, "Acme Devices") {
		t.Errorf("core properties incomplete: %s", core)
	}
	// 9. Fields, V&V rollup and the latest result are shown; custom keys
	//    carry their label.
	for _, probe := range []string{"Priority", "must", "Verification status", "verified", "Custom owner", "Ada", "V&amp;V rollup", "Latest result", "pass", "Nightly", "Verification summary", "Requirement coverage", "Test results"} {
		if !strings.Contains(doc, probe) {
			t.Errorf("document missing %q", probe)
		}
	}
	// Snapshot statement and workspace on the cover.
	if !strings.Contains(text, `Snapshot: baseline "Release 1" (id base-1) captured 2026-09-10 12:00 UTC.`) {
		t.Error("cover does not state the baseline")
	}
	if !strings.Contains(text, "Verification & Validation") {
		t.Error("cover does not name the template")
	}
	// 10. Deterministic.
	again, _ := buildReportDOCX(data, opts)
	if !bytes.Equal(out, again) {
		t.Error("two renders of the same snapshot differ")
	}
}

func TestFidelityDOCXFieldsCanBeSwitchedOff(t *testing.T) {
	data, opts := fidelityFixture(t)
	opts.Content.AllFields = false
	opts.Content.Fields = []string{"priority"}
	out, err := buildReportDOCX(data, opts)
	if err != nil {
		t.Fatal(err)
	}
	doc := docxPart(t, out, "word/document.xml")
	if !strings.Contains(doc, ">Priority<") {
		t.Error("the chosen field is missing")
	}
	if strings.Contains(doc, ">Custom owner<") || strings.Contains(doc, ">Verification method<") {
		t.Error("fields that were switched off are shown")
	}
	opts.Content.Traceability = false
	opts.Content.Figures = false
	out, _ = buildReportDOCX(data, opts)
	doc = docxPart(t, out, "word/document.xml")
	if strings.Contains(doc, ">Traceability<") {
		t.Error("traceability shown although switched off")
	}
	if strings.Contains(doc, "Figure REQ-1-FIG-1") {
		t.Error("figures shown although switched off")
	}
}

func TestFidelityDOCXLiveStatement(t *testing.T) {
	data, opts := fidelityFixture(t)
	opts.Snapshot = Snapshot{ExportedAt: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	out, _ := buildReportDOCX(data, opts)
	text := docxText(docxPart(t, out, "word/document.xml"))
	if !strings.Contains(text, "Live project state as of 2026-09-12 12:00 UTC. This is not a baseline") {
		t.Error("cover does not state that the document is the live project")
	}
}

func TestFidelityPDF(t *testing.T) {
	data, opts := fidelityFixture(t)
	out, err := buildReportPDF(data, opts)
	if err != nil {
		t.Fatalf("pdf: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "%PDF-") {
		t.Fatal("not a PDF")
	}
	pages := strings.Count(s, "/Type /Page\n") + strings.Count(s, "/Type /Page ")
	if pages < 6 {
		t.Errorf("pages = %d, want a multi-page document (cover, contents, product, body, verification, results)", pages)
	}
	// Embedded TrueType fonts, not the core Windows-1252 set.
	if !strings.Contains(s, "/FontFile2") {
		t.Error("no embedded font: non-Latin text would be lost")
	}
	// Outline for navigation, at least one entry per heading and artifact.
	if !strings.Contains(s, "/Outlines") || strings.Count(s, "/Parent") < 6 {
		t.Error("no document outline")
	}
	// Images: the logo and the PNG figure are image XObjects.
	if n := strings.Count(s, "/Subtype /Image"); n < 2 {
		t.Errorf("image objects = %d, want the logo and the figure", n)
	}
	// Links: internal destinations and the one external URI.
	if !strings.Contains(s, "/URI (https://example.com/spec#s3)") {
		t.Error("external link URL was not kept")
	}
	if n := strings.Count(s, "/Subtype /Link"); n < 5 {
		t.Errorf("link annotations = %d, want the citations, traceability rows and contents", n)
	}
	// Metadata.
	if !strings.Contains(s, "/Title") || !strings.Contains(s, "/Author") {
		t.Error("no title or author metadata")
	}
	// Deterministic apart from gofpdf's creation date and the order it
	// lists font resources in (a map iteration inside the library that
	// changes no content).
	again, _ := buildReportPDF(data, opts)
	if pdfNormalise(string(again)) != pdfNormalise(s) {
		t.Error("two renders of the same snapshot differ")
	}
}

// pdfNormalise reduces a PDF to the objects that carry content — pages,
// their content streams, link annotations, images and the outline — and
// drops the creation date. gofpdf builds its embedded-font subsets from map
// iteration, so the font objects differ between two renders of identical
// content; what a reader sees does not.
//
// Page objects reference images through the resources dictionary by name,
// not by object number, so dropping the numbers loses nothing a reader sees.
func pdfNormalise(s string) string {
	s = regexp.MustCompile(`/CreationDate \([^)]*\)`).ReplaceAllString(s, "")
	objs := regexp.MustCompile(`(?s)(\d+) 0 obj\n(.*?)\nendobj`)
	byID := map[string]string{}
	var ids []string
	for _, m := range objs.FindAllStringSubmatch(s, -1) {
		byID[m[1]] = m[2]
		ids = append(ids, m[1])
	}
	keep := map[string]bool{}
	contents := regexp.MustCompile(`/Contents (\d+) 0 R`)
	for id, body := range byID {
		switch {
		case strings.Contains(body, "/Type /Page"), strings.Contains(body, "/Subtype /Link"),
			strings.Contains(body, "/Subtype /Image"), strings.Contains(body, "/Type /Outlines"),
			strings.Contains(body, "/Title (") && strings.Contains(body, "/Parent"):
			keep[id] = true
			for _, c := range contents.FindAllStringSubmatch(body, -1) {
				keep[c[1]] = true
			}
		}
	}
	// Object numbers are left out: gofpdf assigns them to fonts and images
	// in map order, so two identical images can swap numbers between
	// renders. Their bodies, and the page objects that reference them by
	// content, are what must match.
	var kept []string
	for _, id := range ids {
		if keep[id] {
			kept = append(kept, byID[id])
		}
	}
	sort.Strings(kept)
	return strings.Join(kept, "\n")
}

func TestFidelityPDFSurvivesBadFigures(t *testing.T) {
	// Every figure unrenderable, plus a logo that is not an image: the
	// document must still render.
	data, opts := fidelityFixture(t)
	data.Attachments = data.Attachments[1:]
	opts.Workspace.Logo = []byte("nope")
	if _, err := buildReportPDF(data, opts); err != nil {
		t.Fatalf("pdf failed on unrenderable figures: %v", err)
	}
	if _, err := buildReportDOCX(data, opts); err != nil {
		t.Fatalf("docx failed on unrenderable figures: %v", err)
	}
}

func TestReportModelOrdersLinksDeterministically(t *testing.T) {
	links := []*linksdomain.Link{
		{ID: "1", FromID: "a", ToID: "b", Type: "verifies"},
		{ID: "2", FromID: "a", ToID: "c", Type: "derives-from"},
		{ID: "3", FromID: "d", ToID: "a", Type: "satisfies"},
		{ID: "4", FromID: "a", ToID: "b", Type: "verifies"}, // duplicate
	}
	titles := map[string]string{"b": "B", "c": "C", "d": "D"}
	rows := buildLinkRows(links, titles)["a"]
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (duplicate dropped)", len(rows))
	}
	if rows[0].Direction != "Incoming" || rows[0].TargetTitle != "D" {
		t.Errorf("incoming first: %+v", rows[0])
	}
	if rows[1].Relationship > rows[2].Relationship {
		t.Errorf("outgoing rows not ordered by relationship: %+v", rows[1:])
	}
}

func TestFieldRowsFollowContent(t *testing.T) {
	data, opts := fidelityFixture(t)
	m := buildReportModel(data, opts)
	rows := m.fieldRows(m.byID["req1"])
	labels := make([]string, 0, len(rows))
	for _, r := range rows {
		labels = append(labels, r.Label)
	}
	want := "Reference Type Version Priority Status Verification method Verification status Custom owner V&V rollup"
	if got := strings.Join(labels, " "); got != want {
		t.Errorf("rows = %q, want %q", got, want)
	}
	if rows[len(rows)-1].Rollup != "pass" {
		t.Errorf("rollup for a verified requirement with a passing test = %q", rows[len(rows)-1].Rollup)
	}
}
