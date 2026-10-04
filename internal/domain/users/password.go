// Passwords: the length rule, a change by the account's owner, and the
// reset link (REQ-158) that sets a new one.

package users

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is the shortest password the platform accepts, at
// registration and at every later change.
const MinPasswordLength = 8

// Password reset (REQ-158). A reset link is a single-use token, stored
// hashed like a verification link, that lets whoever holds it set a new
// password for one account. It reaches a person one of two ways:
//
//   - emailed, from the sign-in page, on a deployment that can send mail:
//     the link goes only to the account's own address, so following it is
//     also proof of control of that address;
//   - minted by a platform admin and handed over out of band, which is the
//     support path, and the only path on a deployment with no mailer. It
//     proves nothing about the mailbox, so it verifies nothing.
//
// Setting the password through either ends every session of the account:
// the reason to reset a password is usually that somebody else may hold the
// old one.
const (
	// PasswordResetTTL is how long an emailed reset link stays valid.
	PasswordResetTTL = time.Hour
	// AdminPasswordResetTTL is how long an admin-minted link stays valid: it
	// travels through a support conversation, which takes longer than an
	// inbox.
	AdminPasswordResetTTL = 24 * time.Hour

	ResetDeliveryEmail = "email"
	ResetDeliveryAdmin = "admin"
)

// ErrResetInvalid covers every way a reset link can fail — unknown, spent,
// expired — in one answer, so a probe learns nothing about which.
var ErrResetInvalid = errors.New("password reset link is invalid or has expired")

// PasswordReset is one reset link. The raw token is never stored.
type PasswordReset struct {
	ID        string
	UserID    string
	TokenHash string
	// Delivery is ResetDeliveryEmail or ResetDeliveryAdmin; IssuedBy is the
	// platform admin who minted an admin link.
	Delivery  string
	IssuedBy  *string
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
}

// IssuePasswordReset mints a reset link; see the Service interface.
func (s *DefaultService) IssuePasswordReset(userID, delivery string, issuedBy *string) (string, time.Time, error) {
	user, err := s.repo.FindUserByID(userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if user == nil {
		return "", time.Time{}, ErrUserNotFound
	}
	if user.PasswordHash == "" {
		return "", time.Time{}, ErrNoPassword
	}
	ttl := PasswordResetTTL
	if delivery == ResetDeliveryAdmin {
		ttl = AdminPasswordResetTTL
	} else {
		delivery = ResetDeliveryEmail
	}
	token, err := NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 154)
	v := &PasswordReset{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		TokenHash: HashToken(token),
		Delivery:  delivery,
		IssuedBy:  issuedBy,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.repo.SavePasswordReset(v); err != nil {
		return "", time.Time{}, err
	}
	return token, v.ExpiresAt, nil
}

// ResetPassword spends a reset token and sets the password; see the
// Service interface.
func (s *DefaultService) ResetPassword(token, newPassword string) (*User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrResetInvalid
	}
	if len(newPassword) < MinPasswordLength {
		return nil, ErrWeakPassword
	}
	// Compared with expires_at, a TIMESTAMP holding a UTC wall clock: a local
	// now moved the link's deadline by the server's offset (#379 bug 154).
	now := time.Now().UTC()
	reset, err := s.repo.ConsumePasswordReset(HashToken(token), now)
	if err != nil {
		return nil, err
	}
	if reset == nil {
		return nil, ErrResetInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetPasswordHash(reset.UserID, string(hash), now); err != nil {
		return nil, err
	}
	// Whoever held the old password is signed out everywhere; the person
	// resetting signs in fresh with the new one. As with a change, the
	// password DID change, so a sweep that failed is logged, not reported.
	if err := s.repo.DeleteSessionsForUser(reset.UserID, ""); err != nil {
		slog.Error("password reset but sessions were not signed out", "user_id", reset.UserID, "error", err)
	}
	// An emailed link reached the account's own inbox, which is exactly
	// what verification asks for. An admin link proves nothing of the kind.
	if reset.Delivery == ResetDeliveryEmail {
		if err := s.repo.MarkEmailVerified(reset.UserID, now); err != nil {
			slog.Warn("password reset: could not mark the address verified", "user_id", reset.UserID, "error", err)
		}
	}
	user, err := s.repo.FindUserByID(reset.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrResetInvalid
	}
	return user, nil
}

// ChangePassword replaces a password account's password; see Service.
func (s *DefaultService) ChangePassword(userID, currentPassword, newPassword, keepToken string) error {
	user, err := s.repo.FindUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrInvalidCredentials
	}
	// An SSO-only account has nothing to compare against: changing "the
	// password" there would silently mint one, which is not what the person
	// asked for.
	if user.PasswordHash == "" {
		return ErrNoPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return ErrPasswordIncorrect
	}
	if len(newPassword) < MinPasswordLength {
		return ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.repo.SetPasswordHash(user.ID, string(hash), time.Now().UTC()); err != nil {
		return err
	}
	// The point of a password change is often that the old one leaked, so
	// every session it could have opened dies with it — except the one doing
	// the changing, which would otherwise sign the owner out of their own
	// browser (REQ-99).
	except := ""
	if keepToken != "" {
		except = HashToken(keepToken)
	}
	// The password DID change, so the change cannot be reported as failed:
	// a caller told "that did not work" retries with a current password the
	// server no longer holds, and the owner is left believing the old one
	// still opens the account. A sweep that did not run is logged instead;
	// the sessions it would have killed still die at their idle deadline,
	// and the session policy bounds how long that is (REQ-99).
	if err := s.repo.DeleteSessionsForUser(user.ID, except); err != nil {
		slog.Error("password changed but other sessions were not signed out",
			"user_id", user.ID, "error", err)
	}
	return nil
}
