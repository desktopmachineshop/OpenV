package baselines

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
)

func artifact(id, typ, title, body string, parentID *string, status string) *artifacts.Artifact {
	a := &artifacts.Artifact{
		ID:       id,
		Type:     typ,
		Title:    title,
		Body:     body,
		ParentID: parentID,
	}
	if status != "" {
		a.Attributes = map[string]interface{}{"status": status}
	}
	return a
}

func link(from, to, typ string) *links.Link {
	return &links.Link{ID: from + "-" + to, FromID: from, ToID: to, Type: typ}
}

func export(arts []*artifacts.Artifact, lns []*links.Link) *exports.ProjectExport {
	return &exports.ProjectExport{Artifacts: arts, Links: lns}
}

func strPtr(s string) *string { return &s }

func TestDiffAddedRemoved(t *testing.T) {
	base := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "Kept", "body", nil, ""),
		artifact("a2", "requirement", "Removed req", "body", nil, ""),
	}, nil)
	target := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "Kept", "body", nil, ""),
		artifact("a3", "test-case", "Added test", "body", nil, ""),
	}, nil)

	result := Diff(base, target)

	if len(result.Added) != 1 || result.Added[0].ID != "a3" {
		t.Fatalf("expected a3 added, got %+v", result.Added)
	}
	if result.Added[0].Type != "test-case" || result.Added[0].Title != "Added test" {
		t.Errorf("added entry lost fields: %+v", result.Added[0])
	}
	if len(result.Removed) != 1 || result.Removed[0].ID != "a2" {
		t.Fatalf("expected a2 removed, got %+v", result.Removed)
	}
	if len(result.Modified) != 0 {
		t.Errorf("unchanged artifact must not appear as modified: %+v", result.Modified)
	}
}

func TestDiffUnchangedIgnored(t *testing.T) {
	arts := []*artifacts.Artifact{
		artifact("a1", "requirement", "Same", "same body", strPtr("p1"), "approved"),
	}
	lns := []*links.Link{link("a1", "p1", "satisfies")}

	result := Diff(export(arts, lns), export(arts, lns))

	if len(result.Added)+len(result.Removed)+len(result.Modified)+
		len(result.LinksAdded)+len(result.LinksRemoved) != 0 {
		t.Fatalf("identical snapshots must diff empty, got %+v", result)
	}
	// Slices must be non-nil so the JSON encodes as [].
	if result.Added == nil || result.Removed == nil || result.Modified == nil ||
		result.LinksAdded == nil || result.LinksRemoved == nil {
		t.Fatal("diff slices must be non-nil")
	}
}

func TestDiffModifiedFieldFlags(t *testing.T) {
	base := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "Old title", "old body", nil, "draft"),
		artifact("a2", "requirement", "Reparented", "body", nil, ""),
		artifact("a3", "requirement", "Retyped", "body", nil, ""),
	}, nil)
	target := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "New title", "new body", nil, "approved"),
		artifact("a2", "requirement", "Reparented", "body", strPtr("a1"), ""),
		artifact("a3", "hazard", "Retyped", "body", nil, ""),
	}, nil)

	result := Diff(base, target)

	if len(result.Modified) != 3 {
		t.Fatalf("expected 3 modified, got %+v", result.Modified)
	}
	byID := map[string]ModifiedArtifact{}
	for _, m := range result.Modified {
		byID[m.ID] = m
	}

	m1 := byID["a1"]
	if !m1.TitleChanged || !m1.BodyChanged || !m1.StatusChanged {
		t.Errorf("a1 flags wrong: %+v", m1)
	}
	if m1.TypeChanged || m1.ParentChanged {
		t.Errorf("a1 must not flag type/parent: %+v", m1)
	}
	if m1.OldTitle != "Old title" || m1.NewTitle != "New title" {
		t.Errorf("a1 old/new title wrong: %+v", m1)
	}

	m2 := byID["a2"]
	if !m2.ParentChanged || m2.TitleChanged || m2.BodyChanged || m2.TypeChanged || m2.StatusChanged {
		t.Errorf("a2 must flag only parent: %+v", m2)
	}

	m3 := byID["a3"]
	if !m3.TypeChanged || m3.ParentChanged || m3.TitleChanged {
		t.Errorf("a3 must flag type: %+v", m3)
	}
	if m3.Type != "hazard" {
		t.Errorf("modified entry must carry target-side type, got %q", m3.Type)
	}
}

