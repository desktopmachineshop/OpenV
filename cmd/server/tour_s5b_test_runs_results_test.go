//go:build unix

package main

import (
	"strings"
	"testing"
)

// TestTourS5bTestRunsResults is the S5b tour's test runs and results area
// (refactor plan §6.4 S5b, before M8 splits suite_handlers.go; invariants
// I3 (guard, lookup and decode order), I4, I5 and I10; quirks Q1, Q14 and
// Q19, and Q2's deterministic neighbour, fixed under R7; OpenV REQ-13,
// REQ-23, REQ-143), and
// the worked example the other S5b areas follow: one test function calling
// runTourArea, the area's steps in a function named after its key, and
// helpers prefixed the same way. Its golden is
// testdata/tour/s5b/test_runs_results.json.
//
// The owner, an ordinary account in its personal (nightly) workspace, keeps
// project P, whose one heading holds a requirement and three test cases:
// one an agent may execute (no execution_method, so automated), one flagged
// manual and one physical. Project Q holds a test case of its own, and
// project E nothing. A viewer of P and an outsider, both registered here,
// show the role gates, and an editor of P from a workspace of its own shows
// whose agent a launch uses. The area walks a run's life: created (with the
// refusals of its body, its guard and its baseline_id, and a run on P's
// baseline as setup), read and listed;
// results recorded, re-recorded (a re-record adds a result of its own, the
// case's current one from then on, and an omitted evidence list carries the
// current one's), refused (another project's test case among them) and
// listed; a run and a result recorded through a workspace runner key,
// the path the MCP tools take; an agent run launched on the run (the
// launch's refusals, and its answer: the queued run with its prompt, and
// the manual and physical cases it skips), claimed by that key as setup,
// and results recorded with the claimed run's token, which is refused a
// case flagged manual and a run of another project; the run completed and
// aborted, the refused transitions, a result refused by the completed and the
// aborted run; the deletes (a run that holds results kept, an empty one
// deleted); the editor's launch on a run of its own, with P's workspace's
// V&V engineer; R1's history; a run on Q's baseline, refused; and, last, a
// run whose baseline is deleted, read back and listed with the reference
// marked baseline_deleted, and a run on the deleted baseline, refused.
// Every answer of these handlers but a refusal is a
// bare encode with no Content-Type (Q1): sniffed as text/plain, and sent
// with none once compressed, which a long description and long notes make
// the lists show; the refusals go through writeJSONError (application/json).
// The lists of an empty project and of a run with no result answer null
// (Q14).
//
// Nondeterminism: only ids and minted times. The runs and the results each
// come from a request of their own, so their lists, ordered by started_at
// and updated_at, never tie; the test cases sit under one heading, so the
// agent's prompt lists them in the order they were created. The claim is
// the only one in the area and the launched run the only queued run, so it
// claims that run; this area reads no runner_online, which a runner key's
// use would make depend on the clock (so an area that reads it, such as
// the guided copilot's, must not use one).
//
// What the other S5b areas need beyond this example, and the framework has
// for them: eventStream(n) for a route that answers an event stream
// (tour_stream_test.go), tourArea.env for a limit made small enough to reach
// (OPENV_MAX_EVIDENCE_MB and OPENV_LIMITS, in the evidence area, whose
// uploads over them the golden records by digest), unordered on an object
// for a Go map keyed by random ids (latest_results), tr.pattern for a value
// no generic token covers (a report file name's date), tr.headerPattern for
// one only a header should match (a Retry-After that counts down), tr.keep
// for a time the tour sends into a TIMESTAMP column, and tr.probe to wait
// for what a bus subscriber does after an answer.
func TestTourS5bTestRunsResults(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "test_runs_results",
		about: "Test runs (create, read, list, complete and abort, delete) and their results (record, re-record, " +
			"list), as a person, a workspace runner key and an agent run's token, and an agent run launched to " +
			"execute a run's automated test cases.",
		run: testRunsResultsTour,
	})
}

