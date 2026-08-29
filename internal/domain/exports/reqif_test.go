package exports

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// reqifFixture builds a small but representative project: a requirement with an
// enum attribute and special characters, a child test case, an unrelated
// hazard, plus a verifies link. It exercises the hierarchy, spec types,
// attribute mapping, and XML escaping in one document.
func reqifFixture() *ProjectExport {
	exportedAt := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	orgID := "org1"
	return &ProjectExport{
		ExportedAt:  exportedAt,
		Version:     "1.0",
		ProjectID:   "proj-1",
		ProjectName: `Cooling & "Safety" <System>`,
		Artifacts: []*artifacts.Artifact{
			{
				ID:      "a1",
				Type:    artifacts.TypeRequirement,
				Title:   `Pump shall stop on "overheat" <=90C & hold`,
				Body:    `Body & <b>bold</b> with "quotes" and café`,
				Status:  "approved",
				Version: 3,
				Attributes: map[string]interface{}{
					"priority": "high",
					"status":   "approved", // internal mirror; must be skipped
					"owner":    "Ada",      // discovered free-form attribute
				},
			},
			{
				ID:       "a2",
				Type:     artifacts.TypeTestCase,
				Title:    "Verify overheat cutoff",
				Body:     "",
				ParentID: strPtr("a1"),
				Version:  1,
			},
			{
				ID:      "a3",
				Type:    artifacts.TypeHazard,
				Title:   "Overheating causes burns",
				Version: 1,
			},
		},
		Links: []*links.Link{
			{ID: "l1", FromID: "a2", ToID: "a1", Type: "verifies"},
			{ID: "l2", FromID: "a2", ToID: "ghost", Type: "verifies"}, // dangling: dropped
		},
		AttributeDefs: []*attributes.Definition{
			{
				ID:            "d1",
				OrgID:         &orgID,
				Key:           "priority",
				Label:         "Priority",
				DataType:      attributes.DataTypeEnum,
				EnumValues:    []string{"low", "high"},
				AppliesToType: artifacts.TypeRequirement,
			},
		},
	}
}

func TestBuildReqIFWellFormed(t *testing.T) {
	data, err := buildReqIF(reqifFixture())
	if err != nil {
		t.Fatalf("buildReqIF returned error: %v", err)
	}

	// Well-formedness: stream every token without error.
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("ReqIF is not well-formed XML: %v", err)
		}
	}

	raw := string(data)
	if !strings.Contains(raw, xml.Header) && !strings.HasPrefix(raw, "<?xml") {
		t.Errorf("missing XML declaration")
	}
	if !strings.Contains(raw, reqIFNamespace) {
		t.Errorf("missing ReqIF namespace declaration")
	}
	if !strings.Contains(raw, "<REQ-IF-TOOL-ID>OpenV</REQ-IF-TOOL-ID>") {
		t.Errorf("missing OpenV tool id in header")
	}
}

