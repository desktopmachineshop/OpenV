package postgres

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// The provider setting repository's columns and scans (refactor plan X13's
// characterization for provider_setting_repository.go, in the manner of the
// embedding repository's), pinned as found: the nine columns FindByProvider
// and List read back, through the one list both of them send, each holding a
// value no other does, so a swap shows; the NULLs a column may hold and what
// each read answers for them; the scan error that names a column's place,
// unwrapped; what a read answers for a row no one has, for a malformed org
// id, and for a setting stored with no org; nil or [] for a list that matched
// nothing; List's order; what last_detected reads back as; and what Upsert
// writes into each column, refuses, and hands back. The existing tests
// (provider_setting_repository_test.go, and malformed_parent_list_test.go for
// List's malformed-id answer) pin the stored id an update answers and that
// answer alone; none reads the columns back, a NULL, a scan's place, the
// order, or what Upsert writes. The repository has no transaction (every
// method sends one statement), so there is none to pin.
//
// Every test that reads or writes rows gets its own database through rtDB;
// none takes a lock, so none needs NOWAIT.

// rtPSSetting is a setting whose every field differs from the one for any
// other n, so a swap of two columns in a scan, or of two rows, shows. An odd
// n is API-key auth and disabled; an even n is subscription auth and enabled.
func rtPSSetting(org, provider string, n int) *providers.ProviderSetting {
	p := &providers.ProviderSetting{
		ID:           uuid.New().String(),
		OrgID:        org,
		Provider:     provider,
		AuthMode:     providers.AuthSubscriptionCLI,
		APIKeyEnv:    fmt.Sprintf("KEY_ENV_%d", n),
		DefaultModel: fmt.Sprintf("model-%d", n),
		Enabled:      n%2 == 0,
		LastDetected: map[string]interface{}{"n": float64(n), "name": fmt.Sprintf("detected %d", n), "list": []interface{}{"x", float64(n)}},
		UpdatedAt:    rtAt(n),
	}
	if n%2 == 1 {
		p.AuthMode = providers.AuthAPIKey
	}
	return p
}

// rtPSUpsert stores a setting, failing the test on an error, and answers it.
func rtPSUpsert(t *testing.T, repo *ProviderSettingRepository, p *providers.ProviderSetting) *providers.ProviderSetting {
	t.Helper()
	if err := repo.Upsert(p); err != nil {
		t.Fatalf("Upsert %q/%q: %v", p.OrgID, p.Provider, err)
	}
	return p
}

// rtPSSansTime is a copy of a setting with its UpdatedAt zeroed, so the other
// fields can be compared with rtWantSame; the time is compared with
// rtWantTimestamp.
func rtPSSansTime(p *providers.ProviderSetting) providers.ProviderSetting {
	c := *p
	c.UpdatedAt = time.Time{}
	return c
}

// rtPSWant fails unless got is sent, every field, with the time as a TIMESTAMP
// column hands it back.
func rtPSWant(t *testing.T, what string, got, sent *providers.ProviderSetting) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: no setting, want %s/%s", what, sent.OrgID, sent.Provider)
	}
	rtWantTimestamp(t, what+" updated_at", got.UpdatedAt, sent.UpdatedAt)
	rtWantSame(t, what, rtPSSansTime(got), rtPSSansTime(sent))
}

// Every column FindByProvider reads back is the one stored: the id, the org,
// the provider, the auth mode, the API key's env name, the default model, the
// enabled flag, last_detected as a JSON object (its numbers as float64, its
// lists as []interface{}) and updated_at. Another org's setting for the same
// provider is not the one read.
func TestProviderSettingFindByProviderReadsBackEveryColumn(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)
	org, other := uuid.New().String(), uuid.New().String()

	claude := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderClaudeCode, 1))
	codex := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderCodexCLI, 2))
	rtPSUpsert(t, repo, rtPSSetting(other, providers.ProviderClaudeCode, 3))

	for _, want := range []*providers.ProviderSetting{claude, codex} {
		got, err := repo.FindByProvider(org, want.Provider)
		if err != nil {
			t.Fatalf("FindByProvider(%s): %v", want.Provider, err)
		}
		rtPSWant(t, "FindByProvider("+want.Provider+")", got, want)
	}
}

