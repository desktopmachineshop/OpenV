package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/proposals"
)

// registerArtifactRoutes wires an artifact's create, list, read, update,
// status change and delete.
func (h *Handler) registerArtifactRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/artifacts", h.CreateArtifact).Methods("POST")
	router.HandleFunc("/api/v1/artifacts", h.ListArtifacts).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.GetArtifact).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.UpdateArtifact).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}/status", h.ChangeArtifactStatus).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}", h.DeleteArtifact).Methods("DELETE")
}

// CreateArtifact creates a new artifact
func (h *Handler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	var req artifacts.CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}

	// Interviewer-created artifacts always land as interview-tagged drafts,
	// regardless of what attributes the model supplied.
	if run := CurrentRun(r); run != nil && run.InterviewSessionID != nil {
		if req.Attributes == nil {
			req.Attributes = map[string]interface{}{}
		}
		req.Attributes["status"] = "draft"
		req.Attributes["origin"] = "interview"
		req.Attributes["interview_session_id"] = *run.InterviewSessionID
	}

	// Likewise for guided-copilot turn runs: anything they create is a
	// guided-flow-tagged draft.
	if run := CurrentRun(r); run != nil && run.GuidedSessionID != nil {
		if req.Attributes == nil {
			req.Attributes = map[string]interface{}{}
		}
		req.Attributes["status"] = "draft"
		req.Attributes["origin"] = "guided-flow"
		req.Attributes["guided_session_id"] = *run.GuidedSessionID
	}

	// Validate typed attributes against the effective definitions for this
	// project + type (issue #219). Create enforces required attributes; an
	// artifact with no matching definitions is unaffected (the check is a
	// no-op when the org/project has defined none).
	if err := h.validateArtifactAttributes(req.ProjectID, req.Type, req.Attributes, true); err != nil {
		// A definition-lookup failure fails closed on create: sanitized 500,
		// real error already logged (issue #246). A genuine validation error is
		// the client's to fix: 400.
		if errors.Is(err, errAttributeDefinitionsUnavailable) {
			respondError(w, r, http.StatusInternalServerError, "could not validate artifact attributes; please retry", nil)
			return
		}
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if h.maybePropose(w, r, req.ProjectID, proposals.OpCreateArtifact, nil, req) {
		return
	}

	artifact := artifacts.NewArtifact(req)
	if err := h.ArtifactService.CreateArtifact(artifact); err != nil {
		respondInternal(w, r, "failed to create artifact", err)
		return
	}

	h.publish(r, events.ArtifactCreated, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type": artifact.Type,
		"title":         artifact.Title,
	})
	h.noteCopiedFrom(r, artifact, req.CopiedFrom)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(artifact)
}

// noteCopiedFrom opens a copied artifact's feed with the one line its
// history has: where it came from. A duplicate or a paste carries none of
// the original's versions and none of its links (a copy that inherited
// "verifies REQ-12" would assert a verification nobody made), so without
// this a reader could not tell the two apart later. The source must be
// readable by the caller, so the field cannot be used to probe another
// project's artifacts; anything else leaves the copy without a note rather
// than failing the create, which has already happened.
func (h *Handler) noteCopiedFrom(r *http.Request, copy *artifacts.Artifact, sourceID string) {
	if sourceID == "" || h.ChatterService == nil {
		return
	}
	source, err := h.ArtifactService.GetArtifact(sourceID)
	if err != nil || source == nil {
		return
	}
	if !h.requireProjectRole(discardResponse{}, r, source.ProjectID, members.RoleViewer) {
		return
	}
	label := source.Ref
	if label == "" {
		label = source.Title
	}
	h.logAutoNote(r, copy.ID, fmt.Sprintf("Copied from %s (version %d).", label, source.Version), "copy")
}

// errAttributeDefinitionsUnavailable marks a definition-lookup failure on the
// create path, where enforcement matters (issue #246). The create caller maps
// it to a sanitized 500 rather than letting a would-be-required attribute slip
// through; update fails open and never returns it.
var errAttributeDefinitionsUnavailable = errors.New("attribute definitions are temporarily unavailable")

