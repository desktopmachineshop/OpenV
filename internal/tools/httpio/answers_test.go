package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// answersDriver serves each fixture case through the original and the
// rewritten package, and requires the same answer three ways: the calls the
// handler makes on its writer, in order, with the header as it stood at
// each (so any wrapper sees the same); the response on the wire through a
// real server (status, every header but Date, the body bytes); and the
// same through a gzip middleware like internal/api's compression, with the
// response read compressed.
const answersDriver = `package answers_test

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	orig "example.com/fixture/orig"
	rewritten "example.com/fixture/rewritten"
)

type traceWriter struct {
	h   http.Header
	log []string
}

func (t *traceWriter) Header() http.Header { return t.h }

func (t *traceWriter) WriteHeader(code int) {
	t.log = append(t.log, fmt.Sprintf("WriteHeader(%d) %s", code, headerText(t.h, false)))
}

func (t *traceWriter) Write(b []byte) (int, error) {
	t.log = append(t.log, fmt.Sprintf("Write(%q) %s", b, headerText(t.h, false)))
	return len(b), nil
}

func headerText(h http.Header, dropDate bool) string {
	var keys []string
	for k := range h {
		if !(dropDate && k == "Date") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "[%s: %q]", k, h[k])
	}
	return b.String()
}

func request(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	return r
}

func trace(h http.HandlerFunc, method, target, body string) string {
	tw := &traceWriter{h: http.Header{}}
	h(tw, request(method, target, body))
	return strings.Join(tw.log, "\n")
}

type gzipWriter struct {
	http.ResponseWriter
	zw *gzip.Writer
}

func (g *gzipWriter) Write(b []byte) (int, error) { return g.zw.Write(b) }

func gzipped(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		zw := gzip.NewWriter(w)
		defer zw.Close()
		h(&gzipWriter{ResponseWriter: w, zw: zw}, r)
	}
}

func wire(t *testing.T, h http.HandlerFunc, method, target, body string, gz bool) string {
	srv := httptest.NewServer(gzipped(h))
	defer srv.Close()
	req, err := http.NewRequest(method, srv.URL+target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if gz {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d %s\n%q", resp.StatusCode, headerText(resp.Header, true), data)
}

func TestAnswersAlike(t *testing.T) {
	if len(orig.Cases) != len(rewritten.Cases) {
		t.Fatalf("%d cases before, %d after", len(orig.Cases), len(rewritten.Cases))
	}
	for i, before := range orig.Cases {
		after := rewritten.Cases[i]
		if before.Name != after.Name {
			t.Fatalf("case %d: %q before, %q after", i, before.Name, after.Name)
		}
		t.Run(before.Name, func(t *testing.T) {
			b := trace(before.Handler, before.Method, before.Target, before.Body)
			a := trace(after.Handler, after.Method, after.Target, after.Body)
			if a != b {
				t.Errorf("writer calls differ\n--- before\n%s\n--- after\n%s", b, a)
			}
			for _, gz := range []bool{false, true} {
				b := wire(t, before.Handler, before.Method, before.Target, before.Body, gz)
				a := wire(t, after.Handler, after.Method, after.Target, after.Body, gz)
				if a != b {
					t.Errorf("gzip %v: responses differ\n--- before\n%s\n--- after\n%s", gz, b, a)
				}
				if !gz {
					t.Logf("%s", a)
				}
			}
		})
	}
}

// TestHelperContract pins what the helpers answer, so a change to their
// contract shows here and not only as a difference between two copies.
func TestHelperContract(t *testing.T) {
	byName := map[string]rewritten.Case{}
	for _, c := range rewritten.Cases {
		byName[c.Name] = c
	}
	want := map[string]string{
		"create":          "201 [Content-Length: [\"57\"]][Content-Type: [\"application/json\"]]\n\"{\\\"id\\\":\\\"2\\\",\\\"title\\\":\\\"\\\\u003cx\\\\u003e \\\\u0026 y\\\",\\\"tags\\\":[\\\"t\\\"]}\\n\"",
		"create bad body": "400 [Content-Length: [\"33\"]][Content-Type: [\"application/json\"]]\n\"{\\\"error\\\":\\\"invalid request body\\\"}\\n\"",
		"rename bad body": "400 [Content-Length: [\"33\"]][Content-Type: [\"application/json\"]]\n\"{\\\"error\\\":\\\"Invalid request body\\\"}\\n\"",
		"accept later":    "202 [Content-Length: [\"20\"]][Content-Type: [\"text/plain; charset=utf-8\"]]\n\"{\\\"status\\\":\\\"queued\\\"}\\n\"",
		"bare empty":      "200 [Content-Length: [\"5\"]][Content-Type: [\"text/plain; charset=utf-8\"]]\n\"null\\n\"",
		"get":             "200 [Content-Length: [\"138\"]][Content-Type: [\"application/json\"]]\n\"{\\\"item\\\":{\\\"id\\\":\\\"1\\\",\\\"title\\\":\\\"\\\\u003cb\\\\u003eone\\\\u003c/b\\\\u003e \\\\u0026 more\\\",\\\"tags\\\":null},\\\"upper\\\":\\\"\\\\u003cB\\\\u003eONE\\\\u003c/B\\\\u003e \\\\u0026 MORE\\\"}\\n\"",
	}
	for name, w := range want {
		c := byName[name]
		if got := wire(t, c.Handler, c.Method, c.Target, c.Body, false); got != w {
			t.Errorf("%s:\n got %s\nwant %s", name, got, w)
		}
	}
}
`

