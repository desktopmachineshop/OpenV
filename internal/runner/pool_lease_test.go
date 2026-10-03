package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// PoolAgent leases (refactor plan step S15a). A pool node serves one member
// at a time: a heartbeat that hands it a lease points the process-wide HOME
// at a directory made for that lease and starts a headless worker with the
// lease's key; a heartbeat that ends, replaces or loses the lease stops that
// worker, puts HOME back, wipes the lease's directories and reports the node
// free. These tests drive PoolAgent against a stand-in API, with a stand-in
// codex CLI that prints the HOME it was started with, so the detection
// report of each lease's worker shows which HOME its vendor CLIs see. Every
// test restores the test process's HOME when it ends (t.Setenv), whatever
// the agent left it as.

// standInCodexHome reports the HOME it runs with as its version.
const standInCodexHome = `case "$1" in
--version) echo "home=$HOME" ;;
*) exit 2 ;;
esac
`

// Lease ids are uuids, as the API's are: the worker id takes their first 8
// characters.
const (
	lease1 = "5e55a001-0000-4000-8000-000000000001"
	lease2 = "5e55a002-0000-4000-8000-000000000002"
	lease3 = "5e55a003-0000-4000-8000-000000000003"
	lease4 = "5e55a004-0000-4000-8000-000000000004"
	lease5 = "5e55a005-0000-4000-8000-000000000005"
)

// poolWorld is the stand-in API's pool side: registration hands out node
// ids in turn, and each heartbeat answers what the test last set.
type poolWorld struct {
	t   *testing.T
	api *fakeAPI

	mu              sync.Mutex
	nodeIDs         []string
	heartbeatStatus int
	heartbeatBody   string
}

func newPoolWorld(t *testing.T, nodeIDs ...string) *poolWorld {
	w := &poolWorld{t: t, nodeIDs: nodeIDs, heartbeatStatus: http.StatusOK, heartbeatBody: `{"assignment":null}`}
	w.api = newFakeAPI(t, w.respond)
	return w
}

func (w *poolWorld) respond(c apiCall) (int, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case c.Method == "POST" && c.Path == "/api/v1/runner-pool/nodes":
		if len(w.nodeIDs) == 0 {
			return http.StatusServiceUnavailable, `{"error":"no node id left in the test"}`
		}
		id := w.nodeIDs[0]
		w.nodeIDs = w.nodeIDs[1:]
		return http.StatusCreated, `{"id":"` + id + `","name":"","pool":"","status":"idle"}`
	case strings.HasPrefix(c.Path, "/api/v1/runner-pool/nodes/") && c.is("POST", "/heartbeat"):
		return w.heartbeatStatus, w.heartbeatBody
	case strings.HasPrefix(c.Path, "/api/v1/runner-pool/nodes/") && c.is("POST", "/release"):
		return http.StatusNoContent, ""
	}
	return 0, ""
}

// assign sets what the next heartbeats answer: the lease (with its key,
// which the API hands over once), or none when session is empty.
func (w *poolWorld) assign(session, key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.heartbeatStatus = http.StatusOK
	if session == "" {
		w.heartbeatBody = `{"assignment":null}`
		return
	}
	raw, err := json.Marshal(map[string]interface{}{"assignment": PoolAssignment{
		SessionID: session, OrgID: "org-1", UserID: "user-1", UserName: "Ada", WorkerKey: key,
		ExpiresAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
	}})
	if err != nil {
		w.t.Fatal(err)
	}
	w.heartbeatBody = string(raw)
}

// forget makes the next heartbeats answer 404: the API no longer knows the node.
func (w *poolWorld) forget() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.heartbeatStatus, w.heartbeatBody = http.StatusNotFound, `{"error":"pool node not found","code":"not_found"}`
}

// isNodeRelease selects the node-free reports.
func isNodeRelease(c apiCall) bool {
	return strings.HasPrefix(c.Path, "/api/v1/runner-pool/nodes/") && c.is("POST", "/release")
}

// poolReleases returns the node-free reports, as "<path> <body>".
func (w *poolWorld) poolReleases() []string {
	var out []string
	for _, c := range w.api.match(isNodeRelease) {
		if c.Auth != "pool-key" {
			w.t.Errorf("%s went as %q, want the pool key", c.Path, c.Auth)
		}
		out = append(out, c.Path+" "+string(c.Body))
	}
	return out
}

// oneTick is long enough for a worker's 2-second claim ticker to fire at
// least once.
const oneTick = 2500 * time.Millisecond

