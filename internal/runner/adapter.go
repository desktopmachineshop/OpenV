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
type Adapter interface {
	Name() string
	Detect(ctx context.Context) Availability
	Start(ctx context.Context, spec RunSpec) (RunHandle, error)
}

// Registry returns all built-in adapters.
func Registry() []Adapter {
	return []Adapter{
		&ClaudeCodeAdapter{},
		&CodexCLIAdapter{},
		&GeminiCLIAdapter{},
	}
}