// Every column List reads back is the one stored, for each of an org's
// settings, and the rows come in provider order: the two settings are stored
// the other way round, and another org's setting is in no list of this org.
func TestProviderSettingListReadsBackEveryColumnInProviderOrder(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)
	org, other := uuid.New().String(), uuid.New().String()

	codex := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderCodexCLI, 2))
	claude := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderClaudeCode, 1))
	rtPSUpsert(t, repo, rtPSSetting(other, providers.ProviderGeminiCLI, 3))

	list, err := repo.List(org)
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %s, %v; want two settings", rtJSON(list), err)
	}
	for i, want := range []*providers.ProviderSetting{claude, codex} {
		rtPSWant(t, fmt.Sprintf("List[%d]", i), list[i], want)
	}
}

// A read answers nil, nil for a row no one has: an org with no setting for the
// provider, a provider the org has none for, and another org's setting. A
// malformed org id is no row either (noRow), so FindByProvider and List answer
// nil, nil for it; List's answer for nothing matched is a nil list (JSON
// null), never [].
func TestProviderSettingReadsAnswerNothingForNoRow(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)
	org, other := uuid.New().String(), uuid.New().String()
	rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderClaudeCode, 1))

	got, err := repo.FindByProvider(uuid.New().String(), providers.ProviderClaudeCode)
	if got != nil || err != nil {
		t.Errorf("FindByProvider of an org with no setting = %v, %v; want nil, nil", got, err)
	}
	got, err = repo.FindByProvider(org, providers.ProviderCodexCLI)
	if got != nil || err != nil {
		t.Errorf("FindByProvider of a provider the org has none for = %v, %v; want nil, nil", got, err)
	}
	got, err = repo.FindByProvider(other, providers.ProviderClaudeCode)
	if got != nil || err != nil {
		t.Errorf("FindByProvider of another org's setting = %v, %v; want nil, nil", got, err)
	}
	got, err = repo.FindByProvider("not-a-uuid", providers.ProviderClaudeCode)
	if got != nil || err != nil {
		t.Errorf("FindByProvider of a malformed org id = %v, %v; want nil, nil", got, err)
	}

	list, err := repo.List(other)
	rtWantNil(t, "List of an org with no settings", list, err)
	list, err = repo.List(uuid.New().String())
	rtWantNil(t, "List of an org no row has", list, err)
	list, err = repo.List("not-a-uuid")
	rtWantNil(t, "List of a malformed org id", list, err)
}

