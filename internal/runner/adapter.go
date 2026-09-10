package runner

import (
	"context"
	"errors"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

// Availability is a provider detection result.
type Availability struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	LoggedIn  bool   `json:"logged_in"`
	Detail    string `json:"detail,omitempty"`
}

// MCPServerConfig describes the MCP server handed to the agent CLI.
type MCPServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

// RunSpec is everything an adapter needs to execute one run.
type RunSpec struct {
	RunID        string
	WorkDir      string
	Prompt       string
	SystemPrompt string
	Model        string
	// Effort is the reasoning effort level ("", low, medium, high, xhigh,
	// max). Adapters map it to their CLI's nearest equivalent or ignore it.
	Effort string
	MCP    MCPServerConfig
	// AllowedTools is the agent definition's tool allowlist. It is mandatory:
	// a CLI started without one runs with every tool it has, so an adapter
	// refuses a spec that carries none (REQ-91).
	AllowedTools []string
	// Untrusted marks a run whose prompt or tool results carry content
	// authored outside the workspace — an interview participant's words, a
	// cloned repository, a fetched page. Such a run auto-approves nothing:
	// the allowlist is its entire approval surface, and file edits or shell
	// commands outside it are denied rather than granted (REQ-91, HAZ-1).
	// Set by the worker from the agent definition (agents.UntrustedInput).
	Untrusted  bool
	MaxTurns   int
	TimeoutSec int
	Env        map[string]string
}

// RunEvent is one streamed event from a running agent.
type RunEvent struct {
	Kind    string
	Payload map[string]interface{}
}

// Result is the terminal outcome of a run.
type Result struct {
	ExitCode  int
	FinalText string
	TokensIn  int64
	TokensOut int64
	CostUSD   *float64
	// Timing as reported by the provider CLI, when it reports any: the
	// CLI's whole wall time, the part of it spent waiting on the model, and
	// the number of model round trips. Zero when the adapter has no figures.
	DurationMs    int64
	DurationAPIMs int64
	NumTurns      int64
}

// RunHandle controls a started run.
type RunHandle interface {
	Events() <-chan RunEvent
	Wait() (Result, error)
	Cancel()
}

// Adapter drives one agent provider.
//
// Per-adapter capability matrix (RunSpec fields honoured vs. rejected):
//
//	Capability     claude-code          codex-cli            gemini-cli
//	-----------    ------------------   ------------------   ------------------
//	Model          yes                  yes                  yes
//	Effort         yes                  yes (capped "high")  no (ignored*)
//	SystemPrompt   yes                  yes (prefixed)       yes (prefixed)
//	MaxTurns       yes                  error if set         error if set
//	AllowedTools   --allowedTools       sandbox + MCP filter tools.core +
//	                                                         includeTools
//	                                                         + MCP filter
//	Untrusted      (no widening**)      --sandbox read-only  --approval-mode
//	MCP env token  file (0600)          process env (byname) process env ($VAR)
//
// *gemini's headless CLI exposes no reasoning-effort control, so Effort is a
// documented no-op there rather than an error (it never runs unconstrained on
// account of it). MaxTurns, by contrast, is a safety limit: an adapter that
// cannot enforce a requested cap fails the run at Start instead of silently
// running without it.
//
// **claude-code runs on the CLI's default permission mode whether or not the
// run is trusted; the allowlist is the whole approval surface. Trust changes
// what the *other* CLIs may touch (codex's sandbox, gemini's approval mode),
// never what claude may do unasked.
//
// AllowedTools is mandatory on every agent (REQ-91), and an empty one is
// refused everywhere — API, worker and adapter. A non-empty one is never a
// reason to refuse: what differs is how much of it a CLI can apply.
//
//   - claude-code takes it verbatim as --allowedTools.
//   - gemini-cli has it translated into the settings it documents for
//     restricting tools: tools.core for its built-ins, and the openv server's
//     includeTools for the OpenV tools (see geminiToolSettings).
//   - codex exec has no allowlist of any kind, so it is confined instead:
//     --sandbox workspace-write, or read-only when untrusted.
//
// All three additionally hand openv-mcp OPENV_MCP_TOOLS, so the OpenV MCP
// server serves only the mcp__openv__* tools the definition names, whatever
// the CLI's own flags can or cannot express. See docs/agents.md, "Tools an
// agent may use".
type Adapter interface {
	Name() string
	Detect(ctx context.Context) Availability
	Start(ctx context.Context, spec RunSpec) (RunHandle, error)
}

// agentLabel names an agent the way a person would look for it: its name and
// slug, or whichever of the two the claim carries.
func agentLabel(a *agents.Agent) string {
	switch {
	case a == nil:
		return "(unknown)"
	case a.Name != "" && a.Slug != "":
		return a.Name + " (" + a.Slug + ")"
	case a.Name != "":
		return a.Name
	case a.Slug != "":
		return a.Slug
	default:
		return "(unnamed)"
	}
}

// requireAllowedTools refuses to launch a vendor CLI for an agent that names
// no tools (REQ-91). Every adapter calls it first, so the guarantee does not
// depend on the caller: the worker checks too, and names the agent when it
// does, but an adapter driven from anywhere else still cannot start a CLI
// with an implicit "all tools" allowance.
func requireAllowedTools(spec RunSpec) error {
	if len(agents.NonEmptyTools(spec.AllowedTools)) == 0 {
		return errors.New(agents.AllowedToolsRequired)
	}
	return nil
}

// mergedProcEnv builds the environment for a CLI subprocess, overlaying the
// MCP server's env (which carries the run token) on top of the run env. The
// codex and gemini adapters both forward the token from this process
// environment to the MCP server they spawn — by variable name or $VAR
// reference — so the secret never appears in argv or a world-readable file.
func mergedProcEnv(spec RunSpec) map[string]string {
	env := make(map[string]string, len(spec.Env)+len(spec.MCP.Env))
	for k, v := range spec.Env {
		env[k] = v
	}
	for k, v := range spec.MCP.Env {
		env[k] = v
	}
	return env
}

// firstNonEmpty returns the first argument that is not the empty string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Registry returns all built-in adapters.
func Registry() []Adapter {
	return []Adapter{
		&ClaudeCodeAdapter{},
		&CodexCLIAdapter{},
		&GeminiCLIAdapter{},
	}
}
