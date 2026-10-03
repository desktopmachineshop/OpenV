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
	"github.com/openv/requirements-platform/internal/domain/agents"
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
	// Headless marks a worker with no console and no browser on its host —
	// a transient runner in a container. CLI sign-in still happens here, but
	// every step of it is relayed to the member's browser: TUI logins run
	// over a pseudo-terminal, loopback redirects are pasted back.
	Headless bool
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
	headless         bool

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
		headless:           opts.Headless,
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
		if claim.Run == nil || claim.Run.ID == "" {
			// A 200 that names no run claimed nothing: there is no run to
			// execute or to report on, so the slot goes back as for an empty
			// queue.
			log.Printf("worker %s: the claim answer named no run; nothing claimed", w.workerID)
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
// run never takes down the loop. It sequences the run's steps: preflight up
// to the start transition, the run's environment and spec, the adapter and
// its log pump, and the request that finishes the run.
func (w *Worker) execute(ctx context.Context, claim *ClaimResponse) {
	run := claim.Run
	defer func() {
		if r := recover(); r != nil {
			// The recovery must never panic itself: nothing above it would
			// catch that, and the whole process, with every run in flight,
			// would go down with this one. tryClaim hands over no claim
			// without a run, but should one arrive there is none to finish.
			if run == nil || run.ID == "" {
				log.Printf("worker %s: panic executing a claim with no run: %v\n%s", w.workerID, r, debug.Stack())
				return
			}
			log.Printf("run %s: panic: %v\n%s", run.ID, r, debug.Stack())
			_ = w.client.Finish(run.ID, agentruns.FinishRequest{
				Status:     agentruns.StatusFailed,
				Error:      "worker panic during run execution",
				ErrorClass: classifySite(sitePanic, nil),
			})
		}
	}()
	prep, ok := w.preflight(ctx, claim)
	if !ok {
		return
	}
	// Covers every early-return failure path (and the panic recovery above);
	// stopHeartbeat is idempotent, so the explicit hand-off below is fine.
	defer prep.cancelPrep()
	defer prep.stopHeartbeat()

	env, ok := w.runEnv(claim)
	if !ok {
		return
	}
	handle, err := prep.adapter.Start(ctx, w.buildRunSpec(claim, prep.workDir, env))
	if err != nil {
		w.failRun(run.ID, "adapter start failed: "+err.Error(), siteAdapterStart, err)
		return
	}

	// Hand liveness over to the log pump: it flushes (and thereby heartbeats)
	// every 750ms. stopHeartbeat blocks until any in-flight beat has
	// finished, so the pump never races a straggler push.
	prep.stopHeartbeat()
	cancelled := w.pump(run.ID, handle)
	result, waitErr := handle.Wait()
	FinishWorkspace(prep.workDir, prep.repoUsed)

	req, ok := w.finishRequestFor(ctx, run.ID, cancelled, result, waitErr)
	if !ok {
		return
	}
	w.finish(run.ID, req)
	if result.DurationMs > 0 {
		// Split the wall time so a slow run can be read off the runner log:
		// the model's share versus CLI start-up, tool calls and MCP traffic.
		log.Printf("run %s: finished (%s) in %.1fs: model %.1fs over %d turn(s), overhead %.1fs",
			run.ID, req.Status,
			float64(result.DurationMs)/1000, float64(result.DurationAPIMs)/1000, result.NumTurns,
			float64(result.DurationMs-result.DurationAPIMs)/1000)
	} else {
		log.Printf("run %s: finished (%s)", run.ID, req.Status)
	}
}

// preparedRun is what preflight hands execute: the adapter to start, the
// workspace it prepared, and the pre-pump heartbeat, still beating, with the
// context of the workspace preparation it can cancel.
type preparedRun struct {
	adapter       Adapter
	workDir       string
	repoUsed      bool
	stopHeartbeat func()
	cancelPrep    context.CancelFunc
}

// preflight takes a claimed run up to its start transition: it starts the
// pre-pump heartbeat, refuses a run that cannot succeed, prepares the
// workspace and moves the run to running. It reports false when it finished
// the run instead; the heartbeat has then stopped, and on true execute stops
// it.
//
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
func (w *Worker) preflight(ctx context.Context, claim *ClaimResponse) (prep preparedRun, ok bool) {
	run := claim.Run
	log.Printf("run %s: claimed (agent %s, provider %s)", run.ID, claim.Agent.Name, claim.Agent.Provider)
	prepCtx, cancelPrep := context.WithCancel(ctx)
	defer func() {
		if !ok {
			cancelPrep()
		}
	}()
	var prepCancelled atomic.Bool
	stopHeartbeat := startHeartbeat(prepHeartbeatInterval, func() {
		cancelRequested, _, err := w.client.PushLogs(run.ID, nil, "")
		if err != nil {
			log.Printf("run %s: heartbeat failed: %v", run.ID, err)
			return
		}
		if cancelRequested && !prepCancelled.Swap(true) {
			log.Printf("run %s: cancel requested during workspace prep", run.ID)
			cancelPrep()
		}
	})
	// Covers every early-return failure path (and a panic) here; once the
	// run is prepared, execute stops the heartbeat instead.
	defer func() {
		if !ok {
			stopHeartbeat()
		}
	}()

	adapter, found := w.adapters[claim.Agent.Provider]
	if !found {
		w.failRun(run.ID, "no adapter for provider "+claim.Agent.Provider, siteNoAdapter, nil)
		return prep, false
	}

	// No allowlist, no run (REQ-91). Checked here, before a repository is
	// cloned or a workspace built, because nothing about this run can
	// succeed; the adapters refuse it too, but only here is the agent known
	// by name, and a person reading the failed run needs to be told which
	// definition to fix.
	if len(agents.NonEmptyTools(claim.Agent.AllowedTools)) == 0 {
		w.failRun(run.ID, "agent "+agentLabel(claim.Agent)+" names no tools it may use: "+
			agents.AllowedToolsRequired, siteAgentPolicy, nil)
		return prep, false
	}

	// Repository access on a provider that cannot confine edits per tool is
	// refused here, before PrepareWorkspace clones anything (REQ-91). The
	// adapter refuses it too, but only after a clone has already been made —
	// a repository copied onto the runner host for a run that was never going
	// to start. The definition cannot be saved this way either; this catches
	// the one written before that rule, or edited on disk.
	if claim.Agent.RepoAccess && claim.Agent.Provider != providers.ProviderClaudeCode {
		w.failRun(run.ID, "agent "+agentLabel(claim.Agent)+": "+
			agents.RepoAccessUnsupported(claim.Agent.Provider).Error(), siteAgentPolicy, nil)
		return prep, false
	}

	var conns []*repoconns.RepoConnection
	if run.ProjectID != nil && claim.Agent.RepoAccess {
		var err error
		conns, err = w.client.ListRepoConnections(*run.ProjectID, claim.RunToken)
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
		return prep, false
	}
	if err != nil {
		w.failRun(run.ID, "workspace preparation failed: "+err.Error(), siteWorkspacePrep, nil)
		return prep, false
	}
	repoUsed := note != ""
	if note != "" {
		log.Printf("run %s: workspace: %s", run.ID, note)
	}

	if err := w.client.Start(run.ID); err != nil {
		w.failRun(run.ID, "start transition failed: "+err.Error(), siteStartTransition, nil)
		return prep, false
	}
	return preparedRun{
		adapter:       adapter,
		workDir:       workDir,
		repoUsed:      repoUsed,
		stopHeartbeat: stopHeartbeat,
		cancelPrep:    cancelPrep,
	}, true
}

// runEnv is the environment the run's CLI and its MCP server are given: the
// API to call and the run's token, and for a project on api-key auth the
// provider key from the runner host's environment. It reports false when it
// failed the run because that key cannot be handed over.
func (w *Worker) runEnv(claim *ClaimResponse) (map[string]string, bool) {
	run := claim.Run
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
		// The API refuses to store a name outside the provider-key catalog;
		// a row written before that rule, or an API this runner does not
		// trust, still must not turn the runner into a host-secret reader.
		if !providers.IsAllowedAPIKeyEnv(keyEnv) {
			w.failRun(run.ID, "this project's provider setting names "+keyEnv+
				" as its API key variable, which is not a provider key variable; fix the provider setting in workspace settings",
				siteAPIKeyMissing, nil)
			return nil, false
		}
		key := os.Getenv(keyEnv)
		if key == "" {
			w.failRun(run.ID, "this project uses API-key auth, but "+keyEnv+
				" is not set on the runner host — set it (or switch the project back to user-account auth)",
				siteAPIKeyMissing, nil)
			return nil, false
		}
		if native := providers.DefaultAPIKeyEnv(claim.Agent.Provider); native != "" {
			env[native] = key
		} else {
			env[keyEnv] = key
		}
	}
	return env, true
}

