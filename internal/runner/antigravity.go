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

// Antigravity CLI (`agy`) is Google's successor to the Gemini CLI: on
// 18 June 2026 the free, Google One, AI Pro and AI Ultra tiers were moved to
// it, leaving Gemini CLI to Code Assist Standard/Enterprise licences, Google
// Cloud and paid API keys (see docs/agents.md, "Gemini CLI: what Google still
// serves").
//
// Everything this adapter does is derived from Google's published CLI
// documentation rather than from the vendor's source: `agy` is a closed-source
// Go binary. Where the documentation is silent, this adapter fails closed and
// says so, rather than guessing at a flag whose failure mode would be a run
// with wider permissions than it asked for.
const (
	antigravityBinary = "agy"
	// antigravityKeyEnv is the documented way to authenticate `agy` where
	// there is no browser and no OS keyring — which is every runner OpenV
	// owns. See antigravityAuthNote.
	antigravityKeyEnv = "GEMINI_API_KEY"
	// antigravityWorkspaceMCP is the per-workspace MCP config the CLI reads.
	// Workspace-scoped on purpose: the global file lives under the member's
	// HOME, and a run has no business writing there.
	antigravityWorkspaceMCP = ".agents/mcp_config.json"
)

// antigravityAuthNote explains the one authentication path that works on a
// runner, and why the other one cannot.
//
// `agy` keeps OAuth credentials in the operating system's native keyring
// (Keychain, Linux Secret Service/dbus, Windows Credential Manager). A runner
// container has no keyring service, so an OAuth sign-in there has nowhere to
// persist even if it completed. Google documents an API key for exactly this
// case, and that is the path OpenV supports.
const antigravityAuthNote = "Antigravity CLI keeps its sign-in in the operating system keyring, " +
	"which a runner container does not have. Set a Gemini API key on the workspace instead — it is " +
	"the authentication Google documents for machines with no browser and no keyring."

// AntigravityAdapter drives Google's Antigravity CLI.
type AntigravityAdapter struct{}

func (a *AntigravityAdapter) Name() string { return providers.ProviderAntigravityCLI }

// Detect checks for the agy binary and whether it can authenticate at all
// here. Unlike the other CLIs there is no credentials file to stat: the OAuth
// store is the OS keyring, so an API key is the only signal a runner can read.
func (a *AntigravityAdapter) Detect(ctx context.Context) Availability {
	if _, err := exec.LookPath(antigravityBinary); err != nil {
		return Availability{Detail: "agy binary not found on PATH"}
	}
	version, err := runVersion(ctx, antigravityBinary, "--version")
	if err != nil {
		return Availability{Detail: "agy --version failed: " + err.Error()}
	}
	av := Availability{Installed: true, Version: version}
	if os.Getenv(antigravityKeyEnv) != "" {
		av.LoggedIn = true
		av.Detail = "API key mode (" + antigravityKeyEnv + ")"
		return av
	}
	// Deliberately not reported as logged in on the strength of a keyring we
	// cannot read: claiming a session that is not there turns a clear
	// "set a key" into a confusing mid-run authentication failure.
	av.Detail = "no " + antigravityKeyEnv + " set. " + antigravityAuthNote
	return av
}

// antigravityUnsupported refuses the specs this adapter cannot honour, for
// the same reason codex and gemini refuse theirs: better a refusal at Start
// than a run that quietly does less, or more, than it was asked to.
func antigravityUnsupported(spec RunSpec) error {
	if len(spec.AllowedTools) == 0 {
		return fmt.Errorf("%w: an antigravity-cli agent must name its allowed_tools", ErrAgentPolicy)
	}
	if spec.RepoAccess {
		// The CLI's approval rules (permissions.allow) are documented as
		// living in a settings file under HOME, with no per-run override
		// flag. OpenV will not write a member's global settings, and will
		// not pass --dangerously-skip-permissions, so it cannot grant the
		// write approvals a repo-editing agent needs. claude-code can
		// express that safely; this cannot (REQ-91, HAZ-1).
		return fmt.Errorf("%w: antigravity-cli cannot run a repository-access agent — "+
			"its file-write approvals live in a settings file OpenV does not own. Use claude-code for "+
			"repo-editing agents", ErrAgentPolicy)
	}
	return nil
}

// antigravityEffort maps OpenV's effort ladder onto the CLI's three levels.
// Anything above high is capped rather than refused: the extra rungs exist
// for providers that have them, and a cap is the documented nearest thing.
func antigravityEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh", "max":
		return "high"
	}
	return ""
}

// buildAntigravityArgs assembles the headless argv.
//
// `-p` carries the prompt: unlike gemini, the documented non-interactive form
// takes the prompt as an argument rather than on stdin, so a very large prompt
// is bounded by the platform's command-line limit.
//
// --dangerously-skip-permissions is never passed. It is this CLI's equivalent
// of gemini's --yolo: it approves every tool call including file writes and
// shell commands, which is precisely what an allowlist exists to prevent.
func buildAntigravityArgs(spec RunSpec) ([]string, error) {
	if err := antigravityUnsupported(spec); err != nil {
		return nil, err
	}
	prompt := spec.Prompt
	if spec.SystemPrompt != "" {
		// No append-system-prompt flag is documented; prefix it, as the
		// gemini adapter does for the same reason.
		prompt = "System instructions:\n" + spec.SystemPrompt + "\n\nTask:\n" + spec.Prompt
	}
	args := []string{"-p", prompt, "--output-format", "json"}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if effort := antigravityEffort(spec.Effort); effort != "" {
		args = append(args, "--effort", effort)
	}
	return args, nil
}

