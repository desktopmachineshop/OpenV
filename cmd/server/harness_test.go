//go:build unix

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The black-box boot harness (refactor plan §6.4 S4a): it builds the real
// server binary once per test process with coverage instrumentation, boots
// it against a database of its own, talks to it over HTTP from outside, and
// stops it with SIGTERM. Nothing inside the process is reached into, so no
// rewiring of main() or of the middleware can fool it. boot_smoke_test.go
// holds the probes and the profiles; this file holds the process, its
// environment, and the normalisers that make a run repeatable.
//
// The database. OPENV_TEST_DATABASE_URL names a throwaway server (URL form,
// e.g. postgres://openv@localhost:5432/postgres?sslmode=disable), as for the
// Postgres package's tests; every boot creates a database of its own there
// with a random name and drops it when the test ends, so boots never share
// state and a server without the vector extension (CI's plain postgres:15
// leg) works as well as one with it. Unset, the boot tests skip.
//
// The environment. The server gets only the variables below, built from
// nothing: no OPENV_*, DATABASE_URL, DB_*, SMTP, VAPID, SSO, STRIPE, proxy or
// other variable of the developer's or CI's shell reaches it.
//
//	PATH=/usr/bin:/bin     the server runs no program in these profiles
//	HOME, TMPDIR           temporary directories of the test
//	DATABASE_URL           the boot's own database
//	PORT                   a free port (listen on :0, close, hand it over)
//	UPLOADS_DIR            a temporary directory (the default ./uploads
//	OPENV_DATA_DIR           and ./data would write into the repository)
//	HOSTED_RUNNERS=off     never dial a Docker daemon: whether one runs on
//	                       the machine would otherwise change the boot log
//	GOCOVERDIR             a temporary directory for the -cover counters
//
// plus a profile's own variables. The working directory is the repository
// root, as in the image, where the project templates are read from
// examples/. No SMTP, VAPID or SSO variable is set, so mail, web push and
// sign-on are off, as on a fresh deployment.
//
// The S4b boots (boot_profiles_test.go, boot_misconfigured_test.go) also set
// HTTP_PROXY and HTTPS_PROXY to a recordingProxy of the test's own, which
// refuses every request and notes where it was going, so that no request a
// profile's server makes (billing's, above all) leaves the machine, and the
// golden says which it tried. The S4a profiles run without it.

// testDatabaseURLEnv enables the boot tests; see above.
const testDatabaseURLEnv = "OPENV_TEST_DATABASE_URL"

// build is the server binary, built once per test process.
var build struct {
	once sync.Once
	dir  string
	path string
	err  error
	took time.Duration
}

func TestMain(m *testing.M) {
	code := m.Run()
	if build.dir != "" {
		_ = os.RemoveAll(build.dir)
	}
	os.Exit(code)
}

// serverBinary builds `go build -cover ./cmd/server` from the module root
// into a temporary directory, once.
func serverBinary(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	build.once.Do(func() {
		start := time.Now()
		build.dir, build.err = os.MkdirTemp("", "openv-boot-harness-")
		if build.err != nil {
			return
		}
		build.path = filepath.Join(build.dir, "openv-server")
		cmd := exec.Command(goTool(), "build", "-cover", "-o", build.path, "./cmd/server")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			build.err = fmt.Errorf("go build -cover ./cmd/server: %v\n%s", err, out)
		}
		build.took = time.Since(start)
	})
	if build.err != nil {
		t.Fatal(build.err)
	}
	return build.path
}

// testDatabase is a database of the test's own.
type testDatabase struct {
	url    string
	vector bool // the server offers the vector extension
}

