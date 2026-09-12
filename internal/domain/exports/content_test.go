package exports

import (
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
	if DefaultContent().NeedsEvidence() || !DefaultContent().ShowsField("anything") {
		t.Error("the default content shows every field and no evidence")
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
	want := []string{"priority", "status", "verification_method", "owner", "unused", "zeta"}
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
	if fields[3].Label != "Owning team" || !fields[3].Custom || fields[3].Count != 1 {
		t.Errorf("owner = %+v", fields[3])
	}
	if fields[5].Label != "Zeta" || !fields[5].Custom {
		t.Errorf("zeta = %+v", fields[5])
	}
	if Fields(nil) != nil {
		t.Error("nil snapshot has no fields")
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
