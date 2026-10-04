// A run's lifecycle as DefaultService drives it: launch and retry, claim
// and release, running, logs and heartbeats, finish with its auto-retry,
// cancel, the stale reaper, and the resolution of a run awaiting approval.

package agentruns

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/users"
)

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
	// Optional over-budget soft-block (off by default). Checked before any
	// side effect so a rejected launch mints no token and enqueues no run.
	if s.budgetGuard != nil {
		if blocked, reason := s.budgetGuard(req.OrgID); blocked {
			return nil, "", fmt.Errorf("%w: %s", ErrBudgetExceeded, reason)
		}
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
		RetriedFromRunID:   req.RetriedFromRunID,
		Status:             StatusQueued,
		Priority:           req.Priority,
		Prompt:             req.Prompt,
		RunTokenHash:       users.HashToken(token),
		ArtifactsTouched:   []map[string]interface{}{},
		LaunchedBy:         req.LaunchedBy,
		AttemptCount:       req.AttemptCount,
		MaxAttempts:        req.MaxAttempts,
		NextAttemptAt:      req.NextAttemptAt,
		CreatedAt:          time.Now().UTC(),
		// Reproducibility snapshot (issue #216): pin the agent identity as it is
		// right now. A later edit to the agent definition (or a retry, which is a
		// fresh Launch) does not rewrite an existing run's snapshot.
		AgentContentHash: agent.ContentHash,
		AgentModel:       agent.Model,
		AgentEffort:      agent.Effort,
	}
	// Seed the attempt chain: the original launch is attempt 1, capped at the
	// service's configured maximum unless the caller (an auto-retry) supplied
	// its own values.
	if run.AttemptCount <= 0 {
		run.AttemptCount = 1
	}
	if run.MaxAttempts <= 0 {
		run.MaxAttempts = s.maxAttempts
		if run.MaxAttempts <= 0 {
			run.MaxAttempts = DefaultMaxAttempts
		}
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
		hostedAfter := time.Now().UTC().Add(time.Duration(grace) * time.Second)
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

// retryableStatuses are the terminal states Retry accepts. Succeeded (and
// awaiting_approval) runs are deliberately excluded: re-running work that
// already landed invites duplicate side effects.
var retryableStatuses = map[string]bool{
	StatusFailed:    true,
	StatusCancelled: true,
	StatusTimedOut:  true,
}

// Retry enqueues a fresh run copying the source run's org, agent, project and
// prompt. The new run is a plain manual launch: normal priority, launched by
// the retrying user (so their personal-runner reservation applies), with
// provenance recorded in RetriedFromRunID. Automation, crew, delegation,
// interview and kanban links are NOT copied — those flows own their own
// retry/follow-up semantics.
func (s *DefaultService) Retry(sourceRunID string, launchedBy *string) (*Run, error) {
	source, err := s.Get(sourceRunID)
	if err != nil {
		return nil, err
	}
	if !retryableStatuses[source.Status] {
		return nil, fmt.Errorf("%w: run is %s (only failed, cancelled, or timed_out runs can be retried)", ErrNotRetryable, source.Status)
	}
	run, _, err := s.Launch(LaunchRequest{
		OrgID:            source.OrgID,
		AgentID:          source.AgentID,
		ProjectID:        source.ProjectID,
		Prompt:           source.Prompt,
		LaunchedBy:       launchedBy,
		RetriedFromRunID: &source.ID,
	})
	return run, err
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
// state another actor moved the run into meanwhile. It revokes the run's
// token: the worker handing the run back, and the agent it started, no
// longer act for it, and the next claim issues a fresh token.
//
// A run whose cancel was requested is not queued again but ends cancelled
// (#379 bug 148), as its worker reporting it cancelled would have ended it,
// and like that report it publishes RunFinished.
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
		if run.Status == StatusCancelled {
			s.publishRunFinished(run)
		}
	}
	return nil
}

