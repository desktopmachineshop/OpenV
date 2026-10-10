package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/traceability"
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
	if !decodeJSON(w, r, &req) {
		return
	}
	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}
	session, err := h.GuidedService.StartSession(req.ProjectID, CurrentUserID(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBareStatus(w, http.StatusCreated, session)
}

func (h *Handler) ListGuidedSessions(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	sessions, err := h.GuidedService.ListSessions(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list guided sessions", err)
		return
	}
	writeJSONBare(w, sessions)
}

func (h *Handler) GetGuidedSession(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleViewer)
	if session == nil {
		return
	}
	writeJSONBare(w, session)
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
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.GuidedService.SaveStep(session.ID, req.Step, req.Answers)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, updated)
}

// MaterializeGuidedDrafts creates the session's draft artifacts and their
// links. Drafts are artifact writes no proposal carries, so proposal-mode
// agent runs are refused, as CommitGuidedSession refuses them (#379 bug
// 195): a review-gated agent must not write past the review.
func (h *Handler) MaterializeGuidedDrafts(w http.ResponseWriter, r *http.Request) {
	session := h.getGuidedSessionChecked(w, r, members.RoleEditor)
	if session == nil {
		return
	}
	if run := CurrentRun(r); run != nil && h.AgentService != nil {
		if agent, err := h.AgentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot materialize guided drafts")
			return
		}
	}
	var req struct {
		Drafts []guided.DraftSpec `json:"drafts"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	h.dropRefusedDraftLinks(r, session, req.Drafts)
	ids, err := h.GuidedService.MaterializeDrafts(session.ID, req.Drafts)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, map[string]interface{}{"artifact_ids": ids})
}

// dropRefusedDraftLinks takes out of each draft the links a managed edit
// would skip as an add (processManagedLinkChanges), each with a warning; the
// draft itself is still created (#379 bug 193), as guidedDraftPolicy's
// OnInvalid says. The checks ask who is calling, so they are made here: the
// guided service has no request.
func (h *Handler) dropRefusedDraftLinks(r *http.Request, session *guided.Session, drafts []guided.DraftSpec) {
	orgID := h.orgIDForProject(session.ProjectID)
	trace := h.traceabilityFor(r)
	for i := range drafts {
		draft := &drafts[i]
		if len(draft.Links) == 0 {
			continue
		}
		kept := make([]guided.DraftLink, 0, len(draft.Links))
		for _, dl := range draft.Links {
			if err := h.draftLinkRefusal(trace, session.ProjectID, orgID, draft.Type, dl); err != nil {
				slog.Warn("api: skipping a guided draft's link", "session_id", session.ID, "draft_title", draft.Title,
					"link_type", dl.Type, "to_id", dl.ToID, "reason", err)
				continue
			}
			kept = append(kept, dl)
		}
		draft.Links = kept
	}
}

// draftLinkRefusal says why a draft of type draftType, to be created in
// projectID of workspace orgID, may not have the link dl, or nil when it may:
// the target must be an artifact of the same workspace, then the link rules
// must allow the type between the two and a target in another project needs
// editor rights there (guidedDraftPolicy, through trace.CheckLink).
func (h *Handler) draftLinkRefusal(trace *traceability.Service, projectID, orgID, draftType string, dl guided.DraftLink) error {
	target, err := h.ArtifactService.GetArtifact(dl.ToID)
	if err != nil || target == nil {
		return errors.New("no artifact has the target id")
	}
	if target.ProjectID != projectID && (orgID == "" || h.orgIDForProject(target.ProjectID) != orgID) {
		return errors.New("the target is in another workspace")
	}
	draft := traceability.End{Type: draftType, ProjectID: projectID}
	return trace.CheckLink(guidedDraftPolicy, projectID, draft, traceability.EndOf(target), dl.Type)
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
	if run := CurrentRun(r); run != nil && h.AgentService != nil {
		if agent, err := h.AgentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot commit a guided session")
			return
		}
	}
	result, err := h.GuidedService.Commit(session.ID)
	if result != nil {
		h.publishGuidedApprovals(r, session.ID, result.Approved)
	}
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, result.Session)
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
	abandoned, err := h.GuidedService.Abandon(session.ID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSONBare(w, abandoned)
}
