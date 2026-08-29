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

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// GeminiCLIAdapter runs Google's Gemini CLI in headless mode.
type GeminiCLIAdapter struct{}

// Name returns the provider name.
func (a *GeminiCLIAdapter) Name() string { return providers.ProviderGeminiCLI }

// Detect checks for the gemini binary.
func (a *GeminiCLIAdapter) Detect(ctx context.Context) Availability {
	if _, err := exec.LookPath("gemini"); err != nil {
		return Availability{Detail: "gemini binary not found on PATH"}
	}
	version, err := runVersion(ctx, "gemini", "--version")
	if err != nil {
		return Availability{Detail: "gemini --version failed: " + err.Error()}
	}
	av := Availability{Installed: true, Version: version}
	// API-key mode: a key in the environment makes the CLI usable with no
	// interactive sign-in.
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		av.LoggedIn = true
		av.Detail = "API key mode (GEMINI_API_KEY/GOOGLE_API_KEY)"
		return av
	}
	// OAuth sign-in stores credentials under ~/.gemini; their presence is a
	// cheap, real logged-in signal.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dir := filepath.Join(home, ".gemini")
		for _, name := range []string{"oauth_creds.json", "google_accounts.json"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				av.LoggedIn = true
				av.Detail = "~/.gemini credentials present; assuming logged in"
				return av
			}
		}
		av.Detail = "no ~/.gemini credentials found; run `gemini` once to sign in"
	}
	return av
}

