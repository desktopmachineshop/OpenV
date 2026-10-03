package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// GetArtifactVersions retrieves all versions of an artifact. The guard asks
// the project of the versions themselves, not of the current row, so a
// deleted artifact's history stays readable to its project's viewers
// (REQ-4); an id no version has answers as the guard answers any artifact
// no row has.
func (h *Handler) GetArtifactVersions(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	versions, projectID, ok := h.artifactHistory(w, r, id, true)
	if !ok || !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

// artifactHistory answers the project a read of an artifact is guarded by
// and, when the read is of its history, the versions it loaded: a history
// read asks the project of the versions, "" when there are none (an
// artifact never changes project, so any version says), so a deleted
// artifact's history reads as the artifact did (REQ-4); any other read, the
// current artifact's. It writes the 500 of a failed load itself.
func (h *Handler) artifactHistory(w http.ResponseWriter, r *http.Request, id string, history bool) ([]*artifacts.Artifact, string, bool) {
	if !history {
		return nil, h.projectIDForArtifact(id), true
	}
	versions, err := h.artifactService.GetArtifactVersions(id)
	if err != nil {
		respondInternal(w, r, "failed to load artifact versions", err)
		return nil, "", false
	}
	for _, v := range versions {
		if v != nil {
			return versions, v.ProjectID, true
		}
	}
	return versions, "", true
}

// RestoreArtifactVersion restores a previous version of an artifact
func (h *Handler) RestoreArtifactVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch the current artifact BEFORE restoring (to track changes), and
	// answer an id no artifact has as UpdateArtifact does.
	oldArtifact, err := h.artifactService.GetArtifact(id)
	if err != nil {
		respondArtifactLookup(w, r, err)
		return
	}

	if !h.requireProjectRoleFor(w, r, oldArtifact.ProjectID, members.RoleEditor, missing("artifact not found")) {
		return
	}

	artifact, err := h.artifactService.RestoreArtifactVersion(id, req.Version)
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrVersionNotFound):
			respondError(w, r, http.StatusNotFound, err.Error(), err)
		case errors.Is(err, artifacts.ErrNotFound):
			respondError(w, r, http.StatusNotFound, "artifact not found", err)
		default:
			respondInternal(w, r, "failed to restore artifact version", err)
		}
		return
	}

	// Create a chatter entry for the restore
	restoredFromVersion := req.Version
	chatterMessage := h.buildRestoreMessage(oldArtifact, artifact, restoredFromVersion)
	chatterEntry := chatter.NewChatterEntry(id, chatterMessage, true, "restore")
	if err := h.chatterService.CreateEntry(chatterEntry); err != nil {
		// Log but don't fail the request
		slog.Warn("api: failed to create chatter entry for restore", "artifact_id", id, "error", err)
	}

	// A restore is an edit the event stream records like any other (REQ-4):
	// the version it wrote and the one whose content it brought back.
	h.publish(r, events.ArtifactRestored, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type":    artifact.Type,
		"title":            artifact.Title,
		"version":          artifact.Version,
		"restored_version": restoredFromVersion,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// buildRestoreMessage creates a message describing what version was restored
func (h *Handler) buildRestoreMessage(oldArtifact, newArtifact *artifacts.Artifact, restoredFromVersion int) string {
	message := fmt.Sprintf("Restored to version %d (from version %d)", newArtifact.Version, restoredFromVersion)

	// Reuse the changes summary logic to show what changed
	var addedLinks, removedLinks []*links.Link
	changes := h.buildChangesList(oldArtifact, newArtifact, addedLinks, removedLinks)

	if len(changes) > 0 {
		message += "\n\nChanges:\n"
		for _, change := range changes {
			if strings.HasPrefix(change, "  -") || strings.HasPrefix(change, "  +") {
				// Diff lines - include as-is (indented)
				message += change + "\n"
			} else if strings.HasPrefix(change, "    ") {
				// Indented lines
				message += change + "\n"
			} else if strings.HasPrefix(change, "Links:") || strings.HasPrefix(change, "Description modified:") {
				// Headers
				message += "- " + change + "\n"
			} else {
				// Regular changes
				message += "- " + change + "\n"
			}
		}
	}

	return message
}

// GetArtifactVersionLinks retrieves links for a specific artifact version
func (h *Handler) GetArtifactVersionLinks(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	versionStr := r.URL.Query().Get("version")

	// A version's links are the artifact's history, which a deleted artifact
	// keeps (REQ-4); the live links are the current artifact's.
	versions, projectID, ok := h.artifactHistory(w, r, id, versionStr != "")
	if !ok || !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	// If no version specified, return current links from link table
	if versionStr == "" {
		outgoingLinks, err := h.linkService.GetLinksFrom(id)
		if err != nil {
			respondInternal(w, r, "failed to load links", err)
			return
		}
		incomingLinks, err := h.linkService.GetLinksTo(id)
		if err != nil {
			respondInternal(w, r, "failed to load links", err)
			return
		}

		// Combine both incoming and outgoing links and deduplicate by ID
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]*links.Link, 0)

		// Add outgoing links
		for _, link := range outgoingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		// Add incoming links (deduplicated)
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(allLinks)
		return
	}

	// Parse version number
	version := 0
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid version parameter")
		return
	}

	// Find the specific version
	var artifactVersion *artifacts.Artifact
	for _, v := range versions {
		if v.Version == version {
			artifactVersion = v
			break
		}
	}

	if artifactVersion == nil {
		writeJSONError(w, http.StatusNotFound, "artifact version not found")
		return
	}

	// Extract links_snapshot from artifact attributes
	var linksSnapshot []*links.Link
	if artifactVersion.Attributes != nil {
		if snapshot, ok := artifactVersion.Attributes["links_snapshot"]; ok {
			// Convert interface{} to []*links.Link
			if snapshotData, ok := snapshot.([]interface{}); ok {
				linksSnapshot = make([]*links.Link, 0, len(snapshotData))
				for _, linkData := range snapshotData {
					// Each linkData should be a map or already a Link struct
					if linkBytes, err := json.Marshal(linkData); err == nil {
						var link links.Link
						if err := json.Unmarshal(linkBytes, &link); err == nil {
							linksSnapshot = append(linksSnapshot, &link)
						}
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(linksSnapshot)
}
