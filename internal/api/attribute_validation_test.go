package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// erroringAttributeService fails every EffectiveForProject lookup, standing in
// for a transient attribute-catalog read failure.
type erroringAttributeService struct {
	attributes.Service
}

func (erroringAttributeService) EffectiveForProject(orgID, projectID string) ([]*attributes.Definition, error) {
	return nil, errors.New("catalog read failed: connection reset")
}

// TestValidateArtifactAttributesFailDirection covers issue #246: on a
// definition-lookup failure, update fails OPEN (allows the edit) while create
// fails CLOSED (returns errAttributeDefinitionsUnavailable so the handler can
// answer a sanitized 500), because required-attribute enforcement only matters
// on create.
func TestValidateArtifactAttributesFailDirection(t *testing.T) {
	h := &Handler{
		attributeService: erroringAttributeService{},
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-1": {ID: "proj-1", OrgID: "org-1"},
		}},
	}
	attrs := map[string]interface{}{"priority": "high"}

	// Update: fail open — a transient catalog error must not block a legit edit.
	if err := h.validateArtifactAttributes("proj-1", "requirement", attrs, false); err != nil {
		t.Errorf("update should fail open on lookup error, got %v", err)
	}

	// Create: fail closed — the lookup error surfaces as the sentinel so the
	// handler routes it to a sanitized 500 rather than admitting the artifact.
	err := h.validateArtifactAttributes("proj-1", "requirement", attrs, true)
	if !errors.Is(err, errAttributeDefinitionsUnavailable) {
		t.Fatalf("create should fail closed with errAttributeDefinitionsUnavailable, got %v", err)
	}
	// The sentinel message is sanitized: it must not leak the underlying cause.
	if got := err.Error(); got == "" || strings.Contains(got, "connection reset") {
		t.Errorf("sentinel error leaks internals: %q", got)
	}
}

// TestValidateArtifactAttributesNoServiceNoop: with no attribute service wired,
// both directions are a clean no-op regardless of enforcement.
func TestValidateArtifactAttributesNoServiceNoop(t *testing.T) {
	h := &Handler{}
	if err := h.validateArtifactAttributes("proj-1", "requirement", map[string]interface{}{"a": 1}, true); err != nil {
		t.Errorf("no attribute service should be a no-op, got %v", err)
	}
}