// writeAntigravityMCP writes the workspace MCP config wiring openv-mcp in.
//
// No env block is written. The CLI launches an stdio server as a child
// process, so it inherits the environment OpenV gives the CLI — which is
// where the run token belongs. Writing it into a file under the workspace
// would put a live credential on disk inside what may be a repository clone,
// which is the one thing the gemini adapter's ${VAR} indirection exists to
// avoid; not writing it at all is simpler and strictly safer.
func writeAntigravityMCP(workDir string, mcp MCPServerConfig) error {
	path := filepath.Join(workDir, antigravityWorkspaceMCP)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
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
			},
		},
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o600)
}

// Start launches a headless agy run.
//
// The tool surface is confined by OPENV_MCP_TOOLS alone — the OpenV MCP
// server serves only the mcp__openv__* tools the definition names, so an
// allowlist is enforced at the server rather than at the CLI. OpenV widens
// nothing on this provider: the CLI's own settings decide what its built-in
// tools may do, and OpenV neither relaxes them nor pretends to.
func (a *AntigravityAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	spec = withOpenVToolFilter(spec)
	args, err := buildAntigravityArgs(spec)
	if err != nil {
		return nil, err
	}
	noteMaxTurnsUnenforced(providers.ProviderAntigravityCLI, spec)

	if err := writeAntigravityMCP(spec.WorkDir, spec.MCP); err != nil {
		return nil, err
	}

	procEnv := mergedProcEnv(spec)
	for k, v := range spec.MCP.Env {
		procEnv[k] = v
	}

	return startProc(ctx, procConfig{
		Command:    antigravityBinary,
		Args:       args,
		Dir:        spec.WorkDir,
		Env:        procEnv,
		TimeoutSec: spec.TimeoutSec,
		EmitStderr: true,
	}, &antigravityParser{})
}

// antigravityParser accumulates stdout and parses the single JSON envelope
// --output-format json leaves at exit.
//
// The envelope's field names are not published, so the answer is read from
// the first of several plausible keys rather than one assumed name, and an
// envelope none of them match is reported as a failure with the raw output
// attached. That is deliberate: passing unparsed output off as the agent's
// answer is how a usage banner becomes a requirement.
type antigravityParser struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (p *antigravityParser) ParseLine(line string, emit func(RunEvent)) {
	p.mu.Lock()
	p.buf.WriteString(line)
	p.buf.WriteString("\n")
	p.mu.Unlock()
}

// antigravityTextKeys are the envelope keys that might carry the answer,
// most specific first.
var antigravityTextKeys = []string{"response", "result", "text", "output", "message"}

func antigravityText(envelope map[string]interface{}) (string, bool) {
	for _, key := range antigravityTextKeys {
		if v, ok := envelope[key].(string); ok && strings.TrimSpace(v) != "" {
			return v, true
		}
	}
	return "", false
}

// antigravityTokens reads usage counts from whichever of the usual shapes the
// envelope carries. Best effort: a missing count is reported as zero, never
// as a failure — token accounting is not worth failing a good run over.
func antigravityTokens(envelope map[string]interface{}) (int64, int64) {
	for _, key := range []string{"usage", "stats", "tokens"} {
		block, ok := envelope[key].(map[string]interface{})
		if !ok {
			continue
		}
		in := firstInt64(block, "input_tokens", "inputTokens", "prompt", "prompt_tokens")
		out := firstInt64(block, "output_tokens", "outputTokens", "candidates", "completion_tokens")
		if in != 0 || out != 0 {
			return in, out
		}
	}
	return 0, 0
}

func firstInt64(block map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		if v := asInt64(block[k]); v != 0 {
			return v
		}
	}
	return 0
}

func (p *antigravityParser) Result(exitCode int, stderrTail string) (Result, error) {
	p.mu.Lock()
	raw := strings.TrimSpace(p.buf.String())
	p.mu.Unlock()

	res := Result{ExitCode: exitCode}

	var envelope map[string]interface{}
	parsed := raw != "" && json.Unmarshal([]byte(raw), &envelope) == nil
	if parsed {
		res.TokensIn, res.TokensOut = antigravityTokens(envelope)
		if errBlock, ok := envelope["error"].(map[string]interface{}); ok && errBlock != nil {
			msg, _ := errBlock["message"].(string)
			if msg == "" {
				msg = "antigravity reported an error"
			}
			return res, errors.New(msg)
		}
	}

	if exitCode != 0 {
		detail := firstNonEmpty(strings.TrimSpace(stderrTail), raw,
			"agy exited with code "+strconv.Itoa(exitCode))
		return res, errors.New(antigravityFailure(detail))
	}
	if !parsed {
		detail := firstNonEmpty(strings.TrimSpace(stderrTail), raw, "agy produced no JSON output")
		return res, errors.New(antigravityFailure(detail))
	}
	text, ok := antigravityText(envelope)
	if !ok {
		return res, errors.New("agy returned a JSON envelope with no recognisable answer field: " + raw)
	}
	res.FinalText = text
	return res, nil
}

// antigravityFailure appends the authentication note when the failure looks
// like one: a run on a runner with no key fails this way, and the note is the
// difference between "retry" and "this needs a key".
func antigravityFailure(detail string) string {
	lower := strings.ToLower(detail)
	for _, signal := range []string{"auth", "sign in", "sign-in", "login", "credential", "unauthenticated"} {
		if strings.Contains(lower, signal) {
			return detail + " — " + antigravityAuthNote
		}
	}
	return detail
}
