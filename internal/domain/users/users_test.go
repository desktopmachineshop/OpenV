package users

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// memRepo is a minimal in-memory Repository for the service tests.
type memRepo struct {
	users    map[string]*User
	sessions map[string]*Session
}

func newMemRepo() *memRepo {
	return &memRepo{users: map[string]*User{}, sessions: map[string]*Session{}}
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
func (m *memRepo) TouchSession(string, time.Time) error     { return nil }
func (m *memRepo) SetSessionActiveOrg(string, string) error { return nil }
func (m *memRepo) DeleteSession(id string) error            { delete(m.sessions, id); return nil }
func (m *memRepo) DeleteExpiredSessions(time.Time) error    { return nil }

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
