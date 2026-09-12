package notify

import (
	"strings"
	"testing"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// fakeOrgMembers answers a fixed workspace roster. orgMember lives in
// budgets_test.go — the two fan-outs share the same member shape.
type fakeOrgMembers struct {
	list []*orgs.Member
	err  error
}

func (f *fakeOrgMembers) ListMembers(orgID string) ([]*orgs.Member, error) {
	return f.list, f.err
}

// The workspace under test: two admins and an ordinary member. "alice" acts,
// "bob" is acted upon, "carol" is the other admin.
func membershipNotifier(t *testing.T) (*Notifier, *fakeStore) {
	t.Helper()
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{}, nil).
		SetOrgService(&fakeOrgMembers{list: []*orgs.Member{
			orgMember("alice", orgs.RoleAdmin),
			orgMember("carol", orgs.RoleAdmin),
			orgMember("bob", orgs.RoleMember),
		}}).
		SetUserNamer(UserNamerFunc(func(userID string) string {
			return map[string]string{"alice": "Alice", "bob": "Bob", "carol": "Carol"}[userID]
		}))
	return n, store
}

func membershipEvent(eventType, actor string, payload map[string]interface{}) domainevents.Event {
	return domainevents.New(eventType, "", "bob", actor, payload).WithOrg("org-1")
}

// forUser returns the notifications delivered to one recipient.
func forUser(store *fakeStore, userID string) []*notifications.Notification {
	var out []*notifications.Notification
	for _, n := range store.created {
		if n.UserID == userID {
			out = append(out, n)
		}
	}
	return out
}

func TestARoleChangeTellsTheMemberAndTheOtherAdmins(t *testing.T) {
	n, store := membershipNotifier(t)

	n.Handle(membershipEvent(domainevents.OrgMemberRoleChanged, "user:alice", map[string]interface{}{
		"user_id": "bob",
		"from":    orgs.RoleMember,
		"to":      orgs.RoleAdmin,
	}))

	// The member learns what they can do now, stated as the new state rather
	// than as a transition.
	mine := forUser(store, "bob")
	if len(mine) != 1 {
		t.Fatalf("the affected member got %d notifications, want 1", len(mine))
	}
	if mine[0].Type != notifications.TypeAccessChanged {
		t.Errorf("type is %q, want %q", mine[0].Type, notifications.TypeAccessChanged)
	}
	if !strings.Contains(mine[0].Body, "now an admin") {
		t.Errorf("the member is not told their new role: %q", mine[0].Body)
	}

	// The other admin learns who changed, with both ends of the transition.
	theirs := forUser(store, "carol")
	if len(theirs) != 1 {
		t.Fatalf("the other admin got %d notifications, want 1", len(theirs))
	}
	if theirs[0].Type != notifications.TypeMembershipChanged {
		t.Errorf("type is %q, want %q", theirs[0].Type, notifications.TypeMembershipChanged)
	}
	if !strings.Contains(theirs[0].Body, "Bob") ||
		!strings.Contains(theirs[0].Body, "now an admin") ||
		!strings.Contains(theirs[0].Body, "was a member") {
		t.Errorf("the admin's copy loses who or what changed: %q", theirs[0].Body)
	}

	// The admin who made the change is not told about their own action.
	if got := forUser(store, "alice"); len(got) != 0 {
		t.Errorf("the acting admin was notified about their own change: %+v", got[0])
	}
}

// An ordinary member is not an auditor: workspace governance goes to admins.
func TestOrdinaryMembersAreNotToldAboutOtherPeople(t *testing.T) {
	n, store := membershipNotifier(t)

	n.Handle(membershipEvent(domainevents.OrgMemberAdded, "user:alice", map[string]interface{}{
		"user_id": "dave",
		"role":    orgs.RoleMember,
	}))

	if got := forUser(store, "bob"); len(got) != 0 {
		t.Errorf("an ordinary member was told who joined: %+v", got[0])
	}
	if got := forUser(store, "carol"); len(got) != 1 {
		t.Errorf("the other admin got %d notifications about the join, want 1", len(got))
	}
	if got := forUser(store, "dave"); len(got) != 1 {
		t.Errorf("the new member got %d notifications about their own access, want 1", len(got))
	}
}

// Leaving and being removed read completely differently, and only one of them
// is news to the person it happened to.
func TestLeavingAndBeingRemovedAreDifferent(t *testing.T) {
	t.Run("removed by an admin", func(t *testing.T) {
		n, store := membershipNotifier(t)
		n.Handle(membershipEvent(domainevents.OrgMemberRemoved, "user:alice", map[string]interface{}{
			"user_id": "bob",
			"self":    false,
		}))

		mine := forUser(store, "bob")
		if len(mine) != 1 {
			t.Fatalf("a removed member got %d notifications, want 1", len(mine))
		}
		if !strings.Contains(mine[0].Title, "removed") {
			t.Errorf("the removal is not stated: %q", mine[0].Title)
		}
		if got := forUser(store, "carol"); len(got) != 1 ||
			!strings.Contains(got[0].Body, "was removed") {
			t.Errorf("the admin copy does not say they were removed: %+v", got)
		}
	})

	t.Run("left of their own accord", func(t *testing.T) {
		n, store := membershipNotifier(t)
		n.Handle(membershipEvent(domainevents.OrgMemberRemoved, "user:bob", map[string]interface{}{
			"user_id": "bob",
			"self":    true,
		}))

		// Somebody who left knows they left.
		if got := forUser(store, "bob"); len(got) != 0 {
			t.Errorf("a member who left was told they left: %+v", got[0])
		}
		// The admins still need to know the workspace lost somebody.
		got := forUser(store, "carol")
		if len(got) != 1 {
			t.Fatalf("the admin got %d notifications about a departure, want 1", len(got))
		}
		if !strings.Contains(got[0].Body, "left this workspace") {
			t.Errorf("a voluntary departure reads as a removal: %q", got[0].Body)
		}
	})
}

