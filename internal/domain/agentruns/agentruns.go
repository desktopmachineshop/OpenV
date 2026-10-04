package agentruns

import (
	"errors"
	"fmt"
	"time"
)

// Run statuses.
const (
	StatusQueued           = "queued"
	StatusClaimed          = "claimed"
	StatusRunning          = "running"
	StatusSucceeded        = "succeeded"
	StatusFailed           = "failed"
	StatusCancelled        = "cancelled"
	StatusTimedOut         = "timed_out"
	StatusAwaitingApproval = "awaiting_approval"
)

// Priorities: child (delegated) and interview turn runs jump the queue.
const (
	PriorityNormal    = 0
	PriorityChild     = 10
	PriorityInterview = 20
)

// Error classes (issue #184): a structured taxonomy for terminal run
// failures, stored in agent_runs.error_class. The empty string means "no
// failure" (a succeeded or cancelled run). The runner classifies at every
// finish site; the reaper classifies heartbeat-timeout failures as
// worker_error.
const (
	// ErrorClassProviderUnavailable: the provider CLI/back end could not be
	// reached or refused for a transient reason (rate limit, overload, no
	// adapter, the CLI failing to launch). Retryable.
	ErrorClassProviderUnavailable = "provider_unavailable"
	// ErrorClassAuth: a credential problem (missing API key, sign-in expired,
	// 401/403 from the provider). NOT retryable — retrying cannot fix bad
	// credentials.
	ErrorClassAuth = "auth"
	// ErrorClassWorkspace: preparing the run's workspace failed (clone, disk).
	// NOT retryable by default — the same prep tends to fail the same way.
	ErrorClassWorkspace = "workspace"
	// ErrorClassTimeout: the run exceeded its deadline (adapter watchdog or
	// heartbeat timeout). Retryable.
	ErrorClassTimeout = "timeout"
	// ErrorClassAgentError: the agent itself failed — a non-zero CLI exit or an
	// error result the agent produced. NOT retryable — a deterministic agent
	// failure recurs.
	ErrorClassAgentError = "agent_error"
	// ErrorClassWorkerError: the runner/worker faulted (panic, lost worker,
	// an API transition the run could not complete through no fault of its
	// own). Retryable.
	ErrorClassWorkerError = "worker_error"
)

// retryableErrorClasses are the failure classes an auto-retry re-enqueues: a
// transient provider outage, a timeout, or a worker fault. auth, workspace and
// agent_error are deliberately excluded — retrying does not fix bad
// credentials, a broken workspace, or a deterministic agent failure.
var retryableErrorClasses = map[string]bool{
	ErrorClassProviderUnavailable: true,
	ErrorClassTimeout:             true,
	ErrorClassWorkerError:         true,
}

// IsRetryableClass reports whether a terminal failure of the given error class
// is eligible for a bounded auto-retry.
func IsRetryableClass(class string) bool { return retryableErrorClasses[class] }

// DefaultMaxAttempts is the default cap on how many times a run (the original
// plus auto-retries) is attempted before the failure stands. A value of 1
// disables auto-retry.
const DefaultMaxAttempts = 3

// Log entry kinds.
const (
	LogText       = "text"
	LogToolCall   = "tool_call"
	LogToolResult = "tool_result"
	LogUsage      = "usage"
	LogSystem     = "system"
	LogError      = "error"
	// LogMarker flags an operational marker injected by the worker itself
	// (e.g. a notice that streamed events were dropped), or a note the
	// server keeps on a run (NoteRun), as opposed to output parsed from the
	// agent CLI.
	LogMarker = "marker"
)

// MaxAnswerChars is the answer budget every agent is told about (see
// AnswerLengthRule) and the size everything that stores or forwards a run's
// final text — card activity, handoff prompts — must accept without cutting
// it off. Raise it here and both sides move together.
const MaxAnswerChars = 8000

