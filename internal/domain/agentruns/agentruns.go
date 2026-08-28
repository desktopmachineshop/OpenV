package agentruns

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/users"
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

// Log entry kinds.
const (
	LogText       = "text"
	LogToolCall   = "tool_call"
	LogToolResult = "tool_result"
	LogUsage      = "usage"
	LogSystem     = "system"
	LogError      = "error"
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
)

// Run is one agent execution; the agent_runs table doubles as the job queue.
type Run struct {
	ID                 string                   `json:"id"`
	OrgID              string                   `json:"org_id"`
	AgentID            string                   `json:"agent_id"`
	ProjectID          *string                  `json:"project_id,omitempty"`
	AutomationID       *string                  `json:"automation_id,omitempty"`
	TriggerEventID     *string                  `json:"trigger_event_id,omitempty"`
	TeamID             *string                  `json:"team_id,omitempty"`
	TeamNodeID         *string                  `json:"team_node_id,omitempty"`
	ParentRunID        *string                  `json:"parent_run_id,omitempty"`
	WorkItemID         *string                  `json:"work_item_id,omitempty"`
	InterviewSessionID *string                  `json:"interview_session_id,omitempty"`
	GuidedSessionID    *string                  `json:"guided_session_id,omitempty"`
	Status             string                   `json:"status"`
	CancelRequested    bool                     `json:"cancel_requested"`
	Priority           int                      `json:"priority"`
	Prompt             string                   `json:"prompt"`
	RunTokenHash       string                   `json:"-"`
	WorkerID           string                   `json:"worker_id,omitempty"`
	HeartbeatAt        *time.Time               `json:"heartbeat_at,omitempty"`
	StartedAt          *time.Time               `json:"started_at,omitempty"`
	FinishedAt         *time.Time               `json:"finished_at,omitempty"`
	ExitCode           *int                     `json:"exit_code,omitempty"`
	FinalText          string                   `json:"final_text"`
	Error              string                   `json:"error"`
	TokensIn           int64                    `json:"tokens_in"`
	TokensOut          int64                    `json:"tokens_out"`
	CostUSD            *float64                 `json:"cost_usd,omitempty"`
	ArtifactsTouched   []map[string]interface{} `json:"artifacts_touched"`
	LaunchedBy         *string                  `json:"launched_by,omitempty"`
	// PreferredUserID reserves the run for the launcher's personal runner
	// until HostedAfter, when workspace/hosted runners may claim it.
	PreferredUserID *string    `json:"preferred_user_id,omitempty"`
	HostedAfter     *time.Time `json:"hosted_after,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`

	// Denormalized for display.
	AgentName     string `json:"agent_name,omitempty"`
	AgentProvider string `json:"agent_provider,omitempty"`
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
	Priority           int
	Prompt             string
	LaunchedBy         *string
}

// FinishRequest is the worker's terminal report for a run.
type FinishRequest struct {
	Status    string   `json:"status"` // succeeded | failed | cancelled | timed_out
	ExitCode  *int     `json:"exit_code,omitempty"`
	FinalText string   `json:"final_text"`
	Error     string   `json:"error"`
	TokensIn  int64    `json:"tokens_in"`
	TokensOut int64    `json:"tokens_out"`
	CostUSD   *float64 `json:"cost_usd,omitempty"`
}

// QueueStats summarizes an org's queued runs (worker-status endpoint).
type QueueStats struct {
	Queued              int `json:"queued"`
	OldestQueuedSeconds int `json:"oldest_queued_seconds"`
	QueuedRepoAccess    int `json:"queued_repo_access"`
}

