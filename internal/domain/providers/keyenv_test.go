package providers

import (
	"errors"
	"testing"
)

type memRepo struct{ saved *ProviderSetting }

func (m *memRepo) Upsert(s *ProviderSetting) error { m.saved = s; return nil }
func (m *memRepo) FindByProvider(orgID, provider string) (*ProviderSetting, error) {
	return nil, nil
}
func (m *memRepo) List(orgID string) ([]*ProviderSetting, error) { return nil, nil }

func TestUpsertRefusesAKeyEnvOutsideTheCatalog(t *testing.T) {
	repo := &memRepo{}
	svc := NewDefaultService(repo)
	for _, name := range []string{"WORKER_API_KEY", "RUNNER_POOL_KEY", "DATABASE_URL", "PATH", "anthropic_api_key"} {
		err := svc.Upsert(&ProviderSetting{OrgID: "org", Provider: ProviderClaudeCode, AuthMode: AuthAPIKey, APIKeyEnv: name})
		if !errors.Is(err, ErrInvalidSetting) {
			t.Fatalf("%s accepted: %v", name, err)
		}
		if repo.saved != nil {
			t.Fatalf("%s was persisted", name)
		}
	}
	for _, name := range append(AllowedAPIKeyEnvs(), "") {
		if err := svc.Upsert(&ProviderSetting{OrgID: "org", Provider: ProviderClaudeCode, AuthMode: AuthAPIKey, APIKeyEnv: name}); err != nil {
			t.Fatalf("%q refused: %v", name, err)
		}
	}
}

func TestIsAllowedAPIKeyEnvCoversEveryProviderDefault(t *testing.T) {
	for _, p := range KnownProviders() {
		if !IsAllowedAPIKeyEnv(DefaultAPIKeyEnv(p)) {
			t.Fatalf("default for %s is outside the catalog", p)
		}
	}
}