// AnswerLengthRule is appended to every run's system prompt by the runner so
// agents keep final answers inside the budget the log windows are sized for.
var AnswerLengthRule = fmt.Sprintf(
	"\n\nAnswer length: keep your final answer under %d characters — it is logged to the run and its kanban card in full up to that limit and cut off beyond it. Put full detail into artifacts or card comments and keep the final answer a summary that fits the budget.",
	MaxAnswerChars,
)

// Truncate caps text at max bytes without splitting a UTF-8 character,
// marking the cut with an ellipsis.
func Truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut] + "…"
}

// TruncateAnswer caps text at MaxAnswerChars without splitting a UTF-8
// character, marking the cut with an ellipsis.
func TruncateAnswer(text string) string {
	return Truncate(text, MaxAnswerChars)
}

var (
	ErrNotFound          = errors.New("agent run not found")
	ErrInvalidTransition = errors.New("invalid run status transition")
	// ErrNotRetryable rejects retrying a run that is not in a retryable
	// terminal state (only failed, cancelled, and timed_out runs re-enqueue;
	// retrying a succeeded run would just invite duplicate side effects).
	ErrNotRetryable = errors.New("run is not retryable")
	// ErrBudgetExceeded rejects a launch when the workspace is over its
	// monthly spend budget AND soft-block enforcement is enabled (issue #186;
	// off by default — the shipped default is warn-only). API launch handlers
	// map it to a user-facing 402.
	ErrBudgetExceeded = errors.New("workspace monthly budget exceeded")
)

