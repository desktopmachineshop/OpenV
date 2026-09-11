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

// codex's sandbox is picked from BOTH halves of the question, because codex
// has no per-tool allowlist and so grants the sandbox to the whole run:
// workspace-write needs a trusted run AND an allowlist that names something a
// writable workspace would serve. An untrusted run (interview transcript,
// fetched page, cloned repo) never writes (REQ-91, HAZ-1); neither does an
// OpenV-only agent, which was never granted a file tool or a shell in the
// first place.
func TestCodexArgs_SandboxFromTrustAndAllowlist(t *testing.T) {
	sandboxOf := func(t *testing.T, untrusted bool, tools ...string) string {
		t.Helper()
		spec := codexSpec()
		spec.Untrusted = untrusted
		spec.AllowedTools = tools
		args, err := codexArgs(spec)
		if err != nil {
			t.Fatalf("codexArgs: %v", err)
		}
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "danger-full-access") {
			t.Fatalf("danger-full-access must never be used: %v", args)
		}
		for _, mode := range []string{"workspace-write", "read-only"} {
			if strings.Contains(joined, "--sandbox "+mode) {
				return mode
			}
		}
		t.Fatalf("no --sandbox in argv: %v", args)
		return ""
	}

	cases := []struct {
		name      string
		untrusted bool
		tools     []string
		want      string
	}{
		{"an editing agent on a trusted run", false, []string{"mcp__openv__*", "Read", "Edit", "Write"}, "workspace-write"},
		{"the same agent on an untrusted run", true, []string{"mcp__openv__*", "Read", "Edit", "Write"}, "read-only"},
		{"OpenV tools only", false, []string{"mcp__openv__*"}, "read-only"},
		{"the server-wide OpenV spelling", false, []string{"mcp__openv"}, "read-only"},
		{"OpenV tools and read-only vendor tools", false, []string{"mcp__openv__*", "Read", "Grep", "Glob"}, "read-only"},
		// Every Bash spelling is a shell, so every one of them earns the
		// writable sandbox on a trusted run — a scoped shell is still a shell.
		{"a scoped shell, colon form", false, []string{"mcp__openv__*", "Bash(git:*)"}, "workspace-write"},
		{"a scoped shell, glob form", false, []string{"mcp__openv__*", "Bash(git *)"}, "workspace-write"},
		{"a literal command scope", false, []string{"mcp__openv__*", "Bash(npm test)"}, "workspace-write"},
		{"an unscoped shell", false, []string{"mcp__openv__*", "Bash"}, "workspace-write"},
		{"the other file writers", false, []string{"mcp__openv__*", "MultiEdit"}, "workspace-write"},
		{"notebook edits", false, []string{"mcp__openv__*", "NotebookEdit"}, "workspace-write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sandboxOf(t, tc.untrusted, tc.tools...); got != tc.want {
				t.Errorf("--sandbox %s, want %s (untrusted=%v, tools=%v)", got, tc.want, tc.untrusted, tc.tools)
			}
		})
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
