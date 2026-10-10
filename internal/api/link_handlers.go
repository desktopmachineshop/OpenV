package api

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/traceability"
)

// registerLinkRoutes wires links: create, list, read, update, confirm and
// delete.
func (h *Handler) registerLinkRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/links", h.CreateLink).Methods("POST")
	router.HandleFunc("/api/v1/links", h.ListLinks).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.GetLink).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.UpdateLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}/confirm", h.ConfirmLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}", h.DeleteLink).Methods("DELETE")
}

// sourceArtifactNotFound and targetArtifactNotFound answer a link's endpoint
// that no row has, or that lies in a project the caller cannot reach at all.
var (
	sourceArtifactNotFound = notFound{http.StatusBadRequest, "source artifact not found"}
	targetArtifactNotFound = notFound{http.StatusBadRequest, "target artifact not found"}
)

// CreateLink creates a new link
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	var req links.CreateLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// A proposal-mode run may name a sibling create_artifact proposal's
	// temporary ref token in from_id/to_id — an artifact that does not exist
	// yet (issue #235). For such an endpoint the artifact fetch, link-type
	// validation, and its project's authz are deferred to apply time (the ref
	// resolves to the real id then). Endpoints that are real ids are validated
	// here exactly as before; a non-proposal run has no refs, so its behaviour
	// is unchanged.
	proposalRunID, isProposalRun := h.proposalRunID(r)

	// An endpoint in a project the caller cannot reach at all answers as one
	// no row has, where the lookup would, before anything of it shows (I3).
	fromArtifact, fromErr := h.ArtifactService.GetArtifact(req.FromID)
	fromIsRef := fromErr != nil && isProposalRun && h.pendingArtifactRef(proposalRunID, req.FromID) != nil
	if fromErr != nil && !fromIsRef {
		respondError(w, r, http.StatusBadRequest, "source artifact not found", fromErr)
		return
	}
	if fromArtifact != nil && !h.requireProjectVisible(w, r, fromArtifact.ProjectID, sourceArtifactNotFound) {
		return
	}

	toArtifact, toErr := h.ArtifactService.GetArtifact(req.ToID)
	toIsRef := toErr != nil && isProposalRun && h.pendingArtifactRef(proposalRunID, req.ToID) != nil
	if toErr != nil && !toIsRef {
		respondError(w, r, http.StatusBadRequest, "target artifact not found", toErr)
		return
	}
	if toArtifact != nil && !h.requireProjectVisible(w, r, toArtifact.ProjectID, targetArtifactNotFound) {
		return
	}

	// Validate link type only when both endpoint types are known. When an
	// endpoint is a pending ref its type is not yet knowable; the human review
	// of the paired proposals is the check in that case.
	trace, p := h.traceabilityFor(r), linkCreatePolicy(r)
	from, to := traceability.EndOf(fromArtifact), traceability.EndOf(toArtifact)
	if err := traceability.CheckRules(req.Type, from, to); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Determine the project to authorize and to file the proposal under. Prefer
	// a known real endpoint; fall back to the referenced sibling proposal's
	// project when both endpoints are refs.
	projectID := ""
	switch {
	case fromArtifact != nil:
		projectID = fromArtifact.ProjectID
	case toArtifact != nil:
		projectID = toArtifact.ProjectID
	default:
		if ref := h.pendingArtifactRef(proposalRunID, req.FromID); ref != nil {
			projectID = ref.ProjectID
		} else if ref := h.pendingArtifactRef(proposalRunID, req.ToID); ref != nil {
			projectID = ref.ProjectID
		}
	}
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	// A link also writes to the target artifact (version bump + chatter via
	// autoVersionLinkedArtifacts) and exposes its title, so a cross-project
	// link needs editor rights on the target's project too. Only checkable for
	// a known (non-ref) endpoint.
	//
	// The one exception is the flow-down link (REQ-145): a supplier working
	// in a child project refines requirements it can only read, so "refines"
	// crosses into the target's project with viewer rights there, while the
	// workspace has the feature (the policy's gate). What it writes on the
	// parent side is the link snapshot, which is the point.
	if err := trace.CheckFlowDown(p, projectID, req.Type); err != nil {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	targetRole := members.RoleEditor
	if trace.FlowDownViewer(p, projectID, req.Type) {
		targetRole = members.RoleViewer
	}
	if toArtifact != nil && toArtifact.ProjectID != projectID && !h.requireProjectRole(w, r, toArtifact.ProjectID, targetRole) {
		return
	}
	if fromArtifact != nil && fromArtifact.ProjectID != projectID && !h.requireProjectRole(w, r, fromArtifact.ProjectID, members.RoleEditor) {
		return
	}
	if h.maybePropose(w, r, projectID, proposals.OpCreateLink, nil, req) {
		return
	}

	// Make the link and refresh link snapshots for both artifacts it touches
	link, err := trace.CreateLink(req)
	if err != nil {
		respondInternal(w, r, "failed to create link", err)
		return
	}
	trace.PublishLinkEvent(p, events.LinkCreated, fromArtifact.ProjectID, link)

	writeJSON(w, http.StatusCreated, link)
}