// Start launches a headless gemini run. Gemini's json output arrives as one
// final object, so stdout events stream only at the end; stderr is surfaced
// live (EmitStderr) so a long run isn't silent and failures stay visible.
func (a *GeminiCLIAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	args, err := buildGeminiArgs(spec)
	if err != nil {
		return nil, err
	}

	// Isolated settings file: rather than overwriting the user's workspace
	// .gemini/settings.json, write our own and point gemini's *system*
	// settings path (highest precedence) at it. It wires the openv MCP server
	// as trusted (its tool calls auto-approve without yolo) and pins a
	// least-privilege approval mode. The run token is referenced via $VAR and
	// resolved from the process environment, so it never lands in the file.
	settingsPath := filepath.Join(spec.WorkDir, ".openv", "gemini-settings.json")
	if err := writeGeminiSettings(settingsPath, spec.MCP); err != nil {
		return nil, err
	}
	procEnv := mergedProcEnv(spec)
	procEnv["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] = settingsPath

	prompt := spec.Prompt
	if spec.SystemPrompt != "" {
		// Headless gemini has no append-system-prompt flag; prefix instead.
		prompt = "System instructions:\n" + spec.SystemPrompt + "\n\nTask:\n" + spec.Prompt
	}

	return startProc(ctx, procConfig{
		Command:    "gemini",
		Args:       args,
		Dir:        spec.WorkDir,
		Env:        procEnv,
		Stdin:      prompt,
		TimeoutSec: spec.TimeoutSec,
		EmitStderr: true,
	}, &geminiParser{})
}

// buildGeminiArgs assembles the headless gemini argv.
//
// The prompt travels over stdin (piped input is the headless prompt) — prompts
// can exceed the ~32K Windows command-line limit. spec.Effort is a documented
// no-op: the gemini CLI has no headless reasoning-effort control.
func buildGeminiArgs(spec RunSpec) ([]string, error) {
	if err := geminiUnsupported(spec); err != nil {
		return nil, err
	}
	// --approval-mode auto_edit (NOT yolo): file edits auto-approve so the
	// agent can do work, but shell and other sensitive tools are not blanket
	// auto-approved (yolo would auto-approve everything, unsandboxed). The
	// openv MCP server is separately marked trusted in the settings file, so
	// its tool calls run without prompting under this mode.
	args := []string{
		"--output-format", "json",
		"--approval-mode", "auto_edit",
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	return args, nil
}

// geminiUnsupported fails a run that carries constraints the headless gemini
// CLI cannot enforce, rather than silently running unconstrained. The CLI has
// no reliable per-run turn cap, and its tool names don't map to the agent's
// (Claude-shaped) AllowedTools list, so honouring either would be incorrect.
func geminiUnsupported(spec RunSpec) error {
	var unsupported []string
	if spec.MaxTurns > 0 {
		unsupported = append(unsupported, "MaxTurns")
	}
	if len(spec.AllowedTools) > 0 {
		unsupported = append(unsupported, "AllowedTools")
	}
	if len(unsupported) > 0 {
		return fmt.Errorf("gemini-cli adapter cannot enforce %s: the headless gemini CLI has no equivalent, and running without the requested limit would be unconstrained — clear these on the agent or use a provider that supports them (e.g. claude-code)",
			strings.Join(unsupported, " and "))
	}
	return nil
}

// writeGeminiSettings writes the isolated gemini settings file that wires the
// openv MCP server. Pointed at via GEMINI_CLI_SYSTEM_SETTINGS_PATH, it never
// overwrites the user's own .gemini/settings.json. Secret env values are
// written as ${VAR} references resolved from the process environment at load
// time, so the token stays out of the file; the file is still written 0600.
func writeGeminiSettings(path string, mcp MCPServerConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	args := mcp.Args
	if args == nil {
		args = []string{}
	}
	env := make(map[string]string, len(mcp.Env))
	for k := range mcp.Env {
		env[k] = "${" + k + "}"
	}
	cfg := map[string]interface{}{
		"general": map[string]interface{}{
			"defaultApprovalMode": "auto_edit",
		},
		"mcpServers": map[string]interface{}{
			"openv": map[string]interface{}{
				"command": mcp.Command,
				"args":    args,
				"env":     env,
				"trust":   true,
			},
		},
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o600)
}

// geminiParser accumulates stdout and parses the single JSON object at exit.
type geminiParser struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (p *geminiParser) ParseLine(line string, emit func(RunEvent)) {
	p.mu.Lock()
	p.buf.WriteString(line)
	p.buf.WriteString("\n")
	p.mu.Unlock()
}

func (p *geminiParser) Result(exitCode int, stderrTail string) (Result, error) {
	p.mu.Lock()
	raw := strings.TrimSpace(p.buf.String())
	p.mu.Unlock()

	res := Result{ExitCode: exitCode}

	var out struct {
		Response string                 `json:"response"`
		Stats    map[string]interface{} `json:"stats"`
		Error    map[string]interface{} `json:"error"`
	}
	parsed := raw != "" && json.Unmarshal([]byte(raw), &out) == nil
	if parsed {
		res.TokensIn, res.TokensOut = geminiTokens(out.Stats)
		if out.Error != nil {
			msg, _ := out.Error["message"].(string)
			if msg == "" {
				msg = "gemini reported an error"
			}
			return res, errors.New(msg)
		}
	}

	if exitCode != 0 {
		// Never let a usage banner / crash dump become the agent's answer:
		// report it as the failure it is (stderr first, then any raw stdout).
		detail := firstNonEmpty(strings.TrimSpace(stderrTail), raw, "gemini exited with code "+strconv.Itoa(exitCode))
		return res, errors.New(detail)
	}
	if !parsed {
		// `--output-format json` always emits a JSON object on a real success;
		// anything else (empty output, a usage banner printed with exit 0) is a
		// CLI failure, not an answer. Surface it as an error rather than passing
		// the raw text off as the agent's response.
		detail := firstNonEmpty(strings.TrimSpace(stderrTail), raw, "gemini produced no JSON output")
		return res, errors.New(detail)
	}

	res.FinalText = out.Response
	return res, nil
}

// geminiTokens sums prompt/candidate token counts across models in the
// stats blob, best-effort.
func geminiTokens(stats map[string]interface{}) (int64, int64) {
	models, _ := stats["models"].(map[string]interface{})
	var in, out int64
	for _, mAny := range models {
		m, _ := mAny.(map[string]interface{})
		tokens, _ := m["tokens"].(map[string]interface{})
		in += asInt64(tokens["prompt"])
		out += asInt64(tokens["candidates"])
	}
	return in, out
}
