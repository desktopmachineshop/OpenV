package providers

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Known provider names.
const (
	ProviderClaudeCode   = "claude-code"
	ProviderCodexCLI     = "codex-cli"
	ProviderGeminiCLI    = "gemini-cli"
	ProviderAnthropicAPI = "anthropic-api"
	ProviderOpenAIAPI    = "openai-api"
	ProviderGoogleAPI    = "google-api"
)

// Auth modes.
const (
	AuthSubscriptionCLI = "subscription-cli"
	AuthAPIKey          = "api-key"
)

// DefaultAPIKeyEnv returns the environment variable a provider's CLI/SDK
// natively reads its API key from. Used both as the default for a blank
// api_key_env setting and as the variable the runner injects the key into.
func DefaultAPIKeyEnv(provider string) string {
	switch provider {
	case ProviderClaudeCode, ProviderAnthropicAPI:
		return "ANTHROPIC_API_KEY"
	case ProviderCodexCLI, ProviderOpenAIAPI:
		return "OPENAI_API_KEY"
	case ProviderGeminiCLI, ProviderGoogleAPI:
		return "GEMINI_API_KEY"
	}
	return ""
}

// KnownProviders returns the supported provider names in display order.
func KnownProviders() []string {
	return []string{
		ProviderClaudeCode,
		ProviderCodexCLI,
		ProviderGeminiCLI,
		ProviderAnthropicAPI,
		ProviderOpenAIAPI,
		ProviderGoogleAPI,
	}
}

// ProviderSetting configures a single agent provider within an org.
type ProviderSetting struct {
	ID           string                 `json:"id"`
	OrgID        string                 `json:"org_id"`
	Provider     string                 `json:"provider"`
	AuthMode     string                 `json:"auth_mode"`
	APIKeyEnv    string                 `json:"api_key_env"`
	DefaultModel string                 `json:"default_model"`
	Enabled      bool                   `json:"enabled"`
	LastDetected map[string]interface{} `json:"last_detected"`
	UpdatedAt    time.Time              `json:"updated_at"`
	// AvailableModels is derived on read (catalog + detection), never
	// stored; the UI populates its model pickers from it.
	AvailableModels []Model `json:"available_models"`
}

// Repository defines persistence operations for provider settings.
// FindByProvider returns (nil, nil) when no row exists.
type Repository interface {
	Upsert(p *ProviderSetting) error
	FindByProvider(orgID, provider string) (*ProviderSetting, error)
	List(orgID string) ([]*ProviderSetting, error)
}

// Service defines provider settings domain logic.
type Service interface {
	List(orgID string) ([]*ProviderSetting, error)
	Upsert(setting *ProviderSetting) error
	RecordDetection(orgID, provider string, detected map[string]interface{}) error
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new provider settings service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

func isKnownProvider(name string) bool {
	for _, p := range KnownProviders() {
		if p == name {
			return true
		}
	}
	return false
}

func defaultAuthMode(provider string) string {
	switch provider {
	case ProviderClaudeCode, ProviderCodexCLI, ProviderGeminiCLI:
		return AuthSubscriptionCLI
	default:
		return AuthAPIKey
	}
}

func defaultSetting(orgID, provider string) *ProviderSetting {
	return &ProviderSetting{
		ID:           uuid.New().String(),
		OrgID:        orgID,
		Provider:     provider,
		AuthMode:     defaultAuthMode(provider),
		Enabled:      true,
		LastDetected: map[string]interface{}{},
		UpdatedAt:    time.Now(),
	}
}

// List returns a setting for every known provider in an org, filling in
// in-memory defaults for providers without a stored row.
func (s *DefaultService) List(orgID string) ([]*ProviderSetting, error) {
	stored, err := s.repo.List(orgID)
	if err != nil {
		return nil, err
	}
	byProvider := make(map[string]*ProviderSetting, len(stored))
	for _, p := range stored {
		byProvider[p.Provider] = p
	}

	result := make([]*ProviderSetting, 0, len(KnownProviders()))
	for _, name := range KnownProviders() {
		p, ok := byProvider[name]
		if !ok {
			p = defaultSetting(orgID, name)
		}
		p.AvailableModels = AvailableModels(p)
		result = append(result, p)
	}
	return result, nil
}

// Upsert validates and persists a provider setting.
func (s *DefaultService) Upsert(setting *ProviderSetting) error {
	if setting == nil {
		return errors.New("setting is required")
	}
	if setting.OrgID == "" {
		return errors.New("organization id is required")
	}
	if !isKnownProvider(setting.Provider) {
		return fmt.Errorf("unknown provider %q", setting.Provider)
	}
	if setting.AuthMode != AuthSubscriptionCLI && setting.AuthMode != AuthAPIKey {
		return fmt.Errorf("invalid auth_mode %q", setting.AuthMode)
	}
	if setting.ID == "" {
		setting.ID = uuid.New().String()
	}
	if setting.LastDetected == nil {
		setting.LastDetected = map[string]interface{}{}
	}
	setting.UpdatedAt = time.Now()
	if err := s.repo.Upsert(setting); err != nil {
		return err
	}
	// Refresh the derived list so the caller's echoed row stays usable.
	setting.AvailableModels = AvailableModels(setting)
	return nil
}

// RecordDetection merges detection results into a provider's last_detected
// blob, stamping checked_at, and persists it.
func (s *DefaultService) RecordDetection(orgID, provider string, detected map[string]interface{}) error {
	if orgID == "" {
		return errors.New("organization id is required")
	}
	if !isKnownProvider(provider) {
		return fmt.Errorf("unknown provider %q", provider)
	}

	setting, err := s.repo.FindByProvider(orgID, provider)
	if err != nil {
		return err
	}
	if setting == nil {
		setting = defaultSetting(orgID, provider)
	}
	if setting.LastDetected == nil {
		setting.LastDetected = map[string]interface{}{}
	}
	for k, v := range detected {
		setting.LastDetected[k] = v
	}
	setting.LastDetected["checked_at"] = time.Now().UTC().Format(time.RFC3339)
	setting.UpdatedAt = time.Now()
	return s.repo.Upsert(setting)
}
