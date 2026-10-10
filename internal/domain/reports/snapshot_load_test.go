package reports

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// These tests pin how the report service reads the snapshot it renders, in
// its two loaders: loadReportExport (behind LoadReportExport,
// GenerateProjectReport and GenerateProjectReportDOCX) and GenerateVVReport's.
// They are the characterization of X14c, which moves both onto
// snapshot.Load: which source is read, the error text and identity of each
// failure, and the Snapshot a loader hands back, whatever it returns. They
// hold the baseline and export services as fakes, so they read no database.

const (
	liveJSON     = `{"project_name":"Live project"}`
	baselineJSON = `{"project_name":"Frozen project"}`
	badJSON      = `{not json`
	wrongTypes   = `{"artifacts": 5}`
)

var capturedAt = time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)

// loadBaselines is the baseline service the loaders read: one baseline, or
// the error it is made to fail with. It records every read.
type loadBaselines struct {
	baselines.Service
	baseline *baselines.Baseline
	err      error
	reads    [][2]string
}

func (f *loadBaselines) GetProjectBaseline(projectID, id string) (*baselines.Baseline, error) {
	f.reads = append(f.reads, [2]string{projectID, id})
	if f.err != nil {
		return nil, f.err
	}
	return f.baseline, nil
}

// loadExports is the export service the loaders read: the live JSON export,
// or the error it is made to fail with. It records every read.
type loadExports struct {
	exports.Service
	live  string
	err   error
	reads []string
}

func (f *loadExports) ExportProject(projectID string, format exports.ExportFormat) ([]byte, string, error) {
	f.reads = append(f.reads, projectID+" "+string(format))
	if f.err != nil {
		return nil, "", f.err
	}
	return []byte(f.live), "", nil
}

func newLoadService(live, snapshot string) (*DefaultService, *loadBaselines, *loadExports) {
	b := &loadBaselines{baseline: &baselines.Baseline{
		ID: "b-1", ProjectID: "p-1", Name: "Release 1", Snapshot: json.RawMessage(snapshot), CreatedAt: capturedAt,
	}}
	e := &loadExports{live: live}
	return NewService(e, b), b, e
}

// loader is a way into the snapshot load: all three report the failure the
// load returned, and LoadReportExport also the Snapshot.
type loader struct {
	name string
	load func(s *DefaultService, baselineID string) (*exports.ProjectExport, error)
}

var loaders = []loader{
	{"LoadReportExport", func(s *DefaultService, baselineID string) (*exports.ProjectExport, error) {
		data, _, err := s.LoadReportExport("p-1", baselineID)
		return data, err
	}},
	{"GenerateProjectReport", func(s *DefaultService, baselineID string) (*exports.ProjectExport, error) {
		pdf, name, err := s.GenerateProjectReport("p-1", baselineID)
		if err != nil && (pdf != nil || name != "") {
			panic("a failed report returns no document")
		}
		return nil, err
	}},
	{"GenerateProjectReportDOCX", func(s *DefaultService, baselineID string) (*exports.ProjectExport, error) {
		docx, name, err := s.GenerateProjectReportDOCX("p-1", baselineID)
		if err != nil && (docx != nil || name != "") {
			panic("a failed report returns no document")
		}
		return nil, err
	}},
	{"GenerateVVReport", func(s *DefaultService, baselineID string) (*exports.ProjectExport, error) {
		pdf, name, err := s.GenerateVVReport("p-1", baselineID, nil, nil, nil)
		if err != nil && (pdf != nil || name != "") {
			panic("a failed report returns no document")
		}
		return nil, err
	}},
}

// jsonError is the error encoding/json gives for text, to compare the loaders'
// wrapped error with.
func jsonError(t *testing.T, text string) error {
	t.Helper()
	var into exports.ProjectExport
	err := json.Unmarshal([]byte(text), &into)
	if err == nil {
		t.Fatalf("%q decodes; the test needs JSON that does not", text)
	}
	return err
}

// assertDecodeError checks a loader's decode failure: prefix, then the text
// of encoding/json's own error, which the error wraps.
func assertDecodeError(t *testing.T, err error, prefix, text string) {
	t.Helper()
	want := jsonError(t, text)
	if err == nil {
		t.Fatalf("no error; want %q", prefix+want.Error())
	}
	if got := err.Error(); got != prefix+want.Error() {
		t.Fatalf("error = %q; want %q", got, prefix+want.Error())
	}
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	switch want.(type) {
	case *json.SyntaxError:
		if !errors.As(err, &syntax) {
			t.Fatalf("error %q does not wrap a *json.SyntaxError", err)
		}
	case *json.UnmarshalTypeError:
		if !errors.As(err, &typed) {
			t.Fatalf("error %q does not wrap a *json.UnmarshalTypeError", err)
		}
	}
}

