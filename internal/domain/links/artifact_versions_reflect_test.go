package links_test

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// X9a: the branches of the version records (link.go:170-206) that only the
// reflection reaches, because SetArtifactService takes an interface{} and
// finds GetArtifact by name. A typed ArtifactVersions port (X9b) makes each
// of these values a compile error, so this file can go with the reflection;
// the rules that outlive it are in artifact_versions_test.go.

// avNoLookup has no GetArtifact method.
type avNoLookup struct{}

// avOneResult's GetArtifact answers one result, not two.
type avOneResult struct{}

func (avOneResult) GetArtifact(id string) *artifacts.Artifact {
	return &artifacts.Artifact{ID: id, Version: 5}
}

// avNotAnObject's GetArtifact answers a value that is no JSON object.
type avNotAnObject struct{}

func (avNotAnObject) GetArtifact(string) (int, error) { return 5, nil }

// avOtherShape's GetArtifact answers no artifact, but a value with a Version.
type avOtherShape struct{}

func (avOtherShape) GetArtifact(string) (struct{ Version int }, error) {
	return struct{ Version int }{Version: 7}, nil
}

// Whatever the value, the link is saved and the create succeeds; what is
// recorded is what the reflection makes of the value: nothing when it finds
// no GetArtifact(id) with two results whose JSON has a version, and
// otherwise the version it reads, from any type.
func TestLinkVersionRecordsThroughReflection(t *testing.T) {
	for _, tc := range []struct {
		name string
		svc  interface{}
		want []string
	}{
		{"a value with no GetArtifact method", avNoLookup{}, nil},
		{"a string", "artifact service", nil},
		// GetArtifact has a pointer receiver, so the service's value (not a
		// pointer to it) has no such method.
		{"the artifact service by value, not by pointer",
			*artifacts.NewDefaultService(newAVArtifactRepo().at("tc-1", 1).at("req-1", 3)), nil},
		{"a GetArtifact with one result", avOneResult{}, nil},
		{"a GetArtifact whose result is no JSON object", avNotAnObject{}, nil},
		{"a GetArtifact of another type with a Version", avOtherShape{},
			[]string{"link-1 tc-1 7", "link-1 req-1 7"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newAVLinkRepo()
			svc := links.NewDefaultService(repo)
			svc.SetArtifactService(tc.svc)
			avCreate(t, svc, avLink("link-1", "tc-1", "req-1"))
			if _, ok := repo.saved["link-1"]; !ok {
				t.Error("CreateLink did not save the link")
			}
			avWant(t, "CreateLink", repo.recorded, tc.want...)
		})
	}
}
