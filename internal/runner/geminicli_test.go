package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func geminiSpec() RunSpec {
	return RunSpec{
		WorkDir: "/work",
		Prompt:  "do the thing",
		Model:   "gemini-2.5-pro",
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

// yolo (auto-approve everything, unsandboxed) must be gone; the least-privilege
// auto_edit mode replaces it.
func TestBuildGeminiArgs_NoYolo(t *testing.T) {
	args, err := buildGeminiArgs(geminiSpec())
	if err != nil {
		t.Fatalf("buildGeminiArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "yolo") {
		t.Fatalf("yolo mode still present: %v", args)
	}
	if !strings.Contains(joined, "--approval-mode auto_edit") {
		t.Errorf("expected --approval-mode auto_edit: %v", args)
	}
}

func TestBuildGeminiArgs_UnsupportedCapsError(t *testing.T) {
	t.Run("max_turns", func(t *testing.T) {
		spec := geminiSpec()
		spec.MaxTurns = 3
		if _, err := buildGeminiArgs(spec); err == nil {
			t.Fatal("expected error when MaxTurns is set")
		} else if !strings.Contains(err.Error(), "MaxTurns") {
			t.Errorf("error should name MaxTurns: %v", err)
		}
	})
	t.Run("allowed_tools", func(t *testing.T) {
		spec := geminiSpec()
		spec.AllowedTools = []string{"Read"}
		if _, err := buildGeminiArgs(spec); err == nil {
			t.Fatal("expected error when AllowedTools is set")
		} else if !strings.Contains(err.Error(), "AllowedTools") {
			t.Errorf("error should name AllowedTools: %v", err)
		}
	})
}

// The isolated settings file must trust the openv server, pin auto_edit, keep
// the token out (as a ${VAR} reference), and be written 0600.
func TestWriteGeminiSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".openv", "gemini-settings.json")
	if err := writeGeminiSettings(path, geminiSpec().MCP); err != nil {
		t.Fatalf("writeGeminiSettings: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(raw), "super-secret-token-123") {
		t.Fatalf("run token written into settings file: %s", raw)
	}

	var cfg struct {
		General struct {
			DefaultApprovalMode string `json:"defaultApprovalMode"`
		} `json:"general"`
		McpServers map[string]struct {
			Command string            `json:"command"`
			Trust   bool              `json:"trust"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	if cfg.General.DefaultApprovalMode != "auto_edit" {
		t.Errorf("defaultApprovalMode = %q, want auto_edit", cfg.General.DefaultApprovalMode)
	}
	openv, ok := cfg.McpServers["openv"]
	if !ok {
		t.Fatal("openv server missing from settings")
	}
	if !openv.Trust {
		t.Error("openv server should be trusted")
	}
	if openv.Env["OPENV_RUN_TOKEN"] != "${OPENV_RUN_TOKEN}" {
		t.Errorf("token env should be a ${VAR} reference, got %q", openv.Env["OPENV_RUN_TOKEN"])
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("settings file perm = %o, want 600", perm)
		}
	}
}

// A non-zero exit with a usage banner must fail the run — never become the
// agent's answer.
func TestGeminiParser_UsageErrorNotAnswer(t *testing.T) {
	p := &geminiParser{}
	p.ParseLine("error: unknown option '--bogus'", func(RunEvent) {})
	res, err := p.Result(1, "error: unknown option '--bogus'")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if res.FinalText != "" {
		t.Errorf("usage banner leaked into FinalText: %q", res.FinalText)
	}
}

// Non-JSON stdout on a zero exit is a CLI failure, not an answer.
func TestGeminiParser_NonJSONNotAnswer(t *testing.T) {
	p := &geminiParser{}
	p.ParseLine("not json at all", func(RunEvent) {})
	res, err := p.Result(0, "")
	if err == nil {
		t.Fatal("expected error for non-JSON output on exit 0")
	}
	if res.FinalText != "" {
		t.Errorf("raw output leaked into FinalText: %q", res.FinalText)
	}
}

// A well-formed JSON result yields the response as the answer.
func TestGeminiParser_ValidResponse(t *testing.T) {
	p := &geminiParser{}
	p.ParseLine(`{"response":"the answer","stats":{}}`, func(RunEvent) {})
	res, err := p.Result(0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.FinalText != "the answer" {
		t.Errorf("FinalText = %q, want %q", res.FinalText, "the answer")
	}
}
