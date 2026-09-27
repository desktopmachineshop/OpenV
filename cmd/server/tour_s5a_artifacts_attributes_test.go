//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestTourS5aArtifactsAttributes is the S5a tour's artifacts and typed
// attributes area (refactor plan §6.4 S5a; quirks Q1, Q2's deterministic
// neighbours, Q8 and Q14; OpenV REQ-4, REQ-143). Its golden is
// testdata/tour/s5a/artifacts_attributes.json.
//
// The owner, an ordinary account, first works in its personal workspace:
// the two static catalogues (Q1: one bare encode small enough to be sniffed
// as text/plain, one large enough to be gzipped with no Content-Type), then
// one project holding one heading whose children are one artifact of every
// catalogue type, each with its type's ref prefix; a copy from a second
// project (the copied_from note); the list with its filters, pages and the
// Q8 limit parser (over a third project of 201 imported artifacts); updates (attributes carried forward, replaced by {}, a
// retype that re-mints the ref, a status the mirror refuses, a move to the
// root); the versions; a restore, which re-mints the ref too and publishes
// no event; and deletes. Then attribute definitions: the scope, key and type
// refusals, the raw and effective lists, a required enum enforced on create
// only when an attributes map is sent, and updates and deletes.
//
// Last, the gates, in a shared workspace the owner creates (a personal one
// takes no members): an editor and a viewer of its project, both plain
// members of the workspace, show that a workspace-wide definition takes a
// workspace admin, a project one a project editor, and an artifact write an
// editor. The owner then acts in that workspace, and reads its events.
//
// Every artifact list the golden holds has at most one parent with
// children, since the list orders by parent_id first and a parent id is a
// random UUID.
func TestTourS5aArtifactsAttributes(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "artifacts_attributes",
		about: "Artifacts (create with refs per type and copied_from, read, list with filters and paging, update, " +
			"versions, restore, delete), the artifact and link type catalogues, and typed attribute definitions " +
			"(org-wide and project-scoped, their gates, the effective set and its enforcement on artifacts).",
		run: artifactsAttributesTour,
	})
}

func artifactsAttributesTour(tr *tour) {
	artifactsAttributesArtifacts(tr)
	artifactsAttributesDefinitions(tr)
	artifactsAttributesGates(tr)
}

