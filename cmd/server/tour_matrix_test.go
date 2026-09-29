//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// The authorization matrix of the API tour (refactor plan §6.4 S5e;
// invariant I3: 401, 403 or 404 per route and identity, and the order of
// guard, lookup and decode; OpenV REQ-143 and REQ-18). A matrix area sends
// one route to every identity of a fixed list of columns, once each, and
// records per cell only what I3 is about: the status, the error envelope's
// code and message, or the kind of body when the answer is no envelope. The
// bodies themselves are S5a-S5d's goldens. Everything here is new and inert
// for an area that does not call tour.matrix; the edits it needed elsewhere
// are inert too (see the end of this comment).
//
// ROWS. m.row(route, opts...) builds the route's request from routes.txt's
// template and the options, as tour.step does, and sends it once per
// column, in the columns' order, as a probe (tour.probe): not recorded as a
// step, but declared under its route for the /metrics check, which proves
// the path each cell sent matched the template its row names. A GET or HEAD
// is sent once (no gzip variant); an answer that is an event stream is read
// to its head and closed (streamHead). A row's key is its route, then the
// query and the body when they are not the section's convention (a
// well-formed body naming a phantom, a query); a section holds a key once.
// A route under /api/v1/auth/ or /api/v1/public/ is sent from an address of
// its own for every cell (ownAddress), so that no rate-limit bucket keyed by
// address is shared between two cells, however fast the runner is; the area
// sets OPENV_CLIENT_IP_HEADER for that, and tour.matrix fails without it.
// m.rowAs sends some columns from another actor (a second session of the
// same account, for the sign-out row). m.rowCounting counts, in each cell,
// the JSON array at a pointer inside the answer (search's hits), which a
// section's countLists would not see in an object.
//
// CELLS (matrixCell). The status, then:
//   - ": message" for a JSON error envelope (application/json, an object
//     with a string "error"), " code: message" when it has a code, and
//     " {+key,...}" after it for the envelope's other keys;
//   - otherwise " kind": json, text (a body the handler gave no type, which
//     net/http sniffed as text/plain: Q1), sse, html, png, pdf, csv, xml, md,
//     docx, xlsx, zip, bin, or the media type itself, and "-" for no body and
//     no Content-Type; " empty" after it for a typed answer with no body (not
//     a HEAD, nor an event stream, which is read to its head only); ": message"
//     after text when the body is an envelope or one short line;
//   - in a section that counts lists (countLists), "[n]" or " null" after a
//     JSON body that is an array or null; in a row that counts at a pointer
//     (rowCounting), " <pointer>[n]" or " <pointer> null" for the array or
//     null there.
//
// Messages are normalised with the rest of the golden (tour.norm), so a
// registered id reads <name>. A cell never holds " | ", which separates
// cells in a row; the renderer fails the area if one would.
//
// EVENTS. A cell records no events, but m.readSectionEvents, called at the end
// of a section, reads what each workspace a signed-in column acts in
// published during it (tour.readEventsAs, as a column acting there; the
// owner's through tour.readEvents) and lists it with the section, as a step's
// events are rendered; a workspace a quiet actor acts in must have published
// none. So a write that answered a refusal but wrote anyway, or a phantom's
// write that published into the caller's workspace, shows. tour.matrix first
// reads, and leaves out, what the setups published in those workspaces, and
// m.leaveOutSetups does the same for setups an area sends between sections.
//
// THE GOLDEN. The area's golden (tourGolden) gains "matrix" after "steps":
// the columns, the conventions its requests follow ("sends", the area's
// own), the cell grammar ("cells", above), and the sections, each a list of
// rows "<key> | <cell> | ... " with a cell per column. The field is
// omitempty, so no earlier golden changed; a section lists its events after
// its rows. tourChanges names each changed cell by section, row and column
// (tour_matrix_golden_test.go); TestTourCoverage counts each cell's
// status under its row's route, so a route the matrix reached with errors
// alone is "errors only" in the slice's coverage.txt, and writes the union
// across slices, testdata/tour/coverage.txt, which holds the plan's floor:
// at least 90% of the routes answered a 2xx or 3xx somewhere
// (tour_coverage_union_test.go).
//
// IDENTITIES. tour.matrixCast builds the columns S5e's matrix areas share
// (tour_matrix_cast_test.go): anonymous, a viewer, an editor and the owner
// of a project P in the owner's shared workspace W, an outsider acting in
// its own workspace, W's workspace worker key, the token of a running
// direct-mode run in P, and the platform admin.
//
// EDITS ELSEWHERE, each inert for an area without a matrix: tour_test.go's
// tour gains mx (the area's matrix) and addresses (ownAddress's counter),
// tourGolden gains Matrix (omitempty), render fills it, tourChanges diffs it,
// tourGoldenIndex reads it and tourCoverage counts its cells; readEvents reads
// through readEventsAs, as the owner, as before; runTourArea and
// TestTourCoverage also write the union coverage, and an area's log line
// counts its matrix cells; tour_stream_test.go's tourStream gains head, which
// reads a stream's head only. Every S5a-S5d golden and coverage.txt is byte
// for byte what it was.

