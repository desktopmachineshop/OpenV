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
	// Ask the CLI. `claude auth status --json` answers authoritatively,
	// including for credentials that live nowhere under HOME (a token in the
	// environment, or a host-managed provider).
	if out, err := runVersion(ctx, "claude", "auth", "status", "--json"); err == nil {
		if loggedIn, method, ok := parseClaudeAuthStatus(out); ok {
			av.LoggedIn = loggedIn
			if loggedIn {
				av.Detail = "signed in (" + method + ")"
			} else {
				av.Detail = "not signed in; connect Claude Code to sign in"
			}
			return av
		}
	}

	// Older CLIs have no `auth status`. Fall back to looking for config, and
	// say so: the presence of a directory is a guess, not a sign-in — a
	// runner can hold a config and still refuse every run.
	home, _ := os.UserHomeDir()
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
			av.LoggedIn = true
			av.Detail = "~/.claude present; assuming logged in (CLI too old for `auth status`)"
		} else if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
			av.LoggedIn = true
			av.Detail = "~/.claude.json present; assuming logged in (CLI too old for `auth status`)"
		} else {
			av.Detail = "no ~/.claude config found; run `claude` once to log in"
		}
	}
	return av
}

// parseClaudeAuthStatus reads `claude auth status --json`. It reports
// (loggedIn, method, ok); ok is false when the output is not the JSON this
// understands, so the caller can fall back rather than call a CLI that
// answered in some other shape "signed out".
func parseClaudeAuthStatus(out string) (bool, string, bool) {
	var status struct {
		LoggedIn   *bool  `json:"loggedIn"`
		AuthMethod string `json:"authMethod"`
	}
	start := strings.Index(out, "{")
	if start < 0 {
		return false, "", false
	}
	if err := json.Unmarshal([]byte(out[start:]), &status); err != nil || status.LoggedIn == nil {
		return false, "", false
	}
	method := status.AuthMethod
	if method == "" {
		method = "unknown method"
	}
	return *status.LoggedIn, method, true
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

// partialMessagesProbe caches whether the installed claude CLI understands
// --include-partial-messages. Probed once per process: `claude --help` costs
// a second, and the answer cannot change under a running worker.
var partialMessagesProbe struct {
	once      sync.Once
	supported bool
}

// claudeSupportsPartialMessages reports whether this machine's claude accepts
// --include-partial-messages. A probe that fails answers false, so an
// unreadable or ancient CLI keeps running exactly as it did before.
func claudeSupportsPartialMessages(ctx context.Context) bool {
	partialMessagesProbe.once.Do(func() {
		out, err := runVersion(ctx, "claude", "--help")
		if err != nil && out == "" {
			return
		}
		partialMessagesProbe.supported = strings.Contains(out, "--include-partial-messages")
	})
	return partialMessagesProbe.supported
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
	// Token-level streaming, so the chat panel can show the answer as it is
	// written instead of waiting out the whole turn. Older CLIs reject the
	// unknown flag and would fail the run, so it is added only when this
	// machine's claude advertises it; without it the parser still streams at
	// whole-assistant-message granularity.
	if claudeSupportsPartialMessages(ctx) {
		args = append(args, "--include-partial-messages")
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
	// Assistant text as it is written, for the streaming chat bubble:
	// doneText holds the messages that completed during this run and
	// liveText the one currently being typed out of `stream_event` deltas.
	// An `assistant` event is authoritative for the message it reports, so
	// it replaces whatever the deltas accumulated for it — the two shapes
	// describe the same text and must never both be counted.
	doneText  []string
	liveText  string
	tokensIn  int64
	tokensOut int64
	costUSD   *float64
	isError   bool
	errorText string
	// CLI-reported timing (see the result branch of ParseLine).
	durationMs    int64
	durationAPIMs int64
	numTurns      int64
}

func (p *claudeParser) ParseLine(line string, emit func(RunEvent)) {
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": line}})
		return
	}
	switch msg["type"] {
	case "stream_event":
		// Only present with --include-partial-messages: the raw Anthropic
		// message stream, of which the text deltas are what a reader wants
		// to see appear. Tool-use input deltas are deliberately ignored.
		p.appendDelta(msg)
	case "assistant":
		message, _ := msg["message"].(map[string]interface{})
		content, _ := message["content"].([]interface{})
		var finished []string
		for _, blockAny := range content {
			block, _ := blockAny.(map[string]interface{})
			switch block["type"] {
			case "text":
				text, _ := block["text"].(string)
				if strings.TrimSpace(text) != "" {
					finished = append(finished, text)
				}
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
		p.mu.Lock()
		p.doneText = append(p.doneText, finished...)
		p.liveText = ""
		p.mu.Unlock()
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
		// Timing from the CLI itself: duration_ms is its whole wall time and
		// duration_api_ms the part spent waiting on the model, so the
		// difference is CLI, MCP and tool overhead. num_turns says how many
		// model round trips the answer took. Recorded so slow turns can be
		// attributed without guessing (docs/assessments, agent latency).
		emit(RunEvent{Kind: agentruns.LogUsage, Payload: map[string]interface{}{
			"input_tokens":    p.tokensIn,
			"output_tokens":   p.tokensOut,
			"total_cost_usd":  msg["total_cost_usd"],
			"duration_ms":     msg["duration_ms"],
			"duration_api_ms": msg["duration_api_ms"],
			"num_turns":       msg["num_turns"],
		}})
		p.mu.Lock()
		p.durationMs = asInt64(msg["duration_ms"])
		p.durationAPIMs = asInt64(msg["duration_api_ms"])
		p.numTurns = asInt64(msg["num_turns"])
		p.mu.Unlock()
	case "system":
		emit(RunEvent{Kind: agentruns.LogSystem, Payload: msg})
	default:
		emit(RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{"text": line}})
	}
}

// appendDelta folds one `stream_event` line into the live message text.
func (p *claudeParser) appendDelta(msg map[string]interface{}) {
	event, _ := msg["event"].(map[string]interface{})
	if event == nil {
		return
	}
	switch event["type"] {
	case "content_block_start":
		// A text block can start with content already in it.
		block, _ := event["content_block"].(map[string]interface{})
		if block == nil || block["type"] != "text" {
			return
		}
		if text, _ := block["text"].(string); text != "" {
			p.mu.Lock()
			p.liveText += text
			p.mu.Unlock()
		}
	case "content_block_delta":
		delta, _ := event["delta"].(map[string]interface{})
		if delta == nil || delta["type"] != "text_delta" {
			return
		}
		text, _ := delta["text"].(string)
		if text == "" {
			return
		}
		p.mu.Lock()
		p.liveText += text
		p.mu.Unlock()
	}
}

// PartialText returns the assistant text written so far: the messages this
// run has completed plus the one being typed. Safe to call from the log pump
// while the process is still running.
func (p *claudeParser) PartialText() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return joinPartial(p.doneText, p.liveText)
}

func (p *claudeParser) Result(exitCode int, stderrTail string) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	res := Result{
		ExitCode:      exitCode,
		FinalText:     p.finalText,
		TokensIn:      p.tokensIn,
		TokensOut:     p.tokensOut,
		CostUSD:       p.costUSD,
		DurationMs:    p.durationMs,
		DurationAPIMs: p.durationAPIMs,
		NumTurns:      p.numTurns,
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
