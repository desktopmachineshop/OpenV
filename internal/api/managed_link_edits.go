package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// buildChangesSummary creates a detailed summary of what changed in the artifact
func (h *Handler) buildChangesSummary(oldArtifact, newArtifact *artifacts.Artifact, addedLinks, removedLinks []*links.Link) string {
	// Get basic field changes
	changes := h.buildChangesList(oldArtifact, newArtifact, addedLinks, removedLinks)

	// Add detailed link changes
	if len(addedLinks) > 0 {
		changes = append(changes, fmt.Sprintf("Links:"))
		for _, link := range addedLinks {
			var otherArtifactID string
			var linkDirection string

			// Determine if this artifact is the source or target
			if link.FromID == newArtifact.ID {
				otherArtifactID = link.ToID
				linkDirection = link.Type
			} else {
				otherArtifactID = link.FromID
				linkDirection = link.Type
			}

			// Try to get the other artifact's title
			otherArtifact, err := h.ArtifactService.GetArtifact(otherArtifactID)
			var artifactTitle string
			if err == nil {
				artifactTitle = otherArtifact.Title
			} else {
				artifactTitle = otherArtifactID
			}

			changes = append(changes, fmt.Sprintf("    - %s: %s (added)", linkDirection, artifactTitle))
		}
	}

	if len(removedLinks) > 0 {
		if len(addedLinks) == 0 {
			changes = append(changes, fmt.Sprintf("Links:"))
		}
		for _, link := range removedLinks {
			var otherArtifactID string
			var linkDirection string

			// Determine if this artifact is the source or target
			if link.FromID == newArtifact.ID {
				otherArtifactID = link.ToID
				linkDirection = link.Type
			} else {
				otherArtifactID = link.FromID
				linkDirection = link.Type
			}

			// Try to get the other artifact's title
			otherArtifact, err := h.ArtifactService.GetArtifact(otherArtifactID)
			var artifactTitle string
			if err == nil {
				artifactTitle = otherArtifact.Title
			} else {
				artifactTitle = otherArtifactID
			}

			changes = append(changes, fmt.Sprintf("    - %s: %s (removed)", linkDirection, artifactTitle))
		}
	}

	// Build message
	message := fmt.Sprintf("Updated to version %d", newArtifact.Version)

	if len(changes) > 0 {
		message += "\n\nChanges:\n"
		for _, change := range changes {
			if strings.HasPrefix(change, "  -") || strings.HasPrefix(change, "  +") {
				// Diff lines - include as-is (indented)
				message += change + "\n"
			} else if strings.HasPrefix(change, "    ") {
				// Indented lines (link details)
				message += change + "\n"
			} else if strings.HasPrefix(change, "Links:") {
				// Links header
				message += "- " + change + "\n"
			} else if strings.HasPrefix(change, "Description modified:") {
				// Description header
				message += "- " + change + "\n"
			} else {
				// Regular changes
				message += "- " + change + "\n"
			}
		}
	}

	return message
}

