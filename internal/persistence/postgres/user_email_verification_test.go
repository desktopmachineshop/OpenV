package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/users"
)

func saveTestUser(t *testing.T, repo *UserRepository, email, provider string, verified bool) *users.User {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	u := &users.User{
		ID:            uuid.New().String(),
		Email:         email,
		Name:          "Test",
		AuthProvider:  provider,
		EmailVerified: verified,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if verified {
		u.EmailVerifiedAt = &now
	}
	if err := repo.SaveUser(u); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}
	return u
}

func saveLink(t *testing.T, repo *UserRepository, userID, email, token string, expires time.Time) {
	t.Helper()
	if err := repo.SaveEmailVerification(&users.EmailVerification{
		ID:        uuid.New().String(),
		UserID:    userID,
		Email:     email,
		TokenHash: users.HashToken(token),
		ExpiresAt: expires,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveEmailVerification: %v", err)
	}
}

// TestEmailVerificationRoundTrip exercises migration 0024 and the repository:
// the columns persist, a link is spent exactly once, an expired link is
// refused, and a link whose address was taken meanwhile leaves the account
// unverified.
func TestEmailVerificationRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	pending := saveTestUser(t, repo, "pending@example.com", users.ProviderPassword, false)
	got, err := repo.FindUserByID(pending.ID)
	if err != nil {
		t.Fatalf("FindUserByID: %v", err)
	}
	if got.EmailVerified || got.EmailVerifiedAt != nil {
		t.Fatalf("unverified user came back as %+v", got)
	}
	born := saveTestUser(t, repo, "born@example.com", users.ProviderPassword, true)
	if got, _ := repo.FindUserByID(born.ID); !got.EmailVerified || got.EmailVerifiedAt == nil {
		t.Fatalf("verified-at-birth user came back as %+v", got)
	}

	// Two links: the second replaces the first.
	saveLink(t, repo, pending.ID, pending.Email, "first", time.Now().Add(time.Hour))
	saveLink(t, repo, pending.ID, "fixed@example.com", "second", time.Now().Add(time.Hour))
	if u, err := repo.ConsumeEmailVerification(users.HashToken("first"), time.Now()); err != nil || u != nil {
		t.Fatalf("superseded link: user=%v err=%v, want nil,nil", u, err)
	}
	u, err := repo.ConsumeEmailVerification(users.HashToken("second"), time.Now())
	if err != nil {
		t.Fatalf("ConsumeEmailVerification: %v", err)
	}
	if u == nil || !u.EmailVerified || u.EmailVerifiedAt == nil || u.Email != "fixed@example.com" {
		t.Fatalf("consume returned %+v", u)
	}
	if u, err := repo.ConsumeEmailVerification(users.HashToken("second"), time.Now()); err != nil || u != nil {
		t.Fatalf("second consume: user=%v err=%v, want nil,nil", u, err)
	}

	// Expired.
	late := saveTestUser(t, repo, "late@example.com", users.ProviderPassword, false)
	saveLink(t, repo, late.ID, late.Email, "late", time.Now().Add(-time.Minute))
	if u, err := repo.ConsumeEmailVerification(users.HashToken("late"), time.Now()); err != nil || u != nil {
		t.Fatalf("expired link: user=%v err=%v, want nil,nil", u, err)
	}

	// Address taken between issue and confirm: refused, account unchanged.
	mover := saveTestUser(t, repo, "mover@example.com", users.ProviderPassword, false)
	saveLink(t, repo, mover.ID, "wanted@example.com", "mover", time.Now().Add(time.Hour))
	saveTestUser(t, repo, "wanted@example.com", users.ProviderPassword, true)
	if _, err := repo.ConsumeEmailVerification(users.HashToken("mover"), time.Now()); !errors.Is(err, users.ErrEmailTaken) {
		t.Fatalf("taken address returned %v, want ErrEmailTaken", err)
	}
	if got, _ := repo.FindUserByID(mover.ID); got.EmailVerified || got.Email != "mover@example.com" {
		t.Fatalf("refused confirm changed the account: %+v", got)
	}

	if err := repo.DeleteSpentEmailVerifications(time.Now()); err != nil {
		t.Fatalf("DeleteSpentEmailVerifications: %v", err)
	}
}

// MarkEmailVerified records proof of control that did not come from an
// emailed link — an invitation token delivered to the account's own address.
// It writes only the verification columns: the address itself is never
// touched, so it cannot move an account onto one it has not proved.
func TestMarkEmailVerifiedTouchesOnlyTheVerificationColumns(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	user := saveTestUser(t, repo, "invited@example.com", users.ProviderPassword, false)
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.MarkEmailVerified(user.ID, at); err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
	got, err := repo.FindUserByID(user.ID)
	if err != nil || got == nil {
		t.Fatalf("FindUserByID: %v", err)
	}
	if !got.EmailVerified || got.EmailVerifiedAt == nil {
		t.Errorf("user = %+v, want verified with a timestamp", got)
	}
	if got.Email != "invited@example.com" || got.Name != "Test" || got.AuthProvider != users.ProviderPassword {
		t.Errorf("MarkEmailVerified changed more than the verification columns: %+v", got)
	}

	// Verifying again keeps the original timestamp: when they proved it is
	// history, not something a later write resets.
	if err := repo.MarkEmailVerified(user.ID, at.Add(time.Hour)); err != nil {
		t.Fatalf("second MarkEmailVerified: %v", err)
	}
	again, _ := repo.FindUserByID(user.ID)
	if !again.EmailVerifiedAt.Equal(*got.EmailVerifiedAt) {
		t.Errorf("email_verified_at moved from %v to %v", got.EmailVerifiedAt, again.EmailVerifiedAt)
	}
}

// TestEmailVerificationBackfill re-runs the 0024 backfill on rows that
// predate it: SSO accounts become verified, password accounts stay pending.
func TestEmailVerificationBackfill(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	sso := saveTestUser(t, repo, "sso@example.com", users.ProviderGoogle, false)
	pw := saveTestUser(t, repo, "pw@example.com", users.ProviderPassword, false)
	if _, err := db.Exec(`
		UPDATE users SET email_verified = TRUE, email_verified_at = COALESCE(email_verified_at, created_at)
		WHERE auth_provider IN ('google', 'oidc') AND NOT email_verified
	`); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got, _ := repo.FindUserByID(sso.ID); !got.EmailVerified || got.EmailVerifiedAt == nil {
		t.Errorf("SSO row not backfilled: %+v", got)
	}
	if got, _ := repo.FindUserByID(pw.ID); got.EmailVerified {
		t.Errorf("password row was backfilled: %+v", got)
	}
}
