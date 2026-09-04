package downloads

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/reports"
)

func art(id, parent, artType, title string) *artifacts.Artifact {
	a := &artifacts.Artifact{ID: id, Type: artType, Title: title, Ref: strings.ToUpper(id)}
	if parent != "" {
		p := parent
		a.ParentID = &p
	}
	return a
}

func snapshot() *exports.ProjectExport {
	return &exports.ProjectExport{
		ProjectName: "Widget",
		Artifacts: []*artifacts.Artifact{
			art("reqs", "", artifacts.TypeHeading, "Requirements"),
			art("r1", "reqs", "requirement", "The system shall widget"),
			art("r2", "reqs", "requirement", "The system shall sprocket"),
			art("vv", "", artifacts.TypeHeading, "Verification"),
			art("tc1", "vv", "test-case", "Widget suite"),
		},
		Attachments: []*attachments.Attachment{
			{ID: "a1", ArtifactID: "r1", Filename: "REQ-1-FIG-1.png", FigureRef: "REQ-1-FIG-1",
				MimeType: "image/png", FilePath: "/uploads/a1.png", FileSize: 3},
			{ID: "a2", ArtifactID: "tc1", Filename: "rig.csv", MimeType: "text/csv",
				FilePath: "/uploads/a2.csv", FileSize: 4},
		},
	}
}

// Stand-ins for the export and report services. Both interfaces are called
// Service, so they cannot be embedded in one struct; they share a recorder
// instead, which is what the assertions read.
type recorder struct {
	loaded       []string
	renderedFrom *exports.ProjectExport
}

type fakeExports struct {
	exports.Service
	rec *recorder
}

func (f *fakeExports) RenderExport(data *exports.ProjectExport, format exports.ExportFormat) ([]byte, string, error) {
	f.rec.renderedFrom = data
	body, _ := json.Marshal(data)
	return body, "widget." + string(format), nil
}

type fakeReports struct {
	reports.Service
	rec *recorder
}

func (f *fakeReports) LoadReportExport(projectID, baselineID string) (*exports.ProjectExport, string, error) {
	f.rec.loaded = append(f.rec.loaded, projectID+"/"+baselineID)
	name := ""
	if baselineID != "" && baselineID != "live" {
		name = "Baseline " + baselineID
	}
	return snapshot(), name, nil
}

func (f *fakeReports) RenderProjectReport(data *exports.ProjectExport, baselineName string) ([]byte, string, error) {
	f.rec.renderedFrom = data
	return []byte("PDF:" + baselineName), "widget.pdf", nil
}

func (f *fakeReports) RenderProjectReportDOCX(data *exports.ProjectExport, baselineName string) ([]byte, string, error) {
	f.rec.renderedFrom = data
	return []byte("DOCX:" + baselineName), "widget.docx", nil
}

func newService(t *testing.T) (*DefaultService, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := NewService(&fakeExports{rec: rec}, &fakeReports{rec: rec})
	s.SetFileReader(func(path string) ([]byte, error) {
		switch path {
		case "/uploads/a1.png":
			return []byte("png"), nil
		case "/uploads/a2.csv":
			return []byte("a,b\n"), nil
		}
		return nil, fmt.Errorf("no such file: %s", path)
	})
	return s, rec
}

