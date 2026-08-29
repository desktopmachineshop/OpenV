// Package attributes implements org- and project-configurable typed attribute
// definitions (issue #219). A definition names an extra typed field an
// organization wants on its artifacts (e.g. "priority" as an enum, "due_date"
// as a date); values live in the existing artifacts.attributes JSONB, so this
// package adds a vocabulary and a validator on top of storage that already
// exists — no data migration.
//
// Scoping: a definition is org-wide (OrgID set, ProjectID nil) or
// project-scoped (ProjectID set). The effective set for a project is the
// org-wide definitions overlaid with that project's own — a project-scoped
// definition with the same (applies_to_type, key) overrides the org-wide one.
package attributes

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Data type values a definition may carry.
const (
	DataTypeText    = "text"
	DataTypeNumber  = "number"
	DataTypeDate    = "date"
	DataTypeEnum    = "enum"
	DataTypeBoolean = "boolean"
)

// Error definitions.
var (
	ErrNotFound      = errors.New("attribute definition not found")
	ErrInvalidScope  = errors.New("a definition must be either org-wide (org_id) or project-scoped (project_id), not both or neither")
	ErrKeyRequired   = errors.New("attribute key is required")
	ErrInvalidKey    = errors.New("attribute key must contain only lowercase letters, numbers, and underscores")
	ErrInvalidType   = errors.New("data_type must be one of: text, number, date, enum, boolean")
	ErrEnumValues    = errors.New("an enum definition needs at least one enum value")
	ErrInvalidTarget = errors.New("applies_to_type must be a known artifact type or empty for all types")
)

// validDataTypes is the closed set of data types.
var validDataTypes = map[string]bool{
	DataTypeText:    true,
	DataTypeNumber:  true,
	DataTypeDate:    true,
	DataTypeEnum:    true,
	DataTypeBoolean: true,
}