// freshDatabase creates an empty database on the server that
// OPENV_TEST_DATABASE_URL names and drops it when the test ends; it skips
// the test when the variable is unset.
func freshDatabase(t *testing.T) testDatabase {
	t.Helper()
	raw := os.Getenv(testDatabaseURLEnv)
	if raw == "" {
		t.Skipf("%s not set; skipping the boot harness (it needs a Postgres server)", testDatabaseURLEnv)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		t.Fatalf("%s must be a URL (postgres://user@host:port/db?sslmode=disable): %v", testDatabaseURLEnv, err)
	}
	admin, err := sql.Open("postgres", raw)
	if err != nil {
		t.Fatalf("open %s: %v", testDatabaseURLEnv, err)
	}
	if err := admin.Ping(); err != nil {
		t.Fatalf("reach %s: %v", testDatabaseURLEnv, err)
	}
	var vector bool
	if err := admin.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector')`).Scan(&vector); err != nil {
		t.Fatalf("read the server's extensions: %v", err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "openv_boot_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name + " WITH (FORCE)"); err != nil {
			t.Logf("drop database %s: %v", name, err)
		}
		_ = admin.Close()
	})
	u.Path = "/" + name
	return testDatabase{url: u.String(), vector: vector}
}

// outputFile is a file the server writes one of its streams to. The server
// writes into it directly, with no pipe or copying goroutine in between, so
// a log line it wrote before it answered a request is in the file by the
// time the answer arrives, and the file's size marks where each probe
// begins.
type outputFile struct{ path string }

func (f outputFile) Bytes() []byte {
	data, _ := os.ReadFile(f.path)
	return data
}

func (f outputFile) Len() int {
	st, err := os.Stat(f.path)
	if err != nil {
		return 0
	}
	return int(st.Size())
}

// serverProcess is one running server.
type serverProcess struct {
	t      *testing.T
	cmd    *exec.Cmd
	base   string // http://127.0.0.1:<port>
	port   int
	tmp    string
	stdout outputFile
	stderr outputFile
	done   chan struct{}

	clientOnce sync.Once
	httpClient *http.Client
}

// harnessEnv is the server's whole environment; see the top of this file.
func harnessEnv(db testDatabase, port int, tmp string, extra map[string]string) []string {
	env := map[string]string{
		"PATH":           "/usr/bin:/bin",
		"HOME":           filepath.Join(tmp, "home"),
		"TMPDIR":         filepath.Join(tmp, "tmp"),
		"DATABASE_URL":   db.url,
		"PORT":           strconv.Itoa(port),
		"UPLOADS_DIR":    filepath.Join(tmp, "uploads"),
		"OPENV_DATA_DIR": filepath.Join(tmp, "data"),
		"HOSTED_RUNNERS": "off",
		"GOCOVERDIR":     filepath.Join(tmp, "cover"),
	}
	for k, v := range extra {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// handedOut is every port freePort has returned in this test process, so
// that boots running in parallel never get the same one: the kernel may offer
// a port it has released again before the first server listens on it, and
// then the losing boot's /health probes would reach the winning server.
var handedOut struct {
	sync.Mutex
	ports map[int]bool
}

// freePort asks the kernel for a free port that it has not handed out
// before, and releases it for the server.
func freePort(t *testing.T) int {
	t.Helper()
	handedOut.Lock()
	defer handedOut.Unlock()
	if handedOut.ports == nil {
		handedOut.ports = map[int]bool{}
	}
	for range 100 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("find a free port: %v", err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if !handedOut.ports[port] {
			handedOut.ports[port] = true
			return port
		}
	}
	t.Fatal("find a free port: the kernel offered only ports already handed out")
	return 0
}

// bootServer boots the binary on a fresh database and waits until /health
// answers. A port is free when the harness picks it but may be taken before
// the server listens on it by another process (boots in this process never
// share one; see freePort); then the server exits after its migrations, and
// the harness boots again on another fresh database and port, since a second
// boot on the same database would log differently.
func bootServer(t *testing.T, bin string, extra map[string]string) (*serverProcess, testDatabase) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		db := freshDatabase(t)
		s, err := startServer(t, bin, db, extra)
		if err == nil {
			return s, db
		}
		if !errors.Is(err, errPortTaken) || attempt == 3 {
			t.Fatalf("%v\n%s", err, s.output())
		}
		t.Logf("port %d was taken before the server listened on it; booting again", s.port)
	}
}

// errPortTaken is a boot that failed only because its port was taken.
var errPortTaken = errors.New("the port was taken")