// A setting stored with no org (an empty org id) is stored with a NULL org_id.
// The unique index does not match NULLs, so each such Upsert adds a row, and
// neither read answers one: the org filter compares to NULL, so the COALESCE
// both reads apply to org_id never sees a NULL.
func TestProviderSettingWithNoOrgIsInNoRead(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)

	rtPSUpsert(t, repo, rtPSSetting("", providers.ProviderClaudeCode, 1))
	rtPSUpsert(t, repo, rtPSSetting("", providers.ProviderClaudeCode, 2))

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM provider_settings WHERE org_id IS NULL AND provider = $1`,
		providers.ProviderClaudeCode).Scan(&rows); err != nil || rows != 2 {
		t.Errorf("rows with no org = %d, %v; want two, one per Upsert", rows, err)
	}
	got, err := repo.FindByProvider("", providers.ProviderClaudeCode)
	if got != nil || err != nil {
		t.Errorf("FindByProvider with no org = %v, %v; want nil, nil", got, err)
	}
	list, err := repo.List("")
	rtWantNil(t, "List with no org", list, err)
}

// last_detected reads back as a map in every case: a stored object as it was
// sent; an empty map as '{}', which an Upsert of a nil map stores; a NULL as
// an empty map; and a JSON value that is not an object, which the repository
// silently reads as an empty map, not as an error. A JSON null is the one
// that stays nil, since decoding null into a map sets it to nil. Both reads
// give the same answer.
func TestProviderSettingLastDetectedReadsAsAMap(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)
	org := uuid.New().String()

	nilMap := &providers.ProviderSetting{ID: uuid.New().String(), OrgID: org, Provider: providers.ProviderClaudeCode,
		AuthMode: providers.AuthAPIKey, Enabled: true, UpdatedAt: rtAt(1)}
	rtPSUpsert(t, repo, nilMap)
	var stored string
	if err := db.QueryRow(`SELECT last_detected::text FROM provider_settings WHERE id = $1`, nilMap.ID).Scan(&stored); err != nil || stored != "{}" {
		t.Fatalf("an Upsert of a nil last_detected stored %q, %v; want {}", stored, err)
	}
	got, err := repo.FindByProvider(org, providers.ProviderClaudeCode)
	if err != nil || got == nil || got.LastDetected == nil || len(got.LastDetected) != 0 {
		t.Fatalf("FindByProvider over {} = %v, %v; want an empty, non-nil map", rtJSON(got), err)
	}

	for _, c := range []struct {
		name, stored string
		want         map[string]interface{}
		nilMap       bool
	}{
		{name: "an array", stored: `[1, 2]`, want: map[string]interface{}{}},
		{name: "a string", stored: `"text"`, want: map[string]interface{}{}},
		{name: "a JSON null", stored: `null`, nilMap: true},
	} {
		rtSeed(t, db, `UPDATE provider_settings SET last_detected = $2::jsonb WHERE id = $1`, nilMap.ID, c.stored)
		got, err := repo.FindByProvider(org, providers.ProviderClaudeCode)
		if err != nil || got == nil {
			t.Errorf("FindByProvider over %s: %v, %v; want the setting and no error", c.name, got, err)
			continue
		}
		if c.nilMap {
			if got.LastDetected != nil {
				t.Errorf("FindByProvider over %s: last_detected = %v, want nil", c.name, got.LastDetected)
			}
		} else {
			rtWantSame(t, "FindByProvider over "+c.name+" last_detected", got.LastDetected, c.want)
		}
		list, err := repo.List(org)
		if err != nil || len(list) != 1 || (list[0].LastDetected == nil) != c.nilMap {
			t.Errorf("List over %s = %s, %v; want one setting, last_detected nil: %v", c.name, rtJSON(list), err, c.nilMap)
		}
	}

	// A NULL last_detected (the column has no NOT NULL once dropped) reads
	// as an empty map, with no error.
	rtSeed(t, db, `ALTER TABLE provider_settings ALTER COLUMN last_detected DROP NOT NULL`)
	rtSeed(t, db, `UPDATE provider_settings SET last_detected = NULL WHERE id = $1`, nilMap.ID)
	got, err = repo.FindByProvider(org, providers.ProviderClaudeCode)
	if err != nil || got == nil || got.LastDetected == nil || len(got.LastDetected) != 0 {
		t.Errorf("FindByProvider over a NULL last_detected = %v, %v; want an empty, non-nil map", rtJSON(got), err)
	}
	list, err := repo.List(org)
	if err != nil || len(list) != 1 || list[0].LastDetected == nil || len(list[0].LastDetected) != 0 {
		t.Errorf("List over a NULL last_detected = %s, %v; want one setting with an empty, non-nil map", rtJSON(list), err)
	}
}

// rtPSScanCase is one column a row can hold NULL in, with its place among the
// nine the reads scan (id 0, the org 1, provider 2, auth_mode 3,
// api_key_env 4, default_model 5, enabled 6, last_detected 7, updated_at 8),
// and the message database/sql gives for a NULL stored into it. The id is the
// primary key, so no row holds it NULL; the org is COALESCEd; last_detected
// is the NULL that reads as an empty map (TestProviderSettingLastDetectedReadsAsAMap).
type rtPSScanCase struct {
	column  string
	index   int
	message string
	// filtered is set for provider, which FindByProvider's WHERE compares
	// to its argument: a NULL provider matches no FindByProvider, so that
	// read answers nil, nil and does not scan the row.
	filtered bool
}

// A NULL in a column the reads scan fails List, unwrapped, with no list (nil),
// naming the column and its place in the scan. FindByProvider fails the same
// way for every such column but provider, which its WHERE compares, so a NULL
// provider matches no row there and it answers nil, nil.
func TestProviderSettingNullColumnFailsTheScanNamingItsPlace(t *testing.T) {
	const str = "converting NULL to string is unsupported"
	for _, c := range []rtPSScanCase{
		{column: "provider", index: 2, message: str, filtered: true},
		{column: "auth_mode", index: 3, message: str},
		{column: "api_key_env", index: 4, message: str},
		{column: "default_model", index: 5, message: str},
		{column: "enabled", index: 6, message: "sql/driver: couldn't convert <nil> (<nil>) into type bool"},
		{column: "updated_at", index: 8, message: "unsupported Scan, storing driver.Value type <nil> into type *time.Time"},
	} {
		t.Run(c.column, func(t *testing.T) {
			db := rtDB(t)
			repo := NewProviderSettingRepository(db)
			org := uuid.New().String()
			p := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderClaudeCode, 1))

			rtSeed(t, db, `ALTER TABLE provider_settings ALTER COLUMN `+c.column+` DROP NOT NULL`)
			rtSeed(t, db, `UPDATE provider_settings SET `+c.column+` = NULL WHERE id = $1`, p.ID)

			want := fmt.Sprintf("sql: Scan error on column index %d, name %q: %s", c.index, c.column, c.message)
			got, err := repo.FindByProvider(org, providers.ProviderClaudeCode)
			if c.filtered {
				if got != nil || err != nil {
					t.Errorf("FindByProvider over a NULL %s = %s, %v; want nil, nil (no row matches)", c.column, rtJSON(got), err)
				}
			} else if got != nil || err == nil || err.Error() != want {
				t.Errorf("FindByProvider over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(got), err, want)
			}
			list, err := repo.List(org)
			if list != nil || err == nil || err.Error() != want {
				t.Errorf("List over a NULL %s = %s, %v; want nil and %s", c.column, rtJSON(list), err, want)
			}
		})
	}
}

// Upsert writes every column of a new row as sent: the id, the org, the
// provider, the auth mode, the API key's env name, the default model, the
// enabled flag, last_detected as JSON and updated_at. A second Upsert of the
// same org and provider updates the row in place: its six updatable columns
// take the new values, the id stays the one the row was inserted with, and
// there is still one row.
func TestProviderSettingUpsertWritesEveryColumnAndUpdatesInPlace(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)
	org := uuid.New().String()

	first := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderClaudeCode, 1))
	type rawRow struct {
		id, org, provider, authMode, apiKeyEnv, defaultModel string
		enabled                                              bool
		lastDetected                                         string
		updatedAt                                            time.Time
	}
	read := func(what string) rawRow {
		var r rawRow
		if err := db.QueryRow(`SELECT id::text, COALESCE(org_id::text, ''), provider, auth_mode, api_key_env, default_model,
				enabled, last_detected::text, updated_at FROM provider_settings WHERE org_id = $1 AND provider = $2`,
			org, providers.ProviderClaudeCode).Scan(&r.id, &r.org, &r.provider, &r.authMode, &r.apiKeyEnv, &r.defaultModel,
			&r.enabled, &r.lastDetected, &r.updatedAt); err != nil {
			t.Fatalf("read the row (%s): %v", what, err)
		}
		return r
	}
	r := read("after the first Upsert")
	if r.id != first.ID || r.org != org || r.provider != providers.ProviderClaudeCode || r.authMode != first.AuthMode ||
		r.apiKeyEnv != first.APIKeyEnv || r.defaultModel != first.DefaultModel || r.enabled != first.Enabled {
		t.Errorf("the row after the first Upsert = %+v, want the setting as sent %+v", r, first)
	}
	rtWantStoredJSON(t, "the row's last_detected", r.lastDetected, first.LastDetected)
	rtWantTimestamp(t, "the row's updated_at", r.updatedAt, first.UpdatedAt)

	again := rtPSSetting(org, providers.ProviderClaudeCode, 2)
	again.ID = uuid.New().String()
	rtPSUpsert(t, repo, again)
	r = read("after the second Upsert")
	if r.id != first.ID || r.authMode != again.AuthMode || r.apiKeyEnv != again.APIKeyEnv ||
		r.defaultModel != again.DefaultModel || r.enabled != again.Enabled {
		t.Errorf("the row after the second Upsert = %+v, want the id %s and the updated setting %+v", r, first.ID, again)
	}
	rtWantStoredJSON(t, "the row's last_detected after the second Upsert", r.lastDetected, again.LastDetected)
	rtWantTimestamp(t, "the row's updated_at after the second Upsert", r.updatedAt, again.UpdatedAt)

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM provider_settings`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("rows = %d, %v; want one", rows, err)
	}
}