// tourPhantomToken is a well-formed token no link, invitation or interview
// has (64 hex digits, looked up by its SHA-256): written <phantom.token>.
const tourPhantomToken = "0000000000000000000000000000000000000000000000000000000000000000"

// tourPhantomSlug is a well-formed agent slug no agent has.
const tourPhantomSlug = "tour-phantom"

// tourMatrix is an area's authorization matrix.
type tourMatrix struct {
	tr       *tour
	columns  []*tourActor
	sends    []string
	sections []*tourMatrixSection
}

// tourMatrixSection is a titled list of rows.
type tourMatrixSection struct {
	name, about string
	counts      bool // countLists: a JSON array's length or null is recorded
	rows        []*tourMatrixRow
	keys        map[string]bool
	events      []tourMatrixEvents // readSectionEvents
}

// tourMatrixEvents are the events one workspace published during a section.
type tourMatrixEvents struct {
	workspace string
	raw       []json.RawMessage
}

// tourMatrixRow is one route (with its query or body) sent to every column.
type tourMatrixRow struct {
	route string
	key   string // raw: the route, then the query and body when shown
	cells []tourMatrixCell
}

// tourMatrixCell is what one column's answer recorded, raw; render
// normalises it.
type tourMatrixCell struct {
	status   int
	kind     string   // "" for a JSON error envelope
	code     string   // the envelope's code
	message  string   // the envelope's error, or a short text body
	extra    []string // the envelope's other keys
	count    string   // "[n]" or " null", in a section that counts lists; then rowCounting's
	emptyTyp bool     // a typed answer with no body (not a HEAD, nor an event stream)
}

// tourMatrixRecord is the golden's "matrix".
type tourMatrixRecord struct {
	Columns  []string                  `json:"columns"`
	Sends    []string                  `json:"sends"`
	Cells    []string                  `json:"cells"`
	Sections []tourMatrixSectionRecord `json:"sections"`
}

type tourMatrixSectionRecord struct {
	Name  string   `json:"name"`
	About string   `json:"about"`
	Rows  []string `json:"rows"`
	// Events are what the columns' workspaces published during the
	// section (readSectionEvents): "<workspace>: <event>", each as a step's
	// events are, oldest first; absent when none did.
	Events []string `json:"events,omitempty"`
}

// tourMatrixCellGrammar is the golden's "cells": how to read a cell.
var tourMatrixCellGrammar = []string{
	"a row is \"<route>[ ?<query>][ <body>] | <cell> | ...\", a cell per column in the columns' order",
	"STATUS: message -- a JSON error envelope (application/json, {\"error\": ...}); STATUS code: message when it " +
		"carries a code; {+key,...} after it names the envelope's other keys",
	"STATUS kind -- any other answer: json (application/json), text (text/plain: a body the handler gave no type, " +
		"sniffed by net/http, Q1), sse (an event stream, read to its head), html, png, pdf, csv, xml, md, docx, " +
		"xlsx, zip, bin (application/octet-stream), or the media type itself; - for no body and no Content-Type; " +
		"\"empty\" after a typed kind with no body (not a HEAD, nor sse); \": message\" after text when the body " +
		"is an error envelope or one short line",
	"[n] or \" null\" after a kind, in a section that counts lists: a JSON body that is an array of n elements, or null; " +
		"\" <pointer>[n]\" or \" <pointer> null\" in a row that counts the array at a JSON pointer inside the body",
	"messages are normalised as the rest of the golden is: <name> is a registered value",
}

