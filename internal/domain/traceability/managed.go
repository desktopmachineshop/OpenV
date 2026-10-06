// The managed link edits of an artifact update: pendingLinkAdds and
// pendingLinkRemoves in PUT /api/v1/artifacts/{id} (quirk Q3).

package traceability

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/openv/requirements-platform/internal/domain/links"
)

// ApplyManagedLinkEdits makes the managed link edits of an update of the
// artifact artifactID, whose project, base, the caller is already
// authorized to edit: the removals first, then the adds. An edit of one
// artifact changes that artifact's own links only: a removal must name a
// link of the edited artifact (#379 bug 198), and an add must have it at
// one end (#379 bug 201). A removal also needs the role p asks on both its
// ends' projects; an add must pass the link rules, then the gate and the
// roles p asks (CheckLink). An entry it cannot make is dealt with as
// p.OnInvalid says, and an entry that is not a link id or a link object is
// passed over. It publishes no event and refreshes no snapshot: the edited
// artifact's snapshot rides on its update (SetLinksSnapshot), and the other
// ends it returns are the caller's to refresh once the update is written.
func (s *Service) ApplyManagedLinkEdits(p Policy, base, artifactID string, toAdd, toRemove []interface{}) (*ManagedLinkChanges, error) {
	changes := &ManagedLinkChanges{}
	affectedArtifactIDs := make(map[string]bool) // Use map to avoid duplicates

	// Process removals (hard delete from table)
	for _, linkIDInterface := range toRemove {
		linkID, ok := linkIDInterface.(string)
		if !ok {
			continue
		}
		link, err := s.removeManagedLink(p, base, artifactID, linkID)
		if err != nil {
			return nil, err
		}
		if link == nil {
			continue
		}
		changes.Removed = append(changes.Removed, link)
		// Mark the other end as affected
		if link.ToID == artifactID {
			affectedArtifactIDs[link.FromID] = true
		} else {
			affectedArtifactIDs[link.ToID] = true
		}
	}

	// Process additions (create new links)
	for _, linkDataInterface := range toAdd {
		linkDataMap, ok := linkDataInterface.(map[string]interface{})
		if !ok {
			continue
		}
		link, err := s.addManagedLink(p, base, artifactID, linkDataMap)
		if err != nil {
			return nil, err
		}
		if link == nil {
			continue
		}
		changes.Added = append(changes.Added, link)
		// Mark both artifacts as affected
		affectedArtifactIDs[link.FromID] = true
		affectedArtifactIDs[link.ToID] = true
	}

	// Convert map to slice
	changes.Affected = make([]string, 0, len(affectedArtifactIDs))
	for id := range affectedArtifactIDs {
		if id != artifactID { // Don't include the artifact we're currently updating
			changes.Affected = append(changes.Affected, id)
		}
	}

	return changes, nil
}

// removeManagedLink removes the link linkID for an edit of artifactID: the
// link it removed, or nil and nil for one it skipped (Skip), or the reason
// (Refuse).
func (s *Service) removeManagedLink(p Policy, base, artifactID, linkID string) (*links.Link, error) {
	link, err := s.Links.GetLink(linkID)
	if err != nil || link == nil {
		return nil, p.refused(fmt.Errorf("no link %q: %v", linkID, err),
			"traceability: skipping link removal, no such link", "link_id", linkID, "error", err)
	}
	if link.FromID != artifactID && link.ToID != artifactID {
		return nil, p.refused(errors.New("the link does not touch the edited artifact"),
			"traceability: skipping link removal, the link does not touch the edited artifact",
			"link_id", linkID, "artifact_id", artifactID)
	}
	if err := s.CheckFlowDown(p, base, link.Type); err != nil {
		return nil, p.refused(err, "traceability: skipping link removal, the flow-down feature is closed", "link_id", linkID)
	}
	// The other end may live outside the base project; the caller
	// needs the role p asks on both ends' projects to remove the link.
	if err := s.reachArtifact(p, base, link.FromID, false, link.Type); err != nil {
		return nil, p.refused(err, "traceability: skipping link removal, no editor access to a linked artifact's project", "link_id", linkID)
	}
	if err := s.reachArtifact(p, base, link.ToID, true, link.Type); err != nil {
		return nil, p.refused(err, "traceability: skipping link removal, no editor access to a linked artifact's project", "link_id", linkID)
	}

	if err := s.Links.DeleteLink(linkID); err != nil {
		return nil, p.refused(err, "traceability: failed to delete link", "link_id", linkID, "error", err)
	}
	return link, nil
}

