package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// The worker wire (refactor plan step S7, invariant I12): every request
// runner.Client sends and how it reads every answer, pinned per method in
// testdata/wire/<method>.json. Deployed runners talk to newer APIs and new
// runners to older ones, so a changed path, header, body byte or decode is a
// break of the runner protocol's compatibility promise (REQ-143), never a
// refactor. Each golden records, per call: the requests (the route they
// resolve to in the API's inventory, the path, the credential, the content
// type and the exact body bytes), the canned responses, and what the method
// returned. Ids, keys and timestamps appear as <tokens> (see wireTokens).

// wireGoldenDir holds one golden per Client method.
const wireGoldenDir = "testdata/wire"

// wireResponse is one canned answer. body is written with tokens; the stub
// serves it with the real values substituted.
type wireResponse struct {
	status int
	body   string
}

// wireCall is one method call; do makes it and returns what the method
// decoded (nil for a method that returns only an error).
type wireCall struct {
	desc string
	do   func(c *Client) (interface{}, error)
}

// wireCase runs its calls in order on one fresh client, against a stub that
// answers each request with the next canned response.
type wireCase struct {
	name      string
	key       string // the credential token the client holds; <worker-key> when empty
	responses []wireResponse
	calls     []wireCall
}

// The golden file shape.
type wireGolden struct {
	Method string           `json:"method"`
	Cases  []wireGoldenCase `json:"cases"`
}

type wireGoldenCase struct {
	Name  string           `json:"name"`
	Calls []wireGoldenCall `json:"calls"`
}

type wireGoldenCall struct {
	Call     string               `json:"call"`
	HTTP     []wireGoldenExchange `json:"http"`
	Returned json.RawMessage      `json:"returned"`
	Error    *string              `json:"error"`
}

type wireGoldenExchange struct {
	Request  wireGoldenRequest  `json:"request"`
	Response wireGoldenResponse `json:"response"`
}

type wireGoldenRequest struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	Route         string `json:"route"`
	Authorization string `json:"authorization"`
	ContentType   string `json:"content_type"`
	Body          string `json:"body"`
}

type wireGoldenResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// TestRunnerWireGolden pins every runner.Client request and decode. A
// retargeted path, a renamed or retyped body field, a changed header, a
// dropped legacy fallback or a different decode fails here.
func TestRunnerWireGolden(t *testing.T) {
	routes := loadRouteInventory(t)
	all := wireGoldenCases()
	methods := make([]string, 0, len(all))
	for m := range all {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			t.Parallel() // the retry cases wait out one backoff
			doc := wireGolden{Method: "Client." + method}
			for _, wc := range all[method] {
				doc.Cases = append(doc.Cases, runWireCase(t, routes, wc))
			}
			checkGolden(t, wireGoldenFile(method), encodeGolden(t, doc), "TestRunnerWireGolden")
		})
	}
}

// TestRunnerWireGoldenCoversEveryClientMethod keeps the goldens complete:
// every exported Client method has cases, and every golden file belongs to
// a method.
func TestRunnerWireGoldenCoversEveryClientMethod(t *testing.T) {
	all := wireGoldenCases()
	typ := reflect.TypeOf((*Client)(nil))
	want := map[string]bool{}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		want[filepath.Base(wireGoldenFile(name))] = true
		if len(all[name]) == 0 {
			t.Errorf("Client.%s has no wire golden cases: add them to wireGoldenCases", name)
		}
	}
	for name := range all {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("wireGoldenCases has cases for %q, which is not a Client method", name)
		}
	}
	files, err := filepath.Glob(filepath.Join(wireGoldenDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !want[filepath.Base(f)] {
			t.Errorf("%s/%s pins no Client method: delete it", "internal/runner", filepath.ToSlash(f))
		}
	}
}