// buildRunSpec is what the adapter starts: the claimed run's prompt in its
// prepared workspace, under its agent's definition, with the run's
// environment for the CLI and its MCP server.
func (w *Worker) buildRunSpec(claim *ClaimResponse, workDir string, env map[string]string) RunSpec {
	run := claim.Run
	return RunSpec{
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
		// A run that reads content nobody in the workspace wrote gets nothing
		// auto-approved beyond its allowlist (REQ-91, HAZ-1). Two independent
		// sources say so and either is enough: where the run came from (an
		// interview turn is a stranger's transcript whichever agent the
		// interview was bound to) and what the definition grants (repo
		// access, web tools, a foreign MCP server).
		Untrusted:  run.UntrustedOrigin() || claim.Agent.UntrustedInput(),
		RepoAccess: claim.Agent.RepoAccess,
		MaxTurns:   claim.Agent.MaxTurns,
		TimeoutSec: claim.Agent.TimeoutSeconds,
		Env:        env,
	}
}

// finishRequestFor is the request that finishes a run whose adapter has
// returned: its result, with the status and error class of how it ended. A
// worker shutting down releases the run back to the queue instead and
// reports false, as there is then nothing to finish.
func (w *Worker) finishRequestFor(ctx context.Context, runID string, cancelled bool, result Result, waitErr error) (agentruns.FinishRequest, bool) {
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
		if err := w.client.Release(runID, w.workerID); err != nil {
			log.Printf("run %s: release on shutdown failed: %v", runID, err)
		} else {
			log.Printf("run %s: released back to queue on shutdown", runID)
		}
		return agentruns.FinishRequest{}, false
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
	return req, true
}

