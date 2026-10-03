package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/downloads"
)

// TestFormatsGolden is refactor plan step S9's format golden (invariant
// I15; OpenV REQ-143, REQ-6): every export, report and download format the
// API offers, rendered from one fixture with a pinned clock
// (formats_fixture_test.go) through the real handler, snapshot load and
// renderer, and written to testdata/formats/<name>.txt with the answer's
// status, Content-Type and Content-Disposition (the media type and the
// filename) and its body (formats_views_test.go): JSON, CSV and ReqIF
// byte for byte, a workbook's cells with their style ids, each PDF's page
// count and per-page text, a Word document's document.xml, an archive's
// entries.
//
// The routes: GET /projects/{id}/export in its four formats; the download
// options and the six downloads, of the live project, of a baseline, and
// narrowed by a selection, and the figures bundled with the JSON; the
// legacy report in both formats, live and from a baseline; the V&V report,
// live and from a baseline, whose filename is not quoted (pinned here);
// and a crew's export, GET /crews/{id}/export. The test also fails when a
// download format (downloads.Formats) or an export format (an
// exports.ExportFormat constant) has no case, or when a route of the
// inventory (testdata/routes.txt) that ends in export, report or download,
// or lies under /download/, has none and is not exempt
// (s9FormatRoutesExempt), so a new format or document route needs a golden
// from the change that adds it; and when a golden under testdata/formats/
// has no case, so a case cannot be dropped while its golden stays behind.
//
// Regenerate, for a deliberate change only, with
//
//	UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^(TestFormatsGolden|TestProposalPayloadsGolden)$'
//
// Only the value 1 regenerates; any other value compares.
func TestFormatsGolden(t *testing.T) {
	f := newS9Fixture(t)
	p := "/api/v1/projects/" + s9P
	narrowed := url.Values{
		"sections":     {s9Heading1},
		"types":        {"requirement,test-case"},
		"headings":     {"0"},
		"fields":       {"priority,risk"},
		"results":      {"1"},
		"toc":          {"0"},
		"traceability": {"1"},
	}.Encode()
	cases := []struct{ name, path string }{
		{"export_json", p + "/export"},
		{"export_csv", p + "/export?format=csv"},
		{"export_excel", p + "/export?format=excel"},
		{"export_reqif", p + "/export?format=reqif"},

		{"download_options", p + "/download/options"},
		{"download_json", p + "/download/json"},
		{"download_csv", p + "/download/csv"},
		{"download_excel", p + "/download/excel"},
		{"download_reqif", p + "/download/reqif"},
		{"download_pdf", p + "/download/pdf"},
		{"download_docx", p + "/download/docx"},
		{"download_json_figures", p + "/download/json?attachments=figures,documents,drawings"},

		{"download_options_baseline", p + "/download/options?baseline_id=" + s9B1},
		{"download_json_baseline", p + "/download/json?baseline_id=" + s9B1},
		{"download_csv_baseline", p + "/download/csv?baseline_id=" + s9B1},
		{"download_excel_baseline", p + "/download/excel?baseline_id=" + s9B1},
		{"download_reqif_baseline", p + "/download/reqif?baseline_id=" + s9B1},
		{"download_reqif_baseline_without_definitions", p + "/download/reqif?baseline_id=" + s9B0},
		{"download_pdf_baseline", p + "/download/pdf?baseline_id=" + s9B1},
		{"download_docx_baseline", p + "/download/docx?baseline_id=" + s9B1},

		{"download_pdf_narrowed", p + "/download/pdf?" + narrowed},
		{"download_docx_narrowed", p + "/download/docx?" + narrowed},
		{"download_excel_narrowed", p + "/download/excel?" + narrowed},
		{"download_pdf_template", p + "/download/pdf?template=test-planning"},

		{"report_pdf", p + "/report"},
		{"report_docx", p + "/report?format=docx"},
		{"report_pdf_baseline", p + "/report?baseline_id=" + s9B1},
		{"report_docx_baseline", p + "/report?format=docx&baseline_id=" + s9B1},

		{"vv_report", p + "/vv/report"},
		{"vv_report_baseline", p + "/vv/report?baseline_id=" + s9B1},

		{"crew_export", "/api/v1/crews/" + s9Crew + "/export"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			checkS9Golden(t, s9GoldenDir, c.name+".txt", f.get(t, c.path).view)
		})
	}

	// Every download format has its golden, and so does every route that
	// renders a document, by the route inventory (testdata/routes.txt), but
	// those that serve stored bytes or are no document. What the cases
	// cover is read from the cases themselves, each path matched against
	// the router, not from the subtests that ran, so -run naming one of
	// them checks the same coverage.
	names, covered := map[string]bool{}, map[string]bool{}
	for _, c := range cases {
		names[c.name] = true
		covered[f.route(t, c.path)] = true
	}
	// A golden whose case is gone would pin nothing, and regenerating
	// deletes no file, so this holds under UPDATE_GOLDEN=1 too.
	checkS9Orphans(t, s9GoldenDir, names, "case of TestFormatsGolden")
	for _, format := range downloads.Formats {
		if !names["download_"+string(format)] {
			t.Errorf("download format %q has no golden: add a download_%s case to TestFormatsGolden", format, format)
		}
	}
	for _, format := range s9ExportFormats(t) {
		if !names["export_"+format] {
			t.Errorf("export format %q (exports.ExportFormat) has no golden: add an export_%s case to TestFormatsGolden",
				format, format)
		}
	}
	routes, err := os.ReadFile("testdata/routes.txt")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, route := range strings.Split(strings.TrimSpace(string(routes)), "\n") {
		method, path, _ := strings.Cut(route, " ")
		last := path[strings.LastIndex(path, "/")+1:]
		if (method != "GET" && method != "HEAD") || !(last == "export" || last == "report" || last == "download" ||
			strings.Contains(path, "/download/")) {
			continue
		}
		seen[route] = true
		why, exempt := s9FormatRoutesExempt[route]
		switch {
		case exempt && covered[route]:
			t.Errorf("%s is exempt (%s) but has a golden: drop the exemption", route, why)
		case !exempt && !covered[route]:
			t.Errorf("%s renders a document no golden covers: add a case to TestFormatsGolden, "+
				"or an entry to s9FormatRoutesExempt saying why it needs none", route)
		}
	}
	for route := range s9FormatRoutesExempt {
		if !seen[route] {
			t.Errorf("s9FormatRoutesExempt names %s, which is not a route: drop it", route)
		}
	}
}

