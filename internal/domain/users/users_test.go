package users

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// memRepo is a minimal in-memory Repository for the service tests.
type memRepo struct {
	users         map[string]*User
	sessions      map[string]*Session
	verifications map[string]*EmailVerification // by token hash
	// touches counts TouchSession calls, so the session tests can assert
	// that last_seen_at is not rewritten on every single request.
	touches int
	// deleteSessionsErr, when set, fails DeleteSessionsForUser — the sweep a
	// password change makes after the password itself is already stored.
	deleteSessionsErr error
	// markVerifiedErr, when set, fails MarkEmailVerified.
	markVerifiedErr error
}

func newMemRepo() *memRepo {
	return &memRepo{users: map[string]*User{}, sessions: map[string]*Session{}, verifications: map[string]*EmailVerification{}}
}

func (m *memRepo) SaveEmailVerification(v *EmailVerification) error {
	for hash, existing := range m.verifications {
		if existing.UserID == v.UserID && !existing.Used {
			delete(m.verifications, hash)
		}
	}
	m.verifications[v.TokenHash] = v
	return nil
}

// markVerifiedErr, when set, fails MarkEmailVerified.
func (m *memRepo) MarkEmailVerified(userID string, at time.Time) error {
	if m.markVerifiedErr != nil {
		return m.markVerifiedErr
	}
	user := m.users[userID]
	if user == nil {
		return errors.New("no such user")
	}
	user.EmailVerified = true
	user.EmailVerifiedAt = &at
	user.UpdatedAt = at
	return nil
}

func (m *memRepo) ConsumeEmailVerification(tokenHash string, now time.Time) (*User, error) {
	v := m.verifications[tokenHash]
	if v == nil || v.Used || !v.ExpiresAt.After(now) {
		return nil, nil
	}
	user := m.users[v.UserID]
	if user == nil {
		return nil, nil
	}
	for _, other := range m.users {
		if other.ID != user.ID && strings.EqualFold(other.Email, v.Email) {
			return nil, ErrEmailTaken
		}
	}
	v.Used = true
	user.Email = v.Email
	user.EmailVerified = true
	user.EmailVerifiedAt = &now
	user.UpdatedAt = now
	return user, nil
}

func (m *memRepo) SaveUser(u *User) error   { m.users[u.ID] = u; return nil }
func (m *memRepo) UpdateUser(u *User) error { m.users[u.ID] = u; return nil }
func (m *memRepo) FindUserByEmail(email string) (*User, error) {
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, nil
}
func (m *memRepo) FindUserByID(id string) (*User, error) { return m.users[id], nil }
func (m *memRepo) ListUsers() ([]*User, error)           { return nil, nil }
func (m *memRepo) CountUsers() (int, error)              { return len(m.users), nil }
func (m *memRepo) SetEmailNotifications(string, bool) error {
	return nil
}
func (m *memRepo) SaveSession(s *Session) error { m.sessions[s.ID] = s; return nil }
func (m *memRepo) FindSessionByTokenHash(hash string) (*Session, error) {
	for _, s := range m.sessions {
		if s.TokenHash == hash {
			return s, nil
		}
	}
	return nil, nil
}
func (m *memRepo) TouchSession(id string, lastSeen time.Time) error {
	m.touches++
	if s := m.sessions[id]; s != nil {
		s.LastSeenAt = lastSeen
	}
	return nil
}
func (m *memRepo) SetSessionActiveOrg(string, string) error { return nil }
func (m *memRepo) DeleteSession(id string) error            { delete(m.sessions, id); return nil }
func (m *memRepo) DeleteExpiredSessions(time.Time, time.Duration, time.Duration) error {
	return nil
}
func (m *memRepo) DeleteSessionsForUser(userID, exceptTokenHash string) error {
	if m.deleteSessionsErr != nil {
		return m.deleteSessionsErr
	}
	for id, s := range m.sessions {
		if s.UserID == userID && s.TokenHash != exceptTokenHash {
			delete(m.sessions, id)
		}
	}
	return nil
}
func (m *memRepo) SetPasswordHash(userID, hash string, at time.Time) error {
	u := m.users[userID]
	if u == nil {
		return nil
	}
	u.PasswordHash = hash
	u.UpdatedAt = at
	return nil
}

// TestLoginWithSSONewUserRecordsProvider: a first-time SSO login provisions an
// account labelled with the given provider.
func TestLoginWithSSONewUserRecordsProvider(t *testing.T) {
	svc := NewDefaultService(newMemRepo())
	user, token, err := svc.LoginWithSSO(ProviderOIDC, "new@example.com", "New User", "")
	if err != nil {
		t.Fatalf("LoginWithSSO: %v", err)
	}
	if token == "" {
		t.Error("expected a session token")
	}
	if user.AuthProvider != ProviderOIDC {
		t.Errorf("auth_provider = %q, want %q", user.AuthProvider, ProviderOIDC)
	}
}

