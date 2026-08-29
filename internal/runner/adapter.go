package runner

import "context"

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
	MCP          MCPServerConfig
	AllowedTools []string
	MaxTurns     int
	TimeoutSec   int
	Env          map[string]string
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
//	Capability     claude-code   codex-cli            gemini-cli
//	-----------    -----------   ------------------   ------------------
//	Model          yes           yes                  yes
//	Effort         yes           yes (capped "high")  no (ignored*)
//	SystemPrompt   yes           yes (prefixed)       yes (prefixed)
//	MaxTurns       yes           error if set         error if set
//	AllowedTools   yes           error if set         error if set
//	MCP env token  file (0600)   process env (byname) process env ($VAR)
//
// *gemini's headless CLI exposes no reasoning-effort control, so Effort is a
// documented no-op there rather than an error (it never runs unconstrained on
// account of it). MaxTurns/AllowedTools, by contrast, are safety limits: an
// adapter that cannot enforce a requested limit fails the run at Start instead
// of silently running without it.
type Adapter interface {
	Name() string
	Detect(ctx context.Context) Availability
	Start(ctx context.Context, spec RunSpec) (RunHandle, error)
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
