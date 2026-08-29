package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	av := Availability{Installed: true, Version: version}
	// API-key mode: a key in the environment makes the CLI usable with no
	// interactive sign-in.
	if os.Getenv("OPENAI_API_KEY") != "" {
		av.LoggedIn = true
		av.Detail = "API key mode (OPENAI_API_KEY)"
		return av
	}
	// Subscription sign-in (`codex login`) writes auth.json under CODEX_HOME
	// (default ~/.codex). Its presence is a cheap, real logged-in signal.
	if path := codexAuthPath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			av.LoggedIn = true
			av.Detail = "codex auth.json present; assuming logged in"
		} else {
			av.Detail = "no codex auth.json found; run `codex login` to sign in"
		}
	}
	return av
}

// codexAuthPath returns the path codex stores its subscription credentials at,
// honouring CODEX_HOME.
func codexAuthPath() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil || h == "" {
			return ""
		}
		home = filepath.Join(h, ".codex")
	}
	return filepath.Join(home, "auth.json")
}

// Start launches a codex exec run.
func (a *CodexCLIAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	args, err := buildCodexArgs(spec)
	if err != nil {
		return nil, err
	}

	prompt := spec.Prompt
	if spec.SystemPrompt != "" {
		prompt = "System instructions:\n" + spec.SystemPrompt + "\n\nTask:\n" + spec.Prompt
	}

	return startProc(ctx, procConfig{
		Command:    "codex",
		Args:       args,
		Dir:        spec.WorkDir,
		Env:        mergedProcEnv(spec),
		Stdin:      prompt,
		TimeoutSec: spec.TimeoutSec,
	}, &codexParser{})
}

// buildCodexArgs assembles the `codex exec` argv.
//
// The MCP run token is NEVER placed on the command line (where it would be
// visible in process listings). Instead codex is told — by variable NAME only,
// via mcp_servers.openv.env_vars — to forward those variables from its own
// process environment (see mergedProcEnv) to the MCP server it spawns. This is
// the same environment-passing pattern the connector/agentd use, and it is the
// mechanism codex documents for handing secrets to stdio MCP servers.
//
// Values that may contain spaces, backslashes (Windows paths) or other
// metacharacters (command, args, effort) are JSON-encoded so codex parses them
// as literal strings instead of mis-splitting them.
func buildCodexArgs(spec RunSpec) ([]string, error) {
	if err := codexUnsupported(spec); err != nil {
		return nil, err
	}

	args := []string{"exec", "--json", "--cd", spec.WorkDir}

	// MCP server wiring. command/args are JSON-encoded to stay escaped; the run
	// token and API URL are forwarded by NAME only, never by value.
	args = append(args, "-c", "mcp_servers.openv.command="+jsonScalar(spec.MCP.Command))
	if len(spec.MCP.Args) > 0 {
		argsJSON, err := json.Marshal(spec.MCP.Args)
		if err != nil {
			return nil, err
		}
		args = append(args, "-c", "mcp_servers.openv.args="+string(argsJSON))
	}
	if names := sortedKeys(spec.MCP.Env); len(names) > 0 {
		namesJSON, err := json.Marshal(names)
		if err != nil {
			return nil, err
		}
		args = append(args, "-c", "mcp_servers.openv.env_vars="+string(namesJSON))
	}

	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if spec.Effort != "" {
		// Codex tops out at "high"; map the taller Claude tiers down.
		effort := spec.Effort
		if effort == "xhigh" || effort == "max" {
			effort = "high"
		}
		args = append(args, "-c", "model_reasoning_effort="+jsonScalar(effort))
	}
	args = append(args, "--sandbox", "workspace-write", "--skip-git-repo-check")

	// "-" makes codex exec read the prompt from stdin — prompts can exceed
	// the ~32K Windows command-line limit.
	args = append(args, "-")
	return args, nil
}

// codexUnsupported fails a run that carries constraints codex exec cannot
// enforce, rather than silently running unconstrained. codex exec has no
// per-run turn cap and no tool allow-list — its only guardrail is --sandbox,
// which buildCodexArgs always pins to workspace-write.
func codexUnsupported(spec RunSpec) error {
	var unsupported []string
	if spec.MaxTurns > 0 {
		unsupported = append(unsupported, "MaxTurns")
	}
	if len(spec.AllowedTools) > 0 {
		unsupported = append(unsupported, "AllowedTools")
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("codex-cli adapter cannot enforce %s: the codex exec CLI has no equivalent, and running without the requested limit would be unconstrained — clear these on the agent or use a provider that supports them (e.g. claude-code)",
			strings.Join(unsupported, " and "))
	}
	return nil
}

// jsonScalar JSON-encodes a string so it can be handed to codex's `-c
// key=value` override as a properly-escaped literal.
func jsonScalar(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// sortedKeys returns the map keys in a stable order (deterministic argv).
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