func TestBaselineDecodeFailureNamesTheBaselineSnapshot(t *testing.T) {
	for _, text := range []string{badJSON, wrongTypes} {
		for _, l := range loaders {
			s, _, e := newLoadService(liveJSON, text)
			data, err := l.load(s, "b-1")
			assertDecodeError(t, err, "failed to parse baseline snapshot: ", text)
			if data != nil {
				t.Errorf("%s with %s: data = %v; want nil", l.name, text, data)
			}
			if len(e.reads) != 0 {
				t.Errorf("%s: a baseline read the live export: %v", l.name, e.reads)
			}
		}
	}
}

func TestBaselineDecodeFailureKeepsTheBaselineInTheSnapshot(t *testing.T) {
	s, _, _ := newLoadService(liveJSON, badJSON)
	before := time.Now()
	data, snap, err := s.LoadReportExport("p-1", "b-1")
	after := time.Now()
	if err == nil || data != nil {
		t.Fatalf("data, err = %v, %v; want nil and an error", data, err)
	}
	want := Snapshot{BaselineID: "b-1", BaselineName: "Release 1", CapturedAt: capturedAt}
	assertSnapshot(t, snap, want, before, after)
}

func TestLiveDecodeFailureNamesTheExportData(t *testing.T) {
	for _, text := range []string{badJSON, wrongTypes} {
		for _, l := range loaders {
			s, b, e := newLoadService(text, baselineJSON)
			data, err := l.load(s, "")
			assertDecodeError(t, err, "failed to parse export data: ", text)
			if data != nil {
				t.Errorf("%s with %s: data = %v; want nil", l.name, text, data)
			}
			if len(b.reads) != 0 || len(e.reads) != 1 {
				t.Errorf("%s: reads = baselines %v, exports %v; want the live export alone", l.name, b.reads, e.reads)
			}
		}
	}
}

func TestLiveDecodeFailureLeavesAnUnnamedSnapshot(t *testing.T) {
	s, _, _ := newLoadService(badJSON, baselineJSON)
	before := time.Now()
	data, snap, err := s.LoadReportExport("p-1", "")
	after := time.Now()
	if err == nil || data != nil {
		t.Fatalf("data, err = %v, %v; want nil and an error", data, err)
	}
	assertSnapshot(t, snap, Snapshot{}, before, after)
}

func TestBaselineReadErrorPassesThrough(t *testing.T) {
	for _, l := range loaders {
		s, b, e := newLoadService(liveJSON, baselineJSON)
		b.err = baselines.ErrNotFound
		_, err := l.load(s, "b-other")
		if err != baselines.ErrNotFound {
			t.Errorf("%s: err = %v; want baselines.ErrNotFound as it is", l.name, err)
		}
		if len(b.reads) != 1 || b.reads[0] != [2]string{"p-1", "b-other"} {
			t.Errorf("%s: baseline reads = %v; want one, scoped to the project", l.name, b.reads)
		}
		if len(e.reads) != 0 {
			t.Errorf("%s: a baseline read the live export: %v", l.name, e.reads)
		}
	}
	s, b, _ := newLoadService(liveJSON, baselineJSON)
	b.err = errors.New("baseline store down")
	before := time.Now()
	data, snap, err := s.LoadReportExport("p-1", "b-1")
	after := time.Now()
	if err != b.err || data != nil {
		t.Fatalf("data, err = %v, %v; want nil and the store's error as it is", data, err)
	}
	// A failed read names no baseline: only the moment of the load.
	assertSnapshot(t, snap, Snapshot{}, before, after)
}

func TestExportErrorPassesThrough(t *testing.T) {
	for _, l := range loaders {
		s, b, e := newLoadService(liveJSON, baselineJSON)
		e.err = errors.New("export failed")
		_, err := l.load(s, "")
		if err != e.err {
			t.Errorf("%s: err = %v; want the export service's error as it is", l.name, err)
		}
		if len(b.reads) != 0 {
			t.Errorf("%s: a live read read a baseline: %v", l.name, b.reads)
		}
	}
	s, _, e := newLoadService(liveJSON, baselineJSON)
	e.err = errors.New("export failed")
	before := time.Now()
	data, snap, err := s.LoadReportExport("p-1", "")
	after := time.Now()
	if err != e.err || data != nil {
		t.Fatalf("data, err = %v, %v; want nil and the export's error as it is", data, err)
	}
	assertSnapshot(t, snap, Snapshot{}, before, after)
}

