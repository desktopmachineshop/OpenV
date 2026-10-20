// Linked sign-in methods (REQ-206, issue #242's follow-up): an account
// created with a password can also be entered through Google or the
// deployment's OIDC provider once its owner has proved, with that password,
// that the identity provider's account and the OpenV account are the same
// person.
//
// Matching on the email string alone is not that proof. Password sign-ups
// need not prove their address (a deployment with no mailer verifies
// nothing), so whoever registers somebody else's address first would get
// that person's later single sign-on handed to an account whose password
// they know. Instead a single sign-on that meets an account of another
// method mints a short-lived link request, held by the browser that came
// back from the provider, and the account's password spends it.

package users

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// IdentityLinkTTL is how long a link request stays valid: long enough to
// type a password, short enough that a forgotten tab holds nothing.
const IdentityLinkTTL = 10 * time.Minute

// ErrLinkInvalid covers every way a link request can fail — unknown, spent,
// expired — in one answer, so a probe learns nothing about which.
var ErrLinkInvalid = errors.New("this sign-in link has expired; sign in with your identity provider again")

// IdentityLink is one pending request to add Provider to UserID's sign-in
// methods. The raw token is never stored.
type IdentityLink struct {
	ID        string
	UserID    string
	Provider  string
	TokenHash string
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
}

// IdentityLinkPreview is what the sign-in page shows while it asks for the
// password: which provider is being added to which address.
type IdentityLinkPreview struct {
	Provider string `json:"provider"`
	Email    string `json:"email"`
}

// RequestIdentityLink mints a link request for the account registered at
// email, after a single sign-on through provider met it (ErrProviderMismatch).
// Only an account with a password can confirm one; any other answers
// ErrNoPassword, and the sign-on stays refused.
func (s *DefaultService) RequestIdentityLink(provider, email string) (string, error) {
	user, err := s.repo.FindUserByEmail(NormalizeEmail(email))
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrUserNotFound
	}
	if user.PasswordHash == "" {
		return "", ErrNoPassword
	}
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 154)
	l := &IdentityLink{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Provider:  provider,
		TokenHash: HashToken(token),
		ExpiresAt: now.Add(IdentityLinkTTL),
		CreatedAt: now,
	}
	if err := s.repo.SaveIdentityLink(l); err != nil {
		return "", err
	}
	return token, nil
}

// PreviewIdentityLink names the provider and the address of a live link
// request; ErrLinkInvalid for anything else.
func (s *DefaultService) PreviewIdentityLink(token string) (*IdentityLinkPreview, error) {
	l, user, err := s.liveIdentityLink(token)
	if err != nil {
		return nil, err
	}
	return &IdentityLinkPreview{Provider: l.Provider, Email: user.Email}, nil
}

// ConfirmIdentityLink spends a link request with the account's password: the
// provider joins the account's sign-in methods and a session opens. The
// password is checked before the request is spent, so a typing mistake does
// not send the person back through the provider. Errors: ErrLinkInvalid,
// ErrPasswordIncorrect.
func (s *DefaultService) ConfirmIdentityLink(token, password string) (*User, string, error) {
	_, user, err := s.liveIdentityLink(token)
	if err != nil {
		return nil, "", err
	}
	if user.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, "", ErrPasswordIncorrect
	}
	now := time.Now().UTC()
	l, err := s.repo.LinkIdentity(HashToken(strings.TrimSpace(token)), now)
	if err != nil {
		return nil, "", err
	}
	if l == nil {
		// Spent by a racing confirm, or expired while the password was typed.
		return nil, "", ErrLinkInvalid
	}
	// Both callbacks refuse an address the provider has not verified, and
	// the password has just shown this is that address's account.
	if !user.EmailVerified {
		if err := s.repo.MarkEmailVerified(user.ID, now); err != nil {
			slog.Warn("identity link: could not mark the address verified", "user_id", user.ID, "error", err)
		} else {
			user.EmailVerified = true
			user.EmailVerifiedAt = &now
		}
	}
	session, err := s.createSession(user.ID)
	if err != nil {
		return nil, "", err
	}
	return user, session, nil
}

// liveIdentityLink resolves a raw token to its live request and account.
func (s *DefaultService) liveIdentityLink(token string) (*IdentityLink, *User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil, ErrLinkInvalid
	}
	l, err := s.repo.FindIdentityLink(HashToken(token), time.Now().UTC())
	if err != nil {
		return nil, nil, err
	}
	if l == nil {
		return nil, nil, ErrLinkInvalid
	}
	user, err := s.repo.FindUserByID(l.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, ErrLinkInvalid
	}
	return l, user, nil
}
