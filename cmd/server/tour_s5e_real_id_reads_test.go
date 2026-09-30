//go:build unix

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestTourS5eRealIdReads is part (2) of S5e, the real-id pass of the
// authorization matrix (refactor plan §6.4 S5e; invariant I3; OpenV REQ-143
// and REQ-18), on the framework of tour_matrix_test.go with the columns of
// tour_matrix_cast_test.go. Its golden is testdata/tour/s5e/real_id_reads.json;
// part (1), the phantom matrix, and part (3), the over-plan pass, are areas
// of their own.
//
// The owner makes, as setup, one fixture of every kind a GET route's path
// names, in W and P (realIdReadsFixtures), and the matrix then sends, with
// the eight columns of part (1):
//   - "real ids, GET": every GET route of routes.txt with a path variable (98
//     today, the eight public token and open-source ones included, whose
//     answers do not depend on the column), in its order and nothing else
//     (expectRoutes), so a GET route added later fails the area until it names
//     its fixture (realIdReadsIDs) and the golden is regenerated; a JSON
//     list's length is recorded, so a column that reads another's rows shows;
//   - "real ids in the query, GET": the list reads a query scopes;
//   - "real ids, body {": writes to real ids whose handlers were read to check
//     them guard (and look up), then decode, or decode first, and write nothing
//     before the decode; with the body { each column shows the first check that
//     refuses it, and a column that passes every guard gets the decode's 400,
//     so the viewer, editor and owner boundary of each guard kind shows;
//   - "real ids, lists with no query": the list reads that take no query, and
//     search's hits for R1 with no project, counted, after the owner made, as
//     setup, project Q in W, which only it belongs to, and a requirement R1 of
//     Q's own (realIdReadsUnscopedLists): what each column's own scope lets it
//     list, so a membership filter dropped (ListProjects', ListDomainEvents',
//     ListAgentRuns' or search's) shows as the viewer's and editor's count;
//   - "real ids, well-formed creates": the four creates, since they write;
//   - "a proposal-mode run's token": last, the writes a run could set work
//     going with, with the token of a running proposal-mode run in P
//     (realIdReadsReviewRunLaunches) standing in the run column: the draft of
//     test cases, launches of an agent and of a crew in P, a test run's agent
//     run, a retry, an automation's run-now, the assistant's chat message,
//     kickoff and nudge, an interview in P and an invite to <interview>, which
//     arm the interviewer's runs, a project and a project from the template.
//
// After each of the first four sections, W, the outsider's and the admin's
// workspaces must have published nothing (readSectionEvents fails the area
// otherwise); the creates publish nothing either, and the last section lists
// the work item each launch put on P's board.
//
// What the golden pins, per route and identity (I3), among others:
//   - the outsider: every real id of W and P answered exactly as part (1)'s
//     phantom, the same 404 and the same message (existence hiding, I3; no
//     database's TestTourExistenceHiding compares the two goldens): 404
//     "project not found" on every project route, "workspace not found" on
//     W's, and each child resource's own not-found ("baseline not found",
//     "team not found"...); 404 on /agents/{slug} and /provider-logins/{id},
//     which are looked up in the workspace the caller acts in; and the
//     decode's 400 on the writes that decode first (PUT /artifacts/{id},
//     /links/{id}, the status and the restore), as for a phantom; only a
//     public link's token opens for it, and the owner's picture, which W's
//     members and the platform admin read, answers it the 404 of an account
//     with no picture (fixed under R7, #379's decision 14);
//   - the worker key: every read of P and its children, as a workspace-wide
//     editor (REQ-42), and, as the editor is, 403 "you do not have access to
//     this project" on P's owner routes (its share links, and in "body {" its
//     owner writes); 401 on the workspace, automation, crew, agent, run and
//     transcript routes, which take a session; alone 200 on
//     /provider-logins/{id}/full;
//   - the run token: P's reads as an editor of P, and 403 "agent runs act at
//     most as a project editor" on P's owner routes, where a run outside P is
//     told the project is not there (404); 403 "not your delegated run" on
//     delegate/{id} with its own id, where every other column needs a run
//     token;
//   - viewer, editor, owner: the workspace-admin reads (billing, hosted runner,
//     invitations, runner pool, worker keys) 403 to the plain members, and to
//     the owner 200, or billing's 404 billing_unavailable; P's share links are
//     the owner's; in "body {", the viewer is refused every editor write and
//     passes POST /work-items/{id}/comments (a viewer may comment), the editor
//     is refused P's owner writes (members, share links, repository
//     connections, team access, a member's role) and every workspace-admin
//     write, including a workspace-wide crew's (so a project guard's role, a
//     requireProjectRole editor lowered to viewer on PUT /projects/{id}, say,
//     shows here, which part (1) cannot show on a phantom);
//   - the platform admin passes every guard, and a list a project scopes
//     (/events and /agent-runs of P) holds P's rows, filtered by P's
//     workspace rather than the one the admin acts in (fixed under R7);
//   - the lists with no query: P's viewer and editor list P alone, the owner
//     (W's admin) P and Q, the worker key both, having no user to filter by,
//     and the run token P alone, the one project it acts in; the plain
//     members read P's events alone (12) and none of the runs, which they did
//     not launch, where the owner reads W's 15 events and 2 runs; search
//     finds R1 in P for them, in P and Q for the owner, nothing for the
//     outsider and the admin in their own workspaces; automations, crews,
//     agents and members are the workspace's, whoever reads them;
//   - the proposal-mode run: refused the draft of test cases ("proposal-mode
//     agent runs cannot draft test cases"), where a direct run reaches the
//     decode, and every other route that sets a run going ("proposal-mode
//     agent runs cannot launch agent runs", before any lookup: REQ-21,
//     REQ-75), the retry and run-now among them, which answer a direct run's
//     token the 401 of a request with no person, and an interview and its
//     invite, which arm the interviewer's runs, where a direct run reaches
//     the decode; a project and the templated project are refused it as a
//     direct run's token is;
//   - the creates: every signed-in column creates a project, a project from
//     the seeded template, an import and a workspace (201; the workspace's
//     answer has no Content-Type, Q1), in the workspace it acts in; the run
//     token creates none, 403 "agent runs cannot create projects" on the
//     three project creates (a run acts only inside its own project, REQ-42)
//     and 401 on the workspace's, which takes a session; W's worker key
//     creates none either, 403 "runner keys cannot create projects" on the
//     three (a key has no person to own what it would make), and 401 on the
//     workspace's.
func TestTourS5eRealIdReads(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5e",
		key:   "real_id_reads",
		about: "The real-id authorization matrix: every GET route of routes.txt with a path variable, filled with the " +
			"real ids of fixtures the owner made in W and P, sent to anonymous, a viewer, an editor and the owner of " +
			"P, an outsider, W's worker key, the token of a run in P and the platform admin; then the list reads " +
			"scoped by a real id in the query, writes to real ids with the body {, the list reads that take no " +
			"query, the four creates with a well-formed body, and the launches and creates of a proposal-mode " +
			"run's token.",
		run:      realIdReadsTour,
		env:      tourMatrixEnv(),
		files:    tourMatrixFiles(),
		accounts: tourMatrixAccounts(),
	})
}

