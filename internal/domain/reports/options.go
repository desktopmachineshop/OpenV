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
	// Author is written to the document's metadata.
	Author string
}

// defaultRenderOptions is what the legacy report route and a caller with no
// preference get: the specification document.
func defaultRenderOptions(snapshot Snapshot) RenderOptions {
	return RenderOptions{Snapshot: snapshot, Content: exports.DefaultContent()}
}