func TestLiveIsTheEmptyBaselineID(t *testing.T) {
	for _, l := range loaders {
		for _, id := range []string{"", "live"} {
			s, b, e := newLoadService(liveJSON, baselineJSON)
			if _, err := l.load(s, id); err != nil && l.name == "LoadReportExport" {
				t.Fatalf("%s(%q): %v", l.name, id, err)
			}
			if len(b.reads) != 0 {
				t.Errorf("%s(%q) read a baseline: %v", l.name, id, b.reads)
			}
			if len(e.reads) != 1 || e.reads[0] != "p-1 json" {
				t.Errorf("%s(%q) export reads = %v; want the live JSON export of the project", l.name, id, e.reads)
			}
		}
	}
	// A bad live export fails alike under both names.
	for _, id := range []string{"", "live"} {
		s, _, _ := newLoadService(badJSON, baselineJSON)
		_, _, err := s.LoadReportExport("p-1", id)
		assertDecodeError(t, err, "failed to parse export data: ", badJSON)
	}
}

func TestLiveLoadSnapshot(t *testing.T) {
	s, b, e := newLoadService(liveJSON, baselineJSON)
	before := time.Now()
	data, snap, err := s.LoadReportExport("p-1", "")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if data == nil || data.ProjectName != "Live project" {
		t.Fatalf("data = %+v; want the live export", data)
	}
	assertSnapshot(t, snap, Snapshot{}, before, after)
	if snap.IsBaseline() {
		t.Error("a live snapshot is not a baseline")
	}
	if len(b.reads) != 0 || len(e.reads) != 1 {
		t.Errorf("reads = baselines %v, exports %v; want the live export alone", b.reads, e.reads)
	}
}

func TestBaselineLoadSnapshot(t *testing.T) {
	s, b, e := newLoadService(liveJSON, baselineJSON)
	before := time.Now()
	data, snap, err := s.LoadReportExport("p-1", "b-1")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if data == nil || data.ProjectName != "Frozen project" {
		t.Fatalf("data = %+v; want the baseline's snapshot", data)
	}
	assertSnapshot(t, snap, Snapshot{BaselineID: "b-1", BaselineName: "Release 1", CapturedAt: capturedAt}, before, after)
	if !snap.IsBaseline() {
		t.Error("a baseline snapshot is a baseline")
	}
	if len(b.reads) != 1 || b.reads[0] != [2]string{"p-1", "b-1"} || len(e.reads) != 0 {
		t.Errorf("reads = baselines %v, exports %v; want the one baseline alone", b.reads, e.reads)
	}
}

// assertSnapshot compares everything of a Snapshot but ExportedAt with want,
// and checks ExportedAt was taken while the load ran.
func assertSnapshot(t *testing.T, got, want Snapshot, before, after time.Time) {
	t.Helper()
	if got.ExportedAt.Before(before) || got.ExportedAt.After(after) {
		t.Errorf("ExportedAt = %v; want it between %v and %v", got.ExportedAt, before, after)
	}
	got.ExportedAt = time.Time{}
	if got != want {
		t.Errorf("snapshot = %+v; want %+v", got, want)
	}
}

// vvCoverage is a coverage function that records the snapshot GenerateVVReport
// handed it, and computes what the default would.
type vvCoverage struct{ seen []*exports.ProjectExport }

func (c *vvCoverage) compute(data *exports.ProjectExport, latest map[string]*vv.TestResult) *vv.CoverageReport {
	c.seen = append(c.seen, data)
	return vv.ComputeCoverage(data, latest)
}

func TestVVReportRequiresAProject(t *testing.T) {
	s, b, e := newLoadService(liveJSON, baselineJSON)
	_, _, err := s.GenerateVVReport("", "", nil, nil, nil)
	if err == nil || err.Error() != "project_id is required" {
		t.Fatalf("err = %v; want project_id is required", err)
	}
	if len(b.reads)+len(e.reads) != 0 {
		t.Errorf("a report without a project read %v %v", b.reads, e.reads)
	}
}