// testRunsResultsSeed creates the projects, test cases and accounts the
// area reads (setup; the artifacts area pins those routes).
func testRunsResultsSeed(tr *tour) (viewer, outsider *tourActor) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour runs"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour other runs"}`)).capture("q", "/id")
	tr.setup("project E, which never gets a run", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour empty"}`)).
		capture("e", "/id")
	tr.setup("P's heading, the one parent of its artifacts", o, "POST /api/v1/artifacts",
		jsonBody(`{"project_id":"{{p}}","type":"heading","title":"Verification"}`)).capture("heading", "/id")
	tr.setup("a requirement the test cases verify", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
		`"parent_id":"{{heading}}","type":"requirement","title":"Answer in time",`+
		`"body":"The system shall answer within 2 s.","attributes":{"verification_method":"test"}}`)).capture("req", "/id")
	for _, tc := range []struct{ name, title, attributes string }{
		{"tc_auto", "Time the answer", ``},
		{"tc_manual", "Judge the wording", `,"attributes":{"execution_method":"manual"}`},
		{"tc_rig", "Measure on the rig", `,"attributes":{"execution_method":"physical"}`},
	} {
		tr.setup("test case "+tc.title, o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{p}}",`+
			`"parent_id":"{{heading}}","type":"test-case","title":"`+tc.title+`","body":"Steps and expected result."`+
			tc.attributes+`}`)).capture(tc.name, "/id")
		tr.setup(tc.title+" verifies the requirement", o, "POST /api/v1/links",
			jsonBody(`{"from_id":"{{`+tc.name+`}}","to_id":"{{req}}","type":"verifies"}`))
	}
	tr.setup("Q's test case", o, "POST /api/v1/artifacts", jsonBody(`{"project_id":"{{q}}","type":"test-case",`+
		`"title":"Check the other project","body":"Steps."}`)).capture("tc_q", "/id")

	viewer = tr.register("viewer", "Tour Viewer", "a viewer of P, from a workspace of its own: may read, not write")
	tr.setup("the viewer joins P as a viewer", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(201))
	outsider = tr.register("outsider", "Tour Outsider", "an ordinary account in a workspace of its own, with no "+
		"access to P")
	return viewer, outsider
}

