//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTourMatrix checks, with no database, what S5e added to the tour
// (tour_matrix_test.go, tour_matrix_golden_test.go,
// tour_coverage_union_test.go): phantomPath over every route of routes.txt,
// the cell classifier on canned answers, a row's text and its refusal of the
// separator, a row's key, the client addresses ownAddress hands out, the
// workspaces whose events a section reads, a stream read to its head,
// reading a row back for coverage, tourChanges on matrix rows, and the union
// coverage's floor.
func TestTourMatrix(t *testing.T) {
	tr := &tour{t: t, norm: newTourNormaliser(), names: map[string]string{}, values: map[string]string{},
		env: map[string]string{"OPENV_CLIENT_IP_HEADER": "CF-Connecting-IP"}}
	tr.loadRoutes()
	tr.anon = &tourActor{name: "anonymous"}
	tr.remember("phantom", tourPhantom)
	tr.remember("phantom.token", tourPhantomToken)
	check := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s:\n got %s\nwant %s", what, got, want)
		}
	}

	matrixCheckPhantomPaths(t, tr)
	matrixCheckCells(t, tr.norm, check)

	// A row's key: the route; its query; a well-formed JSON body, compact;
	// not the truncated body, which is the section's convention; a multipart
	// body by its parts.
	key := func(route string, opts ...tourOpt) string { return matrixRowKey(tr.build(route, opts)) }
	check("a row's key", key("GET /api/v1/projects/{id}", at("id", "{{phantom}}")), "GET /api/v1/projects/{id}")
	check("a row's key with a query", key("GET /api/v1/artifacts", query("project_id={{phantom}}")),
		"GET /api/v1/artifacts ?project_id="+tourPhantom)
	check("a row's key with the truncated body", key("POST /api/v1/projects", truncatedBody()), "POST /api/v1/projects")
	check("a row's key with a JSON body", key("POST /api/v1/links", jsonBody("{ \"from_id\": \"{{phantom}}\" }")),
		`POST /api/v1/links {"from_id":"`+tourPhantom+`"}`)
	ct, form := multipartForm([][2]string{{"artifact_id", "{{phantom}}"}},
		tourFormFile{field: "file", name: "tour.png", contentType: "image/png", data: []byte(tourPNG)})
	check("a row's key with a multipart body", key("POST /api/v1/attachments/upload", rawBody(ct, form)),
		"POST /api/v1/attachments/upload multipart{artifact_id={{phantom}}, file=tour.png (image/png, 75 bytes)}")

	// ownAddress: a fresh address per request, in the header the env names.
	var got []string
	for i := 0; i < 3; i++ {
		got = append(got, tr.build("POST /api/v1/auth/login", []tourOpt{ownAddress()}).header.Get("CF-Connecting-IP"))
	}
	check("the addresses ownAddress hands out", strings.Join(got, " "), "10.0.0.1 10.0.0.2 10.0.0.3")

	// The workspaces whose events a section reads: each signed-in column's,
	// once, in the columns' order, the owner reading its own.
	owner := &tourActor{name: "owner", session: "o", org: "w"}
	tr.owner = owner
	m := &tourMatrix{tr: tr, columns: []*tourActor{tr.anon, {name: "viewer", session: "v", org: "w"}, owner,
		{name: "outsider", session: "x", org: "home-x"}, {name: "worker", bearer: "k"},
		{name: "admin", session: "a", org: "home-a"}}}
	var where []string
	for _, ws := range m.workspaces() {
		where = append(where, ws.org+" by "+ws.reader.name)
	}
	check("the workspaces a section's events are read in", strings.Join(where, ", "),
		"w by owner, home-x by outsider, home-a by admin")

	matrixCheckStreamHead(t, check)
	matrixCheckGolden(t, check)
}

// matrixCheckPhantomPaths builds every route of routes.txt with phantomPath
// and checks each path variable got its phantom and none is left.
func matrixCheckPhantomPaths(t *testing.T, tr *tour) {
	routes := readRouteList(t)
	if len(routes) < 300 {
		t.Fatalf("routes.txt lists %d routes", len(routes))
	}
	want := map[string]string{"token": tourPhantomToken, "slug": tourPhantomSlug, "version": "1"}
	for _, route := range routes {
		r := tr.build(route, phantomPath(route))
		_, tmpl, _ := strings.Cut(route, " ")
		ts, ps := strings.Split(tmpl, "/"), strings.Split(r.path, "/")
		if strings.Contains(r.path, "{") || len(ts) != len(ps) {
			t.Errorf("%s: phantomPath built %s", route, r.path)
			continue
		}
		for i, seg := range ts {
			if !strings.HasPrefix(seg, "{") {
				continue
			}
			v, ok := want[seg[1:len(seg)-1]]
			if !ok {
				v = tourPhantom
			}
			if ps[i] != v {
				t.Errorf("%s: %s is %q, want %q", route, seg, ps[i], v)
			}
		}
	}
}

