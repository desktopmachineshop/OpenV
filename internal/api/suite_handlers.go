package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
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

	// Interviews (internal management).
	router.HandleFunc("/api/v1/projects/{id}/interviews", h.CreateInterview).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/interviews", h.ListInterviews).Methods("GET")
	router.HandleFunc("/api/v1/interviews/{id}/close", h.CloseInterview).Methods("POST")
	router.HandleFunc("/api/v1/interviews/{id}/invites", h.CreateInterviewInvite).Methods("POST")
	router.HandleFunc("/api/v1/interviews/{id}/invites", h.ListInterviewInvites).Methods("GET")
	router.HandleFunc("/api/v1/interview-invites/{id}/revoke", h.RevokeInterviewInvite).Methods("POST")
	router.HandleFunc("/api/v1/interviews/{id}/sessions", h.ListInterviewSessions).Methods("GET")
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	profile, err := h.productService.UpdateProfile(projectID, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.ProjectID = projectID
	run, err := h.vvService.CreateRun(req, CurrentUserID(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(runs)
}

func (h *Handler) GetTestRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.vvService.GetRun(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleEditor) {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.vvService.UpdateRunStatus(id, req.Status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteTestRun(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleEditor) {
		return
	}
	if err := h.vvService.DeleteRun(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpsertTestResult(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleEditor) {
		return
	}
	var req vv.UpsertResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result, err := h.vvService.UpsertResult(runID, req, CurrentUserID(r), Actor(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) ListTestResults(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	run, err := h.vvService.GetRun(runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, run.ProjectID, members.RoleViewer) {
		return
	}
	results, err := h.vvService.ListResults(runID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(results)
}

func (h *Handler) vvReportData(w http.ResponseWriter, r *http.Request) (*exports.ProjectExport, map[string]*vv.TestResult, bool) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return nil, nil, false
	}
	export, err := h.projectExport(projectID, r.URL.Query().Get("baseline_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, nil, false
	}
	latest, err := h.vvService.LatestResults(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	runs, err := h.vvService.ListRuns(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, filename, err := h.reportService.GenerateVVReport(projectID, r.URL.Query().Get("baseline_id"), latest, runs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.ProjectID = projectID
	item, err := h.workItemService.Create(req, CurrentUserID(r), Actor(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) GetWorkItem(w http.ResponseWriter, r *http.Request) {
	item, activity, err := h.workItemService.GetWithActivity(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleEditor) {
		return
	}
	var req workitems.UpdateWorkItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.workItemService.Update(id, req, Actor(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleEditor) {
		return
	}
	if err := h.workItemService.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleEditor) {
		return
	}
	var req workitems.MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	moved, err := h.workItemService.Move(id, req, Actor(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(moved)
}

func (h *Handler) CommentWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, item.ProjectID, members.RoleViewer) {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	activity, err := h.workItemService.AddComment(id, req.Content, Actor(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}
	session, err := h.guidedService.StartSession(req.ProjectID, CurrentUserID(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(sessions)
}

func (h *Handler) getGuidedSessionChecked(w http.ResponseWriter, r *http.Request, minRole string) *guided.Session {
	session, err := h.guidedService.GetSession(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	updated, err := h.guidedService.SaveStep(session.ID, req.Step, req.Answers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	ids, err := h.guidedService.MaterializeDrafts(session.ID, req.Drafts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	json.NewEncoder(w).Encode(abandoned)
}

// --- Interviews (internal) ---

func (h *Handler) CreateInterview(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req struct {
		Name            string  `json:"name"`
		Brief           string  `json:"brief"`
		AgentSlug       string  `json:"agent_slug"`
		GuidedSessionID *string `json:"guided_session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
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
	interview, err := h.interviewService.CreateInterview(projectID, req.Name, req.Brief, agentID, req.GuidedSessionID, CurrentUserID(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) getInterviewChecked(w http.ResponseWriter, r *http.Request, minRole string) *interviews.Interview {
	interview, err := h.interviewService.GetInterview(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil
	}
	if !h.requireProjectRole(w, r, interview.ProjectID, minRole) {
		return nil
	}
	return interview
}

func (h *Handler) CloseInterview(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleEditor)
	if interview == nil {
		return
	}
	closed, err := h.interviewService.CloseInterview(interview.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	invite, token, err := h.interviewService.CreateInvite(interview.ID, req.InviteeLabel, req.ExpiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	interview, err := h.interviewService.GetInterview(invite.InterviewID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, interview.ProjectID, members.RoleEditor) {
		return
	}
	if err := h.interviewService.RevokeInvite(invite.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) GetInterviewTranscript(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	session, err := h.interviewService.GetSession(mux.Vars(r)["id"])
	if err != nil || session == nil {
		http.Error(w, "interview session not found", http.StatusNotFound)
		return
	}
	interview, err := h.interviewService.GetInterview(session.InterviewID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.requireProjectRole(w, r, interview.ProjectID, members.RoleViewer) {
		return
	}
	transcript, err := h.interviewService.GetTranscript(session.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(transcript)
}

// --- Interviews (public token flow) ---

func (h *Handler) PublicInterviewIntro(w http.ResponseWriter, r *http.Request) {
	interview, invite, err := h.interviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	session, _ := h.interviewService.StartOrResumeSession(invite.ID, interview.ID, "")
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var req struct {
		ParticipantName string `json:"participant_name"`
		Content         string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "message content is required", http.StatusBadRequest)
		return
	}
	session, err := h.interviewService.StartOrResumeSession(invite.ID, interview.ID, req.ParticipantName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	message, err := h.interviewService.AppendMessage(session.ID, interviews.RoleParticipant, req.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

// PublicInterviewStream is the participant's SSE channel.
func (h *Handler) PublicInterviewStream(w http.ResponseWriter, r *http.Request) {
	interview, invite, err := h.interviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	session, err := h.interviewService.StartOrResumeSession(invite.ID, interview.ID, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	session, err := h.interviewService.StartOrResumeSession(invite.ID, interview.ID, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.interviewService.CompleteSession(session.ID, ""); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.publish(r, events.ChatterCreated, interview.ProjectID, session.ID, map[string]interface{}{
		"kind": "interview-completed",
	})
	w.WriteHeader(http.StatusNoContent)
}
