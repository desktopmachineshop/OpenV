package runner

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/mcp"
)

func codexSpec() RunSpec {
	return RunSpec{
		WorkDir: "/work",
		Prompt:  "do the thing",
		Model:   "gpt-5-codex",
		MCP: MCPServerConfig{
			Command: `C:\Program Files\openv\mcp.exe`,
			Args:    []string{"mcp", "--stdio"},
			Env: map[string]string{
				"OPENV_RUN_TOKEN": "super-secret-token-123",
				"OPENV_API_URL":   "http://api.local",
			},
		},
	}
}

// The live run token must never appear anywhere in codex's argv — it travels
// through the process environment and is forwarded to the MCP server by name.
func TestBuildCodexArgs_TokenNotInArgv(t *testing.T) {
	spec := codexSpec()
	args, err := codexArgs(spec)
	if err != nil {
		t.Fatalf("codexArgs: %v", err)
	}
	joined := strings.Join(args, "\x00")
	if strings.Contains(joined, "super-secret-token-123") {
		t.Fatalf("run token leaked into argv: %v", args)
	}
	if strings.Contains(joined, "mcp_servers.openv.env.") {
		t.Fatalf("per-key env override still present (leaks values): %v", args)
	}
}

// The token variables are forwarded by NAME via env_vars, in a stable order.
func TestBuildCodexArgs_ForwardsEnvByName(t *testing.T) {
	args, err := codexArgs(codexSpec())
	if err != nil {
		t.Fatalf("codexArgs: %v", err)
	}
	var envVars string
	for i, a := range args {
		if strings.HasPrefix(a, "mcp_servers.openv.env_vars=") {
			envVars = a
			if i == 0 || args[i-1] != "-c" {
				t.Errorf("env_vars override not preceded by -c: %v", args)
			}
		}
	}
	if envVars == "" {
		t.Fatalf("no env_vars override found: %v", args)
	}
	// Sorted: OPENV_API_URL before OPENV_RUN_TOKEN.
	want := `mcp_servers.openv.env_vars=["OPENV_API_URL","OPENV_RUN_TOKEN"]`
	if envVars != want {
		t.Errorf("env_vars = %q, want %q", envVars, want)
	}
}

// A Windows path (spaces + backslashes) must be JSON-escaped so codex parses it
// as one literal string.
func TestBuildCodexArgs_CommandEscaped(t *testing.T) {
	args, err := codexArgs(codexSpec())
	if err != nil {
		t.Fatalf("codexArgs: %v", err)
	}
	want := `mcp_servers.openv.command="C:\\Program Files\\openv\\mcp.exe"`
	found := false
	for _, a := range args {
		if a == want {
			found = true
		}
	}
	if !found {
		t.Errorf("command override not JSON-escaped; want %q in %v", want, args)
	}
}

