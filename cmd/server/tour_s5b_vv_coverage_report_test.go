//go:build unix

package main

import (
	"testing"
)

// TestTourS5bVvCoverageReport is the S5b tour's V&V coverage, matrix, gaps,
// report and impact area (refactor plan §6.4 S5b, before M8 splits
// suite_handlers.go; invariants I3 (guard, then lookup), I4, I5, I8
// (the report's unquoted Content-Disposition) and I15; quirk Q1; OpenV
// REQ-13, REQ-23, REQ-143, REQ-146). Its golden is
// testdata/tour/s5b/vv_coverage_report.json.
//
// The owner, an ordinary account in its personal (nightly) workspace, keeps
// project P, every artifact of it at the top level (no parent, so the
// export's order is the order they were created in): two user needs (UN1,
// from which the passing requirement derives, and UN2, from which nothing
// does); eleven requirements, which between them take every coverage rollup;
// two design items that satisfy the passing requirement, one of which
// mitigates hazard H1, while hazard H2 is mitigated by nothing; and a test
// case that verifies nothing. Run 0 records a fail for the passing case and
// is completed; run 1 then records its pass, a fail, a blocked result, and
// a blocked and a fail for the two cases of the mixed requirement, and is
// completed; run 2 records a pass for the failing case and is aborted, and
// an aborted run's results never count, so the case still fails. The
// supplier, a project viewer of P, owns child projects C and C2 and C's
// child project G, whose requirements refine P's with no test case of P's
// own (cross-project links, REQ-146, which the nightly channel allows): C's
// c_r refines P's r_nocase and passes in C's run; C2's c2_split refines P's
// r_split and fails in C2's run; C's c_deep, with no test case of its own,
// refines P's r_deep and is refined in turn by G's g_r, which passes in G's
// run. The owner has no rights on C, C2 or G. Baseline B1 is captured
// before a late requirement is added; E is an empty project with a
// baseline of its own, EB, which P's routes refuse as not found, as they do
// an id no baseline has. The viewer is a project viewer of P.
//
// What the golden shows:
//   - coverage, matrix and gaps are bare encodes (Q1: sniffed text/plain,
//     and no Content-Type once compressed); the refusals go through
//     writeJSONError or respondError (application/json);
//   - coverage lists the requirements in the export's order with a summary
//     keyed by rollup, and every rollup appears: pass, fail, blocked, unrun,
//     uncovered, verified-manually and method-missing; a case's latest result
//     counts (run 1's pass over run 0's fail, and run 2's pass is not
//     counted), and a requirement takes the worst of its cases (the mixed
//     one fails, though its blocked case is listed first); a requirement
//     refined from a child project takes that project's rollup
//     (via_refinements, a refinements entry, flow_down) although the owner
//     cannot read it, from each child project (C passes r_nocase, C2 fails
//     r_split) and from further down (G's pass reaches r_deep through C's
//     c_deep, whose own coverage is computed with G's), and so none of them
//     is in the gaps' requirements_without_test_case;
//   - a baseline read uses B1's artifacts and links with the live results
//     (and the child projects' live coverage); E answers entries [] and
//     summary {}, and every gap list is [] when empty, never null;
//   - the V&V report is a PDF with the JSON coverage's flow-down: the
//     refined requirements take their child projects' rollups there too
//     (r_nocase and r_deep pass, r_split fails), and so none of them is
//     listed under "Requirements without a test case"; a baseline report
//     prints "Baseline: <name>" and still uses the live results and runs;
//     the empty project renders "No requirements found.", "No gaps
//     detected." and "No test runs recorded."; the filename is unquoted,
//     unlike the /download routes' (I8), and carries the day it was
//     rendered, an area pattern; a Range request gets the whole PDF with a
//     200;
//   - impact walks the link graph from one artifact, downstream, upstream or
//     both (anything else is both), lists the nodes of a type by title (the
//     two design items at one link from the passing requirement), and
//     follows the cross-project link to C's requirement, which it names with
//     no title or type.
//
// Nondeterminism, and how the fixture avoids it. latest_results is a Go map
// keyed by random test-case ids, so the steps that read P's coverage or
// matrix sort it (unordered on an object); every other list in these answers
// comes in the export's order. Impact walks each node's neighbours in the
// order of their random ids and breaks ties within a type by title and then
// id, so the link graph is a tree (every node has one path from any seed)
// and every title is unique within its type. Each requirement is refined by
// at most one artifact: the documents list two incoming cross-project links
// in an order that changes from run to run. The report renders the time it
// was generated and each run's start and completion as "YYYY-MM-DD hh:mm" in
// the server's local time (TZ=UTC), which the generic <datetime> covers; its
// Info dates are <pdf-date>, and its font subsets and flate streams are left
// to the structural PDF summary.
//
// Also recorded, once the runs exist (S5a pinned them with no test run):
// the /download/pdf V&V template and the Word file with results and V&V on,
// whose evidence comes from the closure main.go wires into the download
// service (M3 names it).
func TestTourS5bVvCoverageReport(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "vv_coverage_report",
		about: "V&V coverage (with the flow-down from a child project), the traceability matrix, the gap analysis, " +
			"the V&V report PDF and impact analysis, live and from a baseline, and the V&V document downloads " +
			"once test runs exist.",
		run: vvCoverageReportTour,
	})
}

