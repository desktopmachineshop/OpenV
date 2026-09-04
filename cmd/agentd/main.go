package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/openv/requirements-platform/internal/runner"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func defaultWorkspaces() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".openv-workspaces"
	}
	return filepath.Join(home, ".openv", "workspaces")
}

func defaultMCPBinary() string {
	name := "openv-mcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(exe), name)
}

// runPool serves leases until the process is interrupted.
func runPool(apiURL, poolKey, pool, nodeName, sessionRoot, workspaces, mcpBinary string, concurrency, childConcurrency int) {
	if sessionRoot == "" {
		sessionRoot = filepath.Join(workspaces, "sessions")
	}
	if err := os.MkdirAll(sessionRoot, 0o700); err != nil {
		log.Fatalf("create session root: %v", err)
	}
	agent := runner.NewPoolAgent(runner.PoolOptions{
		APIURL:           apiURL,
		PoolKey:          poolKey,
		Pool:             pool,
		NodeName:         nodeName,
		SessionRoot:      sessionRoot,
		WorkspaceBase:    filepath.Join(workspaces, "runs"),
		MCPBinary:        mcpBinary,
		Concurrency:      concurrency,
		ChildConcurrency: childConcurrency,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("agentd pool node starting (api %s, sessions %s)", apiURL, sessionRoot)
	if err := agent.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("pool node stopped: %v", err)
	}
	log.Print("agentd pool node stopped")
}

func main() {
	apiURL := flag.String("api", envOr("OPENV_API_URL", "http://localhost:8080"), "OpenV API base URL")
	workerKey := flag.String("worker-key", os.Getenv("WORKER_API_KEY"), "worker API key (required)")
	concurrency := flag.Int("concurrency", envIntOr("AGENT_CONCURRENCY", 1), "concurrent normal runs")
	childConcurrency := flag.Int("child-concurrency", envIntOr("AGENT_CHILD_CONCURRENCY", 2), "extra slots reserved for child/interview runs")
	workspaces := flag.String("workspaces", defaultWorkspaces(), "base directory for run workspaces")
	mcpBinary := flag.String("mcp-binary", defaultMCPBinary(), "path to the openv-mcp binary")
	hosted := flag.Bool("hosted", envOr("OPENV_HOSTED", "") == "true", "token-mode hosted runner: no CLI sign-in, no repo-access runs")
	workspaceRetention := flag.Duration("workspace-retention", envDurationOr("AGENT_WORKSPACE_RETENTION", 24*time.Hour), "how long finished run workspaces are kept before cleanup")
	poolKey := flag.String("pool-key", os.Getenv("RUNNER_POOL_KEY"), "transient runner pool key: run as a pre-warmed pool node instead of a fixed runner")
	pool := flag.String("pool", envOr("RUNNER_POOL", ""), "pool this node belongs to (default \"default\")")
	nodeName := flag.String("node-name", envOr("RUNNER_NODE_NAME", ""), "stable name for this pool node (default: hostname)")
	sessionRoot := flag.String("session-root", envOr("RUNNER_SESSION_ROOT", ""), "parent directory for per-lease HOME directories (pool mode)")
	flag.Parse()

	// Pool mode: this process holds no workspace credential of its own. It
	// waits to be leased by a member, becomes their runner for the length of
	// the lease, and wipes everything the lease wrote when it ends.
	if *poolKey != "" {
		runPool(*apiURL, *poolKey, *pool, *nodeName, *sessionRoot, *workspaces, *mcpBinary, *concurrency, *childConcurrency)
		return
	}

	if *workerKey == "" {
		log.Fatal("worker key is required (--worker-key or WORKER_API_KEY)")
	}
	if err := os.MkdirAll(*workspaces, 0o755); err != nil {
		log.Fatalf("create workspace dir: %v", err)
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "agentd"
	}
	workerID := hostname + "-" + strconv.Itoa(os.Getpid())

	client := runner.NewClient(*apiURL, *workerKey)
	worker := runner.NewWorker(client, runner.Options{
		WorkerID:           workerID,
		Concurrency:        *concurrency,
		ChildConcurrency:   *childConcurrency,
		WorkspaceBase:      *workspaces,
		MCPBinary:          *mcpBinary,
		APIURL:             *apiURL,
		Hosted:             *hosted,
		WorkspaceRetention: *workspaceRetention,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Printf("agentd %s starting (api %s, workspaces %s)", workerID, *apiURL, *workspaces)
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("worker stopped: %v", err)
	}
	log.Print("agentd stopped")
}