// artifactsAttributesArtifacts is the artifact half, in the owner's
// personal workspace.
func artifactsAttributesArtifacts(tr *tour) {
	owner := tr.owner

	// The projects: the tour's, and a source for a copy, whose requirement
	// is at version 2. The workspace is the project's org_id.
	tr.setup("the tour project", owner, "POST /api/v1/projects", jsonBody(`{"name":"Tour artifacts"}`)).
		capture("project", "/id")
	tr.setup("the copy's source project", owner, "POST /api/v1/projects", jsonBody(`{"name":"Tour source"}`)).
		capture("source_project", "/id")
	if org := tr.setup("the tour project read back for its workspace", owner, "GET /api/v1/projects/{id}",
		at("id", "{{project}}")).value("/org_id"); org != tr.id("owner.workspace") {
		tr.t.Fatalf("the tour project's org_id %s is not the owner's workspace %s", org, tr.id("owner.workspace"))
	}
	tr.setup("the source requirement", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{source_project}}",`+
		`"type":"requirement","title":"Source rule","body":"The system shall be copied."}`)).capture("source_req", "/id")
	tr.setup("the source requirement's second version", owner, "PUT /api/v1/artifacts/{id}", at("id", "{{source_req}}"),
		jsonBody(`{"body":"The system shall be copied, version 2."}`))

	// The catalogues: static, and encoded with no Content-Type (Q1).
	tr.step("the artifact type catalogue: 1,060 bytes and no Content-Type, so sniffed as text/plain", owner,
		"GET /api/v1/meta/artifact-types",
		note("Q1: the handler sets no Content-Type; below the compressor's 1,400-byte floor the server sniffs "+
			"text/plain, whether gzip is accepted or not"))
	tr.step("the link type rules: 1,902 bytes, so gzipped, and then with no Content-Type at all", owner,
		"GET /api/v1/meta/link-types",
		note("Q1: plain, the server sniffs text/plain; gzipped, the compressor sets Content-Encoding and nothing "+
			"sniffs, so the answer carries no Content-Type"))
	tr.step("the artifact type catalogue with no session", tr.anon, "GET /api/v1/meta/artifact-types",
		note("the handler has no guard of its own; the auth middleware answers first"))

	// The empty project (Q14).
	tr.step("the tour project's artifacts before any exists: [] and X-Total-Count 0", owner, "GET /api/v1/artifacts",
		query("project_id={{project}}"))
	tr.step("list artifacts with no project_id", owner, "GET /api/v1/artifacts")
	tr.step("list the artifacts of a project that does not exist", owner, "GET /api/v1/artifacts",
		query("project_id={{phantom}}"))

	// Create: a heading, then one child of every catalogue type.
	tr.step("create an artifact with a malformed body", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":`))
	tr.step("create an artifact with no project", owner, "POST /api/v1/artifacts",
		jsonBody(`{"type":"requirement","title":"Nowhere"}`))
	tr.step("create an artifact in a project that does not exist", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{phantom}}","type":"requirement","title":"Nowhere"}`))
	tr.step("the heading: HDG-1, a draft whose status the attributes mirror", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","type":"heading","title":"Scope","body":"What the tour covers."}`)).
		capture("heading", "/id")
	children := []struct{ name, typ, title, extra string }{
		{"description", "description", "Context", `,"body":"Where the <system> runs & why."`},
		{"persona", "persona", "Operator", `,"body":"Runs the machine."`},
		{"need", "user-need", "Quick answers", `,"body":"The operator needs answers fast."`},
		{"requirement", "requirement", "Answer in time", `,"body":"The system shall answer within 2 s.",` +
			`"attributes":{"owner":"alice","priority":"must"}`},
		{"design", "design-item", "Answer cache", `,"body":"Cache the answers."`},
		{"test_case", "test-case", "Time the answer", `,"body":"Measure the answer time.","attributes":{"owner":"alice"}`},
		{"hazard", "hazard", "Stale answer", `,"body":"A cached answer is out of date.","attributes":{"status":"in_review"}`},
		{"other", "other", "Loose note", `,"body":"Anything else.","sort_order":40`},
		{"subheading", "heading", "Details", ``},
	}
	for _, c := range children {
		title := "a " + c.typ + " under the heading"
		switch c.name {
		case "hazard":
			title += ": attributes.status seeds the status column"
		case "other":
			title += ": other takes the ART prefix; an explicit sort_order"
		case "subheading":
			title = "a second heading, under the first: HDG-2"
		}
		tr.step(title, owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}",`+
			`"type":"`+c.typ+`","title":"`+c.title+`"`+c.extra+`}`)).capture(c.name, "/id")
	}

	// A copy from the other project, and one whose source does not exist.
	tr.step("a copy of the source project's requirement: REQ-2 here, with copied_from", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Copied rule",`+
			`"body":"The system shall be copied.","copied_from":"{{source_req}}"}`)).capture("copy", "/id")
	tr.step("the copy's notes: where it came from, the source's ref and version", owner, "GET /api/v1/chatter",
		query("artifact_id={{copy}}"))
	tr.step("a copy whose source does not exist: created, with no note", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Orphan copy",`+
			`"body":"No source: the artifact it names does not exist, so the copy gets no note.",`+
			`"copied_from":"{{phantom}}"}`)).capture("orphan", "/id")
	tr.step("the orphan copy's notes: none", owner, "GET /api/v1/chatter", query("artifact_id={{orphan}}"))

	// Read.
	tr.step("read the requirement", owner, "GET /api/v1/artifacts/{id}", at("id", "{{requirement}}"))
	tr.step("read an artifact that does not exist", owner, "GET /api/v1/artifacts/{id}", at("id", "{{phantom}}"))

	// List: tree order, filters, pages and the limit parser (Q8).
	tr.step("the project's artifacts: the root, then the heading's children by sort_order", owner,
		"GET /api/v1/artifacts", query("project_id={{project}}"))
	tr.step("the headings with document numbers", owner, "GET /api/v1/artifacts",
		query("project_id={{project}}&doc_numbers=1&type=heading"))
	tr.step("the requirements", owner, "GET /api/v1/artifacts", query("project_id={{project}}&type=requirement"))
	tr.step("the artifacts whose owner attribute is alice", owner, "GET /api/v1/artifacts",
		query("project_id={{project}}&owner=alice"))
	tr.step("a type no artifact has", owner, "GET /api/v1/artifacts", query("project_id={{project}}&type=widget"))
	tr.step("a page of two after the first: X-Total-Count is every match", owner, "GET /api/v1/artifacts",
		query("project_id={{project}}&limit=2&offset=1"))
	tr.step("a page past the end", owner, "GET /api/v1/artifacts", query("project_id={{project}}&limit=2&offset=50"))
	// 201 root artifacts, one more than the largest default or cap of the
	// other six limit parsers (shared products: 200 and 500), so a limit that
	// fell back to any of their policies would answer fewer. The exact 1,000
	// is not pinned: that would take 1,001 artifacts.
	bulk := make([]string, 201)
	for i := range bulk {
		bulk[i] = fmt.Sprintf(`{"id":"b%03d","type":"other","title":"Bulk %03d"}`, i+1, i+1)
	}
	tr.setup("a project of 201 root artifacts, in import order", owner, "POST /api/v1/projects/import",
		jsonBody(`{"project_name":"Tour bulk","artifacts":[`+strings.Join(bulk, ",")+`]}`)).capture("bulk", "/project_id")
	tr.step("limit 0 and a negative offset over 201 artifacts: all 201, from the first", owner, "GET /api/v1/artifacts",
		query("project_id={{bulk}}&limit=0&offset=-3"))
	tr.step("a limit over 1000 and an offset that is not a number over 201 artifacts: all 201, from the first", owner,
		"GET /api/v1/artifacts", query("project_id={{bulk}}&limit=1001&offset=x"))
	tr.step("the list with no session", tr.anon, "GET /api/v1/artifacts", query("project_id={{project}}"))

	// Update.
	tr.step("update with a malformed body", owner, "PUT /api/v1/artifacts/{id}", at("id", "{{requirement}}"), jsonBody(`{`))
	tr.step("update an artifact that does not exist", owner, "PUT /api/v1/artifacts/{id}", at("id", "{{phantom}}"),
		jsonBody(`{"title":"Nothing"}`), note("Q2's neighbour: the handler loads the artifact before its guard and "+
			"answers any failure with 500, where GET answers 404"))
	tr.step("retitle the requirement: omitted attributes carry forward, version 2", owner, "PUT /api/v1/artifacts/{id}",
		at("id", "{{requirement}}"), jsonBody(`{"title":"Answer in good time"}`))
	tr.step("replace its attributes with {}: only the status mirror is left, version 3", owner,
		"PUT /api/v1/artifacts/{id}", at("id", "{{requirement}}"), jsonBody(`{"attributes":{}}`))
	tr.step("an attributes.status the mirror overwrites: an update cannot approve, version 4", owner,
		"PUT /api/v1/artifacts/{id}", at("id", "{{requirement}}"),
		jsonBody(`{"body":"The system shall answer within 1 s.","attributes":{"status":"approved"}}`))
	tr.step("retype the other artifact as a hazard: its ref is minted again, HAZ-2", owner, "PUT /api/v1/artifacts/{id}",
		at("id", "{{other}}"), jsonBody(`{"type":"hazard"}`))
	tr.step("move the sub-heading to the root: parent_id null", owner, "PUT /api/v1/artifacts/{id}",
		at("id", "{{subheading}}"), jsonBody(`{"parent_id":null}`))

	// Versions and restore.
	tr.step("the requirement's versions, newest first, each older one closed by valid_to", owner,
		"GET /api/v1/artifacts/{id}/versions", at("id", "{{requirement}}"))
	tr.step("the versions of an artifact that does not exist", owner, "GET /api/v1/artifacts/{id}/versions",
		at("id", "{{phantom}}"), note("the guard looks the project up through the artifact and finds none"))
	tr.step("restore with a malformed body", owner, "POST /api/v1/artifacts/{id}/restore", at("id", "{{requirement}}"),
		jsonBody(`{"version":"one"}`))
	tr.step("restore a version the artifact never had", owner, "POST /api/v1/artifacts/{id}/restore",
		at("id", "{{requirement}}"), jsonBody(`{"version":99}`))
	tr.step("restore an artifact that does not exist", owner, "POST /api/v1/artifacts/{id}/restore",
		at("id", "{{phantom}}"), jsonBody(`{"version":1}`),
		note("Q2's neighbour: the handler loads the artifact before its guard and answers any failure with 500"))
	tr.step("restore version 1: version 5, a new ref, and no event", owner, "POST /api/v1/artifacts/{id}/restore",
		at("id", "{{requirement}}"), jsonBody(`{"version":1}`),
		note("the restored version is built without the current ref, so the repository mints the next one; "+
			"the handler publishes no event"))
	tr.step("the requirement's notes, newest first: the restore, then each update's change summary", owner,
		"GET /api/v1/chatter", query("artifact_id={{requirement}}"))
	tr.step("the requirement's versions after the restore", owner, "GET /api/v1/artifacts/{id}/versions",
		at("id", "{{requirement}}"))

	// Delete.
	tr.step("delete an artifact that does not exist", owner, "DELETE /api/v1/artifacts/{id}", at("id", "{{phantom}}"),
		note("the guard looks the project up through the artifact: 404 project not found"))
	tr.step("delete the persona", owner, "DELETE /api/v1/artifacts/{id}", at("id", "{{persona}}"))
	tr.step("read the deleted persona", owner, "GET /api/v1/artifacts/{id}", at("id", "{{persona}}"))
	tr.step("the deleted persona's versions", owner, "GET /api/v1/artifacts/{id}/versions", at("id", "{{persona}}"))
	tr.step("delete it again", owner, "DELETE /api/v1/artifacts/{id}", at("id", "{{persona}}"))
	tr.step("the project's artifacts after the updates, the restore and the delete", owner, "GET /api/v1/artifacts",
		query("project_id={{project}}"))
}