// Upsert hands back what Postgres refuses, as it came, and stores nothing:
// an id or an org id that is not a uuid (the refusal of a malformed uuid); a
// setting whose id is another row's (the primary key, a unique violation,
// with the constraint's name); and a last_detected that JSON cannot encode,
// which is refused before any statement is sent.
func TestProviderSettingUpsertHandsBackWhatItRefuses(t *testing.T) {
	db := rtDB(t)
	repo := NewProviderSettingRepository(db)
	org := uuid.New().String()
	stored := rtPSUpsert(t, repo, rtPSSetting(org, providers.ProviderClaudeCode, 1))

	bad := rtPSSetting(org, providers.ProviderCodexCLI, 2)
	bad.ID = "not-a-uuid"
	rtWantRefused(t, "Upsert of a malformed id", repo.Upsert(bad))

	bad = rtPSSetting("not-a-uuid", providers.ProviderCodexCLI, 3)
	rtWantRefused(t, "Upsert of a malformed org id", repo.Upsert(bad))

	clash := rtPSSetting(uuid.New().String(), providers.ProviderGeminiCLI, 4)
	clash.ID = stored.ID
	rtWantPQ(t, "Upsert of another row's id", repo.Upsert(clash), "23505", "provider_settings_pkey")

	unencodable := rtPSSetting(org, providers.ProviderCodexCLI, 5)
	unencodable.LastDetected = rtUnencodable
	rtWantUnencodable(t, "Upsert", repo.Upsert(unencodable))

	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM provider_settings`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("rows after the refused Upserts = %d, %v; want the one stored setting", rows, err)
	}
}

// A database that fails a statement for any other reason hands its error back
// as it came, with no setting and no list (nil): Upsert, FindByProvider and
// List each. It needs no server.
func TestProviderSettingHandsBackADatabaseFailure(t *testing.T) {
	repo := NewProviderSettingRepository(rtClosedDB(t))
	org := uuid.New().String()

	rtWantClosed(t, "Upsert", repo.Upsert(rtPSSetting(org, providers.ProviderClaudeCode, 1)), "")
	got, err := repo.FindByProvider(org, providers.ProviderClaudeCode)
	rtWantClosed(t, "FindByProvider", err, "")
	if got != nil {
		t.Errorf("FindByProvider on a failing database answered %v, want nil", rtJSON(got))
	}
	list, err := repo.List(org)
	rtWantClosed(t, "List", err, "")
	if list != nil {
		t.Errorf("List on a failing database answered %s, want nil", rtJSON(list))
	}
}
