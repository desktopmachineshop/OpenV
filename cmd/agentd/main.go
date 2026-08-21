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

func main() {
	apiURL := flag.String("api", envOr("OPENV_API_URL", "http://localhost:8080"), "OpenV API base URL")
	workerKey := flag.String("worker-key", os.Getenv("WORKER_API_KEY"), "worker API key (required)")
	concurrency := flag.Int("concurrency", envIntOr("AGENT_CONCURRENCY", 1), "concurrent normal runs")
	childConcurrency := flag.Int("child-concurrency", envIntOr("AGENT_CHILD_CONCURRENCY", 2), "extra slots reserved for child/interview runs")
	workspaces := flag.String("workspaces", defaultWorkspaces(), "base directory for run workspaces")
	mcpBinary := flag.String("mcp-binary", defaultMCPBinary(), "path to the openv-mcp binary")
	hosted := flag.Bool("hosted", envOr("OPENV_HOSTED", "") == "true", "token-mode hosted runner: no CLI sign-in, no repo-access runs")
	flag.Parse()

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
		WorkerID:         workerID,
		Concurrency:      *concurrency,
		ChildConcurrency: *childConcurrency,
		WorkspaceBase:    *workspaces,
		MCPBinary:        *mcpBinary,
		APIURL:           *apiURL,
		Hosted:           *hosted,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	log.Printf("agentd %s starting (api %s, workspaces %s)", workerID, *apiURL, *workspaces)
	if err := worker.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("worker stopped: %v", err)
	}
	log.Print("agentd stopped")
}