// buildChangesList creates the list of changes without wrapping in a message
func (h *Handler) buildChangesList(oldArtifact, newArtifact *artifacts.Artifact, addedLinks, removedLinks []*links.Link) []string {
	var changes []string

	// Check for field changes
	if oldArtifact.Title != newArtifact.Title {
		if oldArtifact.Title == "" {
			changes = append(changes, fmt.Sprintf("Title: → \"%s\"", newArtifact.Title))
		} else {
			changes = append(changes, fmt.Sprintf("Title: \"%s\" → \"%s\"", oldArtifact.Title, newArtifact.Title))
		}
	}

	if oldArtifact.Body != newArtifact.Body {
		// For multiline content, show a git-style diff
		if strings.Contains(oldArtifact.Body, "\n") || strings.Contains(newArtifact.Body, "\n") {
			changes = append(changes, "Description modified:")
			oldLines := strings.Split(oldArtifact.Body, "\n")
			newLines := strings.Split(newArtifact.Body, "\n")

			// Simple diff: show removed lines starting with -, added lines starting with +
			maxLines := len(oldLines)
			if len(newLines) > maxLines {
				maxLines = len(newLines)
			}

			var diffLines []string
			for i := 0; i < maxLines; i++ {
				if i < len(oldLines) && i < len(newLines) {
					if oldLines[i] != newLines[i] {
						diffLines = append(diffLines, fmt.Sprintf("  - %s", oldLines[i]))
						diffLines = append(diffLines, fmt.Sprintf("  + %s", newLines[i]))
					}
				} else if i < len(oldLines) {
					diffLines = append(diffLines, fmt.Sprintf("  - %s", oldLines[i]))
				} else if i < len(newLines) {
					diffLines = append(diffLines, fmt.Sprintf("  + %s", newLines[i]))
				}
			}

			changes = append(changes, strings.Join(diffLines, "\n"))
		} else {
			// For single-line content, use the short format
			if oldArtifact.Body == "" {
				changes = append(changes, fmt.Sprintf("Body: → \"%s\"", newArtifact.Body))
			} else {
				changes = append(changes, fmt.Sprintf("Body: \"%s\" → \"%s\"", oldArtifact.Body, newArtifact.Body))
			}
		}
	}

	if oldArtifact.Type != newArtifact.Type {
		changes = append(changes, fmt.Sprintf("Type: \"%s\" → \"%s\"", oldArtifact.Type, newArtifact.Type))
	}

	// Check for image changes
	oldImages := oldArtifact.GetImagesSnapshot()
	newImages := newArtifact.GetImagesSnapshot()

	oldImageMap := make(map[string]bool)
	newImageMap := make(map[string]bool)

	// Build maps of filenames
	for _, img := range oldImages {
		if imgData, ok := img.(map[string]interface{}); ok {
			if filename, ok := imgData["filename"].(string); ok {
				oldImageMap[filename] = true
			}
		}
	}

	for _, img := range newImages {
		if imgData, ok := img.(map[string]interface{}); ok {
			if filename, ok := imgData["filename"].(string); ok {
				newImageMap[filename] = true
			}
		}
	}

	// Find added images
	addedImages := 0
	for filename := range newImageMap {
		if !oldImageMap[filename] {
			addedImages++
		}
	}

	// Find removed images
	removedImages := 0
	for filename := range oldImageMap {
		if !newImageMap[filename] {
			removedImages++
		}
	}

	if addedImages > 0 {
		changes = append(changes, fmt.Sprintf("Images: Added %d image(s)", addedImages))
	}

	if removedImages > 0 {
		changes = append(changes, fmt.Sprintf("Images: Removed %d image(s)", removedImages))
	}

	return changes
}

// linksFromPendingAdds converts the pending link-add payloads of an artifact
// update ({from_id,to_id,type,...} objects, mirroring what
// processManagedLinkChanges creates) into Link values for the chatter
// summary. Entries missing any of the three required fields are skipped,
// matching the create path's behavior.
func linksFromPendingAdds(toAdd []interface{}) []*links.Link {
	var added []*links.Link
	for _, linkDataInterface := range toAdd {
		linkData, ok := linkDataInterface.(map[string]interface{})
		if !ok {
			continue
		}
		fromID, _ := linkData["from_id"].(string)
		toID, _ := linkData["to_id"].(string)
		linkType, _ := linkData["type"].(string)
		if fromID == "" || toID == "" || linkType == "" {
			continue
		}
		added = append(added, &links.Link{FromID: fromID, ToID: toID, Type: linkType})
	}
	return added
}

