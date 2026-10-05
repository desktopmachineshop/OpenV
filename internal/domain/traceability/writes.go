// The writes: a link made or removed, the links_snapshot of the artifacts it
// touches, and its event.

package traceability

import (
	"fmt"
	"log/slog"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// CreateLink makes the link req asks for, which the caller has checked, and
// refreshes both its ends' links_snapshot (RefreshLinkSnapshots). It
// returns the store's refusal as it is, having refreshed nothing. Its
// event is the caller's to publish (PublishLinkEvent), in the project the
// caller names.
func (s *Service) CreateLink(req links.CreateLinkRequest) (*links.Link, error) {
	link := links.NewLink(req)
	if err := s.Links.CreateLink(link); err != nil {
		return nil, err
	}
	s.RefreshLinkSnapshots(link.FromID, link.ToID)
	return link, nil
}

// DeleteLink removes the link id and, when the caller read it beforehand
// (link is not nil), refreshes both its ends' links_snapshot. It returns
// the store's refusal as it is, having refreshed nothing.
func (s *Service) DeleteLink(id string, link *links.Link) error {
	if err := s.Links.DeleteLink(id); err != nil {
		return err
	}
	if link != nil {
		s.RefreshLinkSnapshots(link.FromID, link.ToID)
	}
	return nil
}

// PublishLinkEvent publishes eventType (link.created or link.deleted) for
// link in projectID as p.Actor, with the link's type and ends, when p emits
// events and the service has a Publisher.
func (s *Service) PublishLinkEvent(p Policy, eventType, projectID string, link *links.Link) {
	if !p.EmitEvents || s.Publish == nil {
		return
	}
	s.Publish(eventType, projectID, link.ID, p.Actor, map[string]interface{}{
		"link_type": link.Type,
		"from_id":   link.FromID,
		"to_id":     link.ToID,
	})
}

// RefreshLinkSnapshots auto-versions each artifact: a new version whose
// links_snapshot is every live link it has, incoming first, each once (an
// empty list too), and a "link-change" note naming the version read before
// it plus 1 (quirk Q4). An artifact it cannot read, or whose links it
// cannot list, keeps its version, with a warning.
func (s *Service) RefreshLinkSnapshots(artifactIDs ...string) {
	for _, artifactID := range artifactIDs {
		artifact, err := s.Artifacts.GetArtifact(artifactID)
		if err != nil {
			slog.Warn("traceability: could not find artifact for auto-versioning", "artifact_id", artifactID, "error", err)
			continue
		}

		// Get current links for this artifact and deduplicate
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)

		incomingLinks, err := s.Links.GetLinksTo(artifactID)
		if err != nil {
			slog.Warn("traceability: could not get incoming links", "artifact_id", artifactID, "error", err)
			continue
		}

		outgoingLinks, err := s.Links.GetLinksFrom(artifactID)
		if err != nil {
			slog.Warn("traceability: could not get outgoing links", "artifact_id", artifactID, "error", err)
			continue
		}

		// Combine all links with deduplication
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}
		for _, link := range outgoingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		// Ensure attributes is initialized
		attributes := artifact.Attributes
		if attributes == nil {
			attributes = make(map[string]interface{})
		}

		// Merge link snapshot into attributes
		attributes["links_snapshot"] = allLinks

		// Create a new version with updated link snapshot. Content fields
		// (type/title/body) are left nil — "no change" per the issue-#170
		// contract — so this attribute-only write carries them forward and
		// never counts as a content edit (no suspect-link flagging).
		// Structural fields are omitted entirely: an absent ParentID
		// (issue-#172 contract) and nil SortOrder both mean "no change",
		// so the artifact keeps its place in the tree.
		updateReq := artifacts.UpdateArtifactRequest{
			Attributes: attributes,
		}

		_, err = s.Artifacts.UpdateArtifact(artifactID, updateReq)
		if err != nil {
			slog.Warn("traceability: failed to auto-version artifact", "artifact_id", artifactID, "error", err)
			continue
		}

		// Create chatter entry for this auto-version
		newVersion := artifact.Version + 1
		chatterMessage := fmt.Sprintf("Auto-updated to version %d due to link changes", newVersion)
		chatterEntry := chatter.NewChatterEntry(artifactID, chatterMessage, true, "link-change")
		if err := s.Notes.CreateEntry(chatterEntry); err != nil {
			slog.Warn("traceability: failed to create chatter entry for auto-versioned artifact", "artifact_id", artifactID, "error", err)
		}

		slog.Debug("traceability: auto-versioned artifact due to link changes",
			"artifact_id", artifactID, "incoming_links", len(incomingLinks), "outgoing_links", len(outgoingLinks))
	}
}

// SetLinksSnapshot writes the links artifactID has once its managed link
// edits are made, incoming first, each once, into req's attributes as
// links_snapshot, so the one version the update writes carries them. It
// writes it only while at least one link remains (quirk Q4): with none, the
// version carries the previous snapshot forward. A request that left
// attributes untouched (nil) first gets a copy of current, the artifact's
// attributes now, so the snapshot write clears none of them. A direction
// whose links cannot be listed is left out.
func (s *Service) SetLinksSnapshot(req *artifacts.UpdateArtifactRequest, artifactID string, current map[string]interface{}) {
	seenLinkIDs := make(map[string]bool)
	allLinks := make([]interface{}, 0)

	incomingLinks, err := s.Links.GetLinksTo(artifactID)
	if err == nil {
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}
	}

	outgoingLinks, err := s.Links.GetLinksFrom(artifactID)
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
			req.Attributes = make(map[string]interface{}, len(current)+1)
			for k, v := range current {
				req.Attributes[k] = v
			}
		}
		req.Attributes["links_snapshot"] = allLinks
	}
}
