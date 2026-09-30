//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// TestTourS5ePhantomMatrix is part (1) of S5e, the phantom-id authorization
// matrix (refactor plan §6.4 S5e; invariant I3; quirks Q1 and Q19, and Q2's
// neighbours, fixed under R7; OpenV REQ-143 and REQ-18), on the framework of
// tour_matrix_test.go with the columns of tour_matrix_cast_test.go. Its
// golden is testdata/tour/s5e/phantom_matrix.json; parts (2), the real-id
// reads, and (3), the over-plan pass, are areas of their own.
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
//   - existence hiding: a phantom project is 404 "project not found" to
//     every column past the middleware, the owner included, as a real
//     project the caller cannot reach is (part (2)'s outsider; fixed under
//     R7, a user got 403 "you do not have access to this project" and the
//     run token 403 "agent run is not scoped to this project"). A phantom
//     workspace is 404 "workspace not found" to every signed-in column; the
//     key and the token get 401 "authentication required" on every workspace
//     route, which takes a session. A child resource (artifact, attachment,
//     crew, test run, work item, guided session...) answers its own 404 to
//     every column, before any guard, and its guard answers a real one the
//     caller cannot reach with that same 404;
//   - a malformed id: every route with an id in its path, and every row of
//     the second section naming one, is sent again with not-a-uuid, then
//     with a byte that is not UTF-8 and with a NUL, in its place, and must
//     answer every column exactly as the phantom did (the area fails
//     otherwise, and records nothing more; fixed under R7, the project and
//     workspace guards and a dozen lookups answered it 500, and PUT
//     /artifacts/{id} and its restore, with a well-formed body, answered a
//     phantom, and so a malformed id, 500);
//   - the order of guard, lookup and decode: a 400 "invalid request body"
//     where a column should have been refused is a decode before the guard
//     (POST /artifacts, /links, /chatter, /projects, /templates... to every
//     column past the middleware, bearers included, but for the run token and
//     the worker key on the three project creates, POST /projects,
//     /projects/import and /templates/{id}/projects, which refuse them first
//     with 403 "agent runs cannot create projects" and "runner keys cannot
//     create projects", REQ-42); a workspace-admin guard
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
// writes its over-plan steps send as a lower role, and the exemptions;
// the role of every other guarded write (every DELETE and every write with no
// body among them) is pinned only statically, by S2's frozen
// internal/api/testdata/route_guards.txt, which names every route's guard.
//
// Pinned as they behave (plan R7), for release-noted bug-fix pull requests
// that regenerate the golden, with the golden's rows:
//   - a phantom artifact on its versions, links and attachments, and a
//     phantom link's delete, read "project not found" (projectIDForArtifact
//     answers "" and the guard 404s it), as a real one the caller cannot
//     reach does;
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
	"section \"phantom ids, body {\": then every route with an id in its path is sent again with each id " +
		phantomMatrixMalformedKinds + " (a token, a slug and a version as above), and each column must answer it " +
		"exactly as its row records, or the area fails: an id that is not a UUID answers as a well-formed id no row " +
		"has; those requests record nothing more",
	"section \"phantom ids in a well-formed body\": the routes whose scope is in the query or the body, with a " +
		"query or a body naming the phantom (the row shows it); the reads first, so that no write of the section " +
		"shows in them",
	"section \"phantom ids in a well-formed body\": then every row whose query, body or path names the phantom " +
		"is sent again with " + phantomMatrixMalformedKinds + " in its place (in a JSON body, the NUL as \\u0000 " +
		"and the byte as the U+FFFD a JSON decoder reads it as), and must answer each column exactly as its row " +
		"records, or the area fails; those requests record nothing more",
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
	type sent struct {
		route string
		row   *tourMatrixRow
	}
	var withIDs []sent
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
		if malformedPath(route, phantomMatrixMalformed[0]) != nil {
			withIDs = append(withIDs, sent{route, m.lastRow()})
		}
	}
	m.expectRoutes(routes)
	for _, id := range phantomMatrixMalformed {
		for _, w := range withIDs {
			opts := malformedPath(w.route, id)
			if method, _, _ := strings.Cut(w.route, " "); !isRead(method) {
				opts = append(opts, truncatedBody())
			}
			m.expectSame(w.row, w.route, opts...)
		}
	}
	m.readSectionEvents(cast.owner)

	m.section("phantom ids in a well-formed body", "The routes that take their project, artifact or workspace from "+
		"the query or the body, which answer the body's decode to every column above: the reads with the phantom "+
		"in the query, then the writes with a well-formed body naming it.")
	phantomMatrixScoped(m)
	m.readSectionEvents(cast.owner)
}