// s9FormatRoutesExempt are the routes that look like a download or an
// export but render no document, each with why.
var s9FormatRoutesExempt = map[string]string{
	"GET /api/v1/attachments/{id}/download":    "an upload's bytes as stored (S5a pins it, Range requests included)",
	"GET /api/v1/evidence-files/{id}/download": "an evidence file's bytes as stored (S5b pins it)",
	"GET /api/v1/public/connector/download":    "the Agent Connector binary (S5c pins the answer)",
	"HEAD /api/v1/public/connector/download":   "the Agent Connector binary's headers (S5c)",
	"GET /api/v1/teams/{id}/export": "the deprecated alias of GET /api/v1/crews/{id}/export, bound to the same " +
		"handler (S2's route_handlers.txt)",
}

// s9ExportsDir is the export service's package, whose ExportFormat
// constants GET /projects/{id}/export?format= serves.
const s9ExportsDir = "../domain/exports"

// s9ExportFormats lists the values of the ExportFormat constants declared
// in the export service's package (not its tests), read from the source
// since the package keeps no list of them, so a format added there needs an
// export_<format> case. It fails when it finds none: the type has moved,
// and the scan with it.
func s9ExportFormats(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(s9ExportsDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "ExportFormat" {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: an ExportFormat constant that is not a string literal: update s9ExportFormats",
							fset.Position(v.Pos()))
					}
					s, _ := strconv.Unquote(lit.Value)
					out = append(out, s)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no ExportFormat constants under internal/domain/exports: if the type moved, point s9ExportsDir at it")
	}
	sort.Strings(out)
	return out
}

