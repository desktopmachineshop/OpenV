package runner

import (
	"slices"
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

// REQ-91 / HAZ-1: no claude-code run auto-approves anything, trusted or not.
// The allowlist is the whole approval surface, so acceptEdits — which would
// let a run write files nobody put on that list — is passed by no path, and
// neither is any outright bypass.
func TestBuildClaudeArgs_NoRunAutoApproves(t *testing.T) {
	for _, untrusted := range []bool{false, true} {
		spec := claudeSpec()
		spec.Untrusted = untrusted
		args, err := buildClaudeArgs(spec, "/work/.openv/mcp.json")
		if err != nil {
			t.Fatalf("buildClaudeArgs: %v", err)
		}
		if got := flagValue(args, "--permission-mode"); got != "default" {
			t.Errorf("untrusted=%v: --permission-mode = %q, want default", untrusted, got)
		}
		joined := strings.Join(args, " ")
		for _, forbidden := range []string{"acceptEdits", "bypassPermissions", "dangerously-skip-permissions"} {
			if strings.Contains(joined, forbidden) {
				t.Errorf("untrusted=%v: run must not carry %q: %v", untrusted, forbidden, args)
			}
		}
	}
}

// Trust changes nothing about claude's argv: the same flags, both ways. What
// trust decides is how far the content a run reads is believed — which is a
// question for the other CLIs' sandboxes, not for this one's permissions.
func TestBuildClaudeArgs_TrustDoesNotChangeArgv(t *testing.T) {
	trusted, err := buildClaudeArgs(claudeSpec(), "/work/.openv/mcp.json")
	if err != nil {
		t.Fatalf("buildClaudeArgs: %v", err)
	}
	spec := claudeSpec()
	spec.Untrusted = true
	untrusted, err := buildClaudeArgs(spec, "/work/.openv/mcp.json")
	if err != nil {
		t.Fatalf("buildClaudeArgs: %v", err)
	}
	if !slices.Equal(trusted, untrusted) {
		t.Errorf("argv differs by trust:\n trusted = %v\n untrusted = %v", trusted, untrusted)
	}
}

// Every run states its permission mode; none is left to whatever the CLI, or
// a settings file on the runner host, happens to default to.
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
