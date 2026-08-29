package vv

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// impactGraph builds a small but complete traceability chain:
//
//	need N1 <-derives-from- req R1 <-decomposes-to- req R2
//	req R1 <-satisfies- design D1 -mitigates-> hazard H1
//	req R1 <-verifies- test T1
//
// Link direction convention: FromID depends on ToID.
func impactGraph() (*artifacts.Artifact, []*artifacts.Artifact, []*links.Link) {
	n1 := mkArtifact("N1", artifacts.TypeUserNeed, "Need 1", nil)
	r1 := mkArtifact("R1", artifacts.TypeRequirement, "Requirement 1", nil)
	r2 := mkArtifact("R2", artifacts.TypeRequirement, "Sub-requirement 2", nil)
	d1 := mkArtifact("D1", artifacts.TypeDesignItem, "Design 1", nil)
	t1 := mkArtifact("T1", artifacts.TypeTestCase, "Test 1", nil)
	h1 := mkArtifact("H1", artifacts.TypeHazard, "Hazard 1", nil)

	artifactList := []*artifacts.Artifact{n1, r1, r2, d1, t1, h1}
	linkList := []*links.Link{
		mkLink("R1", "N1", "derives-from"),  // R1 depends on N1
		mkLink("R2", "R1", "decomposes-to"), // R2 depends on R1
		mkLink("D1", "R1", "satisfies"),     // D1 depends on R1
		mkLink("T1", "R1", "verifies"),      // T1 depends on R1
		mkLink("D1", "H1", "mitigates"),     // D1 depends on H1
	}
	return r1, artifactList, linkList
}

// nodeByID finds a node across all groups of a direction.
func nodeByID(groups []ImpactGroup, id string) (ImpactNode, bool) {
	for _, g := range groups {
		for _, n := range g.Nodes {
			if n.ArtifactID == id {
				return n, true
			}
		}
	}
	return ImpactNode{}, false
}

func groupTypes(groups []ImpactGroup) map[string]int {
	m := map[string]int{}
	for _, g := range groups {
		m[g.Type] = g.Count
	}
	return m
}

func TestComputeImpact_Downstream(t *testing.T) {
	seed, arts, lnks := impactGraph()
	rep := ComputeImpact(mkExport(arts, lnks), seed.ID, DirectionDownstream)

	if rep.Direction != DirectionDownstream {
		t.Fatalf("direction = %q, want downstream", rep.Direction)
	}
	if len(rep.Upstream) != 0 {
		t.Fatalf("downstream request should not populate upstream, got %d groups", len(rep.Upstream))
	}

	// Things that depend on R1: R2 (decomposes-to), D1 (satisfies), T1 (verifies).
	// D1 in turn mitigates H1, but H1 is upstream of D1 (D1 depends on H1), so
	// H1 must NOT appear downstream of R1.
	want := map[string]int{
		artifacts.TypeRequirement: 1, // R2
		artifacts.TypeDesignItem:  1, // D1
		artifacts.TypeTestCase:    1, // T1
	}
	got := groupTypes(rep.Downstream)
	for typ, n := range want {
		if got[typ] != n {
			t.Errorf("downstream group %q = %d, want %d", typ, got[typ], n)
		}
	}
	if _, ok := nodeByID(rep.Downstream, "H1"); ok {
		t.Errorf("hazard H1 should not be downstream of R1")
	}
	if _, ok := nodeByID(rep.Downstream, "N1"); ok {
		t.Errorf("need N1 is upstream of R1, must not appear downstream")
	}
	if rep.Total != 3 {
		t.Errorf("total = %d, want 3", rep.Total)
	}

	// Distance and via of a direct dependant.
	d1, ok := nodeByID(rep.Downstream, "D1")
	if !ok {
		t.Fatalf("D1 missing from downstream")
	}
	if d1.Distance != 1 {
		t.Errorf("D1 distance = %d, want 1", d1.Distance)
	}
	if d1.Via != "satisfies" {
		t.Errorf("D1 via = %q, want satisfies", d1.Via)
	}
	wantPath := []string{"R1", "D1"}
	if len(d1.Path) != 2 || d1.Path[0] != wantPath[0] || d1.Path[1] != wantPath[1] {
		t.Errorf("D1 path = %v, want %v", d1.Path, wantPath)
	}
}

func TestComputeImpact_Upstream(t *testing.T) {
	seed, arts, lnks := impactGraph()
	rep := ComputeImpact(mkExport(arts, lnks), seed.ID, DirectionUpstream)

	if len(rep.Downstream) != 0 {
		t.Fatalf("upstream request should not populate downstream, got %d groups", len(rep.Downstream))
	}
	// What R1 depends on: N1 (derives-from). That's the only outgoing edge.
	if _, ok := nodeByID(rep.Upstream, "N1"); !ok {
		t.Errorf("need N1 should be upstream of R1")
	}
	if _, ok := nodeByID(rep.Upstream, "D1"); ok {
		t.Errorf("D1 depends on R1, must not be upstream of R1")
	}
	if rep.Total != 1 {
		t.Errorf("total = %d, want 1 (only N1)", rep.Total)
	}
	n1, _ := nodeByID(rep.Upstream, "N1")
	if n1.Via != "derives-from" || n1.Distance != 1 {
		t.Errorf("N1 via=%q distance=%d, want derives-from/1", n1.Via, n1.Distance)
	}
}

