package baselines

import (
	"sort"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

// SnapshotRef identifies one side of a baseline comparison: a stored
// baseline, or the live project (ID "live").
type SnapshotRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ArtifactSummary is a diff entry for an artifact that exists on only one
// side of the comparison (added or removed).
type ArtifactSummary struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// ModifiedArtifact is a diff entry for an artifact present on both sides
// with at least one tracked field changed. The flags summarize which fields
// differ; titles are carried on both sides so a rename can be displayed as
// "old → new".
type ModifiedArtifact struct {
	ID            string `json:"id"`
	Type          string `json:"type"` // target-side type
	OldTitle      string `json:"old_title"`
	NewTitle      string `json:"new_title"`
	TitleChanged  bool   `json:"title_changed"`
	BodyChanged   bool   `json:"body_changed"`
	TypeChanged   bool   `json:"type_changed"`
	StatusChanged bool   `json:"status_changed"`
	ParentChanged bool   `json:"parent_changed"`
}

// LinkChange is a diff entry for a link present on only one side of the
// comparison. Links are identified by (from, to, type) rather than row ID so
// a link that was deleted and recreated does not show up as a change. The
// endpoint titles are resolved from whichever snapshot knows the artifact,
// and fall back to "" when neither does.
type LinkChange struct {
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	Type      string `json:"type"`
	FromTitle string `json:"from_title"`
	ToTitle   string `json:"to_title"`
}

// DiffResult describes how a project changed between a base snapshot and a
// target snapshot: artifacts and links present only in the target are
// "added", ones present only in the base are "removed", and artifacts on
// both sides with tracked-field differences are "modified". Unchanged
// artifacts and links are omitted.
type DiffResult struct {
	Base         SnapshotRef        `json:"base"`
	Target       SnapshotRef        `json:"target"`
	Added        []ArtifactSummary  `json:"added"`
	Removed      []ArtifactSummary  `json:"removed"`
	Modified     []ModifiedArtifact `json:"modified"`
	LinksAdded   []LinkChange       `json:"links_added"`
	LinksRemoved []LinkChange       `json:"links_removed"`
}

// Diff computes the changes from base to target. It is a pure function over
// the two exports: artifacts are matched by ID (baselines snapshot the same
// project, so IDs are stable), links by (from, to, type). Base and Target
// refs on the result are left for the caller to fill in. All slices are
// non-nil so the result marshals as [] rather than null.
func Diff(base, target *exports.ProjectExport) *DiffResult {
	result := &DiffResult{
		Added:        []ArtifactSummary{},
		Removed:      []ArtifactSummary{},
		Modified:     []ModifiedArtifact{},
		LinksAdded:   []LinkChange{},
		LinksRemoved: []LinkChange{},
	}

	baseArtifacts := artifactsByID(base)
	targetArtifacts := artifactsByID(target)

	for id, targetArtifact := range targetArtifacts {
		baseArtifact, ok := baseArtifacts[id]
		if !ok {
			result.Added = append(result.Added, summarize(targetArtifact))
			continue
		}
		if change, changed := compareArtifacts(baseArtifact, targetArtifact); changed {
			result.Modified = append(result.Modified, change)
		}
	}
	for id, baseArtifact := range baseArtifacts {
		if _, ok := targetArtifacts[id]; !ok {
			result.Removed = append(result.Removed, summarize(baseArtifact))
		}
	}

	// titleOf resolves link endpoints against either side, preferring the
	// target (current) title.
	titleOf := func(id string) string {
		if a, ok := targetArtifacts[id]; ok {
			return a.Title
		}
		if a, ok := baseArtifacts[id]; ok {
			return a.Title
		}
		return ""
	}

	baseLinks := linksByKey(base)
	targetLinks := linksByKey(target)
	for key, link := range targetLinks {
		if _, ok := baseLinks[key]; !ok {
			result.LinksAdded = append(result.LinksAdded, linkChange(link, titleOf))
		}
	}
	for key, link := range baseLinks {
		if _, ok := targetLinks[key]; !ok {
			result.LinksRemoved = append(result.LinksRemoved, linkChange(link, titleOf))
		}
	}

	sortSummaries(result.Added)
	sortSummaries(result.Removed)
	sort.Slice(result.Modified, func(i, j int) bool {
		a, b := result.Modified[i], result.Modified[j]
		if a.NewTitle != b.NewTitle {
			return a.NewTitle < b.NewTitle
		}
		return a.ID < b.ID
	})
	sortLinkChanges(result.LinksAdded)
	sortLinkChanges(result.LinksRemoved)

	return result
}

// compareArtifacts builds the modified entry for an artifact present on both
// sides. The second return is false when no tracked field differs.
func compareArtifacts(base, target *artifacts.Artifact) (ModifiedArtifact, bool) {
	change := ModifiedArtifact{
		ID:            target.ID,
		Type:          target.Type,
		OldTitle:      base.Title,
		NewTitle:      target.Title,
		TitleChanged:  base.Title != target.Title,
		BodyChanged:   base.Body != target.Body,
		TypeChanged:   base.Type != target.Type,
		StatusChanged: artifactStatus(base) != artifactStatus(target),
		ParentChanged: parentID(base) != parentID(target),
	}
	changed := change.TitleChanged || change.BodyChanged || change.TypeChanged ||
		change.StatusChanged || change.ParentChanged
	return change, changed
}

// artifactStatus reads the conventional Attributes["status"] string ("" when
// unset or not a string).
func artifactStatus(a *artifacts.Artifact) string {
	if a.Attributes == nil {
		return ""
	}
	status, _ := a.Attributes["status"].(string)
	return status
}

// parentID normalizes the parent pointer: nil and "" both mean "no parent".
func parentID(a *artifacts.Artifact) string {
	if a.ParentID == nil {
		return ""
	}
	return *a.ParentID
}

func summarize(a *artifacts.Artifact) ArtifactSummary {
	return ArtifactSummary{ID: a.ID, Type: a.Type, Title: a.Title}
}

func artifactsByID(export *exports.ProjectExport) map[string]*artifacts.Artifact {
	byID := make(map[string]*artifacts.Artifact, len(export.Artifacts))
	for _, artifact := range export.Artifacts {
		if artifact == nil || artifact.ID == "" {
			continue
		}
		byID[artifact.ID] = artifact
	}
	return byID
}

// linkKey is the semantic identity of a link within a snapshot.
type linkKey struct {
	fromID string
	toID   string
	typ    string
}

func linksByKey(export *exports.ProjectExport) map[linkKey]linkEndpoints {
	byKey := make(map[linkKey]linkEndpoints, len(export.Links))
	for _, link := range export.Links {
		if link == nil {
			continue
		}
		byKey[linkKey{fromID: link.FromID, toID: link.ToID, typ: link.Type}] =
			linkEndpoints{FromID: link.FromID, ToID: link.ToID, Type: link.Type}
	}
	return byKey
}

// linkEndpoints carries the identity fields of a link out of the keyed map.
type linkEndpoints struct {
	FromID string
	ToID   string
	Type   string
}

func linkChange(link linkEndpoints, titleOf func(string) string) LinkChange {
	return LinkChange{
		FromID:    link.FromID,
		ToID:      link.ToID,
		Type:      link.Type,
		FromTitle: titleOf(link.FromID),
		ToTitle:   titleOf(link.ToID),
	}
}

func sortSummaries(entries []ArtifactSummary) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Title != entries[j].Title {
			return entries[i].Title < entries[j].Title
		}
		return entries[i].ID < entries[j].ID
	})
}

func sortLinkChanges(entries []LinkChange) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.FromTitle != b.FromTitle {
			return a.FromTitle < b.FromTitle
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.ToTitle != b.ToTitle {
			return a.ToTitle < b.ToTitle
		}
		if a.FromID != b.FromID {
			return a.FromID < b.FromID
		}
		return a.ToID < b.ToID
	})
}