// Run is one agent execution; the agent_runs table doubles as the job queue.
type Run struct {
	ID                 string  `json:"id"`
	OrgID              string  `json:"org_id"`
	AgentID            string  `json:"agent_id"`
	ProjectID          *string `json:"project_id,omitempty"`
	AutomationID       *string `json:"automation_id,omitempty"`
	TriggerEventID     *string `json:"trigger_event_id,omitempty"`
	TeamID             *string `json:"team_id,omitempty"`
	TeamNodeID         *string `json:"team_node_id,omitempty"`
	ParentRunID        *string `json:"parent_run_id,omitempty"`
	WorkItemID         *string `json:"work_item_id,omitempty"`
	InterviewSessionID *string `json:"interview_session_id,omitempty"`
	GuidedSessionID    *string `json:"guided_session_id,omitempty"`
	// RetriedFromRunID records provenance: the terminal run this run was
	// re-enqueued from via Retry. Unlike ParentRunID it carries no queue or
	// run-tree semantics — a retry is a sibling, not a child.
	RetriedFromRunID *string    `json:"retried_from_run_id,omitempty"`
	Status           string     `json:"status"`
	CancelRequested  bool       `json:"cancel_requested"`
	Priority         int        `json:"priority"`
	Prompt           string     `json:"prompt"`
	RunTokenHash     string     `json:"-"`
	WorkerID         string     `json:"worker_id,omitempty"`
	HeartbeatAt      *time.Time `json:"heartbeat_at,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	ExitCode         *int       `json:"exit_code,omitempty"`
	FinalText        string     `json:"final_text"`
	// PartialText is the assistant text written so far by a run still in
	// flight — the whole text, not a delta, refreshed from the worker's log
	// batches and cleared when the run finishes (FinalText then holds the
	// answer). Empty for every run that is not currently streaming.
	PartialText string `json:"partial_text,omitempty"`
	Error       string `json:"error"`
	// ErrorClass is the structured failure taxonomy bucket for a terminal
	// failure (see the ErrorClass* constants); empty for a run that succeeded
	// or was cancelled.
	ErrorClass string `json:"error_class,omitempty"`
	// AttemptCount is this run's position in its attempt chain (1 for the
	// original launch, 2 for the first auto-retry, ...). MaxAttempts caps the
	// chain. NextAttemptAt gates when a backed-off retry becomes claimable.
	AttemptCount     int                      `json:"attempt_count,omitempty"`
	MaxAttempts      int                      `json:"max_attempts,omitempty"`
	NextAttemptAt    *time.Time               `json:"next_attempt_at,omitempty"`
	TokensIn         int64                    `json:"tokens_in"`
	TokensOut        int64                    `json:"tokens_out"`
	CostUSD          *float64                 `json:"cost_usd,omitempty"`
	ArtifactsTouched []map[string]interface{} `json:"artifacts_touched"`
	LaunchedBy       *string                  `json:"launched_by,omitempty"`
	// PreferredUserID reserves the run for the launcher's personal runner
	// until HostedAfter, when workspace/hosted runners may claim it.
	PreferredUserID *string    `json:"preferred_user_id,omitempty"`
	HostedAfter     *time.Time `json:"hosted_after,omitempty"`
	// ClaimedBy is the member whose personal runner key claimed the run: set
	// by the claim, cleared when a release returns the run to the queue, and
	// kept once the run ends. Nil for a run a workspace key claimed and for
	// one no runner has claimed since it was queued. The run's token reads
	// its project's repository connections with that member's local paths,
	// as the runner on that member's machine needs. Server-side only.
	ClaimedBy *string   `json:"-"`
	CreatedAt time.Time `json:"created_at"`

	// Reproducibility snapshot (issue #216): the agent identity this run was
	// launched with, captured once at Launch and never retro-filled. Because
	// the agent definition is mutable, these pin what actually executed — the
	// definition's content hash (a SHA-256 of the whole markdown file, so it
	// covers the exact prompt+config), the model, and the reasoning effort.
	// Blank on pre-feature rows, which were never snapshotted.
	AgentContentHash string `json:"agent_content_hash,omitempty"`
	AgentModel       string `json:"agent_model,omitempty"`
	AgentEffort      string `json:"agent_effort,omitempty"`

	// Denormalized for display.
	AgentName     string `json:"agent_name,omitempty"`
	AgentProvider string `json:"agent_provider,omitempty"`
}

// UntrustedOrigin reports whether this run's *origin* — where its prompt came
// from, rather than which agent is serving it — carries content authored
// outside the workspace.
//
// Today that is one thing: an interview turn. The prompt of an interview turn
// is the participant's own transcript, typed on a public invite link by
// someone who is not a member of the workspace. Trust is a property of that
// origin, not of a slug: an interview may be bound to any agent
// (`agent_slug` on create), so pinning "untrusted" to the seeded
// interviewer's slug would let a workspace hand its interviews to a
// repo-writing agent and quietly get an auto-approving run (REQ-91, HAZ-1).
//
// The flag travels with the queued run and reaches the runner in the claim
// payload — InterviewSessionID is persisted on agent_runs and serialized into
// the claim — so no separate column is needed for it to survive a restart or
// a hand-off to another runner. The worker ORs it with the signals derived
// from the agent definition (agents.Agent.UntrustedInput) to set
// RunSpec.Untrusted.
func (r *Run) UntrustedOrigin() bool {
	if r == nil {
		return false
	}
	return r.InterviewSessionID != nil
}

// LogEntry is one streamed event from a run.
type LogEntry struct {
	RunID     string                 `json:"run_id"`
	Seq       int                    `json:"seq"`
	Kind      string                 `json:"kind"`
	Payload   map[string]interface{} `json:"payload"`
	CreatedAt time.Time              `json:"created_at"`
}

// LaunchRequest describes a run to enqueue.
type LaunchRequest struct {
	OrgID              string
	AgentID            string
	ProjectID          *string
	AutomationID       *string
	TriggerEventID     *string
	TeamID             *string
	TeamNodeID         *string
	ParentRunID        *string
	WorkItemID         *string
	InterviewSessionID *string
	GuidedSessionID    *string
	RetriedFromRunID   *string
	Priority           int
	Prompt             string
	LaunchedBy         *string
	// AttemptCount / MaxAttempts seed the new run's attempt chain. Both default
	// (1 and the service's configured cap) when left zero — a plain manual
	// launch. Auto-retry sets AttemptCount to the source's + 1 and carries the
	// same MaxAttempts. NextAttemptAt, when set, delays the run's claim
	// eligibility (retry backoff).
	AttemptCount  int
	MaxAttempts   int
	NextAttemptAt *time.Time
}

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

// QueueStats summarizes an org's queued runs (worker-status endpoint).
type QueueStats struct {
	Queued              int `json:"queued"`
	OldestQueuedSeconds int `json:"oldest_queued_seconds"`
	QueuedRepoAccess    int `json:"queued_repo_access"`
}

// AgentUsage aggregates one agent's runs in a usage window.
type AgentUsage struct {
	AgentSlug string  `json:"agent_slug"`
	AgentName string  `json:"agent_name"`
	Runs      int     `json:"runs"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
}

