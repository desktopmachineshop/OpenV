package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/attributes"
)

// TestAttributeDefinitionRoundTrip exercises the repository CRUD plus the
// org/project scoped listings the effective-set resolver depends on.
func TestAttributeDefinitionRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewAttributeDefinitionRepository(db)
	svc := attributes.NewDefaultService(repo, nil)

	orgID := uuid.New().String()
	projectID := uuid.New().String()

	// Org-wide enum definition.
	orgDef, err := svc.CreateDefinition(attributes.CreateDefinitionRequest{
		OrgID: &orgID, Key: "priority", Label: "Priority",
		DataType: attributes.DataTypeEnum, EnumValues: []string{"low", "high"},
		AppliesToType: "requirement", Required: true, SortOrder: 1,
	})
	if err != nil {
		t.Fatalf("create org def: %v", err)
	}
	// Project override of the same key + type with a wider enum.
	if _, err := svc.CreateDefinition(attributes.CreateDefinitionRequest{
		ProjectID: &projectID, Key: "priority", Label: "Priority (project)",
		DataType: attributes.DataTypeEnum, EnumValues: []string{"low", "high", "urgent"},
		AppliesToType: "requirement", SortOrder: 1,
	}); err != nil {
		t.Fatalf("create project def: %v", err)
	}

	// Read back the org def and verify scope + enum round-trip.
	got, err := repo.Get(orgDef.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OrgID == nil || *got.OrgID != orgID || got.ProjectID != nil {
		t.Fatalf("scope round-trip wrong: org=%v project=%v", got.OrgID, got.ProjectID)
	}
	if len(got.EnumValues) != 2 || got.EnumValues[0] != "low" || !got.Required {
		t.Fatalf("enum/required round-trip wrong: %+v", got)
	}

	// Listings.
	orgList, err := repo.ListByOrg(orgID)
	if err != nil || len(orgList) != 1 {
		t.Fatalf("ListByOrg = %v (err %v), want 1 org-wide def", orgList, err)
	}
	projList, err := repo.ListByProject(projectID)
	if err != nil || len(projList) != 1 {
		t.Fatalf("ListByProject = %v (err %v), want 1 project def", projList, err)
	}

	// Effective set: project override wins for requirement/priority.
	eff, err := svc.EffectiveForProject(orgID, projectID)
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if len(eff) != 1 {
		t.Fatalf("effective size = %d, want 1 (override collapses the pair)", len(eff))
	}
	if eff[0].Label != "Priority (project)" || len(eff[0].EnumValues) != 3 {
		t.Fatalf("override did not win: %+v", eff[0])
	}

	// Update: narrow the enum, then confirm persistence.
	if _, err := svc.UpdateDefinition(orgDef.ID, attributes.UpdateDefinitionRequest{
		Label: "Priority", DataType: attributes.DataTypeEnum,
		EnumValues: []string{"low"}, AppliesToType: "requirement", SortOrder: 2,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = repo.Get(orgDef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EnumValues) != 1 || got.SortOrder != 2 {
		t.Fatalf("update not persisted: %+v", got)
	}

	// Delete removes it; a second delete reports not found.
	if err := svc.DeleteDefinition(orgDef.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.Delete(orgDef.ID); err != attributes.ErrNotFound {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
}