// An invited address usually has no account, so there is nobody to notify in
// app — the invitation email is that person's notification. The admins are
// the audience here.
func TestAnInvitationTellsTheAdminsOnly(t *testing.T) {
	n, store := membershipNotifier(t)

	n.Handle(membershipEvent(domainevents.OrgInvitationSent, "user:alice", map[string]interface{}{
		"email": "dave@example.com",
		"role":  orgs.RoleMember,
	}))

	if len(store.created) != 1 {
		t.Fatalf("an invitation produced %d notifications, want 1 (the non-acting admin)", len(store.created))
	}
	got := store.created[0]
	if got.UserID != "carol" || got.Type != notifications.TypeMembershipChanged {
		t.Fatalf("the invitation went to the wrong recipient: %+v", got)
	}
	if !strings.Contains(got.Body, "dave@example.com") {
		t.Errorf("the admin is not told who was invited: %q", got.Body)
	}
}

// Accepting is the join, and it is the moment the admins care about.
func TestAcceptingAnInvitationReadsAsAJoin(t *testing.T) {
	n, store := membershipNotifier(t)

	n.Handle(membershipEvent(domainevents.OrgInvitationAccepted, "user:dave", map[string]interface{}{
		"user_id": "dave",
		"role":    orgs.RoleMember,
	}))

	for _, admin := range []string{"alice", "carol"} {
		got := forUser(store, admin)
		if len(got) != 1 || !strings.Contains(got[0].Body, "joined this workspace") {
			t.Errorf("admin %s was not told about the join: %+v", admin, got)
		}
	}
	// The person who clicked the link does not need telling what they did.
	if got := forUser(store, "dave"); len(got) != 0 {
		t.Errorf("the joiner was notified of their own acceptance: %+v", got[0])
	}
}

// Project membership is the member's business, not the workspace admins':
// project roles change constantly and would drown the arrivals and departures
// that matter.
func TestProjectChangesReachTheMemberButNotWorkspaceAdmins(t *testing.T) {
	n, store := membershipNotifier(t)

	e := domainevents.New(domainevents.ProjectMemberRoleChanged, "proj-1", "bob", "user:alice",
		map[string]interface{}{"user_id": "bob", "from": "viewer", "to": "editor"}).WithOrg("org-1")
	n.Handle(e)

	mine := forUser(store, "bob")
	if len(mine) != 1 {
		t.Fatalf("the member got %d notifications about their project role, want 1", len(mine))
	}
	if !strings.Contains(mine[0].Body, "editor") {
		t.Errorf("the member is not told their new project role: %q", mine[0].Body)
	}
	if ref, _ := mine[0].EntityRef["kind"].(string); ref != "project_membership" {
		t.Errorf("entity ref kind is %q, want project_membership", ref)
	}
	if got := forUser(store, "carol"); len(got) != 0 {
		t.Errorf("a workspace admin was told about a project role change: %+v", got[0])
	}
}

// Without a workspace roster the membership events must be ignored rather
// than half-delivered — the member would otherwise hear about their access
// while the admins silently heard nothing.
func TestWithoutAnOrgServiceOnlyTheMemberIsTold(t *testing.T) {
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{}, nil)

	n.Handle(membershipEvent(domainevents.OrgMemberAdded, "user:alice", map[string]interface{}{
		"user_id": "bob",
		"role":    orgs.RoleMember,
	}))

	if len(store.created) != 1 || store.created[0].UserID != "bob" {
		t.Fatalf("without an org service the fan-out produced %+v", store.created)
	}
}

// A missing display name must not put a UUID in front of a person.
func TestAMissingNameFallsBackToSomethingReadable(t *testing.T) {
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{}, nil).
		SetOrgService(&fakeOrgMembers{list: []*orgs.Member{orgMember("carol", orgs.RoleAdmin)}})

	n.Handle(membershipEvent(domainevents.OrgMemberAdded, "user:alice", map[string]interface{}{
		"user_id": "bob",
		"role":    orgs.RoleMember,
	}))

	got := forUser(store, "carol")
	if len(got) != 1 {
		t.Fatalf("the admin got %d notifications, want 1", len(got))
	}
	if strings.Contains(got[0].Body, "bob") {
		t.Errorf("a raw user id reached the admin's copy: %q", got[0].Body)
	}
	if !strings.Contains(got[0].Body, "Someone") {
		t.Errorf("the fallback name is missing: %q", got[0].Body)
	}
}

// Both new types email by default: an access change is exactly the thing
// somebody needs to know while they are not looking at the app.
func TestAccessChangesEmailByDefault(t *testing.T) {
	defaults := map[string]bool{}
	for _, t := range DefaultEmailTypes() {
		defaults[t] = true
	}
	for _, want := range []string{notifications.TypeAccessChanged, notifications.TypeMembershipChanged} {
		if !defaults[want] {
			t.Errorf("%q does not email by default", want)
		}
	}
}
