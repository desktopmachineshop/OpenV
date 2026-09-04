package downloads

import (
	"sort"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

// BuildOptions reads a snapshot and reports what a download form can offer:
// the document's top-level sections, the artifact types present, and the
// attachment categories held.
//
// Everything here is derived from the project in front of the reader. A form
// built from a fixed list would offer "hazards" to a project with none and
// "data files" to one that holds only figures, and a filter that returns
// nothing is worse than no filter at all.
func BuildOptions(data *exports.ProjectExport) *Options {
	opts := &Options{Sections: []Section{}, Types: []TypeCount{}, Attachments: []exports.CategoryCount{}}
	if data == nil {
		return opts
	}

	numbers := artifacts.SectionNumbers(data.Artifacts)

	// Count what sits under each top-level heading, headings themselves
	// excluded: a section's weight is its content, not its structure.
	byID := make(map[string]*artifacts.Artifact, len(data.Artifacts))
	for _, a := range data.Artifacts {
		if a != nil {
			byID[a.ID] = a
		}
	}
	counts := map[string]int{}
	for _, a := range data.Artifacts {
		if a == nil || a.Type == artifacts.TypeHeading {
			continue
		}
		if root := topLevelAncestor(byID, a); root != "" {
			counts[root]++
		}
	}

	types := map[string]int{}
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		if a.Type == artifacts.TypeHeading {
			if a.ParentID == nil {
				opts.Sections = append(opts.Sections, Section{
					ID:        a.ID,
					Ref:       a.Ref,
					Number:    numbers[a.ID],
					Title:     a.Title,
					Artifacts: counts[a.ID],
				})
			}
			continue
		}
		types[a.Type]++
	}

	for t, n := range types {
		opts.Types = append(opts.Types, TypeCount{Type: t, Count: n})
	}
	sort.Slice(opts.Types, func(i, j int) bool {
		if opts.Types[i].Count != opts.Types[j].Count {
			return opts.Types[i].Count > opts.Types[j].Count
		}
		return opts.Types[i].Type < opts.Types[j].Type
	})

	opts.Attachments = exports.Categories(data.Attachments)
	if opts.Attachments == nil {
		opts.Attachments = []exports.CategoryCount{}
	}
	return opts
}

// topLevelAncestor walks up to the outermost ancestor of an artifact, or ""
// when it has none (it is top-level itself, or its ancestry loops).
func topLevelAncestor(byID map[string]*artifacts.Artifact, a *artifacts.Artifact) string {
	seen := map[string]bool{}
	cur := a
	for cur != nil && !seen[cur.ID] {
		seen[cur.ID] = true
		if cur.ParentID == nil {
			if cur.ID == a.ID {
				return ""
			}
			return cur.ID
		}
		cur = byID[*cur.ParentID]
	}
	return ""
}
