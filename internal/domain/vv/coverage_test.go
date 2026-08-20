package vv

import (
	"reflect"
	"sort"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
)

func mkArtifact(id, artifactType, title string, attrs map[string]interface{}) *artifacts.Artifact {
	if attrs == nil {
		attrs = map[string]interface{}{}
	}
	return &artifacts.Artifact{
		ID:         id,
		ProjectID:  "proj-1",
		Type:       artifactType,
		Title:      title,
		Attributes: attrs,
		Version:    1,
	}
}

func mkLink(fromID, toID, linkType string) *links.Link {
	return &links.Link{
		ID:     fromID + "->" + toID,
		FromID: fromID,
		ToID:   toID,
		Type:   linkType,
	}
}

func mkResult(testCaseID, status string) *TestResult {
	return &TestResult{
		ID:         "result-" + testCaseID,
		RunID:      "run-1",
		TestCaseID: testCaseID,
		Status:     status,
	}
}

func mkExport(artifactList []*artifacts.Artifact, linkList []*links.Link) *exports.ProjectExport {
	return &exports.ProjectExport{
		ProjectID: "proj-1",
		Artifacts: artifactList,
		Links:     linkList,
	}
}

func entryFor(t *testing.T, report *CoverageReport, requirementID string) CoverageEntry {
	t.Helper()
	for _, e := range report.Entries {
		if e.RequirementID == requirementID {
			return e
		}
	}
	t.Fatalf("no coverage entry for requirement %q", requirementID)
	return CoverageEntry{}
}

func TestComputeCoverage(t *testing.T) {
	tests := []struct {
		name          string
		artifacts     []*artifacts.Artifact
		links         []*links.Link
		latest        map[string]*TestResult
		wantRollup    string
		wantResultFor map[string]string // expected LatestResults entries
	}{
		{
			name: "method missing",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", nil),
			},
			wantRollup: RollupMethodMissing,
		},
		{
			name: "test method with no test case is uncovered",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodTest,
				}),
			},
			wantRollup: RollupUncovered,
		},
		{
			name: "all passing test cases roll up to pass",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodTest,
				}),
				mkArtifact("tc-1", "test-case", "TC 1", nil),
				mkArtifact("tc-2", "test-case", "TC 2", nil),
			},
			links: []*links.Link{
				mkLink("tc-1", "req-1", "verifies"),
				mkLink("tc-2", "req-1", "verifies"),
			},
			latest: map[string]*TestResult{
				"tc-1": mkResult("tc-1", ResultPass),
				"tc-2": mkResult("tc-2", ResultPass),
			},
			wantRollup:    RollupPass,
			wantResultFor: map[string]string{"tc-1": ResultPass, "tc-2": ResultPass},
		},
		{
			name: "fail dominates pass and blocked",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodTest,
				}),
				mkArtifact("tc-1", "test-case", "TC 1", nil),
				mkArtifact("tc-2", "test-case", "TC 2", nil),
				mkArtifact("tc-3", "test-case", "TC 3", nil),
			},
			links: []*links.Link{
				mkLink("tc-1", "req-1", "verifies"),
				mkLink("tc-2", "req-1", "verifies"),
				mkLink("tc-3", "req-1", "verifies"),
			},
			latest: map[string]*TestResult{
				"tc-1": mkResult("tc-1", ResultPass),
				"tc-2": mkResult("tc-2", ResultFail),
				"tc-3": mkResult("tc-3", ResultBlocked),
			},
			wantRollup: RollupFail,
		},
		{
			name: "blocked dominates pass and unrun",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodTest,
				}),
				mkArtifact("tc-1", "test-case", "TC 1", nil),
				mkArtifact("tc-2", "test-case", "TC 2", nil),
			},
			links: []*links.Link{
				mkLink("tc-1", "req-1", "verifies"),
				mkLink("tc-2", "req-1", "verifies"),
			},
			latest: map[string]*TestResult{
				"tc-1": mkResult("tc-1", ResultBlocked),
				"tc-2": mkResult("tc-2", ResultPass),
			},
			wantRollup: RollupBlocked,
		},
		{
			name: "missing result rolls up to unrun",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodTest,
				}),
				mkArtifact("tc-1", "test-case", "TC 1", nil),
				mkArtifact("tc-2", "test-case", "TC 2", nil),
			},
			links: []*links.Link{
				mkLink("tc-1", "req-1", "verifies"),
				mkLink("tc-2", "req-1", "verifies"),
			},
			latest: map[string]*TestResult{
				"tc-1": mkResult("tc-1", ResultPass),
			},
			wantRollup:    RollupUnrun,
			wantResultFor: map[string]string{"tc-1": ResultPass, "tc-2": ResultNotRun},
		},
		{
			name: "inspection verified is verified-manually",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodInspection,
					"verification_status": "verified",
				}),
			},
			wantRollup: RollupVerifiedManually,
		},
		{
			name: "analysis not verified is uncovered",
			artifacts: []*artifacts.Artifact{
				mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
					"verification_method": MethodAnalysis,
				}),
			},
			wantRollup: RollupUncovered,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := ComputeCoverage(mkExport(tt.artifacts, tt.links), tt.latest)

			entry := entryFor(t, report, "req-1")
			if entry.Rollup != tt.wantRollup {
				t.Errorf("rollup = %q, want %q", entry.Rollup, tt.wantRollup)
			}
			if report.Summary[tt.wantRollup] != 1 {
				t.Errorf("summary[%q] = %d, want 1", tt.wantRollup, report.Summary[tt.wantRollup])
			}
			for tc, want := range tt.wantResultFor {
				if got := entry.LatestResults[tc]; got != want {
					t.Errorf("latest result for %q = %q, want %q", tc, got, want)
				}
			}
		})
	}
}

