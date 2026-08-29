package runner

import (
	"context"
	"errors"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// Options configures a Worker.
type Options struct {
	WorkerID         string
	Concurrency      int
	ChildConcurrency int
	WorkspaceBase    string
	MCPBinary        string
	APIURL           string
	// Hosted marks a platform-run token-mode worker: no interactive CLI
	// sign-in, and repo-access runs are never claimed.
	Hosted bool
	// WorkspaceRetention is how long a run's workspace directory is kept
	// before the periodic cleanup sweep removes it. Defaults to
	// defaultWorkspaceRetention when unset.
	WorkspaceRetention time.Duration
}

// Worker claims runs from the queue and executes them through provider
// adapters, streaming logs back to the API.
type Worker struct {
	client           *Client
	adapters         map[string]Adapter
	workerID         string
	concurrency      int
	childConcurrency int
	workspaceBase    string
	mcpBinary        string
	apiURL           string
	hosted           bool

	workspaceRetention time.Duration

	// runsWG tracks in-flight run goroutines so shutdown can wait for them to
	// release their claims.
	runsWG sync.WaitGroup

	// providersMu guards providers, which is read by the claim loop while the
	// login loop's post-sign-in redetect can append to it concurrently.
	providersMu sync.RWMutex
	providers   []string
}

// defaultWorkspaceRetention is how long run workspaces are kept before the
// periodic sweep removes them when Options.WorkspaceRetention is unset.
const defaultWorkspaceRetention = 24 * time.Hour

// workspaceCleanupInterval is how often the worker sweeps old workspaces.
const workspaceCleanupInterval = time.Hour

// addProvider records a provider as available, de-duplicating. Safe for
// concurrent use.
func (w *Worker) addProvider(name string) {
	w.providersMu.Lock()
	defer w.providersMu.Unlock()
	for _, p := range w.providers {
		if p == name {
			return
		}
	}
	w.providers = append(w.providers, name)
}

// snapshotProviders returns a copy of the available providers, safe to read
// without holding the lock.
func (w *Worker) snapshotProviders() []string {
	w.providersMu.RLock()
	defer w.providersMu.RUnlock()
	if len(w.providers) == 0 {
		return nil
	}
	out := make([]string, len(w.providers))
	copy(out, w.providers)
	return out
}

// NewWorker wires a worker from a client and the adapter registry.
func NewWorker(client *Client, opts Options) *Worker {
	adapters := map[string]Adapter{}
	for _, a := range Registry() {
		adapters[a.Name()] = a
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.ChildConcurrency < 0 {
		opts.ChildConcurrency = 0
	}
	if opts.WorkerID == "" {
		opts.WorkerID = "worker"
	}
	if opts.WorkspaceRetention <= 0 {
		opts.WorkspaceRetention = defaultWorkspaceRetention
	}
	return &Worker{
		client:             client,
		adapters:           adapters,
		workerID:           opts.WorkerID,
		concurrency:        opts.Concurrency,
		childConcurrency:   opts.ChildConcurrency,
		workspaceBase:      opts.WorkspaceBase,
		mcpBinary:          opts.MCPBinary,
		apiURL:             opts.APIURL,
		hosted:             opts.Hosted,
		workspaceRetention: opts.WorkspaceRetention,
	}
}

// Run detects providers, then polls the queue until the context ends.
func (w *Worker) Run(ctx context.Context) error {
	report := map[string]map[string]interface{}{}
	for name, adapter := range w.adapters {
		av := adapter.Detect(ctx)
		report[name] = map[string]interface{}{
			"installed": av.Installed,
			"version":   av.Version,
			"logged_in": av.LoggedIn,
			"detail":    av.Detail,
		}
		if av.Installed {
			w.addProvider(name)
			log.Printf("provider %s: installed (version %s, logged_in=%v)", name, av.Version, av.LoggedIn)
		} else {
			log.Printf("provider %s: unavailable (%s)", name, av.Detail)
		}
	}
	if err := w.client.ReportDetection(report); err != nil {
		log.Printf("report detection failed: %v", err)
	}
	if len(w.snapshotProviders()) == 0 {
		log.Printf("no providers available; worker will idle")
	}

	// Reclaim disk from prior runs: sweep once at startup, then periodically.
	// (worker.go historically claimed this happened but never wired it up, so
	// clones accumulated forever.)
	if w.workspaceBase != "" {
		CleanupOld(w.workspaceBase, w.workspaceRetention)
		go w.cleanupLoop(ctx)
	}

	// Provider sign-in requests from the UI are handled alongside runs
	// (personal/workspace machines only — hosted runners are token-mode).
	if !w.hosted {
		go w.loginLoop(ctx)
	}

	// Slot pools: normal runs plus dedicated child/interview slots so a
	// parent blocked on delegation can't starve its children.
	normalSlots := make(chan struct{}, w.concurrency)
	for i := 0; i < w.concurrency; i++ {
		normalSlots <- struct{}{}
	}
	childSlots := make(chan struct{}, w.childConcurrency)
	for i := 0; i < w.childConcurrency; i++ {
		childSlots <- struct{}{}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Give in-flight runs a moment to release their claims (see
			// execute) before returning, so a Ctrl-C actually hands runs back
			// to the queue instead of leaving them stranded until the reaper.
			w.drainRuns()
			return ctx.Err()
		case <-ticker.C:
			if len(w.snapshotProviders()) == 0 {
				continue
			}
			w.tryClaim(ctx, normalSlots, 0)
			w.tryClaim(ctx, childSlots, agentruns.PriorityChild)
		}
	}
}

// shutdownGrace bounds how long Run waits for in-flight runs to release their
// claims on shutdown before returning regardless.
const shutdownGrace = 30 * time.Second

// drainRuns waits for in-flight run goroutines to finish (each releasing its
// claim on shutdown), bounded by shutdownGrace so a wedged run can't hang the
// process forever.
func (w *Worker) drainRuns() {
	done := make(chan struct{})
	go func() { w.runsWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		log.Printf("shutdown: gave up waiting for in-flight runs after %s", shutdownGrace)
	}
}

// cleanupLoop periodically removes run workspaces older than the retention
// window until the context ends.
func (w *Worker) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(workspaceCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			CleanupOld(w.workspaceBase, w.workspaceRetention)
		}
	}
}

