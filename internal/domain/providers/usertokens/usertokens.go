// Package usertokens holds a member's own long-lived provider sign-in: today the
// token `claude setup-token` prints for a Claude subscription, which the
// Claude Code CLI reads from CLAUDE_CODE_OAUTH_TOKEN.
//
// A runner that is leased per session (or a personal runner whose CLI sign-in
// lapsed) would otherwise ask the member to sign in again on every lease.
// With a stored token the API hands it, in the claim, to a runner that runs
// for that member only — their personal runner or a lease bound to them —
// and the CLI runs signed in with no browser step.
//
// The token is a secret of the member's subscription, so it is sealed with
// the deployment's providers.Sealer (AES-256-GCM under OPENV_SECRETS_KEY, the
// key that seals workspaces' pasted API keys) before it reaches the
// database, bound to the member, never returned by the API, and opened only
// for a claim. A server without the key has no stored tokens: Save refuses
// with ErrUnavailable.
package usertokens

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// claudeTokenPrefix starts every token `claude setup-token` prints.
const claudeTokenPrefix = "sk-ant-oat01-"

// Lifetime is how long a setup-token is valid: one year from when it was
// minted. The token carries no readable expiry, so the store dates it from
// when it was saved, which is never later than it was minted.
const Lifetime = 365 * 24 * time.Hour

// ExpiringWithin is when the settings card starts warning that a token is
// about to lapse.
const ExpiringWithin = 30 * 24 * time.Hour

var (
	// ErrInvalid flags a caller mistake: an unknown provider or a value that
	// is not a token of that provider.
	ErrInvalid = errors.New("invalid provider token")
	// ErrUnavailable says the server has no secrets key, so it stores none.
	ErrUnavailable = errors.New("stored provider tokens are not configured on this server")
)