// GetLink retrieves a link by ID
func (h *Handler) GetLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	link, err := h.LinkService.GetLink(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "link not found", err)
		return
	}

	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(link.FromID), members.RoleViewer, missing("link not found")) {
		return
	}

	writeJSONOK(w, link)
}

// ListLinks lists links by project
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	links, err := h.LinkService.GetAllLinks(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list links", err)
		return
	}

	writeJSONOK(w, links)
}

// UpdateLink updates a link
func (h *Handler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req links.UpdateLinkRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	existing, err := h.LinkService.GetLink(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "link not found", err)
		return
	}
	projectID := h.projectIDForArtifact(existing.FromID)
	if !h.requireProjectRoleFor(w, r, projectID, members.RoleEditor, missing("link not found")) {
		return
	}

	link, err := h.LinkService.UpdateLink(id, req)
	if err != nil {
		respondInternal(w, r, "failed to update link", err)
		return
	}

	// Refresh link snapshots for both artifacts touched by this link
	h.traceabilityFor(r).RefreshLinkSnapshots(link.FromID, link.ToID)

	// The one path that changes a link's type or attributes, so the one
	// publisher of link.updated (#379 bug 132), shaped as link.created is,
	// with the type the link has now.
	h.publish(r, events.LinkUpdated, projectID, link.ID, map[string]interface{}{
		"link_type": link.Type,
		"from_id":   link.FromID,
		"to_id":     link.ToID,
	})

	writeJSONOK(w, link)
}

// ConfirmLink handles PUT /api/v1/links/{id}/confirm: an editor vouches
// that a suspect link still holds after the linked artifact's content
// changed, clearing the suspect flag (issue #131). Authorization mirrors
// UpdateLink: editor on the source artifact's project. Confirming an
// already-trusted link is a harmless no-op, so the endpoint is idempotent.
//
// Proposal-mode agent runs are refused outright (issue #176). Clearing the
// suspect flag is a human review action — it asserts that a link still holds
// after content changed — and the proposal vocabulary has no op for it.
// Letting a review-gated agent clear suspect directly would defeat the very
// review the flag exists to trigger. Refusal (not diversion to a proposal)
// mirrors ChangeArtifactStatus: both are human sign-off gates with no
// proposal representation.
func (h *Handler) ConfirmLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	existing, err := h.LinkService.GetLink(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "link not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, h.projectIDForArtifact(existing.FromID), members.RoleEditor, missing("link not found")) {
		return
	}
	if run := CurrentRun(r); run != nil && h.AgentService != nil {
		if agent, err := h.AgentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot clear a suspect link")
			return
		}
	}

	link, err := h.LinkService.ConfirmLink(id)
	if err != nil {
		respondInternal(w, r, "failed to confirm link", err)
		return
	}

	writeJSONOK(w, link)
}

// DeleteLink deletes a link. The delete auto-versions both ends, so it needs
// what creating the link needs (#379 bug 194): editor rights on the source's
// project, and on the target's when it is another project, except that a
// flow-down link (refines) takes viewer rights there while the flow-down
// feature is on, so a supplier can remove the link it was allowed to make.
// No feature gate refuses a delete: with the feature off, a refines link
// wants editor rights on the target like any other. A target no artifact
// has any more has no project to ask about. A link no row has answers as
// the source's guard answers a project it cannot resolve.
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	link, _ := h.LinkService.GetLink(id)

	projectID, targetProjectID := "", ""
	if link != nil {
		projectID = h.projectIDForArtifact(link.FromID)
		targetProjectID = h.projectIDForArtifact(link.ToID)
	}
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	// Past the guard the link is one a row has: with none, projectID is ""
	// and the guard refused.
	trace, p := h.traceabilityFor(r), linkDeletePolicy(r)
	if err := trace.CheckFlowDown(p, projectID, link.Type); err != nil {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	if targetProjectID != "" && targetProjectID != projectID {
		targetRole := members.RoleEditor
		if trace.FlowDownViewer(p, projectID, link.Type) {
			targetRole = members.RoleViewer
		}
		if !h.requireProjectRole(w, r, targetProjectID, targetRole) {
			return
		}
	}
	if h.maybePropose(w, r, projectID, proposals.OpDeleteLink, &id, nil) {
		return
	}

	// Delete the link and refresh link snapshots for both artifacts it touches
	if err := trace.DeleteLink(id, link); err != nil {
		respondInternal(w, r, "failed to delete link", err)
		return
	}
	if link != nil {
		trace.PublishLinkEvent(p, events.LinkDeleted, projectID, link)
	}

	w.WriteHeader(http.StatusNoContent)
}
