//go:build unix

package main

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/orchestration"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// TestHandoffReach pins who a crew's hand-off card may go to (handoffReach,
// OpenV REQ-23 and REQ-81), on the real stores, in a database of its own
// (OPENV_TEST_DATABASE_URL; skipped when unset): an admin of the project's
// workspace with no role in the project, a member with a direct role and a
// member through a people team are allowed; a member with no role is refused
// as having none (ReachNoRole); an account outside the workspace, a platform
// admin outside it, and a former member who keeps a direct grant or a people
// team's grant are refused as outside the workspace (ReachNotInWorkspace),
// the reason the hand-off then gives, since no role in the project lets them
// in; a project no row has is refused with the lookup's error. The S5d tour
// covers the wiring in main.go: a member with no role refused, a viewer
// handed the card.
func TestHandoffReach(t *testing.T) {
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := postgres.Migrate(conn); err != nil {
		t.Fatal(err)
	}

	userRepo := postgres.NewUserRepository(conn)
	orgRepo := postgres.NewOrgRepository(conn)
	projectService := projects.NewService(postgres.NewProjectRepository(conn))
	orgService := orgs.NewDefaultService(orgRepo)
	teamService := orgs.NewTeamService(orgRepo, orgService)
	memberService := members.NewDefaultService(postgres.NewMemberRepository(conn))
	reach := handoffReach(projectService, orgService, memberService)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	account := func(name string, platformAdmin bool) string {
		t.Helper()
		id := uuid.NewString()
		must(userRepo.SaveUser(&users.User{ID: id, Email: name + "@example.test", Name: name,
			AuthProvider: "local", IsAdmin: platformAdmin, CreatedAt: now, UpdatedAt: now}))
		return id
	}

	owner := account("owner", false)
	org, err := orgService.CreateOrg("W", orgs.TypeCompany, owner)
	must(err)
	project := &projects.Project{ID: uuid.NewString(), OrgID: org.ID, Name: "P", CreatedAt: now, UpdatedAt: now}
	must(projectService.CreateProject(project))
	team, err := teamService.CreateTeam(org.ID, "Reviewers", "", &owner)
	must(err)
	must(memberService.GrantTeam(project.ID, team.ID, members.RoleViewer))

	member := func(name, role string) string {
		t.Helper()
		id := account(name, false)
		must(orgService.AddMember(org.ID, id, role))
		return id
	}
	admin := member("admin", orgs.RoleAdmin)
	viewer := member("viewer", orgs.RoleMember)
	must(memberService.AddMember(project.ID, viewer, members.RoleViewer))
	teamMember := member("team-member", orgs.RoleMember)
	must(teamService.AddTeamMember(team.ID, teamMember))
	noRole := member("no-role", orgs.RoleMember)
	stranger := account("stranger", false)
	platformAdmin := account("platform-admin", true)
	// Removal from the workspace takes neither a direct project grant nor a
	// people team's with it, so their own role in P survives; the hand-off
	// must refuse them anyway.
	leftWithGrant := member("left-with-grant", orgs.RoleMember)
	must(memberService.AddMember(project.ID, leftWithGrant, members.RoleEditor))
	must(orgService.RemoveMember(org.ID, leftWithGrant))
	leftWithTeam := member("left-with-team", orgs.RoleMember)
	must(teamService.AddTeamMember(team.ID, leftWithTeam))
	must(orgService.RemoveMember(org.ID, leftWithTeam))
	for _, id := range []string{leftWithGrant, leftWithTeam} {
		if role, err := memberService.EffectiveRole(project.ID, id); err != nil || role == "" {
			t.Fatalf("a former member's role in P = %q, %v; want the grant kept, so the case tests the workspace check", role, err)
		}
	}

	for _, c := range []struct {
		name, project, user string
		want                orchestration.Reach
	}{
		{"a workspace admin with no role in the project", project.ID, admin, orchestration.ReachAllowed},
		{"a member with a direct role", project.ID, viewer, orchestration.ReachAllowed},
		{"a member through a people team's grant", project.ID, teamMember, orchestration.ReachAllowed},
		{"a member with no role in the project", project.ID, noRole, orchestration.ReachNoRole},
		{"an account outside the workspace", project.ID, stranger, orchestration.ReachNotInWorkspace},
		{"a platform admin outside the workspace", project.ID, platformAdmin, orchestration.ReachNotInWorkspace},
		{"a former member who keeps a direct grant", project.ID, leftWithGrant, orchestration.ReachNotInWorkspace},
		{"a former member who keeps a people team's grant", project.ID, leftWithTeam, orchestration.ReachNotInWorkspace},
	} {
		got, err := reach(c.project, c.user)
		if err != nil {
			t.Errorf("%s: reach error %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: reach = %v, want %v", c.name, got, c.want)
		}
	}
	// A project no row has: the lookup's error, which the hooks log and
	// refuse on, and never a yes.
	if got, err := reach(uuid.NewString(), admin); got == orchestration.ReachAllowed || err == nil {
		t.Errorf("a project no row has: reach = %v, %v; want a refusal and the lookup's error", got, err)
	}
}
