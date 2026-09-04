package runner

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/runnersessions"
)

// A pool node is a pre-warmed agentd process that belongs to nobody until a
// member leases it. It registers with the deployment's pool key, heartbeats,
// and does nothing else until the API hands it an assignment. Then it becomes
// that member's personal runner for the length of the lease — claiming their
// runs, executing their CLI sign-ins — and when the lease ends it deletes
// everything the session wrote and reports itself free again.
//
// Nothing of a lease survives it. The session HOME (where the vendor CLIs
// keep their credentials) is a directory created for the lease and removed
// when it ends, and the session's API credential is revoked server-side at
// the same moment. That is why the next lease starts with a sign-in: it is
// the design, not a gap in it.

// PoolOptions configures a pool node.
type PoolOptions struct {
	// APIURL is the OpenV API base URL.
	APIURL string
	// PoolKey is the deployment's shared pool credential (RUNNER_POOL_KEY).
	PoolKey string
	// Pool names the group this node belongs to.
	Pool string
	// NodeName identifies the node across restarts (defaults to hostname).
	NodeName string
	// SessionRoot is the parent directory for per-lease HOME directories.
	SessionRoot string
	// WorkspaceBase is the parent directory for run workspaces.
	WorkspaceBase string
	// MCPBinary is the path to openv-mcp.
	MCPBinary string
	// Concurrency and ChildConcurrency size the leased worker's slots.
	Concurrency      int
	ChildConcurrency int
}

// PoolAgent runs one pool node.
type PoolAgent struct {
	opts   PoolOptions
	client *Client
	nodeID string

	// baseHome is the process's own HOME, restored after each lease.
	baseHome string

	mu      sync.Mutex
	current *leasedSession
}

// leasedSession is the state of the lease currently being served.
type leasedSession struct {
	id     string
	home   string
	cancel context.CancelFunc
	done   chan struct{}
}

// NewPoolAgent creates a pool node.
func NewPoolAgent(opts PoolOptions) *PoolAgent {
	if opts.Pool == "" {
		opts.Pool = runnersessions.DefaultPool
	}
	if opts.NodeName == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "pool-node"
		}
		opts.NodeName = host
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	return &PoolAgent{
		opts:     opts,
		client:   NewClient(opts.APIURL, opts.PoolKey),
		baseHome: os.Getenv("HOME"),
	}
}

// Run registers the node and serves leases until ctx is canceled.
func (p *PoolAgent) Run(ctx context.Context) error {
	// Anything left behind by a previous process (a crash mid-lease) is not
	// ours to keep: wipe the session root before announcing availability.
	p.purgeSessionRoot()

	if err := p.register(ctx); err != nil {
		return err
	}
	log.Printf("pool node %s registered as %s (pool %s)", p.opts.NodeName, p.nodeID, p.opts.Pool)

	ticker := time.NewTicker(runnersessions.NodeHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// A shutdown ends the lease the same way an expiry does: the
			// member's credentials do not outlive the process.
			p.endLease(ctx, "shutdown")
			return ctx.Err()
		case <-ticker.C:
			p.beat(ctx)
		}
	}
}

// register announces the node, retrying until the API accepts it — a pool
// node that starts before the API is up must not simply die.
func (p *PoolAgent) register(ctx context.Context) error {
	providers := detectedProviders(ctx)
	for attempt := 0; ; attempt++ {
		node, err := p.client.RegisterPoolNode(p.opts.NodeName, p.opts.Pool, providers)
		if err == nil {
			p.nodeID = node.ID
			return nil
		}
		log.Printf("pool registration failed (attempt %d): %v", attempt+1, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(registerBackoff(attempt)):
		}
	}
}

