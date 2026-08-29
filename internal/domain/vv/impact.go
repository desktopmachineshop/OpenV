package vv

import (
	"sort"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

// Traversal directions for impact analysis.
const (
	DirectionDownstream = "downstream"
	DirectionUpstream   = "upstream"
	DirectionBoth       = "both"
)

// maxImpactDepth bounds how far a traversal walks from the seed artifact. It
// guards against pathological graphs while still comfortably covering real
// need -> requirement -> design -> test chains, which are only a few hops deep.
const maxImpactDepth = 25

// Link semantics in OpenV are directional: a link FromID -> ToID means the
// FromID artifact traces up to (depends on) the ToID artifact. A test case
// "verifies" a requirement (test-case -> requirement); a requirement
// "derives-from" a user need (requirement -> user-need); a design item
// "satisfies" a requirement and "mitigates" a hazard; a requirement
// "decomposes-to" a sub-requirement. In every case the From end is the more
// derived / dependent artifact and the To end is the source it depends on.
//
// From this convention:
//   - DOWNSTREAM of X = the artifacts that depend on X (what breaks if X
//     changes). These are reached by walking links backwards: for every link
//     whose ToID is X, the FromID is downstream.
//   - UPSTREAM of X = the artifacts X itself depends on (its rationale /
//     sources). These are reached by walking links forwards: for every link
//     whose FromID is X, the ToID is upstream.

// ImpactNode is one artifact reached while traversing the link graph from a
// seed artifact, with the shortest path and hop distance back to the seed.
type ImpactNode struct {
	ArtifactID string   `json:"artifact_id"`
	Title      string   `json:"title"`
	Type       string   `json:"type"`
	Distance   int      `json:"distance"` // hops from the seed (>=1)
	Via        string   `json:"via"`      // link type of the final hop
	Path       []string `json:"path"`     // artifact IDs from seed to node, inclusive
}

// ImpactGroup collects the affected artifacts of a single type.
type ImpactGroup struct {
	Type  string       `json:"type"`
	Count int          `json:"count"`
	Nodes []ImpactNode `json:"nodes"`
}

// ImpactReport is the affected-set for one seed artifact, grouped by artifact
// type per direction.
type ImpactReport struct {
	ProjectID  string        `json:"project_id"`
	ArtifactID string        `json:"artifact_id"`
	Direction  string        `json:"direction"`
	Downstream []ImpactGroup `json:"downstream"`
	Upstream   []ImpactGroup `json:"upstream"`
	Total      int           `json:"total"` // distinct affected artifacts across populated directions
}

// impactEdge is a directed adjacency edge annotated with the link type that
// produced it.
type impactEdge struct {
	to  string
	via string
}

// ComputeImpact traverses the project link graph from artifactID and returns
// the reachable artifacts grouped by type. direction selects downstream,
// upstream, or both (any other value is treated as "both"). Traversal is
// breadth-first so recorded distances/paths are the shortest; a visited set
// guards against cycles and depth is bounded by maxImpactDepth.
func ComputeImpact(export *exports.ProjectExport, artifactID, direction string) *ImpactReport {
	switch direction {
	case DirectionDownstream, DirectionUpstream, DirectionBoth:
	default:
		direction = DirectionBoth
	}

	report := &ImpactReport{
		ProjectID:  export.ProjectID,
		ArtifactID: artifactID,
		Direction:  direction,
		Downstream: []ImpactGroup{},
		Upstream:   []ImpactGroup{},
	}

	byID := make(map[string]*artifacts.Artifact, len(export.Artifacts))
	for _, a := range export.Artifacts {
		if a != nil {
			byID[a.ID] = a
		}
	}

	affected := map[string]bool{}
	if direction == DirectionDownstream || direction == DirectionBoth {
		nodes := traverseImpact(artifactID, buildImpactAdjacency(export, true), byID)
		report.Downstream = groupImpactByType(nodes)
		for _, n := range nodes {
			affected[n.ArtifactID] = true
		}
	}
	if direction == DirectionUpstream || direction == DirectionBoth {
		nodes := traverseImpact(artifactID, buildImpactAdjacency(export, false), byID)
		report.Upstream = groupImpactByType(nodes)
		for _, n := range nodes {
			affected[n.ArtifactID] = true
		}
	}
	report.Total = len(affected)
	return report
}

// buildImpactAdjacency builds the traversal adjacency for one direction.
// When downstream is true edges point from a link's ToID back to its FromID
// (who depends on this); otherwise edges point from FromID to ToID (what this
// depends on).
func buildImpactAdjacency(export *exports.ProjectExport, downstream bool) map[string][]impactEdge {
	adj := make(map[string][]impactEdge)
	for _, l := range export.Links {
		if l == nil || l.FromID == "" || l.ToID == "" {
			continue
		}
		if downstream {
			adj[l.ToID] = append(adj[l.ToID], impactEdge{to: l.FromID, via: l.Type})
		} else {
			adj[l.FromID] = append(adj[l.FromID], impactEdge{to: l.ToID, via: l.Type})
		}
	}
	return adj
}

// traverseImpact runs a breadth-first walk from startID over adj, returning
// every reached artifact (excluding the seed) with its shortest path. The
// visited set makes cycles safe and maxImpactDepth caps the walk.
func traverseImpact(startID string, adj map[string][]impactEdge, byID map[string]*artifacts.Artifact) []ImpactNode {
	type queued struct {
		id   string
		path []string
	}
	visited := map[string]bool{startID: true}
	queue := []queued{{id: startID, path: []string{startID}}}
	var out []ImpactNode

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if len(cur.path)-1 >= maxImpactDepth {
			continue
		}
		// Deterministic neighbour order keeps output stable across runs.
		edges := append([]impactEdge{}, adj[cur.id]...)
		sort.SliceStable(edges, func(i, j int) bool { return edges[i].to < edges[j].to })
		for _, e := range edges {
			if visited[e.to] {
				continue
			}
			visited[e.to] = true
			path := make([]string, len(cur.path)+1)
			copy(path, cur.path)
			path[len(cur.path)] = e.to
			node := ImpactNode{
				ArtifactID: e.to,
				Distance:   len(path) - 1,
				Via:        e.via,
				Path:       path,
			}
			if a := byID[e.to]; a != nil {
				node.Title = a.Title
				node.Type = a.Type
			}
			out = append(out, node)
			queue = append(queue, queued{id: e.to, path: path})
		}
	}
	return out
}

