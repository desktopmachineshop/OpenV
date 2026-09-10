package invitations

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// memRepo is a minimal in-memory Repository for the service tests.
type memRepo struct {
	rows map[string]*Invitation
}

func newMemRepo() *memRepo { return &memRepo{rows: map[string]*Invitation{}} }

func (m *memRepo) Save(inv *Invitation) error {
	copied := *inv
	m.rows[inv.ID] = &copied
	return nil
}

func (m *memRepo) ListPending(orgID string, now time.Time) ([]*Invitation, error) {
	var out []*Invitation
	for _, inv := range m.rows {
		if inv.OrgID == orgID && inv.Pending(now) {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (m *memRepo) FindByID(id string) (*Invitation, error) { return m.rows[id], nil }

func (m *memRepo) FindByTokenHash(hash string) (*Invitation, error) {
	for _, inv := range m.rows {
		if inv.TokenHash == hash {
			return inv, nil
		}
	}
	return nil, nil
}

func (m *memRepo) FindPendingForOrg(orgID, email string, now time.Time) (*Invitation, error) {
	for _, inv := range m.rows {
		if inv.OrgID == orgID && strings.EqualFold(inv.Email, email) && inv.Pending(now) {
			return inv, nil
		}
	}
	return nil, nil
}

func (m *memRepo) ListPendingForEmail(email string, now time.Time) ([]*Invitation, error) {
	var out []*Invitation
	for _, inv := range m.rows {
		if strings.EqualFold(inv.Email, email) && inv.Pending(now) {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (m *memRepo) MarkAccepted(id string, at time.Time) (bool, error) {
	inv := m.rows[id]
	if inv == nil || inv.AcceptedAt != nil || !inv.ExpiresAt.After(at) {
		return false, nil
	}
	inv.AcceptedAt = &at
	return true, nil
}

func (m *memRepo) Delete(id string) error { delete(m.rows, id); return nil }

func (m *memRepo) DeleteExpired(before time.Time) error {
	for id, inv := range m.rows {
		if inv.AcceptedAt == nil && inv.ExpiresAt.Before(before) {
			delete(m.rows, id)
		}
	}
	return nil
}

// memMembers records the memberships an acceptance creates, and can refuse
// one workspace to prove a refusal does not strand the others.
type memMembers struct {
	added  []string // "orgID:userID:role"
	refuse map[string]error
}

func (m *memMembers) AddMember(orgID, userID, role string) error {
	if err := m.refuse[orgID]; err != nil {
		return err
	}
	m.added = append(m.added, orgID+":"+userID+":"+role)
	return nil
}

func newTestService() (*DefaultService, *memRepo, *memMembers) {
	repo := newMemRepo()
	members := &memMembers{refuse: map[string]error{}}
	return NewDefaultService(repo, members), repo, members
}

func TestCreateAndAccept(t *testing.T) {
	svc, _, members := newTestService()

	inv, token, err := svc.Create("org-1", "  Dave@Example.com ", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The address is folded and the role defaults to member.
	if inv.Email != "dave@example.com" {
		t.Errorf("email = %q, want the folded address", inv.Email)
	}
	if inv.Role != orgs.RoleMember {
		t.Errorf("role = %q, want %q", inv.Role, orgs.RoleMember)
	}
	if token == "" || inv.TokenHash == token {
		t.Error("the raw token must be returned and never stored as itself")
	}

	// The link resolves for anyone holding it, whatever case they type.
	if _, err := svc.Lookup(token); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if _, err := svc.AcceptToken(token, "user-1"); err != nil {
		t.Fatalf("AcceptToken: %v", err)
	}
	if len(members.added) != 1 || members.added[0] != "org-1:user-1:member" {
		t.Errorf("memberships = %v", members.added)
	}

	// One use only: the link is spent.
	if _, err := svc.AcceptToken(token, "user-2"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second acceptance returned %v, want ErrInvalidToken", err)
	}
	if pending, _ := svc.ListPending("org-1"); len(pending) != 0 {
		t.Errorf("an accepted invitation must leave the pending list, got %d", len(pending))
	}
}

func TestCreateValidates(t *testing.T) {
	svc, _, _ := newTestService()
	if _, _, err := svc.Create("org-1", "not-an-email", "", nil); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad address returned %v, want ErrInvalidEmail", err)
	}
	if _, _, err := svc.Create("org-1", "", "", nil); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("empty address returned %v, want ErrInvalidEmail", err)
	}
	if _, _, err := svc.Create("org-1", "a@example.com", "owner", nil); !errors.Is(err, orgs.ErrInvalidRole) {
		t.Errorf("unknown role returned %v, want orgs.ErrInvalidRole", err)
	}
}

// Re-inviting an address replaces its pending invitation, so only the newest
// link works and the admin never has two live credentials out.
func TestReInvitingReplacesThePendingLink(t *testing.T) {
	svc, _, _ := newTestService()
	_, first, err := svc.Create("org-1", "dave@example.com", orgs.RoleAdmin, nil)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, second, err := svc.Create("org-1", "DAVE@example.com", orgs.RoleAdmin, nil)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if _, err := svc.Lookup(first); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("superseded link returned %v, want ErrInvalidToken", err)
	}
	if _, err := svc.Lookup(second); err != nil {
		t.Errorf("newest link must work: %v", err)
	}
	if pending, _ := svc.ListPending("org-1"); len(pending) != 1 {
		t.Errorf("pending invitations = %d, want 1", len(pending))
	}
}

func TestExpiredInvitationIsNeitherVisibleNorAcceptable(t *testing.T) {
	svc, repo, members := newTestService()
	inv, token, err := svc.Create("org-1", "late@example.com", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	repo.rows[inv.ID].ExpiresAt = time.Now().Add(-time.Minute)

	if _, err := svc.Lookup(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired link returned %v, want ErrInvalidToken", err)
	}
	if _, err := svc.AcceptToken(token, "user-1"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("accepting an expired link returned %v, want ErrInvalidToken", err)
	}
	if len(members.added) != 0 {
		t.Errorf("an expired invitation must create no membership, got %v", members.added)
	}
	if pending, _ := svc.PendingForEmail("late@example.com"); len(pending) != 0 {
		t.Errorf("expired invitations must not count as pending, got %d", len(pending))
	}
	if err := svc.PurgeExpired(time.Now()); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if len(repo.rows) != 0 {
		t.Errorf("purge left %d rows", len(repo.rows))
	}
}

func TestRevoke(t *testing.T) {
	svc, _, _ := newTestService()
	inv, token, err := svc.Create("org-1", "gone@example.com", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Another workspace cannot revoke it, and is told nothing about it.
	if err := svc.Revoke("org-2", inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-workspace revoke returned %v, want ErrNotFound", err)
	}
	if err := svc.Revoke("org-1", inv.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := svc.Lookup(token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("revoked link returned %v, want ErrInvalidToken", err)
	}
	if err := svc.Revoke("org-1", "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id returned %v, want ErrNotFound", err)
	}
}

// A new account joins every workspace that invited its address, whatever
// case each invitation was typed in — and one workspace that refuses does
// not strand the rest.
func TestAcceptAllForEmail(t *testing.T) {
	svc, _, members := newTestService()
	if _, _, err := svc.Create("org-1", "Dave@Example.com", orgs.RoleAdmin, nil); err != nil {
		t.Fatalf("Create org-1: %v", err)
	}
	if _, _, err := svc.Create("org-2", "dave@example.com", orgs.RoleMember, nil); err != nil {
		t.Fatalf("Create org-2: %v", err)
	}
	if _, _, err := svc.Create("org-3", "dave@example.com", orgs.RoleMember, nil); err != nil {
		t.Fatalf("Create org-3: %v", err)
	}
	members.refuse["org-2"] = errors.New("workspace is gone")

	accepted, err := svc.AcceptAllForEmail("DAVE@EXAMPLE.COM", "user-1")
	if err != nil {
		t.Fatalf("AcceptAllForEmail: %v", err)
	}
	if len(accepted) != 2 {
		t.Fatalf("accepted %d invitations, want 2", len(accepted))
	}
	if len(members.added) != 2 {
		t.Errorf("memberships = %v", members.added)
	}
	// Nothing is left pending for the address.
	if pending, _ := svc.PendingForEmail("dave@example.com"); len(pending) != 0 {
		t.Errorf("still pending: %d", len(pending))
	}
}

// Two arrivals racing on one link produce one membership, not two.
func TestAcceptIsClaimedOnce(t *testing.T) {
	svc, _, members := newTestService()
	_, token, err := svc.Create("org-1", "race@example.com", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.AcceptToken(token, "user-1"); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if _, err := svc.AcceptToken(token, "user-2"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("second accept returned %v, want ErrInvalidToken", err)
	}
	if len(members.added) != 1 {
		t.Errorf("memberships = %v, want exactly one", members.added)
	}
}
