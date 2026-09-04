package workerkeys

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/users"
)

var ErrNotFound = errors.New("worker key not found")

// Key is an org-scoped credential for a host worker (agentd).
// UserID nil = a workspace runner key; set = a member's personal runner key
// (claims only runs that user launched). The plaintext is shown once at
// creation; only the hash is stored.
//
// SessionID marks a key minted for a transient runner lease. Such a key is
// still personal — it routes the member's runs and sign-ins exactly like
// their own machine's key — but it lives and dies with the lease and never
// displaces the member's connector key.
type Key struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"org_id"`
	UserID     *string    `json:"user_id,omitempty"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	CreatedBy  *string    `json:"created_by,omitempty"`
	Revoked    bool       `json:"revoked"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	// SessionID is set on transient runner session keys.
	SessionID *string `json:"session_id,omitempty"`
	// UserName is denormalized for display on personal keys.
	UserName string `json:"user_name,omitempty"`
}

// Repository defines worker key persistence.
type Repository interface {
	Save(k *Key) error
	List(orgID string) ([]*Key, error)
	FindByID(id string) (*Key, error)
	FindByHash(hash string) (*Key, error) // (nil, nil) when absent
	// FindPersonal returns the member's own (non-session) personal key.
	FindPersonal(orgID, userID string) (*Key, error)
	Revoke(id string) error
	Touch(id string, at time.Time) error
	// HasOnlinePersonalKey reports whether the user has an unrevoked
	// personal key in the org used since the given time.
	HasOnlinePersonalKey(orgID, userID string, since time.Time) (bool, error)
}

// Resolved identifies an authenticated worker credential.
type Resolved struct {
	OrgID  string
	KeyID  string
	UserID string // "" for workspace keys
	// SessionID is set when the credential belongs to a transient runner
	// lease, so callers can record activity against it.
	SessionID string
}

// Service defines worker key logic.
type Service interface {
	// Create mints a key and returns it with the plaintext (shown once).
	// userID nil creates a workspace key; set creates a personal key.
	Create(orgID, name string, createdBy *string, userID *string) (*Key, string, error)
	List(orgID string) ([]*Key, error)
	// Get returns a key by id, verifying org ownership (nil if absent or
	// belonging to another org).
	Get(orgID, keyID string) (*Key, error)
	// PersonalKey returns the user's personal key in the org (nil if none).
	PersonalKey(orgID, userID string) (*Key, error)
	Revoke(orgID, keyID string) error
	// Resolve maps a presented token to its identity (zero value when
	// invalid/revoked).
	Resolve(token string) (Resolved, error)
	// HasOnlinePersonalRunner reports whether the user's personal runner
	// has polled recently.
	HasOnlinePersonalRunner(orgID, userID string, since time.Time) (bool, error)
	// EnsureBootstrapKey upserts a workspace key with a known plaintext
	// (keeps the WORKER_API_KEY env workflow working).
	EnsureBootstrapKey(orgID, plaintext, name string) error

	// MintSessionKey creates a personal key bound to a transient runner
	// lease, leaving the member's connector key alone.
	MintSessionKey(orgID, userID, sessionID, name string) (string, string, error)
	// RevokeSessionKey revokes a session key by id.
	RevokeSessionKey(orgID, keyID string) error

	// CreatePairing issues a short-lived one-time code the local Agent
	// Connector exchanges for the member's personal runner key.
	CreatePairing(orgID, userID string) (code string, expiresAt time.Time, err error)
	// ExchangePairing consumes a valid code, rotates the member's personal
	// key, and returns it with context for the connector's config.
	ExchangePairing(code, userName string) (*Key, string, error)
}

// PairingRepository persists connector pairing codes.
type PairingRepository interface {
	SavePairing(id, orgID, userID, codeHash string, expiresAt time.Time) error
	// ConsumePairing atomically marks an unused, unexpired code used and
	// returns its org/user ("" when invalid).
	ConsumePairing(codeHash string, now time.Time) (orgID, userID string, err error)
}

// PairingTTL is how long a connector pairing code stays valid.
const PairingTTL = 10 * time.Minute

// DefaultService implements Service.
type DefaultService struct {
	repo     Repository
	pairings PairingRepository
}

// NewDefaultService creates a worker key service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// SetPairingRepository wires connector pairing persistence (wiring-time only).
func (s *DefaultService) SetPairingRepository(pairings PairingRepository) {
	s.pairings = pairings
}

// CreatePairing issues a one-time code for the Agent Connector.
func (s *DefaultService) CreatePairing(orgID, userID string) (string, time.Time, error) {
	if s.pairings == nil {
		return "", time.Time{}, errors.New("pairing is not configured")
	}
	code, err := users.NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(PairingTTL)
	if err := s.pairings.SavePairing(uuid.New().String(), orgID, userID, users.HashToken(code), expires); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// ExchangePairing consumes a code and hands back a fresh personal key.
func (s *DefaultService) ExchangePairing(code, userName string) (*Key, string, error) {
	if s.pairings == nil {
		return nil, "", errors.New("pairing is not configured")
	}
	orgID, userID, err := s.pairings.ConsumePairing(users.HashToken(code), time.Now())
	if err != nil {
		return nil, "", err
	}
	if orgID == "" {
		return nil, "", errors.New("pairing code is invalid or expired")
	}
	name := "personal-runner"
	if userName != "" {
		name = userName + "'s connector"
	}
	return s.Create(orgID, name, &userID, &userID)
}

// Create mints a new key. A user's previous personal key is revoked so each
// member holds at most one active personal runner credential per org.
func (s *DefaultService) Create(orgID, name string, createdBy *string, userID *string) (*Key, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "worker"
	}
	if userID != nil {
		if existing, err := s.repo.FindPersonal(orgID, *userID); err == nil && existing != nil {
			_ = s.repo.Revoke(existing.ID)
		}
	}
	token, err := users.NewToken()
	if err != nil {
		return nil, "", err
	}
	key := &Key{
		ID:        uuid.New().String(),
		OrgID:     orgID,
		UserID:    userID,
		Name:      name,
		KeyHash:   users.HashToken(token),
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
	if err := s.repo.Save(key); err != nil {
		return nil, "", err
	}
	return key, token, nil
}

// MintSessionKey creates the credential a transient runner lease hands to its
// pool node. Unlike Create it rotates nothing: the member may well have a
// connector key on their own machine, and leasing a cloud runner must not
// sign that machine out.
func (s *DefaultService) MintSessionKey(orgID, userID, sessionID, name string) (string, string, error) {
	if orgID == "" || userID == "" || sessionID == "" {
		return "", "", errors.New("workspace, user and session are required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "cloud runner"
	}
	token, err := users.NewToken()
	if err != nil {
		return "", "", err
	}
	key := &Key{
		ID:        uuid.New().String(),
		OrgID:     orgID,
		UserID:    &userID,
		Name:      name,
		KeyHash:   users.HashToken(token),
		CreatedBy: &userID,
		SessionID: &sessionID,
		CreatedAt: time.Now(),
	}
	if err := s.repo.Save(key); err != nil {
		return "", "", err
	}
	return key.ID, token, nil
}

// RevokeSessionKey revokes a lease's credential.
func (s *DefaultService) RevokeSessionKey(orgID, keyID string) error {
	return s.Revoke(orgID, keyID)
}

// List returns an org's keys.
func (s *DefaultService) List(orgID string) ([]*Key, error) {
	return s.repo.List(orgID)
}

// Get returns a key by id, verifying org ownership.
func (s *DefaultService) Get(orgID, keyID string) (*Key, error) {
	key, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, err
	}
	if key == nil || key.OrgID != orgID {
		return nil, nil
	}
	return key, nil
}

// PersonalKey returns the user's active personal key in the org, or nil.
func (s *DefaultService) PersonalKey(orgID, userID string) (*Key, error) {
	return s.repo.FindPersonal(orgID, userID)
}

// Revoke disables a key, verifying org ownership.
func (s *DefaultService) Revoke(orgID, keyID string) error {
	key, err := s.repo.FindByID(keyID)
	if err != nil {
		return err
	}
	if key == nil || key.OrgID != orgID {
		return ErrNotFound
	}
	return s.repo.Revoke(keyID)
}

// Resolve validates a presented worker token.
func (s *DefaultService) Resolve(token string) (Resolved, error) {
	key, err := s.repo.FindByHash(users.HashToken(token))
	if err != nil {
		return Resolved{}, err
	}
	if key == nil || key.Revoked {
		return Resolved{}, nil
	}
	_ = s.repo.Touch(key.ID, time.Now())
	resolved := Resolved{OrgID: key.OrgID, KeyID: key.ID}
	if key.UserID != nil {
		resolved.UserID = *key.UserID
	}
	if key.SessionID != nil {
		resolved.SessionID = *key.SessionID
	}
	return resolved, nil
}

// HasOnlinePersonalRunner reports recent activity on the user's personal key.
func (s *DefaultService) HasOnlinePersonalRunner(orgID, userID string, since time.Time) (bool, error) {
	return s.repo.HasOnlinePersonalKey(orgID, userID, since)
}

// EnsureBootstrapKey upserts the env-configured key for an org.
func (s *DefaultService) EnsureBootstrapKey(orgID, plaintext, name string) error {
	hash := users.HashToken(plaintext)
	existing, err := s.repo.FindByHash(hash)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	key := &Key{
		ID:        uuid.New().String(),
		OrgID:     orgID,
		Name:      name,
		KeyHash:   hash,
		CreatedAt: time.Now(),
	}
	return s.repo.Save(key)
}
