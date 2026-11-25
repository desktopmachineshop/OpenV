package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// projectInviteFixture: proj-1 in org-1, owned by u-admin (a workspace
// admin) and u-owner (a workspace member); invitations through the fake.
func projectInviteFixture(t *testing.T, plan string, mailer *testMailer) (*Handler, *fakeInviteService) {
	t.Helper()
	h, _, invites := newRegistrationHandler(t, "")
	h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
		"proj-1": {ID: "proj-1", OrgID: "org-1", Name: "Brakes"},
	}}
	h.OrgService = &fakeOrgService{plan: plan, roles: map[string]map[string]string{
		"org-1": {"u-admin": orgs.RoleAdmin, "u-owner": orgs.RoleMember},
	}}
	h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
		"proj-1": {"u-admin": members.RoleOwner, "u-owner": members.RoleOwner},
	}}
	if mailer != nil {
		h.Mailer = mailer
	}
	return h, invites
}

func postProjectMember(h *Handler, userID, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.AddProjectMember(rec, asUser(muxReq(http.MethodPost, "/api/v1/projects/proj-1/members", body,
		map[string]string{"id": "proj-1"}), &users.User{ID: userID, Name: "Ada Admin", Email: userID + "@example.com"}))
	return rec
}

// A workspace admin who owns the project adds an address with no account:
// the address is invited to the workspace as a member, the invitation
// carries the project at the role chosen, and the mail names the project.
func TestAddingAnAddressWithNoAccountToAProjectInvitesIt(t *testing.T) {
	mailer := newTestMailer()
	h, invites := projectInviteFixture(t, "", mailer)
	rec := postProjectMember(h, "u-admin", `{"email":"pat@example.com","role":"editor"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	var resp invitationResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Emailed || resp.Link == "" {
		t.Errorf("emailed = %v, link = %q; want the link mailed and shown", resp.Emailed, resp.Link)
	}
	if len(invites.created) != 1 || invites.created[0].Role != orgs.RoleMember || invites.created[0].OrgID != "org-1" {
		t.Fatalf("created = %+v, want one member invitation to org-1", invites.created)
	}
	if got := invites.projectsOf(invites.created[0].ID); len(got) != 1 || got[0].ProjectID != "proj-1" || got[0].Role != "editor" {
		t.Fatalf("grants = %+v, want proj-1 as editor", got)
	}
	if len(resp.Invitation.Projects) != 1 {
		t.Errorf("the response does not show the project: %+v", resp.Invitation)
	}
	select {
	case <-mailer.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no invitation mail was sent")
	}
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	if !strings.Contains(mailer.body[0], "the project Project proj-1 in Test Workspace") {
		t.Errorf("mail does not name the project:\n%s", mailer.body[0])
	}
}

// A project owner who is not a workspace admin cannot bring somebody new
// into the workspace, and is told whom to ask.
func TestAProjectOwnerWhoIsNoWorkspaceAdminIsToldToAskOne(t *testing.T) {
	h, invites := projectInviteFixture(t, "", nil)
	wantAnswer(t, postProjectMember(h, "u-owner", `{"email":"pat@example.com","role":"editor"}`),
		http.StatusForbidden, projectInviteNeedsAdmin)
	if len(invites.created) != 0 {
		t.Error("an invitation was made")
	}
}

// Somebody already invited to the workspace as an admin stays invited as
// an admin: adding a project to the invitation never demotes it.
func TestAProjectAddedToAnAdminInvitationKeepsItAdmin(t *testing.T) {
	h, invites := projectInviteFixture(t, "", nil)
	invites.invite("org-1", "pat@example.com", orgs.RoleAdmin)
	rec := postProjectMember(h, "u-admin", `{"email":"pat@example.com","role":"viewer"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(invites.created) != 1 || invites.created[0].Role != orgs.RoleAdmin {
		t.Fatalf("created = %+v, want the admin role kept", invites.created)
	}
}

func TestAProjectInvitationRefusesTheOwnerRoleAndUnknownRoles(t *testing.T) {
	for _, role := range []string{members.RoleOwner, "superuser", ""} {
		h, invites := projectInviteFixture(t, "", nil)
		rec := postProjectMember(h, "u-admin", `{"email":"pat@example.com","role":"`+role+`"}`)
		if rec.Code != http.StatusBadRequest || len(invites.created) != 0 {
			t.Errorf("role %q: status %d, created %d; want 400 and nothing made", role, rec.Code, len(invites.created))
		}
	}
}

// On the stable channel before email-invites, an address with no account is
// refused as before.
func TestAProjectInvitationIsGated(t *testing.T) {
	h, invites := projectInviteFixture(t, orgs.PlanBusiness, nil)
	wantAnswer(t, postProjectMember(h, "u-admin", `{"email":"pat@example.com","role":"editor"}`),
		http.StatusNotFound, "no user with that email — they must sign up first")
	if len(invites.created) != 0 {
		t.Error("a gated invitation was made")
	}
}

// Taking up an invitation that opens a project publishes the project's
// member added, with the joiner as actor, beside the workspace join.
func TestAcceptingAProjectInvitationPublishesTheProjectJoin(t *testing.T) {
	h, _, invites := newRegistrationHandler(t, "")
	bus := &recordingBus{}
	h.Bus = bus
	inv := invites.invite("org-1", "pat@example.com", orgs.RoleMember)
	if _, err := invites.GrantProject(inv.ID, "proj-1", "editor"); err != nil {
		t.Fatal(err)
	}
	acc, err := invites.AcceptTokenForEmail("tok-pat@example.com", "pat@example.com", "u-pat")
	if err != nil {
		t.Fatal(err)
	}
	h.publishInvitationAccepted("u-pat", acc)
	var types []string
	for _, e := range bus.published {
		types = append(types, e.EventType+"@"+e.ProjectID+" by "+e.Actor)
	}
	want := "org.invitation_accepted@ by user:u-pat|project.member_added@proj-1 by user:u-pat"
	if strings.Join(types, "|") != want {
		t.Errorf("published %v, want %s", types, want)
	}

	// Somebody already in the workspace still gets the project, and only that is said.
	bus2 := &recordingBus{}
	h.Bus = bus2
	h.publishInvitationAccepted("u-pat", &invitations.Acceptance{Invitation: inv, AlreadyMember: true,
		Projects: []invitations.ProjectGrant{{ProjectID: "proj-2", Role: "viewer"}}})
	if got := bus2.published; len(got) != 1 || got[0].ProjectID != "proj-2" {
		t.Errorf("already a member: published %+v", got)
	}
}
