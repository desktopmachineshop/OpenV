package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// CreateInterview names the interviewer whose run each participant message
// launches (launchInterviewTurn), so a proposal-mode run is refused it as a
// launch, as it is the invite (requireNoProposalRunLaunch).
func (h *Handler) CreateInterview(w http.ResponseWriter, r *http.Request) {
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
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
	if !h.validPersonaForProject(w, r, req.PersonaArtifactID, projectID) {
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
	if !h.requireProjectRoleFor(w, r, interview.ProjectID, minRole, missing("interview not found")) {
		return nil
	}
	return interview
}

// validPersonaForProject checks that a persona artifact reference points at a
// persona-type artifact in the given project. A nil reference is valid (the
// link is optional). Writes an HTTP error and returns false when invalid; an
// artifact in a project the caller cannot reach at all is one no row has (I3).
func (h *Handler) validPersonaForProject(w http.ResponseWriter, r *http.Request, personaArtifactID *string, projectID string) bool {
	if personaArtifactID == nil {
		return true
	}
	artifact, err := h.artifactService.GetArtifact(*personaArtifactID)
	if err != nil || artifact == nil {
		personaNotFound.write(w)
		return false
	}
	if artifact.ProjectID != projectID && !h.requireProjectVisible(w, r, artifact.ProjectID, personaNotFound) {
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

// personaNotFound answers a persona reference no artifact has, or one in a
// project the caller cannot reach at all.
var personaNotFound = notFound{http.StatusBadRequest, "persona artifact not found"}

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
	if !h.validPersonaForProject(w, r, req.PersonaArtifactID, interview.ProjectID) {
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
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
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
	if !h.requireProjectRoleFor(w, r, interview.ProjectID, members.RoleEditor, missing("invite not found")) {
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
	if !h.requireProjectRoleFor(w, r, interview.ProjectID, members.RoleViewer, missing("interview session not found")) {
		return
	}
	transcript, err := h.interviewService.GetTranscript(session.ID)
	if err != nil {
		respondInternal(w, r, "failed to load transcript", err)
		return
	}
	json.NewEncoder(w).Encode(transcript)
}
