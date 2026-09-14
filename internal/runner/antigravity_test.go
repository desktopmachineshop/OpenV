package runner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

func antigravitySpec() RunSpec {
	return RunSpec{
		WorkDir:      t0Dir,
		Prompt:       "do the thing",
		Model:        "gemini-3.5-flash-medium",
		AllowedTools: []string{"mcp__openv__get_artifact", "Read"},
		MCP: MCPServerConfig{
			Command: "openv-mcp",
			Args:    []string{"mcp", "--stdio"},
			Env: map[string]string{
				"OPENV_RUN_TOKEN": "super-secret-token-123",
				"OPENV_API_URL":   "http://api.local",
			},
		},
	}
}

const t0Dir = "/work"

// The auto-approve-everything flag is this CLI's --yolo: it approves file
// writes and shell commands alike, which is exactly what an allowlist is for
// (REQ-91, HAZ-1).
func TestBuildAntigravityArgs_NeverSkipsPermissions(t *testing.T) {
	for _, untrusted := range []bool{false, true} {
		spec := antigravitySpec()
		spec.Untrusted = untrusted
		args, err := buildAntigravityArgs(spec)
		if err != nil {
			t.Fatalf("buildAntigravityArgs: %v", err)
		}
		if slices.Contains(args, "--dangerously-skip-permissions") {
			t.Errorf("untrusted=%v: argv must never skip permissions: %v", untrusted, args)
		}
	}
}

// The documented headless shape: a prompt, and JSON rather than prose, so a
// usage banner can never be mistaken for the agent's answer.
func TestBuildAntigravityArgs_HeadlessShape(t *testing.T) {
	args, err := buildAntigravityArgs(antigravitySpec())
	if err != nil {
		t.Fatalf("buildAntigravityArgs: %v", err)
	}
	if i := slices.Index(args, "-p"); i < 0 || i+1 >= len(args) || args[i+1] == "" {
		t.Errorf("argv carries no prompt: %v", args)
	}
	if i := slices.Index(args, "--output-format"); i < 0 || args[i+1] != "json" {
		t.Errorf("argv does not ask for json: %v", args)
	}
	if i := slices.Index(args, "--model"); i < 0 || args[i+1] != "gemini-3.5-flash-medium" {
		t.Errorf("argv does not carry the model: %v", args)
	}
}

