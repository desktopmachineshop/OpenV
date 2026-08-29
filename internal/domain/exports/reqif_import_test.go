package exports

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// TestReqIFRoundTrip exports the shared fixture to ReqIF, parses it back, and
// imports it into a fresh project through the real import machinery. It asserts
// the artifacts, hierarchy, links, attributes and status survive, that ids are
// remapped and every imported artifact is version 1, and that the XHTML body's
// hard line break is preserved.
func TestReqIFRoundTrip(t *testing.T) {
	reqifBytes, err := buildReqIF(reqifFixture())
	if err != nil {
		t.Fatalf("buildReqIF: %v", err)
	}

	// Parse-level checks first: the exporter-inverse recovers the model.
	parsed, err := parseReqIF(reqifBytes)
	if err != nil {
		t.Fatalf("parseReqIF: %v", err)
	}
	if len(parsed.Artifacts) != 3 {
		t.Fatalf("parsed %d artifacts, want 3", len(parsed.Artifacts))
	}
	if len(parsed.Links) != 1 {
		t.Fatalf("parsed %d links, want 1 (dangling dropped on export)", len(parsed.Links))
	}

	// Now the full import through the shared create path.
	svc, repo, linkSvc, projSvc := newImportFixture()
	projectID, err := svc.ImportProjectReqIF(reqifBytes, "org-reqif")
	if err != nil {
		t.Fatalf("ImportProjectReqIF: %v", err)
	}

	if len(projSvc.created) != 1 || projSvc.created[0].OrgID != "org-reqif" {
		t.Fatalf("project not created for the org: %+v", projSvc.created)
	}
	if len(repo.saved) != 3 {
		t.Fatalf("saved %d artifacts, want 3", len(repo.saved))
	}
	if repo.updates != 0 {
		t.Errorf("repo.Update called %d times, want 0 (import must not bump versions)", repo.updates)
	}
	for _, a := range repo.saved {
		if a.Version != 1 {
			t.Errorf("artifact %q version = %d, want 1", a.Title, a.Version)
		}
		if a.ProjectID != projectID {
			t.Errorf("artifact %q project = %q, want %q", a.Title, a.ProjectID, projectID)
		}
		if a.ID == "a1" || a.ID == "a2" || a.ID == "a3" {
			t.Errorf("artifact %q kept its old ReqIF id %q", a.Title, a.ID)
		}
	}

	req := repo.byTitle(t, `Pump shall stop on "overheat" <=90C & hold`)
	tc := repo.byTitle(t, "Verify overheat cutoff")
	hazard := repo.byTitle(t, "Overheating causes burns")

	// Hierarchy: the test case nests under the requirement; the hazard is a root.
	if req.ParentID != nil {
		t.Errorf("requirement parent = %v, want nil (root)", *req.ParentID)
	}
	if tc.ParentID == nil || *tc.ParentID != req.ID {
		t.Errorf("test case parent = %v, want requirement new id %q", tc.ParentID, req.ID)
	}
	if hazard.ParentID != nil {
		t.Errorf("hazard parent = %v, want nil (root)", *hazard.ParentID)
	}

	// Types survive.
	if req.Type != artifacts.TypeRequirement {
		t.Errorf("requirement type = %q, want %q", req.Type, artifacts.TypeRequirement)
	}
	if tc.Type != artifacts.TypeTestCase {
		t.Errorf("test case type = %q, want %q", tc.Type, artifacts.TypeTestCase)
	}

	// Status survives (approved requirement).
	if req.Status != "approved" {
		t.Errorf("requirement status = %q, want approved", req.Status)
	}

	// XHTML body preserves the hard line break.
	wantBody := "Body & <b>bold</b> with \"quotes\" and café\nSecond line after a hard break"
	if req.Body != wantBody {
		t.Errorf("requirement body did not round-trip: got %q want %q", req.Body, wantBody)
	}
	if !strings.Contains(req.Body, "\n") {
		t.Errorf("requirement body lost its newline: %q", req.Body)
	}

	// Attributes: enum and discovered free-form both restored.
	if got := req.Attributes["priority"]; got != "high" {
		t.Errorf("priority attribute = %v, want high", got)
	}
	if got := req.Attributes["owner"]; got != "Ada" {
		t.Errorf("owner attribute = %v, want Ada", got)
	}

	// Link: verifies, from the test case to the requirement, remapped.
	if len(linkSvc.created) != 1 {
		t.Fatalf("created %d links, want 1", len(linkSvc.created))
	}
	l := linkSvc.created[0]
	if l.Type != "verifies" || l.FromID != tc.ID || l.ToID != req.ID {
		t.Errorf("link = %s ->%s (%s), want %s -> %s (verifies)", l.FromID, l.ToID, l.Type, tc.ID, req.ID)
	}
}

