package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func geminiSpec() RunSpec {
	return RunSpec{
		WorkDir:      "/work",
		Prompt:       "do the thing",
		Model:        "gemini-2.5-pro",
		AllowedTools: []string{"mcp__openv__get_artifact", "Read", "Edit", "Bash(git *)"},
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

// yolo (auto-approve everything, unsandboxed) must never be the mode, and an
// untrusted run drops even auto_edit: it approves nothing beyond its
// allowlist (REQ-91).
func TestGeminiApprovalMode(t *testing.T) {
	if got := geminiApprovalMode(geminiSpec()); got != "auto_edit" {
		t.Errorf("trusted approval mode = %q, want auto_edit", got)
	}
	untrusted := geminiSpec()
	untrusted.Untrusted = true
	if got := geminiApprovalMode(untrusted); got != "default" {
		t.Errorf("untrusted approval mode = %q, want default", got)
	}
	for _, spec := range []RunSpec{geminiSpec(), untrusted} {
		if strings.Contains(geminiApprovalMode(spec), "yolo") {
			t.Fatal("yolo mode must never be used")
		}
	}
}

// MaxTurns has no headless equivalent, so a run that asks for one is refused
// rather than run without it. A tool allowlist, by contrast, is translated
// into gemini's own settings and no longer stops the run — but an empty one is
// still refused, because that is the case where the CLI would run with every
// tool it has (REQ-91).
func TestBuildGeminiArgs_UnsupportedCapsError(t *testing.T) {
	t.Run("max_turns", func(t *testing.T) {
		spec := geminiSpec()
		spec.MaxTurns = 3
		if _, err := buildGeminiArgs(spec); err == nil {
			t.Fatal("expected error when MaxTurns is set")
		}
	})
	t.Run("allowed_tools are accepted, not refused", func(t *testing.T) {
		spec := geminiSpec()
		spec.AllowedTools = []string{"Read"}
		if _, err := buildGeminiArgs(spec); err != nil {
			t.Fatalf("a gemini agent with an allowlist must still run: %v", err)
		}
	})
	t.Run("no allowed_tools at all", func(t *testing.T) {
		spec := geminiSpec()
		spec.AllowedTools = nil
		_, err := buildGeminiArgs(spec)
		if err == nil {
			t.Fatal("expected error when the agent names no tools")
		}
		if !strings.Contains(err.Error(), "allowed_tools") {
			t.Errorf("refusal should name allowed_tools: %v", err)
		}
	})
}

// --allowed-tools / tools.allowed are gemini's AUTO-APPROVAL list, not a
// restriction: passing either would widen the run. The allowlist belongs in
// tools.core and includeTools instead, and --yolo is never passed.
func TestBuildGeminiArgs_NeverPassesApprovalWideningFlags(t *testing.T) {
	for _, untrusted := range []bool{false, true} {
		spec := geminiSpec()
		spec.Untrusted = untrusted
		args, err := buildGeminiArgs(spec)
		if err != nil {
			t.Fatalf("buildGeminiArgs: %v", err)
		}
		joined := strings.Join(args, " ")
		for _, forbidden := range []string{"--allowed-tools", "--yolo"} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("untrusted=%v: argv must not carry %q: %v", untrusted, forbidden, args)
			}
		}
	}
}

// The Claude-shaped allowlist an agent definition carries is translated into
// the two settings gemini documents for restricting tools. Built-ins that
// gemini renamed across releases get both spellings; a Claude tool gemini has
// no equivalent for is dropped rather than approximated.
func TestGeminiToolSettings(t *testing.T) {
	core, include, includeAll := geminiToolSettings([]string{
		"mcp__openv__get_artifact",
		"mcp__openv__create_artifact",
		"Read", "Edit", "Grep", "WebSearch", "Bash(git *)",
		"NotebookEdit", // no gemini equivalent
	})
	if includeAll {
		t.Error("includeAll = true without the mcp__openv__* wildcard")
	}
	wantInclude := []string{"create_artifact", "get_artifact"}
	if !slices.Equal(include, wantInclude) {
		t.Errorf("includeTools = %v, want %v (bare names, openv is the only server)", include, wantInclude)
	}
	wantCore := []string{
		"edit", "google_web_search", "grep_search", "read_file", "replace",
		"run_shell_command(git *)", "search_file_content",
	}
	if !slices.Equal(core, wantCore) {
		t.Errorf("tools.core = %v, want %v", core, wantCore)
	}

	// The wildcard means "every OpenV tool", which gemini spells as an
	// omitted includeTools rather than an empty one.
	_, _, includeAll = geminiToolSettings([]string{"mcp__openv__*"})
	if !includeAll {
		t.Error("mcp__openv__* should ask for every tool from the openv server")
	}
	// An agent that names only OpenV tools gets no built-in tools — an empty
	// tools.core, never an omitted one.
	core, _, _ = geminiToolSettings([]string{"mcp__openv__*"})
	if len(core) != 0 {
		t.Errorf("tools.core = %v, want empty for an agent that names no built-in tools", core)
	}
}

// The isolated settings file must trust the openv server, pin auto_edit,
// carry the translated allowlist, keep the token out (as a ${VAR} reference),
// and be written 0600.
func TestWriteGeminiSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".openv", "gemini-settings.json")
	spec := geminiSpec()
	if err := writeGeminiSettings(path, spec.MCP, geminiApprovalMode(spec), spec.AllowedTools); err != nil {
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
		Tools struct {
			Core    []string `json:"core"`
			Allowed []string `json:"allowed"`
		} `json:"tools"`
		McpServers map[string]struct {
			Command      string            `json:"command"`
			Trust        bool              `json:"trust"`
			Env          map[string]string `json:"env"`
			IncludeTools []string          `json:"includeTools"`
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

	// The allowlist reaches gemini in gemini's own terms.
	if !slices.Equal(openv.IncludeTools, []string{"get_artifact"}) {
		t.Errorf("openv includeTools = %v, want [get_artifact]", openv.IncludeTools)
	}
	if !slices.Contains(cfg.Tools.Core, "read_file") || !slices.Contains(cfg.Tools.Core, "replace") {
		t.Errorf("tools.core = %v, want the translated built-ins", cfg.Tools.Core)
	}
	if slices.Contains(cfg.Tools.Core, "write_file") {
		t.Errorf("tools.core = %v, want nothing the agent did not name", cfg.Tools.Core)
	}
	// tools.allowed is an auto-approval list; writing it would widen the run.
	if len(cfg.Tools.Allowed) != 0 {
		t.Errorf("tools.allowed = %v, want nothing (it auto-approves, it does not restrict)", cfg.Tools.Allowed)
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
