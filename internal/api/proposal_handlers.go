package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/proposals"
)

// registerProposalRoutes wires listing agent proposals and reviewing them,
// one at a time or in bulk.
func (h *Handler) registerProposalRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/proposals", h.ListProposals).Methods("GET")
	router.HandleFunc("/api/v1/proposals/bulk", h.BulkReviewProposals).Methods("POST")
	router.HandleFunc("/api/v1/proposals/{id}/approve", h.ApproveProposal).Methods("POST")
	router.HandleFunc("/api/v1/proposals/{id}/reject", h.RejectProposal).Methods("POST")
}

func (h *Handler) ListProposals(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	q := r.URL.Query()
	projectID := q.Get("project_id")
	// A workspace admin with no project filter sees the proposals of the
	// active workspace's projects: the query filters by workspace before its
	// limit, so other workspaces' proposals never crowd them out. Without an
	// active workspace there is nothing to filter by, so even a platform
	// admin must name a project.
	orgID := ""
	if projectID != "" {
		if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
			return
		}
	} else if orgID = ActiveOrg(r); orgID == "" || !h.isOrgAdmin(r, orgID) {
		// Non-admin members must scope the listing to a project they can
		// view: a parameter missing from the request, not a refusal.
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	list, err := h.ProposalService.List(orgID, projectID, q.Get("status"), q.Get("run_id"))
	if err != nil {
		respondInternal(w, r, "failed to list proposals", err)
		return
	}
	writeJSONBare(w, list)
}

func (h *Handler) reviewProposal(w http.ResponseWriter, r *http.Request, approve bool) {
	// A run token is the agent itself, not a person: it reviews no proposal,
	// its own run's least of all (REQ-21). The project guard below would let
	// it through as an editor of its project.
	if CurrentRun(r) != nil {
		writeJSONError(w, http.StatusForbidden, "agent runs cannot review proposals")
		return
	}
	id := mux.Vars(r)["id"]
	proposal, err := h.ProposalService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "proposal not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, proposal.ProjectID, members.RoleEditor, missing("proposal not found")) {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if approve {
		proposal, err = h.ProposalService.Approve(id, CurrentUserID(r), req.Note)
	} else {
		proposal, err = h.ProposalService.Reject(id, CurrentUserID(r), req.Note)
	}
	if err != nil {
		// Genuine proposal-domain validation errors are safe to surface; an
		// applier failure may carry internal details, so route it through the
		// sanitized 500 path per the #146 contract (the proposal is still
		// persisted as apply_failed with the real error in its review note).
		if isProposalValidationError(err) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
		} else {
			respondInternal(w, r, "failed to apply approved proposal", err)
		}
		return
	}
	writeJSONBare(w, proposal)
}

// isProposalValidationError reports whether err is a proposal-domain
// validation sentinel whose message is safe to show a client. Everything
// else from Approve/Reject is an applier failure to be sanitized (#146).
func isProposalValidationError(err error) bool {
	return errors.Is(err, proposals.ErrNotPending) ||
		errors.Is(err, proposals.ErrNotFound) ||
		errors.Is(err, proposals.ErrUnsupportedOp) ||
		errors.Is(err, proposals.ErrRunWriteCap)
}

// proposalReviewErrorMessage returns a client-safe message for a review
// error inside a bulk outcome: validation sentinels pass through, applier
// failures are logged with context and replaced with a stable message.
func proposalReviewErrorMessage(r *http.Request, id string, err error) string {
	if isProposalValidationError(err) {
		return err.Error()
	}
	slog.Error("failed to apply approved proposal",
		slog.String("proposal_id", id),
		slog.String("path", r.URL.Path),
		slog.Any("error", err))
	return "failed to apply approved proposal"
}

func (h *Handler) ApproveProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, true)
}

// bulkOutcome reports one proposal's fate inside a bulk review request.
type bulkOutcome struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// BulkReviewProposals approves or rejects up to MaxProposalsPerRun proposals
// in one request. Proposals are processed sequentially and each one goes
// through exactly the same ladder as a single review: load, project-editor
// authz, then the same Approve/Reject service methods (approvals apply real
// writes via the wired appliers). The response is 200 even on partial
// failure — the client renders the per-id outcomes.
func (h *Handler) BulkReviewProposals(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	var req struct {
		IDs    []string `json:"ids"`
		Action string   `json:"action"`
		Note   string   `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		writeJSONError(w, http.StatusBadRequest, `action must be "approve" or "reject"`)
		return
	}
	if len(req.IDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "ids is required")
		return
	}
	if len(req.IDs) > proposals.MaxProposalsPerRun {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("at most %d proposals per bulk request", proposals.MaxProposalsPerRun))
		return
	}

	// Approvals apply real writes, and a create_link proposal may reference a
	// sibling create_artifact proposal's temporary ref token (issue #235). That
	// ref only resolves once the artifact proposal has been applied, so order
	// the approvals to run create_artifact proposals before create_link ones,
	// regardless of the order the client listed them. Rejections apply nothing,
	// so their order is left untouched.
	if req.Action == "approve" {
		h.orderProposalsForApply(req.IDs)
	}

	reviewer := CurrentUserID(r)
	results := make([]bulkOutcome, 0, len(req.IDs))
	for _, id := range req.IDs {
		proposal, err := h.ProposalService.Get(id)
		if err != nil {
			results = append(results, bulkOutcome{ID: id, Error: "proposal not found"})
			continue
		}
		if !h.hasProjectRole(r, proposal.ProjectID, members.RoleEditor) {
			// A proposal in a project the caller cannot reach at all is
			// reported as one no row has (I3).
			msg := "you do not have access to this project"
			if !h.reachesProject(r, proposal.ProjectID) {
				msg = "proposal not found"
			}
			results = append(results, bulkOutcome{ID: id, Error: msg})
			continue
		}
		if req.Action == "approve" {
			_, err = h.ProposalService.Approve(id, reviewer, req.Note)
		} else {
			_, err = h.ProposalService.Reject(id, reviewer, req.Note)
		}
		if err != nil {
			results = append(results, bulkOutcome{ID: id, Error: proposalReviewErrorMessage(r, id, err)})
			continue
		}
		results = append(results, bulkOutcome{ID: id, OK: true})
	}
	writeJSONBare(w, map[string]interface{}{"results": results})
}

func (h *Handler) RejectProposal(w http.ResponseWriter, r *http.Request) {
	h.reviewProposal(w, r, false)
}

// orderProposalsForApply stably reorders proposal ids in place so create_artifact
// proposals are approved before create_link proposals in one bulk request. A
// link may name a sibling artifact proposal's temporary ref (issue #235), which
// only resolves after that artifact proposal is applied. Ids that fail to load
// keep a neutral middle priority and their relative position; the review loop
// then reports them as not-found.
func (h *Handler) orderProposalsForApply(ids []string) {
	prio := make(map[string]int, len(ids))
	for _, id := range ids {
		prio[id] = 1
		if p, err := h.ProposalService.Get(id); err == nil && p != nil {
			switch p.Op {
			case proposals.OpCreateArtifact:
				prio[id] = 0
			case proposals.OpCreateLink:
				prio[id] = 2
			}
		}
	}
	sort.SliceStable(ids, func(i, j int) bool { return prio[ids[i]] < prio[ids[j]] })
}