// matrix starts the area's authorization matrix over the given columns
// (one per area): it registers the phantom token, reads what the setups
// published in the workspaces the signed-in columns act in (left out, as a
// setup's events always are), and keeps the matrix for the golden. sends are
// the conventions the area's requests follow, listed in the golden after the
// framework's own.
func (tr *tour) matrix(sends []string, columns ...*tourActor) *tourMatrix {
	tr.t.Helper()
	switch {
	case tr.mx != nil:
		tr.t.Fatalf("an area has one authorization matrix; add a section to it instead")
	case len(columns) == 0:
		tr.t.Fatalf("a matrix needs columns")
	case strings.TrimSpace(tr.env["OPENV_CLIENT_IP_HEADER"]) == "":
		tr.t.Fatalf("a matrix area sets OPENV_CLIENT_IP_HEADER in its env, so that each cell on /api/v1/auth/ and " +
			"/api/v1/public/ comes from an address of its own and no rate-limit bucket is shared between cells")
	}
	seen := map[string]bool{}
	for _, c := range columns {
		if seen[c.name] {
			tr.t.Fatalf("the matrix has two columns named %s", c.name)
		}
		seen[c.name] = true
	}
	if _, ok := tr.names["phantom.token"]; !ok {
		tr.remember("phantom.token", tourPhantomToken)
	}
	m := &tourMatrix{tr: tr, columns: columns, sends: append(append([]string(nil), tourMatrixSends...), sends...)}
	m.leaveOutSetups()
	tr.mx = m
	return m
}

// leaveOutSetups reads what setups published in the workspaces the
// signed-in columns act in since the last read, and leaves it out, as a
// setup's events always are: tour.matrix calls it for the setups before the
// first section, an area for setups it sends between two sections (so that
// the next section's readSectionEvents lists only what its rows published).
func (m *tourMatrix) leaveOutSetups() {
	m.tr.t.Helper()
	for _, ws := range m.workspaces() {
		m.readWorkspace(ws)
	}
}

// tourMatrixSends are the conventions every matrix follows.
var tourMatrixSends = []string{
	"each cell is one request, sent once (a GET or HEAD has no gzip variant), as the column's actor: a session " +
		"cookie and X-Org-ID for a signed-in column, Authorization: Bearer for a key or a token, nothing for anonymous",
	"an answer that is an event stream is read to its head and closed (sse)",
	"every cell of a route under /api/v1/auth/ or /api/v1/public/ comes from an address of its own, in the " +
		"header OPENV_CLIENT_IP_HEADER names, so no rate-limit bucket keyed by address is shared between cells",
	"rows run in the order the golden lists them, the cells of a row in the columns' order",
}

// cellsNote is what an area's log line adds for its matrix: its cells ("" for
// an area without one, a nil matrix).
func (m *tourMatrix) cellsNote() string {
	if m == nil {
		return ""
	}
	n := 0
	for _, s := range m.sections {
		for _, r := range s.rows {
			n += len(r.cells)
		}
	}
	return fmt.Sprintf(" and %d matrix cells", n)
}

// section starts a section: the rows that follow belong to it.
func (m *tourMatrix) section(name, about string) *tourMatrixSection {
	m.tr.t.Helper()
	for _, s := range m.sections {
		if s.name == name {
			m.tr.t.Fatalf("the matrix already has a section %q", name)
		}
	}
	s := &tourMatrixSection{name: name, about: about, keys: map[string]bool{}}
	m.sections = append(m.sections, s)
	return s
}

// countLists records, in this section, the length of a JSON array an answer
// holds, or null: for real ids, whose lists are stable (not for a list the
// server writes after it answers, such as the notifications).
func (s *tourMatrixSection) countLists() *tourMatrixSection {
	s.counts = true
	return s
}

// row sends the route to every column, with the options (a column's own
// address added on /auth/ and /public/), and records the answers.
func (m *tourMatrix) row(route string, opts ...tourOpt) {
	m.tr.t.Helper()
	m.send(route, nil, "", opts)
}

// rowAs is row with some columns sent by another actor: stand maps a
// column's name to the actor that sends its cell (a second session of the
// same account, for a request that would end the column's own session).
func (m *tourMatrix) rowAs(route string, stand map[string]*tourActor, opts ...tourOpt) {
	m.tr.t.Helper()
	m.send(route, stand, "", opts)
}