// realIdReadsSends are the area's conventions, beyond the framework's.
var realIdReadsSends = []string{
	"section \"real ids, GET\": every GET route of routes.txt with a path variable, in its order, the variable " +
		"filled with a real id: <w> for /orgs, <p> for /projects and /public/open-source/projects, <run> for " +
		"/agent-runs (delegate/{id} too), tour-matrix for /agents, <a> for /artifacts, <attachment>, <automation>, " +
		"<b1> for /baselines, <crew> for /crews and /teams, <bundle> for /evidence-bundles, <file> for " +
		"/evidence-files, <guided> for /guided-sessions, <interview.session> for /interview-sessions, <interview> " +
		"for /interviews, <link> for /links, <login> for /provider-logins, <invite.token> for /public/interviews, " +
		"<share.token> for /public/share, <result> for /test-results, <test.run> for /test-runs, <owner> for " +
		"/users and <card> for /work-items; /baselines/{id}/diff takes ?against=live (against <b1> itself is a " +
		"400) and /projects/{id}/impact ?artifact=<a> (the row shows them); an answer that is a JSON array or null " +
		"is counted",
	"the fixtures, made as setup by the owner in W and P before the matrix: requirement A (<a>), user need N (<n>) " +
		"and test case TC (<tc>); the link A derives-from N (<link>); a PNG attachment on A (<attachment>); baseline " +
		"B1 (<b1>); a work item (<card>); test run <test.run> with TC's pass (<result>); evidence bundle <bundle> " +
		"with a CSV (<file>) that <result> cites; interview <interview> and its invite (<invite.token>); guided " +
		"session <guided>; a manual automation of tour-matrix pinned to P (<automation>); a workspace-wide crew " +
		"(<crew>); a sign-in of claude-code on W's shared workers (<login>); a public share link of P " +
		"(<share.token>); W's logo and the owner's picture; then the participant's first message on the invite, " +
		"from an address of its own, which opens <interview.session> and queues an interviewer run",
	"section \"real ids in the query, GET\": the list reads scoped by their query, with a real id there (the row " +
		"shows it); an answer that is a JSON array or null is counted",
	"section \"real ids, body {\": writes to real ids whose handlers decode before they write, with " +
		"Content-Type: application/json and the one-byte body { (it decodes into nothing, so each column shows " +
		"which of guard, lookup and decode refuses it first, and nothing is written); a second path id is the " +
		"viewer's (<viewer>)",
	"section \"real ids, lists with no query\": the list reads that take no query, in routes.txt's order, then " +
		"GET /api/v1/search ?q=R1 with no project; an answer that is a JSON array or null is counted, and the " +
		"workspaces' at /orgs and search's at /hits; before it the owner made, as setup, project Q in W (<q>), " +
		"which only the owner belongs to, and a requirement titled R1 of Q (<q.r1>)",
	"section \"real ids, well-formed creates\": the four creates that take no path id or a template's, with a " +
		"well-formed body (the row shows it), after the sections that write nothing, since each writes: a project, " +
		"a project from the seeded template (<template>), an import and a workspace, in the workspace each column " +
		"acts in",
	"section \"a proposal-mode run's token\", last: the run column is sent by <review.run.token>, the token of " +
		"<review.run>, a run of the proposal-mode agent tour-matrix-review in P, launched by the owner, claimed by " +
		"W's worker key and started, as setup after the creates, with a workspace-wide crew whose entry node is " +
		"tour-matrix (<launch.crew>); the draft of test cases, the test run's agent run, the assistant's chat " +
		"message, kickoff and nudge (on <guided>), an interview in P and an invite to <interview> carry the body {, " +
		"the retry (of <run>) and run-now (of <automation>) no body (either row shows none), the launches and " +
		"creates a well-formed body (the row shows it)",
	"after each section, the events of each workspace a signed-in column acts in are read and listed with the " +
		"section (W, <outsider.workspace>, <admin.workspace>); in the first four sections none of them may publish " +
		"any, or the area fails",
}

