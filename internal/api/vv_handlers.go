package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// registerVVRoutes wires V&V: a project's test runs, their results and the
// agent run that executes one, then the coverage, matrix, gaps and report,
// and the change impact.
func (h *Handler) registerVVRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/test-runs", h.CreateTestRun).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/test-runs", h.ListTestRuns).Methods("GET")
	router.HandleFunc("/api/v1/test-runs/{id}", h.GetTestRun).Methods("GET")
	router.HandleFunc("/api/v1/test-runs/{id}", h.UpdateTestRun).Methods("PUT")
	router.HandleFunc("/api/v1/test-runs/{id}", h.DeleteTestRun).Methods("DELETE")
	router.HandleFunc("/api/v1/test-runs/{id}/results", h.UpsertTestResult).Methods("POST")
	router.HandleFunc("/api/v1/test-runs/{id}/results", h.ListTestResults).Methods("GET")
	router.HandleFunc("/api/v1/test-runs/{id}/agent-run", h.LaunchTestRunAgent).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/vv/coverage", h.GetCoverage).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/vv/matrix", h.GetMatrix).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/vv/gaps", h.GetGaps).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/vv/report", h.GetVVReport).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/impact", h.GetImpact).Methods("GET")
}

func (h *Handler) CreateTestRun(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req vv.CreateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A run's baseline is one of its project's, answered as every route
	// answers a baseline that does not exist (REQ-6): an id no baseline has,
	// a malformed one and another project's are 404, and nothing is stored.
	if req.BaselineID != nil {
		if _, err := h.baselineService.GetProjectBaseline(projectID, *req.BaselineID); err != nil {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
	}
	req.ProjectID = projectID
	run, err := h.vvService.CreateRun(req, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

func (h *Handler) ListTestRuns(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	runs, err := h.vvService.ListRuns(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list test runs", err)
		return
	}
	json.NewEncoder(w).Encode(runs)
}

func (h *Handler) GetTestRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.vvService.GetRun(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, run.ProjectID, members.RoleViewer, missing("test run not found")) {
		return
	}
	json.NewEncoder(w).Encode(run)
}

func (h *Handler) UpdateTestRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, run.ProjectID, members.RoleEditor, missing("test run not found")) {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.vvService.UpdateRunStatus(id, req.Status)
	if err != nil {
		switch {
		case errors.Is(err, vv.ErrInvalidStatus):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, vv.ErrInvalidTransition):
			writeJSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, vv.ErrRunNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			respondInternal(w, r, "failed to update test run status", err)
		}
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteTestRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, run.ProjectID, members.RoleEditor, missing("test run not found")) {
		return
	}
	if err := h.vvService.DeleteRun(id); err != nil {
		switch {
		case errors.Is(err, vv.ErrRunHasResults):
			// Its results are the record REQ-13 keeps: the run is closed,
			// not deleted.
			writeJSONError(w, http.StatusConflict, err.Error())
		case errors.Is(err, vv.ErrRunNotFound):
			writeJSONError(w, http.StatusNotFound, err.Error())
		default:
			respondInternal(w, r, "failed to delete test run", err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpsertTestResult(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(runID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, run.ProjectID, members.RoleEditor, missing("test run not found")) {
		return
	}
	var req vv.UpsertResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// An agent run recording a result is stamped with its run id, which also
	// gates test cases flagged as human- or physically-verified.
	agentRunID := ""
	if run := CurrentRun(r); run != nil {
		agentRunID = run.ID
	}
	result, err := h.vvService.UpsertResult(runID, req, CurrentUserID(r), Actor(r), agentRunID)
	if err != nil {
		respondResultError(w, r, err)
		return
	}
	json.NewEncoder(w).Encode(result)
}

// respondResultError answers a refused result, and an agent launched to record
// results in a closed run, which a result there would meet. Domain sentinels
// are the caller's problem and keep their text; any other failure is ours and
// must answer 5xx — a test-executing agent retries on 5xx, and its recorded
// outcome must not be lost to a DB blip mislabeled as a 4xx.
func respondResultError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vv.ErrNotAgentExecutable):
		writeJSONError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, vv.ErrInvalidStatus), errors.Is(err, vv.ErrNotTestCase):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, vv.ErrRunClosed):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, vv.ErrRunNotFound), errors.Is(err, artifacts.ErrNotFound):
		// The run exists (checked by the caller); ErrNotFound here means the
		// referenced test case id does not resolve to an artifact of the
		// run's project (another project's is answered as none).
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		respondInternal(w, r, "failed to record test result", err)
	}
}