// impactTypeRank orders groups so the traceability spine reads top-to-bottom;
// unranked types sort last, alphabetically.
func impactTypeRank(t string) int {
	switch t {
	case artifacts.TypeUserNeed:
		return 0
	case artifacts.TypeRequirement:
		return 1
	case artifacts.TypeDesignItem:
		return 2
	case artifacts.TypeTestCase:
		return 3
	case artifacts.TypeHazard:
		return 4
	case artifacts.TypePersona:
		return 5
	default:
		return 100
	}
}

// groupImpactByType buckets nodes by artifact type, ordering groups by the
// traceability spine and nodes within a group by nearest-first.
func groupImpactByType(nodes []ImpactNode) []ImpactGroup {
	buckets := map[string][]ImpactNode{}
	for _, n := range nodes {
		buckets[n.Type] = append(buckets[n.Type], n)
	}

	types := make([]string, 0, len(buckets))
	for t := range buckets {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool {
		ri, rj := impactTypeRank(types[i]), impactTypeRank(types[j])
		if ri != rj {
			return ri < rj
		}
		return types[i] < types[j]
	})

	groups := make([]ImpactGroup, 0, len(types))
	for _, t := range types {
		ns := buckets[t]
		sort.Slice(ns, func(i, j int) bool {
			if ns[i].Distance != ns[j].Distance {
				return ns[i].Distance < ns[j].Distance
			}
			if ns[i].Title != ns[j].Title {
				return ns[i].Title < ns[j].Title
			}
			return ns[i].ArtifactID < ns[j].ArtifactID
		})
		groups = append(groups, ImpactGroup{Type: t, Count: len(ns), Nodes: ns})
	}
	return groups
}
