package runner

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// Worker.Run's slot accounting (refactor plan step S15a): the worker keeps
// two pools of run slots, Concurrency for ordinary runs (claimed with
// min_priority 0) and ChildConcurrency for orchestrated children and
// interview turns (min_priority agentruns.PriorityChild), so a parent blocked
// on delegation cannot starve its children. Every two seconds it claims
// into each pool until the pool is full or the queue answers 204; a slot
// goes back when a claim fails or comes back empty, and when the run it
// held ends, however it ends. These tests run the real Worker.Run against a
// stand-in queue that fails them if the worker ever claims into a pool it
// has filled. Each waits on the worker's own 2-second claim tick, so they
// skip under -short; `make check` and CI run them.

// slotQueue is the stand-in API's run queue: claims per pool, scripted claim
// failures, what each pool holds, and the runs a member cancels.
type slotQueue struct {
	t *testing.T

	mu       sync.Mutex
	limit    map[int]int
	pending  map[int][]string
	failures map[int][]int // the status codes the next claims answer, per pool
	inflight map[int]map[string]bool
	cancel   map[string]bool
	problems []string
}

func newSlotQueue(t *testing.T, limits map[int]int) *slotQueue {
	q := &slotQueue{t: t, limit: limits, pending: map[int][]string{}, failures: map[int][]int{},
		inflight: map[int]map[string]bool{}, cancel: map[string]bool{}}
	for p := range limits {
		q.inflight[p] = map[string]bool{}
	}
	// Reported however the test ends, so a test that stops waiting for a
	// run still says when the worker overfilled a pool.
	t.Cleanup(func() {
		for _, p := range q.problemList() {
			t.Error(p)
		}
	})
	return q
}

func (q *slotQueue) push(priority int, ids ...string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending[priority] = append(q.pending[priority], ids...)
}

func (q *slotQueue) failNext(priority int, statuses ...int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failures[priority] = append(q.failures[priority], statuses...)
}

func (q *slotQueue) cancelRun(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancel[id] = true
}

func (q *slotQueue) queued(priority int) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.pending[priority]...)
}

func (q *slotQueue) problemList() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.problems...)
}

func (q *slotQueue) respond(c apiCall) (int, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case isClaim(c):
		var body claimBody
		c.json(q.t, &body)
		p := body.MinPriority
		limit, known := q.limit[p]
		if !known {
			q.problems = append(q.problems, fmt.Sprintf("claim #%d asked for min_priority %d, which no pool uses", c.Seq, p))
			return http.StatusNoContent, ""
		}
		if n := len(q.inflight[p]); n >= limit {
			q.problems = append(q.problems, fmt.Sprintf(
				"claim #%d at min_priority %d with %d run(s) of that pool in flight, its limit being %d", c.Seq, p, n, limit))
		}
		if f := q.failures[p]; len(f) > 0 {
			q.failures[p] = f[1:]
			return f[0], `{"error":"the queue is unavailable","code":"internal"}`
		}
		if len(q.pending[p]) == 0 {
			return http.StatusNoContent, ""
		}
		id := q.pending[p][0]
		q.pending[p] = q.pending[p][1:]
		q.inflight[p][id] = true
		return http.StatusOK, `{"run":{"id":"` + id + `","org_id":"org-1","agent_id":"agent-1","status":"claimed","priority":` +
			fmt.Sprint(p) + `,"prompt":"hold"},"agent":` + agentJSON("fake", "Slot Agent", "slot-agent", fieldTools, false) +
			`,"run_token":"rt-` + id + `","auth":{"mode":"user-account"}}`
	case c.is("POST", "/finish"), c.is("POST", "/release"):
		for _, pool := range q.inflight {
			delete(pool, runIDOf(c))
		}
	case c.is("POST", "/logs") && q.cancel[runIDOf(c)]:
		return http.StatusOK, `{"cancel_requested":true,"status":"running"}`
	}
	return 0, ""
}

// heldRuns starts every run as a heldRun and lets the test end it.
type heldRuns struct {
	mu   sync.Mutex
	runs map[string]*heldRun
}

func newHeldRuns() *heldRuns { return &heldRuns{runs: map[string]*heldRun{}} }

