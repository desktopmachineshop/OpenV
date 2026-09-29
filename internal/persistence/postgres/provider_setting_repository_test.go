package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/providers"
)

// TestProviderSettingUpsertAnswersTheStoredID pins that a second upsert of
// an org's provider, which updates the row the first one inserted, leaves
// that row's id on the setting it was given: the PUT answers with it. It
// used to keep the id it was given, which no row had. Postgres-gated
// (OPENV_TEST_DATABASE_URL).
func TestProviderSettingUpsertAnswersTheStoredID(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewProviderSettingRepository(db)
	orgID := uuid.New().String()

	first := &providers.ProviderSetting{ID: uuid.New().String(), OrgID: orgID, Provider: providers.ProviderClaudeCode,
		AuthMode: providers.AuthAPIKey, DefaultModel: "tour-model", Enabled: true, UpdatedAt: time.Now()}
	stored := first.ID
	if err := repo.Upsert(first); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if first.ID != stored {
		t.Fatalf("first Upsert set id %s, want the one it inserted, %s", first.ID, stored)
	}

	again := &providers.ProviderSetting{ID: uuid.New().String(), OrgID: orgID, Provider: providers.ProviderClaudeCode,
		AuthMode: providers.AuthSubscriptionCLI, Enabled: false, UpdatedAt: time.Now()}
	if err := repo.Upsert(again); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if again.ID != stored {
		t.Fatalf("second Upsert left id %s, want the stored row's %s", again.ID, stored)
	}

	row, err := repo.FindByProvider(orgID, providers.ProviderClaudeCode)
	if err != nil || row == nil {
		t.Fatalf("FindByProvider: %v, %v", row, err)
	}
	if row.ID != stored || row.AuthMode != providers.AuthSubscriptionCLI || row.Enabled {
		t.Fatalf("stored row = %+v, want id %s updated to the second setting", row, stored)
	}
}
