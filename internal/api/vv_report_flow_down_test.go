package api

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/downloads"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// flowDownDocsHandler serves three projects of one workspace through the real
// report and download services, with each project's export and latest results
// answered per project as the real services answer them: an export holds the
// project's own artifacts and every link that touches it, with the other
// project's end in linked_artifacts (REQ-145).
//
//	plane  REQ-1  test, no case of its own; refined by gear REQ-1, which passes
//	       REQ-2  test, its own case passes; refined by gear REQ-2, which fails
//	       REQ-3  no verification method; refined by gear REQ-3, which passes
//	       REQ-4  test, no case, refined by nothing
//	gear   the landing gear: three requirements, each verified by its own case
//	solo   REQ-1  test, no case; no other project refines anything of it
//
// The viewer is a viewer of plane and solo and has no rights on gear.
func flowDownDocsHandler(t *testing.T) (*Handler, *fakeExportService) {
	t.Helper()
	req := func(id, project, ref, title, method string) *artifacts.Artifact {
		a := &artifacts.Artifact{ID: id, ProjectID: project, Type: artifacts.TypeRequirement, Ref: ref, Title: title,
			Status: "draft", Version: 1, Attributes: map[string]interface{}{}}
		if method != "" {
			a.Attributes["verification_method"] = method
		}
		return a
	}
	testCase := func(id, project, ref, title string) *artifacts.Artifact {
		return &artifacts.Artifact{ID: id, ProjectID: project, Type: artifacts.TypeTestCase, Ref: ref, Title: title,
			Status: "draft", Version: 1, Attributes: map[string]interface{}{}}
	}
	link := func(from, typ, to string) *links.Link {
		return &links.Link{ID: from + "-" + typ + "-" + to, FromID: from, ToID: to, Type: typ}
	}
	far := func(a *artifacts.Artifact, projectName string) *exports.LinkedArtifact {
		return &exports.LinkedArtifact{ID: a.ID, ProjectID: a.ProjectID, ProjectName: projectName, Ref: a.Ref,
			Type: a.Type, Title: a.Title, Status: a.Status}
	}

	rBare := req("r-bare", "plane", "REQ-1", "Brake within 2 m", "test")
	rOwn := req("r-own", "plane", "REQ-2", "Stop on ice", "test")
	rNoMethod := req("r-nomethod", "plane", "REQ-3", "Look tidy on the apron", "")
	rGap := req("r-gap", "plane", "REQ-4", "Log each stop", "test")
	tcOwn := testCase("tc-own", "plane", "TC-1", "Stop on the ice rig")
	gBare := req("g-bare", "gear", "REQ-1", "Gear brake force", "test")
	gOwn := req("g-own", "gear", "REQ-2", "Gear grip on ice", "test")
	gTidy := req("g-tidy", "gear", "REQ-3", "Gear paint", "test")
	refines := []*links.Link{link("g-bare", "refines", "r-bare"), link("g-own", "refines", "r-own"),
		link("g-tidy", "refines", "r-nomethod")}

	encode := func(e exports.ProjectExport) []byte {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("encode %s: %v", e.ProjectID, err)
		}
		return data
	}
	exportSvc := &fakeExportService{byProject: map[string][]byte{
		"plane": encode(exports.ProjectExport{
			ProjectID: "plane", ProjectName: "Plane",
			Artifacts: []*artifacts.Artifact{rBare, rOwn, rNoMethod, rGap, tcOwn},
			Links:     append([]*links.Link{link("tc-own", "verifies", "r-own")}, refines...),
			LinkedArtifacts: []*exports.LinkedArtifact{far(gBare, "Landing gear"), far(gOwn, "Landing gear"),
				far(gTidy, "Landing gear")},
		}),
		"gear": encode(exports.ProjectExport{
			ProjectID: "gear", ProjectName: "Landing gear",
			Artifacts: []*artifacts.Artifact{gBare, gOwn, gTidy,
				testCase("tc-g-bare", "gear", "TC-1", "Measure the brake force"),
				testCase("tc-g-own", "gear", "TC-2", "Roll on the ice rig"),
				testCase("tc-g-tidy", "gear", "TC-3", "Inspect the paint")},
			Links: append([]*links.Link{link("tc-g-bare", "verifies", "g-bare"), link("tc-g-own", "verifies", "g-own"),
				link("tc-g-tidy", "verifies", "g-tidy")}, refines...),
			LinkedArtifacts: []*exports.LinkedArtifact{far(rBare, "Plane"), far(rOwn, "Plane"),
				far(rNoMethod, "Plane")},
		}),
		"solo": encode(exports.ProjectExport{
			ProjectID: "solo", ProjectName: "Solo",
			Artifacts: []*artifacts.Artifact{req("s-r", "solo", "REQ-1", "Stand alone", "test")},
			Links:     []*links.Link{},
		}),
	}}
	vvSvc := &fakeVVService{latest: map[string]map[string]*vv.TestResult{
		"plane": {"tc-own": {TestCaseID: "tc-own", Status: vv.ResultPass}},
		"gear": {
			"tc-g-bare": {TestCaseID: "tc-g-bare", Status: vv.ResultPass},
			"tc-g-own":  {TestCaseID: "tc-g-own", Status: vv.ResultFail},
			"tc-g-tidy": {TestCaseID: "tc-g-tidy", Status: vv.ResultPass},
		},
	}}
	h := vvRoutesHandler(map[string]*projects.Project{
		"plane": {ID: "plane", OrgID: "org-1"},
		"gear":  {ID: "gear", OrgID: "org-1"},
		"solo":  {ID: "solo", OrgID: "org-1"},
	}, map[string]map[string]string{
		"plane": {"viewer": members.RoleViewer},
		"solo":  {"viewer": members.RoleViewer},
	}, exportSvc, nil, vvSvc)
	downloadSvc := downloads.NewService(exportSvc, h.reportService)
	downloadSvc.SetEvidenceSource(func(projectID string) (map[string]*vv.TestResult, []*vv.TestRun, error) {
		latest, err := vvSvc.LatestResults(projectID)
		return latest, nil, err
	})
	h.downloadService = downloadSvc
	return h, exportSvc
}