// wireGoldenFile names a method's golden: PushLogs -> testdata/wire/push_logs.json.
func wireGoldenFile(method string) string {
	var b strings.Builder
	for i, r := range method {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return filepath.Join(wireGoldenDir, b.String()+".json")
}

// runWireCase runs one case against a fresh stub and client.
func runWireCase(t *testing.T, routes []string, wc wireCase) wireGoldenCase {
	t.Helper()
	stub := &wireStub{responses: wc.responses}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	key := wc.key
	if key == "" {
		key = "<worker-key>"
	}
	client := NewClient(srv.URL, wireDenormalise(key))

	out := wireGoldenCase{Name: wc.name}
	for _, call := range wc.calls {
		before := stub.count()
		returned, err := call.do(client)
		got := wireGoldenCall{
			Call:     call.desc,
			HTTP:     stub.exchanges(t, routes, before),
			Returned: wireReturned(t, returned),
		}
		if err != nil {
			msg := wireNormalise(err.Error())
			got.Error = &msg
		}
		out.Calls = append(out.Calls, got)
	}
	if n := stub.count(); n != len(wc.responses) {
		t.Errorf("case %q: the client made %d requests for %d canned responses", wc.name, n, len(wc.responses))
	}
	return out
}

// wireReturned is what a method returned, as normalised JSON with object
// keys sorted and null-valued keys dropped. "returned" pins the decoded
// values, not the Go declaration order or omitempty of decode-only types, so
// aliasing them to workerproto (refactor plan step P3) keeps it unchanged,
// while a changed JSON tag or decoded value still fails. A method that
// returned nil records null.
func wireReturned(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode the returned value: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep number literals exact
	var tree interface{}
	if err := dec.Decode(&tree); err != nil {
		t.Fatalf("re-read the returned value: %v", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(dropNullKeys(tree)); err != nil { // map keys come out sorted
		t.Fatalf("encode the returned value: %v", err)
	}
	return json.RawMessage(wireNormalise(strings.TrimSpace(buf.String())))
}

// dropNullKeys removes every object key whose value is null, at any depth.
// Array elements and a top-level null stay.
func dropNullKeys(v interface{}) interface{} {
	switch v := v.(type) {
	case map[string]interface{}:
		for key, item := range v {
			if item == nil {
				delete(v, key)
				continue
			}
			v[key] = dropNullKeys(item)
		}
	case []interface{}:
		for i, item := range v {
			v[i] = dropNullKeys(item)
		}
	}
	return v
}

// wireStub answers each request with the next canned response and keeps
// what it saw.
type wireStub struct {
	responses []wireResponse

	mu   sync.Mutex
	seen []wireSeen
}

type wireSeen struct {
	method, uri, auth, contentType, body string
	resp                                 wireResponse
}

func (s *wireStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	resp := wireResponse{status: http.StatusTeapot, body: "no canned response for this request\n"}
	if n := len(s.seen); n < len(s.responses) {
		resp = s.responses[n]
	}
	s.seen = append(s.seen, wireSeen{
		method:      r.Method,
		uri:         r.URL.RequestURI(),
		auth:        r.Header.Get("Authorization"),
		contentType: r.Header.Get("Content-Type"),
		body:        string(body),
		resp:        resp,
	})
	s.mu.Unlock()
	if resp.body != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.status)
	_, _ = io.WriteString(w, wireDenormalise(resp.body))
}

func (s *wireStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

// exchanges returns what the stub saw from request index from on, with the
// values normalised and each request resolved against the route inventory.
func (s *wireStub) exchanges(t *testing.T, routes []string, from int) []wireGoldenExchange {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []wireGoldenExchange{}
	for _, seen := range s.seen[from:] {
		path := wireNormalise(seen.uri)
		route, ok := matchRoute(routes, seen.method, path, isWireToken)
		if !ok {
			t.Errorf("the client sent %s %s, which is not a route in %s", seen.method, path, routeInventoryShown)
			route = "(not in " + routeInventoryShown + ")"
		}
		out = append(out, wireGoldenExchange{
			Request: wireGoldenRequest{
				Method:        seen.method,
				Path:          path,
				Route:         route,
				Authorization: wireNormalise(seen.auth),
				ContentType:   seen.contentType,
				Body:          wireNormalise(seen.body),
			},
			Response: wireGoldenResponse{Status: seen.resp.status, Body: seen.resp.body},
		})
	}
	return out
}

// isWireToken reports whether a normalised path segment is an id token.
func isWireToken(seg string) bool {
	return len(seg) > 2 && strings.HasPrefix(seg, "<") && strings.HasSuffix(seg, ">")
}

func wireNormalise(s string) string {
	pairs := make([]string, 0, 2*len(wireTokens))
	for _, tk := range wireTokens {
		pairs = append(pairs, tk.value, tk.token)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

func wireDenormalise(s string) string {
	pairs := make([]string, 0, 2*len(wireTokens))
	for _, tk := range wireTokens {
		pairs = append(pairs, tk.token, tk.value)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// --- Golden files, diffs and the route inventory. internal/mcp keeps the
// same helpers for its goldens; test files cannot be shared across packages.

// updateGoldenEnv set to exactly 1 rewrites the goldens from the current code
// instead of comparing against them; any other value compares. Only a
// deliberate behavior change regenerates a golden; a refactor never does.
const updateGoldenEnv = "UPDATE_GOLDEN"

func updatingGoldens() bool { return os.Getenv(updateGoldenEnv) == "1" }

// regenerateCommand is the one command that rewrites the golden a test owns,
// with a reminder that no other value of UPDATE_GOLDEN does.
func regenerateCommand(test string) string {
	return updateGoldenEnv + "=1 go test ./internal/mcp ./internal/runner -run " + test +
		"\n(only " + updateGoldenEnv + "=1 regenerates; any other value compares)"
}

// checkGolden compares got with the golden file at path (relative to this
// package), or rewrites the file when UPDATE_GOLDEN is 1.
func checkGolden(t *testing.T, path string, got []byte, test string) {
	t.Helper()
	shown := "internal/runner/" + filepath.ToSlash(path)
	if updatingGoldens() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the directory for %s: %v", shown, err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", shown, err, regenerateCommand(test))
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s does not match the code:\n%s\n"+
			"A refactor never changes a golden. If this is a deliberate behavior change, regenerate it with:\n  %s",
			shown, goldenDiff(string(want), string(got)), regenerateCommand(test))
	}
}

// encodeGolden renders a JSON golden: two-space indent, no HTML escaping, and
// a trailing newline.
func encodeGolden(t *testing.T, v interface{}) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	return buf.Bytes()
}

// goldenDiffLimit caps the diff a failure prints.
const goldenDiffLimit = 60

// goldenDiff is a compact line diff of the golden (want) against the current
// output (got): changed lines marked - and +, each run with two lines of
// context, headed by the golden's line number.
func goldenDiff(want, got string) string {
	ops := diffLines(strings.Split(want, "\n"), strings.Split(got, "\n"))
	show := make([]bool, len(ops))
	changed := 0
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		changed++
		for k := max(0, i-2); k <= min(len(ops)-1, i+2); k++ {
			show[k] = true
		}
	}
	var b strings.Builder
	printed := 0
	for i, op := range ops {
		if !show[i] {
			continue
		}
		if printed == goldenDiffLimit {
			fmt.Fprintf(&b, "... (diff truncated: %d changed lines in all)\n", changed)
			break
		}
		if i == 0 || !show[i-1] {
			fmt.Fprintf(&b, "@@ golden line %d @@\n", op.line)
		}
		fmt.Fprintf(&b, "%c %s\n", op.kind, op.text)
		printed++
	}
	return b.String()
}

// diffOp is one line of a diff: kind is ' ' (both), '-' (golden only) or '+'
// (current output only); line is the golden's line number at that point.
type diffOp struct {
	kind byte
	text string
	line int
}

// diffLines is a longest-common-subsequence line diff.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i], i + 1})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', a[i], i + 1})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j], i + 1})
			j++
		}
	}
	return ops
}