// registerBackoff grows to a 30s ceiling.
func registerBackoff(attempt int) time.Duration {
	d := time.Duration(attempt+1) * 2 * time.Second
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// detectedProviders lists the vendor CLIs installed on this node, so the API
// can tell a member what they will be able to sign in to.
func detectedProviders(ctx context.Context) []string {
	var found []string
	for _, adapter := range Registry() {
		if adapter.Detect(ctx).Installed {
			found = append(found, adapter.Name())
		}
	}
	return found
}

// beat sends one heartbeat and reconciles the node with what it learns.
func (p *PoolAgent) beat(ctx context.Context) {
	assignment, err := p.client.PoolHeartbeat(p.nodeID)
	if err != nil {
		if errors.Is(err, ErrPoolNodeUnknown) {
			log.Printf("pool node no longer registered; re-registering")
			p.endLease(ctx, "node dropped")
			if err := p.register(ctx); err != nil {
				log.Printf("re-registration failed: %v", err)
			}
			return
		}
		log.Printf("pool heartbeat failed: %v", err)
		return
	}

	p.mu.Lock()
	current := p.current
	p.mu.Unlock()

	switch {
	case assignment == nil && current != nil:
		// The lease is over (expired, idle, or ended by the member).
		p.endLease(ctx, "lease ended")
	case assignment != nil && current == nil:
		if assignment.WorkerKey == "" {
			// The key is handed over exactly once; without it there is
			// nothing to authenticate as. The lease will lapse and the
			// member can start another.
			log.Printf("lease %s arrived without a credential; ignoring", assignment.SessionID)
			return
		}
		p.startLease(ctx, assignment)
	case assignment != nil && current != nil && assignment.SessionID != current.id:
		// A new lease while an old one is still running: the old one is
		// finished, whatever the API said about it.
		p.endLease(ctx, "superseded")
		if assignment.WorkerKey != "" {
			p.startLease(ctx, assignment)
		}
	}
}

// startLease turns the node into one member's personal runner.
func (p *PoolAgent) startLease(parent context.Context, a *PoolAssignment) {
	home := filepath.Join(p.opts.SessionRoot, a.SessionID)
	if err := os.MkdirAll(home, 0o700); err != nil {
		log.Printf("lease %s: cannot create session home: %v", a.SessionID, err)
		return
	}
	// The vendor CLIs keep their credentials under HOME. Pointing HOME at a
	// per-lease directory is what keeps one member's sign-in out of the
	// next member's runner — and makes the wipe a single RemoveAll.
	// The node serves one lease at a time, so a process-wide HOME is
	// unambiguous here.
	if err := os.Setenv("HOME", home); err != nil {
		log.Printf("lease %s: cannot set session HOME: %v", a.SessionID, err)
		return
	}

	workspaces := filepath.Join(home, "workspaces")
	if p.opts.WorkspaceBase != "" {
		workspaces = filepath.Join(p.opts.WorkspaceBase, a.SessionID)
	}
	if err := os.MkdirAll(workspaces, 0o700); err != nil {
		log.Printf("lease %s: cannot create workspace dir: %v", a.SessionID, err)
		return
	}

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	worker := NewWorker(NewClient(p.opts.APIURL, a.WorkerKey), Options{
		WorkerID:         p.opts.NodeName + "-" + shortID(a.SessionID),
		Concurrency:      p.opts.Concurrency,
		ChildConcurrency: p.opts.ChildConcurrency,
		WorkspaceBase:    workspaces,
		MCPBinary:        p.opts.MCPBinary,
		APIURL:           p.opts.APIURL,
		// A transient runner is the member's own runner, not a token-mode
		// hosted one: they sign their own CLIs into it. It is headless,
		// though — every step of that sign-in is relayed to their browser.
		Headless: true,
	})

	p.mu.Lock()
	p.current = &leasedSession{id: a.SessionID, home: home, cancel: cancel, done: done}
	p.mu.Unlock()

	who := a.UserName
	if who == "" {
		who = a.UserID
	}
	log.Printf("lease %s: serving %s (workspace %s) until %s", a.SessionID, who, a.OrgID, a.ExpiresAt.Format(time.RFC3339))

	go func() {
		defer close(done)
		if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("lease %s: worker stopped: %v", a.SessionID, err)
		}
	}()
}

// endLease stops the leased worker, deletes everything the lease wrote, and
// tells the API the node is free. The wipe happens even if the API is
// unreachable — the member's credentials are not kept around waiting for a
// successful HTTP call.
func (p *PoolAgent) endLease(ctx context.Context, reason string) {
	p.mu.Lock()
	current := p.current
	p.current = nil
	p.mu.Unlock()
	if current == nil {
		return
	}

	log.Printf("lease %s: ending (%s)", current.id, reason)
	current.cancel()
	select {
	case <-current.done:
	case <-time.After(30 * time.Second):
		log.Printf("lease %s: worker did not stop in time; wiping anyway", current.id)
	}

	if p.baseHome != "" {
		_ = os.Setenv("HOME", p.baseHome)
	}
	if err := os.RemoveAll(current.home); err != nil {
		log.Printf("lease %s: wiping session home failed: %v", current.id, err)
	}
	if p.opts.WorkspaceBase != "" {
		if err := os.RemoveAll(filepath.Join(p.opts.WorkspaceBase, current.id)); err != nil {
			log.Printf("lease %s: wiping workspaces failed: %v", current.id, err)
		}
	}

	if ctx.Err() != nil {
		// Shutting down: the API will reclaim the node when the heartbeat
		// stops, so there is nothing useful to report.
		return
	}
	if err := p.client.ReleasePoolNode(p.nodeID, current.id); err != nil {
		log.Printf("lease %s: release report failed: %v", current.id, err)
	}
}

// purgeSessionRoot clears leftovers from a previous process.
func (p *PoolAgent) purgeSessionRoot() {
	if p.opts.SessionRoot == "" {
		return
	}
	entries, err := os.ReadDir(p.opts.SessionRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(p.opts.SessionRoot, entry.Name())); err != nil {
			log.Printf("purging stale session state failed: %v", err)
		}
	}
}

// shortID trims a uuid for use in a worker id.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
