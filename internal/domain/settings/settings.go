// Package settings holds the preferences a workspace and its projects carry
// alongside their data — today the requirement quality rule set, which decides
// which normative vocabulary the linter judges a project's wording against.
//
// Settings are layered: platform defaults, then the workspace's, then the
// project's, each overriding only the keys it names. That way a workspace sets
// house style once and a project departs from it only where it must.
package settings

import (
	"errors"
	"fmt"

	"github.com/openv/requirements-platform/internal/domain/quality"
)

// ErrInvalidRules flags a rule set a caller tried to store that names an
// unknown convention, rule or severity — user-facing validation (400), not a
// storage failure.
var ErrInvalidRules = errors.New("invalid quality rules")

// Store reads and writes the raw settings objects. Both levels return an empty
// map (never nil) when nothing has been stored.
type Store interface {
	OrgSettings(orgID string) (map[string]interface{}, error)
	SetOrgSettings(orgID string, settings map[string]interface{}) error
	ProjectSettings(projectID string) (map[string]interface{}, error)
	SetProjectSettings(projectID string, settings map[string]interface{}) error
}

// QualityRules is what a caller needs to show or edit the rules: the resolved
// set the linter uses, plus the two overrides that produced it, so the UI can
// tell "inherited" from "set here". A nil override means that level sets
// nothing.
type QualityRules struct {
	Effective quality.RuleSet  `json:"effective"`
	Workspace *quality.RuleSet `json:"workspace"`
	Project   *quality.RuleSet `json:"project"`
	// Summary is Effective rendered as the sentence agents read before they
	// draft a requirement.
	Summary string `json:"summary"`
}

// Service resolves and updates settings.
type Service interface {
	// ProjectQualityRules resolves the rules a project is linted against.
	// projectID may be empty to read the workspace level alone.
	ProjectQualityRules(orgID, projectID string) (*QualityRules, error)
	// WorkspaceQualityRules resolves the workspace level on its own.
	WorkspaceQualityRules(orgID string) (*QualityRules, error)
	// SetWorkspaceQualityRules stores (or, given an empty rule set, clears)
	// the workspace's house style.
	SetWorkspaceQualityRules(orgID string, rs quality.RuleSet) (*QualityRules, error)
	// SetProjectQualityRules stores (or clears) a project's override.
	SetProjectQualityRules(orgID, projectID string, rs quality.RuleSet) (*QualityRules, error)
	// EffectiveRuleSet is the lint path's shortcut: the resolved rule set
	// alone, falling back to the defaults if settings cannot be read, so a
	// storage hiccup degrades the report's wording rather than failing it.
	EffectiveRuleSet(orgID, projectID string) quality.RuleSet
}

// DefaultService is the standard Service.
type DefaultService struct {
	store Store
}

// NewService creates the settings service.
func NewService(store Store) *DefaultService {
	return &DefaultService{store: store}
}

// levels reads both levels' settings, tolerating an empty orgID or projectID.
func (s *DefaultService) levels(orgID, projectID string) (org, project map[string]interface{}, err error) {
	org, project = map[string]interface{}{}, map[string]interface{}{}
	if orgID != "" {
		if org, err = s.store.OrgSettings(orgID); err != nil {
			return nil, nil, err
		}
	}
	if projectID != "" {
		if project, err = s.store.ProjectSettings(projectID); err != nil {
			return nil, nil, err
		}
	}
	return org, project, nil
}

func (s *DefaultService) rules(orgSettings, projectSettings map[string]interface{}) *QualityRules {
	out := &QualityRules{Effective: quality.Resolve(orgSettings, projectSettings)}
	if rs, ok := quality.FromSettings(orgSettings); ok {
		out.Workspace = &rs
	}
	if rs, ok := quality.FromSettings(projectSettings); ok {
		out.Project = &rs
	}
	out.Summary = out.Effective.Describe()
	return out
}

// ProjectQualityRules implements Service.
func (s *DefaultService) ProjectQualityRules(orgID, projectID string) (*QualityRules, error) {
	org, project, err := s.levels(orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.rules(org, project), nil
}

// WorkspaceQualityRules implements Service.
func (s *DefaultService) WorkspaceQualityRules(orgID string) (*QualityRules, error) {
	return s.ProjectQualityRules(orgID, "")
}

// SetWorkspaceQualityRules implements Service.
func (s *DefaultService) SetWorkspaceQualityRules(orgID string, rs quality.RuleSet) (*QualityRules, error) {
	if err := quality.Validate(rs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRules, err)
	}
	current, err := s.store.OrgSettings(orgID)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetOrgSettings(orgID, withQuality(current, rs)); err != nil {
		return nil, err
	}
	return s.WorkspaceQualityRules(orgID)
}

// SetProjectQualityRules implements Service.
func (s *DefaultService) SetProjectQualityRules(orgID, projectID string, rs quality.RuleSet) (*QualityRules, error) {
	if err := quality.Validate(rs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRules, err)
	}
	current, err := s.store.ProjectSettings(projectID)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetProjectSettings(projectID, withQuality(current, rs)); err != nil {
		return nil, err
	}
	return s.ProjectQualityRules(orgID, projectID)
}

// EffectiveRuleSet implements Service.
func (s *DefaultService) EffectiveRuleSet(orgID, projectID string) quality.RuleSet {
	org, project, err := s.levels(orgID, projectID)
	if err != nil {
		return quality.DefaultRuleSet()
	}
	return quality.Resolve(org, project)
}

// withQuality returns the settings object with the quality key replaced —
// or removed, when the rule set names nothing, which is how a level clears its
// override and goes back to inheriting.
func withQuality(settings map[string]interface{}, rs quality.RuleSet) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range settings {
		out[k] = v
	}
	stored := quality.ToSettings(rs)
	if len(stored) == 0 {
		delete(out, quality.SettingsKey)
		return out
	}
	out[quality.SettingsKey] = stored
	return out
}
