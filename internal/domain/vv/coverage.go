package vv

import (
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

// Rollup values computed for requirement verification coverage.
const (
	RollupMethodMissing    = "method-missing"
	RollupVerifiedManually = "verified-manually"
	RollupUncovered        = "uncovered"
	RollupPass             = "pass"
	RollupFail             = "fail"
	RollupBlocked          = "blocked"
	RollupUnrun            = "unrun"
)

// CoverageEntry is the verification coverage state of one requirement.
type CoverageEntry struct {
	RequirementID      string            `json:"requirement_id"`
	Title              string            `json:"title"`
	VerificationMethod string            `json:"verification_method"`
	VerificationStatus string            `json:"verification_status"`
	TestCaseIDs        []string          `json:"test_case_ids"`
	LatestResults      map[string]string `json:"latest_results"` // test_case_id -> status
	Rollup             string            `json:"rollup"`
}

// CoverageReport aggregates coverage entries for a project.
type CoverageReport struct {
	ProjectID string          `json:"project_id"`
	Entries   []CoverageEntry `json:"entries"`
	Summary   map[string]int  `json:"summary"` // rollup -> count
}

// MatrixRow is one requirement's full traceability row.
type MatrixRow struct {
	RequirementID string            `json:"requirement_id"`
	Title         string            `json:"title"`
	UserNeedIDs   []string          `json:"user_need_ids"`
	DesignIDs     []string          `json:"design_ids"`
	TestCaseIDs   []string          `json:"test_case_ids"`
	LatestResults map[string]string `json:"latest_results"`
	HazardIDs     []string          `json:"hazard_ids"`
}

// Matrix is the project traceability matrix.
type Matrix struct {
	Rows []MatrixRow `json:"rows"`
}

// GapReport lists artifact IDs with traceability or verification gaps.
type GapReport struct {
	RequirementsWithoutMethod   []string `json:"requirements_without_method"`
	RequirementsWithoutTestCase []string `json:"requirements_without_test_case"`
	RequirementsFailing         []string `json:"requirements_failing"`
	OrphanTestCases             []string `json:"orphan_test_cases"`
	NeedsWithoutRequirement     []string `json:"needs_without_requirement"`
	HazardsUnmitigated          []string `json:"hazards_unmitigated"`
}

// attrString reads a string attribute from an artifact, "" if absent.
func attrString(a *artifacts.Artifact, key string) string {
	if a.Attributes == nil {
		return ""
	}
	if v, ok := a.Attributes[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// resultSeverity ranks result statuses for worst-of rollups.
// fail > blocked > unrun (not-run or missing) > pass.
func resultSeverity(status string) int {
	switch status {
	case ResultFail:
		return 3
	case ResultBlocked:
		return 2
	case ResultPass:
		return 0
	default: // not-run, missing, unknown
		return 1
	}
}

func rollupForSeverity(severity int) string {
	switch severity {
	case 3:
		return RollupFail
	case 2:
		return RollupBlocked
	case 1:
		return RollupUnrun
	default:
		return RollupPass
	}
}

// verifiersByRequirement maps requirement ID -> test-case IDs linked to it via
// "verifies" links (test case is FromID, requirement is ToID).
func verifiersByRequirement(export *exports.ProjectExport) map[string][]string {
	testCases := map[string]bool{}
	for _, a := range export.Artifacts {
		if a.Type == "test-case" {
			testCases[a.ID] = true
		}
	}

	verifiers := map[string][]string{}
	for _, l := range export.Links {
		if l.Type == "verifies" && testCases[l.FromID] {
			verifiers[l.ToID] = append(verifiers[l.ToID], l.FromID)
		}
	}
	return verifiers
}

// latestStatuses maps the latest results for a set of test cases to plain
// status strings; test cases with no result are reported as "not-run".
func latestStatuses(testCaseIDs []string, latest map[string]*TestResult) map[string]string {
	statuses := map[string]string{}
	for _, tc := range testCaseIDs {
		if r, ok := latest[tc]; ok && r != nil {
			statuses[tc] = r.Status
		} else {
			statuses[tc] = ResultNotRun
		}
	}
	return statuses
}

// ComputeCoverage computes verification coverage per requirement from a
// project export and the latest test result per test case.
func ComputeCoverage(export *exports.ProjectExport, latest map[string]*TestResult) *CoverageReport {
	report := &CoverageReport{
		ProjectID: export.ProjectID,
		Entries:   []CoverageEntry{},
		Summary:   map[string]int{},
	}

	verifiers := verifiersByRequirement(export)

	for _, a := range export.Artifacts {
		if a.Type != "requirement" {
			continue
		}

		method := attrString(a, "verification_method")
		status := attrString(a, "verification_status")

		testCaseIDs := verifiers[a.ID]
		if testCaseIDs == nil {
			testCaseIDs = []string{}
		}

		entry := CoverageEntry{
			RequirementID:      a.ID,
			Title:              a.Title,
			VerificationMethod: method,
			VerificationStatus: status,
			TestCaseIDs:        testCaseIDs,
			LatestResults:      latestStatuses(testCaseIDs, latest),
		}

		switch {
		case method == "":
			entry.Rollup = RollupMethodMissing
		case method != MethodTest:
			if status == "verified" {
				entry.Rollup = RollupVerifiedManually
			} else {
				entry.Rollup = RollupUncovered
			}
		case len(testCaseIDs) == 0:
			entry.Rollup = RollupUncovered
		default:
			worst := 0
			for _, tc := range testCaseIDs {
				severity := resultSeverity(entry.LatestResults[tc])
				if severity > worst {
					worst = severity
				}
			}
			entry.Rollup = rollupForSeverity(worst)
		}

		report.Entries = append(report.Entries, entry)
		report.Summary[entry.Rollup]++
	}

	return report
}

// BuildMatrix builds the full traceability matrix: user needs, design items,
// test cases, latest results, and hazards per requirement.
func BuildMatrix(export *exports.ProjectExport, latest map[string]*TestResult) *Matrix {
	matrix := &Matrix{Rows: []MatrixRow{}}

	verifiers := verifiersByRequirement(export)

	// requirement ID -> user-need IDs ("derives-from": requirement is FromID)
	needsByReq := map[string][]string{}
	// requirement ID -> design IDs ("satisfies": design is FromID, requirement is ToID)
	designsByReq := map[string][]string{}
	// design ID -> hazard IDs ("mitigates": design is FromID, hazard is ToID)
	hazardsByDesign := map[string][]string{}

	for _, l := range export.Links {
		switch l.Type {
		case "derives-from":
			needsByReq[l.FromID] = append(needsByReq[l.FromID], l.ToID)
		case "satisfies":
			designsByReq[l.ToID] = append(designsByReq[l.ToID], l.FromID)
		case "mitigates":
			hazardsByDesign[l.FromID] = append(hazardsByDesign[l.FromID], l.ToID)
		}
	}

	for _, a := range export.Artifacts {
		if a.Type != "requirement" {
			continue
		}

		userNeedIDs := needsByReq[a.ID]
		if userNeedIDs == nil {
			userNeedIDs = []string{}
		}
		designIDs := designsByReq[a.ID]
		if designIDs == nil {
			designIDs = []string{}
		}
		testCaseIDs := verifiers[a.ID]
		if testCaseIDs == nil {
			testCaseIDs = []string{}
		}

		hazardIDs := []string{}
		seenHazards := map[string]bool{}
		for _, designID := range designIDs {
			for _, hazardID := range hazardsByDesign[designID] {
				if !seenHazards[hazardID] {
					seenHazards[hazardID] = true
					hazardIDs = append(hazardIDs, hazardID)
				}
			}
		}

		matrix.Rows = append(matrix.Rows, MatrixRow{
			RequirementID: a.ID,
			Title:         a.Title,
			UserNeedIDs:   userNeedIDs,
			DesignIDs:     designIDs,
			TestCaseIDs:   testCaseIDs,
			LatestResults: latestStatuses(testCaseIDs, latest),
			HazardIDs:     hazardIDs,
		})
	}

	return matrix
}

// GapAnalysis derives traceability and verification gaps from a coverage
// report and the project export.
func GapAnalysis(export *exports.ProjectExport, coverage *CoverageReport) *GapReport {
	report := &GapReport{
		RequirementsWithoutMethod:   []string{},
		RequirementsWithoutTestCase: []string{},
		RequirementsFailing:         []string{},
		OrphanTestCases:             []string{},
		NeedsWithoutRequirement:     []string{},
		HazardsUnmitigated:          []string{},
	}

	for _, entry := range coverage.Entries {
		if entry.Rollup == RollupMethodMissing {
			report.RequirementsWithoutMethod = append(report.RequirementsWithoutMethod, entry.RequirementID)
		}
		if entry.VerificationMethod == MethodTest && len(entry.TestCaseIDs) == 0 {
			report.RequirementsWithoutTestCase = append(report.RequirementsWithoutTestCase, entry.RequirementID)
		}
		if entry.Rollup == RollupFail {
			report.RequirementsFailing = append(report.RequirementsFailing, entry.RequirementID)
		}
	}

	hasVerifies := map[string]bool{}   // FromID of "verifies" links
	isDerivedFrom := map[string]bool{} // ToID of "derives-from" links
	isMitigated := map[string]bool{}   // ToID of "mitigates" links
	for _, l := range export.Links {
		switch l.Type {
		case "verifies":
			hasVerifies[l.FromID] = true
		case "derives-from":
			isDerivedFrom[l.ToID] = true
		case "mitigates":
			isMitigated[l.ToID] = true
		}
	}

	for _, a := range export.Artifacts {
		switch a.Type {
		case "test-case":
			if !hasVerifies[a.ID] {
				report.OrphanTestCases = append(report.OrphanTestCases, a.ID)
			}
		case "user-need":
			if !isDerivedFrom[a.ID] {
				report.NeedsWithoutRequirement = append(report.NeedsWithoutRequirement, a.ID)
			}
		case "hazard":
			if !isMitigated[a.ID] {
				report.HazardsUnmitigated = append(report.HazardsUnmitigated, a.ID)
			}
		}
	}

	return report
}