// vvCoverageReportArtifact creates one artifact of P (setup; the artifacts
// area pins the route) and registers its id under name.
func vvCoverageReportArtifact(tr *tour, project, name, typ, title, attributes string) {
	tr.t.Helper()
	body := `{"project_id":"{{` + project + `}}","type":"` + typ + `","title":"` + title + `"`
	if attributes != "" {
		body += `,"attributes":` + attributes
	}
	tr.setup(typ+" "+title, tr.owner, "POST /api/v1/artifacts", jsonBody(body+`}`)).capture(name, "/id")
}

// vvCoverageReportLink links two artifacts of P (setup; the links area pins
// the route).
func vvCoverageReportLink(tr *tour, a *tourActor, from, typ, to string) {
	tr.t.Helper()
	tr.setup(from+" "+typ+" "+to, a, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{`+from+`}}","to_id":"{{`+to+`}}","type":"`+typ+`"}`))
}

// vvCoverageReportSeed builds P, its runs, the supplier's child project C,
// the baselines and the empty project, and returns the viewer and the
// supplier.
func vvCoverageReportSeed(tr *tour) (viewer, supplier *tourActor) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour V&V: coverage"}`)).capture("p", "/id")
	tr.setup("the empty project E", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour empty"}`)).capture("e", "/id")

	// P's artifacts, all at the top level, so the export lists them in the
	// order they are created.
	vvCoverageReportArtifact(tr, "p", "un1", "user-need", "Get an answer fast", "")
	vvCoverageReportArtifact(tr, "p", "un2", "user-need", "See every past answer", "")
	for _, r := range []struct{ name, title, attributes string }{
		{"r_pass", "Answer in time", `{"verification_method":"test"}`},
		{"r_fail", "Log each answer", `{"verification_method":"test"}`},
		{"r_blocked", "Survive a restart", `{"verification_method":"test"}`},
		{"r_unrun", "Export the log", `{"verification_method":"test"}`},
		{"r_nocase", "Meet the supplier timing", `{"verification_method":"test"}`},
		{"r_manual", "Label the enclosure", `{"verification_method":"inspection","verification_status":"verified"}`},
		{"r_analysis", "Stay within the power budget", `{"verification_method":"analysis"}`},
		{"r_nomethod", "Look tidy on the shelf", ""},
		{"r_mixed", "Keep the clock in step", `{"verification_method":"test"}`},
		{"r_split", "Share the timing budget", `{"verification_method":"test"}`},
		{"r_deep", "Meet the sub-supplier timing", `{"verification_method":"test"}`},
	} {
		vvCoverageReportArtifact(tr, "p", r.name, "requirement", r.title, r.attributes)
	}
	vvCoverageReportArtifact(tr, "p", "d1", "design-item", "Timing loop", "")
	vvCoverageReportArtifact(tr, "p", "d2", "design-item", "Answer cache", "")
	vvCoverageReportArtifact(tr, "p", "h1", "hazard", "Late answer", "")
	vvCoverageReportArtifact(tr, "p", "h2", "hazard", "Lost record", "")
	for _, tc := range []struct{ name, title string }{
		{"tc_pass", "Time the answer"},
		{"tc_fail", "Read the log back"},
		{"tc_blocked", "Restart the rig"},
		{"tc_unrun", "Compare the export"},
		{"tc_orphan", "Spare check"},
		{"tc_mixed_b", "Check the clock drift"},
		{"tc_mixed_f", "Check the clock sync"},
	} {
		vvCoverageReportArtifact(tr, "p", tc.name, "test-case", tc.title, "")
	}
	// A tree: every artifact has one path to any other, so impact's walk
	// finds the same path and via whichever neighbour it visits first.
	vvCoverageReportLink(tr, o, "r_pass", "derives-from", "un1")
	vvCoverageReportLink(tr, o, "d1", "satisfies", "r_pass")
	vvCoverageReportLink(tr, o, "d1", "mitigates", "h1")
	vvCoverageReportLink(tr, o, "tc_pass", "verifies", "r_pass")
	vvCoverageReportLink(tr, o, "tc_fail", "verifies", "r_fail")
	vvCoverageReportLink(tr, o, "tc_blocked", "verifies", "r_blocked")
	vvCoverageReportLink(tr, o, "tc_unrun", "verifies", "r_unrun")
	vvCoverageReportLink(tr, o, "d2", "satisfies", "r_pass")
	// The export lists links newest first, so the blocked case comes first
	// and only a worst-of rollup reads the failing one.
	vvCoverageReportLink(tr, o, "tc_mixed_f", "verifies", "r_mixed")
	vvCoverageReportLink(tr, o, "tc_mixed_b", "verifies", "r_mixed")

	// Run 0, completed before run 1: a fail for the passing case, which run
	// 1's later pass supersedes (the latest result per case wins).
	tr.setup("run 0", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Run 0","description":"An early pass over P."}`)).capture("run0", "/id")
	tr.setup("run 0 records a fail for the passing case", o, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{run0}}"), jsonBody(`{"test_case_id":"{{tc_pass}}","status":"fail"}`))
	tr.setup("run 0 completed", o, "PUT /api/v1/test-runs/{id}", at("id", "{{run0}}"), jsonBody(`{"status":"completed"}`))

	// Run 1, completed: a pass, a fail and a blocked result, and a blocked
	// and a fail result for the mixed requirement's two cases. Run 2,
	// aborted: a pass for the failing case, which the latest results leave
	// out.
	tr.setup("run 1", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Run 1","description":"The first pass over P."}`)).capture("run1", "/id")
	for _, res := range [][2]string{{"tc_pass", "pass"}, {"tc_fail", "fail"}, {"tc_blocked", "blocked"},
		{"tc_mixed_b", "blocked"}, {"tc_mixed_f", "fail"}} {
		tr.setup("run 1 records "+res[1]+" for "+res[0], o, "POST /api/v1/test-runs/{id}/results",
			at("id", "{{run1}}"), jsonBody(`{"test_case_id":"{{`+res[0]+`}}","status":"`+res[1]+`"}`))
	}
	tr.setup("run 1 completed", o, "PUT /api/v1/test-runs/{id}", at("id", "{{run1}}"), jsonBody(`{"status":"completed"}`))
	tr.setup("run 2", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Run 2","description":"A retry, abandoned."}`)).capture("run2", "/id")
	tr.setup("run 2 records a pass for the failing case", o, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{run2}}"), jsonBody(`{"test_case_id":"{{tc_fail}}","status":"pass"}`))
	tr.setup("run 2 aborted", o, "PUT /api/v1/test-runs/{id}", at("id", "{{run2}}"), jsonBody(`{"status":"aborted"}`))

	// The supplier's child project C (REQ-146): a requirement that refines
	// P's requirement with no test case, and passes in C's own run. The
	// owner has no rights on C, nor on C2 and G below.
	supplier = tr.register("supplier", "Tour Supplier", "a project viewer of P who owns child project C, whose "+
		"requirement refines one of P's; the owner has no rights on C")
	tr.setup("the supplier joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-supplier@example.com","role":"viewer"}`), expect(201))
	tr.setup("the supplier's child project C", supplier, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour supplier part"}`)).capture("c", "/id")
	tr.setup("C's requirement", supplier, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{c}}",`+
		`"type":"requirement","title":"Supplier timing","attributes":{"verification_method":"test"}}`)).capture("c_r", "/id")
	vvCoverageReportLink(tr, supplier, "c_r", "refines", "r_nocase")
	tr.setup("C's test case", supplier, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{c}}",`+
		`"type":"test-case","title":"Time the supplier part"}`)).capture("tc_c", "/id")
	vvCoverageReportLink(tr, supplier, "tc_c", "verifies", "c_r")
	tr.setup("C's run", supplier, "POST /api/v1/projects/{id}/test-runs", at("id", "{{c}}"),
		jsonBody(`{"name":"Supplier run"}`)).capture("c_run", "/id")
	tr.setup("C's run records a pass", supplier, "POST /api/v1/test-runs/{id}/results", at("id", "{{c_run}}"),
		jsonBody(`{"test_case_id":"{{tc_c}}","status":"pass"}`))

	// Several children, several levels: r_split is refined from a second
	// child project C2 (fail); r_deep is refined by C's c_deep, which has no
	// test case and is itself refined from grandchild G (pass). Each
	// requirement has at most one refinement: the documents list two
	// incoming cross-project links in an order that changes from run to run.
	supplierReq := func(project, name, title string) {
		tr.setup(name, supplier, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{`+project+`}}",`+
			`"type":"requirement","title":"`+title+`","attributes":{"verification_method":"test"}}`)).capture(name, "/id")
	}
	supplierCase := func(project, name, title, verifies, run, status string) {
		tr.setup(name, supplier, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{`+project+`}}",`+
			`"type":"test-case","title":"`+title+`"}`)).capture(name, "/id")
		vvCoverageReportLink(tr, supplier, name, "verifies", verifies)
		tr.setup(run+" records "+status+" for "+name, supplier, "POST /api/v1/test-runs/{id}/results",
			at("id", "{{"+run+"}}"), jsonBody(`{"test_case_id":"{{`+name+`}}","status":"`+status+`"}`))
	}
	supplierReq("c", "c_deep", "Sub-supplier timing")
	vvCoverageReportLink(tr, supplier, "c_deep", "refines", "r_deep")
	tr.setup("the supplier's second child project C2", supplier, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour second supplier part"}`)).capture("c2", "/id")
	tr.setup("C2's run", supplier, "POST /api/v1/projects/{id}/test-runs", at("id", "{{c2}}"),
		jsonBody(`{"name":"Second supplier run"}`)).capture("c2_run", "/id")
	supplierReq("c2", "c2_split", "Second budget share")
	vvCoverageReportLink(tr, supplier, "c2_split", "refines", "r_split")
	supplierCase("c2", "tc_c2_split", "Time the second share", "c2_split", "c2_run", "fail")
	tr.setup("the supplier's grandchild project G", supplier, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour sub-supplier part"}`)).capture("g", "/id")
	tr.setup("G's run", supplier, "POST /api/v1/projects/{id}/test-runs", at("id", "{{g}}"),
		jsonBody(`{"name":"Sub-supplier run"}`)).capture("g_run", "/id")
	supplierReq("g", "g_r", "Sub-supplier clock")
	vvCoverageReportLink(tr, supplier, "g_r", "refines", "c_deep")
	supplierCase("g", "tc_g", "Time the sub-supplier clock", "g_r", "g_run", "pass")

	// Baseline B1, then a requirement B1 does not hold; E's baseline EB.
	tr.setup("baseline B1 of P", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"),
		jsonBody(`{"name":"B1 before the late requirement"}`)).capture("b1", "/id")
	vvCoverageReportArtifact(tr, "p", "r_late", "requirement", "Added after B1", `{"verification_method":"test"}`)
	tr.setup("baseline EB of E", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{e}}"),
		jsonBody(`{"name":"EB"}`)).capture("eb", "/id")

	viewer = tr.register("viewer", "Tour Viewer", "a project viewer of P, from a workspace of its own")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	return viewer, supplier
}