// ListFilter filters run listings. OrgID and LaunchedBy scope the listing in
// SQL (before LIMIT applies) so one workspace's traffic can never starve
// another's page.
type ListFilter struct {
	OrgID      string
	AgentID    string
	ProjectID  string
	Status     string
	ParentID   string
	WorkItemID string
	LaunchedBy string
	Limit      int
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
	// runs launched by that user (personal runners); "" claims workspace
	// work: runs whose personal reservation is absent or expired.
	// excludeRepoAccess skips runs whose agent needs repo access.
	Claim(workerID string, orgID string, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*Run, error)
	// ReleaseClaim conditionally returns a claimed run to the queue, but only
	// while it is still claimed by workerID (claim handshake failed); reports
	// whether the release was applied.
	ReleaseClaim(runID, workerID string) (bool, error)
	// CancelQueued conditionally cancels a run only while it is still queued
	// (and revokes its token), so a concurrent worker claim is never stomped;
	// reports whether the cancel was applied.
	CancelQueued(id string) (bool, error)
	// SetCancelRequested flags a claimed/running run for cooperative
	// cancellation; reports whether the flag was applied.
	SetCancelRequested(id string) (bool, error)
	// UpdateTerminal writes a run's terminal result fields and revokes its run
	// token, but only while the stored status is still non-terminal; reports
	// whether the transition was applied.
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
	// column (in particular status).
	UpdateTokenHash(runID, hash string) error
	// FailStale marks claimed/running runs failed when their heartbeat is
	// older than cutoff; returns the affected run IDs.
	FailStale(cutoff time.Time) ([]string, error)
	AppendLogs(runID string, entries []LogEntry) error
	ListLogs(runID string, afterSeq int) ([]LogEntry, error)
	CountRunsSince(automationID string, since time.Time) (int, error)
	CountPendingProposals(runID string) (int, error)
	// QueueStats summarizes the org's queued runs.
	QueueStats(orgID string) (QueueStats, error)
}

// Service defines run lifecycle logic.
type Service interface {
	// Launch enqueues a run and returns it plus the raw run token
	// (shown once; only its hash is stored).
	Launch(req LaunchRequest) (*Run, string, error)
	Get(id string) (*Run, error)
	GetByToken(token string) (*Run, error)
	List(filter ListFilter) ([]*Run, error)
	Tree(rootID string) ([]*Run, error)
	Claim(workerID string, orgID string, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*Run, error)
	// ReleaseClaim returns a just-claimed run to the queue when the claim
	// handshake fails after Claim (agent lookup or token mint), so the run is
	// not stranded until the stale reaper. Only applies while the run is
	// still claimed by workerID.
	ReleaseClaim(runID, workerID string) error
	// AttachWorkItem links a run to the kanban card tracking it.
	AttachWorkItem(runID, workItemID string) error
	// ReissueToken mints a fresh run token (returned raw; hash stored).
	// Used at claim time to hand the worker a usable credential.
	ReissueToken(runID string) (string, error)
	MarkRunning(id string) error
	AppendLogs(runID string, entries []LogEntry) (*Run, error)
	Logs(runID string, afterSeq int) ([]LogEntry, error)
	Finish(id string, req FinishRequest) (*Run, error)
	RequestCancel(id string) (*Run, error)
	Heartbeat(id string) error
	FailStale(maxSilence time.Duration) ([]string, error)
	CountRunsSince(automationID string, since time.Time) (int, error)
	// QueueStats summarizes the org's queued runs.
	QueueStats(orgID string) (QueueStats, error)
}

// Subscriber is notified on run lifecycle changes and appended logs
// (used by the SSE hub and by team/kanban follow-up hooks).
type Subscriber interface {
	RunLogsAppended(run *Run, entries []LogEntry)
	RunStatusChanged(run *Run)
}

// DefaultGraceSeconds is how long a run waits for the launcher's personal
// runner before workspace/hosted runners may claim it.
const DefaultGraceSeconds = 60

// DefaultService implements Service.
type DefaultService struct {
	repo         Repository
	agentService agents.Service
	bus          events.Bus
	subscribers  []Subscriber

	// Routing policy (optional): hasPersonalRunner reports whether the
	// launcher has an online personal runner; graceSeconds resolves the
	// org's reservation window.
	hasPersonalRunner func(orgID, userID string) bool
	graceSeconds      func(orgID string) int
}

// NewDefaultService creates a run service.
func NewDefaultService(repo Repository, agentService agents.Service, bus events.Bus) *DefaultService {
	return &DefaultService{repo: repo, agentService: agentService, bus: bus}
}