// s9Answer is one answer and its golden view.
type s9Answer struct {
	status int
	header map[string]string
	body   []byte
	clock  s9Clock
	view   string
}

// route names the route the router matches for a GET of path, as the
// route inventory writes it: GET /api/v1/projects/{id}/export.
func (f *s9Fixture) route(t *testing.T, path string) string {
	t.Helper()
	var match mux.RouteMatch
	if !f.router.Match(s9Request(path), &match) || match.Route == nil {
		t.Fatalf("GET %s matches no route", path)
	}
	tmpl, _ := match.Route.GetPathTemplate()
	return "GET " + tmpl
}

// get serves a GET through the router and returns the answer with its
// view. A non-200 fails the test: every request here is one that succeeds.
func (f *s9Fixture) get(t *testing.T, path string) s9Answer {
	t.Helper()
	w := httptest.NewRecorder()
	from := time.Now()
	f.router.ServeHTTP(w, s9Request(path))
	c := s9Clock{from: from, to: time.Now(), uploads: f.d.uploads}
	if w.Code != 200 {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
	}
	a := s9Answer{status: w.Code, body: w.Body.Bytes(), clock: c, header: map[string]string{
		"Content-Type":        w.Header().Get("Content-Type"),
		"Content-Disposition": w.Header().Get("Content-Disposition"),
	}}
	a.view = s9View(t, c, "GET", s9ShowIDs(path), a.status, a.header, a.body)
	return a
}

// s9ShowIDs names the fixture's ids in a request path, so a golden reads
// /projects/<P>/download/pdf?baseline_id=<B1>.
func s9ShowIDs(path string) string {
	return strings.NewReplacer(s9P, "<P>", s9C, "<C>", s9B1, "<B1>", s9B0, "<B0>", s9Crew, "<crew>",
		s9Heading1, "<HDG-1>").Replace(path)
}

var s9ReqIFEnum = regexp.MustCompile(`<ATTRIBUTE-DEFINITION-ENUMERATION [^>]*LONG-NAME="([^"]*)"`)

// TestReqIFExportAndDownloadTypeAlike pins what the R7 fix for #379's
// decision on REQ-6 (#422) made true (invariant I15): the ReqIF export
// (GET /projects/{id}/export?format=reqif) and the ReqIF download
// (GET /projects/{id}/download/reqif) of the live project type an enum
// attribute alike, by the definitions exports.Service.Definitions answers,
// and are the same document apart from the clock; a baseline's download
// types it by the definitions the baseline kept, and a baseline taken
// before baselines kept them leaves it a string. X14b moves the loads these
// documents come from (snapshot.Load with WithAttributeDefs for the export,
// the definitions after the load for the download); this test fails if the
// two diverge again.
func TestReqIFExportAndDownloadTypeAlike(t *testing.T) {
	f := newS9Fixture(t)
	p := "/api/v1/projects/" + s9P
	export := f.get(t, p+"/export?format=reqif")
	download := f.get(t, p+"/download/reqif")
	exportBody, downloadBody := export.clock.text(string(export.body)), download.clock.text(string(download.body))
	if exportBody != downloadBody {
		t.Errorf("the ReqIF export and the ReqIF download of the live project differ beyond the clock "+
			"(- the export, + the download; a line number is the export's):\n%s", s9Diff(exportBody, downloadBody))
	}
	enums := func(body string) []string {
		var out []string
		for _, m := range s9ReqIFEnum.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	for _, c := range []struct {
		name, body string
		want       string
	}{
		{"export", exportBody, "[Risk]"},
		{"download", downloadBody, "[Risk]"},
		{"baseline download", string(f.get(t, p+"/download/reqif?baseline_id="+s9B1).body), "[Risk]"},
		{"download of a baseline without definitions", string(f.get(t, p+"/download/reqif?baseline_id="+s9B0).body), "[]"},
	} {
		if got := "[" + strings.Join(enums(c.body), " ") + "]"; got != c.want {
			t.Errorf("the %s types these attributes as ReqIF enumerations: %s, want %s", c.name, got, c.want)
		}
	}
}
