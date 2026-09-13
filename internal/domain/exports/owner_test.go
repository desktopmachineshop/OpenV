package exports

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

func ownedExport() *ProjectExport {
	return &ProjectExport{
		Artifacts: []*artifacts.Artifact{
			{ID: "h", Type: artifacts.TypeHeading, Attributes: map[string]interface{}{"owner": "Acme"}},
			{ID: "r1", Type: artifacts.TypeRequirement, Attributes: map[string]interface{}{"owner": "Acme"}},
			{ID: "r2", Type: artifacts.TypeRequirement, Attributes: map[string]interface{}{"owner": " Gear Co "}},
			{ID: "r3", Type: artifacts.TypeRequirement, Attributes: map[string]interface{}{"owner": "Gear Co"}},
			{ID: "r4", Type: artifacts.TypeRequirement},
		},
		Links: []*links.Link{
			{ID: "l1", FromID: "r2", ToID: "r1", Type: "decomposes-to"},
			{ID: "l2", FromID: "r3", ToID: "far", Type: "refines"},
		},
		LinkedArtifacts: []*LinkedArtifact{{ID: "far", ProjectID: "plane", ProjectName: "Plane", Ref: "REQ-9", Type: "requirement"}},
	}
}

// TestOwners: counted most first, trimmed, headings and the unowned left out.
func TestOwners(t *testing.T) {
	got := Owners(ownedExport())
	if len(got) != 2 || got[0].Owner != "Gear Co" || got[0].Count != 2 || got[1].Owner != "Acme" || got[1].Count != 1 {
		t.Fatalf("owners = %+v", got)
	}
	if Owners(nil) != nil {
		t.Fatal("nil snapshot has owners")
	}
}

// TestApplyKeepsOneOwnersShare: the owner filter keeps that owner's
// artifacts and the headings, drops a link whose other end went, and keeps a
// link to another project's artifact since the reader can still see it named.
func TestApplyKeepsOneOwnersShare(t *testing.T) {
	out := Apply(ownedExport(), Selection{Owners: []string{"Gear Co"}, IncludeHeadings: true})
	ids := map[string]bool{}
	for _, a := range out.Artifacts {
		ids[a.ID] = true
	}
	if len(ids) != 3 || !ids["h"] || !ids["r2"] || !ids["r3"] {
		t.Fatalf("kept = %v", ids)
	}
	if len(out.Links) != 1 || out.Links[0].ID != "l2" {
		t.Fatalf("links = %+v", out.Links)
	}
	if !(Selection{Owners: []string{"x"}, IncludeHeadings: true}).NarrowsArtifacts() {
		t.Fatal("an owner filter does not count as narrowing")
	}
	if got := (&LinkedArtifact{ProjectName: "Plane", Ref: "REQ-9"}).QualifiedRef(); got != "Plane / REQ-9" {
		t.Fatalf("QualifiedRef = %q", got)
	}
}
