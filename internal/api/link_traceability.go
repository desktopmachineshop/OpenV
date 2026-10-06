package api

import (
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/traceability"
)

// The four paths that write traceability links call traceability.Service,
// each with a Policy of its own (refactor plan X11b). The policies say
// where the paths differ, as they do today (quirk Q3; X11a's
// traceability_paths_test.go pins each): converging two is a product
// change, not an edit here. The require* guards of POST and DELETE
// /api/v1/links stay in their handlers, where S2's route_guards.txt reads
// them; their policies choose the target's role and the flow-down gate.

// linkCreatePolicy is POST /api/v1/links': a link the rules refuse answers
// 400; a refines link needs the flow-down feature, and takes viewer rights
// on its target's project; link.created is published as the caller.
func linkCreatePolicy(r *http.Request) traceability.Policy {
	return traceability.Policy{
		OnInvalid:              traceability.Refuse,
		RequireFlowDownFeature: true,
		TargetRole:             traceability.AsksEditorOrFlowDownViewer,
		EmitEvents:             true,
		Actor:                  Actor(r),
	}
}

// linkDeletePolicy is DELETE /api/v1/links/{id}'s: no feature gate refuses
// a delete, and a refines link takes viewer rights on its target's project
// only while the feature is on (#379 bug 194); link.deleted is published as
// the caller.
func linkDeletePolicy(r *http.Request) traceability.Policy {
	return traceability.Policy{
		OnInvalid:              traceability.Refuse,
		RequireFlowDownFeature: false,
		TargetRole:             traceability.AsksEditorOrFlowDownViewer,
		EmitEvents:             true,
		Actor:                  Actor(r),
	}
}

// managedEditPolicy is the managed link edits' of PUT
// /api/v1/artifacts/{id} (Q3): an add or a removal the edit cannot make is
// skipped and the update still answers 200; no feature gate; editor rights
// on both ends' projects whatever the type; no link event, only the
// update's artifact.updated.
func managedEditPolicy(r *http.Request) traceability.Policy {
	return traceability.Policy{
		OnInvalid:              traceability.Skip,
		RequireFlowDownFeature: false,
		TargetRole:             traceability.AsksEditor,
		EmitEvents:             false,
		Actor:                  Actor(r),
	}
}

// appliedLinkPolicy is the proposal appliers': a link the rules refuse at
// apply time fails the proposal (apply_failed); no gate and no role, since
// the run reached only its own project when it proposed (unreachable through
// today's routes otherwise, X11a); the events name the system.
var appliedLinkPolicy = traceability.Policy{
	OnInvalid:              traceability.Refuse,
	RequireFlowDownFeature: false,
	TargetRole:             traceability.AsksNoRole,
	EmitEvents:             true,
	Actor:                  events.ActorSystem,
}

// guidedDraftPolicy is the guided drafts' (POST
// /api/v1/guided-sessions/{id}/drafts): a link the draft cannot have is
// skipped and the draft is still made; no feature gate; editor rights on
// the target's project when it is another; no event. Its links are written
// by internal/domain/guided, which versions nothing.
var guidedDraftPolicy = traceability.Policy{
	OnInvalid:              traceability.Skip,
	RequireFlowDownFeature: false,
	TargetRole:             traceability.AsksEditor,
	EmitEvents:             false,
}

// traceabilityFor is the link write service over the handler's services,
// for the caller of r: its roles and features answer the policies' questions,
// and its events are stamped as publish stamps them.
func (h *Handler) traceabilityFor(r *http.Request) *traceability.Service {
	return &traceability.Service{
		Artifacts: h.ArtifactService,
		Links:     h.LinkService,
		Notes:     h.ChatterService,
		Access:    requestAccess{h: h, r: r},
		Publish: func(eventType, projectID, entityID, actor string, payload map[string]interface{}) {
			h.publishAs(r, actor, eventType, projectID, entityID, payload)
		},
	}
}

// appliedTraceability is the link write service for the proposal appliers,
// which run with no request: no caller to ask, and events stamped as
// publishApplied stamps them.
func (h *Handler) appliedTraceability() *traceability.Service {
	return &traceability.Service{
		Artifacts: h.ArtifactService,
		Links:     h.LinkService,
		Notes:     h.ChatterService,
		Publish:   h.publishAppliedAs,
	}
}

// requestAccess answers traceability's questions for the caller of r, as
// hasProjectRole and projectFeatureEnabled answer them, writing no
// response.
type requestAccess struct {
	h *Handler
	r *http.Request
}

func (a requestAccess) HasProjectRole(projectID, role string) bool {
	return a.h.hasProjectRole(a.r, projectID, role)
}

func (a requestAccess) FlowDownEnabled(projectID string) bool {
	return a.h.projectFeatureEnabled(a.r, projectID, release.FeatureFlowDown)
}