// failRun finishes a run as failed, with the message a person reads and the
// error class of the site that failed it.
func (w *Worker) failRun(runID, message string, site finishSite, err error) {
	w.finish(runID, agentruns.FinishRequest{
		Status:     agentruns.StatusFailed,
		Error:      message,
		ErrorClass: classifySite(site, err),
	})
}

// prepHeartbeatInterval is how often the pre-pump heartbeat refreshes a
// claimed run's liveness. Comfortably inside the server reaper's 2-minute
// stale window (runReaper, cmd/server/jobs.go).
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
// Each push also carries the assistant text written so far, when the
// provider reports any and it changed since the last successful push, so the
// chat panels can show the reply forming. The whole text is sent every time
// (not a delta), so a lost batch costs freshness and can never corrupt what
// the reader sees; agentruns.TruncatePartial applies the same cap the API
// stores it under, which keeps one runaway run from pushing megabytes through
// the log endpoint every 750ms.
func (w *Worker) pump(runID string, handle RunHandle) bool {
	var batch []agentruns.LogEntry
	seq := 0
	cancelled := false
	partialSource, _ := handle.(PartialTextSource)
	sentPartial := ""

	flush := func() {
		partial := ""
		if partialSource != nil {
			if text := agentruns.TruncatePartial(partialSource.PartialText()); text != sentPartial {
				partial = text
			}
		}
		cancelRequested, _, err := w.client.PushLogs(runID, batch, partial)
		if err != nil {
			log.Printf("run %s: push logs failed: %v", runID, err)
			return
		}
		batch = batch[:0]
		if partial != "" {
			sentPartial = partial
		}
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
