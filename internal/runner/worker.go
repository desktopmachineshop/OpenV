package runner

import (
	"context"
	"errors"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
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
	providers        []string
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
	return &Worker{
		client:           client,
		adapters:         adapters,
		workerID:         opts.WorkerID,
		concurrency:      opts.Concurrency,
		childConcurrency: opts.ChildConcurrency,
		workspaceBase:    opts.WorkspaceBase,
		mcpBinary:        opts.MCPBinary,
		apiURL:           opts.APIURL,
		hosted:           opts.Hosted,
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
			w.providers = append(w.providers, name)
			log.Printf("provider %s: installed (version %s, logged_in=%v)", name, av.Version, av.LoggedIn)
		} else {
			log.Printf("provider %s: unavailable (%s)", name, av.Detail)
		}
	}
	if err := w.client.ReportDetection(report); err != nil {
		log.Printf("report detection failed: %v", err)
	}
	if len(w.providers) == 0 {
		log.Printf("no providers available; worker will idle")
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
			return ctx.Err()
		case <-ticker.C:
			if len(w.providers) == 0 {
				continue
			}
			w.tryClaim(ctx, normalSlots, 0)
			w.tryClaim(ctx, childSlots, agentruns.PriorityChild)
		}
	}
}

// tryClaim fills as many free slots in the pool as the queue can satisfy.
func (w *Worker) tryClaim(ctx context.Context, slots chan struct{}, minPriority int) {
	for {
		select {
		case <-slots:
		default:
			return
		}
		claim, err := w.client.Claim(w.workerID, w.providers, minPriority, w.hosted)
		if err != nil {
			log.Printf("claim failed: %v", err)
			slots <- struct{}{}
			return
		}
		if claim == nil {
			slots <- struct{}{}
			return
		}
		go func() {
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
				Status: agentruns.StatusFailed,
				Error:  "worker panic during run execution",
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
	stopHeartbeat := startHeartbeat(prepHeartbeatInterval, func() {
		if _, _, err := w.client.PushLogs(run.ID, nil); err != nil {
			log.Printf("run %s: heartbeat failed: %v", run.ID, err)
		}
	})
	// Covers every early-return failure path (and the panic recovery above);
	// stopHeartbeat is idempotent, so the explicit hand-off below is fine.
	defer stopHeartbeat()

	adapter, ok := w.adapters[claim.Agent.Provider]
	if !ok {
		w.finish(run.ID, agentruns.FinishRequest{
			Status: agentruns.StatusFailed,
			Error:  "no adapter for provider " + claim.Agent.Provider,
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
	workDir, note, err := PrepareWorkspace(w.workspaceBase, run, claim.Agent, conns)
	if err != nil {
		w.finish(run.ID, agentruns.FinishRequest{
			Status: agentruns.StatusFailed,
			Error:  "workspace preparation failed: " + err.Error(),
		})
		return
	}
	repoUsed := note != ""
	if note != "" {
		log.Printf("run %s: workspace: %s", run.ID, note)
	}

	if err := w.client.Start(run.ID); err != nil {
		w.finish(run.ID, agentruns.FinishRequest{
			Status: agentruns.StatusFailed,
			Error:  "start transition failed: " + err.Error(),
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
			Status: agentruns.StatusFailed,
			Error:  "adapter start failed: " + err.Error(),
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
	case errors.Is(waitErr, context.DeadlineExceeded):
		req.Status = agentruns.StatusTimedOut
		req.Error = "run exceeded its timeout"
	case waitErr != nil:
		req.Status = agentruns.StatusFailed
		req.Error = waitErr.Error()
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
