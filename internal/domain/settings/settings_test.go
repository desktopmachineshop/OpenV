package settings

import (
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/quality"
)

// memStore is an in-memory Store: two maps, plus an optional read failure so
// the degrade-to-defaults path can be exercised.
type memStore struct {
	orgs     map[string]map[string]interface{}
	projects map[string]map[string]interface{}
	readErr  error
}

func newStore() *memStore {
	return &memStore{orgs: map[string]map[string]interface{}{}, projects: map[string]map[string]interface{}{}}
}

func (m *memStore) OrgSettings(orgID string) (map[string]interface{}, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	if s, ok := m.orgs[orgID]; ok {
		return s, nil
	}
	return map[string]interface{}{}, nil
}

func (m *memStore) SetOrgSettings(orgID string, s map[string]interface{}) error {
	m.orgs[orgID] = s
	return nil
}

func (m *memStore) ProjectSettings(projectID string) (map[string]interface{}, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	if s, ok := m.projects[projectID]; ok {
		return s, nil
	}
	return map[string]interface{}{}, nil
}

func (m *memStore) SetProjectSettings(projectID string, s map[string]interface{}) error {
	m.projects[projectID] = s
	return nil
}

// TestWorkspaceHouseStyleInherits covers the arrangement the feature exists
// for: an admin sets the workspace's convention once and projects follow.
func TestWorkspaceHouseStyleInherits(t *testing.T) {
	svc := NewService(newStore())

	if _, err := svc.SetWorkspaceQualityRules("org-1", quality.RuleSet{Convention: quality.ConventionRFC2119}); err != nil {
		t.Fatal(err)
	}
	rules, err := svc.ProjectQualityRules("org-1", "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	if rules.Effective.Convention != quality.ConventionRFC2119 {
		t.Errorf("project convention = %q, want the workspace's", rules.Effective.Convention)
	}
	if rules.Workspace == nil {
		t.Error("workspace override should be reported so the UI can show where it came from")
	}
	if rules.Project != nil {
		t.Error("a project that set nothing must report no override")
	}
	if rules.Summary == "" {
		t.Error("summary is what agents read; it must never be empty")
	}
}

// TestProjectOverrideAndClearing covers a project departing from house style
// and later going back to inheriting it.
func TestProjectOverrideAndClearing(t *testing.T) {
	store := newStore()
	svc := NewService(store)
	if _, err := svc.SetWorkspaceQualityRules("org-1", quality.RuleSet{Convention: quality.ConventionRFC2119}); err != nil {
		t.Fatal(err)
	}

	rules, err := svc.SetProjectQualityRules("org-1", "proj-1", quality.RuleSet{Convention: quality.ConventionShall})
	if err != nil {
		t.Fatal(err)
	}
	if rules.Effective.Convention != quality.ConventionShall {
		t.Errorf("override ignored: %q", rules.Effective.Convention)
	}

	rules, err = svc.SetProjectQualityRules("org-1", "proj-1", quality.RuleSet{})
	if err != nil {
		t.Fatal(err)
	}
	if rules.Project != nil {
		t.Error("an empty rule set must clear the override")
	}
	if rules.Effective.Convention != quality.ConventionRFC2119 {
		t.Errorf("cleared project should inherit again, got %q", rules.Effective.Convention)
	}
	if _, still := store.projects["proj-1"][quality.SettingsKey]; still {
		t.Error("cleared override should be removed from storage, not stored empty")
	}
}

// TestSetRejectsInvalidRules keeps unknown conventions and severities out of
// storage, and lets the API tell a 400 from a 500.
func TestSetRejectsInvalidRules(t *testing.T) {
	store := newStore()
	svc := NewService(store)
	_, err := svc.SetWorkspaceQualityRules("org-1", quality.RuleSet{Convention: "iso-9001"})
	if !errors.Is(err, ErrInvalidRules) {
		t.Fatalf("err = %v, want ErrInvalidRules", err)
	}
	if len(store.orgs) != 0 {
		t.Error("an invalid rule set must not be stored")
	}
}

// TestSettingsPreserveOtherKeys guards future preferences: writing quality
// rules must not wipe whatever else lives in the settings object.
func TestSettingsPreserveOtherKeys(t *testing.T) {
	store := newStore()
	store.orgs["org-1"] = map[string]interface{}{"something-else": "keep me"}
	svc := NewService(store)
	if _, err := svc.SetWorkspaceQualityRules("org-1", quality.RuleSet{Convention: quality.ConventionShall}); err != nil {
		t.Fatal(err)
	}
	if store.orgs["org-1"]["something-else"] != "keep me" {
		t.Error("unrelated settings were dropped")
	}
}

// TestEffectiveRuleSetDegradesToDefaults keeps a storage failure from breaking
// the lint report: it falls back to the platform defaults.
func TestEffectiveRuleSetDegradesToDefaults(t *testing.T) {
	store := newStore()
	store.readErr = errors.New("database on fire")
	got := NewService(store).EffectiveRuleSet("org-1", "proj-1")
	if got.Convention != quality.ConventionShall {
		t.Errorf("convention = %q, want the default when settings cannot be read", got.Convention)
	}
}
