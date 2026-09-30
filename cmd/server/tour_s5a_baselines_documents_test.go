//go:build unix

package main

import (
	"testing"
)

// TestTourS5aBaselinesDocuments is the S5a tour's baselines and documents
// area (refactor plan §6.4 S5a; I4, I5, I8 Content-Disposition, I15
// filenames and media types, I16 snapshot JSON; quirk Q14; OpenV REQ-5,
// REQ-6). Its golden is testdata/tour/s5a/baselines_documents.json.
//
// The owner, an ordinary account, builds project P in its personal
// workspace, which a download's cover names ("Tour Owner's Space"): one
// heading, the only parent, over requirements and a test case with the
// priority and owner attributes, two links, a PNG figure and a PDF
// attachment (which a document does not draw). Every title is unique, and
// so is every link's (from title, type, to title), since a diff sorts its
// entries by title and then by random id.
//
// Baselines: B1 is named "B1 <v1>", which the create answer's JSON escapes
// (\u003c) and the stored snapshot never holds; the snapshot's own "<", in
// the project name and a body, the jsonb read writes back unescaped. B2 is
// sent with no body, so its name defaults to "Baseline YYYY-MM-DD HH:MM" in
// the server's local time (TZ=UTC), and B1 against B2 is an empty diff
// whose lists are all [] (not null). The project then changes (an artifact
// added, one modified, one removed, a link added and one removed), so B1
// against live is not empty; the editor, a project editor from outside the
// workspace, captures B3, and B3 against B1 reads the same changes the other
// way round. Delete is owner-only, and published as baseline.deleted (fixed
// under R7, REQ-5: it published nothing). An empty project E pins the baseline
// list's null (Q14) and the empty export's nulls, and gives a baseline of
// another project, which the scoped lookups (diff, ai-map, report) refuse as
// not found.
//
// Documents: the AI map (live, from B1, of the empty project, and its
// refusals); the legacy /report as PDF (live and from B1) and as DOCX, with
// the format trimmed and lower-cased, and its refusals; it sets no workspace
// source, so its covers lack the workspace name and the DOCX creator is
// "OpenV", and it carries no V&V status, since it reads no test evidence.
// Then /download/pdf and /download/docx with the selection query: the
// default content, which carries each requirement's V&V status (fixed under
// R7, REQ-6: a document carried it only when asked), the four templates,
// toc=0, figures=0, results=1, vv=1, fields, a baseline, and
// attachments=figures, which answers a zip of the document and the figure. A PDF or DOCX is pinned by its structure and text
// (tour_bodies_test.go), since its bytes carry the render time, font subsets
// and Go's map order; the Markdown renderer drops a body's inline "<fast>",
// pinned as it is.
func TestTourS5aBaselinesDocuments(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "baselines_documents",
		about: "Baselines (capture, list, read the stored snapshot, diff against a baseline or live, delete) and the " +
			"documents a project renders: the AI map, the legacy /report (PDF, DOCX) and /download/pdf and " +
			"/download/docx with their selection query, live and from a baseline.",
		run: baselinesDocumentsTour,
	})
}

func baselinesDocumentsTour(tr *tour) {
	tr.wholeSeconds("GET /api/v1/projects/{id}/ai-map", "its Generated line")
	tr.wholeSeconds("GET /api/v1/projects/{id}/report", "a document's generated and captured times and its core properties")
	tr.wholeSeconds("GET /api/v1/projects/{id}/download/docx", "the Word file's core properties")
	editor := baselinesDocumentsSeed(tr)
	baselinesDocumentsBaselines(tr, editor)
	baselinesDocumentsAIMap(tr)
	baselinesDocumentsReports(tr)
	baselinesDocumentsDownloads(tr)
	baselinesDocumentsDelete(tr, editor)

	// The golden's uploads list is in directory order, which the random UUID
	// that prefixes each stored file decides; with two files it would change
	// from run to run. So the PDF attachment is removed once nothing reads it
	// (the delete removes its file), leaving one file under UPLOADS_DIR.
	tr.setup("the PDF attachment removed, so that one file stays under UPLOADS_DIR", tr.owner,
		"DELETE /api/v1/attachments/{id}", at("id", "{{datasheet}}"))
}

