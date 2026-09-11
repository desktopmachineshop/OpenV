package users

import (
	"errors"
	"os"
	"testing"
	"time"
)

// sessionFor signs a fresh account in and returns the raw token with the
// stored session, so a test can age it by rewriting its timestamps.
func sessionFor(t *testing.T, svc *DefaultService, repo *memRepo, email string) (string, *Session) {
	t.Helper()
	if _, err := svc.Register(email, "password1", "Test"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, token, err := svc.Login(email, "password1")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	session, err := repo.FindSessionByTokenHash(HashToken(token))
	if err != nil || session == nil {
		t.Fatalf("session not stored: %v", err)
	}
	return token, session
}

func TestSessionExpiresAtTheAbsoluteDeadline(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetSessionPolicy(SessionPolicy{MaxAge: 2 * time.Hour, Idle: time.Hour})
	token, session := sessionFor(t, svc, repo, "abs@example.com")

	// Still inside both windows.
	if _, err := svc.GetBySessionToken(token); err != nil {
		t.Fatalf("fresh session: %v", err)
	}

	// Signed in three hours ago but active a minute ago: the idle window is
	// satisfied and the absolute one is not, which must still end it.
	now := time.Now()
	session.CreatedAt = now.Add(-3 * time.Hour)
	session.LastSeenAt = now.Add(-time.Minute)
	if _, err := svc.GetBySessionToken(token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("session past its absolute lifetime returned %v, want ErrSessionInvalid", err)
	}
	if _, err := svc.SessionByToken(token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("SessionByToken must apply the same deadline, got %v", err)
	}
}

func TestSessionExpiresWhenIdleTooLong(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetSessionPolicy(SessionPolicy{MaxAge: 30 * 24 * time.Hour, Idle: time.Hour})
	token, session := sessionFor(t, svc, repo, "idle@example.com")

	now := time.Now()
	// Young session, but nothing has used it for two hours.
	session.CreatedAt = now.Add(-3 * time.Hour)
	session.LastSeenAt = now.Add(-2 * time.Hour)
	if _, err := svc.GetBySessionToken(token); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("idle session returned %v, want ErrSessionInvalid", err)
	}
}

// A session written before last_seen_at existed reads as "used once, at
// sign-in" rather than as instantly idle.
func TestSessionWithNoLastSeenFallsBackToCreation(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	svc.SetSessionPolicy(SessionPolicy{MaxAge: 30 * 24 * time.Hour, Idle: time.Hour})
	token, session := sessionFor(t, svc, repo, "legacy@example.com")

	session.LastSeenAt = time.Time{}
	if _, err := svc.GetBySessionToken(token); err != nil {
		t.Errorf("a session with no last_seen_at must live on its creation time: %v", err)
	}
}

func TestSessionTouchIsThrottled(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	token, session := sessionFor(t, svc, repo, "touch@example.com")

	// Ten requests inside one touch interval: last_seen_at is already
	// current, so none of them should write.
	for i := 0; i < 10; i++ {
		if _, err := svc.GetBySessionToken(token); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if repo.touches != 0 {
		t.Errorf("touches within one interval = %d, want 0", repo.touches)
	}

	// Once the session has gone quiet for longer than the interval, the next
	// request records it.
	session.LastSeenAt = time.Now().Add(-2 * SessionTouchInterval)
	if _, err := svc.GetBySessionToken(token); err != nil {
		t.Fatalf("after the interval: %v", err)
	}
	if repo.touches != 1 {
		t.Errorf("touches after the interval = %d, want 1", repo.touches)
	}
}

func TestChangePasswordInvalidatesEveryOtherSession(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	user, err := svc.Register("owner@example.com", "old-password", "Owner")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, laptop, err := svc.Login("owner@example.com", "old-password")
	if err != nil {
		t.Fatalf("Login (laptop): %v", err)
	}
	_, phone, err := svc.Login("owner@example.com", "old-password")
	if err != nil {
		t.Fatalf("Login (phone): %v", err)
	}

	if err := svc.ChangePassword(user.ID, "wrong-password", "new-password", laptop); !errors.Is(err, ErrPasswordIncorrect) {
		t.Fatalf("wrong current password returned %v, want ErrPasswordIncorrect", err)
	}
	if err := svc.ChangePassword(user.ID, "old-password", "short", laptop); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("short new password returned %v, want ErrWeakPassword", err)
	}
	if err := svc.ChangePassword(user.ID, "old-password", "new-password", laptop); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// The browser that changed it stays signed in; the other does not.
	if _, err := svc.GetBySessionToken(laptop); err != nil {
		t.Errorf("the session that changed the password must survive: %v", err)
	}
	if _, err := svc.GetBySessionToken(phone); !errors.Is(err, ErrSessionInvalid) {
		t.Errorf("other sessions returned %v, want ErrSessionInvalid", err)
	}
	// The new password works and the old one does not.
	if _, _, err := svc.Login("owner@example.com", "new-password"); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
	if _, _, err := svc.Login("owner@example.com", "old-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password returned %v, want ErrInvalidCredentials", err)
	}
}

