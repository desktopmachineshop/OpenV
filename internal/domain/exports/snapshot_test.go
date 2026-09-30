package exports

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
)

// fakeAttributeService answers the definitions in effect for a project.
type fakeAttributeService struct {
	attributes.Service
	effective []*attributes.Definition
	asked     []string
}

func (f *fakeAttributeService) EffectiveForProject(orgID, projectID string) ([]*attributes.Definition, error) {
	f.asked = append(f.asked, projectID)
	return f.effective, nil
}

// snapshotService is project p1 with a requirement carrying a risk enum and
// a figure, and a workspace risk definition in effect.
func snapshotService() (*DefaultService, *fakeAttributeService) {
	svc := newTestService("Cooling System", []*artifacts.Artifact{
		{ID: "a1", Type: artifacts.TypeRequirement, Title: "Stop on overheat", Ref: "REQ-1",
			Attributes: map[string]interface{}{"risk": "high"}},
	}, nil)
	svc.attachmentService = &fakeAttachmentService{byArtifact: map[string][]*attachments.Attachment{
		"a1": {{ID: "f1", ArtifactID: "a1", Filename: "REQ-1-FIG-1.png", OriginalFilename: "pump.png",
			Title: "Pump", MimeType: "image/png", FilePath: "/uploads/f1_pump.png", FileSize: 2048,
			FigureRef: "REQ-1-FIG-1", FigureNum: 1, Version: 1}},
	}}
	defs := &fakeAttributeService{effective: []*attributes.Definition{{ID: "d1", Key: "risk", Label: "Risk",
		DataType: attributes.DataTypeEnum, EnumValues: []string{"low", "high"}, AppliesToType: artifacts.TypeRequirement}}}
	svc.SetAttributeService(defs)
	return svc, defs
}

// A baseline's snapshot is the JSON export with the attribute definitions in
// effect beside it (REQ-5), and with each attachment's metadata (its name,
// type, size and the artifact it belongs to) but never its file: the
// snapshot holds the fields of the attachment record and nothing read from
// disk. The plain JSON export still carries no definitions.
func TestASnapshotKeepsTheDefinitionsAndTheAttachmentMetadata(t *testing.T) {
	svc, _ := snapshotService()

	raw, err := svc.Snapshot("p1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var snap struct {
		Definitions []*attributes.Definition `json:"attribute_definitions"`
		Attachments []map[string]interface{} `json:"attachments"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Definitions) != 1 || snap.Definitions[0].Key != "risk" ||
		strings.Join(snap.Definitions[0].EnumValues, ",") != "low,high" {
		t.Errorf("snapshot definitions = %+v, want the risk enum", snap.Definitions)
	}
	if len(snap.Attachments) != 1 {
		t.Fatalf("snapshot attachments = %v, want the figure's record", snap.Attachments)
	}
	att := snap.Attachments[0]
	if att["artifact_id"] != "a1" || att["original_filename"] != "pump.png" || att["title"] != "Pump" ||
		att["mime_type"] != "image/png" || att["file_size"] != float64(2048) {
		t.Errorf("snapshot attachment = %v, want its name, type, size and artifact", att)
	}
	keys := make([]string, 0, len(att))
	for k := range att {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := "artifact_id,created_at,figure_num,figure_ref,file_path,file_size,filename,id,kind,mime_type,original_filename,title,version"
	if strings.Join(keys, ",") != want {
		t.Errorf("snapshot attachment keys = %s, want the record's metadata alone: %s", strings.Join(keys, ","), want)
	}

	plain, _, err := svc.ExportProject("p1", FormatJSON)
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if strings.Contains(string(plain), "attribute_definitions") {
		t.Error("the plain JSON export carries attribute definitions, which it never has")
	}
}

// Definitions is the one source of a ReqIF document's typing: the export
// prepares its snapshot with them, and so types the risk enum as an
// enumeration, which is what a download of the live project renders with
// too (downloads.DefaultService.Download).
func TestTheReqIFExportIsTypedByDefinitions(t *testing.T) {
	svc, defs := snapshotService()

	got := svc.Definitions("p1")
	if len(got) != 1 || got[0].Key != "risk" || strings.Join(defs.asked, ",") != "p1" {
		t.Fatalf("Definitions(p1) = %v, asked %v; want the risk definition, asked once for p1", got, defs.asked)
	}
	prepared, err := svc.PrepareExport("p1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.AttributeDefs) != 1 || prepared.AttributeDefs[0] != got[0] {
		t.Errorf("PrepareExport's definitions = %v, want Definitions'", prepared.AttributeDefs)
	}

	exported, _, err := svc.ExportProject("p1", FormatReqIF)
	if err != nil {
		t.Fatal(err)
	}
	rendered, _, err := svc.RenderExport(&ProjectExport{ProjectID: "p1", ProjectName: "Cooling System",
		Artifacts: prepared.Artifacts, AttributeDefs: svc.Definitions("p1")}, FormatReqIF)
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string][]byte{"export": exported, "rendered snapshot": rendered} {
		if !strings.Contains(string(doc), "<DATATYPE-DEFINITION-ENUMERATION") ||
			!strings.Contains(string(doc), "<ATTRIBUTE-VALUE-ENUMERATION") {
			t.Errorf("the %s's ReqIF does not type risk as an enumeration", name)
		}
	}

	// Without an attribute service there is nothing to type by: the
	// document degrades to string attributes rather than fail.
	svc.attributeService = nil
	if got := svc.Definitions("p1"); got != nil {
		t.Errorf("Definitions with no attribute service = %v, want nil", got)
	}
}