// SetRoutingPolicy wires personal-runner first-refusal routing (call during
// wiring only).
func (s *DefaultService) SetRoutingPolicy(hasPersonalRunner func(orgID, userID string) bool, graceSeconds func(orgID string) int) {
	s.hasPersonalRunner = hasPersonalRunner
	s.graceSeconds = graceSeconds
}

// AddSubscriber registers a lifecycle subscriber (not concurrency-safe;
// call during wiring only).
func (s *DefaultService) AddSubscriber(sub Subscriber) {
	s.subscribers = append(s.subscribers, sub)
}

// Launch enqueues a run for the worker to claim.
func (s *DefaultService) Launch(req LaunchRequest) (*Run, string, error) {
	if req.OrgID == "" {
		return nil, "", errors.New("org_id is required: every run must belong to a workspace")
	}
	if req.AgentID == "" {
		return nil, "", errors.New("agent_id is required")
	}
	if req.Prompt == "" {
		return nil, "", errors.New("prompt is required")
	}
	agent, err := s.agentService.Get(req.AgentID)
	if err != nil {
		return nil, "", err
	}
	if agent == nil {
		return nil, "", agents.ErrNotFound
	}

	token, err := users.NewToken()
	if err != nil {
		return nil, "", err
	}

	run := &Run{
		ID:                 uuid.New().String(),
		OrgID:              req.OrgID,
		AgentID:            req.AgentID,
		ProjectID:          req.ProjectID,
		AutomationID:       req.AutomationID,
		TriggerEventID:     req.TriggerEventID,
		TeamID:             req.TeamID,
		TeamNodeID:         req.TeamNodeID,
		ParentRunID:        req.ParentRunID,
		WorkItemID:         req.WorkItemID,
		InterviewSessionID: req.InterviewSessionID,
		GuidedSessionID:    req.GuidedSessionID,
		Status:             StatusQueued,
		Priority:           req.Priority,
		Prompt:             req.Prompt,
		RunTokenHash:       users.HashToken(token),
		ArtifactsTouched:   []map[string]interface{}{},
		LaunchedBy:         req.LaunchedBy,
		CreatedAt:          time.Now(),
	}
	// First refusal: reserve the run for the launcher's personal runner
	// when one is online, with a grace window before hosted takeover.
	if req.LaunchedBy != nil && s.hasPersonalRunner != nil && s.hasPersonalRunner(run.OrgID, *req.LaunchedBy) {
		grace := DefaultGraceSeconds
		if s.graceSeconds != nil {
			if g := s.graceSeconds(run.OrgID); g > 0 {
				grace = g
			}
		}
		run.PreferredUserID = req.LaunchedBy
		hostedAfter := time.Now().Add(time.Duration(grace) * time.Second)
		run.HostedAfter = &hostedAfter
	}

	if err := s.repo.Save(run); err != nil {
		return nil, "", err
	}
	run.AgentName = agent.Name
	run.AgentProvider = agent.Provider
	s.notifyStatus(run)
	return run, token, nil
}

// AttachWorkItem links a run to the kanban card tracking it. The write is a
// targeted single-column update so it can never resurrect a status (or any
// other field) from a stale read.
func (s *DefaultService) AttachWorkItem(runID, workItemID string) error {
	return s.repo.UpdateWorkItemID(runID, workItemID)
}

// Get returns a run by id.
func (s *DefaultService) Get(id string) (*Run, error) {
	run, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrNotFound
	}
	return run, nil
}

// GetByToken resolves a raw run token to its run.
func (s *DefaultService) GetByToken(token string) (*Run, error) {
	run, err := s.repo.FindByTokenHash(users.HashToken(token))
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrNotFound
	}
	return run, nil
}

// List returns runs matching the filter.
func (s *DefaultService) List(filter ListFilter) ([]*Run, error) {
	return s.repo.List(filter)
}