// TestReqIFImportMalformed asserts a non-ReqIF / broken payload is rejected
// (the handler maps this to 400) and never creates anything.
func TestReqIFImportMalformed(t *testing.T) {
	for _, bad := range []string{
		"not xml at all",
		"<html><body>nope</body></html>",
		`<?xml version="1.0"?><REQ-IF><THE-HEADER>`, // truncated
	} {
		if _, err := parseReqIF([]byte(bad)); err == nil {
			t.Errorf("parseReqIF(%q) accepted a malformed document", bad)
		}
	}

	svc, repo, _, projSvc := newImportFixture()
	if _, err := svc.ImportProjectReqIF([]byte("not reqif"), "org-1"); err == nil {
		t.Fatal("ImportProjectReqIF accepted garbage")
	}
	if len(repo.saved) != 0 || len(projSvc.created) != 0 {
		t.Errorf("malformed import still created data: %d artifacts, %d projects", len(repo.saved), len(projSvc.created))
	}
}

// TestReqIFImportEnumValidation asserts an enum attribute whose value is not
// among its datatype's declared values is rejected on import.
func TestReqIFImportEnumValidation(t *testing.T) {
	// priority is declared low|high, but the SPEC-OBJECT references an
	// ENUM-VALUE from a different datatype ("urgent"): out of range.
	doc := `<?xml version="1.0" encoding="UTF-8"?>
<REQ-IF xmlns="http://www.omg.org/spec/ReqIF/20110401/reqif.xsd">
  <THE-HEADER><REQ-IF-HEADER IDENTIFIER="h"><CREATION-TIME>2026-08-26T12:00:00Z</CREATION-TIME><REQ-IF-TOOL-ID>OpenV</REQ-IF-TOOL-ID><REQ-IF-VERSION>1.0</REQ-IF-VERSION><SOURCE-TOOL-ID>OpenV</SOURCE-TOOL-ID><TITLE>Enum Test</TITLE></REQ-IF-HEADER></THE-HEADER>
  <CORE-CONTENT><REQ-IF-CONTENT>
    <DATATYPES>
      <DATATYPE-DEFINITION-ENUMERATION IDENTIFIER="DT-ENUM-priority" LONG-NAME="Priority">
        <SPECIFIED-VALUES>
          <ENUM-VALUE IDENTIFIER="EV-priority-0" LONG-NAME="low"><PROPERTIES><EMBEDDED-VALUE KEY="0" OTHER-CONTENT="low"/></PROPERTIES></ENUM-VALUE>
          <ENUM-VALUE IDENTIFIER="EV-priority-1" LONG-NAME="high"><PROPERTIES><EMBEDDED-VALUE KEY="1" OTHER-CONTENT="high"/></PROPERTIES></ENUM-VALUE>
        </SPECIFIED-VALUES>
      </DATATYPE-DEFINITION-ENUMERATION>
      <DATATYPE-DEFINITION-ENUMERATION IDENTIFIER="DT-ENUM-other" LONG-NAME="Other">
        <SPECIFIED-VALUES>
          <ENUM-VALUE IDENTIFIER="EV-other-0" LONG-NAME="urgent"><PROPERTIES><EMBEDDED-VALUE KEY="0" OTHER-CONTENT="urgent"/></PROPERTIES></ENUM-VALUE>
        </SPECIFIED-VALUES>
      </DATATYPE-DEFINITION-ENUMERATION>
    </DATATYPES>
    <SPEC-TYPES>
      <SPEC-OBJECT-TYPE IDENTIFIER="SOT-requirement" LONG-NAME="Requirement">
        <SPEC-ATTRIBUTES>
          <ATTRIBUTE-DEFINITION-ENUMERATION IDENTIFIER="AD-requirement-attr-priority" LONG-NAME="Priority" MULTI-VALUED="false">
            <TYPE><DATATYPE-DEFINITION-ENUMERATION-REF>DT-ENUM-priority</DATATYPE-DEFINITION-ENUMERATION-REF></TYPE>
          </ATTRIBUTE-DEFINITION-ENUMERATION>
        </SPEC-ATTRIBUTES>
      </SPEC-OBJECT-TYPE>
    </SPEC-TYPES>
    <SPEC-OBJECTS>
      <SPEC-OBJECT IDENTIFIER="a1">
        <VALUES>
          <ATTRIBUTE-VALUE-ENUMERATION>
            <DEFINITION><ATTRIBUTE-DEFINITION-ENUMERATION-REF>AD-requirement-attr-priority</ATTRIBUTE-DEFINITION-ENUMERATION-REF></DEFINITION>
            <VALUES><ENUM-VALUE-REF>EV-other-0</ENUM-VALUE-REF></VALUES>
          </ATTRIBUTE-VALUE-ENUMERATION>
        </VALUES>
        <TYPE><SPEC-OBJECT-TYPE-REF>SOT-requirement</SPEC-OBJECT-TYPE-REF></TYPE>
      </SPEC-OBJECT>
    </SPEC-OBJECTS>
    <SPEC-RELATIONS/>
    <SPECIFICATIONS/>
  </REQ-IF-CONTENT></CORE-CONTENT>
</REQ-IF>`
	if _, err := parseReqIF([]byte(doc)); err == nil {
		t.Fatal("parseReqIF accepted an out-of-range enum value")
	} else if !strings.Contains(err.Error(), "priority") {
		t.Errorf("enum error = %v, want it to name the priority attribute", err)
	}
}
