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

// EmailVerificationTTL is how long an emailed verification link stays valid.
// Long enough to survive a slow inbox, short enough that a link forwarded or
// left in a mailbox is not a standing credential.
const EmailVerificationTTL = 24 * time.Hour

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("an account with this email already exists")
	ErrSessionInvalid     = errors.New("session is invalid or expired")
	// ErrProviderMismatch is returned by LoginWithSSO when an account already
	// exists for the email but was created with a different auth_provider (e.g.
	// a password or Google account, when an OIDC login arrives). We refuse to
	// silently sign the caller into that account: proving control of the same
	// email string at a second IdP is not proof it is the same person, so
	// auto-linking would be an account-takeover vector. Explicit verified
	// account linking is a documented follow-up (issue #242).
	ErrProviderMismatch = errors.New("an account with this email already exists with a different sign-in method")
	// ErrVerificationInvalid covers every way a verification link can fail —
	// unknown, already used, expired — with one message, so a probe learns
	// nothing about which token values exist.
	ErrVerificationInvalid = errors.New("verification link is invalid or has expired")
	ErrAlreadyVerified     = errors.New("email is already verified")
)

// EmailVerificationPolicy says whether password accounts must prove control
// of their address before the app serves them (SEC-15 / REQ-95). Required is
// true only when the deployment can actually send mail and the operator has
// not switched it off (see notify.VerificationPolicyFromEnv); with it false,
// accounts are born verified and nothing is enforced, which keeps a default
// self-hosted stack, CI and the E2E suite exactly as they were.
type EmailVerificationPolicy struct {
	Required bool
}

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
	EmailNotifications bool `json:"email_notifications"`
	// PushNotifications is the per-user opt-in for web push delivery of the
	// same higher-signal notification types (REQ-109). Defaults FALSE: push
	// only reaches a device the member has explicitly granted permission on,
	// so there is nothing to opt out of until they opt in.
	PushNotifications bool `json:"push_notifications"`
	// EmailVerified says the account has proved control of Email by following
	// an emailed link (or was created by an identity provider that asserted a
	// verified address). The auth middleware refuses an unverified session
	// while EmailVerificationPolicy.Required is on.
	EmailVerified   bool       `json:"email_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// EmailVerification is one emailed verification link. The raw token is never
// stored, only its hash; Email is the address the link was sent to, which
// becomes the account's address when the link is confirmed (that is how a
// change of address works: the new address is never applied unproven).
type EmailVerification struct {
	ID        string
	UserID    string
	Email     string
	TokenHash string
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
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
	// SetPushNotifications flips one user's web-push opt-in. Scoped by id,
	// same as the email flag.
	SetPushNotifications(userID string, enabled bool) error
	// SaveEmailVerification stores v after discarding the user's unused
	// pending links, so at most one link is live per account.
	SaveEmailVerification(v *EmailVerification) error
	// ConsumeEmailVerification atomically spends the link with this hash
	// (unused, unexpired at now), marks its user verified at now and applies
	// the link's email to the user row. Returns the updated user; nil, nil
	// when the hash is unknown, spent or expired; ErrEmailTaken when the
	// address now belongs to another account.
	ConsumeEmailVerification(tokenHash string, now time.Time) (*User, error)

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
	// SetPushNotifications updates the caller's own web-push opt-in (REQ-109).
	SetPushNotifications(userID string, enabled bool) error
	// IssueEmailVerification mints a fresh verification link for the user.
	// email "" means the account's current address; any other address is a
	// change request — the link goes there and the account's address changes
	// only when it is confirmed. Returns the raw token (for the email) and
	// the normalised address it was issued for. Errors: ErrAlreadyVerified,
	// ErrEmailTaken, or an invalid address.
	IssueEmailVerification(userID, email string) (token, sentTo string, err error)
	// ConfirmEmailVerification spends a raw token: single use, valid for
	// EmailVerificationTTL. It marks the user verified, applies the link's
	// address, and returns the user. ErrVerificationInvalid for anything
	// unknown, used or expired; ErrEmailTaken when the address was claimed
	// by another account in the meantime.
	ConfirmEmailVerification(token string) (*User, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo   Repository
	policy EmailVerificationPolicy
}

// SetEmailVerificationPolicy wires the deployment's verification policy
// (wiring-time only). It decides whether Register creates accounts verified
// (policy off) or pending a link (policy on).
func (s *DefaultService) SetEmailVerificationPolicy(p EmailVerificationPolicy) {
	s.policy = p
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
	// With no way to send a link there is nothing to wait for: the account
	// is verified at birth and the feature is inert.
	if !s.policy.Required {
		user.EmailVerified = true
		user.EmailVerifiedAt = &now
	}
	if err := s.repo.SaveUser(user); err != nil {
		return nil, err
	}
	return user, nil
}

// IssueEmailVerification mints a verification link for the user; see the
// Service interface for the address semantics.
func (s *DefaultService) IssueEmailVerification(userID, email string) (string, string, error) {
	user, err := s.repo.FindUserByID(userID)
	if err != nil {
		return "", "", err
	}
	if user == nil {
		return "", "", errors.New("user not found")
	}
	if user.EmailVerified {
		return "", "", ErrAlreadyVerified
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		email = user.Email
	}
	if !strings.Contains(email, "@") {
		return "", "", errors.New("a valid email is required")
	}
	if email != user.Email {
		if existing, _ := s.repo.FindUserByEmail(email); existing != nil && existing.ID != user.ID {
			return "", "", ErrEmailTaken
		}
	}
	token, err := NewToken()
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	v := &EmailVerification{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Email:     email,
		TokenHash: HashToken(token),
		ExpiresAt: now.Add(EmailVerificationTTL),
		CreatedAt: now,
	}
	if err := s.repo.SaveEmailVerification(v); err != nil {
		return "", "", err
	}
	return token, email, nil
}

// ConfirmEmailVerification spends a raw verification token.
func (s *DefaultService) ConfirmEmailVerification(token string) (*User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrVerificationInvalid
	}
	user, err := s.repo.ConsumeEmailVerification(HashToken(token), time.Now())
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrVerificationInvalid
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
// creates a session. New accounts record the given provider as auth_provider.
// An existing account is entered only when it was created with the SAME
// provider (its name/avatar are refreshed); an account created with a different
// provider is rejected with ErrProviderMismatch rather than silently
// auto-linked (issue #242) — proving control of the same email at a second IdP
// is not proof of the same person.
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

// SetPushNotifications updates a user's web-push opt-in.
func (s *DefaultService) SetPushNotifications(userID string, enabled bool) error {
	return s.repo.SetPushNotifications(userID, enabled)
}
