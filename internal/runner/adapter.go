package runner

import (
	"context"
	"errors"
	"log"
	"strings"

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
	// Set by the worker from BOTH the run's origin
	// (agentruns.Run.UntrustedOrigin — an interview turn is untrusted
	// whichever agent serves it) and the agent definition
	// (agents.Agent.UntrustedInput).
	Untrusted bool
	// RepoAccess says the run's workspace is a clone of a connected
	// repository the agent is expected to *edit*. It is separate from
	// Untrusted because the two want opposite things from a CLI: an
	// untrusted run wants the tightest confinement the CLI has, while a
	// repo-access agent's whole purpose is to write files. Only claude-code
	// can hold both at once (its allowlist names the editing tools
	// individually); codex and gemini refuse the combination at Start rather
	// than hand the agent a sandbox that silently defeats it.
	RepoAccess bool
	// MaxTurns is the definition's per-run turn cap. Only claude-code can
	// enforce it; codex and gemini treat it as a documented no-op and log
	// that they have, with TimeoutSec left as the actual bound.
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

// PartialTextSource is implemented by run handles whose provider reports the
// assistant's answer as it is written. The log pump ships what it returns to
// the API so a chat panel can show the reply forming; providers that only
// produce text at the end implement nothing and stream no partials.
type PartialTextSource interface {
	// PartialText returns the whole assistant text so far, not a delta.
	PartialText() string
}

// joinPartial renders accumulated assistant text: the messages that finished
// during a run, plus the one still being written, separated as paragraphs.
func joinPartial(done []string, live string) string {
	parts := done
	if strings.TrimSpace(live) != "" {
		parts = append(append([]string{}, done...), live)
	}
	return strings.Join(parts, "\n\n")
}

// Adapter drives one agent provider.
//
// Per-adapter capability matrix (RunSpec fields honoured vs. rejected):
//
//	Capability     claude-code          codex-cli            gemini-cli
//	-----------    ------------------   ------------------   ------------------
//	Partial text   yes (tokens)         yes (current msg)    no (one JSON blob)
//	Model          yes                  yes                  yes
//	Effort         yes                  yes (capped "high")  no (ignored*)
//	SystemPrompt   yes                  yes (prefixed)       yes (prefixed)
//	MaxTurns       yes                  no (ignored*)        no (ignored*)
//	AllowedTools   --allowedTools       sandbox + MCP filter tools.core +
//	                                                         includeTools
//	                                                         + MCP filter
//	Untrusted      (no widening**)      --sandbox read-only  --approval-mode
//	RepoAccess     yes                  error if set***      error if set***
//	MCP env token  file (0600)          process env (byname) process env ($VAR)
//
// Partial text is the answer-so-far the log pump streams to the chat panels
// (PartialTextSource): claude reports token deltas when its CLI supports
// --include-partial-messages and whole assistant messages otherwise; codex
// reports its current agent message, the same one its result reports as the
// final answer, so the bubble never shrinks when the reply lands; gemini
// prints one JSON object at the very end, so it has nothing to stream and
// reports no partials.
//
// *A no-op is documented and logged, never silent. gemini's headless CLI
// exposes no reasoning-effort control; neither codex exec nor headless gemini
// has a per-run turn cap. Neither is a safety limit an adapter can be asked
// to fake: MaxTurns bounds cost and looping, and TimeoutSec — which every
// adapter does enforce — bounds both too, so the run is never unbounded. The
// adapter logs once, at Start, that the cap is not applied on that provider.
//
// **claude-code runs on the CLI's default permission mode whether or not the
// run is trusted; the allowlist is the whole approval surface. Trust changes
// what the *other* CLIs may touch (codex's sandbox, gemini's approval mode),
// never what claude may do unasked.
//
// ***RepoAccess is refused, because it is the one case where a confinement
// would defeat the agent rather than protect it: codex's sandbox and gemini's
// approval mode are whole-workspace switches, so the only ways to run a
// repo-editing agent there are "may edit everything" or "may edit nothing".
// claude-code can express the middle — its allowlist names the editing tools
// one at a time — so repo access belongs there. See codexUnsupported.
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
		return agentPolicyError(agents.AllowedToolsRequired)
	}
	return nil
}

// ErrAgentPolicy marks an adapter refusal that only an edit to the agent
// definition can fix: the definition asks for something this provider cannot
// do safely, and the same run attempted again will be refused the same way.
// The worker classifies such a failure as agent_error, so it is never
// auto-retried. Match it with errors.Is; the message a person reads is the
// adapter's own (see agentPolicyError).
var ErrAgentPolicy = errors.New("agent definition is not runnable on this provider")

// policyError is an ErrAgentPolicy that keeps its own wording, so the run's
// failure text says the specific thing that is wrong rather than a generic
// sentinel.
type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

// Is makes errors.Is(err, ErrAgentPolicy) true without wrapping the sentinel's
// text into the message.
func (e *policyError) Is(target error) bool { return target == ErrAgentPolicy }

// agentPolicyError builds a refusal the worker will treat as non-retryable.
func agentPolicyError(msg string) error { return &policyError{msg: msg} }

// noteMaxTurnsUnenforced records, once per run at Start, that a provider is
// not applying the definition's MaxTurns.
//
// codex exec and headless gemini have no per-run turn cap, and Validate
// coerces every definition's MaxTurns to a positive number (50 by default),
// so refusing a spec that carries one would mean refusing every persisted
// agent on those providers — the failure mode this replaces. Ignoring it
// silently is the other bad answer. So it is a *documented* no-op, exactly as
// Effort already is on gemini: logged with the run id, and with the bound
// that does apply — the run-level timeout the adapter enforces itself.
func noteMaxTurnsUnenforced(provider string, spec RunSpec) {
	if spec.MaxTurns <= 0 {
		return
	}
	log.Printf("run %s: %s has no per-run turn cap; max_turns=%d is not enforced — the run's %ds timeout is the bound",
		spec.RunID, provider, spec.MaxTurns, spec.TimeoutSec)
}

// refuseRepoAccess is the refusal codex-cli and gemini-cli share for an agent
// that carries repository access.
//
// Both CLIs express "what may this run touch?" as one whole-workspace switch:
// codex's --sandbox, gemini's --approval-mode. Neither can say "may edit the
// clone, through these tools, and nothing else" — which is exactly what a
// repo-access agent is for. The two available answers are therefore to run it
// confined, where its edits silently fail (it is untrusted: a cloned
// repository's files are content nobody in the workspace wrote), or to run it
// unconfined, which hands a repo's contents the run of the machine. Refusing
// says so out loud instead, at Start, before the CLI is launched.
//
// It is not retryable: nothing about the run changes on a second attempt.
// claude-code is unaffected — its allowlist names Edit/Write/Bash(...) one at
// a time, so the middle ground exists there.
//
// This is now the last of three refusals, not the first: the definition cannot
// be saved that way (agents.Definition.Validate) and the worker refuses the
// run before it clones anything. It stays because an adapter is reached by
// paths that never went through either — a definition written straight to disk
// before this rule existed, a caller building a RunSpec itself — and because a
// refusal here costs nothing when the earlier two did their job. All three say
// the same sentence (agents.RepoAccessUnsupported).
func refuseRepoAccess(provider string, spec RunSpec) error {
	if !spec.RepoAccess {
		return nil
	}
	return agentPolicyError(agents.RepoAccessUnsupported(provider).Error())
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