func testRunsResultsTour(tr *tour) {
	o := tr.owner
	viewer, outsider := testRunsResultsSeed(tr)
	// 1,494 bytes (1,509 once JSON escapes its <, > and &): an answer that
	// carries it is well over the compressor's 1,400-byte floor whatever its
	// timestamps' lengths, so its gzip variant is compressed and shows what
	// the compressor does to a bare encode (Q1: no Content-Type).
	long := strings.Repeat("The tour needs this answer long enough to be compressed. ", 26) + "<end> & done"

	// Creating a run, and the refusals: guard, decode, then the service.
	tr.step("the runs of a project with none: null (Q14)", o, "GET /api/v1/projects/{id}/test-runs", at("id", "{{e}}"))
	tr.step("create a run with a malformed body", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{`))
	tr.step("create a run with no name", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"  ","description":"blank"}`))
	tr.step("the viewer creates a run", viewer, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Viewer run"}`))
	tr.step("create a run in a project that does not exist", o, "POST /api/v1/projects/{id}/test-runs",
		at("id", "{{phantom}}"), jsonBody(`{"name":"Nowhere"}`),
		note("the project guard answers a project no row has as one the caller cannot reach: 404 (I3)"))
	tr.step("create a run whose baseline_id is not a UUID: 404, as a baseline no row has", o,
		"POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"), jsonBody(`{"name":"Bad baseline","baseline_id":"not-a-uuid"}`),
		note("fixed under R7 (#379's decision on REQ-6): it answered 400 with the driver's text (Q19)"))
	tr.step("create a run whose baseline_id no baseline has: 404 baseline not found", o,
		"POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Phantom baseline","baseline_id":"{{phantom}}"}`),
		note("fixed under R7 (#379's decision on REQ-6): it was accepted, since test_runs.baseline_id has no "+
			"foreign key and nothing looked the baseline up"))
	tr.setup("P's baseline", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour runs baseline"}`), expect(201)).capture("bp", "/id")
	tr.setup("a run on P's baseline, aborted later", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Baselined run","baseline_id":"{{bp}}"}`), expect(201)).capture("run_baselined", "/id")
	tr.step("create run R1: a name and description that JSON escapes; started_at, created_at and updated_at are one time", o,
		"POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour run <1> & \"one\"","description":"The first run: <b>all</b> cases"}`)).capture("r1", "/id")
	tr.step("read R1 back: the database's times", o, "GET /api/v1/test-runs/{id}", at("id", "{{r1}}"))
	tr.step("the viewer reads R1", viewer, "GET /api/v1/test-runs/{id}", at("id", "{{r1}}"))
	tr.step("the outsider reads R1", outsider, "GET /api/v1/test-runs/{id}", at("id", "{{r1}}"))
	tr.step("read a run that does not exist", o, "GET /api/v1/test-runs/{id}", at("id", "{{phantom}}"))
	tr.step("read a run by an id that is not a UUID", o, "GET /api/v1/test-runs/{id}", at("id", "not-a-uuid"),
		note("the lookup's driver error is answered as not found, like an id no run has"))
	tr.step("P's runs, newest first", o, "GET /api/v1/projects/{id}/test-runs", at("id", "{{p}}"))
	tr.step("the outsider lists P's runs", outsider, "GET /api/v1/projects/{id}/test-runs", at("id", "{{p}}"))

	// Results: the refusals in the order the handler and the service meet
	// them, then the upsert.
	tr.step("R1's results before any: null (Q14)", o, "GET /api/v1/test-runs/{id}/results", at("id", "{{r1}}"))
	tr.step("record a result with a malformed body", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`nope`))
	tr.step("record a result whose status is not one of pass, fail, blocked, not-run", o,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_auto}}","status":"maybe"}`))
	tr.step("record a result for the heading, which is not a test case", o, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{r1}}"), jsonBody(`{"test_case_id":"{{heading}}","status":"pass"}`))
	tr.step("record a result for a test case id no artifact has: 404", o,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"), jsonBody(`{"test_case_id":"{{phantom}}","status":"pass"}`),
		note("the artifact repository answers a missing row with artifacts.ErrNotFound, which the handler answers "+
			"404 (fixed under R7: it answered an error of its own, so the 404 branch never matched and this was 500, "+
			"the deterministic neighbour of quirk Q2)"))
	tr.step("the viewer records a result", viewer, "POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_auto}}","status":"pass"}`))
	tr.step("record a result in a run that does not exist", o, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{phantom}}"), jsonBody(`{"test_case_id":"{{tc_auto}}","status":"pass"}`))
	tr.step("record a pass for the automated case, with evidence: testrun.recorded", o,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_auto}}","status":"pass","notes":"Answered in 1.2 s <p95>","evidence":["figure-1"]}`),
		note("evidence ids are stored as sent; nothing checks them")).capture("res_auto", "/id")
	tr.step("record a fail for the manual case", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_manual}}","status":"fail","notes":"Wording unclear"}`)).capture("res_manual", "/id")
	tr.step("re-record the automated case with no evidence field: a result of its own beside the first, which "+
		"carries the current one's evidence", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_auto}}","status":"blocked","notes":"Rig offline"}`),
		note("fixed under R7 (OpenV REQ-13): the upsert wrote over the first result, keeping its id and "+
			"created_at")).capture("res_auto_blocked", "/id")
	tr.step("re-record it with an empty evidence list, which clears it", o, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{r1}}"), jsonBody(`{"test_case_id":"{{tc_auto}}","status":"pass","notes":"Answered in 1.1 s","evidence":[]}`)).
		capture("res_auto_pass", "/id")
	tr.step("record Q's test case in a run of P: 404, as a test case no artifact has", o,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_q}}","status":"not-run","notes":"`+long+`"}`),
		note("fixed under R7 (#379 bug 5): it was accepted, since nothing tied the case to the run's project; a "+
			"test case outside it is answered as one no row has, before its type is read"))
	tr.setup("the rig case's result, whose long notes make every list of R1's results long enough to be compressed", o,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_rig}}","status":"not-run","notes":"`+long+`"}`)).capture("res_rig", "/id")
	tr.step("R1's results, the latest recorded first: compressed, and then sent with no Content-Type (Q1)", o,
		"GET /api/v1/test-runs/{id}/results", at("id", "{{r1}}"))
	tr.step("the viewer reads R1's results", viewer, "GET /api/v1/test-runs/{id}/results", at("id", "{{r1}}"))
	tr.step("the automated case's notes: an automatic note per result recorded, newest first", o, "GET /api/v1/chatter",
		query("artifact_id={{tc_auto}}"), note("an S5a route, read to show what recording a result writes beside it"))

	// A workspace runner key: the path the MCP tools take (create_test_run,
	// record_test_result, close_test_run). No person is behind it.
	key := tr.setup("a workspace runner key", o, "POST /api/v1/orgs/{id}/worker-keys", at("id", "{{owner.workspace}}"),
		jsonBody(`{"name":"Tour runner"}`), expect(201)).value("/key")
	worker := tr.bearerActor("worker", key, "a workspace runner key of the owner's workspace, as the MCP tools and a "+
		"runner send it: no person behind it")
	tr.step("the runner key creates a run: no created_by", worker, "POST /api/v1/projects/{id}/test-runs",
		at("id", "{{p}}"), jsonBody(`{"name":"Runner run","description":"`+long+`"}`),
		note("its description makes every list of P's runs from here on long enough to be compressed")).
		capture("r_worker", "/id")
	tr.step("the runner key records a result: no executed_by", worker, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{r_worker}}"), jsonBody(`{"test_case_id":"{{tc_auto}}","status":"pass","notes":"CI job 42"}`)).
		capture("res_worker", "/id")
	tr.step("the runner key completes its run", worker, "PUT /api/v1/test-runs/{id}", at("id", "{{r_worker}}"),
		jsonBody(`{"status":"completed"}`))
	tr.step("the runner key reads its run: compressed, and then sent with no Content-Type (Q1)", worker,
		"GET /api/v1/test-runs/{id}", at("id", "{{r_worker}}"))

	// An agent run to execute R1: the launch's refusals in the handler's
	// order (lookup, guard, the run's status, decode, the slug, the agent,
	// the cases), then the launch.
	tr.step("launch an agent on a run that does not exist", o, "POST /api/v1/test-runs/{id}/agent-run",
		at("id", "{{phantom}}"), jsonBody(`{"agent_slug":"vv-engineer"}`))
	tr.step("the viewer launches an agent on R1", viewer, "POST /api/v1/test-runs/{id}/agent-run", at("id", "{{r1}}"),
		jsonBody(`{"agent_slug":"vv-engineer"}`))
	tr.step("launch an agent with a malformed body", o, "POST /api/v1/test-runs/{id}/agent-run", at("id", "{{r1}}"),
		jsonBody(`[`))
	tr.step("launch an agent with no agent_slug", o, "POST /api/v1/test-runs/{id}/agent-run", at("id", "{{r1}}"),
		jsonBody(`{"agent_slug":" "}`))
	tr.step("launch an agent the workspace does not have", o, "POST /api/v1/test-runs/{id}/agent-run",
		at("id", "{{r1}}"), jsonBody(`{"agent_slug":"no-such-agent"}`))
	tr.step("launch an agent on the manual and physical cases only", o, "POST /api/v1/test-runs/{id}/agent-run",
		at("id", "{{r1}}"), jsonBody(`{"agent_slug":"vv-engineer","test_case_ids":["{{tc_manual}}","{{tc_rig}}"]}`))
	launched := tr.step("launch the V&V engineer on every case of the project: the automated one runs, the others are skipped", o,
		"POST /api/v1/test-runs/{id}/agent-run", at("id", "{{r1}}"), jsonBody(`{"agent_slug":"vv-engineer"}`),
		note("an empty test_case_ids means every test case of the project, not only the run's; the run stays queued "+
			"(HOSTED_RUNNERS=off and no runner has claimed it yet), and launching it creates a tracking card on the "+
			"board"))
	launched.capture("agent_run", "/run/id")
	launched.capture("vv_engineer", "/run/agent_id")
	launched.capture("tracking_card", "/run/work_item_id")

	// The runner key claims the queued run (setup: S5d pins the worker
	// wire) and hands its token to the agent, as a runner does.
	claim := tr.setup("the runner key claims the queued agent run", worker, "POST /api/v1/agent-runs/claim",
		jsonBody(`{"worker_id":"tour-runner","providers":["claude-code"]}`), expect(200))
	if claimed := claim.value("/run/id"); claimed != tr.id("agent_run") {
		tr.t.Fatalf("the runner key claimed run %s, not the agent run the area launched (%s)", claimed, tr.id("agent_run"))
	}
	agent := tr.bearerActor("agent", claim.value("/run_token"), "the token of the agent run launched on R1, "+
		"once the runner key claimed it: scoped to P, as editor")
	// Any 2xx: the steps above pin the create's status, so a change to it
	// shows in the golden's diff rather than stopping the area here.
	tr.setup("Q's run", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{q}}"), jsonBody(`{"name":"Other run"}`)).
		capture("r_q", "/id")
	tr.step("the agent records a pass for the automated case: executed_by_agent_run_id", agent,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_auto}}","status":"pass","notes":"Ran the timing script: 1.0 s"}`)).
		capture("res_agent", "/id")
	tr.step("the agent records the manual case: refused, a person must verify it", agent,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_manual}}","status":"pass"}`))
	tr.step("the agent records a result in a run of another project", agent, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{r_q}}"), jsonBody(`{"test_case_id":"{{tc_q}}","status":"pass"}`))
	tr.step("the agent reads R1's results", agent, "GET /api/v1/test-runs/{id}/results", at("id", "{{r1}}"))

	// Status transitions: only in-progress runs move, and only to
	// completed or aborted.
	tr.step("set R1 back to in-progress", o, "PUT /api/v1/test-runs/{id}", at("id", "{{r1}}"),
		jsonBody(`{"status":"in-progress"}`))
	tr.step("update R1 with a malformed body", o, "PUT /api/v1/test-runs/{id}", at("id", "{{r1}}"), jsonBody(`{`))
	tr.step("the viewer completes R1", viewer, "PUT /api/v1/test-runs/{id}", at("id", "{{r1}}"),
		jsonBody(`{"status":"completed"}`))
	tr.step("complete a run that does not exist", o, "PUT /api/v1/test-runs/{id}", at("id", "{{phantom}}"),
		jsonBody(`{"status":"completed"}`))
	tr.step("complete R1: completed_at", o, "PUT /api/v1/test-runs/{id}", at("id", "{{r1}}"),
		jsonBody(`{"status":"completed"}`))
	tr.step("complete R1 again: not a transition", o, "PUT /api/v1/test-runs/{id}", at("id", "{{r1}}"),
		jsonBody(`{"status":"completed"}`))
	tr.step("abort the run on P's baseline", o, "PUT /api/v1/test-runs/{id}",
		at("id", "{{run_baselined}}"), jsonBody(`{"status":"aborted"}`))
	tr.step("launch an agent on the completed R1: 409, as a result there", o, "POST /api/v1/test-runs/{id}/agent-run",
		at("id", "{{r1}}"), jsonBody(`{"agent_slug":"vv-engineer"}`),
		note("fixed under R7 (#379 bug 50): it answered 400, in the words of the next step's 409"))
	tr.step("record the physical case in the completed R1: 409, a closed run takes no result", o,
		"POST /api/v1/test-runs/{id}/results", at("id", "{{r1}}"),
		jsonBody(`{"test_case_id":"{{tc_rig}}","status":"blocked","notes":"Rig booked"}`),
		note("fixed under R7 (#379 bug 5): it was accepted, since nothing checked the run's status"))
	tr.step("record a result in the aborted run: 409 too", o, "POST /api/v1/test-runs/{id}/results",
		at("id", "{{run_baselined}}"), jsonBody(`{"test_case_id":"{{tc_auto}}","status":"fail"}`))
	tr.step("P's runs: completed, aborted and the runner's", o, "GET /api/v1/projects/{id}/test-runs", at("id", "{{p}}"))

	// Deletes: a run that holds results is kept, its results with it, and
	// one that holds none is deleted.
	tr.step("the viewer deletes R1", viewer, "DELETE /api/v1/test-runs/{id}", at("id", "{{r1}}"))
	tr.step("delete R1, which holds results: 409, a run with results is closed, not deleted", o,
		"DELETE /api/v1/test-runs/{id}", at("id", "{{r1}}"),
		note("fixed under R7 (OpenV REQ-13): the run was deleted, and its results with it"))
	tr.step("delete the aborted run, which holds no result: deleted", o, "DELETE /api/v1/test-runs/{id}",
		at("id", "{{run_baselined}}"))
	tr.step("R1's results once its delete is refused: kept", o, "GET /api/v1/test-runs/{id}/results", at("id", "{{r1}}"))
	tr.step("P's runs after the deletes: R1 kept, the aborted run gone", o, "GET /api/v1/projects/{id}/test-runs",
		at("id", "{{p}}"))

	// Accounts from other workspaces: the outsider is refused a run's
	// results once the run is found; an editor of P, whose own workspace is
	// seeded with a V&V engineer of its own, launches P's workspace's, and
	// the run it queues is P's workspace's too (LaunchTestRunAgent resolves
	// the agent in the project's workspace, not the caller's).
	tr.step("the outsider reads the runner's run's results: the run found, then the guard", outsider,
		"GET /api/v1/test-runs/{id}/results", at("id", "{{r_worker}}"))
	editor := tr.register("editor", "Tour Editor", "an editor of P, from a workspace of its own, with a V&V "+
		"engineer of its own: its launch uses P's workspace's")
	tr.setup("the editor joins P as an editor", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-editor@example.com","role":"editor"}`), expect(201))
	tr.setup("run R3, in progress, for the editor's launch", o, "POST /api/v1/projects/{id}/test-runs",
		at("id", "{{p}}"), jsonBody(`{"name":"Editor's run"}`)).capture("r3", "/id")
	tr.step("an editor of P from another workspace launches the V&V engineer on R3: P's workspace's agent, and "+
		"the run queued in P's workspace", editor, "POST /api/v1/test-runs/{id}/agent-run", at("id", "{{r3}}"),
		jsonBody(`{"agent_slug":"vv-engineer"}`))

	// A run's history: every result recorded in it, the ones later results
	// superseded included (OpenV REQ-13).
	tr.step("R1's history: every result recorded, the superseded ones included, the latest recorded first", o,
		"GET /api/v1/test-runs/{id}/results", at("id", "{{r1}}"), query("history=true"))

	// A run's baseline is one of its own project's (#379's decision on REQ-6).
	tr.setup("Q's baseline", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{q}}"),
		jsonBody(`{"name":"Tour other runs baseline"}`), expect(201)).capture("bq", "/id")
	tr.step("create a run in P on Q's baseline: 404, as a baseline no row has", o,
		"POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Borrowed baseline","baseline_id":"{{bq}}"}`),
		note("fixed under R7 (#379's decision on REQ-6): it was accepted, the run naming a baseline of "+
			"another project"))

	// A baseline a run names can still be deleted (REQ-5); the run keeps its
	// id as history, and every read marks it (#379's question 23, REQ-13).
	tr.setup("a run on P's baseline, which the owner then deletes", o, "POST /api/v1/projects/{id}/test-runs",
		at("id", "{{p}}"), jsonBody(`{"name":"Run on a deleted baseline","baseline_id":"{{bp}}"}`), expect(201)).
		capture("run_deleted_baseline", "/id")
	tr.setup("the owner deletes P's baseline: the delete goes ahead", o, "DELETE /api/v1/baselines/{id}",
		at("id", "{{bp}}"), expect(204))
	tr.step("read the run back: its baseline_id kept, marked baseline_deleted", o, "GET /api/v1/test-runs/{id}",
		at("id", "{{run_deleted_baseline}}"),
		note("fixed under R7 (#379's question 23): the run kept the id with nothing to say the baseline was gone, "+
			"so the V&V dashboard showed the bare id"))
	tr.step("P's runs: the run on the deleted baseline marked, the others not", o,
		"GET /api/v1/projects/{id}/test-runs", at("id", "{{p}}"))
	tr.step("create a run on the deleted baseline: 404, as a baseline no row has", o,
		"POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Deleted baseline","baseline_id":"{{bp}}"}`))
}