func TestComputeImpact_Both(t *testing.T) {
	seed, arts, lnks := impactGraph()
	rep := ComputeImpact(mkExport(arts, lnks), seed.ID, DirectionBoth)

	if len(rep.Downstream) == 0 || len(rep.Upstream) == 0 {
		t.Fatalf("both should populate downstream (%d) and upstream (%d)", len(rep.Downstream), len(rep.Upstream))
	}
	// Distinct affected set across both directions: R2, D1, T1 (down) + N1 (up) = 4.
	if rep.Total != 4 {
		t.Errorf("total = %d, want 4", rep.Total)
	}
}

func TestComputeImpact_DefaultsToBoth(t *testing.T) {
	seed, arts, lnks := impactGraph()
	rep := ComputeImpact(mkExport(arts, lnks), seed.ID, "sideways")
	if rep.Direction != DirectionBoth {
		t.Errorf("unknown direction should default to both, got %q", rep.Direction)
	}
}

func TestComputeImpact_CycleGuard(t *testing.T) {
	// A -> B -> C -> A forms a cycle via generic "relates-to" links. The
	// visited set must stop the walk rather than loop forever.
	a := mkArtifact("A", artifacts.TypeRequirement, "A", nil)
	b := mkArtifact("B", artifacts.TypeRequirement, "B", nil)
	c := mkArtifact("C", artifacts.TypeRequirement, "C", nil)
	lnks := []*links.Link{
		mkLink("A", "B", "relates-to"),
		mkLink("B", "C", "relates-to"),
		mkLink("C", "A", "relates-to"),
	}
	rep := ComputeImpact(mkExport([]*artifacts.Artifact{a, b, c}, lnks), "A", DirectionUpstream)

	// Upstream of A follows From->To: A->B->C, then C->A closes the cycle and A
	// is already visited (the seed). So B and C are reached, A is not re-added.
	if rep.Total != 2 {
		t.Fatalf("cycle walk total = %d, want 2 (B,C)", rep.Total)
	}
	if _, ok := nodeByID(rep.Upstream, "A"); ok {
		t.Errorf("seed A must not appear in its own impact set")
	}
	bNode, ok := nodeByID(rep.Upstream, "B")
	if !ok || bNode.Distance != 1 {
		t.Errorf("B should be at distance 1, got %+v (present=%v)", bNode, ok)
	}
	cNode, ok := nodeByID(rep.Upstream, "C")
	if !ok || cNode.Distance != 2 {
		t.Errorf("C should be at distance 2, got %+v (present=%v)", cNode, ok)
	}
}

func TestComputeImpact_Grouping(t *testing.T) {
	// Two test cases both verify R1 — they must land in one test-case group
	// with Count 2, and the group order must follow the traceability spine.
	seed, arts, lnks := impactGraph()
	arts = append(arts, mkArtifact("T2", artifacts.TypeTestCase, "Test 2", nil))
	lnks = append(lnks, mkLink("T2", "R1", "verifies"))

	rep := ComputeImpact(mkExport(arts, lnks), seed.ID, DirectionDownstream)

	var tcGroup *ImpactGroup
	for i := range rep.Downstream {
		if rep.Downstream[i].Type == artifacts.TypeTestCase {
			tcGroup = &rep.Downstream[i]
		}
	}
	if tcGroup == nil {
		t.Fatalf("no test-case group in downstream")
	}
	if tcGroup.Count != 2 || len(tcGroup.Nodes) != 2 {
		t.Errorf("test-case group count = %d / %d nodes, want 2/2", tcGroup.Count, len(tcGroup.Nodes))
	}
	// Spine order: requirement (0) before design-item (2) before test-case (3).
	rank := map[string]int{}
	for i, g := range rep.Downstream {
		rank[g.Type] = i
	}
	if rank[artifacts.TypeRequirement] > rank[artifacts.TypeDesignItem] ||
		rank[artifacts.TypeDesignItem] > rank[artifacts.TypeTestCase] {
		t.Errorf("group order not on spine: %v", rank)
	}
}

func TestComputeImpact_UnknownSeedHasEmptySets(t *testing.T) {
	_, arts, lnks := impactGraph()
	rep := ComputeImpact(mkExport(arts, lnks), "does-not-exist", DirectionBoth)
	if rep.Total != 0 || len(rep.Downstream) != 0 || len(rep.Upstream) != 0 {
		t.Errorf("unknown seed should yield an empty report, got total=%d", rep.Total)
	}
}
