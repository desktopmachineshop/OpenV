package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// registerGuidedSessionRoutes wires the guided wizard's sessions: start,
// list and read one, save a step, materialize its drafts, commit and
// abandon. The copilot chat beside it comes from
// registerGuidedCopilotRoutes.
func (h *Handler) registerGuidedSessionRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/guided-sessions", h.StartGuidedSession).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions", h.ListGuidedSessions).Methods("GET")
	router.HandleFunc("/api/v1/guided-sessions/{id}", h.GetGuidedSession).Methods("GET")
	router.HandleFunc("/api/v1/guided-sessions/{id}/step", h.SaveGuidedStep).Methods("PUT")
	router.HandleFunc("/api/v1/guided-sessions/{id}/drafts", h.MaterializeGuidedDrafts).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/commit", h.CommitGuidedSession).Methods("POST")
	router.HandleFunc("/api/v1/guided-sessions/{id}/abandon", h.AbandonGuidedSession).Methods("POST")
}

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

// CommitGuidedSession approves the session's drafts and closes it. Approval
// is a review-state change, so proposal-mode agent runs are refused, as
// ChangeArtifactStatus refuses them: a review-gated agent must not sign off
// artifacts.
func (h *Handler) CommitGuidedSession(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	if run := CurrentRun(r); run != nil && h.agentService != nil {
		if agent, err := h.agentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot commit a guided session")
			return
		}
	}
	result, err := h.guidedService.Commit(session.ID)
	if result != nil {
		h.publishGuidedApprovals(r, session.ID, result.Approved)
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(result.Session)
}

// publishGuidedApprovals records each draft a guided commit approved in the
// sign-off history, the event stream (issue #127), with the event an approval
// by hand gets, so the committing user is its actor and it reaches the
// activity log and triggered automations. Only the approval is published: the
// draft's step into review just before it would ask every editor, through
// the notifier, to review something the commit has already signed off. The
// approvals a failed commit made before failing are published too, since a
// retry leaves them as they are.
func (h *Handler) publishGuidedApprovals(r *http.Request, sessionID string, approved []*artifacts.Artifact) {
	for _, a := range approved {
		h.publish(r, events.ArtifactStatusChanged, a.ProjectID, a.ID, map[string]interface{}{
			"artifact_type":  a.Type,
			"title":          a.Title,
			"from":           artifacts.StatusInReview,
			"to":             a.Status,
			"version":        a.Version,
			"guided_session": sessionID,
		})
	}
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