func (r *heldRuns) start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	h := newHeldRun(ctx)
	r.mu.Lock()
	r.runs[spec.RunID] = h
	r.mu.Unlock()
	return h, nil
}

func (r *heldRuns) get(t *testing.T, id string) *heldRun {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.runs[id]
	if !ok {
		t.Fatalf("run %s never reached the adapter", id)
	}
	return h
}

func (r *heldRuns) started() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]bool{}
	for id := range r.runs {
		out[id] = true
	}
	return out
}

// startSlotWorker runs Worker.Run with a stand-in "fake" provider adapter
// (installed unless notInstalled) and returns the channel Run's result
// arrives on.
func startSlotWorker(t *testing.T, ctx context.Context, api *fakeAPI, opts Options, runs *heldRuns, notInstalled bool) <-chan error {
	t.Helper()
	w := NewWorker(NewClient(api.URL(), "worker-key"), opts)
	w.adapters = map[string]Adapter{"fake": &scriptedAdapter{name: "fake", installed: !notInstalled, start: runs.start}}
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	return done
}

// awaitStarted waits until every id has reached the adapter; why says what
// should have let them start.
func awaitStarted(t *testing.T, api *fakeAPI, runs *heldRuns, why string, ids ...string) {
	t.Helper()
	api.await(t, 15*time.Second, "runs "+strings.Join(ids, ", ")+" to start ("+why+")", func([]apiCall) bool {
		started := runs.started()
		for _, id := range ids {
			if !started[id] {
				return false
			}
		}
		return true
	})
}

// awaitFinish waits for a run's finish report and returns its body.
func awaitFinish(t *testing.T, api *fakeAPI, id string) agentruns.FinishRequest {
	t.Helper()
	isFinish := func(c apiCall) bool { return runIDOf(c) == id && c.is("POST", "/finish") }
	calls := api.await(t, 15*time.Second, "run "+id+"'s finish report", func(calls []apiCall) bool {
		return count(calls, isFinish) > 0
	})
	var req agentruns.FinishRequest
	for _, c := range calls {
		if isFinish(c) {
			c.json(t, &req)
		}
	}
	return req
}

// claimRequests lists the claims the worker sent at minPriority.
func claimRequests(t *testing.T, api *fakeAPI, minPriority int) []apiCall {
	return api.match(isClaimAt(t, minPriority))
}

// stopWorker cancels the worker's context and waits for Run to return.
func stopWorker(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Worker.Run returned %v on shutdown, want context.Canceled", err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("Worker.Run did not return after its context ended")
	}
}

// twoTicks is long enough for the worker's 2-second claim ticker to fire at
// least twice.
const twoTicks = 4500 * time.Millisecond

