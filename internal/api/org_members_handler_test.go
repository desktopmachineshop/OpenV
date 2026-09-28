package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// A workspace's member writes, answered by the real orgs service over an
// in-memory store, so that the refusal the domain decides and the status
// the handler gives it are tested together: the S5c tour found both ends
// out of step (members_teams.json 20 and 111).

// orgMemberStore is one company workspace's memberships, org-1.
type orgMemberStore struct {
	orgs.Repository
	roles   map[string]string
	removed []string
}

func (s *orgMemberStore) FindOrgByID(id string) (*orgs.Org, error) {
	if id != "org-1" {
		return nil, nil
	}
	return &orgs.Org{ID: id, OrgType: orgs.TypeCompany, BilledPlan: orgs.PlanBusiness}, nil
}

func (s *orgMemberStore) MemberRole(orgID, userID string) (string, error) {
	return s.roles[userID], nil
}

func (s *orgMemberStore) CountAdmins(orgID string) (int, error) {
	n := 0
	for _, role := range s.roles {
		if role == orgs.RoleAdmin {
			n++
		}
	}
	return n, nil
}

func (s *orgMemberStore) ListMembers(orgID string) ([]*orgs.Member, error) {
	out := make([]*orgs.Member, 0, len(s.roles))
	for userID, role := range s.roles {
		out = append(out, &orgs.Member{OrgID: orgID, UserID: userID, Role: role})
	}
	return out, nil
}

func (s *orgMemberStore) UpsertMember(orgID, userID, role string) error {
	s.roles[userID] = role
	return nil
}

// RemoveMember answers a delete that finds no row as the postgres store does.
func (s *orgMemberStore) RemoveMember(orgID, userID string) error {
	if _, ok := s.roles[userID]; !ok {
		return orgs.ErrNotMember
	}
	s.removed = append(s.removed, userID)
	delete(s.roles, userID)
	return nil
}

// lostRaceStore is the store as the second of two removals of one member
// sees it: the role read finds the member, and the other removal's delete
// lands before this one's.
type lostRaceStore struct{ *orgMemberStore }

func (s lostRaceStore) RemoveMember(orgID, userID string) error {
	delete(s.roles, userID)
	return s.orgMemberStore.RemoveMember(orgID, userID)
}

func orgMemberFixture() (*Handler, *orgMemberStore, *recordingBus) {
	store := &orgMemberStore{roles: map[string]string{"owner": orgs.RoleAdmin, "m": orgs.RoleMember}}
	bus := &recordingBus{}
	return NewHandler(HandlerDeps{OrgService: orgs.NewDefaultService(store), Bus: bus}), store, bus
}

func orgMemberReq(method, userID, body string) *http.Request {
	return asUser(muxReq(method, "/api/v1/orgs/org-1/members/"+userID, body,
		map[string]string{"id": "org-1", "userId": userID}), &users.User{ID: "owner"})
}

// Demoting the only admin is the refusal it is elsewhere — 400 with the
// domain's words, as the same admin's leaving and the platform admins' last
// demotion are — not a 500, and it changes and publishes nothing (REQ-15).
func TestUpdateOrgMemberRefusesDemotingTheLastAdmin(t *testing.T) {
	h, store, bus := orgMemberFixture()

	w := httptest.NewRecorder()
	h.UpdateOrgMember(w, orgMemberReq(http.MethodPut, "owner", `{"role":"member"}`))
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"cannot demote the last admin of an organization"}`+"\n" {
		t.Fatalf("demoting the only admin: %d %q", w.Code, w.Body.String())
	}
	if store.roles["owner"] != orgs.RoleAdmin || len(bus.published) != 0 {
		t.Fatalf("a refused demotion changed the role to %q or published %v", store.roles["owner"], bus.types())
	}

	// With a second admin, the same demotion goes through.
	w = httptest.NewRecorder()
	h.UpdateOrgMember(w, orgMemberReq(http.MethodPut, "m", `{"role":"admin"}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("promoting m: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.UpdateOrgMember(w, orgMemberReq(http.MethodPut, "owner", `{"role":"member"}`))
	if w.Code != http.StatusNoContent || store.roles["owner"] != orgs.RoleMember {
		t.Fatalf("demoting one of two admins: %d %q", w.Code, w.Body.String())
	}
}

// Removing an id that is no member of the workspace — here one no account
// has — answers as a role change for it does, 400 with ErrNotMember's
// words, and publishes no org.member_removed: nobody left, so no admin is
// told somebody did (REQ-122). A member's removal still publishes it.
func TestRemoveOrgMemberRefusesANonMember(t *testing.T) {
	h, store, bus := orgMemberFixture()

	w := httptest.NewRecorder()
	h.RemoveOrgMember(w, orgMemberReq(http.MethodDelete, "phantom", ""))
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"you are not a member of this organization"}`+"\n" {
		t.Fatalf("removing a non-member: %d %q", w.Code, w.Body.String())
	}
	if len(store.removed) != 0 || len(bus.published) != 0 {
		t.Fatalf("a refused removal deleted %v and published %v", store.removed, bus.types())
	}

	w = httptest.NewRecorder()
	h.RemoveOrgMember(w, orgMemberReq(http.MethodDelete, "m", ""))
	if w.Code != http.StatusNoContent {
		t.Fatalf("removing m: %d %q", w.Code, w.Body.String())
	}
	if types := bus.types(); len(types) != 1 || types[0] != events.OrgMemberRemoved {
		t.Fatalf("removing m published %v, want [org.member_removed]", types)
	}
}

// Of two removals of one member at once — two admins, or a member leaving
// while an admin removes them — both pass the role read, and the one whose
// delete finds the row gone is refused as a non-member's removal is: no
// second 204 and no second org.member_removed for the other admins, nor a
// "you were removed" for someone who left (REQ-122).
func TestRemoveOrgMemberThatLosesARaceIsRefused(t *testing.T) {
	store := &orgMemberStore{roles: map[string]string{"owner": orgs.RoleAdmin, "m": orgs.RoleMember}}
	bus := &recordingBus{}
	h := NewHandler(HandlerDeps{OrgService: orgs.NewDefaultService(lostRaceStore{store}), Bus: bus})

	w := httptest.NewRecorder()
	h.RemoveOrgMember(w, orgMemberReq(http.MethodDelete, "m", ""))
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"you are not a member of this organization"}`+"\n" {
		t.Fatalf("the removal that lost the race: %d %q", w.Code, w.Body.String())
	}
	if len(bus.published) != 0 {
		t.Fatalf("the removal that lost the race published %v", bus.types())
	}
}
