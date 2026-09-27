//go:build unix

package main

import (
	"strings"
	"testing"
)

// TestTourS5aProjectsTemplates is the S5a tour's projects and templates
// area (refactor plan §6.4 S5a, quirk Q14; OpenV REQ-4, REQ-143), and the
// worked example the other areas follow: one test function calling
// runTourArea, the area's steps in a function named after its key, and
// helpers (if any) prefixed the same way. Its golden is
// testdata/tour/s5a/projects_templates.json.
//
// The owner, an ordinary account, works in its personal workspace: the
// empty lists first (Q14), then projects created, read, updated (the parent
// rules of REQ-144 and their refusals), listed with their children and
// deleted, where both writes also show how a body is decoded (malformed,
// empty, and text after the JSON value); then templates listed, saved from a project with content, and
// instantiated from the workspace's template, the seeded default and the
// example file template, by key and by id. The admin, the platform admin,
// only owns a project in another workspace for the "other workspace" parent
// refusal.
func TestTourS5aProjectsTemplates(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "projects_templates",
		about: "Projects (create, list, read, update with parent rules, children, delete) and templates " +
			"(list, save from a project, instantiate from a workspace, default or file template).",
		run: projectsTemplatesTour,
	})
}

func projectsTemplatesTour(tr *tour) {
	owner := tr.owner
	// A description of 1,710 bytes: an answer that carries it is well over
	// the compressor's 1,400-byte floor whatever its timestamps' lengths.
	long := strings.Repeat("The tour needs this answer long enough to be compressed. ", 30)

	// The workspace before anything is in it (Q14: null or []).
	tr.step("the owner's projects before any exists", owner, "GET /api/v1/projects")
	tr.step("the templates a fresh workspace sees: the seeded default and the example files", owner, "GET /api/v1/templates").
		captureWhere("guided_template", "", "key", "guided-product-skeleton", "id")

	// Create and read.
	tr.step("create a project with a malformed body", owner, "POST /api/v1/projects", jsonBody(`{`))
	// How the body is decoded: an empty body is refused as malformed, and
	// text after the first JSON value is never read (this one then fails on
	// its unknown agent_auth, so no project is created).
	tr.step("create a project with an empty body", owner, "POST /api/v1/projects", jsonBody(``))
	tr.step("create a project with text after the JSON value: only the first value is read", owner,
		"POST /api/v1/projects", jsonBody(`{"name":"Tour bogus","agent_auth":"bogus"} trailing`))
	tr.step("create a project with an unknown agent_auth", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour bogus","agent_auth":"bogus"}`))
	tr.step("create the tour project, a name and description that JSON escapes", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour <core> & \"quotes\"","description":"Holds the tour's requirements: <b>bold</b> & more"}`)).
		capture("tour_project", "/id")
	tr.step("read it back: the database's timestamps", owner, "GET /api/v1/projects/{id}", at("id", "{{tour_project}}"))
	tr.step("read a project that does not exist", owner, "GET /api/v1/projects/{id}", at("id", "{{phantom}}"))
	tr.step("create a second project that runs agents on the workspace key", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour child","agent_auth":"api-key"}`)).capture("child_project", "/id")
	tr.step("list both, newest first", owner, "GET /api/v1/projects")
	tr.step("create a project whose description makes its answers long enough to be compressed", owner,
		"POST /api/v1/projects", jsonBody(`{"name":"Tour long","description":"`+long+`"}`)).
		capture("long_project", "/id")
	tr.step("read it: over 1,400 bytes, so its gzip variant is compressed and keeps its Content-Type", owner,
		"GET /api/v1/projects/{id}", at("id", "{{long_project}}"))

	// Update, and the parent rules (REQ-144).
	tr.step("rename the tour project; an empty description leaves it as it was", owner, "PUT /api/v1/projects/{id}",
		at("id", "{{tour_project}}"), jsonBody(`{"name":"Tour <core> & \"quotes\", renamed","description":""}`))
	tr.step("update with a malformed body", owner, "PUT /api/v1/projects/{id}", at("id", "{{tour_project}}"), jsonBody(`[`))
	tr.step("update with an empty body", owner, "PUT /api/v1/projects/{id}", at("id", "{{tour_project}}"), jsonBody(``))
	tr.step("update with text after the JSON value: only the first value is read (and refused)", owner,
		"PUT /api/v1/projects/{id}", at("id", "{{tour_project}}"), jsonBody(`{"parent_project_id":"{{tour_project}}"} trailing`))
	tr.step("file the second project under the tour project", owner, "PUT /api/v1/projects/{id}",
		at("id", "{{child_project}}"), jsonBody(`{"parent_project_id":"{{tour_project}}"}`))
	tr.step("make a project its own parent", owner, "PUT /api/v1/projects/{id}",
		at("id", "{{tour_project}}"), jsonBody(`{"parent_project_id":"{{tour_project}}"}`))
	tr.step("file a project under its own child", owner, "PUT /api/v1/projects/{id}",
		at("id", "{{tour_project}}"), jsonBody(`{"parent_project_id":"{{child_project}}"}`))
	tr.step("file a project under one that does not exist", owner, "PUT /api/v1/projects/{id}",
		at("id", "{{tour_project}}"), jsonBody(`{"parent_project_id":"{{phantom}}"}`))
	tr.setup("the admin's project, in the admin's own workspace", tr.admin, "POST /api/v1/projects",
		jsonBody(`{"name":"Admin elsewhere"}`)).capture("admin_project", "/id")
	tr.step("file a project under one in another workspace", owner, "PUT /api/v1/projects/{id}",
		at("id", "{{child_project}}"), jsonBody(`{"parent_project_id":"{{admin_project}}"}`))
	tr.step("the tour project's children", owner, "GET /api/v1/projects/{id}/children", at("id", "{{tour_project}}"))
	tr.step("a project with no children", owner, "GET /api/v1/projects/{id}/children", at("id", "{{child_project}}"))
	tr.step("the children of a project that does not exist", owner, "GET /api/v1/projects/{id}/children",
		at("id", "{{phantom}}"), note("the project guard runs before any lookup here, so an id no project has "+
			"answers 403, where GET /api/v1/projects/{id} looks the project up first and answers 404"))

	// Content for the template: one heading with two children and a link,
	// so the snapshot's order is fixed (artifact lists order by parent_id
	// first, a random UUID, so a project a golden lists has one parent).
	// The artifacts and links areas pin these routes; two of the writes are
	// recorded here too, to show what the template carries and the events a
	// write publishes (a step records them; a setup request's are left out).
	tr.setup("a heading", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{tour_project}}",`+
		`"type":"heading","title":"Scope","body":"What the tour covers."}`)).capture("tour_heading", "/id")
	tr.step("a requirement under the heading: artifact.created", owner, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{tour_project}}","parent_id":"{{tour_heading}}","type":"requirement",`+
			`"title":"Answer in time","body":"The system shall answer within 2 s."}`)).
		capture("tour_requirement", "/id")
	tr.setup("a test case under it", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{tour_project}}",`+
		`"parent_id":"{{tour_heading}}","type":"test-case","title":"Time the answer","body":"Measure the answer time."}`)).
		capture("tour_test_case", "/id")
	tr.step("the test case verifies the requirement: link.created", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{tour_test_case}}","to_id":"{{tour_requirement}}","type":"verifies"}`)).capture("tour_link", "/id")

	// Templates: save, list, instantiate.
	tr.step("save a template with no project", owner, "POST /api/v1/templates", jsonBody(`{}`))
	tr.step("save a template with no name", owner, "POST /api/v1/templates", jsonBody(`{"project_id":"{{tour_project}}"}`),
		note("the template service requires a name, and the handler answers any error of the service with 500"))
	tr.step("save a template with a malformed body", owner, "POST /api/v1/templates", jsonBody(`nope`))
	tr.step("save the tour project as a template: the answer embeds its snapshot", owner, "POST /api/v1/templates",
		jsonBody(`{"project_id":"{{tour_project}}","name":"Tour <template>","description":"Saved by the tour"}`)).
		capture("tour_template", "/id")
	tr.step("the templates now: the default, the workspace's, then the files", owner, "GET /api/v1/templates")
	tr.step("a project from the workspace's template", owner, "POST /api/v1/templates/{id}/projects",
		at("id", "{{tour_template}}"), jsonBody(`{"name":"From the tour template","description":"Instantiated"}`)).
		capture("from_template_project", "/id")
	tr.step("the projects, the one made from the template first", owner, "GET /api/v1/projects")
	tr.step("a project from the seeded default template, by id", owner, "POST /api/v1/templates/{id}/projects",
		at("id", "{{guided_template}}"), jsonBody(`{"name":"From the default"}`)).capture("from_default_project", "/id")
	tr.step("a project from the seeded default template, by its key", owner,
		"POST /api/v1/templates/{id}/projects", at("id", "guided-product-skeleton"), jsonBody(`{"name":"By key"}`),
		note("a key resolves only among the example files; a database template is looked up by id"))
	tr.step("a project from the example file template, by key", owner, "POST /api/v1/templates/{id}/projects",
		at("id", "example-openv-platform"), jsonBody(`{"name":"From the example","description":"The example project"}`)).
		capture("from_example_project", "/id")
	tr.step("a project from the example file template, by its derived id and with no name", owner,
		"POST /api/v1/templates/{id}/projects", at("id", "1e241c0f-c902-5f43-8907-21ec0b1b2414"), jsonBody(`{}`)).
		capture("from_example_id_project", "/id")
	tr.step("a project from a template that does not exist", owner, "POST /api/v1/templates/{id}/projects",
		at("id", "{{phantom}}"), jsonBody(`{"name":"Nothing"}`))
	tr.step("a project from a template, with a malformed body", owner, "POST /api/v1/templates/{id}/projects",
		at("id", "{{tour_template}}"), jsonBody(`{`))
	tr.step("a project from a template, with no session", tr.anon, "POST /api/v1/templates/{id}/projects",
		at("id", "{{tour_template}}"), jsonBody(`{"name":"Anonymous"}`),
		note("the auth middleware answers before routing; the handler's own session check is not reached"))

	// Delete.
	tr.step("delete a project that does not exist", owner, "DELETE /api/v1/projects/{id}", at("id", "{{phantom}}"),
		note("as for its children, the owner guard runs first: 403, not 404"))
	tr.step("delete the project made from the default template", owner, "DELETE /api/v1/projects/{id}",
		at("id", "{{from_default_project}}"))
	tr.step("read the deleted project", owner, "GET /api/v1/projects/{id}", at("id", "{{from_default_project}}"))
	tr.step("delete the tour project, the parent of the second one", owner, "DELETE /api/v1/projects/{id}",
		at("id", "{{tour_project}}"))
	tr.step("the second project once its parent is gone", owner, "GET /api/v1/projects/{id}", at("id", "{{child_project}}"))
	tr.step("the templates once the project a template was saved from is gone", owner, "GET /api/v1/templates")
	tr.step("the projects left, newest first", owner, "GET /api/v1/projects")
}