// tryClaim fills as many free slots in the pool as the queue can satisfy.
func (w *Worker) tryClaim(ctx context.Context, slots chan struct{}, minPriority int) {
	provs := w.snapshotProviders()
	for {
		select {
		case <-slots:
		default:
			return
		}
		claim, err := w.client.Claim(w.workerID, provs, minPriority, w.hosted)
		if err != nil {
			log.Printf("claim failed: %v", err)
			slots <- struct{}{}
			return
		}
		if claim == nil {
			slots <- struct{}{}
			return
		}
		w.runsWG.Add(1)
		go func() {
			defer w.runsWG.Done()
			defer func() { slots <- struct{}{} }()
			w.execute(ctx, claim)
		}()
	}
}

// execute runs one claimed run end to end, recovering from panics so a bad
// run never takes down the loop.
func (w *Worker) execute(ctx context.Context, claim *ClaimResponse) {
	run := claim.Run
	defer func() {
		if r := recover(); r != nil {
			log.Printf("run %s: panic: %v\n%s", run.ID, r, debug.Stack())
			_ = w.client.Finish(run.ID, agentruns.FinishRequest{
				Status:     agentruns.StatusFailed,
				Error:      "worker panic during run execution",
				ErrorClass: classifySite(sitePanic, nil),
			})
		}
	}()
	log.Printf("run %s: claimed (agent %s, provider %s)", run.ID, claim.Agent.Name, claim.Agent.Provider)

	// heartbeat_at is stamped at claim time, but the next refresh would
	// otherwise come from the log pump, which only starts after the adapter
	// does. Everything in between — PrepareWorkspace in particular clones
	// real repositories — can outlast the server reaper's stale window on a
	// slow network or a large repo, deterministically failing the run before
	// it ever starts. Beat explicitly until the pump takes over: an empty
	// log push is exactly the heartbeat call the pump itself makes.
	//
	// The heartbeat response also carries cancel_requested, so a run
	// cancelled during workspace prep aborts the prep (prepCtx cancels any
	// in-flight git clone) instead of running to completion anyway.
	prepCtx, cancelPrep := context.WithCancel(ctx)
	defer cancelPrep()
	var prepCancelled atomic.Bool
	stopHeartbeat := startHeartbeat(prepHeartbeatInterval, func() {
		cancelRequested, _, err := w.client.PushLogs(run.ID, nil)
		if err != nil {
			log.Printf("run %s: heartbeat failed: %v", run.ID, err)
			return
		}
		if cancelRequested && !prepCancelled.Swap(true) {
			log.Printf("run %s: cancel requested during workspace prep", run.ID)
			cancelPrep()
		}
	})
	// Covers every early-return failure path (and the panic recovery above);
	// stopHeartbeat is idempotent, so the explicit hand-off below is fine.
	defer stopHeartbeat()

	adapter, ok := w.adapters[claim.Agent.Provider]
	if !ok {
		w.finish(run.ID, agentruns.FinishRequest{
			Status:     agentruns.StatusFailed,
			Error:      "no adapter for provider " + claim.Agent.Provider,
			ErrorClass: classifySite(siteNoAdapter, nil),
		})
		return
	}

	var conns []*repoconns.RepoConnection
	if run.ProjectID != nil && claim.Agent.RepoAccess {
		var err error
		conns, err = w.client.ListRepoConnections(*run.ProjectID)
		if err != nil {
			log.Printf("run %s: listing repo connections failed: %v", run.ID, err)
		}
	}
	workDir, note, err := PrepareWorkspace(prepCtx, w.workspaceBase, run, claim.Agent, conns)
	if prepCancelled.Load() {
		// Cancelled mid-prep: the abandoned workspace directory is reaped by
		// the periodic CleanupOld sweep, like any failed run's.
		w.finish(run.ID, agentruns.FinishRequest{Status: agentruns.StatusCancelled})
		log.Printf("run %s: cancelled during workspace prep", run.ID)
		return
	}
	if err != nil {
		w.finish(run.ID, agentruns.FinishRequest{
			Status:     agentruns.StatusFailed,
			Error:      "workspace preparation failed: " + err.Error(),
			ErrorClass: classifySite(siteWorkspacePrep, nil),
		})
		return
	}
	repoUsed := note != ""
	if note != "" {
		log.Printf("run %s: workspace: %s", run.ID, note)
	}

	if err := w.client.Start(run.ID); err != nil {
		w.finish(run.ID, agentruns.FinishRequest{
			Status:     agentruns.StatusFailed,
			Error:      "start transition failed: " + err.Error(),
			ErrorClass: classifySite(siteStartTransition, nil),
		})
		return
	}

	env := map[string]string{
		"OPENV_API_URL":   w.apiURL,
		"OPENV_RUN_TOKEN": claim.RunToken,
	}
	// A project on api-key auth overrides the host's CLI sign-in: inject the
	// configured key from the runner host's environment into the variable the
	// provider CLI natively reads. The run still executes on this runner.
	if claim.Auth != nil && claim.Auth.Mode == "api-key" {
		keyEnv := claim.Auth.APIKeyEnv
		if keyEnv == "" {
			keyEnv = providers.DefaultAPIKeyEnv(claim.Agent.Provider)
		}
		key := os.Getenv(keyEnv)
		if key == "" {
			w.finish(run.ID, agentruns.FinishRequest{
				Status: agentruns.StatusFailed,
				Error: "this project uses API-key auth, but " + keyEnv +
					" is not set on the runner host — set it (or switch the project back to user-account auth)",
				ErrorClass: classifySite(siteAPIKeyMissing, nil),
			})
			return
		}
		if native := providers.DefaultAPIKeyEnv(claim.Agent.Provider); native != "" {
			env[native] = key
		} else {
			env[keyEnv] = key
		}
	}
	spec := RunSpec{
		RunID:   run.ID,
		WorkDir: workDir,
		Prompt:  run.Prompt,
		// Every agent gets the standing answer-length rule so final answers
		// stay inside the budget the log windows are sized for.
		SystemPrompt: strings.TrimSpace(claim.Agent.SystemPrompt + agentruns.AnswerLengthRule),
		Model:        claim.Agent.Model,
		Effort:       claim.Agent.Effort,
		MCP: MCPServerConfig{
			Command: w.mcpBinary,
			Env:     env,
		},
		AllowedTools: claim.Agent.AllowedTools,
		MaxTurns:     claim.Agent.MaxTurns,
		TimeoutSec:   claim.Agent.TimeoutSeconds,
		Env:          env,
	}

	handle, err := adapter.Start(ctx, spec)
	if err != nil {
		w.finish(run.ID, agentruns.FinishRequest{
			Status:     agentruns.StatusFailed,
			Error:      "adapter start failed: " + err.Error(),
			ErrorClass: classifySite(siteAdapterStart, nil),
		})
		return
	}

	// Hand liveness over to the log pump: it flushes (and thereby heartbeats)
	// every 750ms. stopHeartbeat blocks until any in-flight beat has
	// finished, so the pump never races a straggler push.
	stopHeartbeat()
	cancelled := w.pump(run.ID, handle)
	result, waitErr := handle.Wait()
	FinishWorkspace(workDir, repoUsed)

	exitCode := result.ExitCode
	req := agentruns.FinishRequest{
		ExitCode:  &exitCode,
		FinalText: result.FinalText,
		TokensIn:  result.TokensIn,
		TokensOut: result.TokensOut,
		CostUSD:   result.CostUSD,
	}
	switch {
	case cancelled:
		req.Status = agentruns.StatusCancelled
	case ctx.Err() != nil:
		// The worker itself is shutting down (Ctrl-C / SIGINT), which killed
		// this run's subprocess mid-flight. That's not the run's fault, so
		// release the claim back to the queue for another (or a restarted)
		// worker to pick up rather than burning it as failed.
		if err := w.client.Release(run.ID, w.workerID); err != nil {
			log.Printf("run %s: release on shutdown failed: %v", run.ID, err)
		} else {
			log.Printf("run %s: released back to queue on shutdown", run.ID)
		}
		return
	case errors.Is(waitErr, context.DeadlineExceeded):
		req.Status = agentruns.StatusTimedOut
		req.Error = "run exceeded its timeout"
		req.ErrorClass = classifySite(siteTimeout, waitErr)
		// Preserve the parser's real error alongside the timeout verdict
		// instead of discarding it — the underlying detail is often the only
		// clue to what the run was stuck on.
		var te *timeoutError
		if errors.As(waitErr, &te) && te.detail != nil {
			req.Error += " (" + te.detail.Error() + ")"
		}
	case waitErr != nil:
		req.Status = agentruns.StatusFailed
		req.Error = waitErr.Error()
		// Classify from the CLI's failure text: an auth/provider problem the
		// agent surfaced vs. a genuine agent error (see classifyAgentError).
		req.ErrorClass = classifySite(siteAgentResult, waitErr)
	default:
		req.Status = agentruns.StatusSucceeded
	}
	w.finish(run.ID, req)
	log.Printf("run %s: finished (%s)", run.ID, req.Status)
}

