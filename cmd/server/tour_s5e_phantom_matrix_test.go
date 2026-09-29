//go:build unix

package main

import (
	"strings"
	"testing"
)

// TestTourS5ePhantomMatrix is part (1) of S5e, the phantom-id authorization
// matrix (refactor plan §6.4 S5e; invariant I3; quirks Q1, Q2, Q19; OpenV
// REQ-143 and REQ-18), on the framework of tour_matrix_test.go with the
// columns of tour_matrix_cast_test.go. Its golden is
// testdata/tour/s5e/phantom_matrix.json; parts (2), the real-id reads, and
// (3), the over-plan pass, are areas of their own.
//
// Section "phantom ids, body {" sends every route of routes.txt, in its
// order and nothing else (expectRoutes), so a route added later fails the
// area until its golden is regenerated: every path id is an id no row has,
// and every write the body {, which decodes into nothing, so the first check
// a handler makes answers and nothing is written. The columns are anonymous,
// P's viewer and editor (plain members of W), the owner (W's admin, P's
// owner), the outsider (acting in its own workspace), W's worker key, the
// token of a running run in P, and the platform admin. What the golden pins,
// per route and identity (I3), among others:
//   - existence hiding: a phantom project is 403 "you do not have access to
//     this project" to every signed-in user, the owner included, as a real
//     foreign project is; the worker key and the platform admin get 404
//     "project not found" (their guards look the project up first) and the
//     run token 403 "agent run is not scoped to this project" (no lookup). A
//     phantom workspace is 403 "you are not a member of this workspace", and
//     404 "workspace not found" to the platform admin; the key and the token
//     get 401 "authentication required" on every workspace route, which takes
//     a session.
//     A child resource (artifact, attachment, crew, test run, work item,
//     guided session...) answers its own 404 to every column, before any
//     guard;
//   - the order of guard, lookup and decode: a 400 "invalid request body"
//     where a column should have been refused is a decode before the guard
//     (POST /artifacts, /links, /chatter, /projects, /templates... to every
//     column past the middleware, bearers included); a workspace-admin guard
//     before the decode shows as the viewer's and editor's 403 beside the
//     owner's and outsider's 400; a lookup first as the lookup's 404 to all;
//   - the credential-scoped routes: the worker routes refuse everyone but a
//     key, the runner-pool routes all eight columns, the delegation pair all
//     but the run token, which gets the lookup's 404, or its own 400 before
//     the decode;
//   - the admin column: a platform admin passes the project and workspace
//     guards with no role, but only for a project or workspace that exists,
//     so it shows the guards' shortcuts: a phantom project or workspace is
//     its 404, and every other phantom its handler's own answer;
//   - the /auth and /public rows, each cell from an address of its own: the
//     same answer for every column, but where the handler reads a session
//     (share acceptance, verification resend, /auth/me);
//   - Q1: a 2xx answer the handler gave no type is "text".
//
// Section "phantom ids in a well-formed body" sends the routes that take
// their project, artifact or workspace from the query or the body, which
// answer the decode's 400 above to everyone: the reads first, then the
// writes, each naming the phantom, so their guards and lookups show.
//
// After each section the events of every workspace a signed-in column acts
// in are read and listed with it: W's must be none (the area fails
// otherwise), and no section publishes any.
//
// The viewer and editor columns coincide throughout: a phantom project
// refuses both before any role is compared, and no workspace is over its
// plan. So a project guard's role (editor for viewer, say) and a dropped
// alwaysWritable exemption do not show here. Part (2) shows the role of the
// 45 writes its section "real ids, body {" sends, and part (3) that of the
// writes its over-plan steps send as a lower role, and the nine exemptions;
// the role of every other guarded write (every DELETE and every write with no
// body among them) is pinned only statically, by S2's frozen
// internal/api/testdata/route_guards.txt, which names every route's guard.
//
// Pinned as they behave (plan R7), for release-noted bug-fix pull requests
// that regenerate the golden, with the golden's rows:
//   - PUT /artifacts/{phantom} and POST /artifacts/{phantom}/restore answer
//     500 "failed to load artifact" to every column (the Q2 family: the lookup's
//     not-found is no sentinel);
//   - existence: GET /projects/{id} and /ai-map look the project up before the
//     guard, so a phantom project is 404 there, where a real foreign one is
//     403; every child resource's 404 comes before its guard; the worker key's
//     phantom project is 404, another workspace's 403; POST /links, /crews,
//     /teams and the crew imports answer 400 "not found" before any guard; a
//     phantom artifact or link reads "project not found" (projectIDForArtifact
//     answers "" and the guard 404s it);
//   - GET /proposals with no project_id answers a plain member 403
//     "project_id is required", a 403 for a missing parameter;
//   - W's worker key reaches the proposal review routes' lookup (404), where a
//     run token is refused first: nothing but the project guard, which passes a
//     workspace key as an editor, stands between the key and an approval with
//     no reviewer (REQ-21); a real proposal is part (2)'s or S5d's to show.
func TestTourS5ePhantomMatrix(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5e",
		key:   "phantom_matrix",
		about: "The phantom-id authorization matrix: every route of routes.txt, every path id a well-formed id no row " +
			"has and every write the body {, sent to anonymous, a viewer, an editor and the owner of P, an outsider, " +
			"W's worker key, the token of a run in P and the platform admin; then the routes that take their scope " +
			"from the body or the query, with a well-formed body or query naming the phantom.",
		run:      phantomMatrixTour,
		env:      tourMatrixEnv(),
		files:    tourMatrixFiles(),
		accounts: tourMatrixAccounts(),
	})
}

