package invitations

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// memRepo is a minimal in-memory Repository for the service tests. markErr,
// when set, fails the NEXT MarkAccepted and then clears itself, which is how
// a half-finished acceptance is staged; clearErr fails every un-stamp, for
// the case where a claim cannot even be given back.
type memRepo struct {
	rows     map[string]*Invitation
	markErr  error
	clearErr error
	cleared  []string
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
	if m.markErr != nil {
		err := m.markErr
		m.markErr = nil // one failure, so a retry can be observed
		return false, err
	}
	inv := m.rows[id]
	if inv == nil || inv.AcceptedAt != nil || !inv.ExpiresAt.After(at) {
		return false, nil
	}
	inv.AcceptedAt = &at
	return true, nil
}

// ClearAccepted returns a stamped row to pending, as the SQL does.
func (m *memRepo) ClearAccepted(id string) error {
	if m.clearErr != nil {
		return m.clearErr
	}
	m.cleared = append(m.cleared, id)
	if inv := m.rows[id]; inv != nil {
		inv.AcceptedAt = nil
	}
	return nil
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
// one workspace to prove a refusal does not strand the others. It answers
// RoleInOrg from the memberships it holds — which is what tells acceptance
// that somebody is already in — and Get, which Create reads to refuse a
// personal workspace: every workspace is a company one unless personal
// names it.
type memMembers struct {
	added    []string          // "orgID:userID:role"
	roles    map[string]string // "orgID:userID" -> role
	refuse   map[string]error
	personal map[string]bool
	missing  map[string]bool
}

func (m *memMembers) AddMember(orgID, userID, role string) error {
	if err := m.refuse[orgID]; err != nil {
		return err
	}
	m.added = append(m.added, orgID+":"+userID+":"+role)
	m.roles[orgID+":"+userID] = role
	return nil
}

func (m *memMembers) RoleInOrg(orgID, userID string) (string, error) {
	return m.roles[orgID+":"+userID], nil
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
	members := &memMembers{
		roles:    map[string]string{},
		refuse:   map[string]error{},
		personal: map[string]bool{},
		missing:  map[string]bool{},
	}
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
	acc, err := svc.AcceptTokenForEmail(token, "dave@example.com", "user-1")
	if err != nil {
		t.Fatalf("AcceptTokenForEmail: %v", err)
	}
	if acc.Role != orgs.RoleMember || acc.AlreadyMember {
		t.Errorf("acceptance = %+v, want a fresh membership at the invited role", acc)
	}
	if len(members.added) != 1 || members.added[0] != "org-1:user-1:member" {
		t.Errorf("memberships = %v", members.added)
	}

	// One use only: the link is spent.
	if _, err := svc.AcceptTokenForEmail(token, "dave@example.com", "user-2"); !errors.Is(err, ErrInvalidToken) {
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
	if _, err := svc.AcceptTokenForEmail(token, "late@example.com", "user-1"); !errors.Is(err, ErrInvalidToken) {
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

// A provider-verified address joins every workspace that invited it,
// whatever case each invitation was typed in — and one workspace that
// refuses does not strand the rest, though it IS reported.
func TestAcceptAllForProviderVerifiedEmail(t *testing.T) {
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

	accepted, err := svc.AcceptAllForProviderVerifiedEmail("DAVE@EXAMPLE.COM", "user-1")
	// The refusal is returned, not swallowed: the caller logs a membership
	// that did not happen instead of silently losing it.
	if err == nil {
		t.Fatal("a refused workspace must be reported, not dropped")
	}
	if !strings.Contains(err.Error(), "org-2") {
		t.Errorf("error %q does not name the workspace that refused", err)
	}
	if len(accepted) != 2 {
		t.Fatalf("accepted %d invitations, want 2", len(accepted))
	}
	if len(members.added) != 2 {
		t.Errorf("memberships = %v", members.added)
	}
	// The two that worked are spent; the one that refused stays pending, so
	// its link can still take it up later.
	pending, _ := svc.PendingForEmail("dave@example.com")
	if len(pending) != 1 || pending[0].OrgID != "org-2" {
		t.Errorf("pending = %+v, want only the workspace that refused", pending)
	}
}

// An invitation is an offer of a way in, never a way to move somebody
// between roles: an admin who follows a later "member" link stays an admin,
// and the workspace's last admin therefore cannot be demoted through this
// path. The invitation is still spent, so the link does not stay live.
func TestAcceptNeverRewritesAnExistingRole(t *testing.T) {
	svc, _, members := newTestService()
	members.roles["org-1:user-1"] = orgs.RoleAdmin // the only admin

	_, token, err := svc.Create("org-1", "admin@example.com", orgs.RoleMember, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	acc, err := svc.AcceptTokenForEmail(token, "admin@example.com", "user-1")
	if err != nil {
		t.Fatalf("AcceptTokenForEmail: %v", err)
	}
	if !acc.AlreadyMember || acc.Role != orgs.RoleAdmin {
		t.Errorf("acceptance = %+v, want the role they already held", acc)
	}
	if len(members.added) != 0 {
		t.Errorf("an existing membership was rewritten: %v", members.added)
	}
	if members.roles["org-1:user-1"] != orgs.RoleAdmin {
		t.Errorf("role = %q, want the last admin to stay an admin", members.roles["org-1:user-1"])
	}
	if _, err := svc.Lookup(token); !errors.Is(err, ErrInvalidToken) {
		t.Error("an accepted invitation must be spent even when it granted nothing new")
	}
}

// The row is claimed BEFORE any membership is written, so a claim that
// fails grants nothing at all — and leaves the link usable, because nothing
// was spent.
func TestAFailedClaimGrantsNothingAndLeavesTheLinkUsable(t *testing.T) {
	svc, repo, members := newTestService()
	_, token, err := svc.Create("org-1", "half@example.com", orgs.RoleMember, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	repo.markErr = errors.New("database went away mid-write")

	if _, err := svc.AcceptTokenForEmail(token, "half@example.com", "user-1"); err == nil {
		t.Fatal("a failed claim must be reported")
	}
	if len(members.added) != 0 {
		t.Fatalf("memberships = %v, want none written behind a failed claim", members.added)
	}
	if _, err := svc.Lookup(token); err != nil {
		t.Fatalf("the invitation must still be pending after a failed claim: %v", err)
	}

	// The retry gets the membership the first attempt never wrote.
	acc, err := svc.AcceptTokenForEmail(token, "half@example.com", "user-1")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if acc.AlreadyMember || acc.Role != orgs.RoleMember {
		t.Errorf("acceptance = %+v, want a fresh membership at the invited role", acc)
	}
	if len(members.added) != 1 {
		t.Errorf("memberships = %v, want exactly one", members.added)
	}
	if _, err := svc.Lookup(token); !errors.Is(err, ErrInvalidToken) {
		t.Error("the retry must spend the invitation")
	}
}

// The revoke race. An invitation deleted between the lookup and the claim
// must grant NO membership: the claim finds no pending row, so the
// acceptance fails with the same flat ErrInvalidToken the caller already
// answers 404 to — instead of handing out a membership the admin had just
// taken back while telling the caller the link was invalid.
func TestARowRevokedBetweenLookupAndClaimGrantsNothing(t *testing.T) {
	svc, repo, members := newTestService()
	inv, token, err := svc.Create("org-1", "raced@example.com", orgs.RoleAdmin, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Resolve the link the way registration does, then revoke it — exactly
	// the window the old membership-first ordering wrote through.
	resolved, err := svc.Lookup(token)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if err := svc.Revoke("org-1", inv.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := svc.AcceptResolvedForEmail(resolved, "raced@example.com", "user-1"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("accepting a revoked invitation returned %v, want ErrInvalidToken", err)
	}
	if len(members.added) != 0 {
		t.Errorf("a revoked invitation granted membership anyway: %v", members.added)
	}
	if len(repo.rows) != 0 {
		t.Errorf("the revoked row came back: %+v", repo.rows)
	}
}

// A claim whose membership cannot be written is given back: the row returns
// to pending, so the link still works and the person is not left holding a
// spent link and no membership.
func TestAFailedAddMemberReturnsTheInvitationToPending(t *testing.T) {
	svc, repo, members := newTestService()
	inv, token, err := svc.Create("org-1", "refused@example.com", orgs.RoleMember, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	members.refuse["org-1"] = errors.New("workspace is locked")

	if _, err := svc.AcceptTokenForEmail(token, "refused@example.com", "user-1"); err == nil {
		t.Fatal("a refused membership must be reported")
	}
	if len(members.added) != 0 {
		t.Errorf("memberships = %v, want none", members.added)
	}
	if repo.rows[inv.ID].AcceptedAt != nil {
		t.Error("the row stayed stamped after its membership failed")
	}
	if _, err := svc.Lookup(token); err != nil {
		t.Fatalf("the link must still work: %v", err)
	}

	// Once the workspace accepts members again, the same link joins.
	delete(members.refuse, "org-1")
	if _, err := svc.AcceptTokenForEmail(token, "refused@example.com", "user-1"); err != nil {
		t.Fatalf("retry after the workspace recovered: %v", err)
	}
	if len(members.added) != 1 {
		t.Errorf("memberships = %v, want exactly one", members.added)
	}
}

// When even the un-stamp fails, both failures are reported: the row is spent
// with no membership behind it, and the caller's log has to be able to say so.
func TestAFailedUnstampIsReportedWithTheMembershipFailure(t *testing.T) {
	svc, repo, members := newTestService()
	_, token, err := svc.Create("org-1", "stuck@example.com", orgs.RoleMember, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	members.refuse["org-1"] = errors.New("workspace is locked")
	repo.clearErr = errors.New("database went away")

	_, err = svc.AcceptTokenForEmail(token, "stuck@example.com", "user-1")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "workspace is locked") || !strings.Contains(err.Error(), "database went away") {
		t.Errorf("error %q must report both the refused membership and the failed un-stamp", err)
	}
}

// An account that is already a member does not need the claim: nothing is
// granted, so losing the race for the stamp is not a failure.
func TestAlreadyAMemberSpendsTheLinkWithoutClaiming(t *testing.T) {
	svc, repo, members := newTestService()
	members.roles["org-1:user-1"] = orgs.RoleMember
	inv, token, err := svc.Create("org-1", "in@example.com", orgs.RoleAdmin, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	acc, err := svc.AcceptTokenForEmail(token, "in@example.com", "user-1")
	if err != nil {
		t.Fatalf("AcceptTokenForEmail: %v", err)
	}
	if !acc.AlreadyMember || acc.Role != orgs.RoleMember {
		t.Errorf("acceptance = %+v, want the role already held", acc)
	}
	if repo.rows[inv.ID].AcceptedAt == nil {
		t.Error("the link must be spent")
	}
	if len(repo.cleared) != 0 {
		t.Errorf("nothing was granted, so nothing should have been un-stamped: %v", repo.cleared)
	}
}

// Two arrivals racing on one link produce one membership, not two.
func TestAcceptIsClaimedOnce(t *testing.T) {
	svc, _, members := newTestService()
	_, token, err := svc.Create("org-1", "race@example.com", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.AcceptTokenForEmail(token, "race@example.com", "user-1"); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if _, err := svc.AcceptTokenForEmail(token, "race@example.com", "user-2"); !errors.Is(err, ErrInvalidToken) {
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
	// A mismatch is its own error: the link is live, it was simply sent
	// somewhere else, and the caller answers differently (403, not 404).
	if _, err := svc.AcceptTokenForEmail(token, "someone-else@example.com", "user-1"); !errors.Is(err, ErrEmailMismatch) {
		t.Errorf("mismatched address returned %v, want ErrEmailMismatch", err)
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
