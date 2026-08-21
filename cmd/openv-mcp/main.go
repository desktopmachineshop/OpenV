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
	token := os.Getenv("OPENV_RUN_TOKEN")
	if token == "" {
		log.Fatal("OPENV_RUN_TOKEN is required")
	}

	client := mcp.NewClient(apiURL, token)
	if err := mcp.ServeStdio(client, mcp.Tools()); err != nil {
		log.Fatalf("mcp server stopped: %v", err)
	}
}
