package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// StartProjectReview handles POST /api/v1/projects/{id}/review-round
// (REQ-165): one run of the project's review process, instead of walking the
// tree and submitting each artifact by hand.
//
// The round is re-runnable and carries no stored state (see
// artifacts/review_round.go). What it does on a second run is the point of the
// feature: an approved artifact nobody touched stays approved, while one whose
// content was edited is already back in draft — UpdateArtifact demotes it —
// and so gets pulled into review again.
//
// Authorization matches the single-artifact status change it is a bulk form
// of: project editor or better, and proposal-mode agent runs are refused
// outright, because the proposal vocabulary has no status op and letting a
// review-gated agent put a project into review would defeat the gate.
func (h *Handler) StartProjectReview(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if !h.projectFeatureEnabled(r, projectID, release.FeatureProjectReviewRound) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	if run := CurrentRun(r); run != nil && h.agentService != nil {
		if agent, err := h.agentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot start a project review")
			return
		}
	}

	// An absent body is the ordinary call — "review everything" — so only a
	// body that is present and malformed is an error.
	var req struct {
		Types []string `json:"types"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	result, err := h.artifactService.StartProjectReview(projectID, artifacts.ReviewRoundRequest{Types: req.Types})
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrInvalidType):
			respondError(w, r, http.StatusBadRequest, err.Error(), nil)
		default:
			respondInternal(w, r, "failed to start the project review", err)
		}
		return
	}

	// Sign-off history lives in the event stream (issue #127), and a round is
	// nothing but a batch of the same transition: each moved artifact gets the
	// event and the feed note it would have got one at a time, so an
	// artifact's own history never has to be read against a separate log of
	// rounds to explain how it got into review.
	for _, a := range result.Moved {
		h.publish(r, events.ArtifactStatusChanged, a.ProjectID, a.ID, map[string]interface{}{
			"artifact_type": a.Type,
			"title":         a.Title,
			"from":          artifacts.StatusDraft,
			"to":            a.Status,
			"version":       a.Version,
			"review_round":  true,
		})
		entry := chatter.NewChatterEntry(a.ID, fmt.Sprintf("Status changed: %s → %s (project review)", artifacts.StatusDraft, a.Status), true, "status-change")
		if err := h.chatterService.CreateEntry(entry); err != nil {
			slog.Warn("api: failed to create chatter entry for review round", "artifact_id", a.ID, "error", err)
		}
	}

	// One project-level event for the round itself, so the activity feed reads
	// "a review round was started" rather than only N status changes.
	h.publish(r, events.ReviewRoundStarted, projectID, projectID, map[string]interface{}{
		"moved":             len(result.Moved),
		"already_in_review": result.AlreadyInReview,
		"approved":          result.Approved,
		"types":             result.Types,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