// matrixCheckCells checks the classifier and a cell's text on canned answers.
func matrixCheckCells(t *testing.T, n *tourNormaliser, check func(what, got, want string)) {
	jsonType := http.Header{"Content-Type": {"application/json"}}
	sniffed := http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
	for _, c := range []struct {
		name, method string
		status       int
		header       http.Header
		body         string
		counts       bool
		want         string
	}{
		{"an envelope", "GET", 403, jsonType, `{"error":"you do not have access to this project"}` + "\n", false,
			"403: you do not have access to this project"},
		{"an envelope with a code and more", "PUT", 403, jsonType,
			`{"code":"plan_read_only","error":"read-only","over":["max_members"],"remedy":"x"}`, false,
			"403 plan_read_only: read-only {+over,remedy}"},
		{"an envelope naming a registered id", "GET", 404, jsonType, `{"error":"no project ` + tourPhantom + `"}`, false,
			"404: no project <phantom>"},
		{"a JSON object", "GET", 200, jsonType, `{"id":1}`, true, "200 json"},
		{"a bare encode, sniffed", "GET", 200, sniffed, `{"ok":true}` + "\n", false, "200 text"},
		{"a bare encode of an envelope", "GET", 400, sniffed, `{"error":"bad"}`, false, "400 text: bad"},
		{"one line of text", "GET", 404, sniffed, "404 page not found\n", false, "404 text: 404 page not found"},
		{"no body, no type", "DELETE", 204, http.Header{}, "", false, "204 -"},
		{"a typed answer with no body", "POST", 200, jsonType, "", false, "200 json empty"},
		{"a HEAD", "HEAD", 404, jsonType, "", false, "404 json"},
		{"an event stream's head", "GET", 200, http.Header{"Content-Type": {"text/event-stream"}}, "", false, "200 sse"},
		{"a document", "GET", 200, http.Header{"Content-Type": {"application/vnd.openxmlformats-officedocument." +
			"wordprocessingml.document"}}, "PK", false, "200 docx"},
		{"another media type", "GET", 200, http.Header{"Content-Type": {"application/x-tour; q=1"}}, "x", false,
			"200 application/x-tour"},
		{"an array, counted", "GET", 200, sniffed, `[{"a":1},{"a":2}]` + "\n", true, "200 text[2]"},
		{"an array, not counted", "GET", 200, sniffed, `[1]`, false, "200 text"},
		{"null, counted", "GET", 200, jsonType, "null\n", true, "200 json null"},
	} {
		cell := matrixCell(c.method, c.status, c.header, []byte(c.body), c.counts)
		check("the cell of "+c.name, cell.render(n), c.want)
	}
	// rowCounting's count at a pointer: an array, null, another value, an
	// envelope, and after a section's own count.
	for _, c := range []struct {
		name, body string
		counts     bool
		want       string
	}{
		{"hits counted", `{"mode_used":"keyword","hits":[{"id":1},{"id":2}]}`, false, "200 json /hits[2]"},
		{"hits null", `{"hits":null}`, false, "200 json /hits null"},
		{"hits not an array", `{"hits":3}`, false, "200 json"},
		{"no hits", `{"mode_used":"keyword"}`, false, "200 json"},
		{"an array, then its pointer", `[[1],[2,3]]`, true, "200 json[2] /1[2]"},
	} {
		cell := matrixCell("GET", 200, jsonType, []byte(c.body), c.counts)
		pointer := "/hits"
		if c.counts {
			pointer = "/1"
		}
		cell.countAt(pointer, []byte(c.body))
		check("the count at a pointer, "+c.name, cell.render(n), c.want)
	}
	envelope := matrixCell("GET", 403, jsonType, []byte(`{"error":"no"}`), false)
	envelope.countAt("/hits", []byte(`{"error":"no"}`))
	check("the count at a pointer of an envelope", envelope.render(n), "403: no")
	cells := []tourMatrixCell{{status: 401, message: "authentication required"}, {status: 200, kind: "json"}}
	row, err := matrixRowText(n, "GET /api/v1/projects/"+tourPhantom, cells)
	check("a row", row, "GET /api/v1/projects/<phantom> | 401: authentication required | 200 json")
	check("a row's error", fmt.Sprint(err), "<nil>")
	if _, err := matrixRowText(n, "GET /x", []tourMatrixCell{{status: 400, message: "a | b"}}); err == nil {
		t.Error("a message holding the separator was written into a row")
	}
	if _, err := matrixRowText(n, "GET /x", []tourMatrixCell{{status: 400, message: "a\nb"}}); err == nil {
		t.Error("a message holding a newline was written into a row")
	}
}