// Definition is one typed attribute an org (or project) wants on its artifacts.
type Definition struct {
	ID string `json:"id"`
	// Exactly one of OrgID / ProjectID is set. OrgID set means org-wide;
	// ProjectID set means project-scoped (an override or a project-only field).
	OrgID     *string `json:"org_id"`
	ProjectID *string `json:"project_id"`
	// Key is the attributes-map key the value is stored under (stable id).
	Key string `json:"key"`
	// Label is the human-facing name shown in the editor.
	Label string `json:"label"`
	// DataType is one of the DataType* constants.
	DataType string `json:"data_type"`
	// EnumValues is the allowed set when DataType == enum (else empty).
	EnumValues []string `json:"enum_values"`
	// AppliesToType restricts the definition to one artifact type key, or ""
	// for all types.
	AppliesToType string `json:"applies_to_type"`
	// Required, when true, means a value must be present on artifact create.
	Required  bool      `json:"required"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateDefinitionRequest is the payload for defining a new attribute.
type CreateDefinitionRequest struct {
	OrgID         *string  `json:"org_id"`
	ProjectID     *string  `json:"project_id"`
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	DataType      string   `json:"data_type"`
	EnumValues    []string `json:"enum_values"`
	AppliesToType string   `json:"applies_to_type"`
	Required      bool     `json:"required"`
	SortOrder     int      `json:"sort_order"`
}

// UpdateDefinitionRequest replaces a definition's editable fields wholesale.
// Scope (org_id/project_id) and key are immutable once created.
type UpdateDefinitionRequest struct {
	Label         string   `json:"label"`
	DataType      string   `json:"data_type"`
	EnumValues    []string `json:"enum_values"`
	AppliesToType string   `json:"applies_to_type"`
	Required      bool     `json:"required"`
	SortOrder     int      `json:"sort_order"`
}

// TypeValidator reports whether an artifact type key is known. The domain
// package stays decoupled from the artifacts catalog by taking this as a
// dependency; "" (all types) is always allowed and never passed here.
type TypeValidator func(typeKey string) bool

// Repository defines persistence operations for attribute definitions.
type Repository interface {
	Create(def *Definition) error
	Update(def *Definition) error
	Delete(id string) error
	Get(id string) (*Definition, error)
	// ListByOrg returns the org-wide definitions for an org (project_id NULL).
	ListByOrg(orgID string) ([]*Definition, error)
	// ListByProject returns the project-scoped definitions for a project.
	ListByProject(projectID string) ([]*Definition, error)
}

// Service defines attribute-definition domain logic.
type Service interface {
	CreateDefinition(req CreateDefinitionRequest) (*Definition, error)
	UpdateDefinition(id string, req UpdateDefinitionRequest) (*Definition, error)
	DeleteDefinition(id string) error
	GetDefinition(id string) (*Definition, error)
	ListByOrg(orgID string) ([]*Definition, error)
	ListByProject(projectID string) ([]*Definition, error)
	// EffectiveForProject returns the org-wide set overlaid with the project's
	// own definitions (project-scoped overrides win on matching
	// applies_to_type + key), sorted for stable UI rendering.
	EffectiveForProject(orgID, projectID string) ([]*Definition, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo     Repository
	validate TypeValidator
}

// NewDefaultService creates a service. typeValidator may be nil, in which case
// any applies_to_type is accepted (validation of the type key is skipped).
func NewDefaultService(repo Repository, typeValidator TypeValidator) *DefaultService {
	return &DefaultService{repo: repo, validate: typeValidator}
}

// normalizeAndValidate cleans and checks the definition-shaping fields shared
// by create and update.
func (s *DefaultService) normalizeAndValidate(key, dataType, appliesToType string, enumValues []string) (string, []string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil, ErrKeyRequired
	}
	if !validKey(key) {
		return "", nil, ErrInvalidKey
	}
	if !validDataTypes[dataType] {
		return "", nil, ErrInvalidType
	}
	cleaned := cleanEnumValues(enumValues)
	if dataType == DataTypeEnum && len(cleaned) == 0 {
		return "", nil, ErrEnumValues
	}
	if dataType != DataTypeEnum {
		cleaned = nil
	}
	if appliesToType != "" && s.validate != nil && !s.validate(appliesToType) {
		return "", nil, ErrInvalidTarget
	}
	return key, cleaned, nil
}

// CreateDefinition validates and persists a new definition.
func (s *DefaultService) CreateDefinition(req CreateDefinitionRequest) (*Definition, error) {
	if err := validateScope(req.OrgID, req.ProjectID); err != nil {
		return nil, err
	}
	key, enumValues, err := s.normalizeAndValidate(req.Key, req.DataType, req.AppliesToType, req.EnumValues)
	if err != nil {
		return nil, err
	}
	def := &Definition{
		ID:            uuid.New().String(),
		OrgID:         req.OrgID,
		ProjectID:     req.ProjectID,
		Key:           key,
		Label:         strings.TrimSpace(req.Label),
		DataType:      req.DataType,
		EnumValues:    enumValues,
		AppliesToType: req.AppliesToType,
		Required:      req.Required,
		SortOrder:     req.SortOrder,
		CreatedAt:     time.Now(),
	}
	if def.Label == "" {
		def.Label = def.Key
	}
	if err := s.repo.Create(def); err != nil {
		return nil, err
	}
	return def, nil
}

// UpdateDefinition replaces a definition's editable fields. Scope and key are
// immutable.
func (s *DefaultService) UpdateDefinition(id string, req UpdateDefinitionRequest) (*Definition, error) {
	def, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	_, enumValues, err := s.normalizeAndValidate(def.Key, req.DataType, req.AppliesToType, req.EnumValues)
	if err != nil {
		return nil, err
	}
	def.Label = strings.TrimSpace(req.Label)
	if def.Label == "" {
		def.Label = def.Key
	}
	def.DataType = req.DataType
	def.EnumValues = enumValues
	def.AppliesToType = req.AppliesToType
	def.Required = req.Required
	def.SortOrder = req.SortOrder
	if err := s.repo.Update(def); err != nil {
		return nil, err
	}
	return def, nil
}

// DeleteDefinition removes a definition. Values already stored under its key in
// artifact attributes are left untouched (storage is shared JSONB).
func (s *DefaultService) DeleteDefinition(id string) error {
	return s.repo.Delete(id)
}

// GetDefinition returns one definition by id.
func (s *DefaultService) GetDefinition(id string) (*Definition, error) {
	return s.repo.Get(id)
}

// ListByOrg returns the org-wide definitions for an org.
func (s *DefaultService) ListByOrg(orgID string) ([]*Definition, error) {
	return s.repo.ListByOrg(orgID)
}

// ListByProject returns the project-scoped definitions for a project.
func (s *DefaultService) ListByProject(projectID string) ([]*Definition, error) {
	return s.repo.ListByProject(projectID)
}

// EffectiveForProject merges org-wide definitions with a project's own.
func (s *DefaultService) EffectiveForProject(orgID, projectID string) ([]*Definition, error) {
	var orgWide []*Definition
	if orgID != "" {
		var err error
		orgWide, err = s.repo.ListByOrg(orgID)
		if err != nil {
			return nil, err
		}
	}
	var projectScoped []*Definition
	if projectID != "" {
		var err error
		projectScoped, err = s.repo.ListByProject(projectID)
		if err != nil {
			return nil, err
		}
	}
	return MergeEffective(orgWide, projectScoped), nil
}

// MergeEffective overlays project-scoped definitions on org-wide ones. A
// project definition with the same (applies_to_type, key) as an org-wide one
// replaces it; project-only definitions are appended. The result is sorted by
// (applies_to_type, sort_order, key) for stable UI rendering. Exported so the
// effective-set resolution can be unit-tested without a repository.
func MergeEffective(orgWide, projectScoped []*Definition) []*Definition {
	type composite struct{ appliesTo, key string }
	byKey := map[composite]*Definition{}
	order := []composite{}
	add := func(def *Definition) {
		k := composite{def.AppliesToType, def.Key}
		if _, seen := byKey[k]; !seen {
			order = append(order, k)
		}
		byKey[k] = def
	}
	for _, d := range orgWide {
		add(d)
	}
	for _, d := range projectScoped {
		add(d) // project-scoped overwrites the org-wide entry at the same key
	}
	out := make([]*Definition, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	sortDefinitions(out)
	return out
}

// ValidateAttributes checks a submitted attributes map against the effective
// definitions for one artifact type. It only inspects definitions whose
// AppliesToType is "" (all) or equals artifactType.
//
// enforceRequired controls whether a required-but-absent value is an error:
// the artifact create path passes true (a wholesale new value set); the update
// path passes false so it never rejects a partial or attribute-preserving
// write (see the nil=no-change contract in artifacts.UpdateArtifact — callers
// must skip this call entirely when attributes are nil).
//
// A nil/absent value for a non-required attribute is always accepted (the
// attribute simply is not set); type/enum validation runs on present,
// non-nil values.
func ValidateAttributes(defs []*Definition, artifactType string, attrs map[string]interface{}, enforceRequired bool) error {
	for _, def := range defs {
		if def.AppliesToType != "" && def.AppliesToType != artifactType {
			continue
		}
		raw, present := attrs[def.Key]
		if !present || raw == nil {
			if def.Required && enforceRequired {
				return fmt.Errorf("attribute %q (%s) is required", def.Label, def.Key)
			}
			continue
		}
		if err := ValidateValue(def, raw); err != nil {
			return err
		}
	}
	return nil
}

// ValidateValue checks a single value against a definition's data type. An
// empty string is treated as "not set" and accepted (required-presence is
// handled by ValidateAttributes, which sees the empty string as present).
func ValidateValue(def *Definition, value interface{}) error {
	// Treat an explicit empty string as "cleared" for every type.
	if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
		return nil
	}
	switch def.DataType {
	case DataTypeText:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("attribute %q must be text", def.Key)
		}
	case DataTypeNumber:
		if !isNumber(value) {
			return fmt.Errorf("attribute %q must be a number", def.Key)
		}
	case DataTypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("attribute %q must be true or false", def.Key)
		}
	case DataTypeDate:
		s, ok := value.(string)
		if !ok || !validDate(s) {
			return fmt.Errorf("attribute %q must be a date (YYYY-MM-DD or RFC3339)", def.Key)
		}
	case DataTypeEnum:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("attribute %q must be one of the allowed values", def.Key)
		}
		for _, allowed := range def.EnumValues {
			if allowed == s {
				return nil
			}
		}
		return fmt.Errorf("attribute %q must be one of: %s", def.Key, strings.Join(def.EnumValues, ", "))
	default:
		return fmt.Errorf("attribute %q has an unknown data type %q", def.Key, def.DataType)
	}
	return nil
}