func vvCoverageReportTour(tr *tour) {
	tr.pattern(`vv-report-.*-(\d{8})\.pdf`, "<yyyymmdd>", "the day the V&V report was rendered, in its filename")
	tr.wholeSeconds("GET /api/v1/projects/{id}/download/docx", "the Word file's core properties")
	viewer, supplier := vvCoverageReportSeed(tr)
	vvCoverageReportCoverage(tr, viewer, supplier)
	vvCoverageReportMatrixGaps(tr, viewer)
	vvCoverageReportReport(tr, viewer)
	vvCoverageReportImpact(tr, viewer)
	vvCoverageReportDownloads(tr)
}

// vvCoverageReportLatest sorts the latest_results of every element of the
// list a pointer names: a Go map keyed by random test-case ids, which holds
// two keys for the requirement with two test cases.
func vvCoverageReportLatest(list string) tourOpt {
	return unordered(list+"/*/latest_results", "a Go map keyed by random test-case ids, written in the order of "+
		"its sorted keys")
}

// vvCoverageReportCoverage pins GET /vv/coverage: live, from a baseline,
// and its refusals.
func vvCoverageReportCoverage(tr *tour, viewer, supplier *tourActor) {
	o := tr.owner
	entries := vvCoverageReportLatest("/entries")
	tr.step("P's coverage: every rollup; a case's latest result, a requirement's worst case, and the flow-down "+
		"from C, from C2 and, through C, from G", o, "GET /api/v1/projects/{id}/vv/coverage", at("id", "{{p}}"),
		entries,
		note("run 1's pass for the passing case is later than run 0's fail, so it counts; run 2's pass for the "+
			"failing case is left out: an aborted run's results never count"),
		note("the mixed requirement lists its blocked case first and fails on its other: the worst result decides"),
		note("C's coverage is computed with C's own child G, so G's pass reaches r_deep through c_deep, which has "+
			"no test case of its own"),
		note("the owner has no rights on C, yet the coverage carries C's rollup for the requirement it refines: "+
			"a rollup per requirement P already links to, never C's content"))
	tr.step("P's coverage with baseline_id=live: the live coverage", o, "GET /api/v1/projects/{id}/vv/coverage",
		at("id", "{{p}}"), query("baseline_id=live"), entries)
	tr.step("P's coverage from B1: B1's requirements (no late one), with the live results", o,
		"GET /api/v1/projects/{id}/vv/coverage", at("id", "{{p}}"), query("baseline_id={{b1}}"), entries,
		note("the snapshot holds the artifacts and links; the results, and C's coverage, are read live"))
	tr.step("P's coverage from another project's baseline: not found", o, "GET /api/v1/projects/{id}/vv/coverage",
		at("id", "{{p}}"), query("baseline_id={{eb}}"),
		note("the baseline lookup is scoped to the project, so another project's baseline reads as missing"))
	tr.step("P's coverage from a baseline that does not exist", o, "GET /api/v1/projects/{id}/vv/coverage",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("P's coverage from a baseline_id that is not a UUID: not found too", o,
		"GET /api/v1/projects/{id}/vv/coverage", at("id", "{{p}}"), query("baseline_id=not-a-uuid"),
		note("the scoped lookup answers any error as not found"))
	tr.step("E's coverage: no entries, an empty summary", o, "GET /api/v1/projects/{id}/vv/coverage",
		at("id", "{{e}}"))
	tr.step("the viewer reads P's coverage", viewer, "GET /api/v1/projects/{id}/vv/coverage", at("id", "{{p}}"),
		entries)
	tr.step("the supplier reads C's coverage: c_r passes on its own case, c_deep through G", supplier,
		"GET /api/v1/projects/{id}/vv/coverage", at("id", "{{c}}"))
	tr.step("the owner reads C's coverage: no rights on C", o, "GET /api/v1/projects/{id}/vv/coverage",
		at("id", "{{c}}"))
	tr.step("the coverage of a project that does not exist", o, "GET /api/v1/projects/{id}/vv/coverage",
		at("id", "{{phantom}}"), note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
}

// vvCoverageReportMatrixGaps pins GET /vv/matrix and GET /vv/gaps.
func vvCoverageReportMatrixGaps(tr *tour, viewer *tourActor) {
	o := tr.owner
	rows := vvCoverageReportLatest("/rows")
	tr.step("P's matrix: user needs, design items, test cases, latest results and hazards per requirement", o,
		"GET /api/v1/projects/{id}/vv/matrix", at("id", "{{p}}"), rows,
		note("a requirement's hazards are those its design items mitigate"))
	tr.step("P's matrix from B1", o, "GET /api/v1/projects/{id}/vv/matrix", at("id", "{{p}}"),
		query("baseline_id={{b1}}"), rows)
	tr.step("E's matrix: no rows", o, "GET /api/v1/projects/{id}/vv/matrix", at("id", "{{e}}"))
	tr.step("P's matrix from another project's baseline", o, "GET /api/v1/projects/{id}/vv/matrix",
		at("id", "{{p}}"), query("baseline_id={{eb}}"))
	tr.step("the viewer reads P's matrix", viewer, "GET /api/v1/projects/{id}/vv/matrix", at("id", "{{p}}"), rows)
	tr.step("the matrix of a project that does not exist", o, "GET /api/v1/projects/{id}/vv/matrix",
		at("id", "{{phantom}}"))

	tr.step("P's gaps: all seven lists; the refined requirements are not without a test case", o,
		"GET /api/v1/projects/{id}/vv/gaps", at("id", "{{p}}"),
		note("the gaps read the flow-down coverage, so a requirement verified through a child project needs no test "+
			"case of its own"))
	tr.step("P's gaps from B1: no requirement without a test case, an empty list", o,
		"GET /api/v1/projects/{id}/vv/gaps", at("id", "{{p}}"), query("baseline_id={{b1}}"))
	tr.step("E's gaps: seven empty lists, never null", o, "GET /api/v1/projects/{id}/vv/gaps", at("id", "{{e}}"))
	tr.step("P's gaps from a baseline that does not exist", o, "GET /api/v1/projects/{id}/vv/gaps",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("the viewer reads P's gaps", viewer, "GET /api/v1/projects/{id}/vv/gaps", at("id", "{{p}}"))
	tr.step("the gaps of a project that does not exist", o, "GET /api/v1/projects/{id}/vv/gaps",
		at("id", "{{phantom}}"))
}

// vvCoverageReportReport pins GET /vv/report, the V&V status PDF.
func vvCoverageReportReport(tr *tour, viewer *tourActor) {
	o := tr.owner
	tr.step("P's V&V report: a PDF with the flow-down, so the refined requirements take their child projects' rollups",
		o, "GET /api/v1/projects/{id}/vv/report", at("id", "{{p}}"),
		note("the report reads the coverage /vv/coverage answers, so a requirement verified through a child project "+
			"is not listed under \"Requirements without a test case\" (REQ-146)"),
		note("the filename is not quoted, unlike the /download routes', and carries the day it was rendered"))
	tr.step("P's V&V report with baseline_id=live", o, "GET /api/v1/projects/{id}/vv/report", at("id", "{{p}}"),
		query("baseline_id=live"))
	tr.step("a Range request for P's V&V report: ignored, the whole PDF", o, "GET /api/v1/projects/{id}/vv/report",
		at("id", "{{p}}"), withHeader("Range", "bytes=0-99"),
		note("the handler writes the rendered bytes itself, so there is no 206 and no Accept-Ranges, unlike an "+
			"evidence download served by http.ServeFile"))
	tr.step("P's V&V report from B1: a Baseline line, the live results and runs", o,
		"GET /api/v1/projects/{id}/vv/report", at("id", "{{p}}"), query("baseline_id={{b1}}"))
	tr.step("E's V&V report: no requirements, no gaps, no runs", o, "GET /api/v1/projects/{id}/vv/report",
		at("id", "{{e}}"))
	tr.step("P's V&V report from another project's baseline", o, "GET /api/v1/projects/{id}/vv/report",
		at("id", "{{p}}"), query("baseline_id={{eb}}"))
	tr.step("P's V&V report from a baseline that does not exist", o, "GET /api/v1/projects/{id}/vv/report",
		at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("the V&V report of a project that does not exist", o, "GET /api/v1/projects/{id}/vv/report",
		at("id", "{{phantom}}"))
	tr.step("the viewer reads P's V&V report", viewer, "GET /api/v1/projects/{id}/vv/report", at("id", "{{p}}"))
}

// vvCoverageReportImpact pins GET /impact.
func vvCoverageReportImpact(tr *tour, viewer *tourActor) {
	o := tr.owner
	for _, d := range []struct{ title, query string }{
		{"downstream: what depends on it", "artifact={{r_pass}}&direction=downstream"},
		{"upstream: what it depends on", "artifact={{r_pass}}&direction=upstream"},
		{"both", "artifact={{r_pass}}&direction=both"},
		{"with no direction: both", "artifact={{r_pass}}"},
		{"with a direction it does not know: both", "artifact={{r_pass}}&direction=sideways"},
	} {
		tr.step("the impact of the passing requirement, "+d.title, o, "GET /api/v1/projects/{id}/impact",
			at("id", "{{p}}"), query(d.query))
	}
	tr.step("the design item's impact upstream: a path two links long", o, "GET /api/v1/projects/{id}/impact",
		at("id", "{{p}}"), query("artifact={{d1}}&direction=upstream"))
	tr.step("the first user need's impact downstream: a path two links long", o,
		"GET /api/v1/projects/{id}/impact", at("id", "{{p}}"), query("artifact={{un1}}&direction=downstream"))
	tr.step("the refined requirement's impact downstream: C's requirement, with no title or type", o,
		"GET /api/v1/projects/{id}/impact", at("id", "{{p}}"), query("artifact={{r_nocase}}&direction=downstream"),
		note("the export holds the cross-project link but not the far artifact, so the walk reaches it and names "+
			"only its id"))
	tr.step("an artifact id with spaces around it: trimmed", o, "GET /api/v1/projects/{id}/impact",
		at("id", "{{p}}"), query("artifact=%20{{tc_pass}}%20&direction=upstream"))
	tr.step("the impact from B1", o, "GET /api/v1/projects/{id}/impact", at("id", "{{p}}"),
		query("artifact={{r_pass}}&baseline_id={{b1}}"))
	tr.step("the impact with no artifact", o, "GET /api/v1/projects/{id}/impact", at("id", "{{p}}"),
		query("direction=downstream"))
	tr.step("no artifact and a baseline that does not exist: the parameter is checked first", o,
		"GET /api/v1/projects/{id}/impact", at("id", "{{p}}"), query("baseline_id={{phantom}}"))
	tr.step("the impact of C's requirement in P: not in the project", o, "GET /api/v1/projects/{id}/impact",
		at("id", "{{p}}"), query("artifact={{c_r}}"))
	tr.step("the late requirement's impact in B1, which does not hold it", o, "GET /api/v1/projects/{id}/impact",
		at("id", "{{p}}"), query("artifact={{r_late}}&baseline_id={{b1}}"))
	tr.step("the impact from another project's baseline", o, "GET /api/v1/projects/{id}/impact",
		at("id", "{{p}}"), query("artifact={{r_pass}}&baseline_id={{eb}}"))
	tr.step("the viewer reads an impact", viewer, "GET /api/v1/projects/{id}/impact", at("id", "{{p}}"),
		query("artifact={{h1}}"))
	tr.step("the impact in a project that does not exist", o, "GET /api/v1/projects/{id}/impact",
		at("id", "{{phantom}}"), query("artifact={{r_pass}}"))
}

// vvCoverageReportDownloads records two S5a routes once test runs exist:
// the documents' evidence comes from the closure main.go wires into the
// download service (M3 names it), which S5a saw only with no run.
func vvCoverageReportDownloads(tr *tour) {
	o := tr.owner
	tr.step("download P's PDF with the vv template: V&V status and the test results of runs 0, 1 and 2", o,
		"GET /api/v1/projects/{id}/download/pdf", at("id", "{{p}}"), query("template=vv"),
		note("an S5a route: the latest results and the runs come from live state beside the snapshot"),
		note("the vv template leaves design items out, and with them the link that mitigates H1, so its gaps list "+
			"both hazards as unmitigated where /vv/gaps lists H2 alone; like the V&V report it applies the flow-down, "+
			"and it names the child projects' requirements, which the snapshot does not hold, by their ids"))
	tr.step("download P's Word file with results and V&V on", o, "GET /api/v1/projects/{id}/download/docx",
		at("id", "{{p}}"), query("results=1&vv=1"), note("an S5a route, read with test runs recorded"))
}