// routeInventoryFile is the API's route list, one "METHOD /path/{param}" per
// line, pinned by TestRouteInventoryIsBackwardCompatible in internal/api. It
// is read as a file: this package does not import internal/api.
const routeInventoryFile = "../api/testdata/routes.txt"

// routeInventoryShown is how failure messages name the inventory.
const routeInventoryShown = "internal/api/testdata/routes.txt"

func loadRouteInventory(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(routeInventoryFile))
	if err != nil {
		t.Fatalf("read the route inventory %s: %v", routeInventoryShown, err)
	}
	var routes []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			routes = append(routes, line)
		}
	}
	return routes
}

// matchRoute returns the inventory line a request resolves to. isID says
// which path segments carry an id: a template matches when its literal
// segments equal the request's and its {parameters} sit exactly on the id
// segments.
func matchRoute(routes []string, method, path string, isID func(string) bool) (string, bool) {
	path, _, _ = strings.Cut(path, "?")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for _, route := range routes {
		m, tmpl, ok := strings.Cut(route, " ")
		if !ok || m != method {
			continue
		}
		tsegs := strings.Split(strings.Trim(tmpl, "/"), "/")
		if len(tsegs) != len(segs) {
			continue
		}
		match := true
		for i, ts := range tsegs {
			param := strings.HasPrefix(ts, "{") && strings.HasSuffix(ts, "}")
			if param != isID(segs[i]) || (!param && ts != segs[i]) {
				match = false
				break
			}
		}
		if match {
			return route, true
		}
	}
	return "", false
}
