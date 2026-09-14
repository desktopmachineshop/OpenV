package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// Gemini approval modes. "auto_edit" auto-approves file edits (and nothing
// else); "default" approves nothing on its own and is what an untrusted run
// gets. "yolo" is never used.
const (
	geminiModeTrusted   = "auto_edit"
	geminiModeUntrusted = "default"
)

// geminiApprovalMode is the approval mode a spec runs under. It is applied
// twice — as the --approval-mode flag and as defaultApprovalMode in the
// isolated settings file — so neither one alone can widen the run.
func geminiApprovalMode(spec RunSpec) string {
	if spec.Untrusted {
		return geminiModeUntrusted
	}
	return geminiModeTrusted
}

// The Gemini CLI refuses to start non-interactively unless an auth method is
// named. It prints "Please set an Auth method in your ~/.gemini/settings.json
// or specify one of the following environment variables before running:
// GEMINI_API_KEY, GOOGLE_GENAI_USE_VERTEXAI, GOOGLE_GENAI_USE_GCA" and exits
// 41 — before the sign-in flow can print the URL it exists to print, and
// before a run does any work. A transient runner hands every lease a fresh
// HOME, so there is never a settings file there to have named one.
//
// The environment is the right lever rather than the isolated settings file
// this adapter already writes: the CLI resolves the member's OWN
// security.auth.selectedAuthType first and falls back to the environment only
// when that is unset, so naming the mode here fills the gap instead of
// overriding a choice they made.
const (
	// geminiOAuthEnv selects Google-account OAuth: the mode OpenV's
	// paste-back sign-in drives, and the one the ~/.gemini credentials it
	// leaves behind belong to.
	geminiOAuthEnv = "GOOGLE_GENAI_USE_GCA"
	// geminiOAuthEnvValue is exact — the CLI compares against "true", so "1"
	// reads as no auth method at all.
	geminiOAuthEnvValue = "true"
	// geminiTrustEnv marks the workspace trusted. The CLI refuses to run in
	// an untrusted directory and names three ways out — trust it in
	// interactive mode, pass --skip-trust, or set this. Only the last suits
	// a lease: its workspace is created fresh for the sign-in, so no trust
	// record for it can exist yet.
	geminiTrustEnv      = "GEMINI_CLI_TRUST_WORKSPACE"
	geminiTrustEnvValue = "true"
)

// geminiHeadlessEnvKeys are the variables whose value makes the CLI call
// itself headless whatever terminal it is on. A sign-in must be interactive
// (see geminiLoginEnv), so the sign-in command clears them.
//
// The CLI reads each as == "true", so an empty value is as good as unset —
// which is what this can do, since the child's environment is the parent's
// with these appended and the later entry winning.
var geminiHeadlessEnvKeys = []string{"CI", "GITHUB_ACTIONS"}

// geminiLoginEnv is the environment the sign-in command runs under.
//
// The CLI refuses a manual authorization unless it believes the session is
// interactive: with the browser suppressed it throws "Manual authorization
// is required but the current session is non-interactive" and dies, instead
// of printing the URL the sign-in exists to relay. Its own isHeadlessMode()
// decides, and answers yes when CI or GITHUB_ACTIONS is "true", when stdin
// or stdout is not a terminal, or when the command line carries -p/--prompt.
// Three causes, three fixes: the pseudo-terminal settles the second, dropping
// -p from the command settles the third, and this settles the first.
func geminiLoginEnv() []string {
	env := []string{
		// There is no browser the runner could show, so the CLI prints a
		// URL and takes a code back instead.
		"NO_BROWSER=1",
		geminiOAuthEnv + "=" + geminiOAuthEnvValue,
		geminiTrustEnv + "=" + geminiTrustEnvValue,
	}
	for _, key := range geminiHeadlessEnvKeys {
		env = append(env, key+"=")
	}
	return env
}

// geminiAuthEnvKeys are the variables that already name an auth mode. The
// list mirrors the CLI's own resolver and adds GOOGLE_API_KEY, which Detect
// reports as API-key mode. geminiOAuthEnv outranks every one of them in that
// resolver, so adding it to a run that set one would quietly move the run
// onto a different account — hence the check before it is added at all.
var geminiAuthEnvKeys = []string{
	geminiOAuthEnv,
	"GOOGLE_GENAI_USE_VERTEXAI",
	"GOOGLE_GEMINI_BASE_URL",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"GEMINI_CLI_USE_COMPUTE_ADC",
	"CLOUD_SHELL",
}