// Effort has three rungs here and five in OpenV; the extra ones cap rather
// than fail, and an unset effort adds no flag at all.
func TestAntigravityEffort(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "low": "low", "medium": "medium",
		"high": "high", "xhigh": "high", "max": "high",
		"nonsense": "",
	} {
		if got := antigravityEffort(in); got != want {
			t.Errorf("antigravityEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

// A system prompt has no flag of its own, so it is prefixed — and must still
// reach the CLI rather than being dropped.
func TestBuildAntigravityArgs_CarriesTheSystemPrompt(t *testing.T) {
	spec := antigravitySpec()
	spec.SystemPrompt = "be terse"
	args, err := buildAntigravityArgs(spec)
	if err != nil {
		t.Fatalf("buildAntigravityArgs: %v", err)
	}
	prompt := args[slices.Index(args, "-p")+1]
	if !strings.Contains(prompt, "be terse") || !strings.Contains(prompt, "do the thing") {
		t.Errorf("prompt lost the system instructions or the task: %q", prompt)
	}
}

// What this adapter refuses, and why each refusal is a policy error the
// worker must not retry.
func TestAntigravityUnsupported(t *testing.T) {
	t.Run("no allowlist", func(t *testing.T) {
		spec := antigravitySpec()
		spec.AllowedTools = nil
		_, err := buildAntigravityArgs(spec)
		if !errors.Is(err, ErrAgentPolicy) {
			t.Fatalf("err = %v, want an agent-policy refusal", err)
		}
		if !strings.Contains(err.Error(), "allowed_tools") {
			t.Errorf("refusal should name allowed_tools: %v", err)
		}
	})
	t.Run("repo access", func(t *testing.T) {
		spec := antigravitySpec()
		spec.RepoAccess = true
		_, err := buildAntigravityArgs(spec)
		if !errors.Is(err, ErrAgentPolicy) {
			t.Fatalf("err = %v, want an agent-policy refusal", err)
		}
		// The refusal has to point somewhere, not just say no.
		if !strings.Contains(err.Error(), "claude-code") {
			t.Errorf("refusal should name the provider that can do this: %v", err)
		}
	})
}

// The run token must not reach disk: the workspace can be a repository clone,
// and the CLI's MCP child inherits the process environment anyway.
func TestWriteAntigravityMCP_KeepsTheTokenOffDisk(t *testing.T) {
	dir := t.TempDir()
	spec := antigravitySpec()
	if err := writeAntigravityMCP(dir, spec.MCP); err != nil {
		t.Fatalf("writeAntigravityMCP: %v", err)
	}
	path := filepath.Join(dir, antigravityWorkspaceMCP)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "super-secret-token-123") {
		t.Errorf("the run token was written to the workspace: %s", raw)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("config is not valid json: %v", err)
	}
	openv, ok := cfg.MCPServers["openv"]
	if !ok {
		t.Fatalf("config does not wire the openv server: %s", raw)
	}
	if openv.Command != "openv-mcp" {
		t.Errorf("command = %q, want openv-mcp", openv.Command)
	}
	if len(openv.Env) != 0 {
		t.Errorf("config carries an env block; the child inherits it instead: %v", openv.Env)
	}

	// A workspace file, not the member's global settings under HOME.
	if !strings.HasPrefix(antigravityWorkspaceMCP, ".agents/") {
		t.Errorf("MCP config should be workspace-scoped, got %q", antigravityWorkspaceMCP)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %v, want 0600", perm)
	}
}

// The envelope's field names are not published, so the parser tries several
// and fails loudly rather than passing unparsed output off as an answer.
func TestAntigravityParserResult(t *testing.T) {
	t.Run("reads the answer", func(t *testing.T) {
		for _, key := range antigravityTextKeys {
			p := &antigravityParser{}
			p.ParseLine(`{"`+key+`":"the answer"}`, func(RunEvent) {})
			res, err := p.Result(0, "")
			if err != nil {
				t.Fatalf("%s: %v", key, err)
			}
			if res.FinalText != "the answer" {
				t.Errorf("%s: FinalText = %q", key, res.FinalText)
			}
		}
	})
	t.Run("an unrecognised envelope is a failure, not an answer", func(t *testing.T) {
		p := &antigravityParser{}
		p.ParseLine(`{"something":"else"}`, func(RunEvent) {})
		if _, err := p.Result(0, ""); err == nil {
			t.Fatal("expected a failure for an envelope with no answer field")
		}
	})
	t.Run("a non-zero exit is a failure whatever it printed", func(t *testing.T) {
		p := &antigravityParser{}
		p.ParseLine(`{"response":"ignore me"}`, func(RunEvent) {})
		res, err := p.Result(3, "boom")
		if err == nil {
			t.Fatal("expected an error on a non-zero exit")
		}
		if res.FinalText != "" {
			t.Errorf("a failed run must not report an answer: %q", res.FinalText)
		}
	})
	t.Run("an error envelope surfaces its message", func(t *testing.T) {
		p := &antigravityParser{}
		p.ParseLine(`{"error":{"message":"model unavailable"}}`, func(RunEvent) {})
		_, err := p.Result(0, "")
		if err == nil || !strings.Contains(err.Error(), "model unavailable") {
			t.Fatalf("err = %v, want the envelope's message", err)
		}
	})
	t.Run("token counts are read where present", func(t *testing.T) {
		p := &antigravityParser{}
		p.ParseLine(`{"response":"hi","usage":{"input_tokens":11,"output_tokens":22}}`, func(RunEvent) {})
		res, err := p.Result(0, "")
		if err != nil {
			t.Fatalf("Result: %v", err)
		}
		if res.TokensIn != 11 || res.TokensOut != 22 {
			t.Errorf("tokens = %d/%d, want 11/22", res.TokensIn, res.TokensOut)
		}
	})
}

// An authentication failure is the expected one on a runner with no key, so
// it carries the explanation rather than a bare CLI message.
func TestAntigravityFailureExplainsAuth(t *testing.T) {
	got := antigravityFailure("Error: authentication required")
	if !strings.Contains(got, "keyring") || !strings.Contains(got, "API key") {
		t.Errorf("an auth failure should explain the keyring and the key: %q", got)
	}
	if other := antigravityFailure("model unavailable"); other != "model unavailable" {
		t.Errorf("an unrelated failure was annotated: %q", other)
	}
}

// The provider is registered end to end, or nothing can route a run to it.
func TestAntigravityIsRegistered(t *testing.T) {
	var found Adapter
	for _, a := range Registry() {
		if a.Name() == providers.ProviderAntigravityCLI {
			found = a
		}
	}
	if found == nil {
		t.Fatal("antigravity-cli is not in the adapter registry")
	}
	if !slices.Contains(providers.KnownProviders(), providers.ProviderAntigravityCLI) {
		t.Error("antigravity-cli is not a known provider")
	}
	// Its key is Google's, and it has to be one the catalogue already allows
	// or the runner will refuse to inject it.
	env := providers.DefaultAPIKeyEnv(providers.ProviderAntigravityCLI)
	if env != antigravityKeyEnv {
		t.Errorf("default key env = %q, want %q", env, antigravityKeyEnv)
	}
	if !providers.IsAllowedAPIKeyEnv(env) {
		t.Errorf("%q is not in the allowed key-env catalogue", env)
	}
}