// validateArtifactAttributes checks a submitted attributes map against the
// effective attribute definitions for the artifact's project + type (issue
// #219). It is a no-op when no attribute service is wired (e.g. unit tests) or
// when attrs is nil. enforceRequired is true on create and false on update
// (see the callers).
//
// Definition-lookup failures are handled by direction (issue #246):
//   - update (enforceRequired=false) FAILS OPEN — a transient catalog read must
//     not block a legitimate edit, and update does not enforce required-ness.
//   - create (enforceRequired=true) FAILS CLOSED — required-attribute
//     enforcement is the point on create, so a lookup error returns
//     errAttributeDefinitionsUnavailable (mapped to a sanitized 500) rather than
//     silently admitting an artifact that may be missing a required attribute.
func (h *Handler) validateArtifactAttributes(projectID, artifactType string, attrs map[string]interface{}, enforceRequired bool) error {
	if h.AttributeService == nil || attrs == nil {
		return nil
	}
	orgID := h.orgForProject(projectID)
	defs, err := h.AttributeService.EffectiveForProject(orgID, projectID)
	if err != nil {
		if enforceRequired {
			// Log the real error; the caller surfaces a sanitized message.
			slog.Error("api: failed to load attribute definitions; rejecting create (fail-closed)", "project_id", projectID, "error", err)
			return errAttributeDefinitionsUnavailable
		}
		slog.Warn("api: failed to load attribute definitions for validation; allowing update (fail-open)", "project_id", projectID, "error", err)
		return nil
	}
	return attributes.ValidateAttributes(defs, artifactType, attrs, enforceRequired)
}

// GetArtifact retrieves an artifact by ID
func (h *Handler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	artifact, err := h.ArtifactService.GetArtifact(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "artifact not found", err)
		return
	}

	if !h.requireProjectRoleFor(w, r, artifact.ProjectID, members.RoleViewer, missing("artifact not found")) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// Artifact listing pagination bounds. Artifacts render as a parent_id tree in
// the module view, so the UI still needs the complete set to build structure;
// the generous default keeps single-request behavior for typical projects
// while bounding worst-case response size. Clients page with limit/offset and
// the X-Total-Count header until they have everything.
const (
	defaultArtifactPageLimit = 1000
	maxArtifactPageLimit     = 1000
)