// flowDownDocsRequest is the viewer's GET of a project route.
func flowDownDocsRequest(projectID, query string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/x?"+query, nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "viewer"}))
	return mux.SetURLVars(r, map[string]string{"id": projectID})
}

// TestVVDocumentsApplyTheFlowDown: the V&V report PDF and a downloaded
// document with V&V status report a requirement refined from a child project
// as GET /vv/coverage and /vv/gaps do (OpenV REQ-146). A requirement with no
// test case of its own takes its refinements' rollup and is not a gap; one
// with evidence of its own reports the worse of its own and theirs; one with
// no verification method stays method-missing. Both documents computed their
// coverage from the snapshot alone, so the plane's REQ-1 was uncovered and
// listed under "Requirements without a test case", and REQ-2 passed.
func TestVVDocumentsApplyTheFlowDown(t *testing.T) {
	t.Run("the V&V report", func(t *testing.T) {
		h, _ := flowDownDocsHandler(t)
		w := httptest.NewRecorder()
		h.GetVVReport(w, flowDownDocsRequest("plane", ""))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		text := pdfShownText(t, w.Body.Bytes())
		wantSaid(t, "report", text,
			"Requirements: 4 pass: 1 fail: 1 uncovered: 1 method-missing: 1",
			"REQ-1 Brake within 2 m test pass",
			"REQ-2 Stop on ice test fail",
			"REQ-3 Look tidy on the apron - method-missing",
			"REQ-4 Log each stop test uncovered",
			"Requirements without a verification method (1) - REQ-3 Look tidy on the apron "+
				"Requirements without a test case (1) - REQ-4 Log each stop "+
				"Requirements with failing tests (1) - REQ-2 Stop on ice Test Runs")

		// Each row is what /vv/coverage answers the same viewer.
		cw := httptest.NewRecorder()
		h.GetCoverage(cw, flowDownDocsRequest("plane", ""))
		var coverage vv.CoverageReport
		if err := json.Unmarshal(cw.Body.Bytes(), &coverage); err != nil {
			t.Fatalf("coverage: %v (body %q)", err, cw.Body.String())
		}
		refs := map[string]string{"r-bare": "REQ-1", "r-own": "REQ-2", "r-nomethod": "REQ-3", "r-gap": "REQ-4"}
		var rows []string
		for _, e := range coverage.Entries {
			method := e.VerificationMethod
			if method == "" {
				method = "-"
			}
			rows = append(rows, strings.Join([]string{refs[e.RequirementID], e.Title, method, e.Rollup}, " "))
		}
		if len(rows) != 4 {
			t.Fatalf("coverage rows = %q, want the plane's four requirements", rows)
		}
		wantSaid(t, "report", text, rows...)
	})

	t.Run("a downloaded Word file with V&V status", func(t *testing.T) {
		h, _ := flowDownDocsHandler(t)
		w := httptest.NewRecorder()
		h.DownloadDOCX(w, flowDownDocsRequest("plane", "vv=1"))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		wantSaid(t, "document", docxBodyText(t, w.Body.Bytes()),
			"Requirements: 4 ■ pass: 1 ■ fail: 1 ■ uncovered: 1 ■ method missing: 1",
			"REQ-1 Brake within 2 m test pass",
			"REQ-2 Stop on ice test fail",
			"REQ-3 Look tidy on the apron - method missing",
			"REQ-4 Log each stop test uncovered",
			"Requirements without a verification method (1) REQ-3 Look tidy on the apron "+
				"Requirements without a test case (1) REQ-4 Log each stop "+
				"Requirements with failing tests (1) REQ-2 Stop on ice")
	})

	// A selection that leaves out the plane's test cases drops their
	// verifies links from the document, but not REQ-2's own failing test
	// from its V&V status: the flow-down is computed on the project as
	// loaded, so REQ-2 reports the worse of its own fail and its
	// refinement's pass, as /vv/coverage does, and is listed as failing.
	// Computed on the narrowed snapshot, REQ-2 had no evidence of its own
	// and passed on its refinement's result.
	t.Run("a narrowed document", func(t *testing.T) {
		for _, query := range []string{"template=requirements-review&vv=1", "vv=1&types=requirement"} {
			h, _ := flowDownDocsHandler(t)
			latest := h.vvService.(*fakeVVService).latest
			latest["plane"]["tc-own"].Status = vv.ResultFail
			latest["gear"]["tc-g-own"].Status = vv.ResultPass

			cw := httptest.NewRecorder()
			h.GetCoverage(cw, flowDownDocsRequest("plane", ""))
			var coverage vv.CoverageReport
			if err := json.Unmarshal(cw.Body.Bytes(), &coverage); err != nil {
				t.Fatalf("coverage: %v (body %q)", err, cw.Body.String())
			}
			for _, e := range coverage.Entries {
				if e.RequirementID == "r-own" && (e.Rollup != vv.RollupFail || e.FlowDown != vv.RollupPass) {
					t.Fatalf("coverage of REQ-2 = %+v, want its own fail over its refinement's pass", e)
				}
			}
			w := httptest.NewRecorder()
			h.DownloadDOCX(w, flowDownDocsRequest("plane", query))
			if w.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200 (body %q)", query, w.Code, w.Body.String())
			}
			text := docxBodyText(t, w.Body.Bytes())
			wantSaid(t, "document ("+query+")", text,
				"Requirements: 4 ■ pass: 1 ■ fail: 1 ■ uncovered: 1 ■ method missing: 1",
				"REQ-1 Brake within 2 m test pass",
				"REQ-2 Stop on ice test fail",
				"Requirements with failing tests (1) REQ-2 Stop on ice")
			if strings.Contains(text, "TC-1") {
				t.Errorf("%s: the document holds the plane's test case, so it narrows nothing: %s", query, text)
			}
		}
	})

	// The report takes no gate of its own, as /vv/coverage takes none, and a
	// project nothing in another project refines reads no other project and
	// reports what it did. A stable-channel workspace without the flow-down
	// feature can still hold a refines link (a managed link edit skips the
	// feature's gate, Q3), and its documents then report it as its
	// dashboard does.
	t.Run("a project nothing refines", func(t *testing.T) {
		h, exportSvc := flowDownDocsHandler(t)
		w := httptest.NewRecorder()
		h.GetVVReport(w, flowDownDocsRequest("solo", ""))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
		}
		wantSaid(t, "report", pdfShownText(t, w.Body.Bytes()),
			"Requirements: 1 uncovered: 1",
			"REQ-1 Stand alone test uncovered",
			"Requirements without a test case (1) - REQ-1 Stand alone")
		if got := strings.Join(exportSvc.exported, ","); got != "solo" {
			t.Errorf("exported %q, want the project alone", got)
		}
	})

	// A narrowed document of a project nothing refines, which covers every
	// stable-channel workspace without the flow-down feature, reports what it
	// did before the flow-down reached the documents: its requirements'
	// V&V status is computed from what the selection keeps. Here the
	// selection drops the passing test case that verifies solo's REQ-1, so
	// the document calls it uncovered, as it always has; only a requirement
	// another project refines takes its status from the project as loaded.
	t.Run("a narrowed document of a project nothing refines", func(t *testing.T) {
		h, exportSvc := flowDownDocsHandler(t)
		data, err := json.Marshal(exports.ProjectExport{
			ProjectID: "solo", ProjectName: "Solo",
			Artifacts: []*artifacts.Artifact{
				{ID: "s-r", ProjectID: "solo", Type: artifacts.TypeRequirement, Ref: "REQ-1", Title: "Stand alone",
					Status: "draft", Version: 1, Attributes: map[string]interface{}{"verification_method": "test"}},
				{ID: "s-tc", ProjectID: "solo", Type: artifacts.TypeTestCase, Ref: "TC-1", Title: "Stand it up",
					Status: "draft", Version: 1, Attributes: map[string]interface{}{}},
			},
			Links: []*links.Link{{ID: "s-tc-verifies-s-r", FromID: "s-tc", ToID: "s-r", Type: "verifies"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		exportSvc.byProject["solo"] = data
		h.vvService.(*fakeVVService).latest["solo"] = map[string]*vv.TestResult{
			"s-tc": {TestCaseID: "s-tc", Status: vv.ResultPass},
		}

		for _, c := range []struct{ query, want string }{
			{"vv=1", "REQ-1 Stand alone test pass"},
			{"vv=1&types=requirement", "REQ-1 Stand alone test uncovered"},
		} {
			w := httptest.NewRecorder()
			h.DownloadDOCX(w, flowDownDocsRequest("solo", c.query))
			if w.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200 (body %q)", c.query, w.Code, w.Body.String())
			}
			wantSaid(t, "document ("+c.query+")", docxBodyText(t, w.Body.Bytes()), c.want)
		}
	})
}

// TestFlowDownCountsAChildProjectReachedTwice: a child project whose
// requirements refine those of two projects of the flow-down, here G, which
// refines P directly and A under it, counts for both (OpenV REQ-146).
//
//	P  p1  test, no case; refined by A's a1
//	   p2  test, no case; refined by G's g2
//	A  a1  test, no case; refined by G's g1
//	G  g1  test, its own case passes
//	   g2  test, no case; refined by H's h1
//	H  h1  test, its own case passes
//
// Both of P's requirements pass. The walk marked each project it computed
// as seen for the rest of the request, so the second path to G skipped it
// and read G's refinements as uncovered: p1 or p2, by the order of a Go
// map, from one request to the next. Each project is still read once.
func TestFlowDownCountsAChildProjectReachedTwice(t *testing.T) {
	req := func(id, project string) *artifacts.Artifact {
		return &artifacts.Artifact{ID: id, ProjectID: project, Type: artifacts.TypeRequirement, Ref: "REQ-" + id,
			Title: "Requirement " + id, Status: "draft", Version: 1,
			Attributes: map[string]interface{}{"verification_method": "test"}}
	}
	tc := &artifacts.Artifact{ID: "tc", Type: artifacts.TypeTestCase, Status: "draft", Version: 1}
	far := func(a *artifacts.Artifact) *exports.LinkedArtifact {
		return &exports.LinkedArtifact{ID: a.ID, ProjectID: a.ProjectID, ProjectName: a.ProjectID, Ref: a.Ref,
			Type: a.Type, Title: a.Title, Status: a.Status}
	}
	link := func(from, typ, to string) *links.Link {
		return &links.Link{ID: from + "-" + typ + "-" + to, FromID: from, ToID: to, Type: typ}
	}
	p1, p2, a1, g1, g2, h1 := req("p1", "P"), req("p2", "P"), req("a1", "A"), req("g1", "G"), req("g2", "G"), req("h1", "H")
	tcG, tcH := *tc, *tc
	tcG.ID, tcG.ProjectID, tcH.ID, tcH.ProjectID = "tc-g1", "G", "tc-h1", "H"
	encode := func(e exports.ProjectExport) []byte {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("encode %s: %v", e.ProjectID, err)
		}
		return data
	}
	exportSvc := &fakeExportService{byProject: map[string][]byte{
		"P": encode(exports.ProjectExport{ProjectID: "P", Artifacts: []*artifacts.Artifact{p1, p2},
			Links:           []*links.Link{link("a1", "refines", "p1"), link("g2", "refines", "p2")},
			LinkedArtifacts: []*exports.LinkedArtifact{far(a1), far(g2)}}),
		"A": encode(exports.ProjectExport{ProjectID: "A", Artifacts: []*artifacts.Artifact{a1},
			Links:           []*links.Link{link("a1", "refines", "p1"), link("g1", "refines", "a1")},
			LinkedArtifacts: []*exports.LinkedArtifact{far(p1), far(g1)}}),
		"G": encode(exports.ProjectExport{ProjectID: "G", Artifacts: []*artifacts.Artifact{g1, g2, &tcG},
			Links: []*links.Link{link("tc-g1", "verifies", "g1"), link("g1", "refines", "a1"),
				link("g2", "refines", "p2"), link("h1", "refines", "g2")},
			LinkedArtifacts: []*exports.LinkedArtifact{far(a1), far(p2), far(h1)}}),
		"H": encode(exports.ProjectExport{ProjectID: "H", Artifacts: []*artifacts.Artifact{h1, &tcH},
			Links:           []*links.Link{link("tc-h1", "verifies", "h1"), link("h1", "refines", "g2")},
			LinkedArtifacts: []*exports.LinkedArtifact{far(g2)}}),
	}}
	h := vvRoutesHandler(map[string]*projects.Project{"P": {ID: "P", OrgID: "org-1"}},
		map[string]map[string]string{"P": {"viewer": members.RoleViewer}}, exportSvc, nil,
		&fakeVVService{latest: map[string]map[string]*vv.TestResult{
			"G": {"tc-g1": {TestCaseID: "tc-g1", Status: vv.ResultPass}},
			"H": {"tc-h1": {TestCaseID: "tc-h1", Status: vv.ResultPass}},
		}})

	// The order of a Go map changes from one request to the next, so ask
	// often enough that both orders of P's two child projects come up.
	for i := 0; i < 50; i++ {
		exportSvc.exported = nil
		w := httptest.NewRecorder()
		h.GetCoverage(w, flowDownDocsRequest("P", ""))
		var coverage vv.CoverageReport
		if err := json.Unmarshal(w.Body.Bytes(), &coverage); err != nil {
			t.Fatalf("coverage: %v (body %q)", err, w.Body.String())
		}
		for _, e := range coverage.Entries {
			if e.Rollup != vv.RollupPass || !e.ViaRefinements {
				t.Fatalf("request %d: %s = %s (flow-down %s), want a pass through its refinements; exported %v",
					i+1, e.RequirementID, e.Rollup, e.FlowDown, exportSvc.exported)
			}
		}
		read := map[string]int{}
		for _, id := range exportSvc.exported {
			read[id]++
		}
		if len(read) != 4 || read["P"] != 1 || read["A"] != 1 || read["G"] != 1 || read["H"] != 1 {
			t.Fatalf("request %d read %v, want P, A, G and H once each", i+1, exportSvc.exported)
		}
	}

	w := httptest.NewRecorder()
	h.GetVVReport(w, flowDownDocsRequest("P", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("report status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	wantSaid(t, "report", pdfShownText(t, w.Body.Bytes()), "Requirements: 2 pass: 2 ",
		"REQ-p1 Requirement p1 test pass", "REQ-p2 Requirement p2 test pass")

	// A loop of refinements between two projects (L's l2 refines M's m1,
	// which refines L's l1) still ends: a project still being computed
	// contributes nothing to the walk that reaches it again.
	l1, l2, m1 := req("l1", "L"), req("l2", "L"), req("m1", "M")
	exportSvc.byProject["L"] = encode(exports.ProjectExport{ProjectID: "L", Artifacts: []*artifacts.Artifact{l1, l2},
		Links:           []*links.Link{link("m1", "refines", "l1"), link("l2", "refines", "m1")},
		LinkedArtifacts: []*exports.LinkedArtifact{far(m1)}})
	exportSvc.byProject["M"] = encode(exports.ProjectExport{ProjectID: "M", Artifacts: []*artifacts.Artifact{m1},
		Links:           []*links.Link{link("m1", "refines", "l1"), link("l2", "refines", "m1")},
		LinkedArtifacts: []*exports.LinkedArtifact{far(l1), far(l2)}})
	h.projectService.(*fakeProjectService).byID["L"] = &projects.Project{ID: "L", OrgID: "org-1"}
	h.memberService.(*fakeMemberService).roles["L"] = map[string]string{"viewer": members.RoleViewer}
	w = httptest.NewRecorder()
	h.GetCoverage(w, flowDownDocsRequest("L", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"summary":{"uncovered":2}`) {
		t.Fatalf("a loop answered %d %s, want l1 and l2 uncovered", w.Code, w.Body.String())
	}
}

// wantSaid fails for each phrase the text does not hold, then shows the text
// once.
func wantSaid(t *testing.T, what, text string, phrases ...string) {
	t.Helper()
	missing := false
	for _, p := range phrases {
		if !strings.Contains(text, p) {
			t.Errorf("the %s does not say %q", what, p)
			missing = true
		}
	}
	if missing {
		t.Logf("the %s says: %s", what, text)
	}
}

var (
	// pdfStreamRE is one stream object's bytes: gofpdf writes the dictionary,
	// then "stream", the bytes and "endstream" on lines of their own.
	pdfStreamRE = regexp.MustCompile(`(?s)>>\s*stream\n(.*?)\nendstream`)
	// pdfShowRE is a literal string shown with Tj.
	pdfShowRE = regexp.MustCompile(`(?s)\(((?:[^\\()]|\\.)*)\)\s*Tj`)
)

// pdfShownText is the text a gofpdf document shows, in drawing order: the
// string of each Tj in its inflated content streams, joined with single
// spaces.
// gofpdf writes a string for an embedded UTF-8 font as UTF-16BE with \\, \(,
// \) and \r escaped.
func pdfShownText(t *testing.T, pdf []byte) string {
	t.Helper()
	var shown []string
	for _, stream := range pdfStreamRE.FindAllSubmatch(pdf, -1) {
		zr, err := zlib.NewReader(bytes.NewReader(stream[1]))
		if err != nil {
			continue
		}
		content, err := io.ReadAll(zr)
		if err != nil {
			continue
		}
		for _, m := range pdfShowRE.FindAllSubmatch(content, -1) {
			var raw []byte
			for i := 0; i < len(m[1]); i++ {
				c := m[1][i]
				if c == '\\' && i+1 < len(m[1]) {
					i++
					if c = m[1][i]; c == 'r' {
						c = '\r'
					}
				}
				raw = append(raw, c)
			}
			units := make([]uint16, 0, len(raw)/2)
			for i := 0; i+1 < len(raw); i += 2 {
				units = append(units, uint16(raw[i])<<8|uint16(raw[i+1]))
			}
			shown = append(shown, string(utf16.Decode(units)))
		}
	}
	if len(shown) == 0 {
		t.Fatal("the PDF shows no text")
	}
	return strings.Join(strings.Fields(strings.Join(shown, " ")), " ")
}

// docxBodyText is the text of a Word file's body: each run's text, joined
// with single spaces.
func docxBodyText(t *testing.T, docx []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		xml, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		var runs []string
		for _, m := range regexp.MustCompile(`<w:t(?: [^>]*)?>([^<]*)</w:t>`).FindAllSubmatch(xml, -1) {
			runs = append(runs, html.UnescapeString(string(m[1])))
		}
		return strings.Join(strings.Fields(strings.Join(runs, " ")), " ")
	}
	t.Fatal("no word/document.xml")
	return ""
}
