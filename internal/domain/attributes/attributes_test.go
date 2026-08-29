package attributes

import (
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

// fakeRepo is an in-memory Repository for service-level tests.
type fakeRepo struct {
	byID map[string]*Definition
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byID: map[string]*Definition{}} }

func (r *fakeRepo) Create(def *Definition) error { r.byID[def.ID] = def; return nil }
func (r *fakeRepo) Update(def *Definition) error {
	if _, ok := r.byID[def.ID]; !ok {
		return ErrNotFound
	}
	r.byID[def.ID] = def
	return nil
}
func (r *fakeRepo) Delete(id string) error {
	if _, ok := r.byID[id]; !ok {
		return ErrNotFound
	}
	delete(r.byID, id)
	return nil
}
func (r *fakeRepo) Get(id string) (*Definition, error) {
	if d, ok := r.byID[id]; ok {
		return d, nil
	}
	return nil, ErrNotFound
}
func (r *fakeRepo) ListByOrg(orgID string) ([]*Definition, error) {
	var out []*Definition
	for _, d := range r.byID {
		if d.OrgID != nil && *d.OrgID == orgID && d.ProjectID == nil {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r *fakeRepo) ListByProject(projectID string) ([]*Definition, error) {
	var out []*Definition
	for _, d := range r.byID {
		if d.ProjectID != nil && *d.ProjectID == projectID {
			out = append(out, d)
		}
	}
	return out, nil
}

// TestValidateValue covers each data type with a valid and an invalid value.
func TestValidateValue(t *testing.T) {
	cases := []struct {
		name    string
		def     *Definition
		value   interface{}
		wantErr bool
	}{
		{"text valid", &Definition{Key: "note", DataType: DataTypeText}, "hello", false},
		{"text invalid", &Definition{Key: "note", DataType: DataTypeText}, 42.0, true},

		{"number valid float", &Definition{Key: "score", DataType: DataTypeNumber}, 3.5, false},
		{"number valid int", &Definition{Key: "score", DataType: DataTypeNumber}, 7, false},
		{"number invalid string", &Definition{Key: "score", DataType: DataTypeNumber}, "3.5", true},

		{"boolean valid", &Definition{Key: "flag", DataType: DataTypeBoolean}, true, false},
		{"boolean invalid", &Definition{Key: "flag", DataType: DataTypeBoolean}, "true", true},

		{"date valid short", &Definition{Key: "due", DataType: DataTypeDate}, "2026-08-29", false},
		{"date valid rfc3339", &Definition{Key: "due", DataType: DataTypeDate}, "2026-08-29T10:00:00Z", false},
		{"date invalid", &Definition{Key: "due", DataType: DataTypeDate}, "not-a-date", true},
		{"date invalid type", &Definition{Key: "due", DataType: DataTypeDate}, 20260829, true},

		{"enum valid", &Definition{Key: "prio", DataType: DataTypeEnum, EnumValues: []string{"low", "high"}}, "high", false},
		{"enum invalid member", &Definition{Key: "prio", DataType: DataTypeEnum, EnumValues: []string{"low", "high"}}, "urgent", true},
		{"enum invalid type", &Definition{Key: "prio", DataType: DataTypeEnum, EnumValues: []string{"low"}}, 1.0, true},

		{"empty string accepted for typed field", &Definition{Key: "score", DataType: DataTypeNumber}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateValue(tc.def, tc.value)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateValue(%v) err = %v, wantErr %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

// TestValidateAttributes covers required enforcement and applies_to_type gating.
func TestValidateAttributes(t *testing.T) {
	defs := []*Definition{
		{Key: "priority", Label: "Priority", DataType: DataTypeEnum, EnumValues: []string{"low", "high"}, AppliesToType: "requirement", Required: true},
		{Key: "reviewed", Label: "Reviewed", DataType: DataTypeBoolean, AppliesToType: ""}, // all types
	}

	t.Run("valid requirement attributes pass", func(t *testing.T) {
		err := ValidateAttributes(defs, "requirement", map[string]interface{}{"priority": "high", "reviewed": true}, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("required missing on create is rejected", func(t *testing.T) {
		err := ValidateAttributes(defs, "requirement", map[string]interface{}{"reviewed": true}, true)
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("want required error, got %v", err)
		}
	})

	t.Run("required missing on update is allowed", func(t *testing.T) {
		if err := ValidateAttributes(defs, "requirement", map[string]interface{}{"reviewed": true}, false); err != nil {
			t.Fatalf("update should not enforce required: %v", err)
		}
	})

	t.Run("definition for other type is ignored", func(t *testing.T) {
		// priority applies only to requirement; a hazard need not carry it.
		if err := ValidateAttributes(defs, "hazard", map[string]interface{}{"reviewed": false}, true); err != nil {
			t.Fatalf("unexpected error for hazard: %v", err)
		}
	})

	t.Run("invalid enum value is rejected even without required", func(t *testing.T) {
		err := ValidateAttributes(defs, "requirement", map[string]interface{}{"priority": "urgent"}, false)
		if err == nil {
			t.Fatal("expected enum violation to be rejected")
		}
	})
}

// TestEffectiveForProject verifies org + project override resolution.
func TestEffectiveForProject(t *testing.T) {
	repo := newFakeRepo()
	svc := NewDefaultService(repo, nil)

	// Org-wide priority (enum low/high) and an org-wide owner (text).
	if _, err := svc.CreateDefinition(CreateDefinitionRequest{
		OrgID: strptr("org-1"), Key: "priority", Label: "Priority",
		DataType: DataTypeEnum, EnumValues: []string{"low", "high"}, AppliesToType: "requirement", SortOrder: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateDefinition(CreateDefinitionRequest{
		OrgID: strptr("org-1"), Key: "owner", Label: "Owner", DataType: DataTypeText, SortOrder: 2,
	}); err != nil {
		t.Fatal(err)
	}
	// Project override: same key+type "priority" but a wider enum, plus a
	// project-only "sprint".
	if _, err := svc.CreateDefinition(CreateDefinitionRequest{
		ProjectID: strptr("proj-1"), Key: "priority", Label: "Priority (project)",
		DataType: DataTypeEnum, EnumValues: []string{"low", "high", "urgent"}, AppliesToType: "requirement", SortOrder: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateDefinition(CreateDefinitionRequest{
		ProjectID: strptr("proj-1"), Key: "sprint", Label: "Sprint", DataType: DataTypeText, SortOrder: 3,
	}); err != nil {
		t.Fatal(err)
	}

	eff, err := svc.EffectiveForProject("org-1", "proj-1")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]*Definition{}
	for _, d := range eff {
		byKey[d.AppliesToType+"/"+d.Key] = d
	}
	if len(eff) != 3 {
		t.Fatalf("effective set size = %d, want 3 — override collapses requirement/priority (%+v)", len(eff), byKey)
	}
	// The project override wins for requirement/priority.
	prio := byKey["requirement/priority"]
	if prio == nil || prio.Label != "Priority (project)" || len(prio.EnumValues) != 3 {
		t.Fatalf("priority not overridden by project scope: %+v", prio)
	}
	// The org-only owner and project-only sprint both survive.
	if byKey["/owner"] == nil {
		t.Error("org-wide owner missing from effective set")
	}
	if byKey["/sprint"] == nil {
		t.Error("project-only sprint missing from effective set")
	}
}

// TestCreateDefinitionValidation covers scope + shape validation.
func TestCreateDefinitionValidation(t *testing.T) {
	svc := NewDefaultService(newFakeRepo(), func(typeKey string) bool { return typeKey == "requirement" })

	cases := []struct {
		name    string
		req     CreateDefinitionRequest
		wantErr error
	}{
		{"neither scope", CreateDefinitionRequest{Key: "k", DataType: DataTypeText}, ErrInvalidScope},
		{"both scopes", CreateDefinitionRequest{OrgID: strptr("o"), ProjectID: strptr("p"), Key: "k", DataType: DataTypeText}, ErrInvalidScope},
		{"empty key", CreateDefinitionRequest{OrgID: strptr("o"), Key: "", DataType: DataTypeText}, ErrKeyRequired},
		{"bad key", CreateDefinitionRequest{OrgID: strptr("o"), Key: "Bad Key", DataType: DataTypeText}, ErrInvalidKey},
		{"bad type", CreateDefinitionRequest{OrgID: strptr("o"), Key: "k", DataType: "money"}, ErrInvalidType},
		{"enum without values", CreateDefinitionRequest{OrgID: strptr("o"), Key: "k", DataType: DataTypeEnum}, ErrEnumValues},
		{"unknown applies_to_type", CreateDefinitionRequest{OrgID: strptr("o"), Key: "k", DataType: DataTypeText, AppliesToType: "bogus"}, ErrInvalidTarget},
		{"valid", CreateDefinitionRequest{OrgID: strptr("o"), Key: "k", DataType: DataTypeText, AppliesToType: "requirement"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateDefinition(tc.req)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != tc.wantErr {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestCreateDefinitionDefaults verifies label falls back to key and enum values
// are cleared for non-enum types.
func TestCreateDefinitionDefaults(t *testing.T) {
	svc := NewDefaultService(newFakeRepo(), nil)
	def, err := svc.CreateDefinition(CreateDefinitionRequest{
		OrgID: strptr("o"), Key: "owner", DataType: DataTypeText, EnumValues: []string{"x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if def.Label != "owner" {
		t.Errorf("label = %q, want fallback to key", def.Label)
	}
	if len(def.EnumValues) != 0 {
		t.Errorf("enum values on a text field = %v, want empty", def.EnumValues)
	}
}