func (h *Handler) ListTestResults(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(runID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, run.ProjectID, members.RoleViewer, missing("test run not found")) {
		return
	}
	// The current result per test case; ?history=true lists every result
	// recorded, the superseded ones included (REQ-13).
	list := h.vvService.ListResults
	if r.URL.Query().Get("history") == "true" {
		list = h.vvService.ListResultHistory
	}
	results, err := list(runID)
	if err != nil {
		respondInternal(w, r, "failed to list test results", err)
		return
	}
	json.NewEncoder(w).Encode(results)
}

// LaunchTestRunAgent starts an agent run that executes a test run's
// agent-executable test cases and records their results. Test cases flagged
// manual or physical are never handed to the agent — they stay with people,
// and are reported back as skipped so the UI can show what still needs doing.
func (h *Handler) LaunchTestRunAgent(w http.ResponseWriter, r *http.Request) {
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
	runID := mux.Vars(r)["id"]
	testRun, err := h.vvService.GetRun(runID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, testRun.ProjectID, members.RoleEditor, missing("test run not found")) {
		return
	}
	// A closed run takes no result, so no agent is launched to record one:
	// the 409 a result there gets (REQ-13, REQ-74).
	if err := vv.CheckAcceptsResults(testRun.Status); err != nil {
		respondResultError(w, r, err)
		return
	}

	var req struct {
		AgentSlug   string   `json:"agent_slug"`
		TestCaseIDs []string `json:"test_case_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.AgentSlug) == "" {
		writeJSONError(w, http.StatusBadRequest, "agent_slug is required")
		return
	}

	orgID := ActiveOrg(r)
	if project, err := h.projectService.GetProject(testRun.ProjectID); err == nil && project != nil && project.OrgID != "" {
		orgID = project.OrgID
	}
	agent, err := h.agentService.GetBySlug(orgID, req.AgentSlug)
	if err != nil || agent == nil {
		writeJSONError(w, http.StatusNotFound, "agent not found")
		return
	}

	runnable, skipped, err := h.vvService.AgentExecutableCases(testRun.ProjectID, req.TestCaseIDs)
	if err != nil {
		respondInternal(w, r, "failed to select agent-executable test cases", err)
		return
	}
	if len(runnable) == 0 {
		msg := "no agent-executable test cases in this run"
		if len(skipped) > 0 {
			msg += fmt.Sprintf(" — all %d selected case(s) are flagged as human- or physically-verified", len(skipped))
		}
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}

	agentRun, err := h.launchRun(r, agentruns.LaunchRequest{
		OrgID:      orgID,
		AgentID:    agent.ID,
		ProjectID:  &testRun.ProjectID,
		Prompt:     testRunAgentPrompt(testRun, runnable, skipped),
		LaunchedBy: CurrentUserID(r),
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	skippedOut := make([]map[string]string, 0, len(skipped))
	for _, tc := range skipped {
		skippedOut = append(skippedOut, map[string]string{
			"id":               tc.ID,
			"title":            tc.Title,
			"execution_method": vv.ExecutionMethod(tc.Attributes),
		})
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"run":       agentRun,
		"executing": len(runnable),
		"skipped":   skippedOut,
	})
}

// testRunAgentPrompt builds the instruction an agent receives to execute a
// test run. It names the exact cases to execute so the agent neither invents
// work nor wanders into cases reserved for a human.
func testRunAgentPrompt(testRun *vv.TestRun, runnable, skipped []*artifacts.Artifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Execute the test cases below for the OpenV test run %q and record a result for each one.\n\n", testRun.Name)
	fmt.Fprintf(&b, "project_id: %s\ntest run id: %s\n\n", testRun.ProjectID, testRun.ID)

	b.WriteString("Test cases to execute:\n")
	for _, tc := range runnable {
		fmt.Fprintf(&b, "- %s (test case id: %s)\n", tc.Title, tc.ID)
	}

	b.WriteString("\nHow to proceed:\n")
	b.WriteString("1. Read each test case with get_artifact to get its steps and expected results.\n")
	b.WriteString("2. Carry out the test as written, using the project's repository and tooling where relevant.\n")
	b.WriteString("3. Record the outcome with record_test_result, passing the run id above, the test case id, ")
	b.WriteString("and a status of \"pass\", \"fail\", or \"blocked\".\n")
	b.WriteString("4. In the notes, state exactly what you did and what you observed — the command you ran, ")
	b.WriteString("the output, or the reason it could not be run. This is verification evidence: a reviewer must be ")
	b.WriteString("able to judge your result without rerunning it.\n\n")

	b.WriteString("Rules:\n")
	b.WriteString("- Only report \"pass\" for behaviour you actually observed. If you could not execute a case ")
	b.WriteString("(missing environment, unclear steps, needs hardware), record it as \"blocked\" and explain why.\n")
	b.WriteString("- Never guess an outcome, and never edit a test case to make it pass.\n")
	b.WriteString("- Record a result for every test case listed above, and for no others.\n")

	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\n%d further test case(s) in this run are flagged as human- or physically-verified and are ", len(skipped))
		b.WriteString("deliberately excluded — do not attempt them or record results for them:\n")
		for _, tc := range skipped {
			fmt.Fprintf(&b, "- %s (%s)\n", tc.Title, vv.ExecutionMethod(tc.Attributes))
		}
	}
	return b.String()
}

func (h *Handler) vvReportData(w http.ResponseWriter, r *http.Request) (*exports.ProjectExport, map[string]*vv.TestResult, bool) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return nil, nil, false
	}
	export, err := h.projectExport(projectID, r.URL.Query().Get("baseline_id"))
	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return nil, nil, false
		}
		respondInternal(w, r, "failed to export project", err)
		return nil, nil, false
	}
	latest, err := h.vvService.LatestResults(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load latest test results", err)
		return nil, nil, false
	}
	return export, latest, true
}

func (h *Handler) GetCoverage(w http.ResponseWriter, r *http.Request) {
	export, latest, ok := h.vvReportData(w, r)
	if !ok {
		return
	}
	json.NewEncoder(w).Encode(h.coverageWithFlowDown(export, latest, nil))
}

// coverageWithFlowDown computes a project's coverage and rolls in the
// verification of the child-project requirements that refine its own
// (REQ-146). Each child project's live coverage is computed the same way,
// so a refinement that is itself refined further down rolls all the way
// up. done holds each project's coverage once the request has computed it,
// so a child project whose requirements refine those of two projects of the
// flow-down is read once and counts for both; a project still being computed holds nil, which
// guards against a loop in stored parents. No rights on the child project
// are needed: what comes back is a rollup per requirement the parent
// already links to, never the child's content.
func (h *Handler) coverageWithFlowDown(export *exports.ProjectExport, latest map[string]*vv.TestResult, done map[string]*vv.CoverageReport) *vv.CoverageReport {
	report := vv.ComputeCoverage(export, latest)
	children := vv.ChildProjectIDs(export)
	if len(children) == 0 {
		return report
	}
	if done == nil {
		done = map[string]*vv.CoverageReport{}
	}
	done[export.ProjectID] = nil
	rollups := map[string]string{}
	for _, childID := range children {
		child, known := done[childID]
		if !known {
			child = h.childCoverage(childID, done)
			done[childID] = child
		}
		if child == nil {
			continue
		}
		for _, e := range child.Entries {
			rollups[e.RequirementID] = e.Rollup
		}
	}
	vv.ApplyFlowDown(report, export, rollups)
	return report
}

// childCoverage is a child project's live coverage with its own flow-down,
// or nil when its export cannot be read.
func (h *Handler) childCoverage(childID string, done map[string]*vv.CoverageReport) *vv.CoverageReport {
	childExport, err := h.projectExport(childID, "")
	if err != nil {
		slog.Warn("vv: could not read a child project for the flow-up", "project_id", childID, "error", err)
		return nil
	}
	childLatest, err := h.vvService.LatestResults(childID)
	if err != nil {
		slog.Warn("vv: could not read a child project's results", "project_id", childID, "error", err)
		childLatest = map[string]*vv.TestResult{}
	}
	return h.coverageWithFlowDown(childExport, childLatest, done)
}

// flowDownCoverage is the coverage GetCoverage answers, as the
// reports.CoverageFunc of a document: the V&V report and a downloaded
// document's V&V status roll up the child projects' verification as the JSON
// does, so neither reports a requirement verified through its refinements as
// a gap (REQ-146).
func (h *Handler) flowDownCoverage(export *exports.ProjectExport, latest map[string]*vv.TestResult) *vv.CoverageReport {
	return h.coverageWithFlowDown(export, latest, nil)
}

func (h *Handler) GetMatrix(w http.ResponseWriter, r *http.Request) {
	export, latest, ok := h.vvReportData(w, r)
	if !ok {
		return
	}
	json.NewEncoder(w).Encode(vv.BuildMatrix(export, latest))
}

func (h *Handler) GetGaps(w http.ResponseWriter, r *http.Request) {
	export, latest, ok := h.vvReportData(w, r)
	if !ok {
		return
	}
	coverage := h.coverageWithFlowDown(export, latest, nil)
	json.NewEncoder(w).Encode(vv.GapAnalysis(export, coverage))
}

// GetImpact returns the change-impact set for one artifact: the artifacts
// reachable through the traceability link graph, grouped by type, in the
// requested direction. downstream = artifacts that depend on the seed (what a
// change to it could break); upstream = the artifacts the seed itself depends
// on. It reuses the same project export (live or baseline) the matrix loads.
func (h *Handler) GetImpact(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	artifactID := strings.TrimSpace(r.URL.Query().Get("artifact"))
	if artifactID == "" {
		writeJSONError(w, http.StatusBadRequest, "artifact query parameter is required")
		return
	}
	export, err := h.projectExport(projectID, r.URL.Query().Get("baseline_id"))
	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
		respondInternal(w, r, "failed to export project", err)
		return
	}
	// The seed must belong to the authorized project (or baseline snapshot);
	// this also stops IDs from other projects being probed.
	found := false
	for _, a := range export.Artifacts {
		if a != nil && a.ID == artifactID {
			found = true
			break
		}
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "artifact not found in project")
		return
	}
	json.NewEncoder(w).Encode(vv.ComputeImpact(export, artifactID, r.URL.Query().Get("direction")))
}

// GetVVReport generates the V&V status PDF for a project or baseline.
func (h *Handler) GetVVReport(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	latest, err := h.vvService.LatestResults(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load latest test results", err)
		return
	}
	runs, err := h.vvService.ListRuns(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list test runs", err)
		return
	}

	data, filename, err := h.reportService.GenerateVVReport(projectID, r.URL.Query().Get("baseline_id"), latest, runs,
		h.flowDownCoverage)
	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
		respondInternal(w, r, "failed to generate V&V report", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
