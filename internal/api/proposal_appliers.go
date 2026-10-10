package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/traceability"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// ProposalApplierDeps are the services the proposal appliers write through,
// the ones the HTTP handlers write through. Bus may be nil, and then no
// event is published; ProjectService may be nil, and then an event carries
// no workspace.
type ProposalApplierDeps struct {
	ArtifactService artifacts.Service
	LinkService     links.Service
	ChatterService  chatter.Service
	ProjectService  projects.Service
	VVService       vv.Service
	Bus             events.Bus
}

// NewProposalAppliers returns the callbacks the proposal service invokes
// when a human approves a pending agent write. They run the same domain
// writes the HTTP handlers do — including the domain events and the
// link-snapshot auto-versioning for link ops — so an agent-applied write is
// indistinguishable downstream (activity log, SSE refresh, automations) from
// a direct human write. They need no request and no Handler: the
// composition root builds them from the services in stage agents and hands
// them to proposals.DefaultService.SetAppliers in stage handlers, after
// NewHandler, where it always has (refactor plan X11c).
func NewProposalAppliers(deps ProposalApplierDeps) proposals.Appliers {
	ap := &appliers{ProposalApplierDeps: deps}
	return proposals.Appliers{
		CreateArtifact:   ap.createArtifact,
		UpdateArtifact:   ap.updateArtifact,
		DeleteArtifact:   ap.deleteArtifact,
		CreateLink:       ap.createLink,
		DeleteLink:       ap.deleteLink,
		RecordTestResult: ap.recordTestResult,
	}
}

// ProposalAppliers returns the proposal appliers over the handler's own
// services, as NewProposalAppliers builds them from the same services.
func (h *Handler) ProposalAppliers() proposals.Appliers {
	return NewProposalAppliers(ProposalApplierDeps{
		ArtifactService: h.ArtifactService,
		LinkService:     h.LinkService,
		ChatterService:  h.ChatterService,
		ProjectService:  h.ProjectService,
		VVService:       h.VVService,
		Bus:             h.Bus,
	})
}

// Each applier over the handler's services, by the name the unit tests call.

func (h *Handler) applyCreateArtifact(payload map[string]interface{}) (string, error) {
	return h.ProposalAppliers().CreateArtifact(payload)
}

func (h *Handler) applyUpdateArtifact(targetID string, payload map[string]interface{}) (string, error) {
	return h.ProposalAppliers().UpdateArtifact(targetID, payload)
}

func (h *Handler) applyDeleteArtifact(targetID string) error {
	return h.ProposalAppliers().DeleteArtifact(targetID)
}

func (h *Handler) applyCreateLink(payload map[string]interface{}) (string, error) {
	return h.ProposalAppliers().CreateLink(payload)
}

func (h *Handler) applyDeleteLink(targetID string) error {
	return h.ProposalAppliers().DeleteLink(targetID)
}

// appliers applies approved proposals through its services.
type appliers struct {
	ProposalApplierDeps
}

// publishApplied emits a system-actor domain event for an approved-proposal
// write. Unlike publish it takes no *http.Request: a proposal is applied out
// of band from the review request, so the actor is the platform itself
// rather than the reviewing user or the originating agent run.
func (ap *appliers) publishApplied(eventType, projectID, entityID string, payload map[string]interface{}) {
	ap.publishAppliedAs(eventType, projectID, entityID, events.ActorSystem, payload)
}