// Tree returns the root run and all descendants (breadth-first).
func (s *DefaultService) Tree(rootID string) ([]*Run, error) {
	root, err := s.Get(rootID)
	if err != nil {
		return nil, err
	}
	result := []*Run{root}
	frontier := []string{rootID}
	for len(frontier) > 0 {
		next := []string{}
		for _, id := range frontier {
			children, err := s.repo.ListChildren(id)
			if err != nil {
				return nil, err
			}
			for _, c := range children {
				result = append(result, c)
				next = append(next, c.ID)
			}
		}
		frontier = next
	}
	return result, nil
}

// Claim hands the oldest matching queued run in the worker's org to a worker.
func (s *DefaultService) Claim(workerID string, orgID string, workerUserID string, providers []string, minPriority int, excludeRepoAccess bool) (*Run, error) {
	run, err := s.repo.Claim(workerID, orgID, workerUserID, providers, minPriority, excludeRepoAccess)
	if err != nil || run == nil {
		return run, err
	}
	s.notifyStatus(run)
	return run, nil
}

// ReleaseClaim returns a just-claimed run to the queue when the claim
// handshake fails, so the run isn't stranded until the stale reaper. The
// release is conditional (still claimed, same worker) so it can never undo a
// state another actor moved the run into meanwhile.
func (s *DefaultService) ReleaseClaim(runID, workerID string) error {
	released, err := s.repo.ReleaseClaim(runID, workerID)
	if err != nil {
		return err
	}
	if !released {
		return nil
	}
	if run, err := s.Get(runID); err == nil {
		s.notifyStatus(run)
	}
	return nil
}

// ReissueToken mints a fresh token for a run and stores its hash.
func (s *DefaultService) ReissueToken(runID string) (string, error) {
	run, err := s.Get(runID)
	if err != nil {
		return "", err
	}
	token, err := users.NewToken()
	if err != nil {
		return "", err
	}
	run.RunTokenHash = users.HashToken(token)
	if err := s.repo.UpdateTokenHash(run.ID, run.RunTokenHash); err != nil {
		return "", err
	}
	return token, nil
}

// MarkRunning transitions a claimed run to running. The transition is a
// conditional write (claimed -> running) so a run that a concurrent actor
// already moved — the stale reaper failing it, a cancel — is never
// resurrected to running from a stale read.
func (s *DefaultService) MarkRunning(id string) error {
	applied, err := s.repo.MarkRunning(id, time.Now())
	if err != nil {
		return err
	}
	if !applied {
		run, err := s.Get(id)
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: %s -> running", ErrInvalidTransition, run.Status)
	}
	if run, err := s.Get(id); err == nil {
		s.notifyStatus(run)
	}
	return nil
}

// AppendLogs persists a log batch, refreshes the heartbeat, and returns the
// current run so the worker sees cancel_requested. The heartbeat refresh is
// conditional on the run still being live, so a late log batch from a worker
// whose run was already failed (or cancelled) never refreshes heartbeat_at
// on a terminal run; the logs themselves are still kept.
func (s *DefaultService) AppendLogs(runID string, entries []LogEntry) (*Run, error) {
	if len(entries) > 0 {
		if err := s.repo.AppendLogs(runID, entries); err != nil {
			return nil, err
		}
	}
	if _, err := s.repo.Heartbeat(runID, time.Now()); err != nil {
		return nil, err
	}
	run, err := s.Get(runID)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 {
		for _, sub := range s.subscribers {
			sub.RunLogsAppended(run, entries)
		}
	}
	return run, nil
}

// Logs returns persisted log entries after a sequence number.
func (s *DefaultService) Logs(runID string, afterSeq int) ([]LogEntry, error) {
	return s.repo.ListLogs(runID, afterSeq)
}

var terminalStatuses = map[string]bool{
	StatusSucceeded: true,
	StatusFailed:    true,
	StatusCancelled: true,
	StatusTimedOut:  true,
}