// phantomMatrixLogout is the route the matrix sends from second sessions.
const phantomMatrixLogout = "POST /api/v1/auth/logout"

// phantomMatrixSends are the area's conventions, beyond the framework's.
var phantomMatrixSends = []string{
	"section \"phantom ids, body {\": every path variable is phantom: an id <phantom> (" + tourPhantom + "), " +
		"{token} <phantom.token> (64 zeros), {slug} " + tourPhantomSlug + ", {version} 1 (every attachment has a " +
		"version 1; only its id is phantom); a route with two ids has <phantom> in both",
	"section \"phantom ids, body {\": a GET or HEAD has no body and no Content-Type; every other method sends " +
		"Content-Type: application/json and the one-byte body { (a JSON value cut short, which decodes into nothing, " +
		"so the check the handler makes first answers, and nothing is written)",
	"section \"phantom ids, body {\": POST /api/v1/auth/logout is sent by each signed-in column from a second " +
		"session of its account (<name>.logout.session), so that the column's own session lives on",
	"section \"phantom ids in a well-formed body\": the routes whose scope is in the query or the body, with a " +
		"query or a body naming the phantom (the row shows it); the reads first, so that no write of the section " +
		"shows in them",
	"after each section, the events of each workspace a signed-in column acts in are read and listed with the " +
		"section (W, <outsider.workspace>, <admin.workspace>); W's must be none, or the area fails",
}

func phantomMatrixTour(tr *tour) {
	cast := tr.matrixCast()
	logout := map[string]*tourActor{}
	for _, a := range cast.signedIn() {
		logout[a.name] = tr.secondSession(a, a.name+".logout", "a second session of "+a.name+
			", which the matrix's sign-out row ends")
	}
	m := tr.matrix(phantomMatrixSends, cast.columns()...)

	m.section("phantom ids, body {", "Every route of internal/api/testdata/routes.txt, in its order: its path ids "+
		"phantom, a write's body {.")
	routes := readRouteList(tr.t)
	for _, route := range routes {
		opts := phantomPath(route)
		if method, _, _ := strings.Cut(route, " "); !isRead(method) {
			opts = append(opts, truncatedBody())
		}
		if route == phantomMatrixLogout {
			m.rowAs(route, logout, opts...)
			continue
		}
		m.row(route, opts...)
	}
	m.expectRoutes(routes)
	m.readSectionEvents(cast.owner)

	m.section("phantom ids in a well-formed body", "The routes that take their project, artifact or workspace from "+
		"the query or the body, which answer the body's decode to every column above: the reads with the phantom "+
		"in the query, then the writes with a well-formed body naming it.")
	phantomMatrixScoped(m)
	m.readSectionEvents(cast.owner)
}