// TestLoginWithSSOSameProviderProceeds: a returning SSO user with the same
// provider logs in and has profile fields refreshed.
func TestLoginWithSSOSameProviderProceeds(t *testing.T) {
	svc := NewDefaultService(newMemRepo())
	if _, _, err := svc.LoginWithSSO(ProviderOIDC, "same@example.com", "Old Name", ""); err != nil {
		t.Fatalf("first login: %v", err)
	}
	user, token, err := svc.LoginWithSSO(ProviderOIDC, "same@example.com", "New Name", "http://av")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if token == "" {
		t.Error("expected a session token on the returning login")
	}
	if user.Name != "New Name" || user.AvatarURL != "http://av" {
		t.Errorf("profile not refreshed: %+v", user)
	}
}

// TestLoginWithSSOCrossProviderRejected: an SSO login for an email that already
// belongs to a different-provider account is rejected with ErrProviderMismatch
// and does not mutate the existing account (issue #242).
func TestLoginWithSSOCrossProviderRejected(t *testing.T) {
	svc := NewDefaultService(newMemRepo())

	// Existing password account.
	existing, err := svc.Register("collide@example.com", "hunter2pw", "Pw User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// OIDC login for the same email must be refused.
	_, token, err := svc.LoginWithSSO(ProviderOIDC, "collide@example.com", "Impostor", "")
	if !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("expected ErrProviderMismatch, got err=%v", err)
	}
	if token != "" {
		t.Error("no session token should be issued on a rejected cross-provider login")
	}

	// Existing account untouched.
	after, _ := svc.FindByEmail("collide@example.com")
	if after.AuthProvider != ProviderPassword {
		t.Errorf("provider re-labelled to %q, want %q", after.AuthProvider, ProviderPassword)
	}
	if after.Name != "Pw User" {
		t.Errorf("name overwritten to %q by rejected login", after.Name)
	}
	if after.ID != existing.ID {
		t.Errorf("account identity changed")
	}
}

// TestLoginWithGoogleThenOIDCRejected: Google and OIDC are distinct providers,
// so a Google account cannot be entered through the generic OIDC flow.
func TestLoginWithGoogleThenOIDCRejected(t *testing.T) {
	svc := NewDefaultService(newMemRepo())
	if _, _, err := svc.LoginWithGoogle("g@example.com", "G User", ""); err != nil {
		t.Fatalf("google login: %v", err)
	}
	if _, _, err := svc.LoginWithSSO(ProviderOIDC, "g@example.com", "G User", ""); !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("expected ErrProviderMismatch for google->oidc, got %v", err)
	}
	// But a repeat Google login still works.
	if _, _, err := svc.LoginWithGoogle("g@example.com", "G User", ""); err != nil {
		t.Errorf("same-provider google login should proceed: %v", err)
	}
}

// --- email verification ----------------------------------------------------

func TestRegisterVerifiedWhenPolicyOff(t *testing.T) {
	svc := NewDefaultService(newMemRepo())
	user, err := svc.Register("off@example.com", "password1", "Off")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !user.EmailVerified || user.EmailVerifiedAt == nil {
		t.Error("with no verification policy the account must be born verified")
	}
	if _, _, err := svc.IssueEmailVerification(user.ID, ""); !errors.Is(err, ErrAlreadyVerified) {
		t.Errorf("issuing for a verified account returned %v, want ErrAlreadyVerified", err)
	}
}

func TestRegisterPendingWhenPolicyOn(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, err := svc.Register("pending@example.com", "password1", "Pending")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.EmailVerified || user.EmailVerifiedAt != nil {
		t.Fatal("with verification required the account must start unverified")
	}

	// SSO accounts are verified by their provider.
	sso, _, err := svc.LoginWithSSO(ProviderOIDC, "sso@example.com", "SSO", "")
	if err != nil {
		t.Fatalf("LoginWithSSO: %v", err)
	}
	if !sso.EmailVerified {
		t.Error("an SSO account must be verified at creation")
	}

	first, sentTo, err := svc.IssueEmailVerification(user.ID, "")
	if err != nil {
		t.Fatalf("IssueEmailVerification: %v", err)
	}
	if sentTo != "pending@example.com" {
		t.Errorf("sentTo = %q", sentTo)
	}
	second, _, err := svc.IssueEmailVerification(user.ID, "")
	if err != nil {
		t.Fatalf("second issue: %v", err)
	}
	// Issuing again replaces the pending link: the first token is dead.
	if _, err := svc.ConfirmEmailVerification(first); !errors.Is(err, ErrVerificationInvalid) {
		t.Errorf("superseded token returned %v, want ErrVerificationInvalid", err)
	}
	if _, err := svc.ConfirmEmailVerification(""); !errors.Is(err, ErrVerificationInvalid) {
		t.Errorf("empty token returned %v, want ErrVerificationInvalid", err)
	}
	verified, err := svc.ConfirmEmailVerification(second)
	if err != nil {
		t.Fatalf("ConfirmEmailVerification: %v", err)
	}
	if !verified.EmailVerified || verified.EmailVerifiedAt == nil || verified.ID != user.ID {
		t.Errorf("confirm did not verify the user: %+v", verified)
	}
	// Single use.
	if _, err := svc.ConfirmEmailVerification(second); !errors.Is(err, ErrVerificationInvalid) {
		t.Errorf("reused token returned %v, want ErrVerificationInvalid", err)
	}
}