// TestDiffStatusColumnFirstAndNormalized covers issue #174: the diff must read
// status the way the CSV export does — first-class column with legacy-mirror
// fallback — and normalize it, so that snapshot-refresh auto-versions (which
// populate the Status column) and legacy "in-review" spellings do not diff as a
// false status_changed against an unchanged attribute mirror.
func TestDiffStatusColumnFirstAndNormalized(t *testing.T) {
	// withColumn builds an artifact whose first-class Status column is set,
	// optionally with a diverging legacy mirror in Attributes.
	withColumn := func(id, column, mirror string) *artifacts.Artifact {
		a := &artifacts.Artifact{ID: id, Type: "requirement", Title: "T", Body: "b", Status: column}
		if mirror != "" {
			a.Attributes = map[string]interface{}{"status": mirror}
		}
		return a
	}

	t.Run("column read in preference to stale mirror", func(t *testing.T) {
		// Base carries only the legacy mirror ("approved"); target is a
		// snapshot-refresh version that populated the Status column with the
		// same value while carrying a stale/empty mirror. Same status: no diff.
		base := export([]*artifacts.Artifact{artifact("a1", "requirement", "T", "b", nil, "approved")}, nil)
		target := export([]*artifacts.Artifact{withColumn("a1", "approved", "")}, nil)

		result := Diff(base, target)
		if len(result.Modified) != 0 {
			t.Fatalf("column-vs-mirror same status must not diff, got %+v", result.Modified)
		}
	})

	t.Run("legacy in-review mirror equals normalized column", func(t *testing.T) {
		base := export([]*artifacts.Artifact{artifact("a1", "requirement", "T", "b", nil, "in-review")}, nil)
		target := export([]*artifacts.Artifact{withColumn("a1", "in_review", "")}, nil)

		result := Diff(base, target)
		if len(result.Modified) != 0 {
			t.Fatalf("legacy in-review mirror must normalize equal to in_review column, got %+v", result.Modified)
		}
	})

	t.Run("empty and draft are equal after normalization", func(t *testing.T) {
		base := export([]*artifacts.Artifact{artifact("a1", "requirement", "T", "b", nil, "")}, nil)
		target := export([]*artifacts.Artifact{artifact("a1", "requirement", "T", "b", nil, "draft")}, nil)

		result := Diff(base, target)
		if len(result.Modified) != 0 {
			t.Fatalf("empty status must normalize equal to draft, got %+v", result.Modified)
		}
	})

	t.Run("genuine status change still flagged", func(t *testing.T) {
		base := export([]*artifacts.Artifact{withColumn("a1", "draft", "")}, nil)
		target := export([]*artifacts.Artifact{withColumn("a1", "approved", "")}, nil)

		result := Diff(base, target)
		if len(result.Modified) != 1 || !result.Modified[0].StatusChanged {
			t.Fatalf("draft->approved must flag status_changed, got %+v", result.Modified)
		}
	})
}

func TestDiffParentNilVsEmptyEqual(t *testing.T) {
	base := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "T", "b", nil, ""),
	}, nil)
	target := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "T", "b", strPtr(""), ""),
	}, nil)

	result := Diff(base, target)
	if len(result.Modified) != 0 {
		t.Fatalf("nil parent and empty parent must compare equal, got %+v", result.Modified)
	}
}

