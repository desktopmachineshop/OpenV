package api

import (
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/notify"
)

// registerInterviewRoutes wires the interviews' internal management: a
// project's interviews, closing one and its persona, its invites, and the
// sessions and their transcripts. A participant's token flow comes from
// registerPublicInterviewRoutes.
func (h *Handler) registerInterviewRoutes(router *mux.Router) {
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
}

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
	if !decodeJSON(w, r, &req) {
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
	if project, err := h.ProjectService.GetProject(projectID); err == nil && project != nil && project.OrgID != "" {
		interviewOrg = project.OrgID
	}
	if agent, err := h.AgentService.GetBySlug(interviewOrg, slug); err == nil && agent != nil {
		agentID = &agent.ID
	}
	interview, err := h.InterviewService.CreateInterview(projectID, req.Name, req.Brief, agentID, req.GuidedSessionID, req.PersonaArtifactID, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, interview)
}

func (h *Handler) ListInterviews(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	list, err := h.InterviewService.ListInterviews(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list interviews", err)
		return
	}
	writeJSONBare(w, list)
}

func (h *Handler) getInterviewChecked(w http.ResponseWriter, r *http.Request, minRole string) *interviews.Interview {
	interview, err := h.InterviewService.GetInterview(mux.Vars(r)["id"])
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
	artifact, err := h.ArtifactService.GetArtifact(*personaArtifactID)
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
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.validPersonaForProject(w, r, req.PersonaArtifactID, interview.ProjectID) {
		return
	}
	updated, err := h.InterviewService.SetInterviewPersona(interview.ID, req.PersonaArtifactID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, updated)
}

func (h *Handler) CloseInterview(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleEditor)
	if interview == nil {
		return
	}
	closed, err := h.InterviewService.CloseInterview(interview.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, closed)
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
		// Email, when given, is where OpenV sends the link (email-invites).
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email != "" {
		if !h.projectFeatureEnabled(r, interview.ProjectID, release.FeatureEmailInvites) {
			writeJSONError(w, http.StatusForbidden, featureGateMessage)
			return
		}
		email = users.NormalizeEmail(email)
		// A bare address only: a display name or a list would make the
		// recipient something other than what the list shows.
		if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
			writeJSONError(w, http.StatusBadRequest, "a valid email is required")
			return
		}
		// Sending is a relay to an address the caller chose, so it spends the
		// same per-account budget as a workspace invitation.
		if ok, retryAfter := h.inviteLimiter.allow(inviteBudgetKey(r)); !ok {
			writeRateLimited(w, "Too many invitations from this account; try again later.", retryAfter)
			return
		}
		// The address names the invite in the list when nothing else does.
		if strings.TrimSpace(req.InviteeLabel) == "" {
			req.InviteeLabel = email
		}
	}
	invite, token, err := h.InterviewService.CreateInvite(interview.ID, req.InviteeLabel, req.ExpiresAt)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]interface{}{
		"invite": invite,
		"token":  token,
		"path":   "/interview/" + token,
	}
	// emailed is answered only when an address was given, so a link-only
	// invite answers exactly as it always has.
	if email != "" {
		resp["emailed"] = h.sendInterviewInviteAsync(r, interview, invite, email, token)
	}
	writeJSONBareStatus(w, http.StatusCreated, resp)
}

// sendInterviewInviteAsync mails the invite's link without making the
// request wait, as a workspace invitation's goes out. It reports whether the
// send was QUEUED: false when the server has no mailer, so the member knows
// to send the link themselves. A failure is logged; the invite stands and
// its link can still be copied.
func (h *Handler) sendInterviewInviteAsync(r *http.Request, interview *interviews.Interview, invite *interviews.Invite, email, token string) bool {
	if h.Mailer == nil || !h.Mailer.Enabled() {
		return false
	}
	inviter := ""
	if user := CurrentUser(r); user != nil {
		inviter = user.Name
	}
	link := notify.InterviewInviteLink(h.EmailLinkBase, token)
	subject, body := notify.RenderInterviewInviteEmail(inviter, interview.Name, link, invite.ExpiresAt)
	go func() {
		if err := notify.SendWithTimeout(h.Mailer, email, subject, body, notify.VerificationSendTimeout); err != nil {
			slog.Warn("interview: failed to send invite email", "interview_id", interview.ID, "invite_id", invite.ID, "error", err)
		}
	}()
	return true
}

func (h *Handler) ListInterviewInvites(w http.ResponseWriter, r *http.Request) {
	interview := h.getInterviewChecked(w, r, members.RoleViewer)
	if interview == nil {
		return
	}
	list, err := h.InterviewService.ListInvites(interview.ID)
	if err != nil {
		respondInternal(w, r, "failed to list invites", err)
		return
	}
	writeJSONBare(w, list)
}

func (h *Handler) RevokeInterviewInvite(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	invite, err := h.InterviewService.GetInvite(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "invite not found", err)
		return
	}
	interview, err := h.InterviewService.GetInterview(invite.InterviewID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "interview not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, interview.ProjectID, members.RoleEditor, missing("invite not found")) {
		return
	}
	if err := h.InterviewService.RevokeInvite(invite.ID); err != nil {
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
	list, err := h.InterviewService.ListSessions(interview.ID)
	if err != nil {
		respondInternal(w, r, "failed to list interview sessions", err)
		return
	}
	writeJSONBare(w, list)
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
	limit, ok := parseLimit(w, r, interviewSessionsLimit)
	if !ok {
		return
	}
	list, err := h.InterviewService.ListProjectSessions(projectID, limit)
	if err != nil {
		respondInternal(w, r, "failed to list interview sessions", err)
		return
	}
	if list == nil {
		list = []*interviews.Session{}
	}
	writeJSONBare(w, list)
}

func (h *Handler) GetInterviewTranscript(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	session, err := h.InterviewService.GetSession(mux.Vars(r)["id"])
	if err != nil || session == nil {
		writeJSONError(w, http.StatusNotFound, "interview session not found")
		return
	}
	interview, err := h.InterviewService.GetInterview(session.InterviewID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "interview not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, interview.ProjectID, members.RoleViewer, missing("interview session not found")) {
		return
	}
	transcript, err := h.InterviewService.GetTranscript(session.ID)
	if err != nil {
		respondInternal(w, r, "failed to load transcript", err)
		return
	}
	writeJSONBare(w, transcript)
}