// ReissueToken mints a fresh token for a run a worker holds and stores its
// hash. A run whose cancel was requested, or that no worker holds any more,
// gets none: ErrInvalidTransition, and the run keeps its token revoked
// (#379 bug 151: a project's delete revoked the token of a run claimed a
// moment before, and the claim's reissue then wrote a working one).
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
	issued, err := s.repo.UpdateTokenHash(run.ID, run.RunTokenHash)
	if err != nil {
		return "", err
	}
	if !issued {
		return "", fmt.Errorf("%w: no token for a run asked to stop or no longer held", ErrInvalidTransition)
	}
	return token, nil
}

// MarkRunning transitions a claimed run to running. The transition is a
// conditional write (claimed -> running) so a run that a concurrent actor
// already moved — the stale reaper failing it, a cancel — is never
// resurrected to running from a stale read.
func (s *DefaultService) MarkRunning(id string) error {
	applied, err := s.repo.MarkRunning(id, time.Now().UTC())
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
func (s *DefaultService) AppendLogs(runID string, entries []LogEntry, partialText string) (*Run, error) {
	if len(entries) > 0 {
		if err := s.repo.AppendLogs(runID, entries); err != nil {
			return nil, err
		}
	}
	// The partial answer is stored before the heartbeat so a reader that sees
	// a fresh heartbeat never sees stale text. A run the reaper (or a cancel)
	// already finished keeps its empty partial: the write is conditional.
	partialStored := false
	if partialText != "" {
		applied, err := s.repo.UpdatePartialText(runID, TruncatePartial(partialText))
		if err != nil {
			return nil, err
		}
		partialStored = applied
	}
	if _, err := s.repo.Heartbeat(runID, time.Now().UTC()); err != nil {
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
	if partialStored {
		for _, sub := range s.subscribers {
			sub.RunPartialText(run, run.PartialText)
		}
	}
	return run, nil
}

// PartialTextLimit caps the stored/streamed partial answer. It exists so one
// runaway run cannot push megabytes through the log endpoint and the SSE
// fan-out; a real answer is orders of magnitude smaller (AnswerLengthRule).
const PartialTextLimit = 64 * 1024

// TruncatePartial cuts partial assistant text to PartialTextLimit bytes
// without splitting a UTF-8 rune, keeping the head: a chat bubble reads from
// its start, and the final text replaces it moments later anyway.
func TruncatePartial(text string) string {
	if len(text) <= PartialTextLimit {
		return text
	}
	cut := PartialTextLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// applyFailedError is the error a run awaiting approval is finalised failed
// with when one of its approved proposals failed to apply.
const applyFailedError = "one or more approved proposals failed to apply"

var terminalStatuses = map[string]bool{
	StatusSucceeded: true,
	StatusFailed:    true,
	StatusCancelled: true,
	StatusTimedOut:  true,
}

// Finish records a run's terminal state. Only a run a worker holds (claimed
// or running) finishes: a queued run has no worker whose result it could be.
func (s *DefaultService) Finish(id string, req FinishRequest) (*Run, error) {
	if !terminalStatuses[req.Status] {
		return nil, fmt.Errorf("%w: finish status %q", ErrInvalidTransition, req.Status)
	}
	run, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if run.Status != StatusClaimed && run.Status != StatusRunning {
		return nil, finishRefusal(run.Status, req.Status)
	}

	now := time.Now().UTC()
	run.Status = req.Status
	run.FinishedAt = &now
	run.ExitCode = req.ExitCode
	run.FinalText = req.FinalText
	// The answer is now final: the streaming bubble's text has no further
	// use, and leaving it behind would let a reader see a half-written reply
	// beside the finished one.
	run.PartialText = ""
	run.Error = req.Error
	run.ErrorClass = req.ErrorClass
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
	// run's credential stops authenticating. It keeps a cancel requested
	// since the read above and sets run.CancelRequested to the stored flag,
	// so the auto-retry below never relaunches a run someone asked to stop
	// as it finished (#379 bug 166: the flag read above was written back
	// over that cancel, and the retry ran).
	applied, err := s.repo.UpdateTerminal(run)
	if err != nil {
		return nil, err
	}
	if !applied {
		current, err := s.Get(id)
		if err != nil {
			return nil, fmt.Errorf("%w: run already finished", ErrInvalidTransition)
		}
		return nil, finishRefusal(current.Status, req.Status)
	}
	run.RunTokenHash = ""
	s.notifyStatus(run)
	s.publishRunFinished(run)
	s.maybeAutoRetry(run)
	return run, nil
}

// finishRefusal is the transition error for a finish that a run in status
// from cannot take: one already finished says so, and one no worker holds
// (queued) names the transition, as MarkRunning's refusal does.
func finishRefusal(from, to string) error {
	if terminalStatuses[from] || from == StatusAwaitingApproval {
		return fmt.Errorf("%w: run already %s", ErrInvalidTransition, from)
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
}

// retryBackoffBase is the first retry's delay; each further attempt doubles it
// (attempt 2 waits base, attempt 3 waits 2*base, ...), capped at
// retryBackoffMax. The delay is written to next_attempt_at so the re-enqueued
// run is not claimable until it elapses.
const (
	retryBackoffBase = 30 * time.Second
	retryBackoffMax  = 10 * time.Minute
)

// retryBackoff returns the delay before the given attempt number becomes
// claimable (attempt is the NEW run's AttemptCount, i.e. 2 for the first
// retry).
func retryBackoff(attempt int) time.Duration {
	// attempt 2 -> base, 3 -> 2*base, 4 -> 4*base, ...
	shift := attempt - 2
	if shift < 0 {
		shift = 0
	}
	d := retryBackoffBase
	for i := 0; i < shift && d < retryBackoffMax; i++ {
		d *= 2
	}
	if d > retryBackoffMax {
		d = retryBackoffMax
	}
	return d
}

// maybeAutoRetry re-enqueues a fresh attempt when a terminal failure is
// retryable and the run's attempt budget is not exhausted. Best effort: a
// re-enqueue failure is logged-by-return only (the caller's terminal report
// already succeeded), never surfaced as a Finish error.
func (s *DefaultService) maybeAutoRetry(run *Run) {
	if !s.autoRetry || s.agentService == nil {
		return
	}
	// Only genuine failures retry; succeeded/cancelled/awaiting_approval do not.
	if run.Status != StatusFailed && run.Status != StatusTimedOut {
		return
	}
	// Orchestrated runs never auto-retry. A run carrying a parent/interview/
	// automation/guided linkage is owned by that orchestrator: its failure is
	// already delivered (via RunFinished) to the parent run, interview session,
	// automation, or guided session, which owns the retry/follow-up decision.
	// A blind auto-retry here would (a) strip those links — Launch copies only
	// the fields listed below, orphaning the new run from its tree/session — and
	// (b) keep the elevated child/interview Priority, so the orphan jumps the
	// queue and then has its result discarded because nothing is listening for
	// it. Only top-level, user-launched runs auto-retry.
	if run.ParentRunID != nil || run.InterviewSessionID != nil || run.AutomationID != nil || run.GuidedSessionID != nil {
		return
	}
	// A run asked to stop is not started again, however it ended: someone
	// cancelled it, or a project's delete did, after which a retry ran with
	// no project (#379 bug 147).
	if run.CancelRequested {
		return
	}
	if !IsRetryableClass(run.ErrorClass) {
		return
	}
	if run.AttemptCount >= run.MaxAttempts {
		return
	}
	next := time.Now().UTC().Add(retryBackoff(run.AttemptCount + 1))
	_, _, _ = s.Launch(LaunchRequest{
		OrgID:            run.OrgID,
		AgentID:          run.AgentID,
		ProjectID:        run.ProjectID,
		Prompt:           run.Prompt,
		LaunchedBy:       run.LaunchedBy,
		RetriedFromRunID: &run.ID,
		Priority:         run.Priority,
		AttemptCount:     run.AttemptCount + 1,
		MaxAttempts:      run.MaxAttempts,
		NextAttemptAt:    &next,
	})
}

// RequestCancel flags a run for cancellation (immediate if still queued).
// The repository writes the cancel the run's status asks for when the write
// lands, not when this read it: a queued run is cancelled at once, a
// claimed or running run has its cancel requested, for its worker to stop
// it. So a concurrent worker claim is never overwritten, and a concurrent
// release never drops the cancel (#379 bug 165: a run read as claimed that
// its worker handed back meanwhile went back to the queue, its cancel
// matched nothing, and the next claim started it again). A finished run,
// or one awaiting approval, is answered as it is.
func (s *DefaultService) RequestCancel(id string) (*Run, error) {
	run, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if terminalStatuses[run.Status] || run.Status == StatusAwaitingApproval {
		return run, nil
	}
	written, err := s.repo.RequestCancel(id)
	if err != nil {
		return nil, err
	}
	run, err = s.Get(id)
	if err != nil {
		return nil, err
	}
	if written {
		s.notifyStatus(run)
	}
	return run, nil
}

// AnnounceCancelled announces runs cancelled outside this service, each in
// the state the cancel left it: a queued run, or one awaiting approval,
// cancelled; a claimed or running one still live with its cancel requested. RequestCancel announces both the
// same way, and publishes no RunFinished for either: a cancelled queued run
// never had a worker to finish it, and a live one finishes when its worker
// reports it cancelled.
func (s *DefaultService) AnnounceCancelled(ids []string) {
	for _, id := range ids {
		if run, err := s.Get(id); err == nil && run != nil {
			s.notifyStatus(run)
		}
	}
}

// Heartbeat refreshes a run's liveness timestamp. A heartbeat for a run that
// is no longer live (already terminal) is silently dropped.
func (s *DefaultService) Heartbeat(id string) error {
	_, err := s.repo.Heartbeat(id, time.Now().UTC())
	return err
}

// FailStale fails runs whose worker went silent.
func (s *DefaultService) FailStale(maxSilence time.Duration) ([]string, error) {
	ids, err := s.repo.FailStale(time.Now().UTC().Add(-maxSilence))
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if run, err := s.Get(id); err == nil {
			s.notifyStatus(run)
			// The reaper's terminal write bypasses Finish, so publish
			// RunFinished here too — otherwise the notifier and automation
			// triggers miss reaper-failed runs (the common worker-crash case).
			s.publishRunFinished(run)
			// A lost worker is a worker_error (retryable): re-enqueue a fresh
			// attempt so a run isn't lost just because its runner crashed.
			s.maybeAutoRetry(run)
		}
	}
	return ids, nil
}

// FinalizeIfResolved completes an awaiting_approval run once every proposal it
// produced has been reviewed. It is a no-op unless the run is awaiting approval
// and no proposal is still pending; otherwise it moves the run to a terminal
// state — failed if any approved proposal failed to apply, succeeded when they
// all landed (or were rejected) cleanly — and publishes RunFinished so crew
// successors, the notifier and automation triggers fire on the real outcome
// instead of the run stalling forever in awaiting_approval. Wired to every
// proposal-resolution path (approve/reject, single or bulk, including the
// applier path where an approval's write fails).
func (s *DefaultService) FinalizeIfResolved(runID string) (*Run, error) {
	run, err := s.Get(runID)
	if err != nil {
		return nil, err
	}
	if run.Status != StatusAwaitingApproval {
		return run, nil
	}
	pending, err := s.repo.CountPendingProposals(runID)
	if err != nil {
		return nil, err
	}
	if pending > 0 {
		return run, nil
	}

	status := StatusSucceeded
	failed, err := s.repo.CountApplyFailedProposals(runID)
	if err != nil {
		return nil, err
	}
	if failed > 0 {
		status = StatusFailed
	}

	// A failed run keeps the reason it failed, stored as a worker's failure
	// is, so that it reads back as its status broadcast told it. Its class
	// is agent_error, which is not retried: the agent's approved writes did
	// not apply, and a second attempt proposes them again (OpenV REQ-84).
	if status == StatusFailed {
		if run.Error == "" {
			run.Error = applyFailedError
		}
		run.ErrorClass = ErrorClassAgentError
	}
	now := time.Now().UTC()
	applied, err := s.repo.FinalizeApproval(runID, status, run.Error, run.ErrorClass, now)
	if err != nil {
		return nil, err
	}
	if !applied {
		// Lost the race: a concurrent resolver already finalized the run.
		// Return its current state without re-notifying or re-publishing.
		return s.Get(runID)
	}
	run.Status = status
	run.FinishedAt = &now
	run.RunTokenHash = ""
	s.notifyStatus(run)
	s.publishRunFinished(run)
	return run, nil
}