// geminiAuthNamed reports whether an auth mode is already named, reading the
// run's own environment first and the process environment behind it — that is
// where a hosted container's injected key lives, and startProc layers the
// run's values over it the same way.
func geminiAuthNamed(procEnv map[string]string) bool {
	for _, key := range geminiAuthEnvKeys {
		if v, ok := procEnv[key]; ok {
			if v != "" {
				return true
			}
			continue
		}
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// geminiRunEnv is the environment one headless run adds: the isolated
// settings file, and — when nothing else names one — the OAuth auth mode,
// without which the CLI exits 41 instead of running.
func geminiRunEnv(spec RunSpec, settingsPath string) map[string]string {
	env := mergedProcEnv(spec)
	env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] = settingsPath
	if !geminiAuthNamed(env) {
		env[geminiOAuthEnv] = geminiOAuthEnvValue
	}
	return env
}

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
	spec = withOpenVToolFilter(spec)
	args, err := buildGeminiArgs(spec)
	if err != nil {
		return nil, err
	}
	noteMaxTurnsUnenforced(providers.ProviderGeminiCLI, spec)

	// Isolated settings file: rather than overwriting the user's workspace
	// .gemini/settings.json, write our own and point gemini's *system*
	// settings path (highest precedence) at it. It wires the openv MCP server
	// as trusted (its tool calls auto-approve without yolo) and pins a
	// least-privilege approval mode. The run token is referenced via $VAR and
	// resolved from the process environment, so it never lands in the file.
	settingsPath := filepath.Join(spec.WorkDir, ".openv", "gemini-settings.json")
	if err := writeGeminiSettings(settingsPath, spec.MCP, geminiApprovalMode(spec), spec.AllowedTools); err != nil {
		return nil, err
	}
	procEnv := geminiRunEnv(spec, settingsPath)

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
	//
	// An untrusted run drops even that: "default" approves nothing on its own
	// (REQ-91, HAZ-1), so a page or a participant's answer cannot talk the
	// agent into an edit.
	//
	// The allowlist itself is not a flag here — it is written into the
	// isolated settings file (tools.core and the openv server's includeTools)
	// by writeGeminiSettings, because that is where gemini documents tool
	// restriction. --allowed-tools exists but is an auto-approval list, and
	// passing it would widen the run.
	args := []string{
		"--output-format", "json",
		"--approval-mode", geminiApprovalMode(spec),
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	return args, nil
}

// geminiUnsupported fails a run the headless gemini CLI cannot serve at all,
// rather than starting one whose confinement defeats it. Two things fail:
//
//   - An empty allowlist — the one case where the CLI would run with every
//     tool it has (REQ-91).
//   - Repository access — see refuseRepoAccess. gemini's approval mode is a
//     whole-run switch, so a repo-editing agent either has every edit
//     auto-approved or waits on a confirmation a headless run cannot answer.
//
// The tool allowlist is not one of them: it is translated into gemini's own
// settings by geminiToolSettings (tools.core plus the openv server's
// includeTools) and enforced again server-side through OPENV_MCP_TOOLS.
//
// MaxTurns is not one either. The CLI has no reliable per-run turn cap, and
// agents.Definition.Validate gives every persisted definition a positive one,
// so refusing it refused every real agent on this provider. It joins Effort
// as a documented, logged no-op (noteMaxTurnsUnenforced); the run's timeout
// is the bound that holds.
func geminiUnsupported(spec RunSpec) error {
	if err := requireAllowedTools(spec); err != nil {
		return err
	}
	return refuseRepoAccess(providers.ProviderGeminiCLI, spec)
}

// writeGeminiSettings writes the isolated gemini settings file that wires the
// openv MCP server and applies the agent's tool allowlist. Pointed at via
// GEMINI_CLI_SYSTEM_SETTINGS_PATH, it never overwrites the user's own
// .gemini/settings.json. Secret env values are written as ${VAR} references
// resolved from the process environment at load time, so the token stays out
// of the file; the file is still written 0600.
//
// The allowlist lands in the two keys gemini documents for restricting tools:
// tools.core ("Restrict the set of built-in tools with an allowlist") and the
// openv server's includeTools ("Subset of tools that should be enabled for
// this server"). tools.allowed and --allowed-tools are NOT used: despite the
// name they are an auto-approval list, which would widen the run rather than
// narrow it.
func writeGeminiSettings(path string, mcp MCPServerConfig, approvalMode string, allowedTools []string) error {
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
	core, include, includeAll := geminiToolSettings(allowedTools)
	openv := map[string]interface{}{
		"command": mcp.Command,
		"args":    args,
		"env":     env,
		"trust":   true,
	}
	// Omitted means "every tool from this server", which is what the
	// mcp__openv__* wildcard asks for; an empty array would mean none.
	if !includeAll {
		openv["includeTools"] = include
	}
	cfg := map[string]interface{}{
		"general": map[string]interface{}{
			"defaultApprovalMode": approvalMode,
		},
		"tools": map[string]interface{}{
			// Always written, empty included: an agent that names only OpenV
			// tools gets no built-in tools at all, where an omitted key would
			// hand it every one gemini ships.
			"core": core,
		},
		"mcpServers": map[string]interface{}{
			"openv": openv,
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
