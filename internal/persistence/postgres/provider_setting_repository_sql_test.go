package postgres

import (
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// X13's literal equality for the provider setting repository: the two reads'
// statements, assembled from providerSettingColumns, are the literals they
// replaced, byte for byte (spacing, line breaks and tabs included), and each
// read still sends its own. The statements are read off the wire by the
// recorder in pgkit_test.go, so the test needs no server and no database, and
// checks what each method sends, not a copy of it. The repository has no
// transaction: each read sends its one statement and nothing else. Upsert's
// INSERT keeps its literal and is not in this file.

const (
	// oldProviderSettingFindByProviderSelect, FindByProvider
	oldProviderSettingFindByProviderSelect = `
		SELECT id, COALESCE(org_id::text, ''), provider, auth_mode, api_key_env, default_model, enabled, last_detected, updated_at
		FROM provider_settings
		WHERE org_id = NULLIF($1, '')::uuid AND provider = $2
	`
	// oldProviderSettingListSelect, List
	oldProviderSettingListSelect = `
		SELECT id, COALESCE(org_id::text, ''), provider, auth_mode, api_key_env, default_model, enabled, last_detected, updated_at
		FROM provider_settings
		WHERE org_id = NULLIF($1, '')::uuid
		ORDER BY provider
	`
)

// The list the base wrote out, on one line.
const oldProviderSettingColumns = `id, COALESCE(org_id::text, ''), provider, auth_mode, api_key_env, default_model, enabled, last_detected, updated_at`

// The columns are the old list.
func TestProviderSettingColumnsAreTheOldList(t *testing.T) {
	if providerSettingColumns != oldProviderSettingColumns {
		t.Errorf("providerSettingColumns =\n  %q\nwant\n  %q", providerSettingColumns, oldProviderSettingColumns)
	}
}

// Each read sends its old literal and nothing else, and answers as it did for
// no row: FindByProvider (nil, nil), and List a nil list (JSON null), never
// []. A malformed org id is no row either: the statement is sent and answers
// the same.
func TestProviderSettingReadsSendTheirOldLiterals(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewProviderSettingRepository(db)
	org := uuid.New().String()

	if got, err := repo.FindByProvider(org, providers.ProviderClaudeCode); got != nil || err != nil {
		t.Errorf("FindByProvider with no row: %v, %v; want nil, nil", got, err)
	}
	wantEvents(t, "FindByProvider", rec.take(), "QUERY "+oldProviderSettingFindByProviderSelect)

	if got, err := repo.FindByProvider("not-a-uuid", providers.ProviderClaudeCode); got != nil || err != nil {
		t.Errorf("FindByProvider of a malformed org id: %v, %v; want nil, nil", got, err)
	}
	wantEvents(t, "FindByProvider of a malformed org id", rec.take(), "QUERY "+oldProviderSettingFindByProviderSelect)

	list, err := repo.List(org)
	rtWantNil(t, "List with no row", list, err)
	wantEvents(t, "List", rec.take(), "QUERY "+oldProviderSettingListSelect)

	list, err = repo.List("not-a-uuid")
	rtWantNil(t, "List of a malformed org id", list, err)
	wantEvents(t, "List of a malformed org id", rec.take(), "QUERY "+oldProviderSettingListSelect)
}

// The scans read their columns in the order of the list, one row each, as the
// recorder answers a statement with a row of values: FindByProvider's row and
// List's row each land in the fields they land in today, with last_detected
// decoded from its JSON into a map. (The scan-error places are pinned by
// provider_setting_repository_columns_test.go.)
func TestProviderSettingScansReadTheirColumnsInOrder(t *testing.T) {
	db, rec := recorderDB(t)
	repo := NewProviderSettingRepository(db)
	org := uuid.New().String()
	when := time.Date(2026, 3, 14, 9, 26, 53, 0, time.UTC)
	values := []driver.Value{"the-id", "the-org", "the-provider", "the-auth", "the-env", "the-model",
		true, []byte(`{"version": "1.2", "count": 3}`), when}
	rec.answers = []recAnswer{
		{"AND provider = $2", values},
		{"ORDER BY provider", values},
	}
	want := &providers.ProviderSetting{ID: "the-id", OrgID: "the-org", Provider: "the-provider", AuthMode: "the-auth",
		APIKeyEnv: "the-env", DefaultModel: "the-model", Enabled: true,
		LastDetected: map[string]interface{}{"version": "1.2", "count": float64(3)}, UpdatedAt: when}

	got, err := repo.FindByProvider(org, "the-provider")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("FindByProvider = %#v, %v; want %#v", got, err, want)
	}
	list, err := repo.List(org)
	if err != nil || !reflect.DeepEqual(list, []*providers.ProviderSetting{want}) {
		t.Errorf("List = %#v, %v; want [%#v]", list, err, want)
	}
}
