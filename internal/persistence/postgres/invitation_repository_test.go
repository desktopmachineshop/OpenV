package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/invitations"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// saveTestOrg creates a company workspace for the invitation tests.
func saveTestOrg(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	id := uuid.New().String()
	if _, err := db.Exec(`
		INSERT INTO organizations (id, name, slug, org_type, plan, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'free', NOW(), NOW())
	`, id, name, "slug-"+id[:8], orgs.TypeCompany); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	return id
}

func saveInvitation(t *testing.T, repo *InvitationRepository, orgID, email, role, token string, expires time.Time, invitedBy *string) *invitations.Invitation {
	t.Helper()
	inv := &invitations.Invitation{
		ID:        uuid.New().String(),
		OrgID:     orgID,
		Email:     email,
		Role:      role,
		TokenHash: users.HashToken(token),
		InvitedBy: invitedBy,
		ExpiresAt: expires,
		CreatedAt: time.Now().UTC(),
	}
	if err := repo.Replace(inv); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	return inv
}

// TestInvitationRoundTrip exercises migration 0025 and the repository: a
// pending invitation is found by token, by workspace and by address, the
// display names come along, acceptance is claimed exactly once, and an
// expired row is invisible to every pending query.
func TestInvitationRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewInvitationRepository(db)
	userRepo := NewUserRepository(db)

	orgID := saveTestOrg(t, db, "Desktop Machine Shop")
	admin := saveTestUser(t, userRepo, "admin@example.com", users.ProviderPassword, true)
	future := time.Now().UTC().Add(invitations.DefaultTTL).Truncate(time.Microsecond)
	inv := saveInvitation(t, repo, orgID, "invited@example.com", orgs.RoleAdmin, "raw-token-1", future, &admin.ID)

	got, err := repo.FindByTokenHash(users.HashToken("raw-token-1"))
	if err != nil || got == nil {
		t.Fatalf("FindByTokenHash: %v (%v)", err, got)
	}
	if got.OrgName != "Desktop Machine Shop" || got.InvitedByName != "Test" {
		t.Errorf("display names = %q / %q", got.OrgName, got.InvitedByName)
	}
	if got.InvitedBy == nil || *got.InvitedBy != admin.ID {
		t.Errorf("invited_by = %v, want %s", got.InvitedBy, admin.ID)
	}
	if got.AcceptedAt != nil {
		t.Error("a fresh invitation must be unaccepted")
	}

	// An unknown token is (nil, nil), not an error.
	if missing, err := repo.FindByTokenHash(users.HashToken("nope")); err != nil || missing != nil {
		t.Errorf("unknown token = %v, %v", missing, err)
	}

	now := time.Now()
	if pending, err := repo.ListPending(orgID, now); err != nil || len(pending) != 1 {
		t.Fatalf("ListPending = %d, %v", len(pending), err)
	}
	// Addresses are stored folded, and a lookup folds what it is given, so
	// the plain equality the email index serves still matches whatever case
	// the caller types.
	if byEmail, err := repo.ListPendingForEmail("Invited@Example.com", now); err != nil || len(byEmail) != 1 {
		t.Errorf("ListPendingForEmail = %d, %v", len(byEmail), err)
	}

	// The claim succeeds once and only once.
	at := time.Now().UTC().Truncate(time.Microsecond)
	won, err := repo.MarkAccepted(inv.ID, at)
	if err != nil || !won {
		t.Fatalf("MarkAccepted = %v, %v", won, err)
	}
	if again, err := repo.MarkAccepted(inv.ID, at); err != nil || again {
		t.Errorf("second MarkAccepted = %v, %v; want false", again, err)
	}
	if pending, _ := repo.ListPending(orgID, time.Now()); len(pending) != 0 {
		t.Errorf("accepted invitation still pending: %d", len(pending))
	}
	accepted, err := repo.FindByID(inv.ID)
	if err != nil || accepted == nil || accepted.AcceptedAt == nil {
		t.Fatalf("FindByID after acceptance = %v, %v", accepted, err)
	}

	// A claim whose membership could not be written is given back: the row
	// returns to pending, the link works again, and it can be re-claimed.
	if err := repo.ClearAccepted(inv.ID); err != nil {
		t.Fatalf("ClearAccepted: %v", err)
	}
	back, err := repo.FindByID(inv.ID)
	if err != nil || back == nil || back.AcceptedAt != nil {
		t.Fatalf("FindByID after ClearAccepted = %v, %v", back, err)
	}
	if pending, _ := repo.ListPending(orgID, time.Now()); len(pending) != 1 {
		t.Error("an un-stamped invitation must be pending again")
	}
	if won, err := repo.MarkAccepted(inv.ID, at); err != nil || !won {
		t.Fatalf("re-claim after an un-stamp = %v, %v", won, err)
	}

	// The partial unique index leaves accepted history alone: the same
	// address can be invited to the same workspace again.
	saveInvitation(t, repo, orgID, "invited@example.com", orgs.RoleMember, "raw-token-2", future, &admin.ID)
	if pending, _ := repo.ListPending(orgID, time.Now()); len(pending) != 1 {
		t.Errorf("re-invite after acceptance should be pending again")
	}
}