// artifactsAttributesDefinitions is the attribute definitions half, in the
// owner's personal workspace, where the owner is the workspace's admin.
func artifactsAttributesDefinitions(tr *tour) {
	owner := tr.owner

	// The empty lists (Q14) and the refusals.
	tr.step("the project's definitions before any exists", owner, "GET /api/v1/attribute-definitions",
		query("project_id={{project}}"))
	tr.step("the workspace's definitions before any exists", owner, "GET /api/v1/attribute-definitions",
		query("org_id={{owner.workspace}}"))
	tr.step("the effective definitions before any exists", owner, "GET /api/v1/meta/attribute-definitions",
		query("project_id={{project}}"))
	tr.step("list definitions with neither project_id nor org_id", owner, "GET /api/v1/attribute-definitions")
	tr.step("list the definitions of a workspace the owner is not in", owner, "GET /api/v1/attribute-definitions",
		query("org_id={{admin.workspace}}"))
	tr.step("define with a malformed body", owner, "POST /api/v1/attribute-definitions", jsonBody(`{"key":`))
	tr.step("define with neither org_id nor project_id", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"key":"risk","data_type":"text"}`))
	tr.step("define with both org_id and project_id", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","project_id":"{{project}}","key":"risk","data_type":"text"}`),
		note("the handler's gate takes the project; the service then refuses both, in words of its own"))
	tr.step("define with a blank key", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","key":"  ","data_type":"text"}`))
	tr.step("define with a key that is not lower case letters, digits and underscores", owner,
		"POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","key":"Risk Level","data_type":"text"}`))
	tr.step("define with an unknown data type", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","key":"risk","data_type":"money"}`))
	tr.step("define an enum with no values but blanks", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","key":"risk","data_type":"enum","enum_values":[" ",""]}`))
	tr.step("define for an artifact type that does not exist", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{owner.workspace}}","key":"risk","data_type":"text","applies_to_type":"widget"}`))
	tr.step("define in a workspace the owner is not in", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{admin.workspace}}","key":"risk","data_type":"text"}`))

	// Definitions made.
	tr.step("a required workspace-wide enum for requirements: label and values trimmed, values deduplicated", owner,
		"POST /api/v1/attribute-definitions", jsonBody(`{"org_id":"{{owner.workspace}}","key":"risk","label":" Risk ",`+
			`"data_type":"enum","enum_values":["low"," high","low",""],"applies_to_type":"requirement","required":true,`+
			`"sort_order":1}`)).capture("def_risk", "/id")
	tr.step("a project text attribute for every type: no label, so the key's; enum_values dropped to null", owner,
		"POST /api/v1/attribute-definitions",
		jsonBody(`{"project_id":"{{project}}","key":"verifier","data_type":"text","enum_values":["ignored"]}`)).
		capture("def_verifier", "/id")
	tr.step("a project number attribute for design items", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"project_id":"{{project}}","key":"budget","label":"Budget","data_type":"number",`+
			`"applies_to_type":"design-item"}`)).capture("def_budget", "/id")
	tr.step("the source project's own risk for requirements", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"project_id":"{{source_project}}","key":"risk","label":"Source risk","data_type":"enum",`+
			`"enum_values":["low","medium","high"],"applies_to_type":"requirement"}`)).capture("def_source_risk", "/id")

	// Their lists.
	tr.step("the workspace's definitions", owner, "GET /api/v1/attribute-definitions", query("org_id={{owner.workspace}}"))
	tr.step("the project's definitions, read back: enum_values [] where the create answered null", owner,
		"GET /api/v1/attribute-definitions", query("project_id={{project}}"))
	tr.step("the project's effective set: the workspace's and the project's", owner,
		"GET /api/v1/meta/attribute-definitions", query("project_id={{project}}"))
	tr.step("the source project's effective set: its risk replaces the workspace's", owner,
		"GET /api/v1/meta/attribute-definitions", query("project_id={{source_project}}"))
	tr.step("the effective set with no project: the active workspace's", owner, "GET /api/v1/meta/attribute-definitions")
	tr.step("the effective set of a project that does not exist", owner, "GET /api/v1/meta/attribute-definitions",
		query("project_id={{phantom}}"))

	// Enforcement on artifacts.
	tr.step("a requirement with an attributes map that lacks the required risk", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Risky rule",`+
			`"attributes":{}}`))
	tr.step("a requirement with no attributes map: required attributes are not checked", owner,
		"POST /api/v1/artifacts", jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement",`+
			`"title":"Unchecked rule"}`)).capture("unchecked", "/id")
	tr.step("a risk out of range", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Risky rule",`+
			`"attributes":{"risk":"medium"}}`))
	tr.step("a verifier that is not text", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Risky rule",`+
			`"attributes":{"risk":"high","verifier":7}}`))
	tr.step("a requirement with its attributes in range", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Risky rule",`+
			`"attributes":{"risk":"high","verifier":"QA"}}`)).capture("risky", "/id")
	tr.step("a design item whose budget is not a number", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"design-item","title":"Pricey cache",`+
			`"attributes":{"budget":"lots"}}`))
	tr.step("update with an attributes map that drops the required risk: not enforced on update", owner,
		"PUT /api/v1/artifacts/{id}", at("id", "{{risky}}"), jsonBody(`{"attributes":{"verifier":"QA"}}`))
	tr.step("update with a risk out of range: values are checked on update", owner, "PUT /api/v1/artifacts/{id}",
		at("id", "{{risky}}"), jsonBody(`{"attributes":{"risk":"bogus"}}`))

	// Update and delete definitions.
	tr.step("update a definition that does not exist", owner, "PUT /api/v1/attribute-definitions/{id}",
		at("id", "{{phantom}}"), jsonBody(`{"label":"Nothing","data_type":"text"}`))
	tr.step("update with a malformed body", owner, "PUT /api/v1/attribute-definitions/{id}", at("id", "{{def_risk}}"),
		jsonBody(`{"label":`))
	tr.step("update to an unknown data type", owner, "PUT /api/v1/attribute-definitions/{id}", at("id", "{{def_risk}}"),
		jsonBody(`{"label":"Risk","data_type":"money"}`))
	tr.step("update the risk: a third value, no longer required; a key in the body is ignored", owner,
		"PUT /api/v1/attribute-definitions/{id}", at("id", "{{def_risk}}"),
		jsonBody(`{"key":"ignored","label":"Risk level","data_type":"enum","enum_values":["low","medium","high"],`+
			`"applies_to_type":"requirement","sort_order":1}`))
	tr.step("the requirement the required risk refused, now accepted", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{project}}","parent_id":"{{heading}}","type":"requirement","title":"Medium rule",`+
			`"attributes":{"risk":"medium"}}`)).capture("medium_rule", "/id")
	tr.step("delete a definition that does not exist", owner, "DELETE /api/v1/attribute-definitions/{id}",
		at("id", "{{phantom}}"))
	tr.step("delete the budget", owner, "DELETE /api/v1/attribute-definitions/{id}", at("id", "{{def_budget}}"))
	tr.step("delete it again", owner, "DELETE /api/v1/attribute-definitions/{id}", at("id", "{{def_budget}}"))
	tr.step("the project's effective set after the update and the delete", owner, "GET /api/v1/meta/attribute-definitions",
		query("project_id={{project}}"))
}