func TestVVReportReadsTheSnapshotItReports(t *testing.T) {
	for _, tc := range []struct {
		id      string
		project string
	}{{"", "Live project"}, {"live", "Live project"}, {"b-1", "Frozen project"}} {
		s, _, _ := newLoadService(liveJSON, baselineJSON)
		var c vvCoverage
		pdf, filename, err := s.GenerateVVReport("p-1", tc.id, nil, nil, c.compute)
		if err != nil {
			t.Fatalf("%q: %v", tc.id, err)
		}
		if len(c.seen) != 1 || c.seen[0].ProjectName != tc.project {
			t.Errorf("%q: coverage computed over %v; want the %s snapshot", tc.id, c.seen, tc.project)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Errorf("%q: not a PDF", tc.id)
		}
		stem := "vv-report-" + strings.ReplaceAll(tc.project, " ", "_") + "-"
		day := time.Now().Format("20060102")
		if filename != stem+day+".pdf" && filename != stem+time.Now().Add(-time.Second).Format("20060102")+".pdf" {
			t.Errorf("%q: filename = %q; want %s%s.pdf", tc.id, filename, stem, day)
		}
	}
}

func TestVVReportFailureRunsNoCoverage(t *testing.T) {
	var c vvCoverage
	for _, tc := range []struct {
		name        string
		live, base  string
		baselineErr error
		exportErr   error
		id          string
		decodes     string // the prefix of the decode failure the load must give
	}{
		{name: "baseline decode", base: badJSON, id: "b-1", decodes: "failed to parse baseline snapshot: "},
		{name: "live decode", live: badJSON, id: "", decodes: "failed to parse export data: "},
		{name: "baseline missing", baselineErr: baselines.ErrNotFound, id: "b-9"},
		{name: "export failure", exportErr: errors.New("export failed"), id: ""},
	} {
		s, b, e := newLoadService(tc.live, tc.base)
		b.err, e.err = tc.baselineErr, tc.exportErr
		_, _, err := s.GenerateVVReport("p-1", tc.id, nil, nil, c.compute)
		switch {
		case tc.decodes != "":
			assertDecodeError(t, err, tc.decodes, badJSON)
		case tc.baselineErr != nil:
			if err != tc.baselineErr {
				t.Errorf("%s: err = %v; want %v as it is", tc.name, err, tc.baselineErr)
			}
		default:
			if err != tc.exportErr {
				t.Errorf("%s: err = %v; want %v as it is", tc.name, err, tc.exportErr)
			}
		}
	}
	if len(c.seen) != 0 {
		t.Errorf("coverage computed after a failed load: %v", c.seen)
	}
}

// pdfStreams is what a PDF draws, in a form that does not depend on the order
// gofpdf writes its objects in (it ranges over maps, so two runs of the same
// report put the fonts in a different order): every stream of it inflated, and
// the lot sorted. The dates it stamps in its Info dictionary are no stream.
func pdfStreams(t *testing.T, pdf []byte) string {
	t.Helper()
	var streams []string
	for rest := pdf; ; {
		i := bytes.Index(rest, []byte("stream\n"))
		if i < 0 {
			break
		}
		rest = rest[i+len("stream\n"):]
		j := bytes.Index(rest, []byte("endstream"))
		if j < 0 {
			t.Fatal("a PDF stream with no end")
		}
		body := rest[:j]
		rest = rest[j+len("endstream"):]
		if r, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			if inflated, err := io.ReadAll(r); err == nil {
				body = inflated
			}
		}
		streams = append(streams, string(body))
	}
	sort.Strings(streams)
	return strings.Join(streams, "\x00")
}

// stableVVPDF generates the V&V report until two runs agree on what they draw,
// so a clock that ticks between them (the minute on its header) does not make
// the comparison flaky.
func stableVVPDF(t *testing.T, s *DefaultService, baselineID string) string {
	t.Helper()
	var last string
	for i := 0; i < 8; i++ {
		pdf, _, err := s.GenerateVVReport("p-1", baselineID, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		drawn := pdfStreams(t, pdf)
		if drawn == last {
			return drawn
		}
		last = drawn
	}
	t.Fatal("the V&V report never came out the same twice")
	return ""
}

// TestVVReportNamesTheBaselineItReads: the baseline's name is the one thing
// of it the V&V report prints, so a report of a baseline differs from the
// live one over the same snapshot, and one name from another.
func TestVVReportNamesTheBaselineItReads(t *testing.T) {
	s, b, _ := newLoadService(baselineJSON, baselineJSON)
	live := stableVVPDF(t, s, "")
	b.baseline.Name = "Release 1"
	one := stableVVPDF(t, s, "b-1")
	b.baseline.Name = "Release 2"
	two := stableVVPDF(t, s, "b-1")
	if live == one {
		t.Error("a baseline's V&V report prints no baseline line")
	}
	if one == two {
		t.Error("a baseline's V&V report does not print its name")
	}
}
