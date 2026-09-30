package exports

import (
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
)

func TestTemplatesArePresetsAReaderCanStartFrom(t *testing.T) {
	keys := map[string]bool{}
	for _, tmpl := range Templates() {
		if tmpl.Key == "" || tmpl.Name == "" || tmpl.Description == "" {
			t.Errorf("template incomplete: %+v", tmpl)
		}
		keys[tmpl.Key] = true
		if found, ok := TemplateByKey(tmpl.Key); !ok || found.Name != tmpl.Name {
			t.Errorf("TemplateByKey(%q) did not find the template", tmpl.Key)
		}
	}
	for _, want := range []string{"standard", "requirements-review", "test-planning", "vv"} {
		if !keys[want] {
			t.Errorf("missing template %q", want)
		}
	}
	if _, ok := TemplateByKey("nope"); ok {
		t.Error("an unknown key should not resolve")
	}
	vvTmpl, _ := TemplateByKey("vv")
	if !vvTmpl.Content.TestResults || !vvTmpl.Content.VVStatus || !vvTmpl.Content.NeedsEvidence() {
		t.Error("the V&V template must carry evidence")
	}
	review, _ := TemplateByKey("requirements-review")
	if review.Content.ShowsField(FieldVerificationStatus) || !review.Content.ShowsField(FieldPriority) {
		t.Errorf("requirements review fields = %+v", review.Content.Fields)
	}
	// REQ-6: the PDF and the Word document include V&V status by default;
	// the test results stay a choice.
	if d := DefaultContent(); !d.VVStatus || d.TestResults || !d.NeedsEvidence() || !d.ShowsField("anything") {
		t.Errorf("default content = %+v, want every field and the V&V status, and no test results", d)
	}
	standard, _ := TemplateByKey("standard")
	if !reflect.DeepEqual(standard.Content, DefaultContent()) {
		t.Errorf("the Specification preset = %+v, want the default content", standard.Content)
	}
}

func TestFieldsListsWhatTheProjectHolds(t *testing.T) {
	data := &ProjectExport{
		Artifacts: []*artifacts.Artifact{
			{ID: "1", Type: artifacts.TypeRequirement, Attributes: map[string]interface{}{"priority": "must", "status": "draft", "links_snapshot": []interface{}{}, "zeta": "z"}},
			{ID: "2", Type: artifacts.TypeRequirement, Attributes: map[string]interface{}{"priority": "should", "verification_method": "test", "owner": "Ada", "empty": ""}},
			{ID: "3", Type: artifacts.TypeHeading},
		},
		AttributeDefs: []*attributes.Definition{{Key: "owner", Label: "Owning team"}, {Key: "unused", Label: "Unused"}},
	}
	fields := Fields(data)
	got := make([]string, 0, len(fields))
	for _, f := range fields {
		got = append(got, f.Key)
	}
	// owner is a standard key now (REQ-147), so it sits with the others in
	// their fixed order even though the project also defines it. unused is
	// defined but no artifact carries it, so a form does not offer it: a
	// baseline keeps every definition in effect (REQ-5), and a field with
	// nothing in it is no choice.
	want := []string{"priority", "status", "owner", "verification_method", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fields = %v, want %v", got, want)
		}
	}
	if fields[0].Count != 2 || fields[0].Custom || fields[0].Label != "Priority" {
		t.Errorf("priority = %+v", fields[0])
	}
	// A standard key keeps the label the project defined for it.
	if fields[2].Label != "Owning team" || fields[2].Custom || fields[2].Count != 1 {
		t.Errorf("owner = %+v", fields[2])
	}
	if fields[4].Label != "Zeta" || !fields[4].Custom {
		t.Errorf("zeta = %+v", fields[4])
	}
	if Fields(nil) != nil {
		t.Error("nil snapshot has no fields")
	}
}

// A baseline keeps the definitions in effect when it was captured (REQ-5), so
// its fields are the ones its artifacts carry, named as their definitions
// named them, where the live project, whose documents load no definitions,
// words each key itself.
func TestABaselinesFieldsAreNamedByTheDefinitionsItKept(t *testing.T) {
	arts := []*artifacts.Artifact{
		{ID: "1", Type: artifacts.TypeRequirement, Attributes: map[string]interface{}{"asil": "B"}},
	}
	live := Fields(&ProjectExport{Artifacts: arts})
	want := []FieldOption{{Key: "asil", Label: "Asil", Count: 1, Custom: true}}
	if !reflect.DeepEqual(live, want) {
		t.Errorf("live fields = %+v, want %+v", live, want)
	}
	baseline := Fields(&ProjectExport{Artifacts: arts, AttributeDefs: []*attributes.Definition{
		{Key: "asil", Label: "ASIL level", DataType: attributes.DataTypeEnum, EnumValues: []string{"A", "B"}},
		{Key: "supplier_margin", Label: "Supplier margin", DataType: attributes.DataTypeNumber},
	}})
	want = []FieldOption{{Key: "asil", Label: "ASIL level", Count: 1, Custom: true}}
	if !reflect.DeepEqual(baseline, want) {
		t.Errorf("baseline fields = %+v, want %+v: a definition no artifact carries is no field", baseline, want)
	}
}

func TestFieldLabelWordsAKey(t *testing.T) {
	cases := map[string]string{"verification_method": "Verification method", "priority": "Priority", "risk-level": "Risk level", "": ""}
	for in, want := range cases {
		if got := FieldLabel(in); got != want {
			t.Errorf("FieldLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