// realIdReadsTour builds the cast and the fixtures, then sends the matrix.
func realIdReadsTour(tr *tour) {
	cast := tr.matrixCast()
	realIdReadsFixtures(tr)
	quiet := []*tourActor{cast.owner, cast.outsider, cast.admin}
	m := tr.matrix(realIdReadsSends, cast.columns()...)

	m.section("real ids, GET", "Every GET route of internal/api/testdata/routes.txt with a path variable, in its "+
		"order, filled with the real id of a fixture in W or P.").countLists()
	var gets []string
	for _, route := range readRouteList(tr.t) {
		if method, tmpl, _ := strings.Cut(route, " "); method == http.MethodGet && strings.Contains(tmpl, "{") {
			gets = append(gets, route)
			m.row(route, realIdReadsPath(tr, route)...)
		}
	}
	m.expectRoutes(gets)
	m.readSectionEvents(quiet...)

	m.section("real ids in the query, GET", "The list reads a query scopes, with the real id of P, A or W in it.").
		countLists()
	for _, r := range realIdReadsQueries {
		if r.count != "" {
			m.rowCounting(r.count, r.route, query(r.query))
			continue
		}
		m.row(r.route, query(r.query))
	}
	m.readSectionEvents(quiet...)

	m.section("real ids, body {", "Writes to real ids whose handlers decode before they write, with the body {: "+
		"the guard's refusal, a lookup's, or the decode's 400 once a column passes them.")
	var writes []string
	for _, route := range readRouteList(tr.t) {
		if second, ok := realIdReadsWrites[route]; ok {
			writes = append(writes, route)
			opts := realIdReadsPath(tr, route)
			if second != "" {
				opts = append(opts, at(second, "{{viewer}}"))
			}
			m.row(route, append(opts, truncatedBody())...)
		}
	}
	if len(writes) != len(realIdReadsWrites) {
		tr.t.Fatalf("realIdReadsWrites names %d routes, %d of them in routes.txt", len(realIdReadsWrites), len(writes))
	}
	m.expectRoutes(writes)
	m.readSectionEvents(quiet...)

	realIdReadsUnscopedLists(tr, m, quiet)

	m.section("real ids, well-formed creates", "The creates that take no project or workspace id in their path, "+
		"with a well-formed body: each column creates in the workspace it acts in.")
	m.row("POST /api/v1/projects", jsonBody(`{"name":"Tour Matrix create"}`))
	m.row("POST /api/v1/templates/{id}/projects", at("id", "{{template}}"),
		jsonBody(`{"name":"Tour Matrix from the template"}`))
	m.row("POST /api/v1/projects/import", jsonBody(realIdReadsImport))
	m.row("POST /api/v1/orgs", jsonBody(`{"name":"Tour Matrix create"}`))
	m.readSectionEvents()

	realIdReadsReviewRunLaunches(tr, m, cast)
}