func TestInvitationExpiryAndPurge(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewInvitationRepository(db)

	orgID := saveTestOrg(t, db, "Expiry Works")
	past := time.Now().UTC().Add(-time.Hour)
	expired := saveInvitation(t, repo, orgID, "late@example.com", orgs.RoleMember, "raw-token-old", past, nil)

	now := time.Now()
	if pending, _ := repo.ListPending(orgID, now); len(pending) != 0 {
		t.Errorf("expired invitation listed as pending")
	}
	if byEmail, _ := repo.ListPendingForEmail("late@example.com", now); len(byEmail) != 0 {
		t.Errorf("expired invitation found as pending for the address")
	}
	// It cannot be claimed either — expiry is enforced in the UPDATE.
	if won, err := repo.MarkAccepted(expired.ID, now); err != nil || won {
		t.Errorf("MarkAccepted on an expired row = %v, %v; want false", won, err)
	}
	if err := repo.DeleteExpired(now); err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if got, _ := repo.FindByID(expired.ID); got != nil {
		t.Error("purge left the expired row behind")
	}
}

// A deleted workspace takes its invitations with it: the row is a credential
// into that workspace and must not outlive it.
func TestInvitationsCascadeWithTheWorkspace(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewInvitationRepository(db)

	orgID := saveTestOrg(t, db, "Doomed")
	inv := saveInvitation(t, repo, orgID, "someone@example.com", orgs.RoleMember, "raw-token-3",
		time.Now().UTC().Add(time.Hour), nil)
	if _, err := db.Exec(`DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
		t.Fatalf("delete organization: %v", err)
	}
	if got, err := repo.FindByID(inv.ID); err != nil || got != nil {
		t.Errorf("invitation survived its workspace: %v, %v", got, err)
	}
}

// TestSessionSweepUsesBothDeadlines covers migration 0025's other half: the
// sweeper deletes on the absolute deadline AND on idleness.
func TestSessionSweepUsesBothDeadlines(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	user := saveTestUser(t, repo, "sweeper@example.com", users.ProviderPassword, true)
	now := time.Now().UTC().Truncate(time.Microsecond)
	save := func(name string, created, lastSeen, expires time.Time) string {
		t.Helper()
		s := &users.Session{
			ID:         uuid.New().String(),
			UserID:     user.ID,
			TokenHash:  users.HashToken(name),
			ExpiresAt:  expires,
			CreatedAt:  created,
			LastSeenAt: lastSeen,
		}
		if err := repo.SaveSession(s); err != nil {
			t.Fatalf("SaveSession(%s): %v", name, err)
		}
		return s.ID
	}
	far := now.Add(30 * 24 * time.Hour)
	live := save("live", now.Add(-time.Hour), now.Add(-time.Minute), far)
	// Signed in long ago but used a minute ago: only the absolute deadline
	// catches it.
	old := save("old", now.Add(-40*24*time.Hour), now.Add(-time.Minute), far)
	// Signed in recently but untouched for a fortnight: only idleness does.
	idle := save("idle", now.Add(-15*24*time.Hour), now.Add(-14*24*time.Hour), far)

	if err := repo.DeleteExpiredSessions(now, users.DefaultSessionMaxAge, users.DefaultSessionIdle); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	alive := func(id string) bool {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n == 1
	}
	if !alive(live) {
		t.Error("the sweep took a live session")
	}
	if alive(old) {
		t.Error("a session past its absolute lifetime survived the sweep")
	}
	if alive(idle) {
		t.Error("an idle session survived the sweep")
	}
}

// A password change ends the account's other sessions in the database, not
// just in the domain (REQ-99).
func TestDeleteSessionsForUserKeepsTheCallersOwn(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	owner := saveTestUser(t, repo, "owner@example.com", users.ProviderPassword, true)
	other := saveTestUser(t, repo, "other@example.com", users.ProviderPassword, true)
	now := time.Now().UTC().Truncate(time.Microsecond)
	save := func(userID, token string) {
		t.Helper()
		if err := repo.SaveSession(&users.Session{
			ID:         uuid.New().String(),
			UserID:     userID,
			TokenHash:  users.HashToken(token),
			ExpiresAt:  now.Add(24 * time.Hour),
			CreatedAt:  now,
			LastSeenAt: now,
		}); err != nil {
			t.Fatalf("SaveSession: %v", err)
		}
	}
	save(owner.ID, "laptop")
	save(owner.ID, "phone")
	save(other.ID, "someone-else")

	if err := repo.DeleteSessionsForUser(owner.ID, users.HashToken("laptop")); err != nil {
		t.Fatalf("DeleteSessionsForUser: %v", err)
	}
	if s, _ := repo.FindSessionByTokenHash(users.HashToken("laptop")); s == nil {
		t.Error("the caller's own session was deleted")
	}
	if s, _ := repo.FindSessionByTokenHash(users.HashToken("phone")); s != nil {
		t.Error("another session of the same account survived")
	}
	if s, _ := repo.FindSessionByTokenHash(users.HashToken("someone-else")); s == nil {
		t.Error("another account's session was deleted")
	}
}

// SetPasswordHash writes the password and nothing else.
func TestSetPasswordHashTouchesOnlyThePassword(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	user := saveTestUser(t, repo, "pw@example.com", users.ProviderPassword, true)
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.SetPasswordHash(user.ID, "new-hash", at); err != nil {
		t.Fatalf("SetPasswordHash: %v", err)
	}
	got, err := repo.FindUserByID(user.ID)
	if err != nil || got == nil {
		t.Fatalf("FindUserByID: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Errorf("password hash = %q", got.PasswordHash)
	}
	if got.Name != user.Name || got.Email != user.Email || got.EmailVerified != user.EmailVerified {
		t.Errorf("the rest of the row changed: %+v", got)
	}
}

// Re-inviting an address the workspace already holds an unaccepted
// invitation for replaces that row instead of tripping
// idx_org_invitations_pending — which covers EVERY unaccepted row, so an
// expired leftover used to make the insert fail with a 500.
func TestReplaceReInvitesOverAnyUnacceptedRow(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewInvitationRepository(db)

	orgID := saveTestOrg(t, db, "Re-invites")
	replace := func(email, role, token string, expires time.Time) *invitations.Invitation {
		t.Helper()
		inv := &invitations.Invitation{
			ID:        uuid.New().String(),
			OrgID:     orgID,
			Email:     email,
			Role:      role,
			TokenHash: users.HashToken(token),
			ExpiresAt: expires,
			CreatedAt: time.Now().UTC(),
		}
		if err := repo.Replace(inv); err != nil {
			t.Fatalf("Replace(%s): %v", token, err)
		}
		return inv
	}
	future := time.Now().UTC().Add(invitations.DefaultTTL)

	// A live invitation is replaced by the newer one, and its link dies —
	// but the row keeps its id, so an id already handed to a client still
	// names this invitation.
	first := replace("again@example.com", orgs.RoleMember, "tok-1", future)
	second := replace("again@example.com", orgs.RoleAdmin, "tok-2", future)
	if got, _ := repo.FindByTokenHash(users.HashToken("tok-1")); got != nil {
		t.Error("the superseded link still resolves")
	}
	if second.ID != first.ID {
		t.Errorf("re-invite id = %s, want the existing row's %s", second.ID, first.ID)
	}
	if got, _ := repo.FindByID(first.ID); got == nil || got.Role != orgs.RoleAdmin {
		t.Errorf("the re-invitation under the original id = %v", got)
	}

	// An EXPIRED leftover is no different: re-inviting works, on the same row.
	expired := replace("again@example.com", orgs.RoleMember, "tok-3", time.Now().UTC().Add(-time.Hour))
	third := replace("again@example.com", orgs.RoleMember, "tok-4", future)
	if third.ID != expired.ID {
		t.Errorf("re-invite after expiry id = %s, want %s", third.ID, expired.ID)
	}
	pending, err := repo.ListPendingForEmail("AGAIN@example.com", time.Now())
	if err != nil || len(pending) != 1 || pending[0].ID != third.ID {
		t.Fatalf("pending after re-invites = %d (%v), want only the newest", len(pending), err)
	}

	// Accepted history is untouched, and the address can be invited again
	// after it has been accepted.
	if won, err := repo.MarkAccepted(third.ID, time.Now().UTC()); err != nil || !won {
		t.Fatalf("MarkAccepted = %v, %v", won, err)
	}
	fourth := replace("again@example.com", orgs.RoleMember, "tok-5", future)
	if got, _ := repo.FindByID(third.ID); got == nil || got.AcceptedAt == nil {
		t.Error("the accepted invitation was overwritten")
	}
	if got, _ := repo.FindByID(fourth.ID); got == nil {
		t.Error("the re-invite after acceptance is missing")
	}
}

// Two admins re-inviting the same address at the same moment both succeed:
// the second waits on the first and overwrites it, rather than failing the
// pending-uniqueness index.
func TestReplaceSurvivesConcurrentReInvites(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewInvitationRepository(db)

	orgID := saveTestOrg(t, db, "Concurrent re-invites")
	saveInvitation(t, repo, orgID, "race@example.com", orgs.RoleMember, "tok-race-0",
		time.Now().UTC().Add(-time.Hour), nil)

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			errs <- repo.Replace(&invitations.Invitation{
				ID:        uuid.New().String(),
				OrgID:     orgID,
				Email:     "race@example.com",
				Role:      orgs.RoleMember,
				TokenHash: users.HashToken(fmt.Sprintf("tok-race-%d", i+1)),
				ExpiresAt: time.Now().UTC().Add(invitations.DefaultTTL),
				CreatedAt: time.Now().UTC(),
			})
		}(i)
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Replace: %v", err)
		}
	}
	if pending, err := repo.ListPendingForEmail("race@example.com", time.Now()); err != nil || len(pending) != 1 {
		t.Fatalf("pending after the race = %d (%v), want 1", len(pending), err)
	}
}

// A re-invite rotates the credential and keeps the invitation's identity:
// the id an admin's client is holding — in a list, behind a Revoke button —
// must still name this invitation afterwards. The stored row comes back from
// the same write, display names joined in, with the delivery stamp cleared:
// the new link has not been sent to anybody yet.
func TestReplaceKeepsTheIDAndReturnsTheStoredRow(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewInvitationRepository(db)
	userRepo := NewUserRepository(db)

	orgID := saveTestOrg(t, db, "Desktop Machine Shop")
	admin := saveTestUser(t, userRepo, "ada@example.com", users.ProviderPassword, true)
	future := time.Now().UTC().Add(invitations.DefaultTTL)
	first := saveInvitation(t, repo, orgID, "keep@example.com", orgs.RoleMember, "tok-keep-1", future, &admin.ID)

	// Replace wrote the stored row back, names and all — Create needs no
	// second read to name the workspace and the inviter in the mail.
	if first.OrgName != "Desktop Machine Shop" || first.InvitedByName == "" {
		t.Errorf("Replace did not return the joined display names: %+v", first)
	}
	if first.LastEmailedAt != nil {
		t.Error("a brand-new invitation cannot already have been emailed")
	}

	stamped := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.MarkEmailed(first.ID, stamped); err != nil {
		t.Fatalf("MarkEmailed: %v", err)
	}
	sent, err := repo.FindPending(orgID, "Keep@Example.com", time.Now())
	if err != nil || sent == nil || sent.LastEmailedAt == nil || !sent.LastEmailedAt.Equal(stamped) {
		t.Fatalf("FindPending after MarkEmailed = %+v (%v), want the row stamped at %v", sent, err, stamped)
	}

	second := saveInvitation(t, repo, orgID, "KEEP@example.com", orgs.RoleAdmin, "tok-keep-2", future, &admin.ID)
	if second.ID != first.ID {
		t.Errorf("re-invite id = %s, want the existing row's %s", second.ID, first.ID)
	}
	if second.LastEmailedAt != nil {
		t.Errorf("the new link inherited the old delivery stamp: %v", second.LastEmailedAt)
	}
	stored, err := repo.FindByID(first.ID)
	if err != nil || stored == nil {
		t.Fatalf("FindByID after the re-invite = %+v (%v), want the invitation under its original id", stored, err)
	}
	if stored.Role != orgs.RoleAdmin || stored.TokenHash != users.HashToken("tok-keep-2") || stored.LastEmailedAt != nil {
		t.Errorf("the row was not rotated onto the new invitation: %+v", stored)
	}
	// An address with nothing live here, and another workspace, are both nil.
	if got, err := repo.FindPending(orgID, "nobody@example.com", time.Now()); err != nil || got != nil {
		t.Errorf("FindPending for an uninvited address = %+v (%v)", got, err)
	}
	if got, err := repo.FindPending(uuid.New().String(), "keep@example.com", time.Now()); err != nil || got != nil {
		t.Errorf("FindPending crossed a workspace boundary: %+v (%v)", got, err)
	}
}