func TestDiffLinks(t *testing.T) {
	arts := []*artifacts.Artifact{
		artifact("a1", "requirement", "Req one", "b", nil, ""),
		artifact("a2", "test-case", "Test one", "b", nil, ""),
		artifact("a3", "design-item", "Design one", "b", nil, ""),
	}
	base := export(arts, []*links.Link{
		link("a2", "a1", "verifies"),
		link("a3", "a1", "satisfies"),
	})
	target := export(arts, []*links.Link{
		link("a2", "a1", "verifies"), // unchanged
		link("a3", "a1", "implements"),
	})

	result := Diff(base, target)

	if len(result.LinksAdded) != 1 {
		t.Fatalf("expected 1 link added, got %+v", result.LinksAdded)
	}
	added := result.LinksAdded[0]
	if added.FromID != "a3" || added.ToID != "a1" || added.Type != "implements" {
		t.Errorf("link added wrong: %+v", added)
	}
	if added.FromTitle != "Design one" || added.ToTitle != "Req one" {
		t.Errorf("link titles not resolved: %+v", added)
	}
	if len(result.LinksRemoved) != 1 || result.LinksRemoved[0].Type != "satisfies" {
		t.Fatalf("expected satisfies link removed, got %+v", result.LinksRemoved)
	}
}

func TestDiffLinkRecreatedWithNewIDUnchanged(t *testing.T) {
	arts := []*artifacts.Artifact{
		artifact("a1", "requirement", "Req", "b", nil, ""),
		artifact("a2", "test-case", "Test", "b", nil, ""),
	}
	base := export(arts, []*links.Link{
		{ID: "row-1", FromID: "a2", ToID: "a1", Type: "verifies"},
	})
	target := export(arts, []*links.Link{
		{ID: "row-2", FromID: "a2", ToID: "a1", Type: "verifies"},
	})

	result := Diff(base, target)
	if len(result.LinksAdded) != 0 || len(result.LinksRemoved) != 0 {
		t.Fatalf("same (from,to,type) with a new row ID must not diff, got %+v", result)
	}
}

func TestDiffLinkTitleForRemovedArtifact(t *testing.T) {
	base := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "Req", "b", nil, ""),
		artifact("a2", "test-case", "Gone test", "b", nil, ""),
	}, []*links.Link{link("a2", "a1", "verifies")})
	target := export([]*artifacts.Artifact{
		artifact("a1", "requirement", "Req", "b", nil, ""),
	}, nil)

	result := Diff(base, target)
	if len(result.LinksRemoved) != 1 {
		t.Fatalf("expected 1 link removed, got %+v", result.LinksRemoved)
	}
	if result.LinksRemoved[0].FromTitle != "Gone test" {
		t.Errorf("removed link must resolve title from base snapshot: %+v", result.LinksRemoved[0])
	}
}

func TestDiffSortedOutput(t *testing.T) {
	base := export(nil, nil)
	target := export([]*artifacts.Artifact{
		artifact("z9", "requirement", "Bravo", "b", nil, ""),
		artifact("a1", "requirement", "Alpha", "b", nil, ""),
		artifact("m5", "requirement", "Charlie", "b", nil, ""),
	}, nil)

	result := Diff(base, target)
	if len(result.Added) != 3 {
		t.Fatalf("expected 3 added, got %+v", result.Added)
	}
	titles := []string{result.Added[0].Title, result.Added[1].Title, result.Added[2].Title}
	if titles[0] != "Alpha" || titles[1] != "Bravo" || titles[2] != "Charlie" {
		t.Errorf("added entries must be sorted by title, got %v", titles)
	}
}

func TestDiffNilEntriesIgnored(t *testing.T) {
	base := export([]*artifacts.Artifact{nil}, []*links.Link{nil})
	target := export([]*artifacts.Artifact{nil, artifact("a1", "requirement", "T", "b", nil, "")}, []*links.Link{nil})

	result := Diff(base, target)
	if len(result.Added) != 1 || result.Added[0].ID != "a1" {
		t.Fatalf("nil snapshot entries must be skipped, got %+v", result)
	}
}
