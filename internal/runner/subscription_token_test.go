package runner

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
)

// The member's subscription token reaches the CLI's environment and never
// the MCP server's, which the Claude adapter writes into .openv/mcp.json.
func TestSubscriptionTokenGoesToTheCLIOnly(t *testing.T) {
	w := &Worker{}
	env := map[string]string{"OPENV_API_URL": "http://api", "OPENV_RUN_TOKEN": "run-tok"}
	claim := &ClaimResponse{
		Agent: &agents.Agent{Provider: "claude-code"},
		Run:   &agentruns.Run{ID: "run-1"},
		Auth:  &RunAuth{Mode: "subscription-token", OAuthToken: "sk-ant-oat01-secret"},
	}
	spec := w.buildRunSpec(claim, t.TempDir(), env)
	if spec.Env[subscriptionTokenEnv] != "sk-ant-oat01-secret" {
		t.Fatalf("CLI env lacks the token: %v", spec.Env)
	}
	if _, ok := spec.MCP.Env[subscriptionTokenEnv]; ok {
		t.Fatal("token leaked into the MCP server env")
	}
	if _, ok := env[subscriptionTokenEnv]; ok {
		t.Fatal("the shared run env was mutated")
	}

	for name, c := range map[string]*ClaimResponse{
		"user-account": {Agent: claim.Agent, Run: claim.Run, Auth: &RunAuth{Mode: "user-account", OAuthToken: "x"}},
		"codex":        {Agent: &agents.Agent{Provider: "codex-cli"}, Run: claim.Run, Auth: claim.Auth},
		"no auth":      {Agent: claim.Agent, Run: claim.Run},
	} {
		if got := w.buildRunSpec(c, t.TempDir(), env); got.Env[subscriptionTokenEnv] != "" {
			t.Errorf("%s: token set: %v", name, got.Env)
		}
	}
}
