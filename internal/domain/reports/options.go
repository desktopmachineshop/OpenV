package reports

import (
	"fmt"
	"time"

	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// Snapshot says what a document was rendered from, so the document can say
// so itself: a named baseline with its id and capture time, or the live
// project as it stood at the moment of export. A reader holding a printed
// specification must be able to tell which without asking.
type Snapshot struct {
	BaselineID   string
	BaselineName string
	// CapturedAt is when the baseline was taken; zero for a live export.
	CapturedAt time.Time
	// ExportedAt is when the document was rendered.
	ExportedAt time.Time
}

// IsBaseline reports whether the snapshot is a captured baseline.
func (s Snapshot) IsBaseline() bool { return s.BaselineID != "" }

// Statement is the sentence the cover prints.
func (s Snapshot) Statement() string {
	if s.IsBaseline() {
		when := ""
		if !s.CapturedAt.IsZero() {
			when = " captured " + s.CapturedAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		return fmt.Sprintf("Snapshot: baseline %q (id %s)%s.", s.BaselineName, s.BaselineID, when)
	}
	return fmt.Sprintf("Live project state as of %s. This is not a baseline: the project may have changed since.", s.ExportedAt.UTC().Format("2006-01-02 15:04 UTC"))
}

// Label is the short form for a footer.
func (s Snapshot) Label() string {
	if s.IsBaseline() {
		return "Baseline " + s.BaselineName
	}
	return "Live " + s.ExportedAt.UTC().Format("2006-01-02 15:04 UTC")
}

// Workspace is what the cover shows of the organisation the project belongs
// to. Logo holds the image bytes as uploaded; the renderers decode it.
type Workspace struct {
	Name     string
	Logo     []byte
	LogoMime string
}

// RenderOptions is everything a renderer needs beside the narrowed snapshot.
type RenderOptions struct {
	Snapshot  Snapshot
	Content   exports.Content
	Workspace Workspace
	// Latest holds the latest result per test case and Runs the project's
	// test runs; both are nil unless Content asks for evidence.
	Latest map[string]*vv.TestResult
	Runs   []*vv.TestRun
	// Coverage computes the V&V status when Content asks for it, from the
	// narrowed snapshot and Latest; nil reads the snapshot alone.
	Coverage CoverageFunc
	// Author is written to the document's metadata.
	Author string
}

// CoverageFunc computes the verification coverage a document reports from
// the snapshot it renders and the latest result per test case.
// vv.ComputeCoverage reads the snapshot alone; the API passes the coverage
// GET /vv/coverage answers, which rolls up the verification of the other
// projects' requirements that refine the snapshot's (REQ-146), so a document
// reports such a requirement as the V&V dashboard does. A download computes
// that flow-down on the snapshot before its selection narrows it
// (downloads.coverageAsLoaded).
type CoverageFunc func(data *exports.ProjectExport, latest map[string]*vv.TestResult) *vv.CoverageReport

// compute is f's coverage, or vv.ComputeCoverage's when f is nil.
func (f CoverageFunc) compute(data *exports.ProjectExport, latest map[string]*vv.TestResult) *vv.CoverageReport {
	if f == nil {
		return vv.ComputeCoverage(data, latest)
	}
	return f(data, latest)
}

// defaultRenderOptions is what the legacy report route gets: the
// specification document as that route has always rendered it, without the
// V&V status a download carries by default. The route reads no test evidence
// (a download does, through its EvidenceSource), and a status computed
// without it would report every tested requirement as never run.
func defaultRenderOptions(snapshot Snapshot) RenderOptions {
	content := exports.DefaultContent()
	content.VVStatus = false
	return RenderOptions{Snapshot: snapshot, Content: content}
}
