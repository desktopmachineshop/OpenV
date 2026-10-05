package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/interviews"
)

// registerPublicInterviewRoutes wires a participant's interview, which an
// invite token authenticates: the intro, a message, the stream and finish.
func (h *Handler) registerPublicInterviewRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/public/interviews/{token}", h.PublicInterviewIntro).Methods("GET")
	router.HandleFunc("/api/v1/public/interviews/{token}/messages", h.PublicInterviewMessage).Methods("POST")
	router.HandleFunc("/api/v1/public/interviews/{token}/stream", h.PublicInterviewStream).Methods("GET")
	router.HandleFunc("/api/v1/public/interviews/{token}/finish", h.PublicInterviewFinish).Methods("POST")
}

func (h *Handler) PublicInterviewIntro(w http.ResponseWriter, r *http.Request) {
	if !h.allowInterviewRead(w, r) {
		return
	}
	interview, invite, err := h.InterviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	// Read-only: a page view must not write. A first visit simply has no
	// session yet (the UI shows the name prompt); the session is created by
	// the first message (or the stream, which needs one for its channel).
	// With no active session, the invite's latest one answers: once the
	// participant has ended the interview it is completed, and the page
	// shows its thank-you and opens no stream, which would start a new
	// session on the same invite (#379 bug 214). Only an active session's
	// transcript is sent: an ended one answers without it (null), so the
	// link reads no finished conversation, as it could not before, and the
	// thank-you page shows none.
	session, _ := h.InterviewService.FindActiveSession(invite.ID)
	if session == nil {
		session, _ = h.InterviewService.FindLatestSession(invite.ID)
	}
	var transcript []*interviews.Message
	if session != nil && session.Status == interviews.SessionStatusActive {
		transcript, _ = h.InterviewService.GetTranscript(session.ID)
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
	interview, invite, err := h.InterviewService.ResolveInviteToken(mux.Vars(r)["token"])
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
	session, err := h.InterviewService.StartOrResumeSession(invite.ID, interview.ID, req.ParticipantName)
	if err != nil {
		// Unauthenticated endpoint: StartOrResumeSession only fails on
		// storage errors, whose text must never reach the public. The real
		// error goes to the server log.
		respondInternal(w, r, "we could not start your interview session; please try again in a moment", err)
		return
	}
	message, err := h.InterviewService.AppendMessage(session.ID, interviews.RoleParticipant, req.Content)
	if err != nil {
		respondInternal(w, r, "failed to append message", err)
		return
	}
	h.SSEHub.BroadcastSession("interview:"+session.ID, "message", message)

	if err := h.launchInterviewTurn(interview, session); err != nil {
		// Surface but don't fail the message write; the participant sees
		// a system note instead of silence.
		note, _ := h.InterviewService.AppendMessage(session.ID, interviews.RoleSystem,
			"The interviewer is unavailable right now. Your answer was saved — please check back shortly.")
		if note != nil {
			h.SSEHub.BroadcastSession("interview:"+session.ID, "message", note)
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
	transcript, err := h.InterviewService.GetTranscript(session.ID)
	if err != nil {
		return err
	}
	profile, _ := h.ProductService.GetProfile(interview.ProjectID)

	var b strings.Builder
	b.WriteString("You are conducting a requirements-elicitation interview.\n\n")
	b.WriteString("Interview brief: " + interview.Brief + "\n")
	if interview.PersonaArtifactID != nil {
		if persona, err := h.ArtifactService.GetArtifact(*interview.PersonaArtifactID); err == nil && persona != nil {
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
	if project, err := h.ProjectService.GetProject(interview.ProjectID); err == nil && project != nil {
		orgID = project.OrgID
	}
	if orgID == "" {
		return fmt.Errorf("could not resolve workspace for interview project %s", interview.ProjectID)
	}

	sessionID := session.ID
	projectID := interview.ProjectID
	_, _, err = h.RunService.Launch(agentruns.LaunchRequest{
		OrgID:              orgID,
		AgentID:            *interview.AgentID,
		ProjectID:          &projectID,
		InterviewSessionID: &sessionID,
		Priority:           agentruns.PriorityInterview,
		Prompt:             b.String(),
	})
	return err
}

// allowInterviewRead applies the coarse per-IP bucket for the
// unauthenticated interview intro GET. Returns false after writing the 429
// when the caller is over budget.
func (h *Handler) allowInterviewRead(w http.ResponseWriter, r *http.Request) bool {
	if ok, retryAfter := h.interviewIPLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w,
			"Too many requests from your network. Please wait a moment and reload the page.",
			retryAfter)
		return false
	}
	return true
}

// PublicInterviewStream is the participant's SSE channel. Stream connects are
// charged to their own, more generous per-IP bucket rather than the intro
// bucket: EventSource clients auto-reconnect after every network hiccup or
// NAT timeout, so reconnects are routine and must stay cheaper than intro
// page loads (see ratelimit.go for the rationale and knobs).
func (h *Handler) PublicInterviewStream(w http.ResponseWriter, r *http.Request) {
	if ok, retryAfter := h.interviewStreamLimiter.allow(clientIP(r)); !ok {
		writeRateLimited(w,
			"Too many stream connections from your network. Please wait a moment and reload the page.",
			retryAfter)
		return
	}
	interview, invite, err := h.InterviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	// The SSE channel is keyed by session, so the stream genuinely needs
	// one; StartOrResumeSession reuses the active session when it exists.
	session, err := h.InterviewService.StartOrResumeSession(invite.ID, interview.ID, "")
	if err != nil {
		// Unauthenticated endpoint: storage-error text must never leak.
		respondInternal(w, r, "we could not open your interview stream; please reload the page", err)
		return
	}
	h.SSEHub.ServeStream(w, r, "interview:"+session.ID, func(emit func(event string, data interface{})) error {
		transcript, err := h.InterviewService.GetTranscript(session.ID)
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
	interview, invite, err := h.InterviewService.ResolveInviteToken(mux.Vars(r)["token"])
	if err != nil {
		respondInviteError(w, r, err)
		return
	}
	// Nothing to finish when no session was ever started — don't create an
	// empty session just to complete it.
	session, err := h.InterviewService.FindActiveSession(invite.ID)
	if err != nil {
		// Unauthenticated endpoint: only storage errors land here, and
		// their text must never leak to the public.
		respondInternal(w, r, "we could not look up your interview session; please try again in a moment", err)
		return
	}
	if session == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.InterviewService.CompleteSession(session.ID, ""); err != nil {
		// The session vanishing between lookup and completion is a benign
		// race with the domain sentinel's safe text; anything else is
		// internal and stays out of the public response.
		if errors.Is(err, interviews.ErrSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		respondInternal(w, r, "we could not finish your interview session; please try again in a moment", err)
		return
	}
	h.publish(r, events.ChatterCreated, interview.ProjectID, session.ID, map[string]interface{}{
		"kind": "interview-completed",
	})
	w.WriteHeader(http.StatusNoContent)
}