// rowCounting is row with each cell also recording the length of the JSON
// array at pointer inside the answer, or null there: for a list an object
// wraps (search's hits), where a read that let a column see rows it may not
// would answer the same status and kind.
func (m *tourMatrix) rowCounting(pointer, route string, opts ...tourOpt) {
	m.tr.t.Helper()
	m.send(route, nil, pointer, opts)
}

// send is row, rowAs and rowCounting: stand as rowAs's, pointer as
// rowCounting's ("" for none).
func (m *tourMatrix) send(route string, stand map[string]*tourActor, pointer string, opts []tourOpt) {
	tr := m.tr
	tr.t.Helper()
	if len(m.sections) == 0 {
		tr.t.Fatalf("a matrix row belongs to a section: call m.section first")
	}
	for name := range stand {
		if !slices.ContainsFunc(m.columns, func(c *tourActor) bool { return c.name == name }) {
			tr.t.Fatalf("rowAs: %s is not a column", name)
		}
	}
	s := m.sections[len(m.sections)-1]
	row := &tourMatrixRow{route: route, key: matrixRowKey(tr.build(route, opts))}
	if s.keys[row.key] {
		tr.t.Fatalf("the matrix section %q already has the row %s", s.name, row.key)
	}
	s.keys[row.key] = true
	method, tmpl, _ := strings.Cut(route, " ")
	extra := []tourOpt{}
	if strings.HasPrefix(tmpl, "/api/v1/auth/") || strings.HasPrefix(tmpl, "/api/v1/public/") {
		extra = append(extra, ownAddress())
	}
	if isRead(method) {
		extra = append(extra, streamHead())
	}
	for _, col := range m.columns {
		a := col
		if st := stand[col.name]; st != nil {
			a = st
		}
		res := tr.probe(a, route, append(append([]tourOpt(nil), opts...), extra...)...)
		cell := matrixCell(method, res.status, res.header, res.body, s.counts)
		if pointer != "" {
			cell.countAt(pointer, res.body)
		}
		row.cells = append(row.cells, cell)
	}
	s.rows = append(s.rows, row)
}

// expectRoutes fails the area unless the current section's rows name exactly
// these routes, in this order: a section that must hold every route of
// routes.txt, or a declared list.
func (m *tourMatrix) expectRoutes(routes []string) {
	m.tr.t.Helper()
	s := m.sections[len(m.sections)-1]
	var got []string
	for _, r := range s.rows {
		got = append(got, r.route)
	}
	if strings.Join(got, "\n") != strings.Join(routes, "\n") {
		m.tr.t.Errorf("the matrix section %q holds %d rows, not the %d routes it must hold, in their order", s.name,
			len(got), len(routes))
	}
}

// readSectionEvents reads the events published since the last read in each
// workspace a signed-in column acts in (the owner's through tour.readEvents,
// another's as the first column acting there), records them under the
// current section, and fails the area if a workspace one of quiet acts in
// published any: a section whose bodies never decode, or whose ids no row
// has, must leave those workspaces as it found them.
func (m *tourMatrix) readSectionEvents(quiet ...*tourActor) {
	tr := m.tr
	tr.t.Helper()
	s := m.sections[len(m.sections)-1]
	for _, ws := range m.workspaces() {
		evs := m.readWorkspace(ws)
		if len(evs) == 0 {
			continue
		}
		s.events = append(s.events, tourMatrixEvents{workspace: ws.org, raw: evs})
		for _, q := range quiet {
			if q.org == ws.org {
				var types []string
				for _, e := range evs {
					var head struct {
						EventType string `json:"event_type"`
					}
					_ = json.Unmarshal(e, &head)
					types = append(types, head.EventType)
				}
				tr.t.Errorf("the workspace %s acts in published %d events during the matrix section %q, which must "+
					"leave it as it found it: %s", q.name, len(evs), s.name, strings.Join(types, ", "))
				break
			}
		}
	}
}

// tourMatrixWorkspace is a workspace a matrix's columns act in, and the
// column that reads its events.
type tourMatrixWorkspace struct {
	org    string
	reader *tourActor
}

