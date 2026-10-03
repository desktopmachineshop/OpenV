// Email verification (SEC-15 / REQ-95): the deployment's policy, the
// emailed link that proves control of an address, or changes it, and
// marking an account verified on other proof.

package users

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EmailVerificationTTL is how long an emailed verification link stays valid.
// Long enough to survive a slow inbox, short enough that a link forwarded or
// left in a mailbox is not a standing credential.
const EmailVerificationTTL = 24 * time.Hour

// EmailVerificationPolicy says whether password accounts must prove control
// of their address before the app serves them (SEC-15 / REQ-95). Required is
// true only when the deployment can actually send mail and the operator has
// not switched it off (see notify.VerificationPolicyFromEnv); with it false,
// accounts are born verified and nothing is enforced, which keeps a default
// self-hosted stack, CI and the E2E suite exactly as they were.
type EmailVerificationPolicy struct {
	Required bool
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

// SetEmailVerificationPolicy wires the deployment's verification policy
// (wiring-time only). It decides whether Register creates accounts verified
// (policy off) or pending a link (policy on).
func (s *DefaultService) SetEmailVerificationPolicy(p EmailVerificationPolicy) {
	s.policy = p
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
	email = NormalizeEmail(email)
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

// MarkEmailVerified marks an account verified on proof that is not an
// emailed link; see the Service interface.
func (s *DefaultService) MarkEmailVerified(userID string) (*User, error) {
	user, err := s.repo.FindUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	if user.EmailVerified {
		return user, nil
	}
	now := time.Now()
	if err := s.repo.MarkEmailVerified(user.ID, now); err != nil {
		return nil, err
	}
	user.EmailVerified = true
	user.EmailVerifiedAt = &now
	user.UpdatedAt = now
	return user, nil
}
