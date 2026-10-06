package api

import (
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/traceability"
)

// processManagedLinkChanges makes the managed link edits of an update of
// the artifact fromArtifactID (pendingLinkAdds and pendingLinkRemoves of PUT
// /api/v1/artifacts/{id}), under managedEditPolicy, through the link write
// service (traceability.Service.ApplyManagedLinkEdits). baseProjectID is the
// project of the artifact being updated; the caller has already verified
// editor rights on it.
func (h *Handler) processManagedLinkChanges(r *http.Request, baseProjectID, fromArtifactID string, toAdd, toRemove []interface{}) (*traceability.ManagedLinkChanges, error) {
	return h.traceabilityFor(r).ApplyManagedLinkEdits(managedEditPolicy(r), baseProjectID, fromArtifactID, toAdd, toRemove)
}
