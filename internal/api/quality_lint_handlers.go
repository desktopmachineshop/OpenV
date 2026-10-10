package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/quality"
)

// registerQualityLintRoutes wires requirement quality linting (issue #217),
// of a whole project and of one artifact. The rule sets it judges against
// come from registerProjectQualityRuleRoutes, which registerSuiteRoutes
// calls next.
func (h *Handler) registerQualityLintRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/quality", h.GetProjectQuality).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}/quality", h.GetArtifactQuality).Methods("GET")
}

// GetProjectQuality lints every requirement-type artifact in the project's
// live export (or a ?baseline_id= snapshot) and returns per-artifact scores and
// findings. Viewer role, mirroring the V&V coverage endpoint.
func (h *Handler) GetProjectQuality(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	export, err := h.projectExport(projectID, r.URL.Query().Get("baseline_id"))
	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
		respondInternal(w, r, "failed to export project", err)
		return
	}
	writeJSONOK(w, quality.LintProject(export, h.qualityRuleSetFor(projectID)))
}

// GetArtifactQuality lints a single artifact. It returns 400 for a type the
// linter does not judge (headings, test cases, etc.) so the caller gets a clear
// signal rather than an empty score.
func (h *Handler) GetArtifactQuality(w http.ResponseWriter, r *http.Request) {
	artifact, err := h.ArtifactService.GetArtifact(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "artifact not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, artifact.ProjectID, members.RoleViewer, missing("artifact not found")) {
		return
	}
	if !quality.IsRequirementType(artifact.Type) {
		writeJSONError(w, http.StatusBadRequest, "artifact type "+artifact.Type+" is not quality-linted")
		return
	}
	rs := h.qualityRuleSetFor(artifact.ProjectID)
	refs, known := h.linkedRefsFor(artifact.ID)
	writeJSONOK(w, struct {
		quality.ArtifactScore
		RuleSet quality.RuleSet `json:"rule_set"`
	}{
		ArtifactScore: quality.LintArtifact(artifact, rs,
			quality.Context{LinkedRefs: refs, LinksKnown: known}),
		RuleSet: rs,
	})
}

// linkedRefsFor collects the refs an artifact is linked to, in either
// direction, by the rule the project report applies (quality.LinkedRefs),
// reading each link's other end. ok is false when the links could not be
// read, which the linter treats as "not checked" rather than "nothing is
// linked" — the difference decides whether every citation in the artifact
// is flagged as untraceable.
func (h *Handler) linkedRefsFor(artifactID string) (map[string]bool, bool) {
	if h.LinkService == nil || h.ArtifactService == nil {
		return nil, false
	}
	outgoing, err := h.LinkService.GetLinksFrom(artifactID)
	if err != nil {
		return nil, false
	}
	incoming, err := h.LinkService.GetLinksTo(artifactID)
	if err != nil {
		return nil, false
	}
	var ends []quality.LinkEnds
	for _, l := range append(outgoing, incoming...) {
		if l != nil {
			ends = append(ends, quality.LinkEnds{FromID: l.FromID, ToID: l.ToID})
		}
	}
	return quality.LinkedRefs(ends, func(id string) string {
		// A counterpart that cannot be read names no ref.
		if id == artifactID {
			return ""
		}
		if a, err := h.ArtifactService.GetArtifact(id); err == nil && a != nil {
			return a.Ref
		}
		return ""
	})[artifactID], true
}