// TestRunSlotsAcrossBothPools: each pool claims up to its own limit and no
// further, and a slot comes back when its run succeeds, fails, panics or is
// cancelled by a member; on shutdown Run releases every run it holds before
// it returns.
func TestRunSlotsAcrossBothPools(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the worker's 2-second claim ticks")
	}
	t.Parallel()
	child := agentruns.PriorityChild
	queue := newSlotQueue(t, map[int]int{0: 2, child: 1})
	queue.push(0, "n1", "n2", "n3")
	queue.push(child, "c1", "c2")
	api := newFakeAPI(t, queue.respond)
	runs := newHeldRuns()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startSlotWorker(t, ctx, api, Options{WorkerID: "w-slots", Concurrency: 2, ChildConcurrency: 1,
		WorkspaceBase: t.TempDir()}, runs, false)

	// Both pools fill on the first tick, and neither claims again while full.
	awaitStarted(t, api, runs, "the first claim tick fills both pools", "n1", "n2", "c1")
	time.Sleep(twoTicks)
	if n := len(claimRequests(t, api, 0)); n != 2 {
		t.Errorf("the ordinary pool (Concurrency 2) sent %d claims while full, want exactly the 2 that filled it", n)
	}
	if n := len(claimRequests(t, api, child)); n != 1 {
		t.Errorf("the child pool (ChildConcurrency 1) sent %d claims while full, want exactly the 1 that filled it", n)
	}
	if got := queue.queued(0); len(got) != 1 || got[0] != "n3" {
		t.Errorf("ordinary queue = %v, want [n3] left unclaimed", got)
	}
	if got := queue.queued(child); len(got) != 1 || got[0] != "c2" {
		t.Errorf("child queue = %v, want [c2] left unclaimed", got)
	}

	// A success frees an ordinary slot, a failure a child slot.
	runs.get(t, "n1").end(Result{ExitCode: 0, FinalText: "ok"}, nil, false)
	runs.get(t, "c1").end(Result{ExitCode: 1}, errorString("exit status 1: tool failed"), false)
	if req := awaitFinish(t, api, "n1"); req.Status != agentruns.StatusSucceeded {
		t.Errorf("n1 finished %q, want %q", req.Status, agentruns.StatusSucceeded)
	}
	if req := awaitFinish(t, api, "c1"); req.Status != agentruns.StatusFailed {
		t.Errorf("c1 finished %q, want %q", req.Status, agentruns.StatusFailed)
	}
	awaitStarted(t, api, runs, "n1's success and c1's failure each free a slot", "n3", "c2")

	// A panic and a member's cancel free both ordinary slots: two runs
	// queued now are claimed together.
	runs.get(t, "n2").end(Result{}, nil, true)
	queue.cancelRun("n3")
	if req := awaitFinish(t, api, "n2"); req.Error != "worker panic during run execution" {
		t.Errorf("n2 finished with error %q, want the worker panic", req.Error)
	}
	if req := awaitFinish(t, api, "n3"); req.Status != agentruns.StatusCancelled {
		t.Errorf("n3 finished %q, want %q", req.Status, agentruns.StatusCancelled)
	}
	queue.push(0, "n4", "n5")
	awaitStarted(t, api, runs, "n2's panic and n3's cancel free both ordinary slots", "n4", "n5")

	// Shutdown: every run still held is released, before Run returns.
	stopWorker(t, cancel, done)
	released := map[string]string{}
	for _, c := range api.match(func(c apiCall) bool { return c.is("POST", "/release") }) {
		released[runIDOf(c)] = string(c.Body)
	}
	for _, id := range []string{"n4", "n5", "c2"} {
		if body := released[id]; body != `{"worker_id":"w-slots"}` {
			t.Errorf("run %s at shutdown: release body %q, want {\"worker_id\":\"w-slots\"}", id, body)
		}
	}
	if len(released) != 3 {
		t.Errorf("released %d runs at shutdown, want the 3 still held: %v", len(released), released)
	}
	checkClaimBodies(t, api, "w-slots", false, 0, child)
}

// TestRunSlotsDefaults: with Concurrency unset and ChildConcurrency
// negative, a worker holds one ordinary run at a time and never claims a
// child; a hosted worker sends hosted:true and never polls for sign-ins;
// an unset WorkerID claims as "worker".
func TestRunSlotsDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the worker's 2-second claim ticks")
	}
	t.Parallel()
	child := agentruns.PriorityChild
	queue := newSlotQueue(t, map[int]int{0: 1, child: 0})
	queue.push(0, "n1", "n2")
	queue.push(child, "c1")
	api := newFakeAPI(t, queue.respond)
	runs := newHeldRuns()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startSlotWorker(t, ctx, api, Options{Concurrency: 0, ChildConcurrency: -1, Hosted: true,
		WorkspaceBase: t.TempDir()}, runs, false)

	awaitStarted(t, api, runs, "the first claim tick", "n1")
	time.Sleep(twoTicks)
	if n := len(claimRequests(t, api, 0)); n != 1 {
		t.Errorf("a worker with Concurrency 0 sent %d ordinary claims holding one run, want 1 (it defaults to 1)", n)
	}
	runs.get(t, "n1").end(Result{ExitCode: 0}, nil, false)
	awaitStarted(t, api, runs, "n1's end frees the only slot", "n2")
	runs.get(t, "n2").end(Result{ExitCode: 0}, nil, false)
	awaitFinish(t, api, "n2")
	stopWorker(t, cancel, done)

	if n := len(claimRequests(t, api, child)); n != 0 {
		t.Errorf("a worker with ChildConcurrency -1 sent %d child claims, want none", n)
	}
	if got := queue.queued(child); len(got) != 1 {
		t.Errorf("child queue = %v, want c1 left unclaimed", got)
	}
	if n := len(api.match(func(c apiCall) bool { return c.is("POST", "/provider-logins/claim") })); n != 0 {
		t.Errorf("a hosted worker polled for sign-ins %d times, want never", n)
	}
	checkClaimBodies(t, api, "worker", true, 0)
}

