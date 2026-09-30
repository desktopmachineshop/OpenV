//go:build unix

package main

import "testing"

// TestTourS5aExportsImports is the S5a tour's exports and imports area
// (refactor plan §6.4 S5a; invariants I4, I8 and I15, quirk Q14; OpenV
// REQ-6): the project export in each of its formats, the download surface
// (its options and its JSON, CSV, Excel and ReqIF renderers, narrowed by
// every filter the query takes, from a baseline, and bundled with the
// figures in a zip), and the import of JSON and ReqIF, each imported
// project read back to show what the import carried. Its golden is
// testdata/tour/s5a/exports_imports.json.
//
// The owner, an ordinary account, works in its personal workspace, which
// defines an enum attribute (risk: low|high) for requirements. Project P
// has one heading, the only parent, over a user need, a requirement and a
// test case, beside a root design item: artifact lists order by parent_id
// first, a random UUID, so a project the golden lists has one parent. Both
// ReqIF paths, the export and the download, type risk as an enumeration by
// the definitions in effect, and the baseline keeps them in its snapshot
// (fixed under R7, REQ-6 and REQ-5: the download typed risk as a string, and
// a snapshot kept no definitions). The
// requirement carries a multi-line body, priority, owner and risk, and a
// PNG figure; the links are derives-from, verifies and satisfies, and a
// requirement of a second project refines P's, so P's export names it
// among linked_artifacts. One baseline is taken before that refines link.
// An empty project pins the null lists (Q14). No route of this area
// publishes an event: the imports create their rows through the services,
// not the handlers that publish.
func TestTourS5aExportsImports(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "exports_imports",
		about: "Project export (json, csv, excel, reqif), the download options and the json, csv, excel and reqif " +
			"downloads with their filters, baselines and figure bundles, and the JSON and ReqIF imports, each " +
			"imported project read back.",
		run: exportsImportsTour,
	})
}

