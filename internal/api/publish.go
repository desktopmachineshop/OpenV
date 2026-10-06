package api

import (
	"log/slog"
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
)

// publish emits a domain event when a bus is wired (nil-safe), stamped with
// the owning org (the project's org when project-scoped, else the caller's
// active workspace).
func (h *Handler) publish(r *http.Request, eventType, projectID, entityID string, payload map[string]interface{}) {
	h.publishAs(r, Actor(r), eventType, projectID, entityID, payload)
}

// publishAs is publish stamped with actor rather than Actor(r): the link
// write service publishes as its Policy's Actor (traceabilityFor).
func (h *Handler) publishAs(r *http.Request, actor, eventType, projectID, entityID string, payload map[string]interface{}) {
	if h.Bus == nil {
		return
	}
	orgID := ""
	if projectID != "" && h.ProjectService != nil {
		if project, err := h.ProjectService.GetProject(projectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	if orgID == "" {
		orgID = ActiveOrg(r)
	}
	h.Bus.Publish(events.New(eventType, projectID, entityID, actor, payload).WithOrg(orgID))
}

// publishOrgEvent publishes a workspace-level event, where the tenant comes
// from the request path rather than from a project. Membership events have no
// project to infer it from, and the acting user's ACTIVE workspace is not
// necessarily the one being edited, so the org id is passed explicitly.
func (h *Handler) publishOrgEvent(r *http.Request, eventType, orgID, entityID string, payload map[string]interface{}) {
	h.publishOrgEventAs(Actor(r), eventType, orgID, entityID, payload)
}

// publishOrgEventAs is publishOrgEvent for the paths that have no request to
// read an actor from — an invitation taken up during an OIDC sign-in, for
// instance. The actor matters here beyond the audit trail: subscribers skip
// notifying the actor about their own action, so naming the right one is what
// stops somebody being told what they just did.
func (h *Handler) publishOrgEventAs(actor, eventType, orgID, entityID string, payload map[string]interface{}) {
	if h.Bus == nil || orgID == "" {
		return
	}
	h.Bus.Publish(events.New(eventType, "", entityID, actor, payload).WithOrg(orgID))
}

// logAutoNote writes a system note of the given type to an artifact's feed,
// attributed to the caller when there is one.
func (h *Handler) logAutoNote(r *http.Request, artifactID, message, entryType string) {
	entry := chatter.NewChatterEntry(artifactID, message, true, entryType)
	entry.AuthorName = "System"
	if user := CurrentUser(r); user != nil {
		entry.CreatedBy = &user.ID
		if user.Name != "" {
			entry.AuthorName = user.Name
		} else {
			entry.AuthorName = user.Email
		}
	}
	if err := h.ChatterService.CreateEntry(entry); err != nil {
		slog.Warn("api: failed to log note", "type", entryType, "artifact_id", artifactID, "error", err)
	}
}
