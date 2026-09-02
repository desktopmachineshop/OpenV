package main

import (
	"log"
	"os"

	"github.com/openv/requirements-platform/internal/mcp"
)

func main() {
	// Log to stderr only; stdout carries the JSON-RPC stream.
	log.SetOutput(os.Stderr)

	apiURL := os.Getenv("OPENV_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	token := resolveToken(os.Getenv)
	if token == "" {
		log.Fatal("OPENV_RUN_TOKEN (agent runs) or OPENV_API_TOKEN (workspace runner key) is required")
	}

	client := mcp.NewClient(apiURL, token)
	if err := mcp.ServeStdio(client, mcp.Tools()); err != nil {
		log.Fatalf("mcp server stopped: %v", err)
	}
}

// resolveToken picks the credential the tools authenticate with. agentd
// injects OPENV_RUN_TOKEN, the token minted for one agent run, and it wins
// where both are set. A session that is not a platform-launched run — an
// agent maintaining the requirements project, a developer at their own CLI —
// presents a workspace runner key in OPENV_API_TOKEN instead. The API takes
// either on the same header, and the credential, not this server, decides
// what the tools may touch.
func resolveToken(env func(string) string) string {
	if token := env("OPENV_RUN_TOKEN"); token != "" {
		return token
	}
	return env("OPENV_API_TOKEN")
}