// What codex exec refuses, and what it merely cannot apply. An empty
// allowlist is refused (that is the case where the CLI would run with every
// tool it has, REQ-91) and so is repository access (codex's only lever is a
// whole-workspace sandbox). A non-empty allowlist runs — codex is confined
// instead — and so does a MaxTurns, which is a logged no-op rather than a
// refusal: Validate gives every persisted definition one, so refusing it
// refused every real codex agent.
func TestBuildCodexArgs_UnsupportedCapsError(t *testing.T) {
	t.Run("max_turns is a no-op, not a refusal", func(t *testing.T) {
		spec := codexSpec()
		spec.AllowedTools = []string{"mcp__openv__*"}
		spec.MaxTurns = 5
		args, err := buildCodexArgs(spec)
		if err != nil {
			t.Fatalf("a codex agent carrying max_turns must still run: %v", err)
		}
		for _, a := range args {
			if strings.Contains(a, "turn") {
				t.Errorf("codex argv should carry no turn cap, got %q", a)
			}
		}
	})
	t.Run("repo access is refused", func(t *testing.T) {
		spec := codexSpec()
		spec.AllowedTools = []string{"mcp__openv__*", "Edit"}
		spec.RepoAccess = true
		_, err := buildCodexArgs(spec)
		if err == nil {
			t.Fatal("expected a refusal for a repo-access agent on codex")
		}
		if !errors.Is(err, ErrAgentPolicy) {
			t.Errorf("a repo-access refusal must be non-retryable agent policy: %v", err)
		}
		for _, want := range []string{"repository access", "codex-cli", "claude-code"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q should mention %q", err, want)
			}
		}
	})
	t.Run("allowed_tools are accepted, not refused", func(t *testing.T) {
		spec := codexSpec()
		spec.AllowedTools = []string{"Bash", "mcp__openv__get_artifact"}
		if _, err := buildCodexArgs(spec); err != nil {
			t.Fatalf("a codex agent with an allowlist must still run: %v", err)
		}
	})
	t.Run("no allowed_tools at all", func(t *testing.T) {
		_, err := buildCodexArgs(codexSpec())
		if err == nil {
			t.Fatal("expected error when the agent names no tools")
		}
		if !strings.Contains(err.Error(), "allowed_tools") {
			t.Errorf("refusal should name allowed_tools: %v", err)
		}
	})
}

// A codex run's OpenV tools are narrowed by the MCP server itself: Start adds
// OPENV_MCP_TOOLS to the MCP server's environment, and codex forwards it there
// by name along with the run token — so the allowlist is enforced even though
// codex exec has no allowlist flag.
func TestCodexStart_ForwardsOpenVToolFilterByName(t *testing.T) {
	spec := codexSpec()
	spec.AllowedTools = []string{"mcp__openv__get_artifact", "mcp__openv__list_artifacts", "Bash"}
	spec = withOpenVToolFilter(spec)

	if got := spec.MCP.Env[mcp.EnvToolAllowlist]; got != "get_artifact,list_artifacts" {
		t.Errorf("%s = %q, want the agent's OpenV tools", mcp.EnvToolAllowlist, got)
	}
	args, err := codexArgs(spec)
	if err != nil {
		t.Fatalf("codexArgs: %v", err)
	}
	want := `mcp_servers.openv.env_vars=["OPENV_API_URL","OPENV_MCP_TOOLS","OPENV_RUN_TOKEN"]`
	if !slices.Contains(args, want) {
		t.Errorf("env_vars override = %v, want it to carry %q", args, want)
	}
}

// An untrusted run gets codex's read-only sandbox: an interview transcript or
// a fetched page cannot turn into a file write (REQ-91, HAZ-1).
func TestCodexArgs_UntrustedSandbox(t *testing.T) {
	trusted, err := codexArgs(codexSpec())
	if err != nil {
		t.Fatalf("codexArgs: %v", err)
	}
	if !strings.Contains(strings.Join(trusted, " "), "--sandbox workspace-write") {
		t.Errorf("trusted run should use workspace-write: %v", trusted)
	}
	spec := codexSpec()
	spec.Untrusted = true
	untrusted, err := codexArgs(spec)
	if err != nil {
		t.Fatalf("codexArgs: %v", err)
	}
	joined := strings.Join(untrusted, " ")
	if !strings.Contains(joined, "--sandbox read-only") {
		t.Errorf("untrusted run should use the read-only sandbox: %v", untrusted)
	}
	if strings.Contains(joined, "danger-full-access") {
		t.Fatalf("danger-full-access must never be used: %v", untrusted)
	}
}

// The merged process env must carry the token so codex can forward it.
func TestMergedProcEnv_CarriesToken(t *testing.T) {
	spec := codexSpec()
	spec.Env = map[string]string{"OPENV_API_URL": "http://api.local"}
	env := mergedProcEnv(spec)
	if env["OPENV_RUN_TOKEN"] != "super-secret-token-123" {
		t.Errorf("merged env missing run token: %v", env)
	}
}