// workspaces are the workspaces the signed-in columns act in, in the
// columns' order, each once: the owner reads its own, the first column acting
// there any other.
func (m *tourMatrix) workspaces() []tourMatrixWorkspace {
	var out []tourMatrixWorkspace
	for _, c := range m.columns {
		if c.session == "" || slices.ContainsFunc(out, func(w tourMatrixWorkspace) bool { return w.org == c.org }) {
			continue
		}
		reader := c
		if m.tr.owner != nil && m.tr.owner.org == c.org {
			reader = m.tr.owner
		}
		out = append(out, tourMatrixWorkspace{org: c.org, reader: reader})
	}
	return out
}

// readWorkspace reads a workspace's events not read before.
func (m *tourMatrix) readWorkspace(ws tourMatrixWorkspace) []json.RawMessage {
	if ws.reader == m.tr.owner {
		return m.tr.readEvents()
	}
	return m.tr.readEventsAs(ws.reader)
}

// matrixRowKey is a row's key: the route, then " ?query" and the body, when
// the request has them and the body is not truncatedBody's (a section's
// convention, which its sends state). A JSON body is shown compact, a
// multipart body by its parts.
func matrixRowKey(r *tourReq) string {
	key := r.route
	if r.query != "" {
		key += " ?" + r.query
	}
	if !r.sendBody || bytes.Equal(r.body, []byte(tourTruncatedBody)) {
		return key
	}
	mediaType, params, _ := mime.ParseMediaType(r.header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		return key + " " + matrixMultipartLabel(r.body, params["boundary"])
	case json.Valid(r.body):
		var b bytes.Buffer
		if json.Compact(&b, r.body) == nil {
			return key + " " + b.String()
		}
	}
	return key + " " + strconv.Quote(string(r.body))
}

// matrixMultipartLabel names a multipart body's parts: a field's value, a
// file's name, type and size.
func matrixMultipartLabel(body []byte, boundary string) string {
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var parts []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(p)
		if p.FileName() != "" {
			parts = append(parts, fmt.Sprintf("%s=%s (%s, %d bytes)", p.FormName(), p.FileName(),
				p.Header.Get("Content-Type"), b.Len()))
		} else {
			parts = append(parts, p.FormName()+"="+b.String())
		}
	}
	return "multipart{" + strings.Join(parts, ", ") + "}"
}

// tourTruncatedBody is a JSON value cut short: it decodes into nothing, so a
// write that carries it changes nothing, and whichever check its handler
// makes first answers (a decode before a guard answers 400).
const tourTruncatedBody = "{"

// truncatedBody sends tourTruncatedBody as application/json.
func truncatedBody() tourOpt { return rawBody("application/json", []byte(tourTruncatedBody)) }

// ownAddress sends the request from an address of its own, 10.x.y.z from a
// counter, in the header OPENV_CLIENT_IP_HEADER names (the area's env must
// set it): a request on a route whose rate limit is keyed by address then
// spends a bucket no other request of the area shares.
func ownAddress() tourOpt {
	return func(tr *tour, r *tourReq) {
		tr.t.Helper()
		h := strings.TrimSpace(tr.env["OPENV_CLIENT_IP_HEADER"])
		if h == "" {
			tr.t.Fatalf("ownAddress needs OPENV_CLIENT_IP_HEADER in the area's env")
		}
		tr.addresses++
		n := tr.addresses
		r.header.Set(h, fmt.Sprintf("10.%d.%d.%d", n>>16&255, n>>8&255, n&255))
	}
}

// streamHead reads an answer that is an event stream to its head and then
// closes it (tourStream.head), for a probe that records only the status and
// kind; an answer that is not a stream is read to its end.
func streamHead() tourOpt {
	return func(tr *tour, r *tourReq) { r.stream = &tourStream{head: true} }
}

// phantomPath fills every path variable of a route with a well-formed value
// no row has: the phantom UUID for an id, the phantom token for {token},
// the phantom slug for {slug}, and 1 for {version} (every attachment has a
// version 1, so only its id is phantom; the handler parses an integer, and
// anything else would fail first).
func phantomPath(route string) []tourOpt {
	var pairs []string
	for _, v := range routeVarRE.FindAllString(route, -1) {
		name := v[1 : len(v)-1]
		pairs = append(pairs, name, phantomValue(name))
	}
	if len(pairs) == 0 {
		return nil
	}
	return []tourOpt{at(pairs...)}
}

