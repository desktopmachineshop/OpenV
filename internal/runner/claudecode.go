package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/providers"
)

// ClaudeCodeAdapter runs Claude Code in headless (-p) mode.
type ClaudeCodeAdapter struct{}

// Name returns the provider name.
func (a *ClaudeCodeAdapter) Name() string { return providers.ProviderClaudeCode }

// Detect checks for the claude binary and a best-effort login signal.
func (a *ClaudeCodeAdapter) Detect(ctx context.Context) Availability {
	if _, err := exec.LookPath("claude"); err != nil {
		return Availability{Detail: "claude binary not found on PATH"}
	}
	version, err := runVersion(ctx, "claude", "--version")
	if err != nil {
		return Availability{Detail: "claude --version failed: " + err.Error()}
	}
	av := Availability{Installed: true, Version: version}
	// Token mode (hosted runners): an API key in the environment makes the
	// CLI fully usable without any interactive sign-in.
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		av.LoggedIn = true
		av.Detail = "API key mode (ANTHROPIC_API_KEY)"
		return av
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
			av.LoggedIn = true
			av.Detail = "~/.claude present; assuming logged in"
		} else if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
			av.LoggedIn = true
			av.Detail = "~/.claude.json present; assuming logged in"
		} else {
			av.Detail = "no ~/.claude config found; run `claude` once to log in"
		}
	}
	return av
}

// writeMCPConfig writes the standard mcpServers JSON config file. The file
// carries the run token in its env block, so it is written 0600 (dir 0700):
// only the same user's CLI child needs to read it.
func writeMCPConfig(path string, mcp MCPServerConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	env := mcp.Env
	if env == nil {
		env = map[string]string{}
	}
	args := mcp.Args
	if args == nil {
		args = []string{}
	}
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"openv": map[string]interface{}{
				"command": mcp.Command,
				"args":    args,
				"env":     env,
			},
		},
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o600)
}

// Start launches a headless Claude Code run.
func (a *ClaudeCodeAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	mcpPath := filepath.Join(spec.WorkDir, ".openv", "mcp.json")
	if err := writeMCPConfig(mcpPath, spec.MCP); err != nil {
		return nil, fmt.Errorf("write mcp config: %w", err)
	}

	// The prompt travels over stdin (`... | claude -p`), never argv: prompts
	// carry transcripts and wizard state, and Windows caps a command line at
	// ~32K characters.
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--mcp-config", mcpPath,
	}
	if spec.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.SystemPrompt)
	}
	if spec.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(spec.MaxTurns))
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.Effort != "" {
		args = append(args, "--effort", spec.Effort)
	}
	if len(spec.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(spec.AllowedTools, ","))
	}

	return startProc(ctx, procConfig{
		Command:    "claude",
		Args:       args,
		Dir:        spec.WorkDir,
		Env:        spec.Env,
		Stdin:      spec.Prompt,
		TimeoutSec: spec.TimeoutSec,
	}, &claudeParser{})
}

// claudeParser parses Claude Code's stream-json output.
type claudeParser struct {
	mu        sync.Mutex
	finalText string
	tokensIn  int64
	tokensOut int64
	costUSD   *float64
	isError   bool
	errorText string
}

func (p *claudeParser) ParseLine(line string, emit func(RunEvent)) {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": line}})
		return
	}
	switch msg["type"] {
	case "assistant":
		message, _ := msg["message"].(map[string]interface{})
		content, _ := message["content"].([]interface{})
		for _, blockAny := range content {
			block, _ := blockAny.(map[string]interface{})
			switch block["type"] {
			case "text":
				emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{
					"text": block["text"],
				}})
			case "tool_use":
				emit(RunEvent{Kind: agentruns.LogToolCall, Payload: map[string]interface{}{
					"name":  block["name"],
					"input": block["input"],
					"id":    block["id"],
				}})
			}
		}
	case "result":
		p.mu.Lock()
		if text, ok := msg["result"].(string); ok {
			p.finalText = text
		}
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			p.tokensIn = asInt64(usage["input_tokens"])
			p.tokensOut = asInt64(usage["output_tokens"])
		}
		if cost, ok := msg["total_cost_usd"].(float64); ok {
			c := cost
			p.costUSD = &c
		}
		if isErr, ok := msg["is_error"].(bool); ok && isErr {
			p.isError = true
			p.errorText = p.finalText
		}
		p.mu.Unlock()
		emit(RunEvent{Kind: agentruns.LogUsage, Payload: map[string]interface{}{
			"input_tokens":   p.tokensIn,
			"output_tokens":  p.tokensOut,
			"total_cost_usd": msg["total_cost_usd"],
		}})
	case "system":
		emit(RunEvent{Kind: agentruns.LogSystem, Payload: msg})
	default:
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": line}})
	}
}

func (p *claudeParser) Result(exitCode int, stderrTail string) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := Result{
		ExitCode:  exitCode,
		FinalText: p.finalText,
		TokensIn:  p.tokensIn,
		TokensOut: p.tokensOut,
		CostUSD:   p.costUSD,
	}
	if p.isError {
		detail := p.errorText
		if detail == "" {
			detail = "claude reported an error result"
		}
		return res, errors.New(detail)
	}
	if exitCode != 0 {
		detail := strings.TrimSpace(stderrTail)
		if detail == "" {
			detail = "claude exited with code " + strconv.Itoa(exitCode)
		}
		return res, errors.New(detail)
	}
	return res, nil
}

func asInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
