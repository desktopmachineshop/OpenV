package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

func (h *Handler) registerSuiteRoutes(router *mux.Router) {
	// Product profile.
	router.HandleFunc("/api/v1/projects/{id}/profile", h.GetProductProfile).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/profile", h.UpdateProductProfile).Methods("PUT")

	// V&V.
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

	// Work items (kanban).
	router.HandleFunc("/api/v1/projects/{id}/work-items", h.CreateWorkItem).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/work-items", h.ListWorkItems).Methods("GET")
	router.HandleFunc("/api/v1/work-items/{id}", h.GetWorkItem).Methods("GET")
	router.HandleFunc("/api/v1/work-items/{id}", h.UpdateWorkItem).Methods("PUT")
	router.HandleFunc("/api/v1/work-items/{id}", h.DeleteWorkItem).Methods("DELETE")
	router.HandleFunc("/api/v1/work-items/{id}/move", h.MoveWorkItem).Methods("POST")
	router.HandleFunc("/api/v1/work-items/{id}/comments", h.CommentWorkItem).Methods("POST")

	// Guided sessions.
	router.HandleFunc("/api/v1/guided-sessions", h.StartGuidedSession).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions", h.ListGuidedSessions).Methods("GET")
	router.HandleFunc("/api/v1/guided-sessions/{id}", h.GetGuidedSession).Methods("GET")
	router.HandleFunc("/api/v1/guided-sessions/{id}/step", h.SaveGuidedStep).Methods("PUT")
	router.HandleFunc("/api/v1/guided-sessions/{id}/drafts", h.MaterializeGuidedDrafts).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/commit", h.CommitGuidedSession).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/abandon", h.AbandonGuidedSession).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/messages", h.ListGuidedChatMessages).Methods("GET")
	router.HandleFunc("/api/v1/guided-sessions/{id}/messages", h.PostGuidedChatMessage).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/chat/kickoff", h.KickoffGuidedChat).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/chat/nudge", h.NudgeGuidedChat).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/chat/stream", h.StreamGuidedChat).Methods("GET")

	// Interviews (internal management).
	router.HandleFunc("/api/v1/projects/{id}/interviews", h.CreateInterview).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/interviews", h.ListInterviews).Methods("GET")
	router.HandleFunc("/api/v1/interviews/{id}/close", h.CloseInterview).Methods("POST")
	router.HandleFunc("/api/v1/interviews/{id}/persona", h.SetInterviewPersona).Methods("PUT")
	router.HandleFunc("/api/v1/interviews/{id}/invites", h.CreateInterviewInvite).Methods("POST")
	router.HandleFunc("/api/v1/interviews/{id}/invites", h.ListInterviewInvites).Methods("GET")
	router.HandleFunc("/api/v1/interview-invites/{id}/revoke", h.RevokeInterviewInvite).Methods("POST")
	router.HandleFunc("/api/v1/interviews/{id}/sessions", h.ListInterviewSessions).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/interview-sessions", h.ListProjectInterviewSessions).Methods("GET")
	router.HandleFunc("/api/v1/interview-sessions/{id}/transcript", h.GetInterviewTranscript).Methods("GET")

	// Interviews (public, token-authenticated).
	router.HandleFunc("/api/v1/public/interviews/{token}", h.PublicInterviewIntro).Methods("GET")
	router.HandleFunc("/api/v1/public/interviews/{token}/messages", h.PublicInterviewMessage).Methods("POST")
	router.HandleFunc("/api/v1/public/interviews/{token}/stream", h.PublicInterviewStream).Methods("GET")
	router.HandleFunc("/api/v1/public/interviews/{token}/finish", h.PublicInterviewFinish).Methods("POST")
}

