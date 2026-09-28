package orgs

import (
	"errors"
	"testing"
)

// memberRepoFake is the smallest Repository that lets the membership writes
// run: one workspace's roles by account, and the writes it was asked for.
type memberRepoFake struct {
	Repository
	roles   map[string]string
	upserts []string
	removed []string
}

func (f *memberRepoFake) MemberRole(orgID, userID string) (string, error) {
	return f.roles[userID], nil
}

func (f *memberRepoFake) CountAdmins(orgID string) (int, error) {
	n := 0
	for _, role := range f.roles {
		if role == RoleAdmin {
			n++
		}
	}
	return n, nil
}

func (f *memberRepoFake) UpsertMember(orgID, userID, role string) error {
	f.upserts = append(f.upserts, userID+":"+role)
	f.roles[userID] = role
	return nil
}

// RemoveMember answers a delete that finds no row as the postgres store does.
func (f *memberRepoFake) RemoveMember(orgID, userID string) error {
	if _, ok := f.roles[userID]; !ok {
		return ErrNotMember
	}
	f.removed = append(f.removed, userID)
	delete(f.roles, userID)
	return nil
}

// Demoting a workspace's only admin is refused with ErrLastAdmin, a sentinel
// the handler can tell from a store failure, and writes nothing; with a
// second admin the same demotion goes through (REQ-15).
func TestSetMemberRoleRefusesToDemoteTheLastAdmin(t *testing.T) {
	repo := &memberRepoFake{roles: map[string]string{"owner": RoleAdmin, "m": RoleMember}}
	svc := NewDefaultService(repo)

	if err := svc.SetMemberRole("o1", "owner", RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoting the only admin: err = %v, want ErrLastAdmin", err)
	}
	if repo.upserts != nil {
		t.Fatalf("a refused demotion wrote %v", repo.upserts)
	}
	if err := svc.SetMemberRole("o1", "m", RoleAdmin); err != nil {
		t.Fatalf("promoting a member: %v", err)
	}
	if err := svc.SetMemberRole("o1", "owner", RoleMember); err != nil {
		t.Fatalf("demoting one of two admins: %v", err)
	}
}

// Removing an account that is not a member is refused with ErrNotMember, as
// a role change for it is, and deletes nothing: there is no departure for
// the handler to report. A member's removal and the last admin's refusal
// are unchanged.
func TestRemoveMemberRefusesANonMember(t *testing.T) {
	repo := &memberRepoFake{roles: map[string]string{"owner": RoleAdmin, "m": RoleMember}}
	svc := NewDefaultService(repo)

	if err := svc.RemoveMember("o1", "phantom"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("removing a non-member: err = %v, want ErrNotMember", err)
	}
	if repo.removed != nil {
		t.Fatalf("a refused removal deleted %v", repo.removed)
	}
	if err := svc.RemoveMember("o1", "owner"); err == nil || errors.Is(err, ErrNotMember) {
		t.Fatalf("removing the only admin: err = %v, want the last-admin refusal", err)
	}
	if err := svc.RemoveMember("o1", "m"); err != nil {
		t.Fatalf("removing a member: %v", err)
	}
	if len(repo.removed) != 1 || repo.removed[0] != "m" {
		t.Fatalf("removed = %v, want [m]", repo.removed)
	}
}
