package artifacts

import (
	"sort"
	"strconv"
	"strings"
)

// Document numbering: the "1.2.1" clause numbers a reader uses to navigate a
// rendered document.
//
// This is deliberately NOT the same thing as a stable ref (ref.go), and the
// two answer different questions:
//
//   - A ref ("REQ-12") identifies one artifact for its whole life. It is
//     minted once from a per-project counter, never reused even after the
//     artifact is deleted, and never changes when the document is
//     reorganized. It is what you cite in a review comment, a test result, or
//     a conversation six months later.
//   - A section number ("1.2") describes where an artifact currently sits.
//     It is derived from position, so inserting a section above renumbers
//     everything below it — which is correct for a table of contents and
//     useless as a durable citation.
//
// Both appear in reports for that reason: the number tells you where you are,
// the ref tells you what you are looking at.
//
// Only headings carry numbers. A requirement or hazard sitting inside a
// section is cited by its ref, exactly as it is in the tools this has to
// interoperate with; numbering every leaf would produce clause numbers that
// churn on every edit and compete with the refs for the reader's attention.

// SectionNumbers returns the document number for each heading artifact,
// keyed by artifact ID. Non-heading artifacts are absent from the map.
//
// The input is a project's live artifacts in any order; ordering within the
// document is taken from the tree itself (SortOrder, then CreatedAt, then ID
// as a final tie-break, so the result is deterministic for equal sort
// orders). Artifacts whose parent is missing from the input are treated as
// roots, so a filtered or partial list still numbers sensibly.
func SectionNumbers(list []*Artifact) map[string]string {
	numbers := make(map[string]string)
	if len(list) == 0 {
		return numbers
	}

	present := make(map[string]bool, len(list))
	for _, a := range list {
		if a != nil {
			present[a.ID] = true
		}
	}

	// Group by parent, treating a missing or dangling parent as a root.
	const rootKey = ""
	children := make(map[string][]*Artifact, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		parent := rootKey
		if a.ParentID != nil && *a.ParentID != "" && present[*a.ParentID] && *a.ParentID != a.ID {
			parent = *a.ParentID
		}
		children[parent] = append(children[parent], a)
	}
	for _, group := range children {
		sortSiblings(group)
	}

	// Depth-first in document order. counters[i] is the number last issued at
	// heading depth i; a non-heading passes its parent's counter stack
	// through unchanged, so a heading nested under a requirement still
	// numbers within its nearest heading ancestor.
	visited := make(map[string]bool, len(list))
	var walk func(parentID string, prefix []int)
	walk = func(parentID string, prefix []int) {
		counter := 0
		for _, a := range children[parentID] {
			// A parent cycle would otherwise recurse forever.
			if visited[a.ID] {
				continue
			}
			visited[a.ID] = true

			next := prefix
			if a.Type == TypeHeading {
				counter++
				next = append(append([]int(nil), prefix...), counter)
				numbers[a.ID] = formatSectionNumber(next)
			}
			walk(a.ID, next)
		}
	}
	walk(rootKey, nil)

	return numbers
}

// sortSiblings orders artifacts as they appear in the document: by explicit
// sort order, then creation time, then ID so equal values never reorder
// between calls.
func sortSiblings(group []*Artifact) {
	sort.SliceStable(group, func(i, j int) bool {
		a, b := group[i], group[j]
		if a.SortOrder != b.SortOrder {
			return a.SortOrder < b.SortOrder
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
}

// formatSectionNumber renders a counter stack as "1.2.1".
func formatSectionNumber(stack []int) string {
	parts := make([]string, len(stack))
	for i, n := range stack {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// ApplySectionNumbers stamps the computed number onto each artifact's
// transient DocNumber field. `all` must be the project's full artifact set
// (numbering is a property of the whole document); `page` is the subset being
// served, which may be a page or a type-filtered slice of it.
func ApplySectionNumbers(all []*Artifact, page []*Artifact) {
	numbers := SectionNumbers(all)
	for _, a := range page {
		if a == nil {
			continue
		}
		a.DocNumber = numbers[a.ID]
	}
}
