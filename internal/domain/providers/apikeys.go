package providers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// A workspace's pasted provider API keys (REQ-197). A workspace admin pastes a key in
// the app, once per key variable (ANTHROPIC_API_KEY serves both
// claude-code and anthropic-api); the server keeps it sealed with
// AES-256-GCM under the deployment's secrets key (OPENV_SECRETS_KEY) and
// hands it, opened, only to a runner claiming a run of a project on api-key
// auth. Nothing reads a key back to a browser: the API shows its last four
// characters, who set it and when.
//
// The seal binds the workspace and the variable as additional data, so a
// sealed key copied onto another workspace's row, or onto another variable,
// does not open.

// StoredKey is one pasted key, as the API lists it. Sealed never leaves the
// server.
type StoredKey struct {
	OrgID     string    `json:"-"`
	KeyEnv    string    `json:"key_env"`
	Last4     string    `json:"last4"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
	Sealed    []byte    `json:"-"`
}

// KeyRepository persists pasted keys, one row per workspace and variable.
// FindKey returns (nil, nil) when there is none.
type KeyRepository interface {
	UpsertKey(k *StoredKey) error
	FindKey(orgID, keyEnv string) (*StoredKey, error)
	ListKeys(orgID string) ([]*StoredKey, error)
	DeleteKey(orgID, keyEnv string) (bool, error)
}

// KeyService is what the API uses for pasted keys.
type KeyService interface {
	// KeysEnabled reports whether the deployment can store keys: false when
	// OPENV_SECRETS_KEY is not set.
	KeysEnabled() bool
	ListKeys(orgID string) ([]*StoredKey, error)
	SetKey(orgID, keyEnv, value, userID string) (*StoredKey, error)
	DeleteKey(orgID, keyEnv string) (bool, error)
	// RevealKey opens the workspace's key for a variable, "" when none is
	// stored.
	RevealKey(orgID, keyEnv string) (string, error)
}

// ErrKeyStoreOff refuses a write when the deployment has no secrets key.
var ErrKeyStoreOff = errors.New("this server cannot store API keys: its OPENV_SECRETS_KEY is not set")

// ErrInvalidKey flags a caller mistake in a key write (an unknown variable,
// a blank or malformed key); the API answers it 400.
var ErrInvalidKey = errors.New("invalid API key")

// MinSecretsKeyLength is the shortest OPENV_SECRETS_KEY accepted.
const MinSecretsKeyLength = 32

const maxKeyLength = 512

// Sealer seals and opens pasted keys with AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the AES-256 key from the deployment's secrets key (its
// SHA-256, so any string of at least MinSecretsKeyLength characters works,
// e.g. `openssl rand -base64 32`).
func NewSealer(secret string) (*Sealer, error) {
	if len(secret) < MinSecretsKeyLength {
		return nil, fmt.Errorf("OPENV_SECRETS_KEY must be at least %d characters", MinSecretsKeyLength)
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func keyBinding(orgID, keyEnv string) []byte {
	return []byte(orgID + "\x00" + keyEnv)
}

// Seal returns the nonce followed by the sealed value.
func (s *Sealer) Seal(orgID, keyEnv, value string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(value), keyBinding(orgID, keyEnv)), nil
}

// Open reverses Seal for the same workspace and variable.
func (s *Sealer) Open(orgID, keyEnv string, sealed []byte) (string, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return "", errors.New("sealed API key is truncated")
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], keyBinding(orgID, keyEnv))
	if err != nil {
		return "", fmt.Errorf("sealed API key does not open (was OPENV_SECRETS_KEY changed?): %w", err)
	}
	return string(plain), nil
}

// DefaultKeyService implements KeyService. A nil sealer turns writes and
// reveals off.
type DefaultKeyService struct {
	repo   KeyRepository
	sealer *Sealer
}

// NewKeyService creates the pasted-key service.
func NewKeyService(repo KeyRepository, sealer *Sealer) *DefaultKeyService {
	return &DefaultKeyService{repo: repo, sealer: sealer}
}

func (s *DefaultKeyService) KeysEnabled() bool { return s.sealer != nil }

func (s *DefaultKeyService) ListKeys(orgID string) ([]*StoredKey, error) {
	return s.repo.ListKeys(orgID)
}

// validKeyEnv is a catalogued variable; unlike a provider setting's
// api_key_env, blank is not allowed here.
func validKeyEnv(keyEnv string) bool {
	return keyEnv != "" && IsAllowedAPIKeyEnv(keyEnv)
}

// errNotAKeyVariable refuses a variable outside the catalog without echoing
// it, so a malformed name answers as any other unknown one.
func errNotAKeyVariable() error {
	return fmt.Errorf("%w: not a provider key variable; use one of %s", ErrInvalidKey,
		strings.Join(AllowedAPIKeyEnvs(), ", "))
}

func (s *DefaultKeyService) SetKey(orgID, keyEnv, value, userID string) (*StoredKey, error) {
	if !validKeyEnv(keyEnv) {
		return nil, errNotAKeyVariable()
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%w: the key is blank", ErrInvalidKey)
	}
	if len(value) > maxKeyLength {
		return nil, fmt.Errorf("%w: the key is longer than %d characters", ErrInvalidKey, maxKeyLength)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return nil, fmt.Errorf("%w: the key contains a space or line break; paste the key alone", ErrInvalidKey)
		}
	}
	if s.sealer == nil {
		return nil, ErrKeyStoreOff
	}
	sealed, err := s.sealer.Seal(orgID, keyEnv, value)
	if err != nil {
		return nil, err
	}
	last4 := value
	if len(last4) > 4 {
		last4 = last4[len(last4)-4:]
	}
	key := &StoredKey{
		OrgID:     orgID,
		KeyEnv:    keyEnv,
		Last4:     last4,
		UpdatedBy: userID,
		UpdatedAt: time.Now().UTC(),
		Sealed:    sealed,
	}
	if err := s.repo.UpsertKey(key); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *DefaultKeyService) DeleteKey(orgID, keyEnv string) (bool, error) {
	if !validKeyEnv(keyEnv) {
		return false, errNotAKeyVariable()
	}
	return s.repo.DeleteKey(orgID, keyEnv)
}

func (s *DefaultKeyService) RevealKey(orgID, keyEnv string) (string, error) {
	if s.sealer == nil || !validKeyEnv(keyEnv) {
		return "", nil
	}
	key, err := s.repo.FindKey(orgID, keyEnv)
	if err != nil || key == nil {
		return "", err
	}
	return s.sealer.Open(orgID, keyEnv, key.Sealed)
}