// processManagedLinkChanges handles link additions and removals
// Returns list of artifact IDs that had links change (for auto-versioning)
// baseProjectID is the project of the artifact being updated; the caller has
// already verified editor rights on it. Entries touching artifacts in other
// projects are skipped unless the caller has editor rights there too.
func (h *Handler) processManagedLinkChanges(r *http.Request, baseProjectID, fromArtifactID string, toAdd, toRemove []interface{}) ([]string, error) {
	affectedArtifactIDs := make(map[string]bool) // Use map to avoid duplicates

	// canEditLinkedArtifact reports whether the caller may edit the project
	// the given artifact belongs to (the base project is already authorized).
	canEditLinkedArtifact := func(artifactID string) bool {
		artifact, err := h.ArtifactService.GetArtifact(artifactID)
		if err != nil || artifact == nil {
			return false
		}
		return artifact.ProjectID == baseProjectID || h.hasProjectRole(r, artifact.ProjectID, members.RoleEditor)
	}

	// Process removals (hard delete from table)
	for _, linkIDInterface := range toRemove {
		linkID, ok := linkIDInterface.(string)
		if !ok {
			continue
		}

		// Get the link before deleting to determine affected artifact
		link, err := h.LinkService.GetLink(linkID)
		if err == nil && link != nil {
			// Both endpoints may live outside the base project; the caller
			// needs editor rights on their projects to remove the link.
			if !canEditLinkedArtifact(link.FromID) || !canEditLinkedArtifact(link.ToID) {
				slog.Warn("api: skipping link removal, no editor access to a linked artifact's project", "link_id", linkID)
				continue
			}
			// Mark the 'to' artifact as affected (it had an incoming link removed)
			if link.ToID == fromArtifactID {
				affectedArtifactIDs[link.FromID] = true
			} else {
				affectedArtifactIDs[link.ToID] = true
			}
		}

		// Hard delete the link
		err = h.LinkService.DeleteLink(linkID)
		if err != nil {
			slog.Warn("api: failed to delete link", "link_id", linkID, "error", err)
		}
	}

	// Process additions (create new links)
	for _, linkDataInterface := range toAdd {
		linkDataMap, ok := linkDataInterface.(map[string]interface{})
		if !ok {
			continue
		}

		fromID, ok := linkDataMap["from_id"].(string)
		if !ok {
			continue
		}
		toID, ok := linkDataMap["to_id"].(string)
		if !ok {
			continue
		}
		linkType, ok := linkDataMap["type"].(string)
		if !ok {
			continue
		}

		// Get or create attributes
		var attributes map[string]interface{}
		if attrs, ok := linkDataMap["attributes"].(map[string]interface{}); ok {
			attributes = attrs
		} else {
			attributes = make(map[string]interface{})
		}

		// Validate link type against artifact types
		fromArtifact, err := h.ArtifactService.GetArtifact(fromID)
		if err != nil {
			slog.Warn("api: failed to get source artifact for link validation", "artifact_id", fromID, "error", err)
			continue
		}

		toArtifact, err := h.ArtifactService.GetArtifact(toID)
		if err != nil {
			slog.Warn("api: failed to get target artifact for link validation", "artifact_id", toID, "error", err)
			continue
		}

		if err := links.ValidateLinkType(linkType, fromArtifact.Type, toArtifact.Type); err != nil {
			slog.Warn("api: invalid link type", "error", err)
			continue
		}

		// Both endpoints get a version bump + chatter; a cross-project link
		// needs editor rights on the other project too.
		if fromArtifact.ProjectID != baseProjectID && !h.hasProjectRole(r, fromArtifact.ProjectID, members.RoleEditor) {
			slog.Warn("api: skipping link add, no editor access to the source artifact's project", "from_id", fromID, "to_id", toID)
			continue
		}
		if toArtifact.ProjectID != baseProjectID && !h.hasProjectRole(r, toArtifact.ProjectID, members.RoleEditor) {
			slog.Warn("api: skipping link add, no editor access to the target artifact's project", "from_id", fromID, "to_id", toID)
			continue
		}

		// Create the link
		linkReq := links.CreateLinkRequest{
			FromID:     fromID,
			ToID:       toID,
			Type:       linkType,
			Attributes: attributes,
		}
		link := links.NewLink(linkReq)
		err = h.LinkService.CreateLink(link)
		if err != nil {
			slog.Warn("api: failed to create link", "from_id", fromID, "to_id", toID, "error", err)
			continue
		}

		// Mark both artifacts as affected
		affectedArtifactIDs[fromID] = true
		affectedArtifactIDs[toID] = true
	}

	// Convert map to slice
	result := make([]string, 0, len(affectedArtifactIDs))
	for id := range affectedArtifactIDs {
		if id != fromArtifactID { // Don't include the artifact we're currently updating
			result = append(result, id)
		}
	}

	return result, nil
}

// autoVersionLinkedArtifacts creates new versions for artifacts that had link changes
func (h *Handler) autoVersionLinkedArtifacts(affectedArtifactIDs []string) error {
	for _, artifactID := range affectedArtifactIDs {
		artifact, err := h.ArtifactService.GetArtifact(artifactID)
		if err != nil {
			slog.Warn("api: could not find artifact for auto-versioning", "artifact_id", artifactID, "error", err)
			continue
		}

		// Get current links for this artifact and deduplicate
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)

		incomingLinks, err := h.LinkService.GetLinksTo(artifactID)
		if err != nil {
			slog.Warn("api: could not get incoming links", "artifact_id", artifactID, "error", err)
			continue
		}

		outgoingLinks, err := h.LinkService.GetLinksFrom(artifactID)
		if err != nil {
			slog.Warn("api: could not get outgoing links", "artifact_id", artifactID, "error", err)
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

		_, err = h.ArtifactService.UpdateArtifact(artifactID, updateReq)
		if err != nil {
			slog.Warn("api: failed to auto-version artifact", "artifact_id", artifactID, "error", err)
			continue
		}

		// Create chatter entry for this auto-version
		newVersion := artifact.Version + 1
		chatterMessage := fmt.Sprintf("Auto-updated to version %d due to link changes", newVersion)
		chatterEntry := chatter.NewChatterEntry(artifactID, chatterMessage, true, "link-change")
		if err := h.ChatterService.CreateEntry(chatterEntry); err != nil {
			slog.Warn("api: failed to create chatter entry for auto-versioned artifact", "artifact_id", artifactID, "error", err)
		}

		slog.Debug("api: auto-versioned artifact due to link changes",
			"artifact_id", artifactID, "incoming_links", len(incomingLinks), "outgoing_links", len(outgoingLinks))
	}

	return nil
}
