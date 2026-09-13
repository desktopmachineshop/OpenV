package vv

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// A plane project with three requirements refined from a landing-gear
// project: one with tests of its own, one with nothing of its own, and one
// with no verification method at all.
func flowDownExport() *exports.ProjectExport {
	req := func(id, method string) *artifacts.Artifact {
		a := &artifacts.Artifact{ID: id, ProjectID: "plane", Type: "requirement", Title: id, Attributes: map[string]interface{}{}}
		if method != "" {
			a.Attributes["verification_method"] = method
		}
		return a
	}
	return &exports.ProjectExport{
		ProjectID: "plane",
		Artifacts: []*artifacts.Artifact{req("own", "test"), req("bare", "test"), req("nomethod", ""), {ID: "tc", ProjectID: "plane", Type: "test-case"}},
		Links: []*links.Link{
			{FromID: "tc", ToID: "own", Type: "verifies"},
			{FromID: "g1", ToID: "own", Type: "refines"},
			{FromID: "g2", ToID: "bare", Type: "refines"},
			{FromID: "g3", ToID: "bare", Type: "refines"},
			{FromID: "g4", ToID: "nomethod", Type: "refines"},
			{FromID: "local", ToID: "bare", Type: "refines"},
		},
		LinkedArtifacts: []*exports.LinkedArtifact{
			{ID: "g1", ProjectID: "gear", ProjectName: "Landing gear", Ref: "REQ-1", Type: "requirement"},
			{ID: "g2", ProjectID: "gear", ProjectName: "Landing gear", Ref: "REQ-2", Type: "requirement"},
			{ID: "g3", ProjectID: "gear", ProjectName: "Landing gear", Ref: "REQ-3", Type: "requirement"},
			{ID: "g4", ProjectID: "gear", ProjectName: "Landing gear", Ref: "REQ-4", Type: "requirement"},
		},
	}
}

func TestChildProjectIDs(t *testing.T) {
	if got := ChildProjectIDs(flowDownExport()); len(got) != 1 || got[0] != "gear" {
		t.Fatalf("children = %v", got)
	}
	if got := ChildProjectIDs(&exports.ProjectExport{}); len(got) != 0 {
		t.Fatalf("no links: %v", got)
	}
}

// TestApplyFlowDown: a requirement with its own passing test takes the worse
// of its own and its refinements'; one with nothing of its own takes the
// flow-down and says so; one with no method keeps method-missing; an
// unknown refinement counts as uncovered; the summary is recomputed.
func TestApplyFlowDown(t *testing.T) {
	export := flowDownExport()
	latest := map[string]*TestResult{"tc": {Status: "pass"}}
	report := ComputeCoverage(export, latest)
	ApplyFlowDown(report, export, map[string]string{"g1": RollupFail, "g2": RollupPass, "g4": RollupPass})

	byID := map[string]CoverageEntry{}
	for _, e := range report.Entries {
		byID[e.RequirementID] = e
	}
	own := byID["own"]
	if own.Rollup != RollupFail || own.FlowDown != RollupFail || own.ViaRefinements || len(own.Refinements) != 1 || own.Refinements[0].Ref != "REQ-1" {
		t.Fatalf("own = %+v", own)
	}
	bare := byID["bare"]
	if bare.Rollup != RollupUncovered || !bare.ViaRefinements || len(bare.Refinements) != 2 || bare.Refinements[1].Rollup != RollupUncovered {
		t.Fatalf("bare = %+v", bare)
	}
	nomethod := byID["nomethod"]
	if nomethod.Rollup != RollupMethodMissing || nomethod.FlowDown != RollupPass {
		t.Fatalf("nomethod = %+v", nomethod)
	}
	if report.Summary[RollupFail] != 1 || report.Summary[RollupUncovered] != 1 || report.Summary[RollupMethodMissing] != 1 {
		t.Fatalf("summary = %v", report.Summary)
	}

	// With every refinement passing, the bare requirement is verified
	// through its children and the gap report has nothing to say about it.
	report = ComputeCoverage(export, latest)
	ApplyFlowDown(report, export, map[string]string{"g1": RollupPass, "g2": RollupPass, "g3": RollupPass, "g4": RollupPass})
	for _, e := range report.Entries {
		if e.RequirementID == "bare" && (e.Rollup != RollupPass || !e.ViaRefinements) {
			t.Fatalf("bare with passing children = %+v", e)
		}
	}
	gaps := GapAnalysis(export, report)
	for _, id := range gaps.RequirementsWithoutTestCase {
		if id == "bare" {
			t.Fatalf("a requirement verified through its refinements was reported as a gap")
		}
	}
}