// DailyUsage aggregates one calendar day's runs in a usage window.
type DailyUsage struct {
	Day       string  `json:"day"` // YYYY-MM-DD (UTC)
	Runs      int     `json:"runs"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
}

// UsageTotals sums a usage window.
type UsageTotals struct {
	Runs      int     `json:"runs"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
}

// UsageSummary is an org's run usage over a window: the same runs rolled up
// by agent and by day, plus grand totals. Runs are bucketed by created_at;
// token/cost columns are zero until a run finishes, so live runs count toward
// run totals but not spend.
type UsageSummary struct {
	Days    int          `json:"days"`
	Totals  UsageTotals  `json:"totals"`
	ByAgent []AgentUsage `json:"by_agent"`
	ByDay   []DailyUsage `json:"by_day"`
	// MonthToDateCostUSD is the current calendar month's spend (UTC),
	// independent of the trailing window above. It is what budget alerts and
	// the budget progress bar measure against the org's monthly budget.
	MonthToDateCostUSD float64 `json:"month_to_date_cost_usd"`
}

// ListFilter filters run listings. OrgID and LaunchedBy scope the listing in
// SQL (before LIMIT applies) so one workspace's traffic can never starve
// another's page. OrgID is MANDATORY and fails closed: an empty OrgID matches
// no rows, so every caller must supply the workspace being listed.
type ListFilter struct {
	OrgID      string
	AgentID    string
	ProjectID  string
	Status     string
	ParentID   string
	WorkItemID string
	LaunchedBy string
	// NoProject keeps only the runs with no project: a whole-workspace
	// automation's, one launched outside any project, and those a
	// project's delete left behind (the workspace Runs page). It only
	// narrows: who may see them is LaunchedBy's to say, as for any other
	// workspace-wide listing.
	NoProject bool
	Limit     int
}

