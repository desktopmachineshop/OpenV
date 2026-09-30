package main

import (
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/orchestration"
)

// handoffReach is who a crew's hand-off card may go to (OpenV REQ-23,
// REQ-81): an admin of the project's workspace, or a member of that
// workspace with a role in the project, directly or through a people team.
// It is stricter than what a person's own session opens (projectAccess in
// internal/api), the safer choice for a card made on someone's behalf: a
// platform admin who is neither, and someone who has left the workspace but
// keeps a role in the project, are refused too. It names the rule that
// refused: anyone outside the workspace is ReachNotInWorkspace, whatever
// role in the project they kept, and a member with no role ReachNoRole. A
// project no row has, or a check that fails, refuses as well, with the
// error.
func handoffReach(projectService projects.Service, orgService orgs.Service, memberService members.Service) orchestration.ProjectReach {
	return func(projectID, userID string) (orchestration.Reach, error) {
		project, err := projectService.GetProject(projectID)
		if err != nil || project == nil {
			return orchestration.ReachNoRole, err
		}
		switch role, err := orgService.RoleInOrg(project.OrgID, userID); {
		case err != nil:
			return orchestration.ReachNoRole, err
		case role == "":
			return orchestration.ReachNotInWorkspace, nil
		case role == orgs.RoleAdmin:
			return orchestration.ReachAllowed, nil
		}
		role, err := memberService.EffectiveRole(projectID, userID)
		if err != nil || role == "" {
			return orchestration.ReachNoRole, err
		}
		return orchestration.ReachAllowed, nil
	}
}