// realIdReadsLists are the list reads that take no query, which the section
// "real ids, lists with no query" counts, in routes.txt's order; count is
// the pointer of a list an object wraps (the workspaces'), as
// realIdReadsQueries' is.
var realIdReadsLists = []struct{ route, count string }{
	{"GET /api/v1/agent-runs", ""},
	{"GET /api/v1/agents", ""},
	{"GET /api/v1/automations", ""},
	{"GET /api/v1/crews", ""},
	{"GET /api/v1/events", ""},
	{"GET /api/v1/orgs", "/orgs"},
	{"GET /api/v1/projects", ""},
	{"GET /api/v1/teams", ""},
	{"GET /api/v1/users", ""},
}

// realIdReadsUnscopedLists makes, as setup, project Q in W, which only the
// owner belongs to, with a requirement R1 of its own, and then counts the
// lists a caller reads with no query, and search's hits for R1 with no
// project: what each column's own scope lets it list (a membership filter
// dropped shows as the viewer's and editor's count of Q). Nothing written
// here is read by an earlier section, so their rows stay as they were.
func realIdReadsUnscopedLists(tr *tour, m *tourMatrix, quiet []*tourActor) {
	tr.t.Helper()
	o := tr.owner
	tr.setup("project Q, which only the owner belongs to", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour Matrix Q"}`), expect(http.StatusCreated)).capture("q", "/id")
	body, _ := json.Marshal(map[string]string{"project_id": tr.id("q"), "type": "requirement", "title": "R1 of Q",
		"body": "The system shall list Q to those who may read it."})
	tr.setup("requirement R1 of Q", o, "POST /api/v1/artifacts", rawBody("application/json", body)).
		capture("q.r1", "/id")
	m.leaveOutSetups()

	m.section("real ids, lists with no query", "The list reads that take no query, and search for R1 with no "+
		"project, after the owner made project Q, which only it belongs to, and R1 in Q.").countLists()
	var lists []string
	for _, r := range realIdReadsLists {
		lists = append(lists, r.route)
		if r.count != "" {
			m.rowCounting(r.count, r.route)
			continue
		}
		m.row(r.route)
	}
	m.expectRoutes(lists)
	m.rowCounting("/hits", "GET /api/v1/search", query("q=R1"))
	m.readSectionEvents(quiet...)
}

// realIdReadsReviewAgent is the proposal-mode agent whose run's token stands
// in the run column of the last section: its provider is one no other agent
// of the area has, so that the worker's claim takes its run and not the
// interviewer's, queued since the participant's first message.
func realIdReadsReviewAgent() string {
	b, _ := json.Marshal(map[string]any{
		"slug": "tour-matrix-review", "name": "Tour Matrix Review",
		"description": "The S5e matrix's proposal-mode agent.", "provider": "codex-cli",
		"allowed_tools": []string{"get_artifact"}, "write_mode": "proposal", "repo_access": false,
		"system_prompt": "You propose, a person decides.",
	})
	return string(b)
}

// realIdReadsReviewRunLaunches makes, as setup, a running run of a
// proposal-mode agent in P and a crew whose entry node is tour-matrix, then
// sends, with that run's token in the run column (rowAs), the writes a run
// could set work going with: the draft of test cases (which refuses a
// proposal-mode run in words of its own), a launch of tour-matrix in P, a
// launch of the crew in P, a test run's agent run, a retry of <run> (running,
// so 409 to a column that passes its guard), the run-now of <automation>, the
// assistant's chat message, kickoff and nudge on <guided>, an interview in P
// and an invite to <interview> (which arm the interviewer's runs, one for each
// message the invite's participant posts), a project and a project from the
// seeded template. The draft, the test run's agent run, the assistant's three,
// the interview and the invite carry the body {, since each checks its guard
// before it decodes; the retry and run-now no body; the others a well-formed
// body, since each decodes first for the columns it lets through (the creates
// refuse a run's token and a runner key before). Last, since the launches and
// creates write.
func realIdReadsReviewRunLaunches(tr *tour, m *tourMatrix, c *tourMatrixCast) {
	tr.t.Helper()
	o := tr.owner
	tr.setup("the proposal-mode agent tour-matrix-review", o, "POST /api/v1/agents", jsonBody(realIdReadsReviewAgent()))
	tr.queueRun("review.run", o, "tour-matrix-review", `{"project_id":"{{p}}","prompt":"Propose a requirement for P."}`)
	review := tr.setup("claim the run review.run", c.worker, "POST /api/v1/agent-runs/claim",
		claimBody("tour-matrix-review", "codex-cli"), expect(http.StatusOK)).claimed("review.run").
		runToken("review.run", "the token of a proposal-mode run of tour-matrix-review in P, launched by the owner, "+
			"claimed by W's worker key and started: the run column of the section \"a proposal-mode run's token\"")
	tr.setup("start the run review.run, as a runner does", c.worker, "POST /api/v1/agent-runs/{id}/start",
		at("id", "{{review.run}}"))
	tr.setup("a workspace-wide crew to launch", o, "POST /api/v1/crews", jsonBody(`{"name":"Tour Matrix launch"}`)).
		capture("launch.crew", "/id")
	tr.setup("tour-matrix as its node", o, "POST /api/v1/crews/{id}/nodes", at("id", "{{launch.crew}}"),
		jsonBody(`{"agent_id":"{{agent}}","label":"Matrix","department":"Tour"}`)).capture("launch.node", "/id")
	tr.setup("the node is the crew's entry", o, "PUT /api/v1/crews/{id}", at("id", "{{launch.crew}}"),
		jsonBody(`{"entry_node_id":"{{launch.node}}"}`))
	m.leaveOutSetups()

	m.section("a proposal-mode run's token", "The writes that set work going or create a project, with the token "+
		"of a running proposal-mode run in P in the run column: the draft of test cases and every other route that "+
		"sets a run going or arms one (an interview and its invite), which refuse it, and creates, which refuse "+
		"every run's token and runner key.")
	stand := map[string]*tourActor{c.run.name: review}
	m.rowAs("POST /api/v1/projects/{id}/draft-test-cases", stand, at("id", "{{p}}"), truncatedBody())
	m.rowAs("POST /api/v1/agents/{slug}/runs", stand, at("slug", "tour-matrix"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P again."}`))
	m.rowAs("POST /api/v1/crews/{id}/runs", stand, at("id", "{{launch.crew}}"),
		jsonBody(`{"project_id":"{{p}}","prompt":"Summarise P as a crew."}`))
	m.rowAs("POST /api/v1/test-runs/{id}/agent-run", stand, at("id", "{{test.run}}"), truncatedBody())
	m.rowAs("POST /api/v1/agent-runs/{id}/retry", stand, at("id", "{{run}}"))
	m.rowAs("POST /api/v1/automations/{id}/run-now", stand, at("id", "{{automation}}"))
	for _, route := range []string{"POST /api/v1/guided-sessions/{id}/messages",
		"POST /api/v1/guided-sessions/{id}/chat/kickoff", "POST /api/v1/guided-sessions/{id}/chat/nudge"} {
		m.rowAs(route, stand, at("id", "{{guided}}"), truncatedBody())
	}
	m.rowAs("POST /api/v1/projects/{id}/interviews", stand, at("id", "{{p}}"), truncatedBody())
	m.rowAs("POST /api/v1/interviews/{id}/invites", stand, at("id", "{{interview}}"), truncatedBody())
	m.rowAs("POST /api/v1/projects", stand, jsonBody(`{"name":"Tour Matrix create in review"}`))
	m.rowAs("POST /api/v1/templates/{id}/projects", stand, at("id", "{{template}}"),
		jsonBody(`{"name":"Tour Matrix from the template in review"}`))
	m.readSectionEvents()
}

// realIdReadsImport is the JSON the import row sends: one requirement.
const realIdReadsImport = `{"project_name":"Tour Matrix import","artifacts":[{"id":"r","type":"requirement",` +
	`"title":"Imported","body":"The system shall import."}]}`

// realIdReadsIDs maps the first path segment after /api/v1/ to the fixture
// whose id fills the route's variable.
var realIdReadsIDs = map[string]string{
	"agent-runs":         "{{run}}",
	"agents":             "tour-matrix",
	"artifacts":          "{{a}}",
	"attachments":        "{{attachment}}",
	"automations":        "{{automation}}",
	"baselines":          "{{b1}}",
	"crews":              "{{crew}}",
	"teams":              "{{crew}}",
	"evidence-bundles":   "{{bundle}}",
	"evidence-files":     "{{file}}",
	"guided-sessions":    "{{guided}}",
	"interview-sessions": "{{interview.session}}",
	"interviews":         "{{interview}}",
	"links":              "{{link}}",
	"orgs":               "{{w}}",
	"projects":           "{{p}}",
	"provider-logins":    "{{login}}",
	"test-results":       "{{result}}",
	"test-runs":          "{{test.run}}",
	"users":              "{{owner}}",
	"work-items":         "{{card}}",
	"public/interviews":  "{{invite.token}}",
	"public/share":       "{{share.token}}",
	"public/open-source": "{{p}}",
}

// realIdReadsQuery is the query some GET routes need to get past their own
// 400.
var realIdReadsQuery = map[string]string{
	"GET /api/v1/baselines/{id}/diff":  "against=live",
	"GET /api/v1/projects/{id}/impact": "artifact={{a}}",
}

// realIdReadsPath fills a route's first path variable with the real id of
// its resource (realIdReadsIDs), and adds the query the route needs; it
// stops the area for a resource with no fixture, so that a GET route added
// to routes.txt names one before its row is sent.
func realIdReadsPath(tr *tour, route string) []tourOpt {
	tr.t.Helper()
	_, tmpl, _ := strings.Cut(route, " ")
	rest := strings.TrimPrefix(tmpl, "/api/v1/")
	seg, _, _ := strings.Cut(rest, "/")
	if seg == "public" {
		parts := strings.SplitN(rest, "/", 3)
		seg = parts[0] + "/" + parts[1]
	}
	id, ok := realIdReadsIDs[seg]
	vars := routeVarRE.FindAllString(tmpl, -1)
	if !ok || len(vars) == 0 {
		tr.t.Fatalf("realIdReadsPath: %s has no fixture for /api/v1/%s: add one to realIdReadsFixtures and "+
			"realIdReadsIDs", route, seg)
	}
	opts := []tourOpt{at(vars[0][1:len(vars[0])-1], id)}
	if q := realIdReadsQuery[route]; q != "" {
		opts = append(opts, query(q))
	}
	return opts
}

// realIdReadsQueries are the list reads a query scopes; count is the
// pointer of a list an object wraps, counted in each cell (search's hits,
// which the columns' own workspaces scope, so that the outsider's and the
// admin's show no hit of P).
var realIdReadsQueries = []struct{ route, query, count string }{
	{"GET /api/v1/artifacts", "project_id={{p}}", ""},
	{"GET /api/v1/links", "project_id={{p}}", ""},
	{"GET /api/v1/chatter", "artifact_id={{a}}", ""},
	{"GET /api/v1/guided-sessions", "project_id={{p}}", ""},
	{"GET /api/v1/attribute-definitions", "project_id={{p}}", ""},
	{"GET /api/v1/attribute-definitions", "org_id={{w}}", ""},
	{"GET /api/v1/meta/attribute-definitions", "project_id={{p}}", ""},
	{"GET /api/v1/agent-runs", "project_id={{p}}", ""},
	{"GET /api/v1/events", "project_id={{p}}", ""},
	{"GET /api/v1/proposals", "project_id={{p}}", ""},
	{"GET /api/v1/search", "q=R1&project_id={{p}}", "/hits"},
	{"GET /api/v1/automations", "project_id={{p}}", ""},
	{"GET /api/v1/crews", "project_id={{p}}", ""},
}

// realIdReadsWrites are the writes sent to real ids with the body {, each
// read first: its handler checks its guard and lookups and then decodes, or
// decodes first, and writes nothing before the decode. The value names a
// second path variable, filled with the viewer's id ("" for none). No
// DELETE, no write without a body (a start, cancel, retry, commit, kickoff,
// revoke, run-now...), nothing of the worker's lifecycle (S5d's), and no
// PUT /agents/{slug}/raw, whose body is the agent's file.
var realIdReadsWrites = map[string]string{
	// The 21 the scout checked.
	"POST /api/v1/projects/{id}/members":     "",
	"POST /api/v1/projects/{id}/share-links": "",
	"POST /api/v1/projects/{id}/work-items":  "",
	"POST /api/v1/projects/{id}/baselines":   "",
	"PUT /api/v1/projects/{id}":              "",
	"PUT /api/v1/projects/{id}/team-access":  "",
	"PUT /api/v1/projects/{id}/profile":      "",
	"PUT /api/v1/projects/{id}/parties":      "",
	"PUT /api/v1/work-items/{id}":            "",
	"POST /api/v1/work-items/{id}/comments":  "",
	"PUT /api/v1/orgs/{id}":                  "",
	"POST /api/v1/orgs/{id}/worker-keys":     "",
	"PUT /api/v1/test-runs/{id}":             "",
	"POST /api/v1/test-runs/{id}/results":    "",
	"PUT /api/v1/evidence-bundles/{id}":      "",
	"PUT /api/v1/interviews/{id}/persona":    "",
	"PUT /api/v1/attachments/{id}":           "",
	"PUT /api/v1/automations/{id}":           "",
	"PUT /api/v1/crews/{id}":                 "",
	"PUT /api/v1/artifacts/{id}":             "",
	"PUT /api/v1/links/{id}":                 "",
	// Read for this area: the decode first, or a guard (and a lookup) and then
	// the decode.
	"PUT /api/v1/artifacts/{id}/status":           "", // ChangeArtifactStatus: the decode first
	"POST /api/v1/artifacts/{id}/restore":         "", // RestoreArtifactVersion: the decode first
	"PUT /api/v1/projects/{id}/quality-rules":     "", // editor
	"PUT /api/v1/orgs/{id}/quality-rules":         "", // workspace admin
	"POST /api/v1/projects/{id}/test-runs":        "", // editor
	"POST /api/v1/projects/{id}/evidence-bundles": "", // editor
	"POST /api/v1/projects/{id}/interviews":       "", // editor
	"POST /api/v1/projects/{id}/repo-connections": "", // owner
	"PUT /api/v1/projects/{id}/members/{userId}":  "userId",
	"POST /api/v1/interviews/{id}/invites":        "", // the interview's lookup, editor
	"POST /api/v1/test-results/{id}/citations":    "", // the result's lookup, editor
	"PUT /api/v1/guided-sessions/{id}/step":       "", // the session's lookup, editor
	"POST /api/v1/guided-sessions/{id}/drafts":    "",
	"POST /api/v1/guided-sessions/{id}/messages":  "",
	"POST /api/v1/work-items/{id}/move":           "", // the card's lookup, editor
	"POST /api/v1/crews/{id}/nodes":               "", // teamWriteChecked
	"POST /api/v1/crews/{id}/edges":               "",
	"PUT /api/v1/agents/{slug}":                   "", // admin of the active workspace
	"POST /api/v1/agents/{slug}/runs":             "", // the agent's lookup, then the decode
	"POST /api/v1/provider-logins/{id}/code":      "", // userLoginWriteChecked
	"POST /api/v1/orgs/{id}/members":              "", // workspace admin
	"PUT /api/v1/orgs/{id}/members/{userId}":      "userId",
	"POST /api/v1/orgs/{id}/teams":                "", // workspace admin, the plan flag
	"POST /api/v1/orgs/{id}/invitations":          "", // workspace admin
}

// realIdReadsFixtures makes, as setup by the owner in W and P, the fixtures
// whose ids the matrix sends (the sends list them), then reads the seeded
// template's id.
func realIdReadsFixtures(tr *tour) {
	tr.t.Helper()
	o := tr.owner
	for _, a := range []struct{ name, typ, title, body string }{
		{"a", "requirement", "R1", "The system shall answer each read with the status its reader is owed."},
		{"n", "user-need", "N1", "A reader sees what its role allows."},
		{"tc", "test-case", "TC1", "Read every route as every identity."},
	} {
		body, _ := json.Marshal(map[string]string{"project_id": tr.id("p"), "type": a.typ, "title": a.title,
			"body": a.body})
		tr.setup(a.typ+" "+a.title, o, "POST /api/v1/artifacts", rawBody("application/json", body)).
			capture(a.name, "/id")
	}
	tr.setup("A derives from N", o, "POST /api/v1/links",
		jsonBody(`{"from_id":"{{a}}","to_id":"{{n}}","type":"derives-from"}`)).capture("link", "/id")
	tr.setup("a PNG figure on A", o, "POST /api/v1/attachments/upload", rawBody(multipartForm(
		[][2]string{{"artifact_id", tr.id("a")}},
		tourFormFile{field: "file", name: "fig.png", contentType: "image/png", data: []byte(tourPNG)}))).
		capture("attachment", "/id")
	tr.setup("baseline B1", o, "POST /api/v1/projects/{id}/baselines", at("id", "{{p}}"),
		jsonBody(`{"name":"B1"}`)).capture("b1", "/id")
	tr.setup("a work item", o, "POST /api/v1/projects/{id}/work-items", at("id", "{{p}}"),
		jsonBody(`{"title":"Tour Matrix card"}`)).capture("card", "/id")
	tr.setup("a test run", o, "POST /api/v1/projects/{id}/test-runs", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour Matrix run"}`)).capture("test.run", "/id")
	tr.setup("TC passes", o, "POST /api/v1/test-runs/{id}/results", at("id", "{{test.run}}"),
		jsonBody(`{"test_case_id":"{{tc}}","status":"pass"}`)).capture("result", "/id")
	tr.setup("an evidence bundle", o, "POST /api/v1/projects/{id}/evidence-bundles", at("id", "{{p}}"),
		jsonBody(`{"title":"Tour Matrix bundle"}`), expect(http.StatusCreated)).capture("bundle", "/id")
	tr.setup("a CSV in the bundle", o, "POST /api/v1/evidence-bundles/{id}/files", at("id", "{{bundle}}"),
		rawBody(multipartForm(nil, tourFormFile{field: "file", name: "levels.csv", contentType: "text/csv",
			data: []byte("condition,level\nquiet,1\n")}))).capture("file", "/id")
	tr.setup("the result cites the bundle", o, "POST /api/v1/test-results/{id}/citations", at("id", "{{result}}"),
		jsonBody(`{"bundle_id":"{{bundle}}"}`)).capture("citation", "/id")
	tr.setup("an interview", o, "POST /api/v1/projects/{id}/interviews", at("id", "{{p}}"),
		jsonBody(`{"name":"Tour Matrix interview","brief":"Ask what a reader needs."}`), expect(http.StatusCreated)).
		capture("interview", "/id")
	inv := tr.setup("an invite", o, "POST /api/v1/interviews/{id}/invites", at("id", "{{interview}}"),
		jsonBody(`{"invitee_label":"Dana"}`), expect(http.StatusCreated))
	inv.capture("invite", "/invite/id")
	inv.capture("invite.token", "/token")
	tr.setup("a guided session", o, "POST /api/v1/guided-sessions", jsonBody(`{"project_id":"{{p}}"}`)).
		capture("guided", "/id")
	tr.setup("a manual automation of tour-matrix in P", o, "POST /api/v1/automations",
		jsonBody(`{"name":"Tour Matrix manual","kind":"manual","agent_id":"{{agent}}","project_id":"{{p}}",`+
			`"prompt_template":"Summarise P."}`), expect(http.StatusCreated)).capture("automation", "/id")
	tr.setup("a workspace-wide crew", o, "POST /api/v1/crews", jsonBody(`{"name":"Tour Matrix crew"}`)).
		capture("crew", "/id")
	tr.setup("a sign-in of claude-code on W's shared workers", o, "POST /api/v1/provider-logins",
		jsonBody(`{"provider":"claude-code"}`)).capture("login", "/id")
	share := tr.setup("a public share link of P", o, "POST /api/v1/projects/{id}/share-links", at("id", "{{p}}"),
		jsonBody(`{"role":"public","label":"Tour Matrix"}`))
	share.capture("share.link", "/id")
	share.capture("share.token", "/token")
	tr.setup("W's logo", o, "POST /api/v1/orgs/{id}/logo", at("id", "{{w}}"), rawBody(multipartForm(nil,
		tourFormFile{field: "file", name: "logo.png", contentType: "image/png", data: []byte(tourPNG)})))
	tr.setup("the owner's picture", o, "POST /api/v1/me/avatar", rawBody(multipartForm(nil,
		tourFormFile{field: "file", name: "me.png", contentType: "image/png", data: []byte(tourPNG)})))
	tr.setup("the participant's first message: it opens the session and queues an interviewer run", tr.anon,
		"POST /api/v1/public/interviews/{token}/messages", at("token", "{{invite.token}}"),
		jsonBody(`{"participant_name":"Dana","content":"I need to know who may read what."}`), ownAddress(),
		expect(http.StatusOK)).capture("interview.session", "/session/id")
	tr.setup("the templates", o, "GET /api/v1/templates").
		captureWhere("template", "", "key", "guided-product-skeleton", "id")
}