// exportsImportsReqIF is the one-SPEC-OBJECT ReqIF document of
// internal/domain/exports/reqif_import_test.go (TestReqIFImportEnumValidation),
// with the enum value ref it takes: EV-priority-1 is in range, EV-other-0
// is not.
func exportsImportsReqIF(enumValue string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
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
            <VALUES><ENUM-VALUE-REF>` + enumValue + `</ENUM-VALUE-REF></VALUES>
          </ATTRIBUTE-VALUE-ENUMERATION>
        </VALUES>
        <TYPE><SPEC-OBJECT-TYPE-REF>SOT-requirement</SPEC-OBJECT-TYPE-REF></TYPE>
      </SPEC-OBJECT>
    </SPEC-OBJECTS>
    <SPEC-RELATIONS/>
    <SPECIFICATIONS/>
  </REQ-IF-CONTENT></CORE-CONTENT>
</REQ-IF>`
}

// exportsImportsMinimalJSON is the least JSON import that shows what the
// importer does: a ref kept, a parent remapped, a link kept and a link to
// an artifact outside the payload dropped.
const exportsImportsMinimalJSON = `{"project_name":"Imported","artifacts":[` +
	`{"id":"a","type":"requirement","title":"R","body":"The system shall work.","ref":"REQ-7"},` +
	`{"id":"b","type":"test-case","title":"T","parent_id":"a"}],` +
	`"links":[{"from_id":"b","to_id":"a","type":"verifies"},{"from_id":"b","to_id":"zzz","type":"verifies"}]}`

func exportsImportsTour(tr *tour) {
	o := tr.owner
	tr.keep("2026-08-26T12:00:00Z", "the CREATION-TIME of the ReqIF document the tour imports")
	tr.keep("2006-09-16T00:00:00Z", "excelize's fixed docProps/core.xml date: a workbook records no export time there")
	tr.wholeSeconds("GET /api/v1/projects/{id}/export", "the JSON export's times")
	tr.wholeSeconds("GET /api/v1/projects/{id}/download/csv", "the CSV's created and updated columns")
	tr.wholeSeconds("GET /api/v1/projects/{id}/download/excel", "the workbook's created and updated cells")
	tr.wholeSeconds("GET /api/v1/projects/{id}/download/reqif", "CREATION-TIME and each LAST-CHANGE")
	// An imported project is read back as its JSON download.
	imported := func(title, name string, opts ...tourOpt) {
		tr.step(title, o, "GET /api/v1/projects/{id}/download/json", append([]tourOpt{at("id", "{{"+name+"}}"),
			unordered("/links", "an import creates its links in one loop, so their created_at can tie, and link "+
				"lists order by created_at alone")}, opts...)...)
	}

	// The workspace's enum attribute: the ReqIF export and the ReqIF download
	// type it as an enumeration (I15).
	tr.setup("an org-wide enum attribute for requirements", o, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","key":"risk","label":"Risk","data_type":"enum",`+
			`"enum_values":["low","high"],"applies_to_type":"requirement"}`))

	// An empty project: the export's lists are null (Q14), a download's are
	// [] (the selection copies them).
	tr.setup("an empty project", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour empty"}`)).capture("empty", "/id")
	tr.step("export an empty project: artifacts, links and attachments are null (Q14)", o,
		"GET /api/v1/projects/{id}/export", at("id", "{{empty}}"))
	tr.step("the download options of an empty project: every list []", o,
		"GET /api/v1/projects/{id}/download/options", at("id", "{{empty}}"))
	tr.step("download an empty project as JSON: the selection makes the lists []", o,
		"GET /api/v1/projects/{id}/download/json", at("id", "{{empty}}"))
	tr.step("download an empty project as CSV: the header alone", o,
		"GET /api/v1/projects/{id}/download/csv", at("id", "{{empty}}"))

	// Project P.
	tr.setup("project P, a name the filename sanitiser trims", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour \"export\" <a/b> & c","description":"Pinned by the export tour"}`)).capture("p", "/id")
	tr.setup("the heading, P's only parent", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"type":"heading","title":"Scope","body":"What the tour covers."}`)).capture("heading", "/id")
	tr.setup("a user need under it", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{heading}}","type":"user-need","title":"Operators need speed","body":"Operators wait for answers."}`)).
		capture("need", "/id")
	tr.setup("a requirement under it, multi-line, with priority, owner and risk", o, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{p}}","parent_id":"{{heading}}","type":"requirement","title":"Answer in time",`+
			`"body":"The system shall answer within 2 s.\nThe system shall log each answer.",`+
			`"attributes":{"priority":"must","owner":"alice","risk":"high"}}`)).capture("req", "/id")
	tr.setup("a test case under it", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{heading}}","type":"test-case","title":"Time the answer","body":"Measure the answer time."}`)).
		capture("test", "/id")
	tr.setup("a root design item", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"type":"design-item","title":"Fast path","body":"A cache in front of the store."}`)).capture("design", "/id")
	tr.setup("the requirement derives from the need", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{req}}","to_id":"{{need}}","type":"derives-from"}`)).capture("derives", "/id")
	tr.setup("the test case verifies the requirement", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{test}}","to_id":"{{req}}","type":"verifies"}`)).capture("verifies", "/id")
	tr.setup("the design item satisfies the requirement", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{design}}","to_id":"{{req}}","type":"satisfies"}`)).capture("satisfies", "/id")
	ct, form := multipartForm([][2]string{{"artifact_id", tr.id("req")}},
		tourFormFile{"file", "fig.png", "image/png", []byte(tourPNG)})
	tr.setup("a PNG figure on the requirement", o, "POST /api/v1/attachments/upload", rawBody(ct, form)).
		capture("figure", "/id")
	tr.setup("a baseline of P", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour baseline"}`)).capture("baseline", "/id")
	tr.setup("a supplier project", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour supplier"}`)).capture("supplier", "/id")
	tr.setup("filed under P", o, "PUT /api/v1/projects/{id}", at("id", "{{supplier}}"),
		jsonBody(`{"parent_project_id":"{{p}}"}`))
	tr.setup("a supplier requirement", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{supplier}}",`+
		`"type":"requirement","title":"Cache hits","body":"The cache shall answer within 1 s."}`)).capture("supplier_req", "/id")
	tr.setup("it refines P's requirement, after the baseline", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{supplier_req}}","to_id":"{{req}}","type":"refines"}`)).capture("refines", "/id")

	// The export.
	tr.step("export P with no format: JSON, indented, no trailing newline", o, "GET /api/v1/projects/{id}/export",
		at("id", "{{p}}"))
	tr.step("export P as CSV", o, "GET /api/v1/projects/{id}/export", at("id", "{{p}}"), query("format=csv"))
	tr.step("export P as Excel", o, "GET /api/v1/projects/{id}/export", at("id", "{{p}}"), query("format=excel"))
	reqif := tr.step("export P as ReqIF: the export loads attribute definitions, so risk is an enumeration", o,
		"GET /api/v1/projects/{id}/export", at("id", "{{p}}"), query("format=reqif"),
		note("risk is a DATATYPE-DEFINITION-ENUMERATION and, on the requirement type, an "+
			"ATTRIBUTE-DEFINITION-ENUMERATION: 8 lines name ENUMERATION here, as in the ReqIF download below (I15)"))
	tr.step("export with a format in capitals: the format is case-sensitive", o, "GET /api/v1/projects/{id}/export",
		at("id", "{{p}}"), query("format=JSON"))
	tr.step("export as PDF, which only the download and the report render", o, "GET /api/v1/projects/{id}/export",
		at("id", "{{p}}"), query("format=pdf"))
	tr.step("export a project that does not exist", o, "GET /api/v1/projects/{id}/export", at("id", "{{phantom}}"),
		note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("export with no session", tr.anon, "GET /api/v1/projects/{id}/export", at("id", "{{p}}"))

	// The download options.
	tr.step("P's download options: sections, types, figures, fields, owners, templates and defaults", o,
		"GET /api/v1/projects/{id}/download/options", at("id", "{{p}}"),
		note("the defaults and the Specification preset carry V&V status (fixed under R7, REQ-6: off)"))
	tr.step("the options of P's baseline", o, "GET /api/v1/projects/{id}/download/options", at("id", "{{p}}"),
		query("baseline_id={{baseline}}"))
	tr.step("the options of a baseline that does not exist", o, "GET /api/v1/projects/{id}/download/options",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))

	// The downloads: live, from the baseline, narrowed, bundled.
	live := tr.step("download P as JSON: the export's document, through the selection", o,
		"GET /api/v1/projects/{id}/download/json", at("id", "{{p}}"))
	tr.step("download P's baseline as JSON: before the refines link, with the definitions it kept", o,
		"GET /api/v1/projects/{id}/download/json", at("id", "{{p}}"), query("baseline_id={{baseline}}"),
		note("a baseline's snapshot keeps the attribute definitions in effect when it was captured, which the live "+
			"JSON leaves out (fixed under R7, REQ-5: a snapshot kept none)"))
	tr.step("download from a baseline that does not exist: 404", o, "GET /api/v1/projects/{id}/download/json",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"),
		note("as the report route answers the same baseline (the next step); fixed under R7, the download mapped "+
			"every load error to 500"))
	tr.step("the report of a baseline that does not exist: 404", o, "GET /api/v1/projects/{id}/report",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("download requirements and test cases, no headings: a link survives only with both ends", o,
		"GET /api/v1/projects/{id}/download/json", at("id", "{{p}}"), query("types=requirement,test-case&headings=0"))
	tr.step("download the heading's section without headings", o, "GET /api/v1/projects/{id}/download/json",
		at("id", "{{p}}"), query("sections={{heading}}&headings=false"))
	tr.step("download alice's share: the owner filter keeps headings", o, "GET /api/v1/projects/{id}/download/json",
		at("id", "{{p}}"), query("owners=alice"))
	tr.step("download P as CSV", o, "GET /api/v1/projects/{id}/download/csv", at("id", "{{p}}"))
	tr.step("download P as CSV with baseline_id=live: the live project", o, "GET /api/v1/projects/{id}/download/csv",
		at("id", "{{p}}"), query("baseline_id=live"))
	tr.step("download CSV from the requirements-review template: its types, and the rest ignored", o,
		"GET /api/v1/projects/{id}/download/csv", at("id", "{{p}}"), query("template=requirements-review"))
	tr.step("the document switches and an unknown template leave a data format as it was", o,
		"GET /api/v1/projects/{id}/download/csv", at("id", "{{p}}"),
		query("template=bogus&fields=none&traceability=0&figures=0&toc=0&results=1&vv=1"))
	tr.step("download P's baseline as Excel: the cover names the baseline", o,
		"GET /api/v1/projects/{id}/download/excel", at("id", "{{p}}"), query("baseline_id={{baseline}}"))
	tr.step("download P as ReqIF: typed by the definitions as the export is, so risk is an enumeration (I15)", o,
		"GET /api/v1/projects/{id}/download/reqif", at("id", "{{p}}"),
		note("the download takes the definitions in effect from the function the export takes them from (fixed "+
			"under R7, REQ-6: it loaded none, so risk was a discovered ATTRIBUTE-DEFINITION-STRING like owner and "+
			"priority)"))
	tr.step("download P as JSON with its figures: a zip of the document and the figure", o,
		"GET /api/v1/projects/{id}/download/json", at("id", "{{p}}"), query("attachments=figures"))
	tr.step("download with a category P does not hold: no archive", o, "GET /api/v1/projects/{id}/download/csv",
		at("id", "{{p}}"), query("attachments=documents"))
	tr.step("download a project that does not exist", o, "GET /api/v1/projects/{id}/download/json",
		at("id", "{{phantom}}"))

	// The JSON import: the body is the file's text, not a form.
	tr.step("import the least JSON that shows the importer's rules", o, "POST /api/v1/projects/import",
		jsonBody(exportsImportsMinimalJSON)).capture("imported_minimal", "/project_id")
	imported("read it back: the ref kept, the dangling link dropped, links_snapshot from creation, version 1",
		"imported_minimal")
	tr.step("import an empty object: a project with no name", o, "POST /api/v1/projects/import", jsonBody(`{}`)).
		capture("imported_empty", "/project_id")
	tr.step("export it: the filename falls back to export", o, "GET /api/v1/projects/{id}/export",
		at("id", "{{imported_empty}}"))
	tr.step("import malformed JSON", o, "POST /api/v1/projects/import", jsonBody(`{"project_name":`))
	tr.step("import P's JSON download back: a round trip", o, "POST /api/v1/projects/import",
		answerOf(live, "application/json"), note("the refines link comes from the supplier project, outside the "+
			"payload, so the import drops it; attachments are metadata the import does not restore")).
		capture("imported_json", "/project_id")
	imported("read the round trip back: what a JSON import carries", "imported_json",
		note("refs, parents, sort order, bodies and attributes carry over and every artifact is at version 1; "+
			"links_snapshot entries carry the import's clock as valid_from, where the link rows read back the zero time"))
	tr.step("import with no session", tr.anon, "POST /api/v1/projects/import", jsonBody(`{}`))

	// The ReqIF import: the frontend posts the file's text as
	// application/json, so the server sniffs it.
	tr.step("import P's ReqIF export back, posted as application/json: sniffed as ReqIF", o,
		"POST /api/v1/projects/import", answerOf(reqif, "application/json")).capture("imported_reqif", "/project_id")
	imported("read the ReqIF round trip back: what a ReqIF import carries", "imported_reqif")
	tr.step("import the one-object ReqIF document behind a byte-order mark", o, "POST /api/v1/projects/import",
		rawBody("application/json", []byte("\xef\xbb\xbf"+exportsImportsReqIF("EV-priority-1"))),
		note("the body starts with the UTF-8 byte-order mark (EF BB BF, U+FEFF, which the golden writes as its JSON "+
			"escape), then an XML declaration: the sniffer skips the mark and finds <REQ-IF")).
		capture("imported_bom", "/project_id")
	imported("read it back: the enum value's text", "imported_bom")
	tr.step("format=ReqIF in any case makes a JSON body ReqIF", o, "POST /api/v1/projects/import",
		query("format=ReqIF"), jsonBody(`{}`))
	tr.step("a Content-Type naming reqif makes a JSON body ReqIF", o, "POST /api/v1/projects/import",
		rawBody("application/reqif+xml", []byte(`{}`)))
	tr.step("import a truncated ReqIF document", o, "POST /api/v1/projects/import",
		rawBody("application/json", []byte(`<REQ-IF xmlns="http://www.omg.org/spec/ReqIF/20110401/reqif.xsd"><THE-HEADER>`)),
		note("the message passes encoding/xml's error text through (Q19)"))
	tr.step("import a ReqIF document whose enum value is outside its datatype", o, "POST /api/v1/projects/import",
		rawBody("application/json", []byte(exportsImportsReqIF("EV-other-0"))))
}