func TestDownloadRendersTheNarrowedSnapshot(t *testing.T) {
	s, rec := newService(t)
	_, err := s.Download(Request{
		ProjectID: "p1",
		Format:    FormatJSON,
		Selection: exports.Selection{Sections: []string{"reqs"}, IncludeHeadings: true},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got := []string{}
	for _, a := range rec.renderedFrom.Artifacts {
		got = append(got, a.ID)
	}
	// The renderer never sees the verification section: the filter is applied
	// once, before any format is chosen.
	if strings.Join(got, ",") != "reqs,r1,r2" {
		t.Errorf("rendered artifacts = %v, want the requirements section only", got)
	}
}

// Every format reads the same narrowed snapshot — that is the whole point of
// preparing once and rendering per format.
func TestEveryFormatSeesTheSameSelection(t *testing.T) {
	for _, format := range Formats {
		s, rec := newService(t)
		res, err := s.Download(Request{
			ProjectID: "p1",
			Format:    format,
			Selection: exports.Selection{Types: []string{"requirement"}},
		})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if res.ContentType != ContentType(format) {
			t.Errorf("%s: content type = %q, want %q", format, res.ContentType, ContentType(format))
		}
		got := []string{}
		for _, a := range rec.renderedFrom.Artifacts {
			got = append(got, a.ID)
		}
		if strings.Join(got, ",") != "r1,r2" {
			t.Errorf("%s: rendered artifacts = %v, want the two requirements", format, got)
		}
	}
}

func TestDownloadRejectsAnUnknownFormat(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.Download(Request{ProjectID: "p1", Format: "xls"}); err == nil {
		t.Fatal("want an error for an unsupported format")
	}
}

func TestDownloadNeedsAProject(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.Download(Request{Format: FormatJSON}); err == nil {
		t.Fatal("want an error when no project is named")
	}
}

func TestDownloadCarriesTheBaselineName(t *testing.T) {
	s, _ := newService(t)
	res, err := s.Download(Request{ProjectID: "p1", BaselineID: "b7", Format: FormatPDF})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(res.Data) != "PDF:Baseline b7" {
		t.Errorf("data = %q, want the baseline's name to reach the renderer", res.Data)
	}
}

func zipEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestDownloadWithoutAttachmentsIsThePlainFile(t *testing.T) {
	s, _ := newService(t)
	res, err := s.Download(Request{ProjectID: "p1", Format: FormatJSON, Selection: exports.Everything()})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.Filename != "widget.json" || res.ContentType != "application/json" {
		t.Errorf("got %s / %s, want the bare document", res.Filename, res.ContentType)
	}
}

func TestDownloadWithAttachmentsIsAnArchive(t *testing.T) {
	s, _ := newService(t)
	res, err := s.Download(Request{
		ProjectID: "p1",
		Format:    FormatPDF,
		Selection: exports.Selection{
			IncludeHeadings: true,
			Attachments:     []string{exports.CategoryFigures, exports.CategoryData},
		},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.Filename != "widget.zip" || res.ContentType != "application/zip" {
		t.Fatalf("got %s / %s, want a zip", res.Filename, res.ContentType)
	}
	entries := zipEntries(t, res.Data)
	for _, want := range []string{"widget.pdf", "attachments/REQ-1-FIG-1.png", "attachments/rig.csv"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("archive is missing %s; has %v", want, keys(entries))
		}
	}
	if entries["attachments/REQ-1-FIG-1.png"] != "png" {
		t.Errorf("figure content = %q, want the file's bytes", entries["attachments/REQ-1-FIG-1.png"])
	}
}

func TestArchiveHoldsOnlyTheCategoriesAskedFor(t *testing.T) {
	s, _ := newService(t)
	res, err := s.Download(Request{
		ProjectID: "p1",
		Format:    FormatJSON,
		Selection: exports.Selection{IncludeHeadings: true, Attachments: []string{exports.CategoryFigures}},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	entries := zipEntries(t, res.Data)
	if _, ok := entries["attachments/rig.csv"]; ok {
		t.Error("the data file is in an archive that asked only for figures")
	}
}

// A drawing that has gone missing from disk must not deny the reader the
// document they asked for — but they have to be told.
func TestAnUnreadableAttachmentIsRecordedNotFatal(t *testing.T) {
	s, _ := newService(t)
	s.SetFileReader(func(path string) ([]byte, error) {
		if path == "/uploads/a1.png" {
			return nil, fmt.Errorf("gone")
		}
		return []byte("a,b\n"), nil
	})
	res, err := s.Download(Request{
		ProjectID: "p1",
		Format:    FormatJSON,
		Selection: exports.Selection{
			IncludeHeadings: true,
			Attachments:     []string{exports.CategoryFigures, exports.CategoryData},
		},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	entries := zipEntries(t, res.Data)
	if _, ok := entries["attachments/REQ-1-FIG-1.png"]; ok {
		t.Error("an unreadable file should not be in the archive")
	}
	note, ok := entries["attachments/MISSING.txt"]
	if !ok || !strings.Contains(note, "REQ-1-FIG-1.png") {
		t.Errorf("missing-file note = %q, want it to name the file", note)
	}
	if _, ok := entries["attachments/rig.csv"]; !ok {
		t.Error("the readable file should still be there")
	}
}

func TestArchiveNamesCannotClimbOutOfIt(t *testing.T) {
	s, _ := newService(t)
	s.SetFileReader(func(string) ([]byte, error) { return []byte("x"), nil })
	res, err := s.Download(Request{
		ProjectID: "p1",
		Format:    FormatJSON,
		Selection: exports.Selection{IncludeHeadings: true, Attachments: []string{exports.CategoryFigures, exports.CategoryData}},
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	for name := range zipEntries(t, res.Data) {
		if strings.Contains(name, "..") {
			t.Errorf("archive entry %q escapes the archive", name)
		}
	}
}

func TestAttachmentNameIsFlattened(t *testing.T) {
	cases := map[string]string{
		"REQ-1-FIG-1.png":       "REQ-1-FIG-1.png",
		"../../etc/passwd":      "passwd",
		`..\..\windows\sys.ini`: "sys.ini",
		"":                      "a9",
	}
	for in, want := range cases {
		got := attachmentName(&attachments.Attachment{ID: "a9", Filename: in})
		if got != want {
			t.Errorf("attachmentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTwoFilesWithOneNameBothSurvive(t *testing.T) {
	used := map[string]int{}
	first := uniqueName(used, "attachments/plan.pdf")
	second := uniqueName(used, "attachments/plan.pdf")
	if first != "attachments/plan.pdf" || second != "attachments/plan (2).pdf" {
		t.Errorf("names = %q, %q, want the second to be distinguished", first, second)
	}
}

func TestBuildOptionsDescribesTheProject(t *testing.T) {
	opts := BuildOptions(snapshot())
	if len(opts.Sections) != 2 {
		t.Fatalf("sections = %+v, want the two top-level headings", opts.Sections)
	}
	if opts.Sections[0].Title != "Requirements" || opts.Sections[0].Artifacts != 2 {
		t.Errorf("first section = %+v, want Requirements holding two artifacts", opts.Sections[0])
	}
	if opts.Sections[0].Number != "1" {
		t.Errorf("section number = %q, want the document number", opts.Sections[0].Number)
	}
	if len(opts.Types) != 2 || opts.Types[0].Type != "requirement" || opts.Types[0].Count != 2 {
		t.Errorf("types = %+v, want requirement (2) first", opts.Types)
	}
	if len(opts.Attachments) != 2 {
		t.Errorf("attachment categories = %+v, want figures and data", opts.Attachments)
	}
}

func TestBuildOptionsOffersNothingForNothing(t *testing.T) {
	opts := BuildOptions(nil)
	if len(opts.Sections) != 0 || len(opts.Types) != 0 || len(opts.Attachments) != 0 {
		t.Errorf("options = %+v, want empty lists rather than nil", opts)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
