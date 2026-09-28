package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// A workspace membership's removal reports whether it removed anything. The
// service reads the role before it deletes (for the last-admin refusal), so
// two removals of one member can both pass that read; only the DELETE can
// tell which of them removed the row, and the other must answer
// ErrNotMember, not a second success that the handler would answer 204 and
// publish as a second org.member_removed (REQ-122).

type memberFixture struct {
	repo  *OrgRepository
	svc   orgs.Service
	orgID string
	owner string
	m     string
}

func newMemberFixture(t *testing.T) memberFixture {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)
	f := memberFixture{repo: NewOrgRepository(db), owner: uuid.New().String(), m: uuid.New().String()}
	f.svc = orgs.NewDefaultService(f.repo)
	for _, u := range []struct{ id, email string }{{f.owner, "owner@example.com"}, {f.m, "m@example.com"}} {
		if _, err := db.Exec(`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Someone')`, u.id, u.email); err != nil {
			t.Fatal(err)
		}
	}
	org, err := f.svc.CreateOrg("Members Workspace", orgs.TypeCompany, f.owner)
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	f.orgID = org.ID
	if err := f.repo.UpsertMember(f.orgID, f.m, orgs.RoleMember); err != nil {
		t.Fatalf("UpsertMember: %v", err)
	}
	return f
}

func TestOrgRemoveMemberReportsARowItDidNotRemove(t *testing.T) {
	f := newMemberFixture(t)

	if err := f.repo.RemoveMember(f.orgID, f.m); err != nil {
		t.Fatalf("removing a member: %v", err)
	}
	if err := f.repo.RemoveMember(f.orgID, f.m); !errors.Is(err, orgs.ErrNotMember) {
		t.Fatalf("removing the same member again: err = %v, want ErrNotMember", err)
	}
	if role, err := f.repo.MemberRole(f.orgID, f.owner); err != nil || role != orgs.RoleAdmin {
		t.Fatalf("the other membership: role %q, err %v; want admin", role, err)
	}
}

// barrierRepo holds each caller of MemberRole until two have read a role, so
// both removals below decide on the same stale read before either deletes:
// two admins removing one member at once, or a member leaving while an admin
// removes them.
type barrierRepo struct {
	orgs.Repository
	arrived chan struct{}
	release chan struct{}
}

func (b *barrierRepo) MemberRole(orgID, userID string) (string, error) {
	role, err := b.Repository.MemberRole(orgID, userID)
	b.arrived <- struct{}{}
	select {
	case <-b.release:
	case <-time.After(10 * time.Second):
		return "", errors.New("barrier: the second removal never read the role")
	}
	return role, err
}

func TestOrgConcurrentRemovalsOfOneMemberReportOneRemoval(t *testing.T) {
	f := newMemberFixture(t)
	repo := &barrierRepo{Repository: f.repo, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	svc := orgs.NewDefaultService(repo)

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- svc.RemoveMember(f.orgID, f.m) }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-repo.arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("a removal never read the member's role")
		}
	}
	close(repo.release)

	var removed, refused int
	for i := 0; i < 2; i++ {
		switch err := <-errs; {
		case err == nil:
			removed++
		case errors.Is(err, orgs.ErrNotMember):
			refused++
		default:
			t.Fatalf("a concurrent removal failed: %v", err)
		}
	}
	if removed != 1 || refused != 1 {
		t.Fatalf("two concurrent removals of one member: %d succeeded and %d were refused, want 1 and 1", removed, refused)
	}
	if role, err := f.repo.MemberRole(f.orgID, f.m); err != nil || role != "" {
		t.Fatalf("after the removals: role %q, err %v; want no membership", role, err)
	}
}
