package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// A team change that alters nobody's access says nothing: adding someone
// already in the team, removing someone not in it, re-granting the role the
// team holds, and revoking a team the project does not grant. Each real
// change publishes its one event.
func TestTeamChangesPublishOnlyWhenAccessChanges(t *testing.T) {
	bus := &recordingBus{}
	h := newTestHandler(t, func(h *Handler) {
		h.Bus = bus
		h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
			payloadProject: {ID: payloadProject, OrgID: payloadOrg, Name: "Project"},
		}}
		h.MemberService = &payloadMembers{}
		h.OrgService = &payloadOrgs{fakeMemberOrgs: &fakeMemberOrgs{roles: map[string]string{}}}
		h.OrgTeamService = &payloadTeams{byID: map[string]*orgs.OrgTeam{
			"team-design": {ID: "team-design", OrgID: payloadOrg, Name: "Design",
				Members: []*orgs.Member{{OrgID: payloadOrg, UserID: "u-known"}}},
			"team-old": {ID: "team-old", OrgID: payloadOrg, Name: "Old"},
		}}
	})
	router := mux.NewRouter()
	h.RegisterRoutes(router)
	admin := &users.User{ID: "u-admin", IsAdmin: true}
	send := func(method, path, body string) {
		t.Helper()
		var r *http.Request
		if body == "" {
			r = httptest.NewRequest(method, path, nil)
		} else {
			r = jsonReq(method, path, body, "")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, asUser(r, admin))
		if rec.Code < 200 || rec.Code > 299 {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
	}

	// No change of access: nothing published.
	send(http.MethodPost, "/api/v1/org-teams/team-design/members/u-known", "")
	send(http.MethodDelete, "/api/v1/org-teams/team-design/members/u-stranger", "")
	send(http.MethodPut, "/api/v1/projects/proj-1/team-access", `{"org_team_id":"team-old","role":"`+members.RoleViewer+`"}`)
	send(http.MethodDelete, "/api/v1/projects/proj-1/team-access/team-design", "")
	if len(bus.published) != 0 {
		t.Fatalf("no-op changes published %v", bus.types())
	}

	// Real changes: one event each.
	send(http.MethodPost, "/api/v1/org-teams/team-design/members/u-new", "")
	send(http.MethodDelete, "/api/v1/org-teams/team-design/members/u-known", "")
	send(http.MethodPut, "/api/v1/projects/proj-1/team-access", `{"org_team_id":"team-old","role":"editor"}`)
	send(http.MethodDelete, "/api/v1/projects/proj-1/team-access/team-old", "")
	want := []string{"org.team_member_added", "org.team_member_removed", "project.team_access_granted", "project.team_access_revoked"}
	if got := bus.types(); len(got) != len(want) {
		t.Fatalf("published %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("published %v, want %v", got, want)
			}
		}
	}
}