// publishAppliedAs is publishApplied with the actor the caller names: the
// link write service's (trace), whose Policy names the system.
func (ap *appliers) publishAppliedAs(eventType, projectID, entityID, actor string, payload map[string]interface{}) {
	if ap.Bus == nil {
		return
	}
	orgID := ""
	if projectID != "" && ap.ProjectService != nil {
		if project, err := ap.ProjectService.GetProject(projectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	ap.Bus.Publish(events.New(eventType, projectID, entityID, actor, payload).WithOrg(orgID))
}

// projectIDForArtifact resolves an artifact id to its project id ("" on
// failure), as Handler.projectIDForArtifact does.
func (ap *appliers) projectIDForArtifact(artifactID string) string {
	artifact, err := ap.ArtifactService.GetArtifact(artifactID)
	if err != nil || artifact == nil {
		return ""
	}
	return artifact.ProjectID
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

func (ap *appliers) createArtifact(payload map[string]interface{}) (string, error) {
	var req artifacts.CreateArtifactRequest
	if err := decodeProposalPayload(payload, &req); err != nil {
		return "", err
	}
	artifact := artifacts.NewArtifact(req)
	if err := ap.ArtifactService.CreateArtifact(artifact); err != nil {
		return "", err
	}
	ap.publishApplied(events.ArtifactCreated, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type": artifact.Type,
		"title":         artifact.Title,
		"version":       artifact.Version,
	})
	return artifact.ID, nil
}

func (ap *appliers) updateArtifact(targetID string, payload map[string]interface{}) (string, error) {
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
	updated, err := ap.ArtifactService.UpdateArtifact(targetID, req)
	if err != nil {
		return "", err
	}
	ap.publishApplied(events.ArtifactUpdated, updated.ProjectID, updated.ID, map[string]interface{}{
		"artifact_type": updated.Type,
		"title":         updated.Title,
		"version":       updated.Version,
	})
	return updated.ID, nil
}

func (ap *appliers) deleteArtifact(targetID string) error {
	// Capture identity before the row is gone so the event can carry it.
	projectID, artifactType, title := "", "", ""
	if a, err := ap.ArtifactService.GetArtifact(targetID); err == nil && a != nil {
		projectID, artifactType, title = a.ProjectID, a.Type, a.Title
	}
	if err := ap.ArtifactService.DeleteArtifact(targetID); err != nil {
		return err
	}
	ap.publishApplied(events.ArtifactDeleted, projectID, targetID, map[string]interface{}{
		"artifact_type": artifactType,
		"title":         title,
	})
	return nil
}

func (ap *appliers) createLink(payload map[string]interface{}) (string, error) {
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
	// link is created. The applier's Policy asks no role and no feature.
	fromArtifact, err := ap.ArtifactService.GetArtifact(req.FromID)
	if err != nil {
		return "", fmt.Errorf("cannot apply create_link: source artifact %q not found: %w", req.FromID, err)
	}
	toArtifact, err := ap.ArtifactService.GetArtifact(req.ToID)
	if err != nil {
		return "", fmt.Errorf("cannot apply create_link: target artifact %q not found: %w", req.ToID, err)
	}
	trace, p := ap.trace(), appliedLinkPolicy
	from, to := traceability.EndOf(fromArtifact), traceability.EndOf(toArtifact)
	if err := trace.CheckLink(p, from.ProjectID, from, to, req.Type); err != nil {
		return "", fmt.Errorf("cannot apply create_link: %w", err)
	}
	// Refresh both endpoints' link snapshots, exactly as the CreateLink
	// handler does, so endpoint versions stay in step with the new edge.
	link, err := trace.CreateLink(req)
	if err != nil {
		return "", err
	}
	trace.PublishLinkEvent(p, events.LinkCreated, ap.projectIDForArtifact(link.FromID), link)
	return link.ID, nil
}

func (ap *appliers) deleteLink(targetID string) error {
	link, _ := ap.LinkService.GetLink(targetID)
	// Refresh both endpoints' link snapshots, exactly as the DeleteLink
	// handler does.
	trace := ap.trace()
	if err := trace.DeleteLink(targetID, link); err != nil {
		return err
	}
	if link != nil {
		trace.PublishLinkEvent(appliedLinkPolicy, events.LinkDeleted, ap.projectIDForArtifact(link.FromID), link)
	}
	return nil
}

func (ap *appliers) recordTestResult(payload map[string]interface{}) (string, error) {
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
	result, err := ap.VVService.UpsertResult(runID, req, nil, "system", "")
	if err != nil {
		return "", err
	}
	return result.ID, nil
}