func TestBuildReqIFStructure(t *testing.T) {
	data, err := buildReqIF(reqifFixture())
	if err != nil {
		t.Fatalf("buildReqIF returned error: %v", err)
	}

	var doc xReqIF
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("ReqIF did not unmarshal back into the document model: %v", err)
	}

	content := doc.CoreContent.ReqIFContent

	// Header carries the project name (escaped in the raw bytes, recovered here).
	if doc.Header.ReqIFHeader.Title != `Cooling & "Safety" <System>` {
		t.Errorf("header TITLE = %q, want the project name round-tripped", doc.Header.ReqIFHeader.Title)
	}
	if doc.Header.ReqIFHeader.CreationTime != "2026-08-26T12:00:00Z" {
		t.Errorf("header CREATION-TIME = %q, want export timestamp", doc.Header.ReqIFHeader.CreationTime)
	}

	// One SPEC-OBJECT per artifact.
	if got := len(content.SpecObjects.SpecObjects); got != 3 {
		t.Fatalf("SPEC-OBJECT count = %d, want 3", got)
	}
	// One SPEC-RELATION per link with both endpoints exported (the dangling one dropped).
	if got := len(content.SpecRelations.SpecRelations); got != 1 {
		t.Fatalf("SPEC-RELATION count = %d, want 1 (dangling link dropped)", got)
	}
	rel := content.SpecRelations.SpecRelations[0]
	if rel.SourceRef != "a2" || rel.TargetRef != "a1" || rel.TypeRef != "SRT-verifies" {
		t.Errorf("relation = %+v, want source a2 -> target a1 typed SRT-verifies", rel)
	}

	// SPEC-OBJECT-TYPE exists for every used type.
	typeIDs := map[string]bool{}
	for _, sot := range content.SpecTypes.SpecObjectTypes {
		typeIDs[sot.Identifier] = true
	}
	for _, want := range []string{"SOT-requirement", "SOT-test-case", "SOT-hazard"} {
		if !typeIDs[want] {
			t.Errorf("missing SPEC-OBJECT-TYPE %q", want)
		}
	}

	// SPEC-RELATION-TYPE per link-type rule includes verifies.
	relTypeIDs := map[string]bool{}
	for _, srt := range content.SpecTypes.SpecRelationTypes {
		relTypeIDs[srt.Identifier] = true
	}
	if !relTypeIDs["SRT-verifies"] {
		t.Errorf("missing SPEC-RELATION-TYPE SRT-verifies")
	}

	// Enum attribute became a ReqIF enumeration datatype with its values.
	var priorityEnum *xDatatypeEnum
	for i := range content.Datatypes.Enums {
		if content.Datatypes.Enums[i].Identifier == "DT-ENUM-priority" {
			priorityEnum = &content.Datatypes.Enums[i]
		}
	}
	if priorityEnum == nil {
		t.Fatalf("missing DATATYPE-DEFINITION-ENUMERATION for priority")
	}
	if len(priorityEnum.SpecifiedValues) != 2 {
		t.Errorf("priority enum has %d values, want 2", len(priorityEnum.SpecifiedValues))
	}

	// The requirement SPEC-OBJECT: title/body round-trip through escaping, and
	// the enum value resolves to the "high" ENUM-VALUE.
	var reqObj *xSpecObject
	for i := range content.SpecObjects.SpecObjects {
		if content.SpecObjects.SpecObjects[i].Identifier == "a1" {
			reqObj = &content.SpecObjects.SpecObjects[i]
		}
	}
	if reqObj == nil {
		t.Fatalf("missing SPEC-OBJECT a1")
	}
	if reqObj.TypeRef != "SOT-requirement" {
		t.Errorf("a1 TYPE ref = %q, want SOT-requirement", reqObj.TypeRef)
	}

	values := map[string]string{} // definition-ref -> THE-VALUE
	for _, v := range reqObj.StringValues {
		values[v.DefRef] = v.TheValue
	}
	if got := values["AD-requirement-std-title"]; got != `Pump shall stop on "overheat" <=90C & hold` {
		t.Errorf("title did not round-trip: %q", got)
	}
	if got := values["AD-requirement-std-text"]; got != `Body & <b>bold</b> with "quotes" and café` {
		t.Errorf("body did not round-trip: %q", got)
	}
	if got := values["AD-requirement-std-status"]; got != "approved" {
		t.Errorf("status = %q, want approved", got)
	}
	if got := values["AD-requirement-std-version"]; got != "3" {
		t.Errorf("version = %q, want 3", got)
	}
	// Discovered free-form attribute exported as a string value.
	if got := values["AD-requirement-attr-owner"]; got != "Ada" {
		t.Errorf("owner attribute = %q, want Ada", got)
	}
	// Internal "status" attribute key must NOT appear as a separate attr value.
	if _, ok := values["AD-requirement-attr-status"]; ok {
		t.Errorf("internal status attribute key leaked into ReqIF attributes")
	}

	if len(reqObj.EnumValues) != 1 {
		t.Fatalf("a1 enum value count = %d, want 1", len(reqObj.EnumValues))
	}
	ev := reqObj.EnumValues[0]
	if ev.DefRef != "AD-requirement-attr-priority" {
		t.Errorf("enum value def ref = %q, want AD-requirement-attr-priority", ev.DefRef)
	}
	// "high" is index 1 in [low, high].
	if len(ev.ValueRefs) != 1 || ev.ValueRefs[0] != "EV-priority-1" {
		t.Errorf("enum value refs = %v, want [EV-priority-1]", ev.ValueRefs)
	}
}

func TestBuildReqIFHierarchy(t *testing.T) {
	data, err := buildReqIF(reqifFixture())
	if err != nil {
		t.Fatalf("buildReqIF returned error: %v", err)
	}
	var doc xReqIF
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	specs := doc.CoreContent.ReqIFContent.Specifications.Specifications
	if len(specs) != 1 {
		t.Fatalf("SPECIFICATION count = %d, want 1", len(specs))
	}
	if specs[0].Children == nil {
		t.Fatalf("SPECIFICATION has no CHILDREN")
	}
	roots := specs[0].Children.Items
	// Roots: a1 (requirement) and a3 (hazard). a2 nests under a1.
	rootRefs := map[string]xSpecHierarchy{}
	for _, r := range roots {
		rootRefs[r.ObjectRef] = r
	}
	if _, ok := rootRefs["a1"]; !ok {
		t.Fatalf("a1 not a root of the outline")
	}
	if _, ok := rootRefs["a3"]; !ok {
		t.Errorf("a3 not a root of the outline")
	}
	if _, ok := rootRefs["a2"]; ok {
		t.Errorf("a2 should be nested under a1, not a root")
	}
	a1 := rootRefs["a1"]
	if a1.Children == nil || len(a1.Children.Items) != 1 || a1.Children.Items[0].ObjectRef != "a2" {
		t.Errorf("a1 children = %+v, want single child a2", a1.Children)
	}
}

func TestExportReqIFThroughService(t *testing.T) {
	svc := newTestService("Cooling System", reqifFixture().Artifacts, reqifFixture().Links)

	data, filename, err := svc.ExportProject("p1", FormatReqIF)
	if err != nil {
		t.Fatalf("ExportProject(reqif) returned error: %v", err)
	}
	if !strings.HasSuffix(filename, ".reqif") {
		t.Errorf("filename %q missing .reqif extension", filename)
	}
	// Even without an attribute service wired, the document must be well-formed
	// and contain the artifacts (enum attribute degrades to nothing gracefully).
	var doc xReqIF
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("service ReqIF did not parse: %v", err)
	}
	if got := len(doc.CoreContent.ReqIFContent.SpecObjects.SpecObjects); got != 3 {
		t.Errorf("SPEC-OBJECT count = %d, want 3", got)
	}
}