// endLeaseBeat sends the heartbeat that ends lease session, whose worker
// claims as key, and checks that the lease's worker stopped with it: the
// heartbeat returns at once (endLease gives a worker that does not stop 30
// seconds, then wipes and reports the node free anyway), and a claim tick
// later no claim has gone as the lease's key since the node reported the
// lease free (or, with no report, since the heartbeat returned). A worker
// sends its claims from Worker.Run's own loop, which has returned before
// the node reports, so a stopped worker's last claim comes first; its
// sign-in poll is not waited for, so it is not checked. Under -short, which
// skips the tests that wait on a tick, only the time is checked.
func (w *poolWorld) endLeaseBeat(ctx context.Context, p *PoolAgent, session, key string) {
	w.t.Helper()
	began := time.Now()
	p.beat(ctx)
	if took := time.Since(began); took > 10*time.Second {
		w.t.Errorf("the heartbeat that ended lease %s took %s, want the lease's worker stopped at once",
			session, took.Round(100*time.Millisecond))
	}
	after := len(w.api.Calls()) - 1
	if testing.Short() {
		return
	}
	time.Sleep(oneTick)
	report := ""
	for _, c := range w.api.match(isNodeRelease) {
		if string(c.Body) == `{"session_id":"`+session+`"}` {
			after, report = c.Seq, fmt.Sprintf(" (node-free report #%d)", c.Seq)
		}
	}
	for _, c := range w.api.match(func(c apiCall) bool { return isClaim(c) && c.Auth == key && c.Seq > after }) {
		w.t.Errorf("claim #%d went as %s after lease %s ended%s: its worker did not stop", c.Seq, key, session, report)
	}
}

// awaitLeaseWorker waits for the detection report a lease's worker sends
// with the lease's key, and returns the HOME its codex reported.
func (w *poolWorld) awaitLeaseWorker(key string) (apiCall, string) {
	w.t.Helper()
	isReport := func(c apiCall) bool { return c.Auth == key && c.is("POST", "/provider-settings/detect") }
	calls := w.api.await(w.t, 15*time.Second, "the detection report of the worker holding "+key, func(calls []apiCall) bool {
		return count(calls, isReport) > 0
	})
	for _, c := range calls {
		if isReport(c) {
			var report map[string]map[string]interface{}
			c.json(w.t, &report)
			version, _ := report["codex-cli"]["version"].(string)
			return c, strings.TrimPrefix(version, "home=")
		}
	}
	return apiCall{}, ""
}

// newPoolNode isolates the test's environment for a pool agent: a HOME of
// its own (returned as base) and a PATH holding only the stand-in codex.
func newPoolNode(t *testing.T) (base string) {
	t.Helper()
	skipWithoutPOSIXShell(t)
	base = t.TempDir()
	t.Setenv("HOME", base)
	bin := t.TempDir()
	writeScript(t, bin, "codex", standInCodexHome)
	isolateRunnerEnv(t, bin)
	return base
}

func wantHome(t *testing.T, when, want string) {
	t.Helper()
	if got := os.Getenv("HOME"); got != want {
		t.Errorf("%s: HOME = %q, want %q", when, got, want)
	}
}

func wantDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("%s: %v, want a directory", path, err)
		return
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("%s: mode %v, want a directory with mode 0700", path, info.Mode())
	}
}

func wantGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: stat error %v, want it gone", path, err)
	}
}