// artifactsAttributesGates is the role gates, in a shared workspace: a
// personal workspace takes no members.
func artifactsAttributesGates(tr *tour) {
	owner := tr.owner
	tr.keep("2026-01-31", "a date the tour sends as a date attribute; its round trip is the point")
	editor := tr.register("editor", "Tour Editor", "a plain member of the owner's shared workspace (not an admin) "+
		"and an editor of its project")
	viewer := tr.register("viewer", "Tour Viewer", "a plain member of the owner's shared workspace (not an admin) "+
		"and a viewer of its project")
	// From here the owner acts in the shared workspace, and so reads its
	// events; the setups' are read before the next step and left out.
	tr.sharedWorkspace("team", "Tour team")
	tr.setup("the shared workspace's project", owner, "POST /api/v1/projects", jsonBody(`{"name":"Tour team project"}`)).
		capture("team_project", "/id")
	for _, m := range []struct {
		actor *tourActor
		role  string
	}{{editor, "editor"}, {viewer, "viewer"}} {
		tr.join(m.actor, "{{team}}", "member")
		tr.setup("add "+m.actor.name+" to its project", owner, "POST /api/v1/projects/{id}/members",
			at("id", "{{team_project}}"), jsonBody(`{"email":"`+m.actor.email+`","role":"`+m.role+`"}`), expect(201))
	}
	tr.setup("a requirement in the shared project", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{team_project}}","type":"requirement","title":"Team rule"}`)).capture("team_req", "/id")

	// Definitions.
	tr.step("the editor may not define a workspace-wide attribute: that takes a workspace admin", editor,
		"POST /api/v1/attribute-definitions", jsonBody(`{"org_id":"{{team}}","key":"risk","data_type":"text"}`))
	tr.step("the viewer may not define a project attribute: that takes a project editor", viewer,
		"POST /api/v1/attribute-definitions", jsonBody(`{"project_id":"{{team_project}}","key":"verifier","data_type":"text"}`))
	tr.step("the editor defines a project attribute", editor, "POST /api/v1/attribute-definitions",
		jsonBody(`{"project_id":"{{team_project}}","key":"verifier","label":"Verifier","data_type":"boolean"}`)).
		capture("def_team_verifier", "/id")
	tr.step("the owner, the workspace's admin, defines a workspace-wide one", owner, "POST /api/v1/attribute-definitions",
		jsonBody(`{"org_id":"{{team}}","key":"due","label":"Due","data_type":"date"}`)).capture("def_team_due", "/id")
	tr.step("the viewer lists the workspace's definitions", viewer, "GET /api/v1/attribute-definitions",
		query("org_id={{team}}"))
	tr.step("the viewer reads the effective set of the active workspace", viewer, "GET /api/v1/meta/attribute-definitions")
	tr.step("the viewer reads the project's effective set", viewer, "GET /api/v1/meta/attribute-definitions",
		query("project_id={{team_project}}"))
	tr.step("the editor may not update the workspace-wide definition", editor, "PUT /api/v1/attribute-definitions/{id}",
		at("id", "{{def_team_due}}"), jsonBody(`{"label":"Due","data_type":"text"}`),
		note("the gate follows the stored definition's scope, read before the body"))
	tr.step("the editor updates the project one", editor, "PUT /api/v1/attribute-definitions/{id}",
		at("id", "{{def_team_verifier}}"), jsonBody(`{"label":"Verified","data_type":"boolean","sort_order":3}`))
	tr.step("the viewer may not delete it", viewer, "DELETE /api/v1/attribute-definitions/{id}",
		at("id", "{{def_team_verifier}}"))
	tr.step("a date and a boolean that do not parse", editor, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{team_project}}","type":"requirement","title":"Dated rule",`+
			`"attributes":{"due":"soon","verifier":true}}`))
	tr.step("a boolean that is not one", editor, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{team_project}}","type":"requirement","title":"Dated rule",`+
			`"attributes":{"due":"2026-01-31","verifier":"yes"}}`))
	tr.step("the editor creates with both in range; the other workspace's required risk does not apply here", editor,
		"POST /api/v1/artifacts", jsonBody(`{"project_id":"{{team_project}}","type":"requirement","title":"Dated rule",`+
			`"attributes":{"due":"2026-01-31","verifier":true}}`)).capture("team_dated", "/id")
	tr.step("the editor deletes the project definition", editor, "DELETE /api/v1/attribute-definitions/{id}",
		at("id", "{{def_team_verifier}}"))

	// Artifacts.
	tr.step("the viewer reads the requirement", viewer, "GET /api/v1/artifacts/{id}", at("id", "{{team_req}}"))
	tr.step("the viewer lists the project", viewer, "GET /api/v1/artifacts", query("project_id={{team_project}}"))
	tr.step("the viewer reads the versions", viewer, "GET /api/v1/artifacts/{id}/versions", at("id", "{{team_req}}"))
	tr.step("the viewer may not create", viewer, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{team_project}}","type":"requirement","title":"Viewer rule"}`))
	tr.step("the viewer may not update", viewer, "PUT /api/v1/artifacts/{id}", at("id", "{{team_req}}"),
		jsonBody(`{"title":"Viewer title"}`))
	tr.step("the viewer may not restore", viewer, "POST /api/v1/artifacts/{id}/restore", at("id", "{{team_req}}"),
		jsonBody(`{"version":1}`))
	tr.step("the viewer may not delete", viewer, "DELETE /api/v1/artifacts/{id}", at("id", "{{team_req}}"))
	tr.step("the editor updates", editor, "PUT /api/v1/artifacts/{id}", at("id", "{{team_req}}"),
		jsonBody(`{"body":"The team shall agree."}`))
	tr.step("the editor restores version 1", editor, "POST /api/v1/artifacts/{id}/restore", at("id", "{{team_req}}"),
		jsonBody(`{"version":1}`))
	tr.step("the editor deletes", editor, "DELETE /api/v1/artifacts/{id}", at("id", "{{team_dated}}"))
}