// Finish records a run's terminal state.
func (s *DefaultService) Finish(id string, req FinishRequest) (*Run, error) {
	if !terminalStatuses[req.Status] {
		return nil, fmt.Errorf("%w: finish status %q", ErrInvalidTransition, req.Status)
	}
	run, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if terminalStatuses[run.Status] || run.Status == StatusAwaitingApproval {
		return nil, fmt.Errorf("%w: run already %s", ErrInvalidTransition, run.Status)
	}

	now := time.Now()
	run.Status = req.Status
	run.FinishedAt = &now
	run.ExitCode = req.ExitCode
	run.FinalText = req.FinalText
	run.Error = req.Error
	run.TokensIn = req.TokensIn
	run.TokensOut = req.TokensOut
	run.CostUSD = req.CostUSD

	// A successful run with pending proposals surfaces as awaiting_approval.
	if req.Status == StatusSucceeded {
		pending, err := s.repo.CountPendingProposals(id)
		if err == nil && pending > 0 {
			run.Status = StatusAwaitingApproval
		}
	}

	// The terminal write is conditional so a concurrent finisher (the stale
	// reaper, a duplicate worker report) can never overwrite an
	// already-terminal status; it also revokes the run token, so a finished
	// run's credential stops authenticating.
	applied, err := s.repo.UpdateTerminal(run)
	if err != nil {
		return nil, err
	}
	if !applied {
		current, err := s.Get(id)
		if err != nil {
			return nil, fmt.Errorf("%w: run already finished", ErrInvalidTransition)
		}
		return nil, fmt.Errorf("%w: run already %s", ErrInvalidTransition, current.Status)
	}
	run.RunTokenHash = ""
	s.notifyStatus(run)
	if s.bus != nil {
		projectID := ""
		if run.ProjectID != nil {
			projectID = *run.ProjectID
		}
		s.bus.Publish(events.New(events.RunFinished, projectID, run.ID, "agent:"+run.ID, map[string]interface{}{
			"status":   run.Status,
			"agent_id": run.AgentID,
		}).WithOrg(run.OrgID))
	}
	return run, nil
}

// RequestCancel flags a run for cancellation (immediate if still queued).
// Both paths are conditional state transitions in the repository, so a
// concurrent worker claim is never overwritten: a queued run cancels only
// while still queued, otherwise the cancel-requested flag is set only while
// the run is live (claimed/running).
func (s *DefaultService) RequestCancel(id string) (*Run, error) {
	run, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if terminalStatuses[run.Status] || run.Status == StatusAwaitingApproval {
		return run, nil
	}
	if run.Status == StatusQueued {
		cancelled, err := s.repo.CancelQueued(id)
		if err != nil {
			return nil, err
		}
		if cancelled {
			run, err = s.Get(id)
			if err != nil {
				return nil, err
			}
			s.notifyStatus(run)
			return run, nil
		}
		// Lost the race to a worker claim (or a finish): fall through to the
		// cooperative flag path against the run's current state.
	}
	flagged, err := s.repo.SetCancelRequested(id)
	if err != nil {
		return nil, err
	}
	run, err = s.Get(id)
	if err != nil {
		return nil, err
	}
	if flagged {
		s.notifyStatus(run)
	}
	return run, nil
}

// Heartbeat refreshes a run's liveness timestamp. A heartbeat for a run that
// is no longer live (already terminal) is silently dropped.
func (s *DefaultService) Heartbeat(id string) error {
	_, err := s.repo.Heartbeat(id, time.Now())
	return err
}

// FailStale fails runs whose worker went silent.
func (s *DefaultService) FailStale(maxSilence time.Duration) ([]string, error) {
	ids, err := s.repo.FailStale(time.Now().Add(-maxSilence))
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if run, err := s.Get(id); err == nil {
			s.notifyStatus(run)
		}
	}
	return ids, nil
}

// CountRunsSince counts an automation's runs in a window (rate guard).
func (s *DefaultService) CountRunsSince(automationID string, since time.Time) (int, error) {
	return s.repo.CountRunsSince(automationID, since)
}

// QueueStats summarizes the org's queued runs.
func (s *DefaultService) QueueStats(orgID string) (QueueStats, error) {
	return s.repo.QueueStats(orgID)
}

func (s *DefaultService) notifyStatus(run *Run) {
	for _, sub := range s.subscribers {
		sub.RunStatusChanged(run)
	}
}