// ListArtifacts lists artifacts by project and optional type filter, one page
// at a time. Query params: project_id (required), type, owner (the "owner"
// attribute, exact match), limit (default and cap 1000), offset. The response body stays a plain JSON array for
// compatibility; the total number of matching artifacts rides on the
// X-Total-Count header so clients can page until exhaustion.
func (h *Handler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID := q.Get("project_id")
	artifactType := q.Get("type")
	owner := strings.TrimSpace(q.Get("owner"))

	if projectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > maxArtifactPageLimit {
		limit = defaultArtifactPageLimit
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	page, total, err := h.ArtifactService.ListArtifactsPage(projectID, artifactType, owner, limit, offset)
	if err != nil {
		respondInternal(w, r, "failed to list artifacts", err)
		return
	}
	if page == nil {
		page = []*artifacts.Artifact{}
	}

	// Document numbers ("1.2") are a property of the whole document, not of
	// this page: a page or a type filter is a slice through the tree, so the
	// numbering has to be computed from every artifact in the project and
	// then stamped onto the rows being served. Opt-in, because that is a
	// second query the tree view needs and a plain paginated read does not.
	if q.Get("doc_numbers") == "1" {
		if all, err := h.ArtifactService.ListArtifacts(projectID, ""); err == nil {
			artifacts.ApplySectionNumbers(all, page)
		} else {
			// Numbering is a display aid; losing it must not fail the read.
			slog.Warn("api: could not compute document numbers", "project_id", projectID, "error", err)
		}
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// UpdateArtifact updates an artifact
func (h *Handler) UpdateArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req artifacts.UpdateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch the old artifact BEFORE updating to track changes. An id no
	// artifact has, a malformed one among them, and one the caller cannot
	// reach at all answer alike, as GetArtifact answers them (I3).
	oldArtifact, err := h.ArtifactService.GetArtifact(id)
	if err != nil {
		respondArtifactLookup(w, r, err)
		return
	}

	if !h.requireProjectRoleFor(w, r, oldArtifact.ProjectID, members.RoleEditor, missing("artifact not found")) {
		return
	}

	// Validate typed attributes against the effective definitions (issue
	// #219). Only a PRESENT attributes map is checked, so the nil=no-change
	// contract is preserved: an update that omits attributes skips validation
	// entirely and carries the current attributes forward untouched. Required
	// attributes are NOT enforced on update (a partial or field-only edit must
	// not be rejected for an attribute it never intended to touch). The
	// effective type is the incoming type when the update changes it, else the
	// current one.
	if req.Attributes != nil {
		effectiveType := oldArtifact.Type
		if req.Type != nil {
			effectiveType = *req.Type
		}
		if err := h.validateArtifactAttributes(oldArtifact.ProjectID, effectiveType, req.Attributes, false); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if h.maybePropose(w, r, oldArtifact.ProjectID, proposals.OpUpdateArtifact, &id, req) {
		return
	}

	// Attributes contract (issue #125): a request that OMITS attributes
	// (nil after decode) means "leave them unchanged" — the domain layer
	// carries the current attributes forward. An explicit map — even an
	// empty {} — replaces them wholesale. Do NOT normalize nil to an empty
	// map here: that would turn every attribute-less update (e.g. the MCP
	// update_artifact tool) into a wipe.

	// Process link changes FIRST (add/remove from table)
	var affectedArtifactIDs []string
	var addedLinks, removedLinks []*links.Link
	if len(req.PendingLinkAdds) > 0 || len(req.PendingLinkRemoves) > 0 {
		// Convert string array to interface array for removal IDs
		removeInterfaceArray := make([]interface{}, len(req.PendingLinkRemoves))
		for i, v := range req.PendingLinkRemoves {
			removeInterfaceArray[i] = v
		}

		// The note lists the links the edit made and removed, read back
		// from what it did, never from what it was asked (#379 bug 196).
		changes, err := h.processManagedLinkChanges(r, oldArtifact.ProjectID, id, req.PendingLinkAdds, removeInterfaceArray)
		if err != nil {
			respondInternal(w, r, "failed to process link changes", err)
			return
		}
		affectedArtifactIDs, addedLinks, removedLinks = changes.affected, changes.added, changes.removed

		// After processing link changes, fetch current links and store in snapshot (deduplicated)
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)

		incomingLinks, err := h.LinkService.GetLinksTo(id)
		if err == nil {
			for _, link := range incomingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}

		outgoingLinks, err := h.LinkService.GetLinksFrom(id)
		if err == nil {
			for _, link := range outgoingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}

		if len(allLinks) > 0 {
			// Storing the snapshot needs a concrete attributes map. If the
			// request left attributes untouched (nil), seed a copy of the
			// current ones so the snapshot write doesn't clear the rest.
			if req.Attributes == nil {
				req.Attributes = make(map[string]interface{}, len(oldArtifact.Attributes)+1)
				for k, v := range oldArtifact.Attributes {
					req.Attributes[k] = v
				}
			}
			req.Attributes["links_snapshot"] = allLinks
		}
	}

	// Update the artifact ONCE with all changes including link snapshot
	// This single update will create ONE new version
	artifact, err := h.ArtifactService.UpdateArtifact(id, req)
	if err != nil {
		respondInternal(w, r, "failed to update artifact", err)
		return
	}

	// A content change marks every link of the artifact suspect, the ones
	// this edit has just made too; those were made against the new content,
	// so they are cleared again (#379 bug 197).
	if artifact.Type != oldArtifact.Type || artifact.Title != oldArtifact.Title || artifact.Body != oldArtifact.Body {
		for _, link := range addedLinks {
			if _, err := h.LinkService.ConfirmLink(link.ID); err != nil {
				slog.Warn("api: failed to clear the suspect flag of a link made with a content change", "link_id", link.ID, "error", err)
			}
		}
	}

	// Build a detailed change summary for chatter
	chatterMessage := h.buildChangesSummary(oldArtifact, artifact, addedLinks, removedLinks)
	chatterEntry := chatter.NewChatterEntry(id, chatterMessage, true, "version-change")
	if err := h.ChatterService.CreateEntry(chatterEntry); err != nil {
		// Log but don't fail the request
		slog.Warn("api: failed to create chatter entry for version change", "artifact_id", id, "error", err)
	}

	// Auto-version any artifacts that had link changes
	// These are OTHER artifacts affected by link changes, not the one we just updated
	if len(affectedArtifactIDs) > 0 {
		err = h.autoVersionLinkedArtifacts(affectedArtifactIDs)
		if err != nil {
			// Log but don't fail the request
			slog.Warn("api: failed to auto-version linked artifacts", "error", err)
		}
	}

	h.publish(r, events.ArtifactUpdated, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type": artifact.Type,
		"title":         artifact.Title,
		"version":       artifact.Version,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// ChangeArtifactStatus handles PUT /api/v1/artifacts/{id}/status: one review
// state-machine transition (draft <-> in_review -> approved -> superseded).
//
// Authorization: every transition — approval included — requires project
// editor or better. Owner-only approval would be tighter, but project
// membership has no granularity between editor and owner beyond the role
// ladder, so editor-approval is the documented choice (see
// artifacts.DefaultService.ChangeStatus). Proposal-mode agent runs are
// refused outright: the proposal vocabulary has no status op, and silently
// letting a review-gated agent flip review states would defeat the gate.
func (h *Handler) ChangeArtifactStatus(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	artifact, err := h.ArtifactService.GetArtifact(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "artifact not found", err)
		return
	}

	if !h.requireProjectRoleFor(w, r, artifact.ProjectID, members.RoleEditor, missing("artifact not found")) {
		return
	}
	if run := CurrentRun(r); run != nil && h.AgentService != nil {
		if agent, err := h.AgentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot change artifact status")
			return
		}
	}

	from := artifacts.NormalizeStatus(artifact.Status)
	updated, err := h.ArtifactService.ChangeStatus(id, req.Status)
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrInvalidStatus):
			respondError(w, r, http.StatusBadRequest, err.Error(), nil)
		case errors.Is(err, artifacts.ErrInvalidStatusTransition):
			respondError(w, r, http.StatusConflict, err.Error(), nil)
		case errors.Is(err, artifacts.ErrNotFound):
			respondError(w, r, http.StatusNotFound, "artifact not found", err)
		default:
			respondInternal(w, r, "failed to change artifact status", err)
		}
		return
	}

	// Sign-off history lives in the event stream (issue #127).
	h.publish(r, events.ArtifactStatusChanged, updated.ProjectID, updated.ID, map[string]interface{}{
		"artifact_type": updated.Type,
		"title":         updated.Title,
		"from":          from,
		"to":            updated.Status,
		"version":       updated.Version,
	})

	entry := chatter.NewChatterEntry(id, fmt.Sprintf("Status changed: %s → %s", from, updated.Status), true, "status-change")
	if err := h.ChatterService.CreateEntry(entry); err != nil {
		slog.Warn("api: failed to create chatter entry for status change", "artifact_id", id, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// DeleteArtifact deletes an artifact
func (h *Handler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	projectID := h.projectIDForArtifact(id)
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if h.maybePropose(w, r, projectID, proposals.OpDeleteArtifact, &id, nil) {
		return
	}

	err := h.ArtifactService.DeleteArtifact(id)
	if err != nil {
		respondInternal(w, r, "failed to delete artifact", err)
		return
	}

	h.publish(r, events.ArtifactDeleted, projectID, id, nil)

	w.WriteHeader(http.StatusNoContent)
}
