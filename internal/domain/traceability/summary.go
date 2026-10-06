// The change summaries: the version note an artifact update writes, and the
// list of changes a restore's note reuses.

package traceability

import (
	"fmt"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// BuildChangesSummary is the note an update of an artifact writes to its
// feed: "Updated to version N", then what changed (BuildChangesList) and
// the links the update made and removed, each named by the other end's
// title, or its id when the title cannot be read.
func (s *Service) BuildChangesSummary(oldArtifact, newArtifact *artifacts.Artifact, addedLinks, removedLinks []*links.Link) string {
	// Get basic field changes
	changes := BuildChangesList(oldArtifact, newArtifact, addedLinks, removedLinks)

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
			otherArtifact, err := s.Artifacts.GetArtifact(otherArtifactID)
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
			otherArtifact, err := s.Artifacts.GetArtifact(otherArtifactID)
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

// BuildChangesList lists what changed between two versions of an artifact,
// without the note's wrapping: the title, the body (a line diff when either
// side has several lines), the type, and the images added and removed. It
// lists no link: the links are BuildChangesSummary's.
func BuildChangesList(oldArtifact, newArtifact *artifacts.Artifact, addedLinks, removedLinks []*links.Link) []string {
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
		changes = append(changes, bodyChanges(oldArtifact.Body, newArtifact.Body)...)
	}

	if oldArtifact.Type != newArtifact.Type {
		changes = append(changes, fmt.Sprintf("Type: \"%s\" → \"%s\"", oldArtifact.Type, newArtifact.Type))
	}

	return append(changes, imageChanges(oldArtifact, newArtifact)...)
}

// bodyChanges is BuildChangesList's entry for a changed body.
func bodyChanges(oldBody, newBody string) []string {
	var changes []string
	// For multiline content, show a git-style diff
	if strings.Contains(oldBody, "\n") || strings.Contains(newBody, "\n") {
		changes = append(changes, "Description modified:")
		oldLines := strings.Split(oldBody, "\n")
		newLines := strings.Split(newBody, "\n")

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
		if oldBody == "" {
			changes = append(changes, fmt.Sprintf("Body: → \"%s\"", newBody))
		} else {
			changes = append(changes, fmt.Sprintf("Body: \"%s\" → \"%s\"", oldBody, newBody))
		}
	}
	return changes
}

// imageChanges is BuildChangesList's entries for the images added and
// removed, by file name.
func imageChanges(oldArtifact, newArtifact *artifacts.Artifact) []string {
	var changes []string

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
