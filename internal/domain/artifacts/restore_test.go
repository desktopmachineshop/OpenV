package artifacts

import (
	"errors"
	"testing"
)

// restoreFixture is artifact art-1 at version 3, of type current and ref
// ref, whose version 1 was of type restored.
func restoreFixture(t *testing.T, current, ref, restored string) (*DefaultService, *fakeSuspectRepo) {
	t.Helper()
	svc, repo, _, base := newUpdateFixture(t)
	base.Type, base.Ref = current, ref
	repo.versions = []*Artifact{
		{ID: base.ID, ProjectID: base.ProjectID, Type: current, Ref: ref, Title: "Now", Version: 3},
		{ID: base.ID, ProjectID: base.ProjectID, Type: current, Ref: ref, Title: "Then", Version: 2},
		{ID: base.ID, ProjectID: base.ProjectID, Type: restored, Title: "First", Version: 1},
	}
	return svc, repo
}

// A restore keeps the artifact's ref (REQ-4): a ref is the artifact's, not a
// version's, so the version written carries it and the repository mints
// nothing. It carried none, so every restore drew the next number.
func TestARestoreKeepsTheArtifactsRef(t *testing.T) {
	for _, c := range []struct{ name, typ, ref string }{
		{"a requirement's own ref", TypeRequirement, "REQ-7"},
		{"a ref an import kept under a prefix of its own", TypeRequirement, "SYS-5"},
	} {
		svc, repo := restoreFixture(t, c.typ, c.ref, c.typ)
		restored, err := svc.RestoreArtifactVersion("art-1", 1)
		if err != nil {
			t.Fatalf("%s: RestoreArtifactVersion: %v", c.name, err)
		}
		if restored.Ref != c.ref || repo.updated.Ref != c.ref {
			t.Errorf("%s: the restored version carries ref %q (stored %q), want %q", c.name, restored.Ref, repo.updated.Ref, c.ref)
		}
		if restored.Title != "First" || restored.Version != 4 {
			t.Errorf("%s: restored %q at version %d, want version 1's content as version 4", c.name, restored.Title, restored.Version)
		}
	}
}

// A restore that brings back a type whose prefix the ref does not carry
// draws a new ref, as a retype edit does (ref.go): the retired number is
// never reissued, and the ref keeps telling the truth about the type.
func TestARestoreThatChangesThePrefixDrawsANewRef(t *testing.T) {
	svc, repo := restoreFixture(t, TypeRequirement, "REQ-7", TypeHeading)
	if _, err := svc.RestoreArtifactVersion("art-1", 1); err != nil {
		t.Fatalf("RestoreArtifactVersion: %v", err)
	}
	if repo.updated.Type != TypeHeading || repo.updated.Ref != "" {
		t.Errorf("stored type %q ref %q, want a heading with the ref left for the repository to mint", repo.updated.Type, repo.updated.Ref)
	}
}

// A version the artifact never had is ErrVersionNotFound, which the API
// answers 404, and not the artifact's ErrNotFound, which it may not be.
func TestRestoringAVersionTheArtifactNeverHadIsErrVersionNotFound(t *testing.T) {
	svc, repo := restoreFixture(t, TypeRequirement, "REQ-7", TypeRequirement)
	_, err := svc.RestoreArtifactVersion("art-1", 9)
	if !errors.Is(err, ErrVersionNotFound) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrVersionNotFound", err)
	}
	if repo.updated != nil {
		t.Error("a refused restore wrote a version")
	}
}
