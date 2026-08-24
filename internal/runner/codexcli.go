package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/providers"
)

// CodexCLIAdapter runs OpenAI Codex CLI in non-interactive exec mode.
type CodexCLIAdapter struct{}

// Name returns the provider name.
func (a *CodexCLIAdapter) Name() string { return providers.ProviderCodexCLI }

// Detect checks for the codex binary.
func (a *CodexCLIAdapter) Detect(ctx context.Context) Availability {
	if _, err := exec.LookPath("codex"); err != nil {
		return Availability{Detail: "codex binary not found on PATH"}
	}
	version, err := runVersion(ctx, "codex", "--version")
	if err != nil {
		return Availability{Detail: "codex --version failed: " + err.Error()}
	}
	return Availability{
		Installed: true,
		Version:   version,
		LoggedIn:  true,
		Detail:    "version check succeeded; login not verified",
	}
}

// Start launches a codex exec run.
func (a *CodexCLIAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	args := []string{
		"exec",
		"--json",
		"--cd", spec.WorkDir,
		"-c", "mcp_servers.openv.command=" + spec.MCP.Command,
	}
	if len(spec.MCP.Args) > 0 {
		argsJSON, err := json.Marshal(spec.MCP.Args)
		if err != nil {
			return nil, err
		}
		args = append(args, "-c", "mcp_servers.openv.args="+string(argsJSON))
	}
	for k, v := range spec.MCP.Env {
		args = append(args, "-c", "mcp_servers.openv.env."+k+"="+v)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	args = append(args, "--sandbox", "workspace-write", "--skip-git-repo-check")

	prompt := spec.Prompt
	if spec.SystemPrompt != "" {
		prompt = "System instructions:\n" + spec.SystemPrompt + "\n\nTask:\n" + spec.Prompt
	}
	// "-" makes codex exec read the prompt from stdin — prompts can exceed
	// the ~32K Windows command-line limit.
	args = append(args, "-")

	return startProc(ctx, procConfig{
		Command:    "codex",
		Args:       args,
		Dir:        spec.WorkDir,
		Env:        spec.Env,
		Stdin:      prompt,
		TimeoutSec: spec.TimeoutSec,
	}, &codexParser{})
}

// codexParser parses codex exec's JSONL event stream.
type codexParser struct {
	mu        sync.Mutex
	finalText string
	lastText  string
	tokensIn  int64
	tokensOut int64
	failed    string
}

func (p *codexParser) ParseLine(line string, emit func(RunEvent)) {
	var evt map[string]interface{}
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": line}})
		return
	}
	msg, _ := evt["msg"].(map[string]interface{})
	if msg == nil {
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": line}})
		return
	}
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "agent_message":
		text, _ := msg["message"].(string)
		p.mu.Lock()
		p.lastText = text
		p.mu.Unlock()
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": text}})
	case "agent_reasoning":
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": msg["text"], "reasoning": true}})
	case "exec_command_begin", "mcp_tool_call_begin":
		emit(RunEvent{Kind: agentruns.LogToolCall, Payload: msg})
	case "exec_command_end", "mcp_tool_call_end":
		emit(RunEvent{Kind: agentruns.LogToolResult, Payload: msg})
	case "token_count":
		in, out := codexTokens(msg)
		p.mu.Lock()
		if in > 0 {
			p.tokensIn = in
		}
		if out > 0 {
			p.tokensOut = out
		}
		p.mu.Unlock()
		emit(RunEvent{Kind: agentruns.LogUsage, Payload: map[string]interface{}{
			"input_tokens":  in,
			"output_tokens": out,
		}})
	case "task_complete":
		p.mu.Lock()
		if text, ok := msg["last_agent_message"].(string); ok && text != "" {
			p.finalText = text
		}
		p.mu.Unlock()
	case "error":
		text, _ := msg["message"].(string)
		p.mu.Lock()
		p.failed = text
		p.mu.Unlock()
		emit(RunEvent{Kind: agentruns.LogError, Payload: map[string]interface{}{"message": text}})
	default:
		emit(RunEvent{Kind: agentruns.LogSystem, Payload: msg})
	}
}

// codexTokens digs token counts out of a token_count event, tolerating both
// the flat and the info.total_token_usage shapes.
func codexTokens(msg map[string]interface{}) (int64, int64) {
	if info, ok := msg["info"].(map[string]interface{}); ok {
		if usage, ok := info["total_token_usage"].(map[string]interface{}); ok {
			return asInt64(usage["input_tokens"]), asInt64(usage["output_tokens"])
		}
	}
	return asInt64(msg["input_tokens"]), asInt64(msg["output_tokens"])
}

func (p *codexParser) Result(exitCode int, stderrTail string) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	final := p.finalText
	if final == "" {
		final = p.lastText
	}
	res := Result{
		ExitCode:  exitCode,
		FinalText: final,
		TokensIn:  p.tokensIn,
		TokensOut: p.tokensOut,
	}
	if p.failed != "" {
		return res, errors.New(p.failed)
	}
	if exitCode != 0 {
		detail := strings.TrimSpace(stderrTail)
		if detail == "" {
			detail = "codex exited with code " + strconv.Itoa(exitCode)
		}
		return res, errors.New(detail)
	}
	return res, nil
}