// reachArtifact is reach for the artifact at one end of a link being
// removed: an artifact no row has refuses, as one outside every project
// the caller may edit.
func (s *Service) reachArtifact(p Policy, base, artifactID string, target bool, linkType string) error {
	artifact, err := s.Artifacts.GetArtifact(artifactID)
	if err != nil || artifact == nil {
		return fmt.Errorf("no artifact %q", artifactID)
	}
	return s.reach(p, base, EndOf(artifact), target, linkType)
}

// addManagedLink makes the link one entry of pendingLinkAdds asks for, for
// an edit of artifactID: the link it made, or nil and nil for one it
// skipped (Skip) or passed over, or the reason (Refuse). The link takes the
// entry's attributes, or an empty object when it has none, where POST
// /api/v1/links stores null.
func (s *Service) addManagedLink(p Policy, base, artifactID string, linkDataMap map[string]interface{}) (*links.Link, error) {
	fromID, ok := linkDataMap["from_id"].(string)
	if !ok {
		return nil, nil
	}
	toID, ok := linkDataMap["to_id"].(string)
	if !ok {
		return nil, nil
	}
	linkType, ok := linkDataMap["type"].(string)
	if !ok {
		return nil, nil
	}
	if fromID != artifactID && toID != artifactID {
		return nil, p.refused(errors.New("the link does not touch the edited artifact"),
			"traceability: skipping link add, the link does not touch the edited artifact", "from_id", fromID, "to_id", toID)
	}

	// Get or create attributes
	var attributes map[string]interface{}
	if attrs, ok := linkDataMap["attributes"].(map[string]interface{}); ok {
		attributes = attrs
	} else {
		attributes = make(map[string]interface{})
	}

	// Validate link type against artifact types
	fromArtifact, err := s.Artifacts.GetArtifact(fromID)
	if err != nil {
		return nil, p.refused(err, "traceability: failed to get source artifact for link validation", "artifact_id", fromID, "error", err)
	}
	toArtifact, err := s.Artifacts.GetArtifact(toID)
	if err != nil {
		return nil, p.refused(err, "traceability: failed to get target artifact for link validation", "artifact_id", toID, "error", err)
	}

	// The link rules, then the gate and, since both ends get a version
	// bump and a note, the role p asks on the other project.
	if err := s.CheckLink(p, base, EndOf(fromArtifact), EndOf(toArtifact), linkType); err != nil {
		var refusal *AccessError
		switch {
		case errors.As(err, &refusal) && refusal.End == "source":
			return nil, p.refused(err, "traceability: skipping link add, no editor access to the source artifact's project", "from_id", fromID, "to_id", toID)
		case errors.As(err, &refusal):
			return nil, p.refused(err, "traceability: skipping link add, no editor access to the target artifact's project", "from_id", fromID, "to_id", toID)
		case errors.Is(err, ErrFlowDownClosed):
			return nil, p.refused(err, "traceability: skipping link add, the flow-down feature is closed", "from_id", fromID, "to_id", toID)
		}
		return nil, p.refused(err, "traceability: invalid link type", "error", err)
	}

	// Create the link
	link := links.NewLink(links.CreateLinkRequest{
		FromID:     fromID,
		ToID:       toID,
		Type:       linkType,
		Attributes: attributes,
	})
	if err := s.Links.CreateLink(link); err != nil {
		return nil, p.refused(err, "traceability: failed to create link", "from_id", fromID, "to_id", toID, "error", err)
	}
	return link, nil
}

// refused is what a write of several links does with one it cannot make:
// under Refuse it returns err, which stops the write; under Skip it logs
// msg with args and returns nil, and the write goes on.
func (p Policy) refused(err error, msg string, args ...interface{}) error {
	if p.OnInvalid == Refuse {
		return err
	}
	slog.Warn(msg, args...)
	return nil
}