// The password DID change, so a sweep of the other sessions that fails
// afterwards must not be reported as a failed change: a caller told "that
// did not work" retries with a current password the server no longer holds,
// and the owner is left believing the old one still opens their account.
// The sessions that survived die at their idle deadline instead.
func TestChangePasswordSucceedsWhenTheSessionSweepFails(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	user, err := svc.Register("owner@example.com", "old-password", "Owner")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, _, err := svc.Login("owner@example.com", "old-password"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	repo.deleteSessionsErr = errors.New("database went away after the password was written")

	if err := svc.ChangePassword(user.ID, "old-password", "new-password", ""); err != nil {
		t.Fatalf("ChangePassword returned %v, want success: the password did change", err)
	}
	// And it really did change: the new one signs in, the old one does not.
	if _, _, err := svc.Login("owner@example.com", "new-password"); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
	if _, _, err := svc.Login("owner@example.com", "old-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password returned %v, want ErrInvalidCredentials", err)
	}
}

func TestChangePasswordRefusesAnSSOAccount(t *testing.T) {
	repo := newMemRepo()
	svc := NewDefaultService(repo)
	user, _, err := svc.LoginWithSSO(ProviderOIDC, "sso@example.com", "SSO", "")
	if err != nil {
		t.Fatalf("LoginWithSSO: %v", err)
	}
	if err := svc.ChangePassword(user.ID, "", "new-password", ""); !errors.Is(err, ErrNoPassword) {
		t.Errorf("SSO account returned %v, want ErrNoPassword", err)
	}
}

func TestSessionPolicyFromEnvClampsAndDefaults(t *testing.T) {
	cases := []struct {
		name             string
		maxAge, idle     string
		wantMax, wantIdl time.Duration
	}{
		{"unset uses the defaults", "", "", DefaultSessionMaxAge, DefaultSessionIdle},
		{"an operator may shorten", "12h", "45m", 12 * time.Hour, 45 * time.Minute},
		{"above the ceiling is clamped", "8760h", "8760h", DefaultSessionMaxAge, DefaultSessionIdle},
		{"unparseable falls back", "30d", "week", DefaultSessionMaxAge, DefaultSessionIdle},
		{"non-positive falls back", "0s", "-4h", DefaultSessionMaxAge, DefaultSessionIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(envSessionMaxAge, tc.maxAge)
			os.Setenv(envSessionIdle, tc.idle)
			defer os.Unsetenv(envSessionMaxAge)
			defer os.Unsetenv(envSessionIdle)

			p := SessionPolicyFromEnv()
			if p.MaxAge != tc.wantMax || p.Idle != tc.wantIdl {
				t.Errorf("policy = %v/%v, want %v/%v", p.MaxAge, p.Idle, tc.wantMax, tc.wantIdl)
			}
		})
	}
}

// A service that was never given a policy behaves as it always did.
func TestZeroSessionPolicyIsTheDefault(t *testing.T) {
	svc := NewDefaultService(newMemRepo())
	if got := svc.SessionLifetime(); got.MaxAge != DefaultSessionMaxAge || got.Idle != DefaultSessionIdle {
		t.Errorf("unconfigured policy = %v, want the defaults", got)
	}
}
