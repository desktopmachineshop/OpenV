package runner

import (
	"strings"
	"testing"
)

func claudeSpec() RunSpec {
	return RunSpec{
		WorkDir:      "/work",
		Prompt:       "do the thing",
		Model:        "claude-opus-4",
		AllowedTools: []string{"mcp__openv__get_artifact", "mcp__openv__record_candidate_need"},
		MCP: MCPServerConfig{
			Command: "openv-mcp",
			Env:     map[string]string{"OPENV_RUN_TOKEN": "super-secret-token-123"},
		},
	}
}

// flagValue returns the argument following name, or "" if the flag is absent.
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// REQ-91: an agent that names no tools does not run. The CLI would otherwise
// start with every tool it has, which is the opposite of an allowlist.
func TestBuildClaudeArgs_RefusesEmptyAllowlist(t *testing.T) {
	for _, tools := range [][]string{nil, {}, {""}, {"  "}} {
		spec := claudeSpec()
		spec.AllowedTools = tools
		_, err := buildClaudeArgs(spec, "/work/.openv/mcp.json")
		if err == nil {
			t.Fatalf("allowed_tools %q was accepted; it must fail before launch", tools)
		}
		if !strings.Contains(err.Error(), "allowed_tools") {
			t.Errorf("refusal should name allowed_tools: %v", err)
		}
	}
}

// The allowlist reaches the CLI in its own terms: --allowedTools, comma-joined.
func TestBuildClaudeArgs_PassesAllowlist(t *testing.T) {
	args, err := buildClaudeArgs(claudeSpec(), "/work/.openv/mcp.json")
	if err != nil {
		t.Fatalf("buildClaudeArgs: %v", err)
	}
	got := flagValue(args, "--allowedTools")
	want := "mcp__openv__get_artifact,mcp__openv__record_candidate_need"
	if got != want {
		t.Errorf("--allowedTools = %q, want %q", got, want)
	}
}

// REQ-91 / HAZ-1: a run carrying content from outside the workspace approves
// nothing on its own. Whatever the mode is called, it is never one that skips
// permissions.
func TestBuildClaudeArgs_UntrustedNeverAutoApproves(t *testing.T) {
	spec := claudeSpec()
	spec.Untrusted = true
	args, err := buildClaudeArgs(spec, "/work/.openv/mcp.json")
	if err != nil {
		t.Fatalf("buildClaudeArgs: %v", err)
	}
	if got := flagValue(args, "--permission-mode"); got != "default" {
		t.Errorf("untrusted --permission-mode = %q, want default", got)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"bypassPermissions", "acceptEdits", "dangerously-skip-permissions"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("untrusted run must not carry %q: %v", forbidden, args)
		}
	}
}

// A trusted run may auto-approve edits in its own workspace — but never
// bypasses permissions altogether, for any run.
func TestBuildClaudeArgs_TrustedModeAndNeverBypass(t *testing.T) {
	args, err := buildClaudeArgs(claudeSpec(), "/work/.openv/mcp.json")
	if err != nil {
		t.Fatalf("buildClaudeArgs: %v", err)
	}
	if got := flagValue(args, "--permission-mode"); got != "acceptEdits" {
		t.Errorf("trusted --permission-mode = %q, want acceptEdits", got)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"bypassPermissions", "dangerously-skip-permissions"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("no run may carry %q: %v", forbidden, args)
		}
	}
}

// Every run states its permission mode; none is left to the CLI's default.
func TestBuildClaudeArgs_AlwaysStatesPermissionMode(t *testing.T) {
	for _, untrusted := range []bool{false, true} {
		spec := claudeSpec()
		spec.Untrusted = untrusted
		args, err := buildClaudeArgs(spec, "/work/.openv/mcp.json")
		if err != nil {
			t.Fatalf("buildClaudeArgs: %v", err)
		}
		if flagValue(args, "--permission-mode") == "" {
			t.Errorf("untrusted=%v: no --permission-mode in %v", untrusted, args)
		}
	}
}

// The prompt and the run token stay out of argv (stdin and an 0600 file
// respectively) — unchanged by the permission work, and worth keeping honest.
func TestBuildClaudeArgs_NoSecretsInArgv(t *testing.T) {
	spec := claudeSpec()
	args, err := buildClaudeArgs(spec, "/work/.openv/mcp.json")
	if err != nil {
		t.Fatalf("buildClaudeArgs: %v", err)
	}
	joined := strings.Join(args, "\x00")
	if strings.Contains(joined, "super-secret-token-123") {
		t.Fatalf("run token leaked into argv: %v", args)
	}
	if strings.Contains(joined, spec.Prompt) {
		t.Fatalf("prompt leaked into argv: %v", args)
	}
}
