package api

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// The AI map is the token-optimal read surface for coding agents: the whole
// project as an indented outline, one artifact per line, addressed by stable
// refs, with link annotations inline. It deliberately carries no bodies —
// an agent orients on the map (a few thousand tokens for hundreds of
// artifacts, ~10× smaller than the full listing), then pulls bodies for the
// handful of artifacts it actually needs via get_artifact / get_context.
// Served by GET /api/v1/projects/{id}/ai-map (see ai_map_handlers.go) and
// the get_project_map MCP tool.

// buildAIMap renders the map. source describes provenance ("live state" or
// a baseline stamp); artifacts and links are the project's current set or a
// baseline snapshot.
func buildAIMap(projectName, source string, arts []*artifacts.Artifact, lks []*links.Link, now time.Time) string {
	refOf := make(map[string]string, len(arts)) // artifact id -> display ref
	for _, a := range arts {
		if a.Ref != "" {
			refOf[a.ID] = a.Ref
		} else if len(a.ID) >= 8 {
			// Pre-ref data (e.g. a baseline captured before refs existed):
			// fall back to a #uuid-prefix pseudo-ref so lines stay short.
			refOf[a.ID] = "#" + a.ID[:8]
		} else {
			refOf[a.ID] = "#" + a.ID
		}
	}

	// Inline link annotations per artifact: → this artifact links out,
	// ← something links to it. Sorted for stable output.
	out := make(map[string][]string)
	in := make(map[string][]string)
	for _, l := range lks {
		fromRef, okFrom := refOf[l.FromID]
		toRef, okTo := refOf[l.ToID]
		if !okFrom || !okTo {
			continue // endpoint outside this artifact set
		}
		suspect := ""
		if l.Suspect {
			suspect = "?" // legend: trailing ? marks a suspect link
		}
		out[l.FromID] = append(out[l.FromID], fmt.Sprintf("→%s %s%s", l.Type, toRef, suspect))
		in[l.ToID] = append(in[l.ToID], fmt.Sprintf("←%s %s%s", l.Type, fromRef, suspect))
	}

	// Children in the repository's stable tree order.
	children := make(map[string][]*artifacts.Artifact)
	byID := make(map[string]*artifacts.Artifact, len(arts))
	for _, a := range arts {
		byID[a.ID] = a
	}
	var roots []*artifacts.Artifact
	for _, a := range arts {
		if a.ParentID != nil && *a.ParentID != "" {
			if _, ok := byID[*a.ParentID]; ok {
				children[*a.ParentID] = append(children[*a.ParentID], a)
				continue
			}
		}
		roots = append(roots, a)
	}
	order := func(list []*artifacts.Artifact) {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].SortOrder != list[j].SortOrder {
				return list[i].SortOrder < list[j].SortOrder
			}
			return list[i].CreatedAt.Before(list[j].CreatedAt)
		})
	}
	order(roots)
	for _, c := range children {
		order(c)
	}

	typeCounts := make(map[string]int)
	for _, a := range arts {
		typeCounts[artifacts.RefPrefix(a.Type)]++
	}
	prefixes := make([]string, 0, len(typeCounts))
	for p := range typeCounts {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	countParts := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		countParts = append(countParts, fmt.Sprintf("%s %d", p, typeCounts[p]))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — AI map\n", projectName)
	fmt.Fprintf(&b, "Source: %s · Generated: %s\n", source, now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Artifacts: %d (%s) · Links: %d\n", len(arts), strings.Join(countParts, ", "), len(lks))
	b.WriteString("Syntax: REF Title [status] {→type TARGET; ←type SOURCE} — indent = hierarchy, → links out, ← linked from, trailing ? = suspect link, [status] omitted when draft. Bodies via get_artifact/get_context.\n\n")

	seen := make(map[string]bool, len(arts))
	var walk func(a *artifacts.Artifact, depth int)
	walk = func(a *artifacts.Artifact, depth int) {
		if seen[a.ID] {
			return // parent cycle guard; corrupt data must not hang the map
		}
		seen[a.ID] = true

		line := strings.Repeat("  ", depth) + refOf[a.ID] + " " + a.Title
		if a.Status != "" && a.Status != artifacts.StatusDraft {
			line += " [" + a.Status + "]"
		}
		ann := make([]string, 0, 2)
		if o := out[a.ID]; len(o) > 0 {
			sort.Strings(o)
			ann = append(ann, strings.Join(o, "; "))
		}
		if i := in[a.ID]; len(i) > 0 {
			sort.Strings(i)
			ann = append(ann, strings.Join(i, "; "))
		}
		if len(ann) > 0 {
			line += " {" + strings.Join(ann, "; ") + "}"
		}
		b.WriteString(line)
		b.WriteByte('\n')
		for _, c := range children[a.ID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}

	return b.String()
}