// Repository defines persistence for runs and their logs.
type Repository interface {
	Save(r *Run) error
	FindByID(id string) (*Run, error)
	FindByTokenHash(hash string) (*Run, error)
	List(filter ListFilter) ([]*Run, error)
	ListChildren(parentRunID string) ([]*Run, error)
	// Claim atomically claims the oldest queued run (highest priority first)
	// in the worker's org whose agent's provider is in providers.
	// minPriority > 0 restricts the claim to priority >= minPriority
	// (dedicated child slots). workerUserID != "" restricts the claim to
	// runs launched by that user and the ownerless runs that user could see
	// (personal runners, which never take a run their user could not): a
	// workspace admin sees them all, anyone else those in a project they
	// hold a role in, and a run with no project is a workspace admin's
	// alone. "" claims workspace work: runs whose personal reservation is
	// absent or expired. excludeRepoAccess skips runs whose agent needs
	// repo access. The claimed run records workerUserID as ClaimedBy.
	Claim(workerID string, orgID string, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*Run, error)
	// ReleaseClaim conditionally returns a claimed run to the queue, but only
	// while it is still claimed by workerID (claim handshake failed), and
	// revokes its run token, so the departing worker's token stops
	// authenticating at once (the next claim issues a fresh one); a run
	// whose cancel was requested ends cancelled instead. Reports whether the
	// release was applied.
	ReleaseClaim(runID, workerID string) (bool, error)
	// RequestCancel cancels a run as its status when the write lands asks,
	// whatever a concurrent claim or release made of it since the caller
	// read it: a queued run is cancelled at once and its token revoked; a
	// claimed or running run has its cancel requested, for its worker to
	// stop it. A run in any other status is left as it is. Reports whether
	// it wrote.
	RequestCancel(id string) (bool, error)
	// UpdateTerminal writes a run's terminal result fields and revokes its run
	// token, but only while a worker still holds the run (claimed or
	// running); reports whether the transition was applied. A cancel
	// requested since r was read is kept, never cleared, and once applied
	// r.CancelRequested is the flag as stored.
	UpdateTerminal(r *Run) (bool, error)
	// MarkRunning conditionally transitions a run from claimed to running,
	// stamping started_at/heartbeat_at, so a run another actor moved on
	// (reaper failure, cancel) is never resurrected; reports whether the
	// transition was applied.
	MarkRunning(runID string, at time.Time) (bool, error)
	// Heartbeat refreshes liveness, but only while the run is still live
	// (claimed/running), so a late worker report can never refresh a terminal
	// run; reports whether the refresh was applied.
	Heartbeat(runID string, at time.Time) (bool, error)
	// UpdateWorkItemID links a run to its kanban card without touching any
	// other column (in particular status).
	UpdateWorkItemID(runID, workItemID string) error
	// UpdateTokenHash rotates a run's token hash without touching any other
	// column (in particular status), but only while a worker holds the run
	// and its cancel has not been requested, so a revoked token stays
	// revoked; reports whether it was applied.
	UpdateTokenHash(runID, hash string) (bool, error)
	// FailStale marks claimed/running runs failed when their heartbeat is
	// older than cutoff, or cancelled, with no error, when their cancel was
	// requested; returns the affected run IDs.
	FailStale(cutoff time.Time) ([]string, error)
	AppendLogs(runID string, entries []LogEntry) error
	// UpdatePartialText stores the assistant text a live run has written so
	// far, but only while the run is still claimed/running, so a late batch
	// can never resurrect a finished run's bubble; reports whether the write
	// was applied.
	UpdatePartialText(runID string, text string) (bool, error)
	ListLogs(runID string, afterSeq int) ([]LogEntry, error)
	// AppendNote writes one entry at the end of a run's log, numbered after
	// the last entry there, and returns it as stored: a note the server
	// keeps on a run (NoteRun), where AppendLogs stores what a worker
	// numbered itself.
	AppendNote(runID string, entry LogEntry) (LogEntry, error)
	CountRunsSince(automationID string, since time.Time) (int, error)
	CountPendingProposals(runID string) (int, error)
	// CountApplyFailedProposals counts a run's approved proposals whose write
	// failed to apply, so an awaiting_approval run can resolve to failed when
	// any of its writes did not land.
	CountApplyFailedProposals(runID string) (int, error)
	// FinalizeApproval transitions an awaiting_approval run to a terminal
	// status (succeeded/failed) once its proposals are resolved, storing
	// errMsg and errorClass as the run's error and error class, but only
	// while it is still awaiting approval so a concurrent resolver can never
	// double-finalize; reports whether the transition was applied.
	FinalizeApproval(runID, status, errMsg, errorClass string, at time.Time) (bool, error)
	// QueueStats summarizes the org's queued runs.
	QueueStats(orgID string) (QueueStats, error)
	// Usage aggregates an org's runs created at/after since, grouped by
	// agent slug and by day (see UsageSummary; Days/Totals are the
	// service's concern).
	Usage(orgID string, since time.Time) ([]AgentUsage, []DailyUsage, error)
	// MonthlySpend sums an org's cost_usd over runs created at/after
	// monthStart (the current-month total; NULL costs count as zero). It
	// shares the usage rollup's created_at bucketing so the figure matches
	// what the usage tab shows.
	MonthlySpend(orgID string, monthStart time.Time) (float64, error)
}