// TestRunSlotsSurviveClaimFailures: a claim that fails or comes back empty
// hands its slot back, so a worker with one slot keeps claiming and takes
// the next run.
func TestRunSlotsSurviveClaimFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the worker's 2-second claim ticks")
	}
	t.Parallel()
	queue := newSlotQueue(t, map[int]int{0: 1})
	queue.failNext(0, http.StatusInternalServerError, http.StatusUnauthorized)
	api := newFakeAPI(t, queue.respond)
	runs := newHeldRuns()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startSlotWorker(t, ctx, api, Options{WorkerID: "w-retry", Concurrency: 1, WorkspaceBase: t.TempDir()}, runs, false)

	// Two failed claims and an empty one, then the run: each hands its
	// slot back, or the worker would never claim again.
	api.await(t, 15*time.Second, "three claims (500, 401, 204)", func(calls []apiCall) bool {
		return count(calls, isClaim) >= 3
	})
	queue.push(0, "n1")
	awaitStarted(t, api, runs, "the failed and empty claims each handed the slot back", "n1")
	runs.get(t, "n1").end(Result{ExitCode: 0}, nil, false)
	awaitFinish(t, api, "n1")
	stopWorker(t, cancel, done)
	checkClaimBodies(t, api, "w-retry", false, 0)
}

// TestRunClaimsNothingWithoutProviders: a worker whose adapters detect no
// installed CLI reports its detection and keeps polling for sign-ins, but
// never claims a run.
func TestRunClaimsNothingWithoutProviders(t *testing.T) {
	if testing.Short() {
		t.Skip("waits on the worker's 3-second sign-in ticks")
	}
	t.Parallel()
	queue := newSlotQueue(t, map[int]int{0: 1})
	queue.push(0, "n1")
	api := newFakeAPI(t, queue.respond)
	runs := newHeldRuns()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startSlotWorker(t, ctx, api, Options{WorkerID: "w-idle", Concurrency: 1, WorkspaceBase: t.TempDir()}, runs, true)

	// Two sign-in polls are six seconds: three claim ticks have passed.
	api.await(t, 15*time.Second, "two sign-in polls", func(calls []apiCall) bool {
		return count(calls, func(c apiCall) bool { return c.is("POST", "/provider-logins/claim") }) >= 2
	})
	stopWorker(t, cancel, done)
	calls := api.Calls()
	if n := count(calls, isClaim); n != 0 {
		t.Errorf("a worker with no provider sent %d claims, want none", n)
	}
	reports := api.match(func(c apiCall) bool { return c.is("POST", "/provider-settings/detect") })
	if len(reports) != 1 || !strings.Contains(string(reports[0].Body), `"installed":false`) {
		t.Errorf("detection reports = %s, want one saying fake is not installed", describeCalls(reports))
	}
}

// checkClaimBodies holds every claim to the worker's identity and pools.
func checkClaimBodies(t *testing.T, api *fakeAPI, workerID string, hosted bool, pools ...int) {
	t.Helper()
	for _, c := range api.match(isClaim) {
		var body claimBody
		c.json(t, &body)
		allowed := false
		for _, p := range pools {
			allowed = allowed || body.MinPriority == p
		}
		if body.WorkerID != workerID || body.Hosted != hosted || !allowed ||
			strings.Join(body.Providers, ",") != "fake" || c.Auth != "worker-key" {
			got := fmt.Sprintf("%+v as %s", body, c.Auth)
			want := fmt.Sprintf("worker_id %s, hosted %v, providers [fake], min_priority in %v, as worker-key", workerID, hosted, pools)
			t.Errorf("claim #%d: %s, want %s", c.Seq, got, want)
		}
	}
}
