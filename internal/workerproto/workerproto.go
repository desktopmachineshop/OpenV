// Package workerproto holds the bodies of the worker wire: what a runner
// (agentd) and the API send each other to claim, start, log, finish and
// release a run, to report the provider CLIs a runner found, to relay a CLI
// sign-in, and to serve a cloud runner pool (refactor plan P3). It is a
// types-only leaf, importing no package of this module (K7), so the
// domain, the API and the runner can all name these types: agentruns
// keeps FinishRequest and LogEntry as aliases of them, and the runner
// RunAuth, PoolNode and PoolAssignment.
//
// A body that replaced a map literal declares its fields in the map's key
// order, which encoding/json sorts, so it writes the map's bytes (R10). A
// body that carries a domain value the leaf cannot name, such as the run
// and agent of a claim or a pool node's row, holds it as any, as the map
// did. The domain values the API answers whole, such as a finished run, a
// provider sign-in request or a registered pool node, stay the domain's
// types. Starting a run sends no body and answers none.
package workerproto

import "time"

// --- Claim: POST /api/v1/agent-runs/claim ---

// ClaimRequest is the body a runner claims a run with: the CLIs it has, the
// lowest priority it takes now, and whether it is a hosted runner.
type ClaimRequest struct {
	Hosted      bool     `json:"hosted"`
	MinPriority int      `json:"min_priority"`
	Providers   []string `json:"providers"`
	WorkerID    string   `json:"worker_id"`
}

// ClaimResponse is what the API answers a claim that got a run: the run's
// agentruns.Run and agents.Agent, the run's token, and the credential mode
// resolved for it, a map holding "mode" and, for an API key, "api_key_env".
// The auth key is always written, with no omitempty (I12); the runner reads
// it as a RunAuth.
type ClaimResponse struct {
	Agent    any    `json:"agent"`
	Auth     any    `json:"auth"`
	Run      any    `json:"run"`
	RunToken string `json:"run_token"`
}

// RunAuth is the credential mode the API resolved for a claimed run.
type RunAuth struct {
	// Mode is "user-account" (use the host's CLI sign-in, the default) or
	// "api-key" (inject the key named by APIKeyEnv from the host env).
	Mode      string `json:"mode"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// --- Logs: POST /api/v1/agent-runs/{id}/logs ---

// LogEntry is one streamed event from a run.
type LogEntry struct {
	RunID     string                 `json:"run_id"`
	Seq       int                    `json:"seq"`
	Kind      string                 `json:"kind"`
	Payload   map[string]interface{} `json:"payload"`
	CreatedAt time.Time              `json:"created_at"`
}

// LogsRequest is the log push: a batch of entries, which is also the run's
// heartbeat, and the assistant's whole answer so far, not a delta; an empty
// PartialText leaves the answer the API holds as it is.
type LogsRequest struct {
	Entries     []LogEntry `json:"entries"`
	PartialText string     `json:"partial_text"`
}

// LegacyLogsRequest is the log push's earlier body, the bare array of
// entries, which runners built before streaming still send, and which a
// runner falls back to when an older API refuses a LogsRequest.
type LegacyLogsRequest []LogEntry

// LogsResponse is what the API answers a log push: whether the run was
// asked to stop, and its status.
type LogsResponse struct {
	CancelRequested bool   `json:"cancel_requested"`
	Status          string `json:"status"`
}

// --- Finish: POST /api/v1/agent-runs/{id}/finish ---

// FinishRequest is the worker's terminal report for a run.
type FinishRequest struct {
	Status    string `json:"status"` // succeeded | failed | cancelled | timed_out
	ExitCode  *int   `json:"exit_code,omitempty"`
	FinalText string `json:"final_text"`
	Error     string `json:"error"`
	// ErrorClass is the runner's classification of a terminal failure (one of
	// the ErrorClass* constants); empty for a succeeded or cancelled run.
	ErrorClass string   `json:"error_class,omitempty"`
	TokensIn   int64    `json:"tokens_in"`
	TokensOut  int64    `json:"tokens_out"`
	CostUSD    *float64 `json:"cost_usd,omitempty"`
}

// --- Release: POST /api/v1/agent-runs/{id}/release ---

// ReleaseRequest hands a claimed or running run back to the queue; the API
// releases it only while this worker still holds it.
type ReleaseRequest struct {
	WorkerID string `json:"worker_id"`
}

// --- Detection: POST /api/v1/provider-settings/detect ---

// DetectionReport is what a runner found of each provider CLI, keyed by
// provider: "installed", "version", "logged_in" and "detail".
type DetectionReport map[string]map[string]interface{}

// --- Sign-ins: POST /api/v1/provider-logins/{id}/progress ---

// LoginProgress is a runner's report on a provider sign-in it is relaying.
type LoginProgress struct {
	AuthURL string `json:"auth_url"`
	Detail  string `json:"detail"`
	// PasteKind ("code" or "url") is what the worker is waiting for the
	// member to paste back; older workers omit it.
	PasteKind string `json:"paste_kind"`
	Status    string `json:"status"`
}

// --- Cloud runner pool: /api/v1/runner-pool/nodes ---

// PoolNodeRegistration announces a process as a pool node available to
// lease: POST /api/v1/runner-pool/nodes.
type PoolNodeRegistration struct {
	Name      string   `json:"name"`
	Pool      string   `json:"pool"`
	Providers []string `json:"providers"`
}

// PoolNode is a registered pool node as the API sees it.
type PoolNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Pool   string `json:"pool"`
	Status string `json:"status"`
}

// PoolAssignment is the lease a node has been handed. WorkerKey is present
// only on the heartbeat that picks the lease up.
type PoolAssignment struct {
	SessionID string    `json:"session_id"`
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	WorkerKey string    `json:"worker_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PoolHeartbeatRequest is a pool node's heartbeat body, an empty object:
// POST /api/v1/runner-pool/nodes/{id}/heartbeat.
type PoolHeartbeatRequest struct{}

// PoolHeartbeatResponse is what the API answers a heartbeat: the node's
// runnersessions.Node and its lease, a *runnersessions.Assignment, null when
// the node has none. The runner reads the lease as a PoolAssignment.
type PoolHeartbeatResponse struct {
	Assignment any `json:"assignment"`
	Node       any `json:"node"`
}

// PoolNodeRelease is a node's report that it has wiped a lease's state:
// POST /api/v1/runner-pool/nodes/{id}/release.
type PoolNodeRelease struct {
	SessionID string `json:"session_id"`
}
