//go:build unix

package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

// TestBootSmoke boots the real server once per profile, probes it from
// outside, stops it with SIGTERM while a stream is open, and compares what it
// saw with testdata/boot/<profile>.txt (refactor plan §6.4 S4a; invariants
// I6 and I7, the default-profile parts of I8, I13 and I17, and quirk Q15).
// The probes, in order:
//
//   - /health against /api/v1/public/build;
//   - an allowed and a refused CORS preflight;
//   - the security headers, on every answer (200, 301, 401, 404, 405, 413);
//   - 401 before routing, for a known and for an unknown protected path;
//   - gorilla/mux's own 404 text, its empty 405 and its 301 path cleaning;
//   - the 32 MiB body cap, at and one byte over it, and its multipart
//     exemption; a 413 from an upload's own cap;
//   - gzip on a 1,400-byte answer but not on a 1,399-byte one or on
//     text/event-stream;
//   - /metrics with no token, a wrong one and the configured one, and the
//     route labels it has counted by then;
//   - the boot log, in order, and each request's access-log line;
//   - a SIGTERM with a notification stream open: how the stream ends, the
//     shutdown log, the exit status, and that it exits within the drain
//     timeout.
//
// A profile is a set of variables on top of the harness's fixed environment
// (harness_test.go). This test boots two: the default, and
// OPENV_METRICS_TOKEN set, which is the only way to see /metrics refuse a
// request. S4b's profiles, one per setting the plan lists, boot through the
// same runBootProfile in TestBootProfiles (boot_profiles_test.go).
func TestBootSmoke(t *testing.T) {
	if os.Getenv(testDatabaseURLEnv) == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	bin := serverBinary(t)
	t.Logf("built the server with -cover in %s", build.took.Round(time.Millisecond))
	for _, p := range bootProfiles {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			got := runBootProfile(t, bin, p, bootGoldenHeader)
			checkGolden(t, filepath.Join("testdata", "boot", p.name+".txt"), got, bootSmokeRegenerate)
			took := time.Since(start)
			if took > bootProfileBudget {
				t.Errorf("profile %s took %s to boot, probe and drain; the budget is %s (refactor plan §6.4 S4a)",
					p.name, took.Round(time.Millisecond), bootProfileBudget)
			}
			t.Logf("profile %s: booted, probed and drained in %s", p.name, took.Round(time.Millisecond))
		})
	}
}

// bootProfileBudget is the plan's bound on one profile's boot, probes and
// drain, the build excluded.
const bootProfileBudget = 60 * time.Second

const bootSmokeRegenerate = testDatabaseURLEnv + "=<server URL> " + updateGoldenEnv +
	"=1 go test ./cmd/server -count=1 -run '^TestBootSmoke$'"

// bootProfile is one boot: the variables it sets on top of the harness's
// fixed environment, and what they are for. Its golden is
// testdata/boot/<name>.txt.
type bootProfile struct {
	name  string
	about string
	env   map[string]string

	// The fields below serve S4b's profiles (boot_profiles_test.go). The S4a
	// profiles leave them zero, and then neither their boot nor their golden
	// differs from what S4a wrote.

	// proxy sets HTTP_PROXY and HTTPS_PROXY to a recordingProxy, shown as
	// proxyPlaceholder, and closes the golden with what it saw.
	proxy bool
	// async lists the messages this profile's boot logs from goroutines
	// besides bootAsyncMessages: they are awaited before the first probe
	// and listed apart, like those.
	async []string
	// signUp makes the account the later probes use on a profile whose
	// register answers without a session; the register probe then records
	// the refusal in full.
	signUp func(r *probeRun) *http.Cookie
	// extra probes run after bootProbes and before the drain.
	extra []probe
}

// metricsTestToken is OPENV_METRICS_TOKEN in the metrics_token profile, and
// the token every profile presents in the third /metrics request.
const metricsTestToken = "boot-harness-metrics-token"

var bootProfiles = []bootProfile{
	{name: "default", about: "nothing beyond the harness's fixed environment"},
	{name: "metrics_token", about: "/metrics behind a bearer token",
		env: map[string]string{"OPENV_METRICS_TOKEN": metricsTestToken}},
}