func TestComputeCoverageIgnoresNonRequirements(t *testing.T) {
	export := mkExport([]*artifacts.Artifact{
		mkArtifact("tc-1", "test-case", "TC 1", nil),
		mkArtifact("h-1", "hazard", "Hazard 1", nil),
	}, nil)

	report := ComputeCoverage(export, nil)
	if len(report.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(report.Entries))
	}
}

func TestGapAnalysis(t *testing.T) {
	export := mkExport(
		[]*artifacts.Artifact{
			// no verification method
			mkArtifact("req-nomethod", "requirement", "No method", nil),
			// test method, no verifying test case
			mkArtifact("req-notc", "requirement", "No test case", map[string]interface{}{
				"verification_method": MethodTest,
			}),
			// test method, failing test case
			mkArtifact("req-fail", "requirement", "Failing", map[string]interface{}{
				"verification_method": MethodTest,
			}),
			// healthy requirement: test method, passing test case
			mkArtifact("req-pass", "requirement", "Passing", map[string]interface{}{
				"verification_method": MethodTest,
			}),
			// test cases
			mkArtifact("tc-fail", "test-case", "TC fail", nil),
			mkArtifact("tc-pass", "test-case", "TC pass", nil),
			mkArtifact("tc-orphan", "test-case", "TC orphan", nil),
			// user needs
			mkArtifact("need-linked", "user-need", "Linked need", nil),
			mkArtifact("need-orphan", "user-need", "Orphan need", nil),
			// hazards and a design item
			mkArtifact("design-1", "design-item", "Design 1", nil),
			mkArtifact("hazard-mitigated", "hazard", "Mitigated", nil),
			mkArtifact("hazard-open", "hazard", "Open", nil),
		},
		[]*links.Link{
			mkLink("tc-fail", "req-fail", "verifies"),
			mkLink("tc-pass", "req-pass", "verifies"),
			mkLink("req-pass", "need-linked", "derives-from"),
			mkLink("design-1", "req-pass", "satisfies"),
			mkLink("design-1", "hazard-mitigated", "mitigates"),
		},
	)

	latest := map[string]*TestResult{
		"tc-fail": mkResult("tc-fail", ResultFail),
		"tc-pass": mkResult("tc-pass", ResultPass),
	}

	coverage := ComputeCoverage(export, latest)
	gaps := GapAnalysis(export, coverage)

	assertIDs := func(name string, got, want []string) {
		t.Helper()
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	assertIDs("RequirementsWithoutMethod", gaps.RequirementsWithoutMethod, []string{"req-nomethod"})
	assertIDs("RequirementsWithoutTestCase", gaps.RequirementsWithoutTestCase, []string{"req-notc"})
	assertIDs("RequirementsFailing", gaps.RequirementsFailing, []string{"req-fail"})
	assertIDs("OrphanTestCases", gaps.OrphanTestCases, []string{"tc-orphan"})
	assertIDs("NeedsWithoutRequirement", gaps.NeedsWithoutRequirement, []string{"need-orphan"})
	assertIDs("HazardsUnmitigated", gaps.HazardsUnmitigated, []string{"hazard-open"})
}

func TestGapAnalysisEmptyProject(t *testing.T) {
	export := mkExport(nil, nil)
	coverage := ComputeCoverage(export, nil)
	gaps := GapAnalysis(export, coverage)

	if len(gaps.RequirementsWithoutMethod) != 0 ||
		len(gaps.RequirementsWithoutTestCase) != 0 ||
		len(gaps.RequirementsFailing) != 0 ||
		len(gaps.OrphanTestCases) != 0 ||
		len(gaps.NeedsWithoutRequirement) != 0 ||
		len(gaps.HazardsUnmitigated) != 0 {
		t.Errorf("expected empty gap report, got %+v", gaps)
	}
}

func TestBuildMatrix(t *testing.T) {
	export := mkExport(
		[]*artifacts.Artifact{
			mkArtifact("req-1", "requirement", "Req 1", map[string]interface{}{
				"verification_method": MethodTest,
			}),
			mkArtifact("need-1", "user-need", "Need 1", nil),
			mkArtifact("design-1", "design-item", "Design 1", nil),
			mkArtifact("tc-1", "test-case", "TC 1", nil),
			mkArtifact("hazard-1", "hazard", "Hazard 1", nil),
		},
		[]*links.Link{
			mkLink("req-1", "need-1", "derives-from"),
			mkLink("design-1", "req-1", "satisfies"),
			mkLink("tc-1", "req-1", "verifies"),
			mkLink("design-1", "hazard-1", "mitigates"),
		},
	)

	latest := map[string]*TestResult{
		"tc-1": mkResult("tc-1", ResultPass),
	}

	matrix := BuildMatrix(export, latest)
	if len(matrix.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(matrix.Rows))
	}

	row := matrix.Rows[0]
	if row.RequirementID != "req-1" {
		t.Errorf("requirement id = %q, want req-1", row.RequirementID)
	}
	if !reflect.DeepEqual(row.UserNeedIDs, []string{"need-1"}) {
		t.Errorf("user needs = %v, want [need-1]", row.UserNeedIDs)
	}
	if !reflect.DeepEqual(row.DesignIDs, []string{"design-1"}) {
		t.Errorf("designs = %v, want [design-1]", row.DesignIDs)
	}
	if !reflect.DeepEqual(row.TestCaseIDs, []string{"tc-1"}) {
		t.Errorf("test cases = %v, want [tc-1]", row.TestCaseIDs)
	}
	if !reflect.DeepEqual(row.HazardIDs, []string{"hazard-1"}) {
		t.Errorf("hazards = %v, want [hazard-1]", row.HazardIDs)
	}
	if row.LatestResults["tc-1"] != ResultPass {
		t.Errorf("latest result = %q, want pass", row.LatestResults["tc-1"])
	}
}
