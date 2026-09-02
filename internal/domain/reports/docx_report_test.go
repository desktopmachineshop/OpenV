package reports

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
)

// readZipPart returns the decompressed contents of a named file inside a zip
// archive, failing the test if the entry is absent.
func readZipPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return string(b)
		}
	}
	t.Fatalf("zip is missing entry %q", name)
	return ""
}

func ptr(s string) *string { return &s }

// TestBuildReportDOCXStructure verifies the DOCX bytes are a valid OOXML zip
// containing the expected parts and that document.xml carries the project
// headings, requirement text, and traceability link labels/targets.
func TestBuildReportDOCXStructure(t *testing.T) {
	data := &exports.ProjectExport{
		ProjectName: "My Widget",
		ProjectDesc: "A small widget project.",
		Artifacts: []*artifacts.Artifact{
			{ID: "h1", Type: "heading", Title: "Requirements"},
			{ID: "req1", ParentID: ptr("h1"), Type: "requirement", Title: "The system shall boot", Body: "Boots within 5 seconds.", Version: 3},
			{ID: "need1", Type: "user-need", Title: "User can start the device"},
		},
		Links: []*linksdomain.Link{
			{ID: "l1", FromID: "req1", ToID: "need1", Type: "satisfies"},
		},
	}

	out, err := buildReportDOCX(data, "")
	if err != nil {
		t.Fatalf("buildReportDOCX: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("buildReportDOCX returned no bytes")
	}

	// Required OOXML container parts must all be present.
	for _, part := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml"} {
		readZipPart(t, out, part)
	}

	doc := readZipPart(t, out, "word/document.xml")

	assertContains := func(needle string) {
		t.Helper()
		if !strings.Contains(doc, needle) {
			t.Errorf("document.xml missing %q", needle)
		}
	}

	// Project title as Heading1.
	assertContains(`<w:pStyle w:val="Heading1"/>`)
	assertContains("My Widget")
	// Heading artifact as Heading2 (depth 0 -> level 2).
	assertContains(`<w:pStyle w:val="Heading2"/>`)
	assertContains("Requirements")
	// Nested requirement heading (depth 1 -> Heading3) and its body/type.
	assertContains(`<w:pStyle w:val="Heading3"/>`)
	assertContains("The system shall boot")
	assertContains("Boots within 5 seconds.")
	assertContains("requirement") // Type row value
	assertContains("v3")          // Version row value
	// Traceability: outgoing "satisfies" to the target need's title.
	assertContains("Traceability")
	assertContains("satisfies")
	assertContains("User can start the device")
	// Well-formed document root.
	assertContains("<w:document")
	assertContains("</w:document>")
}

// TestDocxEscape ensures XML-significant characters in artifact content are
// escaped so the produced document stays well-formed.
func TestBuildReportDOCXEscaping(t *testing.T) {
	data := &exports.ProjectExport{
		ProjectName: "A & B <Test>",
		Artifacts: []*artifacts.Artifact{
			{ID: "r", Type: "requirement", Title: `Handle "quotes" & <tags>`, Version: 1},
		},
	}
	out, err := buildReportDOCX(data, "")
	if err != nil {
		t.Fatalf("buildReportDOCX: %v", err)
	}
	doc := readZipPart(t, out, "word/document.xml")
	if strings.Contains(doc, "A & B <Test>") {
		t.Error("raw unescaped project name leaked into document.xml")
	}
	if !strings.Contains(doc, "A &amp; B &lt;Test&gt;") {
		t.Error("project name was not XML-escaped")
	}
	// The document must still parse as XML overall (balanced, escaped).
	if !strings.Contains(doc, "&amp;") || !strings.Contains(doc, "&lt;tags&gt;") {
		t.Error("special characters in title were not escaped")
	}
}

// TestReportShowsNumbersAndRefs is the cross-referencing guarantee the
// numbering exists for: a reader holding the exported document can see where
// a section sits ("1.1") and can cite any artifact by an address that will
// still mean the same thing after the document is reorganized ("REQ-1").
func TestReportShowsNumbersAndRefs(t *testing.T) {
	intro := &artifacts.Artifact{ID: "intro", Type: artifacts.TypeHeading, Ref: "HDG-1", Title: "Intro", SortOrder: 1}
	background := &artifacts.Artifact{ID: "background", ParentID: strptr("intro"), Type: artifacts.TypeHeading, Ref: "HDG-2", Title: "Background", SortOrder: 1}
	req := &artifacts.Artifact{ID: "req", ParentID: strptr("background"), Type: artifacts.TypeRequirement, Ref: "REQ-1", Title: "Brake within 2 m", SortOrder: 1, Version: 3}

	data := &exports.ProjectExport{
		ProjectName: "Numbering",
		Artifacts:   []*artifacts.Artifact{intro, background, req},
		Links: []*linksdomain.Link{
			{ID: "l1", FromID: "req", ToID: "background", Type: "satisfies"},
		},
	}

	out, err := buildReportDOCX(data, "")
	if err != nil {
		t.Fatalf("buildReportDOCX: %v", err)
	}
	doc := readZipPart(t, out, "word/document.xml")

	for _, want := range []string{
		"1 Intro",            // a root section is numbered by position
		"1.1 Background",     // nesting produces the dotted number
		"REQ-1 Brake within", // a requirement is addressed by its stable ref
		"Reference",          // the details table names the ref explicitly
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("report is missing %q", want)
		}
	}

	// A section is not also labelled with its heading ref: the number is its
	// address in the document, and showing both would just be noise.
	if strings.Contains(doc, "HDG-1 Intro") {
		t.Error("section heading shows a ref instead of its document number")
	}

	// The traceability row pointing at the section carries the number, so a
	// reader can follow the cross-reference without hunting for the title.
	if !strings.Contains(doc, "1.1 Background") {
		t.Error("traceability target is not addressable")
	}
}

func strptr(s string) *string { return &s }
