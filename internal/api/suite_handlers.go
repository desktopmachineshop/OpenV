package api

import (
	"github.com/gorilla/mux"
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
	router.HandleFunc("/api/v1/projects/{id}/impact", h.GetImpact).Methods("GET")

	// Requirement quality linting (issue #217) and the rule sets it judges
	// against: workspace house style, overridable per project.
	router.HandleFunc("/api/v1/projects/{id}/quality", h.GetProjectQuality).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}/quality", h.GetArtifactQuality).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/quality-rules", h.GetProjectQualityRules).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/quality-rules", h.UpdateProjectQualityRules).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/parties", h.GetProjectParties).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/parties", h.UpdateProjectParties).Methods("PUT")

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

// --- Reference parties (REQ-147) ---

// --- Product profile ---

// --- V&V ---

// --- Requirement quality linting (issue #217) ---

// --- Work items ---

// --- Guided sessions ---

// --- Guided copilot chat ---

// --- Interviews (internal) ---

// --- Interviews (public token flow) ---