// projectExport loads the live export or a baseline snapshot as a DTO.
func (h *Handler) projectExport(projectID, baselineID string) (*exports.ProjectExport, error) {
	if baselineID != "" && baselineID != "live" {
		baseline, err := h.baselineService.GetBaseline(baselineID)
		if err != nil {
			return nil, err
		}
		var data exports.ProjectExport
		if err := json.Unmarshal(baseline.Snapshot, &data); err != nil {
			return nil, fmt.Errorf("failed to parse baseline snapshot: %w", err)
		}
		return &data, nil
	}
	raw, _, err := h.exportService.ExportProject(projectID, exports.FormatJSON)
	if err != nil {
		return nil, err
	}
	var data exports.ProjectExport
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// --- Product profile ---

func (h *Handler) GetProductProfile(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	profile, err := h.productService.GetProfile(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load product profile", err)
		return
	}
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) UpdateProductProfile(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req products.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	profile, err := h.productService.UpdateProfile(projectID, req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(profile)
}

// --- V&V ---

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
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleViewer) {
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
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleEditor) {
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
		writeJSONError(w, http.StatusBadRequest, err.Error())
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
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleEditor) {
		return
	}
	if err := h.vvService.DeleteRun(id); err != nil {
		respondInternal(w, r, "failed to delete test run", err)
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
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleEditor) {
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
		status := http.StatusBadRequest
		if errors.Is(err, vv.ErrNotAgentExecutable) {
			status = http.StatusForbidden
		}
		writeJSONError(w, status, err.Error())
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) ListTestResults(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(runID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleViewer) {
		return
	}
	results, err := h.vvService.ListResults(runID)
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
	runID := mux.Vars(r)["id"]
	testRun, err := h.vvService.GetRun(runID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "test run not found", err)
		return
	}
	if !h.requireProjectRole(w, r, testRun.ProjectID, members.RoleEditor) {
		return
	}
	if testRun.Status != vv.RunStatusInProgress {
		writeJSONError(w, http.StatusBadRequest, "this test run is "+testRun.Status+"; only in-progress runs accept new results")
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

	agentRun, _, err := h.runService.Launch(agentruns.LaunchRequest{
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
	json.NewEncoder(w).Encode(vv.ComputeCoverage(export, latest))
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
	coverage := vv.ComputeCoverage(export, latest)
	json.NewEncoder(w).Encode(vv.GapAnalysis(export, coverage))
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

	data, filename, err := h.reportService.GenerateVVReport(projectID, r.URL.Query().Get("baseline_id"), latest, runs)
	if err != nil {
		respondInternal(w, r, "failed to generate V&V report", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// --- Work items ---

func (h *Handler) CreateWorkItem(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req workitems.CreateWorkItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ProjectID = projectID
	item, err := h.workItemService.Create(req, CurrentUserID(r), Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

func (h *Handler) ListWorkItems(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	items, err := h.workItemService.ListByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list work items", err)
		return
	}
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) GetWorkItem(w http.ResponseWriter, r *http.Request) {
	item, activity, err := h.workItemService.GetWithActivity(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleViewer) {
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"item":     item,
		"activity": activity,
	})
}

func (h *Handler) UpdateWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleEditor) {
		return
	}
	var req workitems.UpdateWorkItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.workItemService.Update(id, req, Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleEditor) {
		return
	}
	if err := h.workItemService.Delete(id); err != nil {
		respondInternal(w, r, "failed to delete work item", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleEditor) {
		return
	}
	var req workitems.MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	moved, err := h.workItemService.Move(id, req, Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(moved)
}

func (h *Handler) CommentWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleViewer) {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	activity, err := h.workItemService.AddComment(id, req.Content, Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(activity)
}

// --- Guided sessions ---

func (h *Handler) StartGuidedSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}
	session, err := h.guidedService.StartSession(req.ProjectID, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(session)
}

func (h *Handler) ListGuidedSessions(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	sessions, err := h.guidedService.ListSessions(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list guided sessions", err)
		return
	}
	json.NewEncoder(w).Encode(sessions)
}

func (h *Handler) getGuidedSessionChecked(w http.ResponseWriter, r *http.Request, minRole string) *guided.Session {
	session, err := h.guidedService.GetSession(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "guided session not found", err)
		return nil
	}
	if !h.requireProjectRole(w, r, session.ProjectID, minRole) {
		return nil
	}
	return session
}

func (h *Handler) GetGuidedSession(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleViewer)
	if session == nil {
		return
	}
	json.NewEncoder(w).Encode(session)
}

func (h *Handler) SaveGuidedStep(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	var req struct {
		Step    int                    `json:"step"`
		Answers map[string]interface{} `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.guidedService.SaveStep(session.ID, req.Step, req.Answers)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) MaterializeGuidedDrafts(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	var req struct {
		Drafts []guided.DraftSpec `json:"drafts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ids, err := h.guidedService.MaterializeDrafts(session.ID, req.Drafts)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"artifact_ids": ids})
}

func (h *Handler) CommitGuidedSession(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	committed, err := h.guidedService.Commit(session.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(committed)
}

func (h *Handler) AbandonGuidedSession(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	abandoned, err := h.guidedService.Abandon(session.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(abandoned)
}

// --- Guided copilot chat ---

// guidedRunnerOnline reports whether any runner is currently polling for the
// session project's workspace — without one, copilot turns queue unanswered
// and the chat panel should say "not connected" instead of "thinking".
func (h *Handler) guidedRunnerOnline(session *guided.Session) bool {
	project, err := h.projectService.GetProject(session.ProjectID)
	if err != nil || project == nil {
		return false
	}
	keys, err := h.workerKeyService.List(project.OrgID)
	if err != nil {
		return false
	}
	for _, key := range keys {
		if !key.Revoked && key.LastUsedAt != nil && time.Since(*key.LastUsedAt) < workerOnlineWindow {
			return true
		}
	}
	return false
}

// guidedStepLabels mirrors the wizard's step names for prompt context.
var guidedStepLabels = []string{
	"Product framing", "Personas", "User needs", "Requirements",
	"NFRs & constraints", "Hazards", "Verification stubs", "Review & commit",
}

func (h *Handler) ListGuidedChatMessages(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleViewer)
	if session == nil {
		return
	}
	transcript, err := h.guidedService.GetChatTranscript(session.ID)
	if err != nil {
		respondInternal(w, r, "failed to load chat transcript", err)
		return
	}
	json.NewEncoder(w).Encode(transcript)
}

func (h *Handler) PostGuidedChatMessage(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	var req struct {
		Content string                 `json:"content"`
		Step    int                    `json:"step"`
		State   map[string]interface{} `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSONError(w, http.StatusBadRequest, "message content is required")
		return
	}
	message, err := h.guidedService.AppendChatMessage(session.ID, guided.ChatRoleUser, req.Content)
	if err != nil {
		respondInternal(w, r, "failed to append chat message", err)
		return
	}
	h.sseHub.BroadcastSession("guided:"+session.ID, "message", message)

	if err := h.launchGuidedTurn(r, session, req.Step, req.State, ""); err != nil {
		note, _ := h.guidedService.AppendChatMessage(session.ID, guided.ChatRoleSystem,
			"The copilot is unavailable right now ("+err.Error()+"). Your message was saved — please try again shortly.")
		if note != nil {
			h.sseHub.BroadcastSession("guided:"+session.ID, "message", note)
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":       message,
		"runner_online": h.guidedRunnerOnline(session),
	})
}

// KickoffGuidedChat launches an opening copilot turn for a session whose chat
// is still empty, so the AI speaks first. No-op when messages exist or a turn
// is already pending.
func (h *Handler) KickoffGuidedChat(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	var req struct {
		Step  int                    `json:"step"`
		State map[string]interface{} `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runnerOnline := h.guidedRunnerOnline(session)
	reply := func(status string) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        status,
			"runner_online": runnerOnline,
		})
	}
	transcript, err := h.guidedService.GetChatTranscript(session.ID)
	if err != nil {
		respondInternal(w, r, "failed to load chat transcript", err)
		return
	}
	if len(transcript) > 0 {
		reply("skipped")
		return
	}
	if session.AgentRunID != nil {
		if run, err := h.runService.Get(*session.AgentRunID); err == nil && run != nil {
			switch run.Status {
			case agentruns.StatusQueued, agentruns.StatusClaimed, agentruns.StatusRunning:
				reply("pending")
				return
			}
		}
	}
	if err := h.launchGuidedTurn(r, session, req.Step, req.State, ""); err != nil {
		note, _ := h.guidedService.AppendChatMessage(session.ID, guided.ChatRoleSystem,
			"The copilot is unavailable right now ("+err.Error()+"). You can keep filling in the wizard and try the chat again shortly.")
		if note != nil {
			h.sseHub.BroadcastSession("guided:"+session.ID, "message", note)
		}
		reply("unavailable")
		return
	}
	reply("launched")
}

// NudgeGuidedChat launches a copilot turn in reaction to a wizard action
// (saving or skipping a step) without a chat message from the user, so the
// copilot comments on newly entered data as the user progresses. Silently
// skipped while a turn is already pending.
func (h *Handler) NudgeGuidedChat(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	var req struct {
		Step  int                    `json:"step"`
		State map[string]interface{} `json:"state"`
		Event string                 `json:"event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The response tells the chat panel whether a reply is coming, so it can
	// show (or clear) its thinking indicator: "launched" and "pending" both
	// mean a copilot message will arrive; "unavailable" means none will; and
	// runner_online=false means turns are queuing with nobody to answer.
	runnerOnline := h.guidedRunnerOnline(session)
	reply := func(status string) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        status,
			"runner_online": runnerOnline,
		})
	}
	if session.AgentRunID != nil {
		if run, err := h.runService.Get(*session.AgentRunID); err == nil && run != nil {
			switch run.Status {
			case agentruns.StatusQueued, agentruns.StatusClaimed, agentruns.StatusRunning:
				reply("pending")
				return
			}
		}
	}
	event := strings.TrimSpace(req.Event)
	if event == "" {
		event = "updated the wizard"
	}
	// Nudges are best-effort commentary: no system note on failure.
	if err := h.launchGuidedTurn(r, session, req.Step, req.State, event); err != nil {
		reply("unavailable")
		return
	}
	reply("launched")
}

// StreamGuidedChat is the wizard's copilot SSE channel.
func (h *Handler) StreamGuidedChat(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleViewer)
	if session == nil {
		return
	}
	h.sseHub.ServeStream(w, r, "guided:"+session.ID, func(emit func(event string, data interface{})) error {
		transcript, err := h.guidedService.GetChatTranscript(session.ID)
		if err != nil {
			return err
		}
		for _, m := range transcript {
			emit("message", m)
		}
		return nil
	})
}

// launchGuidedTurn enqueues one copilot response as a priority run. event,
// when non-empty, describes a wizard action the user took without chatting
// (e.g. saving a step) for the copilot to react to.
func (h *Handler) launchGuidedTurn(r *http.Request, session *guided.Session, step int, state map[string]interface{}, event string) error {
	// The copilot agent lives in the project's workspace.
	orgID := ""
	if project, err := h.projectService.GetProject(session.ProjectID); err == nil && project != nil {
		orgID = project.OrgID
	}
	if orgID == "" {
		return fmt.Errorf("could not resolve workspace for guided session project %s", session.ProjectID)
	}
	agent, err := h.agentService.GetBySlug(orgID, "requirements-copilot")
	if err != nil || agent == nil {
		return fmt.Errorf("requirements-copilot agent is not available in this workspace")
	}

	transcript, err := h.guidedService.GetChatTranscript(session.ID)
	if err != nil {
		return err
	}
	profile, _ := h.productService.GetProfile(session.ProjectID)

	stepLabel := ""
	if step >= 1 && step <= len(guidedStepLabels) {
		stepLabel = guidedStepLabels[step-1]
	}

	var b strings.Builder
	b.WriteString("You are the requirements copilot inside the guided product-definition wizard. The user fills in manual entry sections step by step; you chat alongside, ask sharp questions grounded in what they have entered, and surface gaps: hazards, missing NFRs, ambiguous or untestable requirements, personas or needs without requirements.\n\n")
	if profile != nil {
		if profile.Vision != "" {
			b.WriteString("Product vision: " + profile.Vision + "\n")
		}
		if profile.ProblemStatement != "" {
			b.WriteString("Problem statement: " + profile.ProblemStatement + "\n")
		}
		if profile.TargetUsers != "" {
			b.WriteString("Target users: " + profile.TargetUsers + "\n")
		}
	}
	if stepLabel != "" {
		fmt.Fprintf(&b, "\nThe user is on wizard step %d of %d: %q.\n", step, len(guidedStepLabels), stepLabel)
	}
	if state == nil {
		state = session.Answers
	}
	if state != nil {
		if stateJSON, err := json.Marshal(state); err == nil {
			s := string(stateJSON)
			if len(s) > 12000 {
				s = s[:12000] + "…(truncated)"
			}
			b.WriteString("\nCurrent wizard state (everything entered so far):\n" + s + "\n")
			b.WriteString("State key legend: step_1 {vision, problem_statement, target_users} = Product framing; step_2.personas (each has a stable id); step_3.needs (persona_id references a step_2 persona's id); step_4.requirements (need_id references a step_3 need's id); step_5.nfrs; step_6.hazards; step_7 = test stubs; step 8 = review & commit; copilot_applied = keys of your suggestions already applied.\n")
		}
	}
	b.WriteString("\nConversation so far:\n")
	if len(transcript) == 0 {
		b.WriteString("(none — this is your opening message; greet in one sentence, no more. If the state above already contains content, react to it specifically — name what stands out and what is missing. If it is empty, ask what the product is and who it is for. Never open with suggestion blocks.)\n")
	}
	start := 0
	if len(transcript) > 40 {
		start = len(transcript) - 40
	}
	for _, m := range transcript[start:] {
		fmt.Fprintf(&b, "[%s] %s\n", m.Role, m.Content)
	}
	if event != "" {
		b.WriteString("\n[Event] The user just " + event + " — they did not send a chat message. React to the newly entered content in the state above: acknowledge specifics in their own words, flag the most important gap, risk, or hazard you notice in what they wrote, and ask one focused question or offer suggestion blocks where clearly valuable. Do not greet again and do not repeat earlier feedback.\n")
	}
	b.WriteString(`
Respond with your next chat message to the user.

How to respond:
- Be a conversation partner first. Ground every reply in what the user actually entered — quote or reference their own wording — before offering anything new. Never give generic requirements-engineering advice untethered from their content.
- Understand before you suggest: do not emit suggestion blocks until the conversation or wizard state gives you real grounding, and never lead with one. When the user asks you to draft, fill in, or improve something, answer with suggestion blocks (several at once is fine) instead of telling them what to type.
- Never invent facts about the product; ask when you need information. Keep replies short; ask at most two questions.
- Exception: when the user asks for a review, gap analysis, or conflict check, be systematic instead of brief — cover every relevant entry, cite each by its own wording, organize findings as a compact list, and attach replace suggestions for entries worth fixing.

When you propose a concrete entry for the wizard, put each one in its own fenced code block tagged openv-suggestion containing exactly one JSON object, using one of these shapes:
- {"kind":"framing","field":"vision|problem_statement|target_users","text":"..."} — full replacement text for that Product framing field (step 1); the user clicks Apply to fill the field with it
- {"kind":"persona","name":"","role":"","goals":"","pains":""}
- {"kind":"need","persona":"<existing persona name>","capability":"","outcome":""}
- {"kind":"requirement","need":"<capability of the user need it derives from>","text":"The system shall ...","fit_criterion":"","verification_method":"inspection|analysis|demonstration|test"}
- {"kind":"nfr","category":"Performance|Reliability|Usability|Security|Maintainability|Regulatory","text":"The system shall ...","fit_criterion":"","verification_method":"inspection|analysis|demonstration|test"}
- {"kind":"hazard","hazard":"","harm":"","severity":"minor|moderate|serious|critical"}

To improve or correct an entry the user already has, add "replaces":"<exact current value>" to the object — matched against the persona name, need capability, requirement text, NFR text, or hazard text respectively. The user then gets a Replace button that overwrites that entry in place instead of adding a duplicate. Entries already locked to artifacts (they show a green dot) cannot be replaced — propose a new entry instead. Omit "replaces" for brand-new entries. Framing suggestions always replace their field.

Example (new entry):
` + "```openv-suggestion\n" + `{"kind":"hazard","hazard":"Spindle starts while guard is open","harm":"Operator hand injury","severity":"critical"}` + "\n```" + `

Example (revision of an existing requirement):
` + "```openv-suggestion\n" + `{"kind":"requirement","replaces":"The system shall be fast","text":"The system shall render the requirements list within 500 ms for projects of up to 5,000 artifacts.","fit_criterion":"P95 list render time ≤ 500 ms at 5,000 artifacts","verification_method":"test"}` + "\n```" + `

The user clicks Add/Apply/Replace on a suggestion to put it into the wizard, so suggestions must be self-contained and match the shapes exactly. Never assume a suggestion was accepted until it appears in the wizard state. Do not create or modify OpenV artifacts yourself.`)

	sessionID := session.ID
	projectID := session.ProjectID
	run, _, err := h.runService.Launch(agentruns.LaunchRequest{
		OrgID:           orgID,
		AgentID:         agent.ID,
		ProjectID:       &projectID,
		GuidedSessionID: &sessionID,
		Priority:        agentruns.PriorityInterview,
		Prompt:          b.String(),
		LaunchedBy:      CurrentUserID(r),
	})
	if err != nil {
		return err
	}
	if err := h.guidedService.AttachAgentRun(session.ID, run.ID); err != nil {
		fmt.Printf("Warning: failed to attach copilot run %s to guided session %s: %v\n", run.ID, session.ID, err)
	}
	return nil
}

// --- Interviews (internal) ---

func (h *Handler) CreateInterview(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req struct {
		Name              string  `json:"name"`
		Brief             string  `json:"brief"`
		AgentSlug         string  `json:"agent_slug"`
		GuidedSessionID   *string `json:"guided_session_id"`
		PersonaArtifactID *string `json:"persona_artifact_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.validPersonaForProject(w, req.PersonaArtifactID, projectID) {
		return
	}
	var agentID *string
	slug := req.AgentSlug
	if slug == "" {
		slug = "requirements-interviewer"
	}
	// The interviewer agent lives in the project's workspace.
	interviewOrg := ActiveOrg(r)
	if project, err := h.projectService.GetProject(projectID); err == nil && project != nil && project.OrgID != "" {
		interviewOrg = project.OrgID
	}
	if agent, err := h.agentService.GetBySlug(interviewOrg, slug); err == nil && agent != nil {
		agentID = &agent.ID
	}
	interview, err := h.interviewService.CreateInterview(projectID, req.Name, req.Brief, agentID, req.GuidedSessionID, req.PersonaArtifactID, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(interview)
}

func (h *Handler) ListInterviews(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.interviewService.ListInterviews(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list interviews", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) getInterviewChecked(w http.ResponseWriter, r *http.Request, minRole string) *interviews.Interview {
	interview, err := h.interviewService.GetInterview(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "interview not found", err)
		return nil
	}
	if !h.requireProjectRole(w, r, interview.ProjectID, minRole) {
		return nil
	}
	return interview
}

// validPersonaForProject checks that a persona artifact reference points at a
// persona-type artifact in the given project. A nil reference is valid (the
// link is optional). Writes an HTTP error and returns false when invalid.
func (h *Handler) validPersonaForProject(w http.ResponseWriter, personaArtifactID *string, projectID string) bool {
	if personaArtifactID == nil {
		return true
	}
	artifact, err := h.artifactService.GetArtifact(*personaArtifactID)
	if err != nil || artifact == nil {
		writeJSONError(w, http.StatusBadRequest, "persona artifact not found")
		return false
	}
	if artifact.ProjectID != projectID {
		writeJSONError(w, http.StatusBadRequest, "persona artifact belongs to a different project")
		return false
	}
	if artifact.Type != artifacts.TypePersona {
		writeJSONError(w, http.StatusBadRequest, "artifact is not a persona")
		return false
	}
	return true
}

// SetInterviewPersona links an interview to a persona artifact (or clears the
// link when persona_artifact_id is null).
func (h *Handler) SetInterviewPersona(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleEditor)
	if interview == nil {
		return
	}
	var req struct {
		PersonaArtifactID *string `json:"persona_artifact_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.validPersonaForProject(w, req.PersonaArtifactID, interview.ProjectID) {
		return
	}
	updated, err := h.interviewService.SetInterviewPersona(interview.ID, req.PersonaArtifactID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) CloseInterview(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleEditor)
	if interview == nil {
		return
	}
	closed, err := h.interviewService.CloseInterview(interview.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(closed)
}

func (h *Handler) CreateInterviewInvite(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleEditor)
	if interview == nil {
		return
	}
	var req struct {
		InviteeLabel string     `json:"invitee_label"`
		ExpiresAt    *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	invite, token, err := h.interviewService.CreateInvite(interview.ID, req.InviteeLabel, req.ExpiresAt)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"invite": invite,
		"token":  token,
		"path":   "/interview/" + token,
	})
}

func (h *Handler) ListInterviewInvites(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleViewer)
	if interview == nil {
		return
	}
	list, err := h.interviewService.ListInvites(interview.ID)
	if err != nil {
		respondInternal(w, r, "failed to list invites", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) RevokeInterviewInvite(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	invite, err := h.interviewService.GetInvite(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "invite not found", err)
		return
	}
	interview, err := h.interviewService.GetInterview(invite.InterviewID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "interview not found", err)
		return
	}
	if !h.requireProjectRole(w, r, interview.ProjectID, members.RoleEditor) {
		return
	}
	if err := h.interviewService.RevokeInvite(invite.ID); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListInterviewSessions(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleViewer)
	if interview == nil {
		return
	}
	list, err := h.interviewService.ListSessions(interview.ID)
	if err != nil {
		respondInternal(w, r, "failed to list interview sessions", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

// ListProjectInterviewSessions returns the most recent sessions across every
// interview in a project, newest first — one call for summary cards instead
// of a listSessions fan-out per interview. ?limit=N bounds the page; the
// domain service applies the default and cap.
func (h *Handler) ListProjectInterviewSessions(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = n
	}
	list, err := h.interviewService.ListProjectSessions(projectID, limit)
	if err != nil {
		respondInternal(w, r, "failed to list interview sessions", err)
		return
	}
	if list == nil {
		list = []*interviews.Session{}
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) GetInterviewTranscript(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	session, err := h.interviewService.GetSession(mux.Vars(r)["id"])
	if err != nil || session == nil {
		writeJSONError(w, http.StatusNotFound, "interview session not found")
		return
	}
	interview, err := h.interviewService.GetInterview(session.InterviewID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "interview not found", err)
		return
	}
	if !h.requireProjectRole(w, r, interview.ProjectID, members.RoleViewer) {
		return
	}
	transcript, err := h.interviewService.GetTranscript(session.ID)
	if err != nil {
		respondInternal(w, r, "failed to load transcript", err)
		return
	}
	json.NewEncoder(w).Encode(transcript)
}

// --- Interviews (public token flow) ---

// respondInviteError answers a failed invite-token resolution: the
// participant-facing verdicts (unknown, revoked, expired, interview closed)
// pass through as 404s, anything else is an internal failure.
func respondInviteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, interviews.ErrInviteNotFound),
		errors.Is(err, interviews.ErrInviteRevoked),
		errors.Is(err, interviews.ErrInviteExpired),
		errors.Is(err, interviews.ErrInterviewClosed):
		writeJSONError(w, http.StatusNotFound, err.Error())
	default:
		respondInternal(w, r, "failed to resolve invite", err)
	}
}

func (h *Handler) PublicInterviewIntro(w http.ResponseWriter, r *http.Request) {
	if !h.allowInterviewRead(w, r) {
		return
	}
	interview, invite, err := h.interviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	// Read-only: a page view must not write. A first visit simply has no
	// session yet (the UI shows the name prompt); the session is created by
	// the first message (or the stream, which needs one for its channel).
	session, _ := h.interviewService.FindActiveSession(invite.ID)
	var transcript []*interviews.Message
	if session != nil {
		transcript, _ = h.interviewService.GetTranscript(session.ID)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"interview_name": interview.Name,
		"session":        session,
		"transcript":     transcript,
	})
}

// PublicInterviewMessage records a participant message and enqueues an
// interviewer turn run.
func (h *Handler) PublicInterviewMessage(w http.ResponseWriter, r *http.Request) {
	interview, invite, err := h.interviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	var req struct {
		ParticipantName string `json:"participant_name"`
		Content         string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeJSONError(w, http.StatusBadRequest, "message content is required")
		return
	}
	// Every message enqueues a priority LLM run, so throttle per invite:
	// a leaked link cannot rack up unbounded provider cost.
	if ok, retryAfter := h.interviewMsgLimiter.allow(invite.ID); !ok {
		writeRateLimited(w,
			"You're sending messages a little too quickly. Please wait a moment and try again.",
			retryAfter)
		return
	}
	session, err := h.interviewService.StartOrResumeSession(invite.ID, interview.ID, req.ParticipantName)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	message, err := h.interviewService.AppendMessage(session.ID, interviews.RoleParticipant, req.Content)
	if err != nil {
		respondInternal(w, r, "failed to append message", err)
		return
	}
	h.sseHub.BroadcastSession("interview:"+session.ID, "message", message)

	if err := h.launchInterviewTurn(interview, session); err != nil {
		// Surface but don't fail the message write; the participant sees
		// a system note instead of silence.
		note, _ := h.interviewService.AppendMessage(session.ID, interviews.RoleSystem,
			"The interviewer is unavailable right now. Your answer was saved — please check back shortly.")
		if note != nil {
			h.sseHub.BroadcastSession("interview:"+session.ID, "message", note)
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session": session,
		"message": message,
	})
}

// launchInterviewTurn enqueues one interviewer response as a priority run.
func (h *Handler) launchInterviewTurn(interview *interviews.Interview, session *interviews.Session) error {
	if interview.AgentID == nil {
		return fmt.Errorf("interview has no interviewer agent")
	}
	transcript, err := h.interviewService.GetTranscript(session.ID)
	if err != nil {
		return err
	}
	profile, _ := h.productService.GetProfile(interview.ProjectID)

	var b strings.Builder
	b.WriteString("You are conducting a requirements-elicitation interview.\n\n")
	b.WriteString("Interview brief: " + interview.Brief + "\n")
	if interview.PersonaArtifactID != nil {
		if persona, err := h.artifactService.GetArtifact(*interview.PersonaArtifactID); err == nil && persona != nil {
			b.WriteString("Target persona: " + persona.Title + "\n")
			if persona.Body != "" {
				b.WriteString("Persona description: " + persona.Body + "\n")
			}
		}
	}
	if profile != nil {
		if profile.Vision != "" {
			b.WriteString("Product vision: " + profile.Vision + "\n")
		}
		if profile.ProblemStatement != "" {
			b.WriteString("Problem statement: " + profile.ProblemStatement + "\n")
		}
	}
	if session.ParticipantName != "" {
		b.WriteString("Participant: " + session.ParticipantName + "\n")
	}
	b.WriteString("\nConversation so far:\n")
	for _, m := range transcript {
		b.WriteString(fmt.Sprintf("[%s] %s\n", m.Role, m.Content))
	}
	b.WriteString("\nRespond with your next message to the participant: one question at a time, plain language, natural conversation. When you learn a concrete need, record it with the record_candidate_need tool before replying.")

	// Interview turns belong to the interview's project's org.
	orgID := ""
	if project, err := h.projectService.GetProject(interview.ProjectID); err == nil && project != nil {
		orgID = project.OrgID
	}
	if orgID == "" {
		return fmt.Errorf("could not resolve workspace for interview project %s", interview.ProjectID)
	}

	sessionID := session.ID
	projectID := interview.ProjectID
	_, _, err = h.runService.Launch(agentruns.LaunchRequest{
		OrgID:              orgID,
		AgentID:            *interview.AgentID,
		ProjectID:          &projectID,
		InterviewSessionID: &sessionID,
		Priority:           agentruns.PriorityInterview,
		Prompt:             b.String(),
	})
	return err
}

// allowInterviewRead applies the coarse per-IP bucket shared by the
// unauthenticated interview GETs (intro + stream). Returns false after
// writing the 429 when the caller is over budget.
func (h *Handler) allowInterviewRead(w http.ResponseWriter, r *http.Request) bool {
	if ok, retryAfter := h.interviewIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w,
			"Too many requests from your network. Please wait a moment and reload the page.",
			retryAfter)
		return false
	}
	return true
}

// PublicInterviewStream is the participant's SSE channel.
func (h *Handler) PublicInterviewStream(w http.ResponseWriter, r *http.Request) {
	if !h.allowInterviewRead(w, r) {
		return
	}
	interview, invite, err := h.interviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	// The SSE channel is keyed by session, so the stream genuinely needs
	// one; StartOrResumeSession reuses the active session when it exists.
	session, err := h.interviewService.StartOrResumeSession(invite.ID, interview.ID, "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.sseHub.ServeStream(w, r, "interview:"+session.ID, func(emit func(event string, data interface{})) error {
		transcript, err := h.interviewService.GetTranscript(session.ID)
		if err != nil {
			return err
		}
		for _, m := range transcript {
			emit("message", m)
		}
		return nil
	})
}

// PublicInterviewFinish ends the session and enqueues a summary turn.
func (h *Handler) PublicInterviewFinish(w http.ResponseWriter, r *http.Request) {
	interview, invite, err := h.interviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	// Nothing to finish when no session was ever started — don't create an
	// empty session just to complete it.
	session, err := h.interviewService.FindActiveSession(invite.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if session == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.interviewService.CompleteSession(session.ID, ""); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.publish(r, events.ChatterCreated, interview.ProjectID, session.ID, map[string]interface{}{
		"kind": "interview-completed",
	})
	w.WriteHeader(http.StatusNoContent)
}