// TestPoolLeaseStartSupersedeAndEnd walks one node through every heartbeat
// answer: a lease arriving, the same lease again, a new lease replacing it
// with and without a key, the keyless lease again, the lease ending, and the
// node forgotten mid-lease. Each lease that ends has its worker stopped at
// once, before the node reports it free.
func TestPoolLeaseStartSupersedeAndEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the leased worker's 2-second claim tick")
	}
	root := t.TempDir()
	sessions, workspaces := filepath.Join(root, "sessions"), filepath.Join(root, "workspaces")
	base := newPoolNode(t)
	world := newPoolWorld(t, "node-1", "node-2")
	p := NewPoolAgent(PoolOptions{APIURL: world.api.URL(), PoolKey: "pool-key", Pool: "gpu", NodeName: "node-a",
		SessionRoot: sessions, WorkspaceBase: workspaces, MCPBinary: "/opt/openv/bin/openv-mcp",
		Concurrency: 2, ChildConcurrency: 1})
	ctx := context.Background()
	if err := p.register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	register := world.api.match(func(c apiCall) bool { return c.Path == "/api/v1/runner-pool/nodes" })
	if len(register) != 1 || string(register[0].Body) != `{"name":"node-a","pool":"gpu","providers":["codex-cli"]}` ||
		register[0].Auth != "pool-key" {
		t.Errorf("registration = %s, want one as the pool key naming node-a, pool gpu and the installed codex-cli",
			describeCalls(register))
	}

	// 1. A lease arrives: HOME and the workspaces move to directories of its
	// own, and a worker claims as the lease's key, for both pools.
	world.assign(lease1, "lease-key-1")
	p.beat(ctx)
	home1 := filepath.Join(sessions, lease1)
	wantHome(t, "lease 1 started", home1)
	wantDir(t, home1)
	wantDir(t, filepath.Join(workspaces, lease1))
	if _, seen := world.awaitLeaseWorker("lease-key-1"); seen != home1 {
		t.Errorf("lease 1's worker ran codex with HOME %q, want %q", seen, home1)
	}
	claims := world.api.await(t, 15*time.Second, "lease 1's worker to claim into both pools", func(calls []apiCall) bool {
		return count(calls, func(c apiCall) bool { return isClaim(c) && c.Auth == "lease-key-1" }) >= 2
	})
	pools := map[int]bool{}
	for _, c := range claims {
		if isClaim(c) && c.Auth == "lease-key-1" {
			var body claimBody
			c.json(t, &body)
			pools[body.MinPriority] = true
			if body.WorkerID != "node-a-5e55a001" || body.Hosted || strings.Join(body.Providers, ",") != "codex-cli" {
				t.Errorf("lease 1's claim = %+v, want worker_id node-a-5e55a001, not hosted, providers [codex-cli]", body)
			}
		}
	}
	if !pools[0] || !pools[10] {
		t.Errorf("lease 1's worker claimed at min_priority %v, want 0 and 10 (Concurrency 2, ChildConcurrency 1)", pools)
	}

	// 2. The same lease again, its key not handed over twice: nothing changes.
	world.assign(lease1, "")
	p.beat(ctx)
	wantHome(t, "lease 1 heartbeat without its key", home1)
	if got := world.poolReleases(); len(got) != 0 {
		t.Errorf("the node reported itself free %v while still serving lease 1", got)
	}

	// 3. A new lease replaces it: lease 1 ends (its worker stopped, wiped and
	// reported) before lease 2's worker sends anything.
	world.assign(lease2, "lease-key-2")
	world.endLeaseBeat(ctx, p, lease1, "lease-key-1")
	home2 := filepath.Join(sessions, lease2)
	wantHome(t, "lease 2 superseded lease 1", home2)
	wantGone(t, home1)
	wantGone(t, filepath.Join(workspaces, lease1))
	wantDir(t, home2)
	if got, want := world.poolReleases(), []string{"/api/v1/runner-pool/nodes/node-1/release {\"session_id\":\"" + lease1 + "\"}"}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("node-free reports = %q, want %q", got, want)
	}
	if _, seen := world.awaitLeaseWorker("lease-key-2"); seen != home2 {
		t.Errorf("lease 2's worker ran codex with HOME %q, want %q", seen, home2)
	}
	first2 := world.api.match(func(c apiCall) bool { return c.Auth == "lease-key-2" })[0]
	for _, c := range world.api.match(func(c apiCall) bool { return c.Path == "/api/v1/runner-pool/nodes/node-1/release" }) {
		if c.Seq > first2.Seq {
			t.Errorf("lease 1 was reported free (request #%d) after lease 2's worker sent its first request (#%d)", c.Seq, first2.Seq)
		}
	}

	// 4. A new lease with no key replaces lease 2: lease 2 ends, and with
	// nothing to authenticate as no lease starts.
	world.assign(lease3, "")
	world.endLeaseBeat(ctx, p, lease2, "lease-key-2")
	wantHome(t, "lease 3 (no key) superseded lease 2", base)
	wantGone(t, home2)
	wantGone(t, filepath.Join(sessions, lease3))

	// 5. The keyless lease again: ignored.
	p.beat(ctx)
	wantHome(t, "lease 3 (no key) again", base)
	wantGone(t, filepath.Join(sessions, lease3))

	// 6. A lease, then no lease: it ended.
	world.assign(lease4, "lease-key-4")
	p.beat(ctx)
	wantHome(t, "lease 4 started", filepath.Join(sessions, lease4))
	world.awaitLeaseWorker("lease-key-4")
	world.assign("", "")
	world.endLeaseBeat(ctx, p, lease4, "lease-key-4")
	wantHome(t, "lease 4 ended", base)
	wantGone(t, filepath.Join(sessions, lease4))
	wantGone(t, filepath.Join(workspaces, lease4))

	// 7. The API forgets the node mid-lease: the lease ends, the node reports
	// it free under its old id, and registers again.
	world.assign(lease5, "lease-key-5")
	p.beat(ctx)
	world.awaitLeaseWorker("lease-key-5")
	world.forget()
	world.endLeaseBeat(ctx, p, lease5, "lease-key-5")
	wantHome(t, "lease 5 ended by the node being forgotten", base)
	wantGone(t, filepath.Join(sessions, lease5))
	world.assign("", "")
	p.beat(ctx)
	beats := world.api.match(func(c apiCall) bool { return c.is("POST", "/heartbeat") })
	if last := beats[len(beats)-1]; last.Path != "/api/v1/runner-pool/nodes/node-2/heartbeat" || last.Auth != "pool-key" {
		t.Errorf("the heartbeat after re-registering went to %s as %q, want node-2's as the pool key", last.Path, last.Auth)
	}

	want := []string{
		"/api/v1/runner-pool/nodes/node-1/release {\"session_id\":\"" + lease1 + "\"}",
		"/api/v1/runner-pool/nodes/node-1/release {\"session_id\":\"" + lease2 + "\"}",
		"/api/v1/runner-pool/nodes/node-1/release {\"session_id\":\"" + lease4 + "\"}",
		"/api/v1/runner-pool/nodes/node-1/release {\"session_id\":\"" + lease5 + "\"}",
	}
	if got := world.poolReleases(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("node-free reports:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := len(world.api.match(func(c apiCall) bool { return c.Path == "/api/v1/runner-pool/nodes" })); n != 2 {
		t.Errorf("the node registered %d times, want 2 (once more after being forgotten)", n)
	}
	wantHome(t, "the end", base)
}