// phantomMatrixMalformed are ids that are not UUIDs, which every route must
// answer exactly as it answers the phantom (bug 15): text Postgres reads and
// refuses as a uuid, and a byte that is not UTF-8 and a NUL, which it refuses
// before it reads any type. The matrix sends each row that names an id again
// with each and records nothing more.
var phantomMatrixMalformed = []string{"not-a-uuid", "\xff", "a\x00b"}

// phantomMatrixMalformedKinds names phantomMatrixMalformed in the area's
// conventions, as a path or a query sends them.
const phantomMatrixMalformedKinds = "not-a-uuid, then %FF (a byte that is not UTF-8), then a%00b (a NUL)"

// malformedPath is phantomPath with every id the malformed id (a token, a
// slug and a version as phantomPath fills them), or nil for a route with no
// id in its path.
func malformedPath(route, malformed string) []tourOpt {
	var pairs []string
	ids := 0
	for _, v := range routeVarRE.FindAllString(route, -1) {
		name, value := v[1:len(v)-1], malformed
		if filled := phantomValue(name); filled != "{{phantom}}" {
			value = filled
		} else {
			ids++
		}
		pairs = append(pairs, name, value)
	}
	if ids == 0 {
		return nil
	}
	return []tourOpt{at(pairs...)}
}

// phantomMatrixScoped sends the routes scoped by their query or body, then
// each whose query, body or path names an id again with the id malformed,
// of each kind (expectSame).
func phantomMatrixScoped(m *tourMatrix) {
	var rows []*tourMatrixRow
	for _, r := range phantomMatrixScopedRows(scopedID{"{{phantom}}", "{{phantom}}", tourPhantom}, phantomPath) {
		m.row(r.route, r.opts...)
		rows = append(rows, m.lastRow())
	}
	for _, id := range phantomMatrixMalformed {
		quoted, _ := json.Marshal(id)
		named := scopedID{url.QueryEscape(id), string(quoted[1 : len(quoted)-1]), id}
		path := func(route string) []tourOpt { return malformedPath(route, id) }
		for i, r := range phantomMatrixScopedRows(named, path) {
			if r.namesID {
				m.expectSame(rows[i], r.route, r.opts...)
			}
		}
	}
}

// scopedID is an id as phantomMatrixScopedRows names it: in a query, inside
// a JSON string, and as a multipart part carries it.
type scopedID struct{ query, json, literal string }

type phantomMatrixScopedRow struct {
	route   string
	opts    []tourOpt
	namesID bool // an id in the query, the body or the path
}