// phantomValue is phantomPath's value for one variable, filled.
func phantomValue(name string) string {
	switch name {
	case "token":
		return "{{phantom.token}}"
	case "slug":
		return tourPhantomSlug
	case "version":
		return "1"
	}
	return "{{phantom}}"
}

// matrixCell classifies an answer (see the top of this file).
func matrixCell(method string, status int, header http.Header, body []byte, counts bool) tourMatrixCell {
	c := tourMatrixCell{status: status}
	ct := header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)
	if mediaType == "application/json" {
		if msg, code, extra, ok := matrixEnvelope(body); ok {
			c.message, c.code, c.extra = msg, code, extra
			return c
		}
	}
	c.kind = matrixKind(mediaType, ct, len(body) > 0)
	switch {
	case ct != "" && len(body) == 0 && method != http.MethodHead && c.kind != "sse":
		c.emptyTyp = true
	case c.kind == "text" && len(body) > 0:
		if msg, code, _, ok := matrixEnvelope(body); ok {
			c.message, c.code = msg, code
		} else if !json.Valid(body) {
			if line := strings.TrimSuffix(string(body), "\n"); len(line) <= 200 && !strings.Contains(line, "\n") {
				c.message = line
			}
		}
	}
	if counts && json.Valid(body) {
		var v any
		_ = json.Unmarshal(body, &v)
		switch x := v.(type) {
		case []any:
			c.count = fmt.Sprintf("[%d]", len(x))
		case nil:
			c.count = " null"
		}
	}
	return c
}

// countAt adds to a cell the length of the JSON array at pointer in the
// answer's body, " <pointer>[n]", or " <pointer> null" for null there; nothing
// when the body holds no value there (an error envelope) or another value.
func (c *tourMatrixCell) countAt(pointer string, body []byte) {
	v, err := jsonValue(body, pointer)
	if err != nil {
		return
	}
	switch x := v.(type) {
	case []any:
		c.count += fmt.Sprintf(" %s[%d]", pointer, len(x))
	case nil:
		c.count += " " + pointer + " null"
	}
}

// matrixEnvelope reads an error envelope: a JSON object with a string
// "error", its "code" if any, and its other keys, sorted.
func matrixEnvelope(body []byte) (message, code string, extra []string, ok bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(body, &obj) != nil {
		return "", "", nil, false
	}
	if json.Unmarshal(obj["error"], &message) != nil {
		return "", "", nil, false
	}
	if raw, has := obj["code"]; has && json.Unmarshal(raw, &code) != nil {
		return "", "", nil, false
	}
	for _, k := range sortedKeys(obj) {
		if k != "error" && k != "code" {
			extra = append(extra, k)
		}
	}
	return message, code, extra, true
}

// matrixKinds names the media types a cell shows by a short kind.
var matrixKinds = map[string]string{
	"application/json":         "json",
	"text/plain":               "text",
	"text/event-stream":        "sse",
	"text/html":                "html",
	"image/png":                "png",
	"application/pdf":          "pdf",
	"text/csv":                 "csv",
	"application/xml":          "xml",
	"text/xml":                 "xml",
	"text/markdown":            "md",
	"application/zip":          "zip",
	"application/octet-stream": "bin",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       "xlsx",
}

// matrixKind is a cell's kind for an answer that is no JSON envelope.
func matrixKind(mediaType, ct string, hasBody bool) string {
	switch {
	case ct == "" && !hasBody:
		return "-"
	case ct == "":
		return "untyped"
	case matrixKinds[mediaType] != "":
		return matrixKinds[mediaType]
	case mediaType != "":
		return mediaType
	}
	return strconv.Quote(ct)
}

// render writes a cell, normalised.
func (c tourMatrixCell) render(n *tourNormaliser) string {
	s := strconv.Itoa(c.status)
	if c.kind == "" {
		if c.code != "" {
			s += " " + c.code
		}
		s += ": " + n.text(c.message)
		if len(c.extra) > 0 {
			s += " {+" + strings.Join(c.extra, ",") + "}"
		}
		return s
	}
	s += " " + c.kind
	if c.emptyTyp {
		s += " empty"
	}
	if c.message != "" {
		if c.code != "" {
			s += " " + c.code
		}
		s += ": " + n.text(c.message)
	}
	return s + c.count
}