// TestPoolLeaseStartFailures: a lease whose directories cannot be made
// starts no worker. When its HOME cannot be made, HOME stays the process's;
// when its workspaces cannot, HOME has already moved to the lease's
// directory and stays there, with the directory left on disk, until another
// lease starts (pinned as found; see the S15a pull request).
func TestPoolLeaseStartFailures(t *testing.T) {
	t.Run("session home", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "file")
		if err := os.WriteFile(blocker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		base := newPoolNode(t)
		world := newPoolWorld(t, "node-1")
		p := NewPoolAgent(PoolOptions{APIURL: world.api.URL(), PoolKey: "pool-key", NodeName: "node-a",
			SessionRoot: filepath.Join(blocker, "sessions")})
		if err := p.register(context.Background()); err != nil {
			t.Fatal(err)
		}
		world.assign(lease1, "lease-key-1")
		p.beat(context.Background())
		wantHome(t, "a lease whose HOME cannot be made", base)
		world.assign("", "")
		p.beat(context.Background())
		wantHome(t, "after that lease's end", base)
		if n := count(world.api.Calls(), func(c apiCall) bool { return c.Auth == "lease-key-1" }); n != 0 {
			t.Errorf("a worker sent %d request(s) as the lease's key, want none started", n)
		}
		if got := world.poolReleases(); len(got) != 0 {
			t.Errorf("node-free reports %v for a lease that never started, want none", got)
		}
	})
	t.Run("workspaces", func(t *testing.T) {
		root := t.TempDir()
		sessions, blocker := filepath.Join(root, "sessions"), filepath.Join(root, "file")
		if err := os.WriteFile(blocker, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		newPoolNode(t)
		world := newPoolWorld(t, "node-1")
		p := NewPoolAgent(PoolOptions{APIURL: world.api.URL(), PoolKey: "pool-key", NodeName: "node-a",
			SessionRoot: sessions, WorkspaceBase: blocker})
		if err := p.register(context.Background()); err != nil {
			t.Fatal(err)
		}
		world.assign(lease1, "lease-key-1")
		p.beat(context.Background())
		home1 := filepath.Join(sessions, lease1)
		wantHome(t, "a lease whose workspaces cannot be made", home1)
		wantDir(t, home1)
		world.assign("", "")
		p.beat(context.Background())
		wantHome(t, "after that lease's end", home1)
		wantDir(t, home1)
		if n := count(world.api.Calls(), func(c apiCall) bool { return c.Auth == "lease-key-1" }); n != 0 {
			t.Errorf("a worker sent %d request(s) as the lease's key, want none started", n)
		}
		if got := world.poolReleases(); len(got) != 0 {
			t.Errorf("node-free reports %v for a lease that never started, want none", got)
		}
	})
}