// prepHeartbeatInterval is how often the pre-pump heartbeat refreshes a
// claimed run's liveness. Comfortably inside the server reaper's 2-minute
// stale window (cmd/server/main.go).
const prepHeartbeatInterval = 30 * time.Second

// startHeartbeat invokes beat every interval on a background goroutine until
// the returned stop function is called. stop is idempotent and blocks until
// the goroutine has fully exited — including any beat in flight — so a caller
// can hand heartbeating over to another mechanism without two writers racing.
func startHeartbeat(interval time.Duration, beat func()) (stop func()) {
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				beat()
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stopCh)
			<-done
		})
	}
}

// pump batches run events into log pushes every 750ms until the event
// channel closes; returns true when the run was cancelled server-side.
func (w *Worker) pump(runID string, handle RunHandle) bool {
	var batch []agentruns.LogEntry
	seq := 0
	cancelled := false

	flush := func() {
		cancelRequested, _, err := w.client.PushLogs(runID, batch)
		if err != nil {
			log.Printf("run %s: push logs failed: %v", runID, err)
			return
		}
		batch = batch[:0]
		if cancelRequested && !cancelled {
			cancelled = true
			handle.Cancel()
		}
	}

	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case ev, ok := <-handle.Events():
			if !ok {
				flush()
				return cancelled
			}
			seq++
			batch = append(batch, agentruns.LogEntry{
				RunID:   runID,
				Seq:     seq,
				Kind:    ev.Kind,
				Payload: ev.Payload,
			})
		case <-ticker.C:
			flush()
		}
	}
}

func (w *Worker) finish(runID string, req agentruns.FinishRequest) {
	if err := w.client.Finish(runID, req); err != nil {
		log.Printf("run %s: finish failed: %v", runID, err)
	}
}
