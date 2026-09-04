package exports

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/links"
)

func art(id, parent, artType string) *artifacts.Artifact {
	a := &artifacts.Artifact{ID: id, Type: artType, Title: id}
	if parent != "" {
		p := parent
		a.ParentID = &p
	}
	return a
}

// A document with two sections: requirements (one heading, two requirements)
// and verification (one heading, one test case).
func snapshot() *ProjectExport {
	return &ProjectExport{
		ProjectName: "Widget",
		Artifacts: []*artifacts.Artifact{
			art("reqs", "", artifacts.TypeHeading),
			art("r1", "reqs", "requirement"),
			art("r2", "reqs", "requirement"),
			art("vv", "", artifacts.TypeHeading),
			art("tc1", "vv", "test-case"),
		},
		Links: []*links.Link{
			{ID: "l1", FromID: "tc1", ToID: "r1", Type: "verifies"},
			{ID: "l2", FromID: "r1", ToID: "r2", Type: "decomposes-to"},
		},
		Attachments: []*attachments.Attachment{
			{ID: "a1", ArtifactID: "r1", FigureRef: "REQ-1-FIG-1", MimeType: "image/png", FileSize: 10},
			{ID: "a2", ArtifactID: "tc1", MimeType: "text/csv", FileSize: 20},
		},
	}
}

func ids(list []*artifacts.Artifact) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestApplyEverything(t *testing.T) {
	got := Apply(snapshot(), Everything())
	if !equal(ids(got.Artifacts), []string{"reqs", "r1", "r2", "vv", "tc1"}) {
		t.Errorf("artifacts = %v, want the whole document", ids(got.Artifacts))
	}
	if len(got.Links) != 2 {
		t.Errorf("links = %d, want both", len(got.Links))
	}
	// Attachment METADATA always travels with its artifact — a JSON export an
	// import can restore must carry it whether or not the files are packed.
	if len(got.Attachments) != 2 {
		t.Errorf("attachments = %d, want both regardless of file categories", len(got.Attachments))
	}
	if files := SelectedFiles(got, Everything()); len(files) != 0 {
		t.Errorf("files = %d, want none: attachment files are opt-in", len(files))
	}
}

func TestApplyLeavesTheOriginalAlone(t *testing.T) {
	data := snapshot()
	Apply(data, Selection{Types: []string{"requirement"}})
	if len(data.Artifacts) != 5 {
		t.Errorf("original artifacts = %d, want 5: Apply must not mutate the snapshot", len(data.Artifacts))
	}
}

func TestApplyDropsHeadings(t *testing.T) {
	got := Apply(snapshot(), Selection{})
	if !equal(ids(got.Artifacts), []string{"r1", "r2", "tc1"}) {
		t.Errorf("artifacts = %v, want the artifacts without their headings", ids(got.Artifacts))
	}
}

func TestApplySection(t *testing.T) {
	// The requirements section only: its heading and everything under it.
	got := Apply(snapshot(), Selection{Sections: []string{"reqs"}, IncludeHeadings: true})
	if !equal(ids(got.Artifacts), []string{"reqs", "r1", "r2"}) {
		t.Errorf("artifacts = %v, want the requirements section", ids(got.Artifacts))
	}
	// tc1 is gone, so the link that verifies r1 goes with it: half a link
	// would assert a relationship to something the reader cannot see.
	if len(got.Links) != 1 || got.Links[0].ID != "l2" {
		t.Errorf("links = %v, want only the link with both ends inside", got.Links)
	}
}

func TestApplySectionReachesGrandchildren(t *testing.T) {
	data := snapshot()
	data.Artifacts = append(data.Artifacts, art("sub", "reqs", artifacts.TypeHeading), art("r3", "sub", "requirement"))
	got := Apply(data, Selection{Sections: []string{"reqs"}, IncludeHeadings: true})
	if !equal(ids(got.Artifacts), []string{"reqs", "r1", "r2", "sub", "r3"}) {
		t.Errorf("artifacts = %v, want the whole subtree", ids(got.Artifacts))
	}
}

func TestApplyTypes(t *testing.T) {
	got := Apply(snapshot(), Selection{Types: []string{"requirement"}, IncludeHeadings: true})
	if !equal(ids(got.Artifacts), []string{"reqs", "r1", "r2", "vv"}) {
		t.Errorf("artifacts = %v, want requirements and the headings that organise them", ids(got.Artifacts))
	}
}

