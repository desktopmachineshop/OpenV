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

func (m *memRepo) save(inv *Invitation) error {
	copied := *inv
	m.rows[inv.ID] = &copied
	return nil
}

// Replace mirrors the postgres upsert: any unaccepted row for the same
// (org, email) — expired or not — makes way for the new one.
func (m *memRepo) Replace(inv *Invitation) error {
	for id, row := range m.rows {
		if row.OrgID == inv.OrgID && row.Email == inv.Email && row.AcceptedAt == nil {
			delete(m.rows, id)
		}
	}
	return m.save(inv)
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
// one workspace to prove a refusal does not strand the others. It also
// answers Get, which Create reads to refuse a personal workspace: every
// workspace is a company one unless personal names it.
type memMembers struct {
	added    []string // "orgID:userID:role"
	refuse   map[string]error
	personal map[string]bool
	missing  map[string]bool
}

func (m *memMembers) AddMember(orgID, userID, role string) error {
	if err := m.refuse[orgID]; err != nil {
		return err
	}
	m.added = append(m.added, orgID+":"+userID+":"+role)
	return nil
}

func (m *memMembers) Get(orgID string) (*orgs.Org, error) {
	if m.missing[orgID] {
		return nil, orgs.ErrNotFound
	}
	orgType := orgs.TypeCompany
	if m.personal[orgID] {
		orgType = orgs.TypePersonal
	}
	return &orgs.Org{ID: orgID, Name: "Test Workspace", OrgType: orgType}, nil
}

func newTestService() (*DefaultService, *memRepo, *memMembers) {
	repo := newMemRepo()
	members := &memMembers{refuse: map[string]error{}, personal: map[string]bool{}, missing: map[string]bool{}}
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

// A personal workspace cannot have members, so it cannot invite: the refusal
// comes before any row is written.
func TestCreateRefusesAPersonalWorkspace(t *testing.T) {
	svc, repo, _ := newTestService()
	svc.members.(*memMembers).personal["org-personal"] = true

	if _, _, err := svc.Create("org-personal", "someone@example.com", "", nil); !errors.Is(err, orgs.ErrPersonalOrgMembers) {
		t.Fatalf("Create on a personal workspace returned %v, want orgs.ErrPersonalOrgMembers", err)
	}
	if len(repo.rows) != 0 {
		t.Errorf("a refused invitation wrote %d rows", len(repo.rows))
	}
}

// Re-inviting an address whose previous invitation has EXPIRED works: the
// pending-uniqueness index covers expired rows too, so the old row has to go
// whether it was still live or not (it used to 500).
func TestReInvitingAnExpiredAddressReplacesTheOldRow(t *testing.T) {
	svc, repo, _ := newTestService()
	first, _, err := svc.Create("org-1", "lapsed@example.com", orgs.RoleMember, nil)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	repo.rows[first.ID].ExpiresAt = time.Now().Add(-time.Hour)

	second, token, err := svc.Create("org-1", "Lapsed@Example.com", orgs.RoleAdmin, nil)
	if err != nil {
		t.Fatalf("re-invite after expiry: %v", err)
	}
	if _, ok := repo.rows[first.ID]; ok {
		t.Error("the expired row survived the re-invite")
	}
	if len(repo.rows) != 1 || repo.rows[second.ID] == nil {
		t.Errorf("rows = %d, want only the new invitation", len(repo.rows))
	}
	if _, err := svc.Lookup(token); err != nil {
		t.Errorf("the new link must work: %v", err)
	}
}

// Create hands back the row as stored, so the invitation email can name the
// workspace and the person who sent it.
func TestCreateReturnsTheDisplayNames(t *testing.T) {
	svc, repo, _ := newTestService()
	inv, _, err := svc.Create("org-1", "named@example.com", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// The repository joins the names in on the way out; Create must read the
	// row back rather than return the struct it wrote.
	repo.rows[inv.ID].OrgName = "Desktop Machine Shop"
	repo.rows[inv.ID].InvitedByName = "Dave"
	again, _, err := svc.Create("org-1", "named@example.com", "", nil)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	stored := repo.rows[again.ID]
	if again.ID != stored.ID || again.Email != stored.Email {
		t.Errorf("Create returned %+v, want the stored row", again)
	}
}

// A token joins only the address it was sent to: registering as somebody
// else with a link that reached a different mailbox grants nothing.
func TestAcceptTokenForEmailChecksTheAddress(t *testing.T) {
	svc, _, members := newTestService()
	_, token, err := svc.Create("org-1", "invited@example.com", orgs.RoleAdmin, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.AcceptTokenForEmail(token, "someone-else@example.com", "user-1"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("mismatched address returned %v, want ErrInvalidToken", err)
	}
	if len(members.added) != 0 {
		t.Fatalf("a mismatched address joined anyway: %v", members.added)
	}
	// The invited address, however it is typed, joins at the invited role.
	if _, err := svc.AcceptTokenForEmail(token, " Invited@Example.com ", "user-1"); err != nil {
		t.Fatalf("AcceptTokenForEmail: %v", err)
	}
	if len(members.added) != 1 || members.added[0] != "org-1:user-1:admin" {
		t.Errorf("memberships = %v", members.added)
	}
}