// TestFixtureAnswersAlike serves every fixture case before and after the
// rewrite (answersDriver) in a module of its own, with the fixture's
// respond.go holding helperSource's helpers, and requires identical writer
// calls and identical responses, plain and gzipped. It also vets the
// rewritten package.
func TestFixtureAnswersAlike(t *testing.T) {
	out, err := serveBoth(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "--- PASS: TestAnswersAlike/"); n != len(fixtureCaseNames(t)) {
		t.Errorf("%d cases passed, want %d:\n%s", n, len(fixtureCaseNames(t)), out)
	}
}

// TestAnswersDriverCatchesADifference breaks the rewritten copy's helpers
// after the rewrite, in ways the S5 tour would see, and requires the
// driver to fail on each: it is not comparing nothing.
func TestAnswersDriverCatchesADifference(t *testing.T) {
	cases := map[string][2]string{
		"an explicit 200": {"func writeJSONOK(w http.ResponseWriter, v any) {\n", "func writeJSONOK(w http.ResponseWriter, v any) {\n\tw.WriteHeader(http.StatusOK)\n"},
		"no HTML escaping": {"func writeJSONBare(w http.ResponseWriter, v any) {\n\t_ = json.NewEncoder(w).Encode(v)",
			"func writeJSONBare(w http.ResponseWriter, v any) {\n\tenc := json.NewEncoder(w)\n\tenc.SetEscapeHTML(false)\n\t_ = enc.Encode(v)"},
		"a Content-Type on a bare status": {"func writeJSONBareStatus(w http.ResponseWriter, status int, v any) {\n",
			"func writeJSONBareStatus(w http.ResponseWriter, status int, v any) {\n\tw.Header().Set(\"Content-Type\", \"application/json\")\n"},
		"another message": {`const invalidRequestBody = "invalid request body"`, `const invalidRequestBody = "invalid request"`},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := serveBoth(t, map[string][2]string{"respond.go": edit})
			if err == nil || !strings.Contains(out, "--- FAIL: TestAnswersAlike/") {
				t.Errorf("the driver passed a broken helper (%v):\n%s", err, out)
			}
		})
	}
}

// serveBoth rewrites the fixture, applies edits to the rewritten copy,
// and runs answersDriver on the two in a module of its own; it returns the
// test output and the go command's error.
func serveBoth(t *testing.T, edits map[string][2]string) (string, error) {
	t.Helper()
	root := fixtureRoot(t, nil)
	if code, _, stderr := runTool(t, root, "api"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	mod := t.TempDir()
	writeFile(t, filepath.Join(mod, "go.mod"), "module example.com/fixture\n\ngo 1.22\n")
	for _, name := range fixtureNames(t) {
		src := readFile(t, filepath.Join(root, "api", name))
		if e, ok := edits[name]; ok {
			if !strings.Contains(src, e[0]) {
				t.Fatalf("%s does not contain %q", name, e[0])
			}
			src = strings.Replace(src, e[0], e[1], 1)
		}
		writeFile(t, filepath.Join(mod, "orig", name), readFile(t, filepath.Join("testdata", "fixture", name)))
		writeFile(t, filepath.Join(mod, "rewritten", name), src)
	}
	writeFile(t, filepath.Join(mod, "answers_test.go"), answersDriver)
	goCmd(t, mod, "vet", "./...")
	return goOut(mod, "test", "-count=1", "-v", "-run", "^(TestAnswersAlike|TestHelperContract)$", ".")
}

// fixtureCaseNames lists the fixture's cases, read from cases.go.
func fixtureCaseNames(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "fixture", "cases.go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `{"`) {
			names = append(names, line)
		}
	}
	return names
}