func TestApplySectionAndTypeCombine(t *testing.T) {
	got := Apply(snapshot(), Selection{Sections: []string{"vv"}, Types: []string{"requirement"}})
	if len(got.Artifacts) != 0 {
		t.Errorf("artifacts = %v, want none: no requirement lives in the verification section", ids(got.Artifacts))
	}
}

func TestApplyTerminatesOnACycleAlreadyInTheData(t *testing.T) {
	a, b := art("x", "y", "requirement"), art("y", "x", "requirement")
	data := &ProjectExport{Artifacts: []*artifacts.Artifact{a, b}}
	got := Apply(data, Selection{Sections: []string{"nowhere"}})
	if len(got.Artifacts) != 0 {
		t.Errorf("artifacts = %v, want none", ids(got.Artifacts))
	}
}

func TestSelectedFilesByCategory(t *testing.T) {
	data := Apply(snapshot(), Everything())
	figures := SelectedFiles(data, Selection{IncludeHeadings: true, Attachments: []string{CategoryFigures}})
	if len(figures) != 1 || figures[0].ID != "a1" {
		t.Errorf("figures = %v, want just the numbered figure", figures)
	}
	both := SelectedFiles(data, Selection{IncludeHeadings: true, Attachments: []string{CategoryFigures, CategoryData}})
	if len(both) != 2 {
		t.Errorf("files = %d, want the figure and the data file", len(both))
	}
}

func TestSelectedFilesFollowTheDocument(t *testing.T) {
	// Narrow to the requirements section: the test case's data file is not in
	// the download, because the artifact it describes is not either.
	data := Apply(snapshot(), Selection{Sections: []string{"reqs"}, IncludeHeadings: true})
	files := SelectedFiles(data, Selection{Attachments: []string{CategoryFigures, CategoryData}})
	if len(files) != 1 || files[0].ID != "a1" {
		t.Errorf("files = %v, want only the figure inside the chosen section", files)
	}
}

func TestAttachmentCategory(t *testing.T) {
	cases := []struct {
		name string
		in   *attachments.Attachment
		want string
	}{
		{"numbered figure", &attachments.Attachment{FigureRef: "REQ-1-FIG-1", MimeType: "image/png"}, CategoryFigures},
		{"image with no number", &attachments.Attachment{MimeType: "image/jpeg"}, CategoryImages},
		{"pdf", &attachments.Attachment{MimeType: "application/pdf"}, CategoryDocuments},
		{"word", &attachments.Attachment{MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}, CategoryDocuments},
		{"csv with a charset", &attachments.Attachment{MimeType: "text/csv; charset=utf-8"}, CategoryData},
		{"spreadsheet", &attachments.Attachment{MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"}, CategoryData},
		{"step file", &attachments.Attachment{MimeType: "application/step"}, CategoryOther},
		{"nothing at all", nil, CategoryOther},
	}
	for _, tc := range cases {
		if got := AttachmentCategory(tc.in); got != tc.want {
			t.Errorf("%s: category = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCategoriesCountsWhatIsThere(t *testing.T) {
	got := Categories(snapshot().Attachments)
	if len(got) != 2 {
		t.Fatalf("categories = %v, want the two kinds this project holds", got)
	}
	if got[0].Category != CategoryFigures || got[0].Count != 1 || got[0].Bytes != 10 {
		t.Errorf("first = %+v, want one 10-byte figure", got[0])
	}
	if got[1].Category != CategoryData || got[1].Bytes != 20 {
		t.Errorf("second = %+v, want the data file", got[1])
	}
	if len(Categories(nil)) != 0 {
		t.Error("a project with no attachments should offer no categories")
	}
}

func TestNarrowsArtifacts(t *testing.T) {
	if Everything().NarrowsArtifacts() {
		t.Error("the whole project narrows nothing")
	}
	if !(Selection{}).NarrowsArtifacts() {
		t.Error("dropping headings narrows the document")
	}
	if (Selection{IncludeHeadings: true, Attachments: []string{CategoryFigures}}).NarrowsArtifacts() {
		t.Error("adding files does not narrow the document")
	}
}
