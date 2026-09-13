package api

// What the V&V Assistant is shown of a project when it works beside the
// project itself rather than beside the wizard.
//
// In the wizard the assistant reads the form; outside it there is no form,
// only the project's artifacts. Without a view of them it cannot name what
// to edit or say where something goes, and every "move REQ-12 under the
// safety heading" would be guesswork. So a project-mode turn carries an
// outline: every artifact's reference, type and title, indented under its
// parent in document order. Titles only, never bodies — the outline is a
// map, and the artifact on screen is the one whose text the assistant sees.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// projectOutline is the project as the assistant sees it beside the notes
// panel, and what it is allowed to propose about it.
type projectOutline struct {
	Artifacts []*artifacts.Artifact
	// Edits reports whether the workspace's release channel has the feature
	// that lets the assistant propose new artifacts of any type, edits and
	// moves. Off, the assistant is told so and offers only the wizard's
	// shapes, which land as drafts under the standard headings.
	Edits bool
}

// outlineBudget bounds the outline in characters: about six thousand
// tokens, which a project of a few hundred artifacts fits inside. Past it
// the outline is cut in document order and says how much it left out.
const outlineBudget = 24000

// renderProjectOutline writes one line per artifact — reference, type,
// title — indented under its parent, siblings in the order the module view
// shows them. A parent the list does not carry (a stale pointer) makes its
// children roots rather than losing them.
func renderProjectOutline(list []*artifacts.Artifact, budget int) string {
	if len(list) == 0 {
		return "(the project has no artifacts yet)"
	}
	byID := make(map[string]bool, len(list))
	for _, a := range list {
		byID[a.ID] = true
	}
	children := map[string][]*artifacts.Artifact{}
	for _, a := range list {
		parent := ""
		if a.ParentID != nil && byID[*a.ParentID] {
			parent = *a.ParentID
		}
		children[parent] = append(children[parent], a)
	}
	for _, group := range children {
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].SortOrder != group[j].SortOrder {
				return group[i].SortOrder < group[j].SortOrder
			}
			return group[i].Title < group[j].Title
		})
	}

	var b strings.Builder
	written := 0
	// seen guards against a cycle in the data, which would otherwise never
	// stop descending.
	seen := map[string]bool{}
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, a := range children[parent] {
			if seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			if b.Len() < budget {
				ref := a.Ref
				if ref == "" {
					ref = a.ID
				}
				fmt.Fprintf(&b, "%s%s  %s  %q\n", strings.Repeat("  ", depth), ref, a.Type, a.Title)
				written++
			}
			walk(a.ID, depth+1)
		}
	}
	walk("", 0)
	if left := len(list) - written; left > 0 {
		fmt.Fprintf(&b, "…(%d more artifacts not listed)\n", left)
	}
	return strings.TrimRight(b.String(), "\n")
}
