package postgres

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// X9a: the one case of link_artifacts_test.go that only the reflection of
// internal/domain/links/link.go:176-179 reaches, an artifact service with no
// GetArtifact method. A typed ArtifactVersions port (X9b) makes such a value
// a compile error, so this file can go with the reflection while
// link_artifacts_test.go stays as it is. The other values the reflection
// takes are pinned by internal/domain/links/artifact_versions_reflect_test.go.

// The link is saved, the create and the update succeed, and no row is
// written for either end.
func TestLinkArtifactsRowsWithNoGetArtifactMethod(t *testing.T) {
	f := newLAFixture(t)
	from := f.artifact("from", artifacts.TypeTestCase, 1)
	to := f.artifact("to", artifacts.TypeRequirement, 3)
	svc := f.unwired()
	svc.SetArtifactService(struct{}{})
	l := f.link(svc, "link", from.ID, to.ID)
	if _, err := svc.GetLink(l.ID); err != nil {
		t.Errorf("the link was not saved: %v", err)
	}
	f.want("CreateLink")
	f.update(svc, l)
	f.want("CreateLink then UpdateLink")
}
