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
	// Refinements are the requirements of child projects that refine this
	// one (REQ-146), each with its own rollup in its own project. FlowDown
	// is the worst of them, and when the requirement has no evidence of its
	// own the flow-down is its rollup (ViaRefinements says so).
	Refinements    []Refinement `json:"refinements,omitempty"`
	FlowDown       string       `json:"flow_down,omitempty"`
	ViaRefinements bool         `json:"via_refinements,omitempty"`
}

// Refinement is one child-project requirement refining a parent one.
type Refinement struct {
	RequirementID string `json:"requirement_id"`
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	Ref           string `json:"ref"`
	Title         string `json:"title"`
	// Rollup is the refinement's own verification rollup in its project;
	// "uncovered" when that project could not be read.
	Rollup string `json:"rollup"`
}

// rollupSeverity orders rollups for a flow-up: a failing refinement
// outweighs a blocked one, which outweighs one not yet run, which outweighs
// one with nothing behind it, and a pass or manual attestation weighs
// nothing.
func rollupSeverity(rollup string) int {
	switch rollup {
	case RollupFail:
		return 4
	case RollupBlocked:
		return 3
	case RollupUnrun:
		return 2
	case RollupUncovered, RollupMethodMissing, "":
		return 1
	default:
		return 0
	}
}

// RefinersByRequirement maps a local requirement to the foreign requirements
// that refine it: "refines" links whose target is local and whose source is
// one of the export's linked (other-project) artifacts.
func RefinersByRequirement(export *exports.ProjectExport) map[string][]*exports.LinkedArtifact {
	linked := export.LinkedByID()
	if len(linked) == 0 {
		return nil
	}
	out := map[string][]*exports.LinkedArtifact{}
	for _, l := range export.Links {
		if l == nil || l.Type != "refines" {
			continue
		}
		if far, ok := linked[l.FromID]; ok && far.Type == "requirement" {
			out[l.ToID] = append(out[l.ToID], far)
		}
	}
	return out
}

// ChildProjectIDs lists the projects whose requirements refine this
// project's, so a caller can compute their coverage for ApplyFlowDown.
func ChildProjectIDs(export *exports.ProjectExport) []string {
	seen := map[string]bool{}
	var out []string
	for _, refiners := range RefinersByRequirement(export) {
		for _, far := range refiners {
			if !seen[far.ProjectID] {
				seen[far.ProjectID] = true
				out = append(out, far.ProjectID)
			}
		}
	}
	return out
}

// ApplyFlowDown rolls child-project verification up into a coverage report
// (REQ-146). childRollups maps a foreign requirement id to its rollup in its
// own project; a refinement absent from it counts as uncovered. A
// requirement with no evidence of its own takes the flow-down as its
// rollup; one with evidence keeps the worse of the two; one with no
// verification method keeps method-missing, since the method is still its
// own to state. The summary is recomputed.
func ApplyFlowDown(report *CoverageReport, export *exports.ProjectExport, childRollups map[string]string) {
	if report == nil || export == nil {
		return
	}
	refiners := RefinersByRequirement(export)
	if len(refiners) == 0 {
		return
	}
	for i := range report.Entries {
		entry := &report.Entries[i]
		fars := refiners[entry.RequirementID]
		if len(fars) == 0 {
			continue
		}
		worst := 0
		flowDown := RollupPass
		for _, far := range fars {
			rollup := childRollups[far.ID]
			if rollup == "" {
				rollup = RollupUncovered
			}
			entry.Refinements = append(entry.Refinements, Refinement{
				RequirementID: far.ID, ProjectID: far.ProjectID, ProjectName: far.ProjectName,
				Ref: far.Ref, Title: far.Title, Rollup: rollup,
			})
			if s := rollupSeverity(rollup); s > worst {
				worst, flowDown = s, rollup
			}
		}
		entry.FlowDown = flowDown
		switch {
		case entry.Rollup == RollupMethodMissing:
			// Still the parent's to fix.
		case entry.Rollup == RollupUncovered:
			entry.Rollup = flowDown
			entry.ViaRefinements = true
		case rollupSeverity(flowDown) > rollupSeverity(entry.Rollup):
			entry.Rollup = flowDown
		}
	}
	report.Summary = map[string]int{}
	for _, e := range report.Entries {
		report.Summary[e.Rollup]++
	}
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
	// RequirementsUnverified holds requirements whose verification method is
	// anything other than test — demonstration, analysis and inspection, and
	// any other value a project uses — that nothing has yet attested to. They
	// can never appear in RequirementsWithoutTestCase — a non-test method
	// needs no test case — so without this bucket the gap report stayed silent
	// about them while the coverage rollup already counted them as
	// "uncovered". It is derived from that same rollup (see GapAnalysis),
	// so the two views cannot disagree.
	RequirementsUnverified  []string `json:"requirements_unverified"`
	RequirementsFailing     []string `json:"requirements_failing"`
	OrphanTestCases         []string `json:"orphan_test_cases"`
	NeedsWithoutRequirement []string `json:"needs_without_requirement"`
	HazardsUnmitigated      []string `json:"hazards_unmitigated"`
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
		RequirementsUnverified:      []string{},
		RequirementsFailing:         []string{},
		OrphanTestCases:             []string{},
		NeedsWithoutRequirement:     []string{},
		HazardsUnmitigated:          []string{},
	}

	for _, entry := range coverage.Entries {
		if entry.Rollup == RollupMethodMissing {
			report.RequirementsWithoutMethod = append(report.RequirementsWithoutMethod, entry.RequirementID)
		}
		// A requirement verified through the child-project requirements
		// that refine it (REQ-146) needs no test case of its own: its
		// evidence is theirs, and the rollup above already carries it.
		if entry.VerificationMethod == MethodTest && len(entry.TestCaseIDs) == 0 && len(entry.Refinements) == 0 {
			report.RequirementsWithoutTestCase = append(report.RequirementsWithoutTestCase, entry.RequirementID)
		}
		// A requirement whose method is anything but test is verified by an
		// attestation on the artifact, not by a test case, so the bucket
		// above can never hold it. ComputeCoverage already decides whether
		// such a requirement counts: it rolls up as verified-manually once
		// attested and as uncovered until then. Mirroring that branch —
		// the same open "not test" predicate, not a closed list of the
		// canonical methods — is what keeps the gap report and the coverage
		// rollup from telling two different stories when a project carries a
		// method value the enumeration never anticipated. An uncovered
		// rollup already implies a non-empty method: a missing one rolls up
		// as method-missing and belongs to RequirementsWithoutMethod alone.
		if entry.Rollup == RollupUncovered && entry.VerificationMethod != MethodTest {
			report.RequirementsUnverified = append(report.RequirementsUnverified, entry.RequirementID)
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