// baselinesDocumentsSeed builds project P, the empty project E and the
// editor, and returns the editor. The projects, artifacts, links and
// attachments areas pin these routes; here they are setup, and what the
// baselines and documents show of them is what this area records.
func baselinesDocumentsSeed(tr *tour) *tourActor {
	owner := tr.owner
	tr.setup("project P", owner, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour <spec> & baselines","description":"The project the tour's baselines and documents read."}`)).
		capture("p", "/id")
	tr.setup("the empty project E", owner, "POST /api/v1/projects", jsonBody(`{"name":"Tour empty"}`)).capture("e", "/id")

	tr.setup("heading Scope, the only parent", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"type":"heading","title":"Scope","body":"What the specification covers."}`)).capture("scope", "/id")
	tr.setup("requirement Answer in time", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{scope}}","type":"requirement","title":"Answer in time",`+
		`"body":"The system shall answer <fast> within **2 s**.","attributes":{"priority":"must","owner":"Ada"}}`)).
		capture("answer", "/id")
	tr.setup("requirement Keep records", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{scope}}","type":"requirement","title":"Keep records",`+
		`"body":"The system shall keep a record of each answer.","attributes":{"priority":"should","owner":"Grace"}}`)).
		capture("records", "/id")
	tr.setup("requirement Old rule, removed after B2", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{scope}}","type":"requirement","title":"Old rule","body":"The system shall do the old thing.",`+
		`"attributes":{"priority":"could"}}`)).capture("old", "/id")
	tr.setup("test case Time the answer", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{scope}}","type":"test-case","title":"Time the answer","body":"Measure the answer time.",`+
		`"attributes":{"owner":"Ada"}}`)).capture("timing", "/id")
	tr.setup("Time the answer verifies Answer in time", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{timing}}","to_id":"{{answer}}","type":"verifies"}`)).capture("verifies_answer", "/id")
	tr.setup("Answer in time decomposes to Keep records, removed after B2", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{answer}}","to_id":"{{records}}","type":"decomposes-to"}`)).capture("decomposes", "/id")

	ct, body := multipartForm([][2]string{{"artifact_id", tr.id("answer")}, {"title", "Answer timing"}},
		tourFormFile{"file", "timing.png", "image/png", []byte(tourPNG)})
	tr.setup("a PNG figure on Answer in time", owner, "POST /api/v1/attachments/upload", rawBody(ct, body)).
		capture("figure", "/id")
	ct, body = multipartForm([][2]string{{"artifact_id", tr.id("records")}},
		tourFormFile{"file", "records.pdf", "application/pdf", []byte(tourPDF)})
	tr.setup("a PDF attachment on Keep records", owner, "POST /api/v1/attachments/upload", rawBody(ct, body)).
		capture("datasheet", "/id")

	editor := tr.register("editor", "Tour Editor",
		"a project editor of P from outside the owner's workspace: may capture a baseline, may not delete one")
	tr.setup("the editor joins P as an editor", owner, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-editor@example.com","role":"editor"}`))
	return editor
}

// baselinesDocumentsChange makes the changes the diffs read: an artifact
// added, one modified, one removed, a link added and one removed.
func baselinesDocumentsChange(tr *tour) {
	owner := tr.owner
	tr.setup("requirement New rule, added after B2", owner, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{scope}}","type":"requirement","title":"New rule","body":"The system shall do the new thing.",`+
		`"attributes":{"priority":"must","owner":"Grace"}}`)).capture("new", "/id")
	tr.setup("Keep records renamed and reworded", owner, "PUT /api/v1/artifacts/{id}", at("id", "{{records}}"),
		jsonBody(`{"title":"Keep audit records","body":"The system shall keep an audit record of each answer."}`))
	tr.setup("Old rule removed", owner, "DELETE /api/v1/artifacts/{id}", at("id", "{{old}}"))
	tr.setup("Time the answer verifies New rule", owner, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{timing}}","to_id":"{{new}}","type":"verifies"}`)).capture("verifies_new", "/id")
	tr.setup("the decomposition removed", owner, "DELETE /api/v1/links/{id}", at("id", "{{decomposes}}"))
}

