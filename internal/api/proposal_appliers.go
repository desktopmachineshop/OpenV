package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// ProposalAppliers returns the callbacks the proposal service invokes when a
// human approves a pending agent write. They run the same domain writes the
// HTTP handlers do — including the domain events and the link-snapshot
// auto-versioning for link ops — so an agent-applied write is
// indistinguishable downstream (activity log, SSE refresh, automations) from
// a direct human write. Wired from the composition root once the handler
// exists (see proposals.DefaultService.SetAppliers).
func (h *Handler) ProposalAppliers() proposals.Appliers {
	return proposals.Appliers{
		CreateArtifact:   h.applyCreateArtifact,
		UpdateArtifact:   h.applyUpdateArtifact,
		DeleteArtifact:   h.applyDeleteArtifact,
		CreateLink:       h.applyCreateLink,
		DeleteLink:       h.applyDeleteLink,
		RecordTestResult: h.applyRecordTestResult,
	}
}

// publishApplied emits a system-actor domain event for an approved-proposal
// write. Unlike publish it takes no *http.Request: a proposal is applied out
// of band from the review request, so the actor is the platform itself
// rather than the reviewing user or the originating agent run.
func (h *Handler) publishApplied(eventType, projectID, entityID string, payload map[string]interface{}) {
	if h.bus == nil {
		return
	}
	orgID := ""
	if projectID != "" && h.projectService != nil {
		if project, err := h.projectService.GetProject(projectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	h.bus.Publish(events.New(eventType, projectID, entityID, events.ActorSystem, payload).WithOrg(orgID))
}

// decodeProposalPayload re-hydrates a stored proposal payload into a typed
// request struct via a JSON round-trip.
func decodeProposalPayload(payload map[string]interface{}, out interface{}) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (h *Handler) applyCreateArtifact(payload map[string]interface{}) (string, error) {
	var req artifacts.CreateArtifactRequest
	if err := decodeProposalPayload(payload, &req); err != nil {
		return "", err
	}
	artifact := artifacts.NewArtifact(req)
	if err := h.artifactService.CreateArtifact(artifact); err != nil {
		return "", err
	}
	h.publishApplied(events.ArtifactCreated, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type": artifact.Type,
		"title":         artifact.Title,
		"version":       artifact.Version,
	})
	return artifact.ID, nil
}

func (h *Handler) applyUpdateArtifact(targetID string, payload map[string]interface{}) (string, error) {
	var req artifacts.UpdateArtifactRequest
	if err := decodeProposalPayload(payload, &req); err != nil {
		return "", err
	}
	// The apply path has no request context, so it cannot run the
	// cross-project authorization processManagedLinkChanges performs for
	// managed link edits (the HTTP handler at PUT /artifacts/{id} does).
	// Rather than silently drop those edits — the #176 integrity bug, where
	// only the HTTP handler ever processed pendingLinkAdds/Removes — reject
	// the proposal so nothing is lost quietly. The link changes must be
	// re-proposed as explicit create_link/delete_link operations, which have
	// their own appliers.
	if len(req.PendingLinkAdds) > 0 || len(req.PendingLinkRemoves) > 0 {
		return "", errors.New("proposal carries managed link edits (pendingLinkAdds/pendingLinkRemoves) that cannot be applied here; propose the link changes as separate create_link/delete_link operations")
	}
	updated, err := h.artifactService.UpdateArtifact(targetID, req)
	if err != nil {
		return "", err
	}
	h.publishApplied(events.ArtifactUpdated, updated.ProjectID, updated.ID, map[string]interface{}{
		"artifact_type": updated.Type,
		"title":         updated.Title,
		"version":       updated.Version,
	})
	return updated.ID, nil
}

func (h *Handler) applyDeleteArtifact(targetID string) error {
	// Capture identity before the row is gone so the event can carry it.
	projectID, artifactType, title := "", "", ""
	if a, err := h.artifactService.GetArtifact(targetID); err == nil && a != nil {
		projectID, artifactType, title = a.ProjectID, a.Type, a.Title
	}
	if err := h.artifactService.DeleteArtifact(targetID); err != nil {
		return err
	}
	h.publishApplied(events.ArtifactDeleted, projectID, targetID, map[string]interface{}{
		"artifact_type": artifactType,
		"title":         title,
	})
	return nil
}

func (h *Handler) applyCreateLink(payload map[string]interface{}) (string, error) {
	var req links.CreateLinkRequest
	if err := decodeProposalPayload(payload, &req); err != nil {
		return "", err
	}
	// The payload reaching here has already had any pending-proposal refs
	// resolved to real artifact ids (see proposals.DefaultService.resolveLinkPayload),
	// so both endpoints now exist and their types are knowable. Re-run the same
	// link-type validation the HTTP CreateLink handler applies at propose time
	// for non-ref links (issue #250): with a pending-ref endpoint the propose-time
	// check was skipped, so this apply-time check is the only guard before an
	// invalid edge is written. Fetch the real endpoint types and validate the
	// link type exists and its from/to constraints hold; on failure return an
	// error so the proposal resolves to apply_failed with a clear message and no
	// link is created.
	fromArtifact, err := h.artifactService.GetArtifact(req.FromID)
	if err != nil {
		return "", fmt.Errorf("cannot apply create_link: source artifact %q not found: %w", req.FromID, err)
	}
	toArtifact, err := h.artifactService.GetArtifact(req.ToID)
	if err != nil {
		return "", fmt.Errorf("cannot apply create_link: target artifact %q not found: %w", req.ToID, err)
	}
	if err := links.ValidateLinkType(req.Type, fromArtifact.Type, toArtifact.Type); err != nil {
		return "", fmt.Errorf("cannot apply create_link: %w", err)
	}
	link := links.NewLink(req)
	if err := h.linkService.CreateLink(link); err != nil {
		return "", err
	}
	// Refresh both endpoints' link snapshots, exactly as the CreateLink
	// handler does, so endpoint versions stay in step with the new edge.
	_ = h.autoVersionLinkedArtifacts([]string{link.FromID, link.ToID})
	h.publishApplied(events.LinkCreated, h.projectIDForArtifact(link.FromID), link.ID, map[string]interface{}{
		"link_type": link.Type,
		"from_id":   link.FromID,
		"to_id":     link.ToID,
	})
	return link.ID, nil
}

func (h *Handler) applyDeleteLink(targetID string) error {
	link, _ := h.linkService.GetLink(targetID)
	if err := h.linkService.DeleteLink(targetID); err != nil {
		return err
	}
	if link != nil {
		// Refresh both endpoints' link snapshots, exactly as the DeleteLink
		// handler does.
		_ = h.autoVersionLinkedArtifacts([]string{link.FromID, link.ToID})
		h.publishApplied(events.LinkDeleted, h.projectIDForArtifact(link.FromID), link.ID, map[string]interface{}{
			"link_type": link.Type,
			"from_id":   link.FromID,
			"to_id":     link.ToID,
		})
	}
	return nil
}

func (h *Handler) applyRecordTestResult(payload map[string]interface{}) (string, error) {
	runID, _ := payload["run_id"].(string)
	if runID == "" {
		return "", errors.New("record_test_result payload requires run_id")
	}
	var req vv.UpsertResultRequest
	if err := decodeProposalPayload(payload, &req); err != nil {
		return "", err
	}
	// Applying an approved proposal: a human signed off on this result, so it
	// is not stamped as agent-executed. vvService already publishes
	// TestRunRecorded through its own bus, so no publishApplied is needed here.
	result, err := h.vvService.UpsertResult(runID, req, nil, "system", "")
	if err != nil {
		return "", err
	}
	return result.ID, nil
}
