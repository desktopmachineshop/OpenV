package runner

import (
	"encoding/json"
	"errors"
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

// What headless gemini refuses, and what it merely cannot apply. An empty
// allowlist is refused (REQ-91) and so is repository access — gemini's
// approval mode is a whole-run switch, so a repo-editing agent either
// auto-approves every edit or waits on a confirmation nobody can answer. A
// MaxTurns is a logged no-op like Effort, not a refusal: Validate gives every
// persisted definition one, so refusing it refused every real gemini agent.
func TestBuildGeminiArgs_UnsupportedCapsError(t *testing.T) {
	t.Run("max_turns is a no-op, not a refusal", func(t *testing.T) {
		spec := geminiSpec()
		spec.MaxTurns = 3
		args, err := buildGeminiArgs(spec)
		if err != nil {
			t.Fatalf("a gemini agent carrying max_turns must still run: %v", err)
		}
		if slices.Contains(args, "--max-turns") {
			t.Errorf("gemini argv should carry no turn cap: %v", args)
		}
	})
	t.Run("repo access is refused", func(t *testing.T) {
		spec := geminiSpec()
		spec.RepoAccess = true
		_, err := buildGeminiArgs(spec)
		if err == nil {
			t.Fatal("expected a refusal for a repo-access agent on gemini")
		}
		if !errors.Is(err, ErrAgentPolicy) {
			t.Errorf("a repo-access refusal must be non-retryable agent policy: %v", err)
		}
		for _, want := range []string{"repository access", "gemini-cli", "claude-code"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("refusal %q should mention %q", err, want)
			}
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
	// The shell scope is gemini's, not Claude's: tools.core matches a literal
	// command PREFIX, so "git *" becomes "git". Left as the glob it would
	// match a command literally starting "git *" and scope the shell to
	// nothing.
	wantCore := []string{
		"edit", "google_web_search", "grep_search", "read_file", "replace",
		"run_shell_command(git)", "search_file_content",
	}
	if !slices.Equal(core, wantCore) {
		t.Errorf("tools.core = %v, want %v", core, wantCore)
	}

	// Shell scopes, in the shapes an allowlist actually carries.
	for _, tc := range []struct {
		entry string
		want  string
	}{
		// Claude Code's documented prefix form is the colon one; "git *" is
		// the older spelling the seeded agents were written in. Both mean
		// "any git command" and both have to reach the same gemini prefix —
		// read literally, "git:*" would have become run_shell_command(git:),
		// which matches nothing.
		{"Bash(git:*)", "run_shell_command(git)"},
		{"Bash(git *)", "run_shell_command(git)"},
		{"Bash(gh pr:*)", "run_shell_command(gh pr)"},
		{"Bash(npm test)", "run_shell_command(npm test)"},
		{"Bash(*)", "run_shell_command"},
		{"Bash", "run_shell_command"},
	} {
		got, _, _ := geminiToolSettings([]string{tc.entry})
		if !slices.Equal(got, []string{tc.want}) {
			t.Errorf("geminiToolSettings(%q) core = %v, want [%s]", tc.entry, got, tc.want)
		}
	}

	// The wildcard means "every OpenV tool", which gemini spells as an
	// omitted includeTools rather than an empty one.
	// Both wildcard spellings: the per-tool glob and Claude Code's
	// server-wide form, which names the MCP server on its own.
	for _, wildcard := range []string{"mcp__openv__*", "mcp__openv"} {
		gotCore, gotInclude, gotAll := geminiToolSettings([]string{wildcard})
		if !gotAll {
			t.Errorf("%q should ask for every tool from the openv server", wildcard)
		}
		if len(gotInclude) != 0 {
			t.Errorf("%q: includeTools = %v, want none (the key is omitted)", wildcard, gotInclude)
		}
		if len(gotCore) != 0 {
			t.Errorf("%q: tools.core = %v, want empty — it names no built-in tool", wildcard, gotCore)
		}
	}
	// An agent that names only OpenV tools gets no built-in tools — an empty
	// tools.core, never an omitted one.
	core, _, _ = geminiToolSettings([]string{"mcp__openv__*"})
	if len(core) != 0 {
		t.Errorf("tools.core = %v, want empty for an agent that names no built-in tools", core)
	}

	// Both prefix spellings of a scoped shell reach the same entry, so an
	// allowlist carrying both registers one tool, not two.
	core, _, _ = geminiToolSettings([]string{"Bash(git:*)", "Bash(git *)"})
	if !slices.Equal(core, []string{"run_shell_command(git)"}) {
		t.Errorf("tools.core = %v, want the one shell entry both spellings mean", core)
	}
}

// geminiToolSettings reads the mcp__openv grammar through openvToolNames — the
// same function OPENV_MCP_TOOLS is built from — rather than a second parser of
// its own. The two used to be separate and were free to drift; this pins them
// to the same answer, duplicates and all.
func TestGeminiIncludeToolsMatchesTheOpenVToolFilter(t *testing.T) {
	cases := [][]string{
		{"mcp__openv__get_artifact", "mcp__openv__get_artifact", "Read"},
		{"mcp__openv__list_artifacts", "mcp__openv__get_context", "Bash(git:*)"},
		{"mcp__openv__get_artifact", "mcp__openv"},
		{"mcp__openv__*", "mcp__openv__get_artifact"},
		{"Read", "Grep"},
	}
	for _, tools := range cases {
		include, includeAll := openvToolNames(tools)
		gotInclude, gotAll := func() ([]string, bool) {
			_, inc, all := geminiToolSettings(tools)
			return inc, all
		}()
		if gotAll != includeAll {
			t.Errorf("%v: includeAll = %v, want %v", tools, gotAll, includeAll)
		}
		want := append([]string(nil), include...)
		slices.Sort(want)
		if !slices.Equal(gotInclude, want) {
			t.Errorf("%v: includeTools = %v, want %v (the same tools OPENV_MCP_TOOLS gets)", tools, gotInclude, want)
		}
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