func baselinesDocumentsBaselines(tr *tour, editor *tourActor) {
	owner := tr.owner

	// Q14: an empty list is null, not [].
	tr.step("the empty project's baselines before any: null (Q14)", owner, "GET /api/v1/projects/{id}/baselines",
		at("id", "{{e}}"))
	tr.step("P's baselines before any", owner, "GET /api/v1/projects/{id}/baselines", at("id", "{{p}}"))
	tr.step("the baselines of a project that does not exist", owner, "GET /api/v1/projects/{id}/baselines",
		at("id", "{{phantom}}"), note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))

	// Capture.
	tr.step("capture with a malformed body", owner, "POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"),
		jsonBody(`{`))
	tr.step("capture a baseline of a project that does not exist", owner, "POST /api/v1/projects/{id}/baselines",
		at("id", "{{phantom}}"), jsonBody(`{"name":"Nowhere"}`))
	tr.step("capture B1: 201 with the compacted export as its snapshot, baseline.captured", owner,
		"POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"), jsonBody(`{"name":"B1 <v1>"}`),
		note("the answer is encoded with HTML escaping: the name's < is \\u003c, and the embedded snapshot is "+
			"compacted with the same escaping; created_by_name is left out until a list resolves it")).
		capture("b1", "/id")
	tr.step("capture B2 with no body: its name defaults to the server's local time", owner,
		"POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"),
		note("the handler accepts no body at all; the name is the capture time as YYYY-MM-DD hh:mm in the "+
			"server's time zone (TZ=UTC)")).
		capture("b2", "/id")
	tr.step("capture the empty project: its export's lists are null (Q14)", owner,
		"POST /api/v1/projects/{id}/baselines", at("id", "{{e}}"), jsonBody(`{"name":"Empty"}`)).
		capture("eb", "/id")

	// Read.
	tr.step("read B1: the stored jsonb bytes as they are", owner, "GET /api/v1/baselines/{id}", at("id", "{{b1}}"),
		note("the handler writes the stored snapshot as the database returns it: Postgres's jsonb text, keys ordered by "+
			"length then bytes, \", \" and \": \" separators, < unescaped, and no trailing newline (I16)"))
	tr.step("read the empty project's baseline", owner, "GET /api/v1/baselines/{id}", at("id", "{{eb}}"))
	tr.step("read a baseline that does not exist", owner, "GET /api/v1/baselines/{id}", at("id", "{{phantom}}"))

	// Diff: the refusals, then an empty diff and the changes both ways.
	tr.step("diff with no against", owner, "GET /api/v1/baselines/{id}/diff", at("id", "{{b1}}"))
	tr.step("diff B1 against itself", owner, "GET /api/v1/baselines/{id}/diff", at("id", "{{b1}}"),
		query("against={{b1}}"))
	tr.step("diff B1 against a baseline that does not exist", owner, "GET /api/v1/baselines/{id}/diff",
		at("id", "{{b1}}"), query("against={{phantom}}"))
	tr.step("diff B1 against another project's baseline", owner, "GET /api/v1/baselines/{id}/diff",
		at("id", "{{b1}}"), query("against={{eb}}"),
		note("the comparison is looked up within B1's project, so a baseline of another project is not found"))
	tr.step("diff a baseline that does not exist", owner, "GET /api/v1/baselines/{id}/diff", at("id", "{{phantom}}"),
		query("against=live"))
	tr.step("diff B1 against B2, captured with nothing between them: every list [] (not null)", owner,
		"GET /api/v1/baselines/{id}/diff", at("id", "{{b1}}"), query("against={{b2}}"))

	baselinesDocumentsChange(tr)

	tr.step("diff B1 against live: an artifact added, one modified, one removed, a link added and one removed", owner,
		"GET /api/v1/baselines/{id}/diff", at("id", "{{b1}}"), query("against=live"),
		note("a link's titles are the target side's when it has the artifact, so the removed decomposition "+
			"names Keep records by its new title"))
	tr.step("the editor captures B3 after the changes", editor, "POST /api/v1/projects/{id}/baselines",
		at("id", "{{p}}"), jsonBody(`{"name":"B3 after changes"}`)).capture("b3", "/id")
	tr.step("diff B3 against B1: the same changes the other way round", owner, "GET /api/v1/baselines/{id}/diff",
		at("id", "{{b3}}"), query("against={{b1}}"))
	tr.step("P's baselines, newest first, with no snapshot and each author's name", owner,
		"GET /api/v1/projects/{id}/baselines", at("id", "{{p}}"))
}