// Service defines run lifecycle logic.
type Service interface {
	// Launch enqueues a run and returns it plus the raw run token
	// (shown once; only its hash is stored).
	Launch(req LaunchRequest) (*Run, string, error)
	// Retry re-enqueues a NEW run with the source run's org, agent, project
	// and prompt, recording provenance in RetriedFromRunID. Only runs in a
	// retryable terminal state (failed, cancelled, timed_out) can be
	// retried; anything else answers ErrNotRetryable. launchedBy is the
	// retrying user (their personal-runner reservation applies), not the
	// source run's launcher.
	Retry(sourceRunID string, launchedBy *string) (*Run, error)
	Get(id string) (*Run, error)
	GetByToken(token string) (*Run, error)
	List(filter ListFilter) ([]*Run, error)
	Tree(rootID string) ([]*Run, error)
	Claim(workerID string, orgID string, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*Run, error)
	// ReleaseClaim returns a just-claimed run to the queue when the claim
	// handshake fails after Claim (agent lookup or token mint), so the run is
	// not stranded until the stale reaper. Only applies while the run is
	// still claimed by workerID. A run whose cancel was requested ends
	// cancelled instead.
	ReleaseClaim(runID, workerID string) error
	// AttachWorkItem links a run to the kanban card tracking it.
	AttachWorkItem(runID, workItemID string) error
	// ReissueToken mints a fresh run token (returned raw; hash stored).
	// Used at claim time to hand the worker a usable credential. A run whose
	// cancel was requested, or that no worker holds, gets none:
	// ErrInvalidTransition.
	ReissueToken(runID string) (string, error)
	MarkRunning(id string) error
	// AppendLogs persists a log batch and, when partialText is non-empty, the
	// assistant text written so far (see Run.PartialText). An empty
	// partialText means "unchanged" — the stored value is left alone.
	AppendLogs(runID string, entries []LogEntry, partialText string) (*Run, error)
	Logs(runID string, afterSeq int) ([]LogEntry, error)
	Finish(id string, req FinishRequest) (*Run, error)
	RequestCancel(id string) (*Run, error)
	// AnnounceCancelled tells the runs' subscribers of a cancel written
	// outside this service, as RequestCancel tells them of its own: the
	// runs a project's delete cancelled, or asked to stop, in its own
	// transaction (projects.Removed), one awaiting approval among them. An
	// id no run has is skipped.
	AnnounceCancelled(ids []string)
	Heartbeat(id string) error
	// FailStale ends the runs whose worker has been silent for maxSilence:
	// failed as worker lost, or cancelled when their cancel was requested.
	FailStale(maxSilence time.Duration) ([]string, error)
	// FinalizeIfResolved completes an awaiting_approval run once every proposal
	// it produced has been reviewed, publishing RunFinished on the transition.
	// A no-op for runs that are not awaiting approval or still have pending
	// proposals. Called wherever a proposal is resolved.
	FinalizeIfResolved(runID string) (*Run, error)
	// NoteRun keeps a note on a run: an entry of kind marker at the end of
	// its log, whose payload carries the note's marker, message and detail,
	// sent to the log's subscribers as a worker's batch is.
	NoteRun(runID, marker, message string, detail map[string]interface{}) error
	// SuccessorsSkipped records that a finished crew run's agent successors
	// were not launched because the workspace's budget refused them: a note
	// on the run and a RunSuccessorsSkipped event, each naming the skipped
	// successors and the budget.
	SuccessorsSkipped(run *Run, skipped []Successor, refusal error) error
	CountRunsSince(automationID string, since time.Time) (int, error)
	// QueueStats summarizes the org's queued runs.
	QueueStats(orgID string) (QueueStats, error)
	// Usage rolls up the org's run usage over the window starting at since.
	Usage(orgID string, since time.Time) (*UsageSummary, error)
	// MonthlySpend returns the org's month-to-date spend (SUM of cost_usd for
	// runs created at/after monthStart).
	MonthlySpend(orgID string, monthStart time.Time) (float64, error)
}

// DefaultGraceSeconds is how long a run waits for the launcher's personal
// runner before workspace/hosted runners may claim it.
const DefaultGraceSeconds = 60