// matrixCheckStreamHead sends a matrix probe to a server that streams and
// never ends: streamHead returns at the head, and the handler sees the client
// leave.
func matrixCheckStreamHead(t *testing.T, check func(what, got, want string)) {
	left := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		left <- struct{}{}
	}))
	defer srv.Close()
	tr := &tour{t: t, s: &serverProcess{base: srv.URL}, norm: newTourNormaliser(), names: map[string]string{},
		values: map[string]string{}, sent: map[string]int{}, seen: map[string]bool{}}
	tr.loadRoutes()
	a := &tourActor{name: "viewer", session: "s", org: "w", home: "w"}
	start := time.Now()
	res := tr.probe(a, "GET /api/v1/notifications/stream", streamHead())
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("a stream read to its head took %s", took)
	}
	check("a stream's head", matrixCell("GET", res.status, res.header, res.body, false).render(tr.norm), "200 sse")
	select {
	case <-left:
	case <-time.After(2 * time.Second):
		t.Error("the stream's handler did not see the client leave")
	}
}

// matrixCheckGolden reads rows back (coverage), compares two matrices
// (tourChanges), and renders the union coverage with its floor.
func matrixCheckGolden(t *testing.T, check func(what, got, want string)) {
	key, route, statuses, err := matrixRowParts(`POST /api/v1/links {"a":"b c"} | 401: x | 400 weak_password: y | ` +
		`200 json[2]`)
	check("a row's parts", fmt.Sprint(key, "|", route, "|", statuses, "|", err),
		`POST /api/v1/links {"a":"b c"}|POST /api/v1/links|[401 400 200]|<nil>`)
	if _, _, _, err := matrixRowParts("GET /x | two hundred"); err == nil {
		t.Error("a cell with no status was read")
	}
	var g tourGoldenIndex
	if err := json.Unmarshal([]byte(`{"test":"T","steps":[],"matrix":{"sections":[{"name":"a","rows":[`+
		`"GET /api/v1/projects | 401: x | 403: y","GET /api/v1/projects ?q=1 | 200 json"]}]}}`), &g); err != nil {
		t.Fatal(err)
	}
	statusesOf, areas := map[string]map[int]bool{}, map[string]map[string]bool{}
	countMatrixCells(t, "s9", "k", g, map[string]bool{"GET /api/v1/projects": true}, statusesOf, areas)
	const projects = "GET /api/v1/projects"
	check("the statuses counted", fmt.Sprint(sortedInts(statusesOf[projects]), sortedKeys(areas[projects])),
		"[200 401 403] [k]")

	golden := func(rows ...string) []byte {
		b, _ := json.Marshal(map[string]any{"steps": []any{}, "matrix": tourMatrixRecord{
			Columns: []string{"anonymous", "owner"}, Sections: []tourMatrixSectionRecord{{Name: "s", Rows: rows}}}})
		return b
	}
	want := golden("GET /a | 401: x | 200 json", "GET /b | 401: x | 403: y", "GET /c | 401: x | 404: z")
	check("a changed cell, a gone and an added row", tourChanges(want,
		golden("GET /a | 401: x | 403: y", "GET /b | 401: x | 403: y", "GET /d | 401: x | 200 json"), false),
		"  matrix \"s\": GET /a: owner: 200 json -> 403: y\n  matrix \"s\": added row GET /d\n"+
			"  matrix \"s\": gone row GET /c")
	check("an early stop", tourChanges(want, golden("GET /a | 401: x | 200 json"), true),
		"  matrix \"s\": (not run: 2 rows)")
	check("no matrix on either side", tourChanges([]byte(`{"steps":[]}`), []byte(`{"steps":[]}`), false), "")
	withEvents := func(events ...string) []byte {
		b, _ := json.Marshal(map[string]any{"steps": []any{}, "matrix": tourMatrixRecord{Columns: []string{"a"},
			Sections: []tourMatrixSectionRecord{{Name: "s", Rows: []string{"GET /a | 200 json"}, Events: events}}}})
		return b
	}
	check("a section's events", tourChanges(withEvents(), withEvents(`<w>: {"type":"artifact.created"}`), false),
		"  matrix \"s\": events (1, were 0)")

	routes := []string{"GET /a", "GET /b", "GET /c"}
	text, missing := renderTourUnionCoverage(routes, map[string][]string{"GET /a": {"s5e", "s5a"}, "GET /b": {"s5e"}},
		map[string][]string{"GET /a": {"s5a"}})
	body := string(text[strings.Index(string(text), "\n\n")+2:])
	check("the union", body, "GET /a: s5a, s5e; 2xx or 3xx in s5a\nGET /b: s5e (errors only)\nGET /c: not reached\n\n"+
		"the tour: 2 of 3 routes reached (66.7%), 1 with a 2xx or 3xx answer (33.3%); the floor is 90%, 3 routes\n")
	check("the routes below the floor", strings.Join(missing, ", "), "GET /b, GET /c")
}