// phantomMatrixScopedRows are the rows phantomMatrixScoped sends, naming the
// id (a template the tour fills, or its text) in the query or body and
// path's ids in the path.
func phantomMatrixScopedRows(named scopedID, path func(string) []tourOpt) []phantomMatrixScopedRow {
	id, literal := named.json, named.literal
	var out []phantomMatrixScopedRow
	for _, r := range []struct{ route, query string }{
		{"GET /api/v1/artifacts", "project_id=%s"},
		{"GET /api/v1/links", "project_id=%s"},
		{"GET /api/v1/chatter", "artifact_id=%s"},
		{"GET /api/v1/guided-sessions", "project_id=%s"},
		{"GET /api/v1/attribute-definitions", "project_id=%s"},
		{"GET /api/v1/meta/attribute-definitions", "project_id=%s"},
		{"GET /api/v1/events", "project_id=%s"},
		{"GET /api/v1/proposals", "project_id=%s"},
		{"GET /api/v1/agent-runs", "project_id=%s"},
		{"GET /api/v1/automations", "project_id=%s"},
		{"GET /api/v1/crews", "project_id=%s"},
	} {
		out = append(out, phantomMatrixScopedRow{r.route, []tourOpt{query(fmt.Sprintf(r.query, named.query))}, true})
	}
	ct, upload := multipartForm([][2]string{{"artifact_id", literal}},
		tourFormFile{field: "file", name: "tour.png", contentType: "image/png", data: []byte(tourPNG)})
	for _, w := range []struct {
		route string
		opts  []tourOpt
	}{
		{"POST /api/v1/artifacts", []tourOpt{jsonBody(`{"project_id":"` + id + `","type":"requirement",` +
			`"title":"Tour phantom","body":"The system shall do nothing."}`)}},
		{"POST /api/v1/artifacts/{id}/restore", []tourOpt{jsonBody(`{"version":1}`)}},
		{"PUT /api/v1/artifacts/{id}", []tourOpt{jsonBody(`{"title":"Tour phantom"}`)}},
		{"PUT /api/v1/artifacts/{id}/status", []tourOpt{jsonBody(`{"status":"approved"}`)}},
		{"POST /api/v1/links", []tourOpt{jsonBody(`{"from_id":"` + id + `","to_id":"` + id + `","type":"derives-from"}`)}},
		{"PUT /api/v1/links/{id}", []tourOpt{jsonBody(`{"type":"derives-from"}`)}},
		{"POST /api/v1/attachments/upload", []tourOpt{rawBody(ct, upload)}},
		{"POST /api/v1/attribute-definitions", []tourOpt{jsonBody(`{"project_id":"` + id + `","key":"tour",` +
			`"label":"Tour","data_type":"text"}`)}},
		{"POST /api/v1/automations", []tourOpt{jsonBody(`{"project_id":"` + id + `","agent_id":"` + id + `",` +
			`"kind":"manual","name":"Tour"}`)}},
		{"POST /api/v1/chatter", []tourOpt{jsonBody(`{"artifact_id":"` + id + `","body":"Tour"}`)}},
		{"POST /api/v1/crews", []tourOpt{jsonBody(`{"project_id":"` + id + `","name":"Tour"}`)}},
		{"POST /api/v1/teams", []tourOpt{jsonBody(`{"project_id":"` + id + `","name":"Tour"}`)}},
		{"POST /api/v1/crews/import", []tourOpt{query("project_id=" + named.query), jsonBody(`{}`)}},
		{"POST /api/v1/teams/import", []tourOpt{query("project_id=" + named.query), jsonBody(`{}`)}},
		{"POST /api/v1/guided-sessions", []tourOpt{jsonBody(`{"project_id":"` + id + `"}`)}},
		{"POST /api/v1/proposals/bulk", []tourOpt{jsonBody(`{"action":"approve","ids":["` + id + `"]}`)}},
		{"POST /api/v1/templates", []tourOpt{jsonBody(`{"project_id":"` + id + `","name":"Tour"}`)}},
		{"POST /api/v1/templates/{id}/projects", []tourOpt{jsonBody(`{"name":"Tour phantom"}`)}},
		{"PUT /api/v1/me/default-workspace", []tourOpt{jsonBody(`{"org_id":"` + id + `"}`)}},
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
		names := !strings.HasPrefix(w.route, "POST /api/v1/auth/") && !strings.HasPrefix(w.route, "POST /api/v1/public/")
		out = append(out, phantomMatrixScopedRow{w.route, append(path(w.route), w.opts...), names})
	}
	return out
}
