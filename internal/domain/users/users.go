package users

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Auth providers.
const (
	ProviderPassword = "password"
	ProviderGoogle   = "google"
	// ProviderOIDC marks accounts provisioned through the generic OIDC
	// single-sign-on flow (issue #225). One IdP per deployment for the MVP.
	ProviderOIDC = "oidc"
)

// SessionDuration is how long a login session stays valid.
const SessionDuration = 30 * 24 * time.Hour

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("an account with this email already exists")
	ErrSessionInvalid     = errors.New("session is invalid or expired")
)

// User is a platform account.
type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url"`
	AuthProvider string `json:"auth_provider"`
	PasswordHash string `json:"-"`
	IsAdmin      bool   `json:"is_admin"`
	// EmailNotifications is the per-user opt-out for email delivery of
	// higher-signal notifications (issue #187). Defaults TRUE; only has any
	// effect when the server has SMTP configured (email is opt-in infra).
	EmailNotifications bool      `json:"email_notifications"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

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

// Repository defines persistence for users and sessions.
type Repository interface {
	SaveUser(u *User) error
	UpdateUser(u *User) error
	FindUserByEmail(email string) (*User, error)
	FindUserByID(id string) (*User, error)
	ListUsers() ([]*User, error)
	CountUsers() (int, error)
	// SetEmailNotifications flips one user's email opt-out. Scoped by id so it
	// can only ever touch that user's own row.
	SetEmailNotifications(userID string, enabled bool) error

	SaveSession(s *Session) error
	FindSessionByTokenHash(hash string) (*Session, error)
	TouchSession(id string, lastSeen time.Time) error
	SetSessionActiveOrg(id string, orgID string) error
	DeleteSession(id string) error
	DeleteExpiredSessions(now time.Time) error
}

// Service defines user/auth domain logic.
type Service interface {
	Register(email, password, name string) (*User, error)
	Login(email, password string) (*User, string, error)
	LoginWithGoogle(email, name, avatarURL string) (*User, string, error)
	// LoginWithSSO upserts a user from a verified external identity (any OIDC
	// provider) and creates a session. provider is stored as auth_provider.
	LoginWithSSO(provider, email, name, avatarURL string) (*User, string, error)
	Logout(token string) error
	GetBySessionToken(token string) (*User, error)
	// SessionByToken returns the session record itself (for org context).
	SessionByToken(token string) (*Session, error)
	// SetActiveOrg persists the session's default workspace.
	SetActiveOrg(token, orgID string) error
	GetByID(id string) (*User, error)
	FindByEmail(email string) (*User, error)
	ListUsers() ([]*User, error)
	// SetEmailNotifications updates the caller's own email opt-out (issue #187).
	SetEmailNotifications(userID string, enabled bool) error
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new user service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// HashToken returns the stored form of a session or run token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken generates a random URL-safe token.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Register creates a password account. The first user becomes admin.
func (s *DefaultService) Register(email, password, name string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, errors.New("a valid email is required")
	}
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}
	if existing, _ := s.repo.FindUserByEmail(email); existing != nil {
		return nil, ErrEmailTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	count, err := s.repo.CountUsers()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := &User{
		ID:                 uuid.New().String(),
		Email:              email,
		Name:               strings.TrimSpace(name),
		AuthProvider:       ProviderPassword,
		PasswordHash:       string(hash),
		IsAdmin:            count == 0,
		EmailNotifications: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.repo.SaveUser(user); err != nil {
		return nil, err
	}
	return user, nil
}

// Login verifies credentials and creates a session, returning the raw token.
func (s *DefaultService) Login(email, password string) (*User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
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
// creates a session. New accounts record the given provider as auth_provider;
// existing accounts keep their original provider (an email that first signed up
// with a password is not silently re-labelled) but refresh name/avatar.
func (s *DefaultService) LoginWithSSO(provider, email, name, avatarURL string) (*User, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
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
		now := time.Now()
		user = &User{
			ID:                 uuid.New().String(),
			Email:              email,
			Name:               name,
			AvatarURL:          avatarURL,
			AuthProvider:       provider,
			IsAdmin:            count == 0,
			EmailNotifications: true,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		if err := s.repo.SaveUser(user); err != nil {
			return nil, "", err
		}
	} else {
		user.Name = name
		user.AvatarURL = avatarURL
		user.UpdatedAt = time.Now()
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
	now := time.Now()
	session := &Session{
		ID:         uuid.New().String(),
		UserID:     userID,
		TokenHash:  HashToken(token),
		ExpiresAt:  now.Add(SessionDuration),
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

// GetBySessionToken resolves a session token to its user.
func (s *DefaultService) GetBySessionToken(token string) (*User, error) {
	session, err := s.repo.FindSessionByTokenHash(HashToken(token))
	if err != nil {
		return nil, err
	}
	if session == nil || session.ExpiresAt.Before(time.Now()) {
		return nil, ErrSessionInvalid
	}
	// Touch at most opportunistically; failures don't invalidate the session.
	_ = s.repo.TouchSession(session.ID, time.Now())
	return s.repo.FindUserByID(session.UserID)
}

// SessionByToken returns the live session for a raw token.
func (s *DefaultService) SessionByToken(token string) (*Session, error) {
	session, err := s.repo.FindSessionByTokenHash(HashToken(token))
	if err != nil {
		return nil, err
	}
	if session == nil || session.ExpiresAt.Before(time.Now()) {
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

// GetByID returns a user by id.
func (s *DefaultService) GetByID(id string) (*User, error) {
	return s.repo.FindUserByID(id)
}

// FindByEmail returns a user by email, or nil if not found.
func (s *DefaultService) FindByEmail(email string) (*User, error) {
	return s.repo.FindUserByEmail(strings.ToLower(strings.TrimSpace(email)))
}

// ListUsers returns all users.
func (s *DefaultService) ListUsers() ([]*User, error) {
	return s.repo.ListUsers()
}

// SetEmailNotifications updates a user's email-notification opt-out.
func (s *DefaultService) SetEmailNotifications(userID string, enabled bool) error {
	return s.repo.SetEmailNotifications(userID, enabled)
}
