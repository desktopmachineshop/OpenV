package artifacts

import "testing"

// TestUpdateArtifactClearsRefOnTypeChange: retyping an artifact away from a
// mistaken type must not leave a ref whose prefix lies about what the
// artifact now is. The common case this guards is a heading created by
// accident and immediately retyped to a requirement — the artifact must stop
// showing "HDG-N" once it is a requirement, not keep the heading's ref
// forever.
func TestUpdateArtifactClearsRefOnTypeChange(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	base.Type = TypeHeading
	base.Ref = "HDG-5"
	repo.byID[base.ID] = base

	req := sameContentReq(base)
	req.Type = strPtr(TypeRequirement)

	updated, err := svc.UpdateArtifact(base.ID, req)
	if err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	if updated.Ref != "" {
		t.Errorf("Ref after retyping heading -> requirement = %q, want cleared so the repository mints a fresh REQ- ref", updated.Ref)
	}
	if updated.Type != TypeRequirement {
		t.Errorf("Type = %q, want %q", updated.Type, TypeRequirement)
	}
}

// TestUpdateArtifactKeepsRefWhenTypeUnchanged: an ordinary content edit must
// not disturb the ref — it is only the prefix mismatch from an actual type
// change that should clear it.
func TestUpdateArtifactKeepsRefWhenTypeUnchanged(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	base.Ref = "REQ-3"
	repo.byID[base.ID] = base

	req := sameContentReq(base)
	req.Title = strPtr("Retitled")

	updated, err := svc.UpdateArtifact(base.ID, req)
	if err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	if updated.Ref != "REQ-3" {
		t.Errorf("Ref after title-only edit = %q, want unchanged REQ-3", updated.Ref)
	}
}

// TestUpdateArtifactKeepsRefWhenPrefixUnaffected: a type change that still
// maps to the same ref prefix (e.g. two legacy type names that share a
// derived prefix) must not needlessly burn the existing ref.
func TestUpdateArtifactKeepsRefWhenPrefixUnaffected(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	base.Type = TypeRequirement
	base.Ref = "REQ-3"
	repo.byID[base.ID] = base

	req := sameContentReq(base)
	req.Type = strPtr(TypeRequirement) // same type: no-op change

	updated, err := svc.UpdateArtifact(base.ID, req)
	if err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	if updated.Ref != "REQ-3" {
		t.Errorf("Ref after same-type update = %q, want unchanged REQ-3", updated.Ref)
	}
}

// TestUpdateArtifactLeavesUnparseableRefAlone: an artifact from before refs
// existed (or with an otherwise unparseable ref) has nothing safe to compare
// prefixes against, so a type change must leave it untouched rather than
// guessing.
func TestUpdateArtifactLeavesUnparseableRefAlone(t *testing.T) {
	svc, repo, _, base := newUpdateFixture(t)
	base.Ref = ""
	repo.byID[base.ID] = base

	req := sameContentReq(base)
	req.Type = strPtr(TypeHeading)

	updated, err := svc.UpdateArtifact(base.ID, req)
	if err != nil {
		t.Fatalf("UpdateArtifact: %v", err)
	}
	if updated.Ref != "" {
		t.Errorf("Ref = %q, want still empty", updated.Ref)
	}
}