// The requests' fixed parts.
const (
	allowedOrigin    = "http://localhost:3000" // CORS_ORIGIN's default
	refusedOrigin    = "https://elsewhere.example"
	harnessEmail     = "boot-harness@example.com"
	harnessPassword  = "boot harness password 1"
	phantomArtifact  = "00000000-0000-4000-8000-000000000000"
	defaultBodyCapMB = 32 // OPENV_MAX_BODY_MB's default
	exportErrPrefix  = `{"error":"unsupported export format: `
	exportErrSuffix  = "\"}\n"
)

// probe is one numbered step of a boot.
type probe struct {
	name  string
	title string
	// stream marks a request whose access-log line is written when the
	// stream closes, which can be after the next probe has started.
	stream bool
	run    func(r *probeRun)
}

// probeRun is the state the probes share, and the lines one probe records.
type probeRun struct {
	t       *testing.T
	s       *serverProcess
	p       bootProfile
	db      testDatabase
	env     map[string]string // the variables the profile booted with, proxy included
	session *http.Cookie
	project string
	org     string // the account's workspace (S4b's billing probes)

	start int      // stderr offset when this probe began
	keys  []string // "METHOD path" of each request it made
	lines []string
	logs  []string
}

var bootProbes = []probe{
	{name: "health", title: "GET /health", run: func(r *probeRun) {
		r.recordText(r.send(r.request("GET", "/health", nil)))
	}},
	{name: "build", title: "GET /api/v1/public/build", run: func(r *probeRun) {
		r.recordText(r.send(r.request("GET", "/api/v1/public/build", nil)))
	}},
	{name: "preflight-allowed", title: "OPTIONS /api/v1/projects from the allowed origin", run: func(r *probeRun) {
		req := r.request("OPTIONS", "/api/v1/projects", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		r.recordText(r.send(req))
	}},
	{name: "preflight-refused", title: "OPTIONS /api/v1/projects from another origin", run: func(r *probeRun) {
		req := r.request("OPTIONS", "/api/v1/projects", nil)
		req.Header.Set("Origin", refusedOrigin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		r.recordText(r.send(req))
	}},
	{name: "auth-required", title: "GET /api/v1/projects with no session", run: func(r *probeRun) {
		r.recordText(r.send(r.request("GET", "/api/v1/projects", nil)))
	}},
	{name: "auth-before-routing", title: "GET /api/v1/no-such-route with no session", run: func(r *probeRun) {
		r.recordText(r.send(r.request("GET", "/api/v1/no-such-route", nil)))
	}},
	{name: "mux-404", title: "GET /api/v1/public/no-such-route (an open path)", run: func(r *probeRun) {
		r.recordText(r.send(r.request("GET", "/api/v1/public/no-such-route", nil)))
	}},
	{name: "mux-405", title: "DELETE /health", run: func(r *probeRun) {
		r.recordText(r.send(r.request("DELETE", "/health", nil)))
	}},
	{name: "mux-301", title: "GET /api/v1/public//build", run: func(r *probeRun) {
		r.recordText(r.send(r.request("GET", "/api/v1/public//build", nil)))
	}},
	{name: "register", title: "POST /api/v1/auth/register (the session the later probes use)", run: func(r *probeRun) {
		body, _ := json.Marshal(map[string]string{"email": harnessEmail, "password": harnessPassword, "name": "Boot Harness"})
		req := r.request("POST", "/api/v1/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, data := r.send(req)
		if r.p.signUp != nil {
			r.recordText(resp, data)
			r.session = r.p.signUp(r)
			return
		}
		r.recordHeadAs(resp, false)
		r.line("body not recorded (the new account; S5c pins it)")
		for _, c := range resp.Cookies() {
			if c.Name == "openv_session" {
				r.session = c
			}
		}
		if r.session == nil {
			r.t.Fatalf("registering gave no session cookie: %s %s\n%s", resp.Status, data, r.s.output())
		}
	}},
	{name: "project", title: "POST /api/v1/projects (the project the export probes use)", run: func(r *probeRun) {
		req := r.request("POST", "/api/v1/projects", strings.NewReader(`{"name":"Boot harness"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, data := r.send(r.withSession(req))
		r.recordHeadAs(resp, false)
		r.line("body not recorded (the new project; S5a pins it)")
		var p struct{ ID string }
		if err := json.Unmarshal(data, &p); err != nil || p.ID == "" {
			r.t.Fatalf("creating a project: %s %s\n%s", resp.Status, data, r.s.output())
		}
		r.project = p.ID
	}},
	{name: "body-cap-at", title: "POST /api/v1/auth/login, a JSON body of exactly the cap", run: func(r *probeRun) {
		r.loginWithBody(r.bodyCap())
	}},
	{name: "body-cap-over", title: "POST /api/v1/auth/login, a JSON body one byte over the cap", run: func(r *probeRun) {
		r.loginWithBody(r.bodyCap() + 1)
	}},
	{name: "multipart-exempt", title: "POST /api/v1/attachments/upload, a multipart body 1 MiB over the JSON cap, for a phantom artifact", run: func(r *probeRun) {
		body, size, ctype := multipartBody([][2]string{{"artifact_id", phantomArtifact}}, "file", "large.bin", "application/octet-stream", r.bodyCap()+1<<20)
		req := r.request("POST", "/api/v1/attachments/upload", body)
		req.ContentLength = size
		req.Header.Set("Content-Type", ctype)
		r.line("the handler answers only once it has read the whole form (r.FormValue); over the JSON cap, it would see no artifact_id")
		r.recordText(r.send(r.withSession(req)))
	}},
	{name: "upload-413", title: "POST /api/v1/me/avatar, a multipart body 1 KiB over the avatar upload's own 3 MiB bound", run: func(r *probeRun) {
		body, size, ctype := multipartBody(nil, "file", "avatar.png", "image/png", 3<<20+1<<10)
		req := r.request("POST", "/api/v1/me/avatar", body)
		req.ContentLength = size
		req.Header.Set("Content-Type", ctype)
		r.recordText(r.send(r.withSession(req)))
	}},
	{name: "gzip-1399", title: "a 1,399-byte answer accepting gzip (GET /api/v1/projects/{id}/export with an unknown format)", run: func(r *probeRun) {
		r.exportError(1399)
	}},
	{name: "gzip-1400", title: "a 1,400-byte answer accepting gzip (the same, one byte longer)", run: func(r *probeRun) {
		r.exportError(1400)
	}},
	{name: "gzip-event-stream", title: "GET /api/v1/notifications/stream accepting gzip", stream: true, run: func(r *probeRun) {
		req := r.request("GET", "/api/v1/notifications/stream", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp := r.open(r.withSession(req))
		r.recordHead(resp)
		r.line("body not read: the stream stays open until the client leaves")
		_ = resp.Body.Close()
		// The handler returns, and its request is counted and logged, only
		// once it notices the client left; wait for that, so the /metrics
		// probe next always counts it.
		r.waitForAccessLog("GET /api/v1/notifications/stream")
	}},
	{name: "metrics", title: "GET /metrics with no token, a wrong one and the configured one", run: func(r *probeRun) {
		labels := false
		for _, auth := range []struct{ what, header string }{
			{"no Authorization header", ""},
			{"Authorization: Bearer wrong-token", "Bearer wrong-token"},
			{"Authorization: Bearer " + metricsTestToken, "Bearer " + metricsTestToken},
		} {
			req := r.request("GET", "/metrics", nil)
			if auth.header != "" {
				req.Header.Set("Authorization", auth.header)
			}
			r.line("-- " + auth.what)
			resp, body := r.send(req)
			if resp.StatusCode != http.StatusOK {
				r.recordText(resp, body)
				continue
			}
			r.recordHeadAs(resp, false)
			r.line("body not recorded (Prometheus text)")
			if !labels {
				labels = true
				r.line("route labels counted so far (method, route, status):")
				for _, l := range routeLabels(body) {
					r.line("  " + l)
				}
			}
		}
	}},
}

// runBootProfile boots one profile, runs the probes (bootProbes, then the
// profile's extra ones) and the drain, and writes its golden text under
// header.
func runBootProfile(t *testing.T, bin string, p bootProfile, header string) []byte {
	env := p.env
	var proxy *recordingProxy
	if p.proxy {
		proxy = startRecordingProxy(t)
		env = proxy.env(p.env)
	}
	s, db := bootServer(t, bin, env)
	s.waitForAsyncLines(p.async)
	probes := append(append([]probe(nil), bootProbes...), p.extra...)
	shared := &probeRun{}
	var runs []*probeRun
	for _, pr := range probes {
		r := &probeRun{t: t, s: s, p: p, db: db, env: env, session: shared.session, project: shared.project, org: shared.org,
			start: s.stderr.Len()}
		pr.run(r)
		shared.session, shared.project, shared.org = r.session, r.project, r.org
		runs = append(runs, r)
	}
	drain := &probeRun{t: t, s: s, p: p, db: db, env: env, session: shared.session, start: s.stderr.Len()}
	sigterm, exitLines := runDrain(drain)
	if testing.Verbose() {
		logCoverage(t, s)
	}
	g := bootGolden{header: header, probes: probes, runs: runs, drain: drain, sigterm: sigterm, exit: exitLines, proxy: proxy}
	return g.render(t, s, db, p)
}

// logCoverage logs how much of cmd/server's code the boot ran, from the
// counters the -cover build wrote at exit (go test -v only).
func logCoverage(t *testing.T, s *serverProcess) {
	out, err := exec.Command(goTool(), "tool", "covdata", "percent", "-i", filepath.Join(s.tmp, "cover")).CombinedOutput()
	if err != nil {
		t.Logf("go tool covdata percent: %v\n%s", err, out)
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		i := strings.Index(line, "coverage:")
		if i < 0 || !strings.Contains(line, "/cmd/server") {
			continue
		}
		if f := strings.Fields(line[i+len("coverage:"):]); len(f) > 0 {
			t.Logf("this boot ran %s of cmd/server's statements", f[0])
		}
	}
}

// runDrain opens a notification stream, sends SIGTERM, and records how the
// stream ends and how the process exits. It returns the stderr offset at
// SIGTERM.
func runDrain(r *probeRun) (int, []string) {
	req := r.request("GET", "/api/v1/notifications/stream", nil)
	resp := r.open(r.withSession(req))
	r.recordHead(resp)
	type readResult struct {
		n   int64
		err error
	}
	read := make(chan readResult, 1)
	go func() {
		n, err := io.Copy(io.Discard, resp.Body)
		read <- readResult{n, err}
	}()
	sigterm := r.s.stderr.Len()
	code, took, err := r.s.terminate(30 * time.Second)
	if err != nil {
		r.t.Fatalf("%v\n%s", err, r.s.output())
	}
	select {
	case res := <-read:
		if res.err == nil {
			r.line(fmt.Sprintf("after SIGTERM: the stream ended cleanly (EOF) after %d more bytes", res.n))
		} else {
			r.line(fmt.Sprintf("after SIGTERM: the stream failed after %d more bytes: %v", res.n, res.err))
		}
	case <-time.After(10 * time.Second):
		r.line("after SIGTERM: the stream was still open when the process had exited")
	}
	_ = resp.Body.Close()
	within := "yes"
	if took >= 15*time.Second {
		within = "no"
	}
	return sigterm, []string{
		fmt.Sprintf("exit status %d", code),
		"exited within the 15 s drain timeout: " + within,
	}
}

// bootGolden is what a profile's golden is written from, besides the
// server's own output.
type bootGolden struct {
	header  string
	probes  []probe     // bootProbes, then the profile's extra ones
	runs    []*probeRun // one per probe
	drain   *probeRun
	sigterm int // the stderr offset at SIGTERM
	exit    []string
	proxy   *recordingProxy // nil unless the profile sets proxy
}

// withoutVectorWarning takes the no-vector warning out of a boot's log
// lines, after checking that it is there exactly as often as it should be:
// want times on a server without the vector extension, never on one with it.
func withoutVectorWarning(t *testing.T, s *serverProcess, db testDatabase, want int) []logLine {
	t.Helper()
	var kept []logLine
	vectorWarnings := 0
	for _, l := range s.logLines() {
		if strings.HasPrefix(l.text, noVectorWarning) {
			vectorWarnings++
			continue
		}
		kept = append(kept, l)
	}
	switch {
	case db.vector && vectorWarnings != 0:
		t.Errorf("the server has the vector extension, yet the boot log warns it is unavailable\n%s", s.output())
	case !db.vector && vectorWarnings != want:
		t.Errorf("the server lacks the vector extension, so the boot log should warn %d time(s) that it is unavailable; it did %d times\n%s",
			want, vectorWarnings, s.output())
	}
	return kept
}

// render sorts the server's log into the boot log, the lines of each probe
// and the shutdown log, and writes the golden text.
func (g bootGolden) render(t *testing.T, s *serverProcess, db testDatabase, p bootProfile) []byte {
	kept := withoutVectorWarning(t, s, db, 1)
	runs, drain, sigterm := g.runs, g.drain, g.sigterm

	all := append(append([]*probeRun(nil), runs...), drain)
	claimed := map[int]bool{}
	for i, r := range all {
		stream := r == drain || g.probes[i].stream
		if !stream {
			continue
		}
		for _, key := range r.keys {
			for j, l := range kept {
				if !claimed[j] && l.offset >= r.start && accessKey(l.text) == key {
					claimed[j] = true
					r.logs = append(r.logs, l.text)
					break
				}
			}
		}
	}
	var boot, async, shutdown []string
	for j, l := range kept {
		switch {
		case claimed[j]:
		case isAsync(l.text, p.async):
			async = append(async, l.text)
		case l.offset < all[0].start:
			boot = append(boot, l.text)
		case l.offset >= sigterm:
			shutdown = append(shutdown, l.text)
		default:
			owner := all[0]
			for _, r := range all {
				if r.start <= l.offset {
					owner = r
				}
			}
			owner.logs = append(owner.logs, l.text)
		}
	}
	sort.Strings(async)

	var w goldenWriter
	w.WriteString(g.header)
	w.profileHead("profile", p.name, p.about, p.env, g.proxy != nil)
	w.section("boot log, in order", boot)
	w.section("boot log lines from goroutines, sorted", async)
	w.section("stdout", stdoutLines(s))
	for i, r := range runs {
		w.section("probe "+g.probes[i].name+": "+g.probes[i].title, append(r.lines, logSection(r.logs)...))
	}
	w.section("drain: SIGTERM with GET /api/v1/notifications/stream open", append(drain.lines, logSection(drain.logs)...))
	w.section("shutdown log, in order", shutdown)
	w.section("exit", g.exit)
	if g.proxy != nil {
		w.section("outbound requests (the recording proxy, after exit)", g.proxy.summary())
	}
	return []byte(w.String())
}

// goldenWriter writes a boot golden's text.
type goldenWriter struct{ strings.Builder }

// profileHead writes the line naming the boot and the variables it set on
// top of the harness's, sorted; with proxy, HTTP_PROXY and HTTPS_PROXY
// among them, as proxyPlaceholder.
func (w *goldenWriter) profileHead(kind, name, about string, env map[string]string, proxy bool) {
	fmt.Fprintf(w, "== %s %s: %s\n", kind, name, about)
	shown := map[string]string{}
	for k, v := range env {
		shown[k] = v
	}
	if proxy {
		shown["HTTP_PROXY"], shown["HTTPS_PROXY"] = proxyPlaceholder, proxyPlaceholder
	}
	if len(shown) == 0 {
		w.WriteString("(no variables beyond the harness's)\n")
	}
	keys := make([]string, 0, len(shown))
	for k := range shown {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := shown[k]
		// A value with spaces at an end, or with a control character such as
		// a line break, is shown Go-quoted, so that it stays on its line and
		// its ends can be seen.
		if strings.TrimSpace(v) != v || strings.ContainsFunc(v, unicode.IsControl) {
			v = strconv.Quote(v)
		}
		fmt.Fprintf(w, "%s=%s\n", k, v)
	}
}

// section writes a titled block of lines, or (none).
func (w *goldenWriter) section(title string, lines []string) {
	fmt.Fprintf(w, "\n== %s\n", title)
	if len(lines) == 0 {
		w.WriteString("(none)\n")
	}
	for _, l := range lines {
		w.WriteString(l + "\n")
	}
}

// stdoutLines is what the server wrote to standard output, normalised.
func stdoutLines(s *serverProcess) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(string(s.stdout.Bytes()), "\n"), "\n") {
		if l != "" {
			out = append(out, s.normaliseLine(l))
		}
	}
	return out
}

func logSection(logs []string) []string {
	if len(logs) == 0 {
		return []string{"log (none)"}
	}
	out := make([]string, len(logs))
	for i, l := range logs {
		out[i] = "log " + l
	}
	return out
}

const bootGoldenHeader = `# The server binary (go build -cover ./cmd/server) booted on a fresh
# database and probed from outside (refactor plan §6.4 S4a; invariants I6,
# I7, I8, I13, I17; quirk Q15). Written by TestBootSmoke
# (cmd/server/boot_smoke_test.go); the fixed environment and the normalisers
# are described in cmd/server/harness_test.go. Regenerate with
#   OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./cmd/server -count=1 -run '^TestBootSmoke$'
# against a server with or without the vector extension.
# Normalised: log timestamps dropped; durations, ports, the release version,
# UUIDs, the temporary directory, the session token and its expiry replaced
# by <...>; the Date header dropped; a no-vector server's warning taken out.

`

// ----------------------------------------------------------------------------
// Requests and what is recorded of them

func (r *probeRun) line(text string) { r.lines = append(r.lines, text) }

func (r *probeRun) request(method, path string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, r.s.base+path, body)
	if err != nil {
		r.t.Fatal(err)
	}
	return req
}

func (r *probeRun) withSession(req *http.Request) *http.Request {
	if r.session == nil {
		r.t.Fatal("a probe needs the session the register probe creates")
	}
	req.AddCookie(&http.Cookie{Name: r.session.Name, Value: r.session.Value})
	return req
}

// open sends a request and returns once the headers are in, noting its
// access-log key.
func (r *probeRun) open(req *http.Request) *http.Response {
	r.t.Helper()
	r.keys = append(r.keys, req.Method+" "+uuidRE.ReplaceAllString(req.URL.Path, "<uuid>"))
	resp, err := r.s.client().Do(req)
	if err != nil {
		r.t.Fatalf("%s %s: %v\n%s", req.Method, req.URL.Path, err, r.s.output())
	}
	return resp
}

// send makes a request and reads the whole answer.
func (r *probeRun) send(req *http.Request) (*http.Response, []byte) {
	r.t.Helper()
	ctx, cancel := probeContext()
	defer cancel()
	resp := r.open(req.WithContext(ctx))
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		r.t.Fatalf("read the answer to %s %s: %v\n%s", req.Method, req.URL.Path, err, r.s.output())
	}
	return resp, body
}

// recordHead records the status line, the transfer encoding and every
// header but Date, sorted, normalised. The Content-Length of a body that is
// not recorded as it was sent (gzip, whose bytes depend on the Go version's
// compressor, or a body the probe leaves out) is not recorded either.
func (r *probeRun) recordHead(resp *http.Response) {
	r.recordHeadAs(resp, resp.Header.Get("Content-Encoding") == "")
}

func (r *probeRun) recordHeadAs(resp *http.Response, lengthRecorded bool) {
	r.line("status " + resp.Status)
	if resp.Close {
		r.line("connection close (the server ends the connection after this answer)")
	}
	if len(resp.TransferEncoding) > 0 {
		r.line("transfer-encoding " + strings.Join(resp.TransferEncoding, ", "))
	}
	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		if name != "Date" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		for _, v := range resp.Header[name] {
			if name == "Content-Length" && !lengthRecorded {
				v = "<not recorded>"
			}
			r.line("header " + name + ": " + normaliseHeader(name, v))
		}
	}
}

// recordText records an answer and its body: in full, quoted, when it is
// short; otherwise its length, digest and first bytes. A gzip body is
// recorded decoded.
func (r *probeRun) recordText(resp *http.Response, body []byte) {
	r.recordHead(resp)
	what := "body"
	if resp.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			r.t.Fatalf("a gzip answer that does not decode: %v", err)
		}
		decoded, err := io.ReadAll(zr)
		if err != nil {
			r.t.Fatalf("a gzip answer that does not decode: %v", err)
		}
		what, body = "body (gzip-decoded)", decoded
	}
	text := r.s.normaliseText(string(body))
	switch {
	case len(body) == 0:
		r.line(what + " empty")
	case len(text) <= 200:
		r.line(what + " " + strconv.Quote(text))
	default:
		sum := sha256.Sum256(body)
		r.line(fmt.Sprintf("%s %d bytes, sha256 %s, beginning %s", what, len(body),
			hex.EncodeToString(sum[:8]), strconv.Quote(text[:60])))
	}
}

// waitForAccessLog waits until the server has logged a request with key
// ("METHOD path") since this probe began.
func (r *probeRun) waitForAccessLog(key string) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, l := range r.s.logLines() {
			if l.offset >= r.start && accessKey(l.text) == key {
				return
			}
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("the server did not log %s within 10 s\n%s", key, r.s.output())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// bodyCap is the JSON body cap the profile runs with: OPENV_MAX_BODY_MB
// read by internal/envparse's rule, a whole number above 0 once trimmed.
func (r *probeRun) bodyCap() int64 {
	mb := int64(defaultBodyCapMB)
	if v, ok := r.p.env["OPENV_MAX_BODY_MB"]; ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			mb = n
		}
	}
	return mb << 20
}

// loginWithBody posts a JSON body of exactly size bytes to the login route:
// spaces, then {}. The decoder has to read to the end to find the value, so
// a body over the cap fails to decode, and one at the cap is a login with
// no credentials.
func (r *probeRun) loginWithBody(size int64) {
	body := append(bytes.Repeat([]byte(" "), int(size)-2), '{', '}')
	req := r.request("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.line(fmt.Sprintf("request body %d bytes", size))
	r.recordText(r.send(req))
}

// exportError asks the export route for a format made of size-37-3 letters,
// which it refuses with a JSON error of exactly size bytes.
func (r *probeRun) exportError(size int) {
	pad := strings.Repeat("a", size-len(exportErrPrefix)-len(exportErrSuffix))
	req := r.request("GET", "/api/v1/projects/"+r.project+"/export?format="+pad, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, body := r.send(r.withSession(req))
	r.recordText(resp, body)
	decoded := body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err == nil {
			decoded, _ = io.ReadAll(zr)
		}
	}
	if string(decoded) == exportErrPrefix+pad+exportErrSuffix {
		r.line(fmt.Sprintf("the body is the export route's error for the %d-letter format, %d bytes", len(pad), size))
	} else {
		r.line("the body is not the export route's error for the format asked for")
	}
}

// zeros reads zero bytes forever.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// multipartBody is a multipart form: the fields, then one file part of size
// zero bytes. It streams the file rather than holding it.
func multipartBody(fields [][2]string, fileField, fileName, fileType string, size int64) (io.Reader, int64, string) {
	var head bytes.Buffer
	mw := multipart.NewWriter(&head)
	for _, f := range fields {
		_ = mw.WriteField(f[0], f[1])
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, fileName))
	h.Set("Content-Type", fileType)
	if _, err := mw.CreatePart(h); err != nil {
		panic(err)
	}
	tail := "\r\n--" + mw.Boundary() + "--\r\n"
	total := int64(head.Len()) + size + int64(len(tail))
	body := io.MultiReader(bytes.NewReader(head.Bytes()), io.LimitReader(zeros{}, size), strings.NewReader(tail))
	return body, total, mw.FormDataContentType()
}

var metricsLineRE = regexp.MustCompile(`^http_requests_total\{(.*)\} \S+$`)
var labelRE = regexp.MustCompile(`(\w+)="((?:[^"\\]|\\.)*)"`)

// routeLabels lists the distinct method, route and status label sets of
// http_requests_total, sorted, without their counts.
func routeLabels(body []byte) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		m := metricsLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		labels := map[string]string{}
		for _, kv := range labelRE.FindAllStringSubmatch(m[1], -1) {
			labels[kv[1]] = kv[2]
		}
		seen[labels["method"]+" "+labels["route"]+" "+labels["status"]] = true
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
