// The checks a link must pass before a write makes it: the link rules, the
// flow-down gate and the roles a Policy asks.

package traceability

import (
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/members"
)

// CheckRules is the link rules' answer for a link of linkType from `from` to
// `to`: links.ValidateLinkType's refusal, or nil. A link with an end the
// caller has not resolved is not checked: a link to a pending proposal's
// ref is checked when the proposal is applied.
func CheckRules(linkType string, from, to End) error {
	if !from.Known() || !to.Known() {
		return nil
	}
	return links.ValidateLinkType(linkType, from.Type, to.Type)
}

// CheckLink is every check a write asks of a link it makes in its base
// project, in order: the link rules (CheckRules), then the flow-down gate
// and the roles (CheckAccess). A write whose own guards come between the two
// (POST /api/v1/links asks the base project's role there) calls them one by
// one.
func (s *Service) CheckLink(p Policy, base string, from, to End, linkType string) error {
	if err := CheckRules(linkType, from, to); err != nil {
		return err
	}
	return s.CheckAccess(p, base, from, to, linkType)
}

// CheckAccess is the flow-down gate (CheckFlowDown), then the role p asks on
// the project of each end outside base, the source's first: an
// *AccessError for the first the caller lacks. An end in base, or one not
// resolved, asks nothing.
func (s *Service) CheckAccess(p Policy, base string, from, to End, linkType string) error {
	if err := s.CheckFlowDown(p, base, linkType); err != nil {
		return err
	}
	if err := s.reach(p, base, from, false, linkType); err != nil {
		return err
	}
	return s.reach(p, base, to, true, linkType)
}

// CheckFlowDown refuses a refines link with ErrFlowDownClosed while the
// flow-down feature is closed to the caller in base's workspace, when p
// requires the feature; any other link, or a Policy that does not require
// it, passes without asking.
func (s *Service) CheckFlowDown(p Policy, base, linkType string) error {
	if p.RequireFlowDownFeature && linkType == links.TypeRefines && !s.flowDownOn(base) {
		return ErrFlowDownClosed
	}
	return nil
}

// FlowDownViewer reports whether p lets a link of linkType take viewer
// rights on its target's project instead of editor rights: a refines link,
// under AsksEditorOrFlowDownViewer, while the flow-down feature is on in
// base's workspace (REQ-145). It asks the feature only for such a link.
func (s *Service) FlowDownViewer(p Policy, base, linkType string) bool {
	return p.TargetRole == AsksEditorOrFlowDownViewer && linkType == links.TypeRefines && s.flowDownOn(base)
}

// reach is the role p asks on the project of one end, the target when
// target is true: nil when the end is in base, not resolved, or p asks no
// role, else an *AccessError unless the caller holds the role.
func (s *Service) reach(p Policy, base string, end End, target bool, linkType string) error {
	if !end.Known() || end.ProjectID == base || p.TargetRole == AsksNoRole {
		return nil
	}
	role, which := members.RoleEditor, "source"
	if target {
		which = "target"
		if s.FlowDownViewer(p, base, linkType) {
			role = members.RoleViewer
		}
	}
	if s.Access == nil || !s.Access.HasProjectRole(end.ProjectID, role) {
		return &AccessError{End: which, Role: role}
	}
	return nil
}

// flowDownOn asks Access whether the flow-down feature is on in the
// project's workspace, once per project for the service's caller. With no
// Access it is closed.
func (s *Service) flowDownOn(projectID string) bool {
	if on, asked := s.flowDown[projectID]; asked {
		return on
	}
	on := s.Access != nil && s.Access.FlowDownEnabled(projectID)
	if s.flowDown == nil {
		s.flowDown = map[string]bool{}
	}
	s.flowDown[projectID] = on
	return on
}