func TestVerificationExpiry(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, _ := svc.Register("late@example.com", "password1", "Late")
	token, _, err := svc.IssueEmailVerification(user.ID, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	repo.verifications[HashToken(token)].ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := svc.ConfirmEmailVerification(token); !errors.Is(err, ErrVerificationInvalid) {
		t.Errorf("expired token returned %v, want ErrVerificationInvalid", err)
	}
}

func TestVerificationChangeOfAddress(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, _ := svc.Register("typo@example.com", "password1", "Typo")
	if _, err := svc.Register("taken@example.com", "password1", "Taken"); err != nil {
		t.Fatalf("Register taken: %v", err)
	}

	if _, _, err := svc.IssueEmailVerification(user.ID, "Taken@Example.com"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("change to a taken address returned %v, want ErrEmailTaken", err)
	}
	if _, _, err := svc.IssueEmailVerification(user.ID, "not-an-address"); err == nil {
		t.Error("an invalid address must be refused")
	}
	token, sentTo, err := svc.IssueEmailVerification(user.ID, " Fixed@Example.com ")
	if err != nil {
		t.Fatalf("issue change: %v", err)
	}
	if sentTo != "fixed@example.com" {
		t.Errorf("sentTo = %q, want the normalised new address", sentTo)
	}
	if repo.users[user.ID].Email != "typo@example.com" {
		t.Error("the account address must not change before the link is confirmed")
	}

	verified, err := svc.ConfirmEmailVerification(token)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if verified.Email != "fixed@example.com" || !verified.EmailVerified {
		t.Errorf("confirm did not apply the new address: %+v", verified)
	}
}

func TestVerificationConfirmRefusesAddressTakenMeanwhile(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, _ := svc.Register("first@example.com", "password1", "First")
	token, _, err := svc.IssueEmailVerification(user.ID, "new@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// Someone registers the new address before the link is clicked.
	if _, err := svc.Register("new@example.com", "password1", "Other"); err != nil {
		t.Fatalf("Register other: %v", err)
	}
	if _, err := svc.ConfirmEmailVerification(token); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("confirm returned %v, want ErrEmailTaken", err)
	}
	if repo.users[user.ID].EmailVerified {
		t.Error("a refused confirm must leave the account unverified")
	}
}

// MarkEmailVerified records proof of control that did not come from an
// emailed link — an invitation token delivered to the account's own address.
// It touches only the verification columns, and verifying twice is a no-op.
func TestMarkEmailVerified(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetEmailVerificationPolicy(EmailVerificationPolicy{Required: true})
	user, err := svc.Register("invited@example.com", "a-password", "Invited")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.EmailVerified {
		t.Fatal("a password account on a verifying deployment starts unverified")
	}

	verified, err := svc.MarkEmailVerified(user.ID)
	if err != nil {
		t.Fatalf("MarkEmailVerified: %v", err)
	}
	if !verified.EmailVerified || verified.EmailVerifiedAt == nil {
		t.Errorf("user = %+v, want verified with a timestamp", verified)
	}
	if verified.Email != "invited@example.com" {
		t.Errorf("email = %q, want the address left alone", verified.Email)
	}
	// Again is a no-op, not an error.
	if _, err := svc.MarkEmailVerified(user.ID); err != nil {
		t.Errorf("verifying twice returned %v", err)
	}
	if _, err := svc.MarkEmailVerified("no-such-user"); err == nil {
		t.Error("an unknown account must be reported")
	}
}

// The refusal names the length the service actually enforces. Both come from
// MinPasswordLength, so raising the minimum cannot leave the message telling
// people a number the server no longer uses.
func TestWeakPasswordMessageNamesTheMinimum(t *testing.T) {
	want := fmt.Sprintf("password must be at least %d characters", MinPasswordLength)
	if ErrWeakPassword.Error() != want {
		t.Errorf("ErrWeakPassword = %q, want %q", ErrWeakPassword.Error(), want)
	}
	// And a password one character short really is refused with it.
	svc := NewDefaultService(newMemRepo())
	short := strings.Repeat("a", MinPasswordLength-1)
	if _, err := svc.Register("short@example.com", short, "Short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("Register with %d characters returned %v, want ErrWeakPassword", len(short), err)
	}
}