// startServer starts the binary and waits until /health answers. The whole
// process group is killed when the test ends, whatever happened.
func startServer(t *testing.T, bin string, db testDatabase, extra map[string]string) (*serverProcess, error) {
	t.Helper()
	tmp := t.TempDir()
	for _, d := range []string{"home", "tmp", "uploads", "data", "cover"} {
		if err := os.Mkdir(filepath.Join(tmp, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	port := freePort(t)
	s := &serverProcess{t: t, port: port, base: fmt.Sprintf("http://127.0.0.1:%d", port), tmp: tmp, done: make(chan struct{})}
	s.cmd = exec.Command(bin)
	s.cmd.Dir = moduleRoot(t)
	s.cmd.Env = harnessEnv(db, port, tmp, extra)
	s.stdout.path = filepath.Join(tmp, "stdout.log")
	s.stderr.path = filepath.Join(tmp, "stderr.log")
	stdout, err := os.Create(s.stdout.path)
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	stderr, err := os.Create(s.stderr.path)
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	s.cmd.Stdout = stdout
	s.cmd.Stderr = stderr
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := s.cmd.Start(); err != nil {
		t.Fatalf("start the server: %v", err)
	}
	go func() {
		_ = s.cmd.Wait()
		close(s.done)
	}()
	t.Cleanup(func() {
		if !s.exited() {
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		}
		select {
		case <-s.done:
		case <-time.After(10 * time.Second):
			t.Errorf("the server (pid %d) did not exit after SIGKILL", s.cmd.Process.Pid)
		}
	})
	return s, s.waitReady(60 * time.Second)
}

func (s *serverProcess) exited() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// output is what the server wrote, for failure messages.
func (s *serverProcess) output() string {
	return fmt.Sprintf("--- server stdout ---\n%s--- server stderr ---\n%s", s.stdout.Bytes(), s.stderr.Bytes())
}

// waitReady polls /health until it answers 200, the process exits, or the
// deadline passes.
func (s *serverProcess) waitReady(within time.Duration) error {
	deadline := time.Now().Add(within)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, Timeout: 2 * time.Second}
	for {
		if s.exited() {
			if strings.Contains(string(s.stderr.Bytes()), "address already in use") {
				return errPortTaken
			}
			return fmt.Errorf("the server exited before it answered /health (%s)", s.cmd.ProcessState)
		}
		resp, err := client.Get(s.base + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the server did not answer /health within %s (last error: %v)", within, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// client talks to the server exactly as asked: no proxy, no automatic gzip,
// no redirects followed. It keeps connections alive, as a browser does, so
// that a server's own Connection: close shows in Response.Close.
func (s *serverProcess) client() *http.Client {
	s.clientOnce.Do(func() {
		s.httpClient = &http.Client{
			Transport: &http.Transport{Proxy: nil, DisableCompression: true},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	})
	return s.httpClient
}

// terminate sends SIGTERM and waits for the process to exit.
func (s *serverProcess) terminate(within time.Duration) (exitCode int, took time.Duration, err error) {
	start := time.Now()
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return 0, 0, fmt.Errorf("send SIGTERM: %v", err)
	}
	select {
	case <-s.done:
		return s.cmd.ProcessState.ExitCode(), time.Since(start), nil
	case <-time.After(within):
		return 0, time.Since(start), fmt.Errorf("the server did not exit within %s of SIGTERM", within)
	}
}

// ----------------------------------------------------------------------------
// Normalisers
//
// A boot's output carries values that change from run to run or machine to
// machine. Each is replaced by a token before it reaches a golden, and
// nothing else is:
//
//   - the time= field of a log line is dropped;
//   - duration=<d> becomes duration=<duration>, port=<n> port=<port>;
//   - version=<x.y.z> becomes version=<version>: the release the binary
//     reports is RELEASE_NOTES.md's top section, which every promotion adds;
//   - a UUID becomes <uuid> (ids of the rows a boot creates);
//   - the test's temporary directory becomes <tmp>;
//   - a response's Date header is dropped; the session cookie's value and
//     Expires become <token> and <expires>;
//   - on a server without the vector extension, the migration's one warning
//     that the extension is unavailable is taken out of the boot log after
//     checking that it is there exactly once, and on a server with it the
//     warning must be absent, so one golden serves both CI legs;
//   - lines logged from goroutines that race main() (bootAsyncMessages) are
//     listed apart, sorted, since their place in the log is not fixed.
//
// There is no request ID to normalise, and the build SHA is empty in every
// profile that does not set OPENV_BUILD_SHA (RAILWAY_GIT_COMMIT_SHA is not
// inherited). The -cover build differs from a plain one only in writing its
// counters to GOCOVERDIR at exit; with GOCOVERDIR set it prints nothing.

var (
	uuidRE     = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	logTimeRE  = regexp.MustCompile(`^time=\S+ `)
	durationRE = regexp.MustCompile(`\bduration=\S+`)
	portRE     = regexp.MustCompile(`\bport=\d+`)
	versionRE  = regexp.MustCompile(`\bversion=\d+\.\d+\.\d+\S*`)
	cookieRE   = regexp.MustCompile(`^(openv_session=)[^;]*`)
	expiresRE  = regexp.MustCompile(`(; Expires=)[^;]*`)
)

// bootAsyncMessages are the log messages written from goroutines main()
// starts during boot, whose order against main's own lines is not fixed.
// Every profile's boot must log each of them: runBootProfile waits for all
// of them after /health answers and before the first probe
// (waitForAsyncLines), so that what they report cannot depend on the probes
// (the announcer's accounts= count would change once register adds a
// member).
var bootAsyncMessages = []string{
	`msg="release: announced"`, // logged by the announcer main() starts with go announcer.Announce
}

// waitForAsyncLines waits until the server has logged every
// bootAsyncMessages line, and each of a profile's own (bootProfile.async).
// main() starts their goroutines before it listens, so once /health answers
// they are running. The deadline allows for the billing reconcile, whose
// provider calls each retry twice with backoff before they give up.
func (s *serverProcess) waitForAsyncLines(profile []string) {
	s.t.Helper()
	deadline := time.Now().Add(asyncDeadline)
	for _, m := range append(append([]string(nil), bootAsyncMessages...), profile...) {
		for !strings.Contains(string(s.stderr.Bytes()), m) {
			if time.Now().After(deadline) {
				s.t.Fatalf("the server did not log %s within %s of answering /health. UPDATE_GOLDEN=1 does not "+
					"change this: if the message was reworded or dropped on purpose, edit bootAsyncMessages "+
					"(cmd/server/harness_test.go) or the profile's own async list (billingReconcileMessages, "+
					"cmd/server/boot_profiles_test.go) in the same change\n%s", m, asyncDeadline, s.output())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// asyncDeadline bounds waitForAsyncLines.
const asyncDeadline = 30 * time.Second

// noVectorWarning is the one line a server without the vector extension
// adds to the boot log, up to its error attribute, whose text is the
// server's and varies with its version.
const noVectorWarning = `level=WARN msg="vector extension unavailable; skipping artifact_embeddings table and semantic search (existing trigram/ILIKE search is unaffected)" error=`

func (s *serverProcess) normaliseLine(line string) string {
	line = logTimeRE.ReplaceAllString(line, "")
	line = durationRE.ReplaceAllString(line, "duration=<duration>")
	line = portRE.ReplaceAllString(line, "port=<port>")
	line = versionRE.ReplaceAllString(line, "version=<version>")
	return s.normaliseText(line)
}

func (s *serverProcess) normaliseText(text string) string {
	text = uuidRE.ReplaceAllString(text, "<uuid>")
	return strings.ReplaceAll(text, s.tmp, "<tmp>")
}

func normaliseHeader(name, value string) string {
	switch name {
	case "Set-Cookie":
		value = cookieRE.ReplaceAllString(value, "${1}<token>")
		value = expiresRE.ReplaceAllString(value, "${1}<expires>")
	}
	return uuidRE.ReplaceAllString(value, "<uuid>")
}

// logLine is one line of the server's stderr and where it starts.
type logLine struct {
	offset int
	text   string // normalised
	raw    string
}

func (s *serverProcess) logLines() []logLine {
	var out []logLine
	data := string(s.stderr.Bytes())
	offset := 0
	for _, raw := range strings.SplitAfter(data, "\n") {
		if raw == "" {
			continue
		}
		text := strings.TrimSuffix(raw, "\n")
		out = append(out, logLine{offset: offset, text: s.normaliseLine(text), raw: text})
		offset += len(raw)
	}
	return out
}

// isAsync reports whether line is one of bootAsyncMessages or of a
// profile's own.
func isAsync(line string, profile []string) bool {
	for _, m := range append(append([]string(nil), bootAsyncMessages...), profile...) {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// accessKey is "METHOD path" of an access-log line, or "" for another line.
func accessKey(line string) string {
	if !strings.Contains(line, `msg="http request"`) {
		return ""
	}
	method, path := logField(line, "method"), logField(line, "path")
	return method + " " + path
}

func logField(line, key string) string {
	i := strings.Index(line, " "+key+"=")
	if i < 0 {
		return ""
	}
	rest := line[i+len(key)+2:]
	if j := strings.IndexByte(rest, ' '); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// probeContext bounds one request.
func probeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// ----------------------------------------------------------------------------
// The recording proxy (S4b)

// recordingProxy is the forward proxy an S4b boot's HTTP_PROXY and
// HTTPS_PROXY name: a listener of the test's own that reads each request's
// head, notes where it was going, and answers 403 before a byte is sent
// anywhere else. Go's proxy-aware transports (http.DefaultTransport, which
// the billing provider's client uses, and any other that takes its proxy
// from the environment) send every request to a host other than localhost
// through it, so what it noted is every such request the server tried, and
// nothing it tried left the machine. A connection dialled without a proxy
// would pass it by; the server dials none in these profiles.
type recordingProxy struct {
	ln   net.Listener
	wg   sync.WaitGroup
	mu   sync.Mutex
	seen []string // "CONNECT host:port", or "METHOD scheme://host" for plain HTTP
}

// proxyRefusal is the reason phrase of the proxy's 403, which Go's
// transport reports as the request's error text.
const proxyRefusal = "refused by the boot harness proxy"

// proxyPlaceholder stands for the proxy's address in a golden.
const proxyPlaceholder = "http://127.0.0.1:<recording proxy port>"

func startRecordingProxy(t *testing.T) *recordingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start the recording proxy: %v", err)
	}
	p := &recordingProxy{ln: ln}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				p.refuse(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		p.wg.Wait()
	})
	return p
}

// url is the proxy's address, for HTTP_PROXY and HTTPS_PROXY.
func (p *recordingProxy) url() string { return "http://" + p.ln.Addr().String() }

// env adds HTTP_PROXY and HTTPS_PROXY, naming this proxy, to a copy of env.
func (p *recordingProxy) env(env map[string]string) map[string]string {
	out := map[string]string{"HTTP_PROXY": p.url(), "HTTPS_PROXY": p.url()}
	for k, v := range env {
		out[k] = v
	}
	return out
}

// refuse notes one request and answers it 403. The note is taken before the
// answer is written, so the server cannot see its request fail before the
// proxy has recorded it.
func (p *recordingProxy) refuse(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	what := "a request the proxy could not read"
	if req, err := http.ReadRequest(bufio.NewReader(conn)); err == nil {
		if req.Method == http.MethodConnect {
			what = "CONNECT " + req.Host
		} else {
			what = req.Method + " " + req.URL.Scheme + "://" + req.URL.Host
		}
	}
	p.mu.Lock()
	p.seen = append(p.seen, what)
	p.mu.Unlock()
	_, _ = io.WriteString(conn, "HTTP/1.1 403 "+proxyRefusal+"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
}

// summary lists where the server's requests were going, with how many
// times each was tried, sorted.
func (p *recordingProxy) summary() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seen) == 0 {
		return []string{"none: the server sent no request through HTTP_PROXY or HTTPS_PROXY"}
	}
	counts := map[string]int{}
	for _, s := range p.seen {
		counts[s]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s: %d tried, each answered 403 %q and sent no further", k, counts[k], proxyRefusal))
	}
	return out
}
