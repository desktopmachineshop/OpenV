package users

import (
	"errors"
	"fmt"
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

var (
	// ErrLastAdmin refuses demoting the only platform admin (REQ-155).
	ErrLastAdmin = errors.New("the last platform admin cannot be demoted")
	// ErrUserNotFound names an unknown account id.
	ErrUserNotFound       = errors.New("user not found")
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
	// ErrWeakPassword, ErrPasswordIncorrect and ErrNoPassword are the three
	// ways a password change refuses. They are separate errors so the handler
	// can answer 400, 403 and 409 instead of one indiscriminate 400.
	// The length in the message is derived from MinPasswordLength, so the
	// rule and what the person is told can never drift apart.
	ErrWeakPassword      = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordIncorrect = errors.New("current password is incorrect")
	ErrNoPassword        = errors.New("this account signs in through an identity provider and has no password to change")
)

// User is a platform account.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	// AvatarPath and AvatarMime locate an uploaded profile picture on disk
	// (under the uploads directory); both empty when none has been
	// uploaded, in which case AvatarURL is whatever the identity provider
	// supplied, or empty. Never serialised — clients learn only HasAvatar
	// and fetch the bytes through AvatarURL.
	AvatarPath string `json:"-"`
	AvatarMime string `json:"-"`
	// HasAvatar reports whether an uploaded picture is stored (AvatarPath != "").
	HasAvatar    bool   `json:"has_avatar"`
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
	// DefaultOrgID is the workspace a sign-in lands in when the member has
	// chosen one (REQ-156); empty means the personal workspace. It is a
	// choice, not a grant: membership is checked wherever it is used, so a
	// member who has since left the workspace lands in their personal one.
	DefaultOrgID string `json:"default_org_id"`
	// BillingTrialUsedAt is when this person first bought a subscription
	// with a free trial, on any workspace; nil until then. One trial per
	// buyer: a person can create workspaces freely, each its own billing
	// customer, so the trial is keyed on the human. Never serialised.
	BillingTrialUsedAt *time.Time `json:"-"`
	// EmailVerified says the account has proved control of Email by following
	// an emailed link (or was created by an identity provider that asserted a
	// verified address). The auth middleware refuses an unverified session
	// while EmailVerificationPolicy.Required is on.
	EmailVerified   bool       `json:"email_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
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
	// SetDefaultOrg records the workspace one user's sign-in lands in; ""
	// clears it.
	SetDefaultOrg(userID, orgID string) error
	// SetBillingTrialUsed records that the user's one free trial is spent.
	SetBillingTrialUsed(userID string, at time.Time) error
	// SetPushNotifications flips one user's web-push opt-in. Scoped by id,
	// same as the email flag.
	SetPushNotifications(userID string, enabled bool) error
	// SetAvatar writes only the three avatar columns (path, MIME type and
	// the URL the picture is served at); empty strings clear them.
	SetAvatar(userID, path, mime, url string, at time.Time) error
	// SaveEmailVerification stores v after discarding the user's unused
	// pending links, so at most one link is live per account.
	SaveEmailVerification(v *EmailVerification) error
	// MarkEmailVerified records that an account has proved control of its
	// CURRENT address by some other means than an emailed link — today, an
	// invitation token that reached that mailbox. It touches only the
	// verification columns, never the address itself.
	MarkEmailVerified(userID string, at time.Time) error
	// ConsumeEmailVerification atomically spends the link with this hash
	// (unused, unexpired at now), marks its user verified at now and applies
	// the link's email to the user row. Returns the updated user; nil, nil
	// when the hash is unknown, spent or expired; ErrEmailTaken when the
	// address now belongs to another account.
	ConsumeEmailVerification(tokenHash string, now time.Time) (*User, error)
	// SavePasswordReset stores a reset link after discarding the user's
	// unused pending ones, so at most one link is live per account.
	SavePasswordReset(v *PasswordReset) error
	// ConsumePasswordReset atomically spends the link with this hash
	// (unused, unexpired at now) and returns it; nil, nil when the hash is
	// unknown, spent or expired.
	ConsumePasswordReset(tokenHash string, now time.Time) (*PasswordReset, error)

	SaveSession(s *Session) error
	FindSessionByTokenHash(hash string) (*Session, error)
	TouchSession(id string, lastSeen time.Time) error
	SetSessionActiveOrg(id string, orgID string) error
	DeleteSession(id string) error
	// DeleteSessionsForUser removes every session of one account except the
	// one whose token hashes to exceptTokenHash ("" removes them all). It is
	// how a password change signs the other browsers out (REQ-99).
	DeleteSessionsForUser(userID, exceptTokenHash string) error
	// SetPasswordHash writes only the password column, so a change cannot
	// carry a stale copy of the rest of the row back into the database.
	SetPasswordHash(userID, hash string, at time.Time) error
	// DeleteExpiredSessions sweeps sessions past their stored expiry, older
	// than maxAge, or unused for longer than idle.
	DeleteExpiredSessions(now time.Time, maxAge, idle time.Duration) error
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
	// ChangePassword replaces a password account's password and, per REQ-99,
	// invalidates every session of that account except the caller's own
	// (keepToken; "" keeps none). Errors: ErrNoPassword when the account has
	// no password, ErrPasswordIncorrect, ErrWeakPassword — all of them
	// before anything is written. Once the new password is stored the change
	// has happened, so a sweep of the other sessions that fails is logged,
	// not returned: those sessions expire at their idle deadline anyway, and
	// reporting failure would tell the owner their old password still works.
	ChangePassword(userID, currentPassword, newPassword, keepToken string) error
	// SessionByToken returns the session record itself (for org context).
	SessionByToken(token string) (*Session, error)
	// SetActiveOrg persists the session's default workspace.
	SetActiveOrg(token, orgID string) error
	GetByID(id string) (*User, error)
	FindByEmail(email string) (*User, error)
	ListUsers() ([]*User, error)
	// SetAdmin grants or removes platform-admin standing (REQ-155). The
	// last platform admin cannot be demoted (ErrLastAdmin): a deployment
	// with none would have nobody able to make one.
	SetAdmin(id string, isAdmin bool) (*User, error)
	// SetEmailNotifications updates the caller's own email opt-out (issue #187).
	SetEmailNotifications(userID string, enabled bool) error
	// SetPushNotifications updates the caller's own web-push opt-in (REQ-109).
	SetPushNotifications(userID string, enabled bool) error
	// SetDefaultOrg records the workspace the caller's sign-in lands in
	// (REQ-156); "" means the personal workspace again.
	SetDefaultOrg(userID, orgID string) error
	// MarkBillingTrialUsed records that the caller's one free trial is spent.
	MarkBillingTrialUsed(userID string) error
	// SetAvatar records an uploaded profile picture's on-disk path, MIME
	// type and the URL it is served at, and returns the updated user.
	SetAvatar(userID, path, mime, url string) (*User, error)
	// ClearAvatar forgets an uploaded profile picture (the file is the
	// caller's to remove) and returns the updated user. The account is left
	// with no picture until its identity provider supplies one again.
	ClearAvatar(userID string) (*User, error)
	// IssueEmailVerification mints a fresh verification link for the user.
	// email "" means the account's current address; any other address is a
	// change request — the link goes there and the account's address changes
	// only when it is confirmed. Returns the raw token (for the email) and
	// the normalised address it was issued for. Errors: ErrAlreadyVerified,
	// ErrEmailTaken, or an invalid address.
	IssueEmailVerification(userID, email string) (token, sentTo string, err error)
	// MarkEmailVerified marks the account verified without an emailed link,
	// for a caller that already holds proof the account controls its current
	// address — an invitation token delivered to that mailbox. Returns the
	// updated user. Verifying an already-verified account is a no-op.
	MarkEmailVerified(userID string) (*User, error)
	// ConfirmEmailVerification spends a raw token: single use, valid for
	// EmailVerificationTTL. It marks the user verified, applies the link's
	// address, and returns the user. ErrVerificationInvalid for anything
	// unknown, used or expired; ErrEmailTaken when the address was claimed
	// by another account in the meantime.
	ConfirmEmailVerification(token string) (*User, error)
	// IssuePasswordReset mints a reset link for a password account (REQ-158):
	// delivery is ResetDeliveryEmail (valid PasswordResetTTL) or
	// ResetDeliveryAdmin (valid AdminPasswordResetTTL, issuedBy the admin).
	// Returns the raw token and when it expires. ErrUserNotFound for an
	// unknown id; ErrNoPassword for an account that signs in through an
	// identity provider, which has no password to reset.
	IssuePasswordReset(userID, delivery string, issuedBy *string) (token string, expiresAt time.Time, err error)
	// ResetPassword spends a raw reset token and sets the new password,
	// ending every session of the account. The password rule is checked
	// before the token is spent, so a weak password does not cost the
	// link. An emailed link also marks the address verified. Errors:
	// ErrWeakPassword, ErrResetInvalid.
	ResetPassword(token, newPassword string) (*User, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo     Repository
	policy   EmailVerificationPolicy
	sessions SessionPolicy
}

// NewDefaultService creates a new user service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// NormalizeEmail folds an address into the one form the platform stores and
// compares: trimmed and lower-cased. It is the single definition every
// caller uses — the user service, the API handlers, invitations — so
// "Dave@Example.com" and "dave@example.com " are the same account, the same
// rate-limit bucket and the same invitation everywhere.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Register creates a password account. The first user becomes admin.
func (s *DefaultService) Register(email, password, name string) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, errors.New("a valid email is required")
	}
	if len(password) < MinPasswordLength {
		return nil, ErrWeakPassword
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

	now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 154)
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

// GetByID returns a user by id.
func (s *DefaultService) GetByID(id string) (*User, error) {
	return s.repo.FindUserByID(id)
}

// FindByEmail returns a user by email, or nil if not found.
func (s *DefaultService) FindByEmail(email string) (*User, error) {
	return s.repo.FindUserByEmail(NormalizeEmail(email))
}

// ListUsers returns all users.
func (s *DefaultService) ListUsers() ([]*User, error) {
	return s.repo.ListUsers()
}

// SetAdmin implements Service.
func (s *DefaultService) SetAdmin(id string, isAdmin bool) (*User, error) {
	u, err := s.repo.FindUserByID(id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	if u.IsAdmin == isAdmin {
		return u, nil
	}
	if !isAdmin {
		all, err := s.repo.ListUsers()
		if err != nil {
			return nil, err
		}
		admins := 0
		for _, other := range all {
			if other.IsAdmin {
				admins++
			}
		}
		if admins <= 1 {
			return nil, ErrLastAdmin
		}
	}
	u.IsAdmin = isAdmin
	u.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateUser(u); err != nil {
		return nil, err
	}
	return u, nil
}

// SetEmailNotifications updates a user's email-notification opt-out.
func (s *DefaultService) SetEmailNotifications(userID string, enabled bool) error {
	return s.repo.SetEmailNotifications(userID, enabled)
}

// MarkBillingTrialUsed implements Service.
func (s *DefaultService) MarkBillingTrialUsed(userID string) error {
	return s.repo.SetBillingTrialUsed(userID, time.Now().UTC())
}

// SetPushNotifications updates a user's web-push opt-in.
func (s *DefaultService) SetPushNotifications(userID string, enabled bool) error {
	return s.repo.SetPushNotifications(userID, enabled)
}

// SetDefaultOrg records the workspace a user's sign-in lands in. Whether the
// user may land there is the caller's check: this layer does not know
// workspaces, only the choice.
func (s *DefaultService) SetDefaultOrg(userID, orgID string) error {
	return s.repo.SetDefaultOrg(userID, orgID)
}

// SetAvatar records an uploaded profile picture. The write touches only the
// avatar columns, so it can never carry a stale copy of the rest of the row.
func (s *DefaultService) SetAvatar(userID, path, mime, url string) (*User, error) {
	if err := s.repo.SetAvatar(userID, path, mime, url, time.Now().UTC()); err != nil {
		return nil, err
	}
	return s.repo.FindUserByID(userID)
}

// ClearAvatar forgets an uploaded profile picture, URL included.
func (s *DefaultService) ClearAvatar(userID string) (*User, error) {
	return s.SetAvatar(userID, "", "", "")
}