// TestPoolLeaseEndKeepsTheLeaseHOMEWhenTheProcessHadNone: a node started
// with HOME empty has no HOME to put back, so after a lease HOME still names
// the lease's wiped directory (pinned as found; see the S15a pull request).
func TestPoolLeaseEndKeepsTheLeaseHOMEWhenTheProcessHadNone(t *testing.T) {
	sessions := filepath.Join(t.TempDir(), "sessions")
	newPoolNode(t)
	t.Setenv("HOME", "")
	world := newPoolWorld(t, "node-1")
	p := NewPoolAgent(PoolOptions{APIURL: world.api.URL(), PoolKey: "pool-key", NodeName: "node-a", SessionRoot: sessions})
	if err := p.register(context.Background()); err != nil {
		t.Fatal(err)
	}
	world.assign(lease1, "lease-key-1")
	p.beat(context.Background())
	home1 := filepath.Join(sessions, lease1)
	wantHome(t, "lease 1 started", home1)
	world.awaitLeaseWorker("lease-key-1")
	world.assign("", "")
	world.endLeaseBeat(context.Background(), p, lease1, "lease-key-1")
	wantHome(t, "lease 1 ended on a node started without HOME", home1)
	wantGone(t, home1)
}

// TestPoolRunPurgesRegistersAndEndsTheLeaseOnShutdown drives PoolAgent.Run:
// it wipes what a previous process left under the session root before it
// registers (with the default pool and the host name), takes a lease on its
// first heartbeat, and on shutdown ends the lease (its worker stopped at
// once, HOME back, directories wiped) without reporting the node free.
func TestPoolRunPurgesRegistersAndEndsTheLeaseOnShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the node's 5-second heartbeat")
	}
	sessions := filepath.Join(t.TempDir(), "sessions")
	stale := filepath.Join(sessions, "stale-lease", ".codex")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := newPoolNode(t)
	world := newPoolWorld(t, "node-1")
	world.assign(lease1, "lease-key-1")
	p := NewPoolAgent(PoolOptions{APIURL: world.api.URL(), PoolKey: "pool-key", SessionRoot: sessions})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	register := world.api.await(t, 15*time.Second, "the node to register", func(calls []apiCall) bool {
		return count(calls, func(c apiCall) bool { return c.Path == "/api/v1/runner-pool/nodes" }) > 0
	})
	wantGone(t, filepath.Join(sessions, "stale-lease"))
	host, _ := os.Hostname()
	if host == "" {
		host = "pool-node"
	}
	wantBody, _ := json.Marshal(map[string]interface{}{"name": host, "pool": "default", "providers": []string{"codex-cli"}})
	for _, c := range register {
		if c.Path == "/api/v1/runner-pool/nodes" && (string(c.Body) != string(wantBody) || c.Auth != "pool-key") {
			t.Errorf("registration %s as %q, want %s as the pool key", c.Body, c.Auth, wantBody)
		}
	}

	home1 := filepath.Join(sessions, lease1)
	if _, seen := world.awaitLeaseWorker("lease-key-1"); seen != home1 {
		t.Errorf("the lease's worker ran codex with HOME %q, want %q", seen, home1)
	}
	wantHome(t, "lease 1 started by Run's heartbeat", home1)
	wantDir(t, filepath.Join(home1, "workspaces"))

	cancel()
	stopping := time.Now()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("PoolAgent.Run returned %v on shutdown, want context.Canceled", err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("PoolAgent.Run did not return after its context ended")
	}
	if took := time.Since(stopping); took > 10*time.Second {
		t.Errorf("PoolAgent.Run took %s to return on shutdown, want the lease's worker stopped at once",
			took.Round(100*time.Millisecond))
	}
	stopped := len(world.api.Calls())
	wantHome(t, "after shutdown", base)
	wantGone(t, home1)
	if got := world.poolReleases(); len(got) != 0 {
		t.Errorf("node-free reports %v on shutdown, want none (the API reclaims a node whose heartbeat stops)", got)
	}
	// No report to order against: the lease's worker sends no claim once Run
	// has returned.
	time.Sleep(oneTick)
	for _, c := range world.api.Calls()[stopped:] {
		if isClaim(c) && c.Auth == "lease-key-1" {
			t.Errorf("claim #%d went as lease-key-1 after PoolAgent.Run returned: the lease's worker did not stop", c.Seq)
		}
	}
}
