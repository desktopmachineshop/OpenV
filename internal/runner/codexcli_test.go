package runner

import (
	"strings"
	"testing"
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
	args, err := buildCodexArgs(spec)
	if err != nil {
		t.Fatalf("buildCodexArgs: %v", err)
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
	args, err := buildCodexArgs(codexSpec())
	if err != nil {
		t.Fatalf("buildCodexArgs: %v", err)
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
	args, err := buildCodexArgs(codexSpec())
	if err != nil {
		t.Fatalf("buildCodexArgs: %v", err)
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

// MaxTurns / AllowedTools are unsupportable on codex exec: setting either must
// fail the run at Start rather than silently running unconstrained.
func TestBuildCodexArgs_UnsupportedCapsError(t *testing.T) {
	t.Run("max_turns", func(t *testing.T) {
		spec := codexSpec()
		spec.MaxTurns = 5
		if _, err := buildCodexArgs(spec); err == nil {
			t.Fatal("expected error when MaxTurns is set")
		} else if !strings.Contains(err.Error(), "MaxTurns") {
			t.Errorf("error should name MaxTurns: %v", err)
		}
	})
	t.Run("allowed_tools", func(t *testing.T) {
		spec := codexSpec()
		spec.AllowedTools = []string{"Bash"}
		if _, err := buildCodexArgs(spec); err == nil {
			t.Fatal("expected error when AllowedTools is set")
		} else if !strings.Contains(err.Error(), "AllowedTools") {
			t.Errorf("error should name AllowedTools: %v", err)
		}
	})
	t.Run("clean_spec_ok", func(t *testing.T) {
		if _, err := buildCodexArgs(codexSpec()); err != nil {
			t.Fatalf("clean spec should build: %v", err)
		}
	})
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