// phantomMatrixScoped sends the routes scoped by their query or body.
func phantomMatrixScoped(m *tourMatrix) {
	for _, r := range []struct{ route, query string }{
		{"GET /api/v1/artifacts", "project_id={{phantom}}"},
		{"GET /api/v1/links", "project_id={{phantom}}"},
		{"GET /api/v1/chatter", "artifact_id={{phantom}}"},
		{"GET /api/v1/guided-sessions", "project_id={{phantom}}"},
		{"GET /api/v1/attribute-definitions", "project_id={{phantom}}"},
		{"GET /api/v1/meta/attribute-definitions", "project_id={{phantom}}"},
		{"GET /api/v1/events", "project_id={{phantom}}"},
		{"GET /api/v1/proposals", "project_id={{phantom}}"},
		{"GET /api/v1/agent-runs", "project_id={{phantom}}"},
		{"GET /api/v1/automations", "project_id={{phantom}}"},
		{"GET /api/v1/crews", "project_id={{phantom}}"},
	} {
		m.row(r.route, query(r.query))
	}
	ct, upload := multipartForm([][2]string{{"artifact_id", tourPhantom}},
		tourFormFile{field: "file", name: "tour.png", contentType: "image/png", data: []byte(tourPNG)})
	for _, w := range []struct {
		route string
		opts  []tourOpt
	}{
		{"POST /api/v1/artifacts", []tourOpt{jsonBody(`{"project_id":"{{phantom}}","type":"requirement",` +
			`"title":"Tour phantom","body":"The system shall do nothing."}`)}},
		{"POST /api/v1/artifacts/{id}/restore", []tourOpt{jsonBody(`{"version":1}`)}},
		{"PUT /api/v1/artifacts/{id}", []tourOpt{jsonBody(`{"title":"Tour phantom"}`)}},
		{"PUT /api/v1/artifacts/{id}/status", []tourOpt{jsonBody(`{"status":"approved"}`)}},
		{"POST /api/v1/links", []tourOpt{jsonBody(`{"from_id":"{{phantom}}","to_id":"{{phantom}}","type":"derives-from"}`)}},
		{"PUT /api/v1/links/{id}", []tourOpt{jsonBody(`{"type":"derives-from"}`)}},
		{"POST /api/v1/attachments/upload", []tourOpt{rawBody(ct, upload)}},
		{"POST /api/v1/attribute-definitions", []tourOpt{jsonBody(`{"project_id":"{{phantom}}","key":"tour",` +
			`"label":"Tour","data_type":"text"}`)}},
		{"POST /api/v1/automations", []tourOpt{jsonBody(`{"project_id":"{{phantom}}","agent_id":"{{phantom}}",` +
			`"kind":"manual","name":"Tour"}`)}},
		{"POST /api/v1/chatter", []tourOpt{jsonBody(`{"artifact_id":"{{phantom}}","body":"Tour"}`)}},
		{"POST /api/v1/crews", []tourOpt{jsonBody(`{"project_id":"{{phantom}}","name":"Tour"}`)}},
		{"POST /api/v1/teams", []tourOpt{jsonBody(`{"project_id":"{{phantom}}","name":"Tour"}`)}},
		{"POST /api/v1/crews/import", []tourOpt{query("project_id={{phantom}}"), jsonBody(`{}`)}},
		{"POST /api/v1/teams/import", []tourOpt{query("project_id={{phantom}}"), jsonBody(`{}`)}},
		{"POST /api/v1/guided-sessions", []tourOpt{jsonBody(`{"project_id":"{{phantom}}"}`)}},
		{"POST /api/v1/proposals/bulk", []tourOpt{jsonBody(`{"action":"approve","ids":["{{phantom}}"]}`)}},
		{"POST /api/v1/templates", []tourOpt{jsonBody(`{"project_id":"{{phantom}}","name":"Tour"}`)}},
		{"POST /api/v1/templates/{id}/projects", []tourOpt{jsonBody(`{"name":"Tour phantom"}`)}},
		{"PUT /api/v1/me/default-workspace", []tourOpt{jsonBody(`{"org_id":"{{phantom}}"}`)}},
		{"POST /api/v1/auth/share/accept", []tourOpt{jsonBody(`{"token":"{{phantom.token}}"}`)}},
		{"POST /api/v1/auth/invitations/accept", []tourOpt{jsonBody(`{"token":"{{phantom.token}}"}`)}},
		{"POST /api/v1/auth/invitations/preview", []tourOpt{jsonBody(`{"token":"{{phantom.token}}"}`)}},
		{"POST /api/v1/auth/verify-email", []tourOpt{jsonBody(`{"token":"{{phantom.token}}"}`)}},
		{"POST /api/v1/auth/password-reset/confirm", []tourOpt{jsonBody(`{"token":"{{phantom.token}}",` +
			`"new_password":"short"}`)}},
		{"POST /api/v1/auth/password-reset/confirm", []tourOpt{jsonBody(`{"token":"{{phantom.token}}",` +
			`"new_password":"tour password 2"}`)}},
		{"POST /api/v1/public/connector/pair", []tourOpt{jsonBody(`{"code":"TOURPHNT"}`)}},
	} {
		m.row(w.route, append(phantomPath(w.route), w.opts...)...)
	}
}