// Token is one member's stored token for one provider, as persisted.
type Token struct {
	UserID   string
	Provider string
	// Sealed is the nonce followed by the AES-GCM ciphertext.
	Sealed []byte
	// Last4 is the last four characters, for the member to recognise it.
	Last4     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Status is what the API shows a member about their token: never the token.
type Status struct {
	Provider string `json:"provider"`
	// Available is false on a server with no secrets key.
	Available bool       `json:"available"`
	Connected bool       `json:"connected"`
	Last4     string     `json:"last4,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Expired   bool       `json:"expired"`
}

// Repository persists tokens, one per member and provider.
type Repository interface {
	Get(userID, provider string) (*Token, error)
	// Upsert stores t, replacing the member's earlier token for the provider.
	Upsert(t *Token) error
	Delete(userID, provider string) error
}

// Service is the façade the API uses.
type Service interface {
	Status(userID, provider string) (*Status, error)
	Save(userID, provider, token string) (*Status, error)
	// SaveFor is Save for a token whose lifetime the CLI reported; a
	// lifetime of 0 or one out of range is taken as Lifetime.
	SaveFor(userID, provider, token string, lifetime time.Duration) (*Status, error)
	Delete(userID, provider string) error
	// ForRun opens the member's live token for a run, or "" when they have
	// none, it has expired, or the server cannot open it.
	ForRun(userID, provider string) (string, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo   Repository
	sealer *providers.Sealer // nil when the server has no secrets key
	now    func() time.Time
}

// NewDefaultService builds the service over the deployment's sealer; a nil
// sealer leaves the store unavailable.
func NewDefaultService(repo Repository, sealer *providers.Sealer) *DefaultService {
	return &DefaultService{repo: repo, sealer: sealer, now: func() time.Time { return time.Now().UTC() }}
}

// Available reports whether the server can store tokens.
func (s *DefaultService) Available() bool { return s != nil && s.sealer != nil }

// sealScope is the first half of a token's seal binding, the member: the
// sealer binds a workspace id there for pasted API keys, and the prefix keeps
// the two kinds of row from ever opening as each other.
func sealScope(userID string) string { return "user:" + userID }

func checkProvider(provider string) error {
	if provider != providers.ProviderClaudeCode {
		return fmt.Errorf("%w: only claude-code keeps a stored token", ErrInvalid)
	}
	return nil
}

// Validate checks that token looks like one `claude setup-token` prints.
func Validate(provider, token string) error {
	if err := checkProvider(provider); err != nil {
		return err
	}
	if !strings.HasPrefix(token, claudeTokenPrefix) || len(token) < len(claudeTokenPrefix)+20 {
		return fmt.Errorf("%w: paste the whole token `claude setup-token` printed; it starts with %s", ErrInvalid, claudeTokenPrefix)
	}
	for _, c := range token {
		if !(c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return fmt.Errorf("%w: the token holds a character a Claude token never does", ErrInvalid)
		}
	}
	return nil
}

func (s *DefaultService) status(provider string, t *Token) *Status {
	st := &Status{Provider: provider, Available: s.Available()}
	if t == nil {
		return st
	}
	created, expires := t.CreatedAt, t.ExpiresAt
	st.Connected = true
	st.Last4 = t.Last4
	st.CreatedAt = &created
	st.ExpiresAt = &expires
	st.Expired = !s.now().Before(expires)
	return st
}

// Status reports the member's token without opening it.
func (s *DefaultService) Status(userID, provider string) (*Status, error) {
	if err := checkProvider(provider); err != nil {
		return nil, err
	}
	t, err := s.repo.Get(userID, provider)
	if err != nil {
		return nil, err
	}
	return s.status(provider, t), nil
}

// Save seals and stores the member's token, replacing any earlier one,
// dated to lapse Lifetime from now: a pasted token says nothing of its own.
func (s *DefaultService) Save(userID, provider, token string) (*Status, error) {
	return s.SaveFor(userID, provider, token, Lifetime)
}

// MaxLifetime bounds the lifetime a runner may report for a token.
const MaxLifetime = 5 * 365 * 24 * time.Hour

// SaveFor seals and stores the member's token, dated to lapse after the
// lifetime the CLI reported.
func (s *DefaultService) SaveFor(userID, provider, token string, lifetime time.Duration) (*Status, error) {
	if lifetime < time.Hour || lifetime > MaxLifetime {
		lifetime = Lifetime
	}
	token = strings.TrimSpace(token)
	if err := Validate(provider, token); err != nil {
		return nil, err
	}
	if !s.Available() {
		return nil, ErrUnavailable
	}
	sealed, err := s.sealer.Seal(sealScope(userID), provider, token)
	if err != nil {
		return nil, err
	}
	now := s.now()
	t := &Token{
		UserID:    userID,
		Provider:  provider,
		Sealed:    sealed,
		Last4:     token[len(token)-4:],
		CreatedAt: now,
		ExpiresAt: now.Add(lifetime),
	}
	if err := s.repo.Upsert(t); err != nil {
		return nil, err
	}
	return s.status(provider, t), nil
}

// Delete forgets the member's token.
func (s *DefaultService) Delete(userID, provider string) error {
	if err := checkProvider(provider); err != nil {
		return err
	}
	return s.repo.Delete(userID, provider)
}

// ForRun opens the member's token for a run on a runner that serves only
// that member.
func (s *DefaultService) ForRun(userID, provider string) (string, error) {
	if userID == "" || provider != providers.ProviderClaudeCode || !s.Available() {
		return "", nil
	}
	t, err := s.repo.Get(userID, provider)
	if err != nil || t == nil {
		return "", err
	}
	if !s.now().Before(t.ExpiresAt) {
		return "", nil
	}
	plain, err := s.sealer.Open(sealScope(userID), provider, t.Sealed)
	if err != nil {
		// A changed secrets key or a row moved between members: unusable,
		// not fatal.
		return "", err
	}
	return plain, nil
}
