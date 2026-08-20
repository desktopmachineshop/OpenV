package agents

import (
	"strings"
	"testing"
)

func TestParseFileRoundTrip(t *testing.T) {
	def := &Definition{
		Slug:         "test-agent",
		Name:         "Test Agent",
		Provider:     "claude-code",
		Model:        "claude-sonnet-5",
		AllowedTools: []string{"mcp__openv__*", "Read"},
		WriteMode:    WriteModeDirect,
		RepoAccess:   true,
		SystemPrompt: "You are a test agent.\n\nBe brief.",
	}
	content, err := SerializeFile(def)
	if err != nil {
		t.Fatalf("SerializeFile failed: %v", err)
	}
	parsed, err := ParseFile(content)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if parsed.Slug != def.Slug || parsed.Name != def.Name || parsed.Provider != def.Provider {
		t.Errorf("identity fields lost in round trip: %+v", parsed)
	}
	if parsed.WriteMode != WriteModeDirect || !parsed.RepoAccess {
		t.Errorf("mode/repo fields lost: %+v", parsed)
	}
	if len(parsed.AllowedTools) != 2 {
		t.Errorf("allowed_tools lost: %v", parsed.AllowedTools)
	}
	if parsed.SystemPrompt != def.SystemPrompt {
		t.Errorf("system prompt mismatch: %q != %q", parsed.SystemPrompt, def.SystemPrompt)
	}
}

func TestParseFileDefaults(t *testing.T) {
	content := "---\nslug: minimal\nname: Minimal\nprovider: gemini-cli\n---\nDo things.\n"
	def, err := ParseFile(content)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if def.WriteMode != WriteModeProposal {
		t.Errorf("expected default write_mode proposal, got %q", def.WriteMode)
	}
	if def.MaxTurns != 50 || def.TimeoutSeconds != 1800 {
		t.Errorf("expected defaults 50/1800, got %d/%d", def.MaxTurns, def.TimeoutSeconds)
	}
}

func TestParseFileErrors(t *testing.T) {
	cases := map[string]string{
		"no frontmatter":     "just a prompt",
		"unclosed":           "---\nslug: x\nname: y\nprovider: z\n",
		"missing slug":       "---\nname: y\nprovider: z\n---\nbody",
		"missing provider":   "---\nslug: x\nname: y\n---\nbody",
		"bad write mode":     "---\nslug: x\nname: y\nprovider: z\nwrite_mode: yolo\n---\nbody",
		"invalid yaml":       "---\nslug: [unterminated\n---\nbody",
	}
	for label, content := range cases {
		if _, err := ParseFile(content); err == nil {
			t.Errorf("%s: expected error, got none", label)
		}
	}
}

func TestParseFileWindowsLineEndings(t *testing.T) {
	content := strings.ReplaceAll("---\nslug: crlf\nname: CRLF\nprovider: codex-cli\n---\nprompt body\n", "\n", "\r\n")
	def, err := ParseFile(content)
	if err != nil {
		t.Fatalf("ParseFile with CRLF failed: %v", err)
	}
	if def.Slug != "crlf" || def.SystemPrompt != "prompt body" {
		t.Errorf("CRLF parse mismatch: %+v", def)
	}
}