func baselinesDocumentsAIMap(tr *tour) {
	owner := tr.owner
	tr.step("the AI map of P's live state", owner, "GET /api/v1/projects/{id}/ai-map", at("id", "{{p}}"),
		note("Markdown; the house-style line is the project's quality rule set (ISO/IEC/IEEE 29148 by default)"))
	tr.step("the AI map from B1", owner, "GET /api/v1/projects/{id}/ai-map", at("id", "{{p}}"),
		query("baseline_id={{b1}}"))
	tr.step("the AI map of the empty project", owner, "GET /api/v1/projects/{id}/ai-map", at("id", "{{e}}"))
	tr.step("the AI map from another project's baseline", owner, "GET /api/v1/projects/{id}/ai-map",
		at("id", "{{p}}"), query("baseline_id={{eb}}"))
	tr.step("the AI map from a baseline that does not exist", owner, "GET /api/v1/projects/{id}/ai-map",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("the AI map of a project that does not exist", owner, "GET /api/v1/projects/{id}/ai-map",
		at("id", "{{phantom}}"), note("the guard answers a project no row has 404 before any lookup, as on every "+
			"route here (I3)"))
}

// baselinesDocumentsReports pins the legacy report route, which renders with
// the default content and no workspace source.
func baselinesDocumentsReports(tr *tour) {
	owner := tr.owner
	tr.step("the report with no format: the live PDF", owner, "GET /api/v1/projects/{id}/report", at("id", "{{p}}"),
		note("the legacy route sets no workspace source, so the cover names no workspace; the filename is "+
			"project_report_<name, [^a-zA-Z0-9_-]+ as _>_<YYYYMMDD_HHMMSS local>.pdf"))
	tr.step("the report as \" DOCX \": trimmed and lower-cased, the live Word file", owner,
		"GET /api/v1/projects/{id}/report", at("id", "{{p}}"), query("format=%20DOCX%20"),
		note("with no workspace source the document's dc:creator is OpenV"))
	tr.step("the report of B1 as PDF", owner, "GET /api/v1/projects/{id}/report", at("id", "{{p}}"),
		query("format=PDF&baseline_id={{b1}}"),
		note("the filename carries the baseline's name, sanitised, before the stamp"))
	tr.step("the report in a format it does not render", owner, "GET /api/v1/projects/{id}/report",
		at("id", "{{p}}"), query("format=html"))
	tr.step("the report of a baseline that does not exist", owner, "GET /api/v1/projects/{id}/report",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("the report of another project's baseline", owner, "GET /api/v1/projects/{id}/report",
		at("id", "{{p}}"), query("format=docx&baseline_id={{eb}}"))
	tr.step("the report of a project that does not exist", owner, "GET /api/v1/projects/{id}/report",
		at("id", "{{phantom}}"))
}

// baselinesDocumentsDownloads pins the download routes, which take the
// selection query and name the workspace on the cover.
func baselinesDocumentsDownloads(tr *tour) {
	owner := tr.owner
	tr.step("download the PDF with no selection: the default content, V&V status included, the workspace on the cover",
		owner, "GET /api/v1/projects/{id}/download/pdf", at("id", "{{p}}"),
		note("the default content carries each requirement's V&V status, the coverage summary and the gaps "+
			"(fixed under R7, REQ-6: only vv=1 or the vv template turned it on)"))
	tr.step("download the Word file with no selection: V&V status included", owner,
		"GET /api/v1/projects/{id}/download/docx", at("id", "{{p}}"))
	tr.step("download the PDF with the requirements-review template", owner, "GET /api/v1/projects/{id}/download/pdf",
		at("id", "{{p}}"), query("template=requirements-review"))
	tr.step("download the PDF with the test-planning template", owner, "GET /api/v1/projects/{id}/download/pdf",
		at("id", "{{p}}"), query("template=test-planning"))
	tr.step("download the PDF with the vv template: evidence and V&V status, with no test run yet", owner,
		"GET /api/v1/projects/{id}/download/pdf", at("id", "{{p}}"), query("template=vv"))
	tr.step("download the Word file, standard template, no contents or figures, results and V&V on, one field", owner,
		"GET /api/v1/projects/{id}/download/docx", at("id", "{{p}}"),
		query("template=standard&toc=0&figures=0&results=1&vv=1&fields=priority"))
	tr.step("download B1 as PDF, only the owner field", owner, "GET /api/v1/projects/{id}/download/pdf",
		at("id", "{{p}}"), query("baseline_id={{b1}}&fields=owner"))
	tr.step("download B1 as Word", owner, "GET /api/v1/projects/{id}/download/docx", at("id", "{{p}}"),
		query("baseline_id={{b1}}"))
	tr.step("download the PDF with its figures: a zip of the document and the files", owner,
		"GET /api/v1/projects/{id}/download/pdf", at("id", "{{p}}"), query("attachments=figures"))
	tr.step("download the PDF of a baseline that does not exist", owner, "GET /api/v1/projects/{id}/download/pdf",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"),
		note("a baseline no row has answers 404, as the report answers it (fixed under R7: the download answered "+
			"any load error 500)"))
	tr.step("download the Word file of a project that does not exist", owner,
		"GET /api/v1/projects/{id}/download/docx", at("id", "{{phantom}}"))
}

func baselinesDocumentsDelete(tr *tour, editor *tourActor) {
	owner := tr.owner
	tr.step("the editor deletes B2: owner only", editor, "DELETE /api/v1/baselines/{id}", at("id", "{{b2}}"),
		note("an editor may capture a baseline (B3) but deleting one takes the project's owner"))
	tr.step("the owner deletes B2: baseline.deleted", owner, "DELETE /api/v1/baselines/{id}", at("id", "{{b2}}"),
		note("the delete is published under the name B2 had (fixed under R7, REQ-5: no event was published)"))
	tr.step("delete B2 again", owner, "DELETE /api/v1/baselines/{id}", at("id", "{{b2}}"),
		note("the baseline is looked up before the guard, so an id no baseline has answers 404, not 403"))
	tr.step("read the deleted B2", owner, "GET /api/v1/baselines/{id}", at("id", "{{b2}}"))
	tr.step("P's baselines after the delete", owner, "GET /api/v1/projects/{id}/baselines", at("id", "{{p}}"))
}
