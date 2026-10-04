// Sessions (REQ-99): signing in with a password or through an identity
// provider, the session that opens, resolving a session token against the
// session policy (session_policy.go) on every request, the session's
// workspace, and signing out.

package users

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Session is a logged-in browser session. Token is only present at creation.
type Session struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	TokenHash   string    `json:"-"`
	ActiveOrgID string    `json:"active_org_id,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// SetSessionPolicy wires the deployment's session lifetime bounds
// (wiring-time only). The zero policy means the defaults.
func (s *DefaultService) SetSessionPolicy(p SessionPolicy) {
	s.sessions = p.Normalized()
}

// SessionLifetime reports the session bounds in force.
func (s *DefaultService) SessionLifetime() SessionPolicy {
	return s.sessions.Normalized()
}

// Login verifies credentials and creates a session, returning the raw token.
func (s *DefaultService) Login(email, password string) (*User, string, error) {
	email = NormalizeEmail(email)
	user, err := s.repo.FindUserByEmail(email)
	if err != nil || user == nil || user.PasswordHash == "" {
		return nil, "", ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, "", ErrInvalidCredentials
	}
	token, err := s.createSession(user.ID)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// LoginWithGoogle upserts a user from a verified Google identity and creates a session.
func (s *DefaultService) LoginWithGoogle(email, name, avatarURL string) (*User, string, error) {
	return s.LoginWithSSO(ProviderGoogle, email, name, avatarURL)
}

// LoginWithSSO upserts a user from a verified external (OIDC) identity and
// creates a session. New accounts record the given provider as auth_provider.
// An existing account is entered only when it was created with the SAME
// provider (its name/avatar are refreshed); an account created with a different
// provider is rejected with ErrProviderMismatch rather than silently
// auto-linked (issue #242) — proving control of the same email at a second IdP
// is not proof of the same person.
func (s *DefaultService) LoginWithSSO(provider, email, name, avatarURL string) (*User, string, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, "", errors.New("sso identity has no email")
	}
	if provider == "" {
		provider = ProviderOIDC
	}

	user, err := s.repo.FindUserByEmail(email)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		count, err := s.repo.CountUsers()
		if err != nil {
			return nil, "", err
		}
		now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 154)
		// The identity provider asserted a verified address (both callbacks
		// refuse anything else), so the account is verified from the start.
		user = &User{
			ID:                 uuid.New().String(),
			Email:              email,
			Name:               name,
			AvatarURL:          avatarURL,
			AuthProvider:       provider,
			IsAdmin:            count == 0,
			EmailNotifications: true,
			EmailVerified:      true,
			EmailVerifiedAt:    &now,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := s.repo.SaveUser(user); err != nil {
			return nil, "", err
		}
	} else {
		// Guard against cross-provider account takeover (issue #242). An account
		// that first signed up with a different method (password, Google, another
		// OIDC provider) must not be silently entered through this provider just
		// because the two share an email string. Same-provider logins proceed and
		// refresh profile fields; a mismatch is rejected for the caller to resolve
		// (explicit verified linking is a follow-up).
		if user.AuthProvider != "" && user.AuthProvider != provider {
			return nil, "", ErrProviderMismatch
		}
		user.Name = name
		// A picture the member uploaded outranks the provider's: it is only
		// refreshed from the provider while none is stored.
		if user.AvatarPath == "" {
			user.AvatarURL = avatarURL
		}
		user.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateUser(user); err != nil {
			return nil, "", err
		}
	}

	token, err := s.createSession(user.ID)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

func (s *DefaultService) createSession(userID string) (string, error) {
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	// The session's times are TIMESTAMP columns, which keep the wall clock
	// they are sent and are read back as UTC: a local time.Now() moved the
	// deadlines sessionLive enforces by the server's offset (#379 bug 154).
	now := time.Now().UTC()
	session := &Session{
		ID:         uuid.New().String(),
		UserID:     userID,
		TokenHash:  HashToken(token),
		ExpiresAt:  now.Add(s.SessionLifetime().MaxAge),
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if err := s.repo.SaveSession(session); err != nil {
		return "", err
	}
	return token, nil
}

// Logout deletes the session for the given token. Unknown tokens are a no-op.
func (s *DefaultService) Logout(token string) error {
	session, err := s.repo.FindSessionByTokenHash(HashToken(token))
	if err != nil || session == nil {
		return nil
	}
	return s.repo.DeleteSession(session.ID)
}

// sessionLive applies both halves of REQ-99 to a stored session: the
// absolute deadline from sign-in and the idle deadline from its last
// request. The policy is re-applied on every read rather than trusted from
// expires_at alone, so shortening OPENV_SESSION_MAX_AGE takes effect for the
// sessions that already exist instead of only the next ones.
//
// It compares instants, so now may be in any zone; what it relies on is that
// the session's times were stored as UTC wall clocks, which is how a
// TIMESTAMP column's value is read back (#379 bug 154).
func (s *DefaultService) sessionLive(session *Session, now time.Time) bool {
	if session == nil || !session.ExpiresAt.After(now) {
		return false
	}
	p := s.SessionLifetime()
	if now.Sub(session.CreatedAt) >= p.MaxAge {
		return false
	}
	return now.Sub(lastSeen(session)) < p.Idle
}

// lastSeen falls back to creation for a row written before last_seen_at
// existed, which reads as "used once, at sign-in".
func lastSeen(session *Session) time.Time {
	if session.LastSeenAt.IsZero() {
		return session.CreatedAt
	}
	return session.LastSeenAt
}

// GetBySessionToken resolves a session token to its user.
func (s *DefaultService) GetBySessionToken(token string) (*User, error) {
	session, err := s.repo.FindSessionByTokenHash(HashToken(token))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC() // touches a TIMESTAMP (#379 bug 154)
	if !s.sessionLive(session, now) {
		return nil, ErrSessionInvalid
	}
	// One write per SessionTouchInterval at most: idle expiry is measured in
	// days, so an UPDATE on every authenticated request buys nothing.
	// Opportunistic — a failed touch never invalidates the session.
	if now.Sub(lastSeen(session)) >= SessionTouchInterval {
		_ = s.repo.TouchSession(session.ID, now)
	}
	return s.repo.FindUserByID(session.UserID)
}

// SessionByToken returns the live session for a raw token.
func (s *DefaultService) SessionByToken(token string) (*Session, error) {
	session, err := s.repo.FindSessionByTokenHash(HashToken(token))
	if err != nil {
		return nil, err
	}
	if !s.sessionLive(session, time.Now()) {
		return nil, ErrSessionInvalid
	}
	return session, nil
}

// SetActiveOrg persists the session's default workspace.
func (s *DefaultService) SetActiveOrg(token, orgID string) error {
	session, err := s.SessionByToken(token)
	if err != nil {
		return err
	}
	return s.repo.SetSessionActiveOrg(session.ID, orgID)
}
