// Package runnersessions implements transient runners: a member leases one
// process from a pool of pre-warmed, always-on worker nodes for a bounded
// stretch of time, signs their vendor CLIs into it from the browser, and gets
// it wiped and handed back when the lease ends.
//
// The pool exists because the platform cannot always create containers on
// demand — a Railway deployment has no Docker daemon — and because a
// pre-warmed node is ready in seconds where provisioning is minutes. Nodes
// are org-agnostic until they are leased; a lease binds one node to one
// member of one workspace, and nothing of that member survives the wipe.
package runnersessions

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Node statuses.
const (
	// NodeIdle: registered, heartbeating, available to lease.
	NodeIdle = "idle"
	// NodeLeased: bound to a session.
	NodeLeased = "leased"
	// NodeDraining: the lease ended and the node is wiping its session home
	// before it reports itself idle again.
	NodeDraining = "draining"
	// NodeOffline: stopped heartbeating; not leasable.
	NodeOffline = "offline"
)

// Session statuses.
const (
	// StatusStarting: the node has been reserved but has not yet picked the
	// assignment up on a heartbeat.
	StatusStarting = "starting"
	// StatusActive: the node is running the member's runner.
	StatusActive = "active"
	// StatusEnding: the lease is over and the node is wiping.
	StatusEnding = "ending"
	// StatusEnded: wiped (or the node was lost); the credential is revoked.
	StatusEnded = "ended"
)

// End reasons, recorded on the session so the UI can say why a runner went
// away rather than just that it did.
const (
	EndReasonExpired  = "expired"   // hit its maximum lifetime
	EndReasonIdle     = "idle"      // no run activity within the idle window
	EndReasonUser     = "user"      // the member ended it
	EndReasonNodeLost = "node_lost" // the node stopped heartbeating
	EndReasonReplaced = "replaced"  // superseded by a newer session
)

// Lease timings. Both are org-tunable (orgs.LimitRunnerSessionMinutes /
// LimitRunnerSessionIdleMinutes); these are the fallbacks.
const (
	// DefaultSessionMinutes is a lease's maximum lifetime.
	DefaultSessionMinutes = 60
	// DefaultIdleMinutes is how long a lease may go without run activity
	// before it is reclaimed. "Times out after use" is this window: the
	// runner goes away once the member stops using it, not while they are
	// mid-task.
	DefaultIdleMinutes = 15
	// MaxSessionMinutes caps what an org limit may ask for, so a bad limits
	// row cannot pin a pool node forever.
	MaxSessionMinutes = 480
)

// NodeOfflineAfter is how long a node may go without a heartbeat before the
// sweep marks it offline and ends whatever it was running. Nodes heartbeat
// every NodeHeartbeatInterval.
const (
	NodeHeartbeatInterval = 5 * time.Second
	NodeOfflineAfter      = 45 * time.Second
)

// StartTimeout bounds how long a session may sit in "starting" waiting for
// its node to pick the assignment up. Past it the session is ended and the
// member can try another node.
const StartTimeout = 60 * time.Second

var (
	ErrNotFound     = errors.New("runner session not found")
	ErrNoNodes      = errors.New("no transient runner is free right now")
	ErrNodeNotFound = errors.New("runner pool node not found")
)

// Node is one pre-warmed worker process in the pool. A node registers itself
// at startup with the deployment's pool key and heartbeats from then on; it
// holds no workspace identity of its own.
type Node struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Pool labels the group a node belongs to, so a deployment can run more
	// than one pool (say, a bigger-memory tier) later. Empty is "default".
	Pool string `json:"pool"`
	// Providers are the vendor CLIs found on the node at startup. A node
	// with none installed can still be leased — the member simply has
	// nothing to sign in to.
	Providers []string `json:"providers"`
	Status    string   `json:"status"`
	// SessionID is the lease the node is serving, if any.
	SessionID  *string   `json:"session_id,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Online reports whether the node has heartbeated recently enough to be
// trusted with a lease.
func (n *Node) Online(now time.Time) bool {
	return now.Sub(n.LastSeenAt) < NodeOfflineAfter
}

// Session is one member's lease over a pool node.
type Session struct {
	ID     string `json:"id"`
	OrgID  string `json:"org_id"`
	UserID string `json:"user_id"`
	NodeID string `json:"node_id"`
	// WorkerKeyID is the session-scoped personal runner key minted for the
	// lease. It is revoked the moment the session ends, which is what makes
	// the member sign in again next time.
	WorkerKeyID    *string    `json:"worker_key_id,omitempty"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	EndReason      string     `json:"end_reason,omitempty"`
	// IdleMinutes is the window this lease was created with, so the UI can
	// show it without re-reading org limits.
	IdleMinutes int `json:"idle_minutes"`
	// NodeName is denormalized for display.
	NodeName string `json:"node_name,omitempty"`
}

// Live reports whether the session is still holding its node.
func (s *Session) Live() bool {
	return s.Status == StatusStarting || s.Status == StatusActive
}

// IdleDeadline is when the lease lapses for inactivity.
func (s *Session) IdleDeadline() time.Time {
	idle := s.IdleMinutes
	if idle <= 0 {
		idle = DefaultIdleMinutes
	}
	return s.LastActivityAt.Add(time.Duration(idle) * time.Minute)
}

// Deadline is the earlier of the hard expiry and the idle deadline — the
// moment the runner actually goes away.
func (s *Session) Deadline() time.Time {
	if d := s.IdleDeadline(); d.Before(s.ExpiresAt) {
		return d
	}
	return s.ExpiresAt
}

// Assignment is what a node learns on the heartbeat that hands it a lease.
// WorkerKey is the plaintext session credential and is returned exactly once,
// on the heartbeat that moves the session out of "starting".
type Assignment struct {
	SessionID string    `json:"session_id"`
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name,omitempty"`
	WorkerKey string    `json:"worker_key,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Repository defines pool and lease persistence. Find methods return
// (nil, nil) when no row matches.
type Repository interface {
	SaveNode(n *Node) error
	FindNodeByID(id string) (*Node, error)
	FindNodeByName(pool, name string) (*Node, error)
	// TouchNode records a heartbeat and returns the node's current row, so
	// the caller sees an assignment made since the last beat.
	TouchNode(id string, at time.Time) (*Node, error)
	// SetNodeStatus moves a node between pool states. sessionID nil clears
	// the node's lease.
	SetNodeStatus(id, status string, sessionID *string) error
	ListNodes(pool string) ([]*Node, error)
	// MarkStaleNodesOffline flags nodes that stopped heartbeating and
	// returns them, so their sessions can be ended.
	MarkStaleNodesOffline(before time.Time) ([]*Node, error)

	SaveSession(s *Session) error
	UpdateSession(s *Session) error
	FindSessionByID(id string) (*Session, error)
	// FindLiveSessionForUser returns the member's current lease, or nil.
	FindLiveSessionForUser(orgID, userID string) (*Session, error)
	// LeaseIdleNode atomically reserves one idle, recently-seen node for a
	// session and returns it; (nil, nil) when the pool has nothing free.
	// Atomicity is what stops two members leasing the same node.
	LeaseIdleNode(pool, sessionID string, seenSince time.Time) (*Node, error)
	// TouchSession bumps last_activity_at on a live lease. Silent no-op for
	// a lease that has already ended.
	TouchSession(sessionID string, at time.Time) error
	// ListLapsedSessions returns live sessions past their hard expiry or
	// their idle window, plus ones stuck in "starting".
	ListLapsedSessions(now time.Time) ([]*Session, error)
	ListLiveSessions(orgID string) ([]*Session, error)
}

// PoolCounts summarizes a pool for the UI: whether leasing one now would
// succeed, and how many members are already out there.
type PoolCounts struct {
	Total  int `json:"total"`
	Idle   int `json:"idle"`
	Leased int `json:"leased"`
}

// Service defines transient-runner logic.
type Service interface {
	// RegisterNode records (or re-registers) a pool node and returns it. A
	// node that restarts under the same name reclaims its row rather than
	// leaking a second one.
	RegisterNode(pool, name string, providers []string) (*Node, error)
	// Heartbeat records a beat and returns the node's assignment, if any.
	// The plaintext worker key is present only on the first beat that sees
	// the assignment.
	Heartbeat(nodeID string) (*Node, *Assignment, error)
	// ReleaseNode is the node's confirmation that it finished wiping. The
	// node returns to the idle pool.
	ReleaseNode(nodeID, sessionID string) error

	// Start leases a node for the member. An existing live session is
	// returned as-is rather than leasing a second node.
	Start(orgID, userID string, sessionMinutes, idleMinutes int) (*Session, error)
	// Get returns the member's live session, or nil.
	Get(orgID, userID string) (*Session, error)
	// Extend pushes the hard expiry out by minutes from now and clears the
	// idle clock, capped at MaxSessionMinutes from the original start.
	Extend(sessionID string, minutes int) (*Session, error)
	// End terminates a lease: the node is told to wipe and the session's
	// credential is revoked.
	End(sessionID, reason string) (*Session, error)
	// Touch records run activity against a live lease, resetting its idle
	// clock.
	Touch(sessionID string) error
	// Sweep ends lapsed sessions and offline nodes. Returns the sessions it
	// ended, so the caller can revoke their keys.
	Sweep(now time.Time) ([]*Session, error)
	// Counts summarizes the pool.
	Counts(pool string) (PoolCounts, error)
	// ListLive returns a workspace's current leases.
	ListLive(orgID string) ([]*Session, error)
}

// KeyMinter mints and revokes the session-scoped runner credential. It is
// satisfied by the worker key service; the indirection keeps this package
// free of a dependency on it.
type KeyMinter interface {
	// MintSessionKey creates a personal runner key bound to a session,
	// returning the key id and its plaintext. It must not disturb the
	// member's own (connector) personal key.
	MintSessionKey(orgID, userID, sessionID, name string) (keyID string, plaintext string, err error)
	// RevokeSessionKey revokes a key minted by MintSessionKey.
	RevokeSessionKey(orgID, keyID string) error
}

// DefaultPool is the pool label used when a node does not name one.
const DefaultPool = "default"

// DefaultService implements Service.
type DefaultService struct {
	repo   Repository
	minter KeyMinter
	// pending holds each starting session's plaintext credential until its
	// node collects it on a heartbeat. It never reaches the database: the
	// key is stored hashed like every other worker key, and a lease whose
	// node never arrives simply expires with the plaintext discarded.
	pending map[string]string
}

// NewDefaultService creates the transient runner service.
func NewDefaultService(repo Repository, minter KeyMinter) *DefaultService {
	return &DefaultService{repo: repo, minter: minter, pending: map[string]string{}}
}

func normalizePool(pool string) string {
	pool = strings.TrimSpace(pool)
	if pool == "" {
		return DefaultPool
	}
	return pool
}

// RegisterNode records or refreshes a node's registration.
func (s *DefaultService) RegisterNode(pool, name string, providers []string) (*Node, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("node name is required")
	}
	pool = normalizePool(pool)
	now := time.Now()

	// A restarted node reclaims its row: same pool and name, fresh state.
	// Whatever session it was serving is gone with the process, so the row
	// comes back idle and the sweep ends the orphaned lease.
	existing, err := s.repo.FindNodeByName(pool, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existing.Providers = providers
		existing.Status = NodeIdle
		existing.SessionID = nil
		existing.LastSeenAt = now
		if err := s.repo.SaveNode(existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	node := &Node{
		ID:         uuid.New().String(),
		Name:       name,
		Pool:       pool,
		Providers:  providers,
		Status:     NodeIdle,
		LastSeenAt: now,
		CreatedAt:  now,
	}
	if err := s.repo.SaveNode(node); err != nil {
		return nil, err
	}
	return node, nil
}

// Heartbeat records a beat and hands over any pending assignment.
func (s *DefaultService) Heartbeat(nodeID string) (*Node, *Assignment, error) {
	node, err := s.repo.TouchNode(nodeID, time.Now())
	if err != nil {
		return nil, nil, err
	}
	if node == nil {
		return nil, nil, ErrNodeNotFound
	}
	if node.SessionID == nil {
		return node, nil, nil
	}
	session, err := s.repo.FindSessionByID(*node.SessionID)
	if err != nil {
		return nil, nil, err
	}
	if session == nil || !session.Live() {
		// The lease went away underneath the node (ended or swept): tell it
		// to stop by reporting no assignment.
		return node, nil, nil
	}

	assignment := &Assignment{
		SessionID: session.ID,
		OrgID:     session.OrgID,
		UserID:    session.UserID,
		ExpiresAt: session.Deadline(),
	}
	// The plaintext is handed over exactly once, on the beat that promotes
	// the session to active.
	if session.Status == StatusStarting {
		assignment.WorkerKey = s.pending[session.ID]
		delete(s.pending, session.ID)
		session.Status = StatusActive
		session.LastActivityAt = time.Now()
		if err := s.repo.UpdateSession(session); err != nil {
			return nil, nil, err
		}
	}
	return node, assignment, nil
}

// ReleaseNode returns a wiped node to the idle pool.
func (s *DefaultService) ReleaseNode(nodeID, sessionID string) error {
	node, err := s.repo.FindNodeByID(nodeID)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}
	// Only release the lease the node actually reports finishing, so a late
	// release can never free a node that has since been leased again.
	if node.SessionID != nil && sessionID != "" && *node.SessionID != sessionID {
		return nil
	}
	return s.repo.SetNodeStatus(nodeID, NodeIdle, nil)
}

// Start leases a pool node for the member.
func (s *DefaultService) Start(orgID, userID string, sessionMinutes, idleMinutes int) (*Session, error) {
	if orgID == "" || userID == "" {
		return nil, errors.New("workspace and user are required")
	}
	if sessionMinutes <= 0 {
		sessionMinutes = DefaultSessionMinutes
	}
	if sessionMinutes > MaxSessionMinutes {
		sessionMinutes = MaxSessionMinutes
	}
	if idleMinutes <= 0 {
		idleMinutes = DefaultIdleMinutes
	}

	// One live lease per member: a second click returns the first session.
	if existing, err := s.repo.FindLiveSessionForUser(orgID, userID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	now := time.Now()
	session := &Session{
		ID:             uuid.New().String(),
		OrgID:          orgID,
		UserID:         userID,
		Status:         StatusStarting,
		StartedAt:      now,
		ExpiresAt:      now.Add(time.Duration(sessionMinutes) * time.Minute),
		LastActivityAt: now,
		IdleMinutes:    idleMinutes,
	}

	node, err := s.repo.LeaseIdleNode(DefaultPool, session.ID, now.Add(-NodeOfflineAfter))
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, ErrNoNodes
	}
	session.NodeID = node.ID
	session.NodeName = node.Name

	keyID, plaintext, err := s.minter.MintSessionKey(orgID, userID, session.ID, "cloud runner")
	if err != nil {
		_ = s.repo.SetNodeStatus(node.ID, NodeIdle, nil)
		return nil, err
	}
	session.WorkerKeyID = &keyID

	if err := s.repo.SaveSession(session); err != nil {
		_ = s.minter.RevokeSessionKey(orgID, keyID)
		_ = s.repo.SetNodeStatus(node.ID, NodeIdle, nil)
		return nil, err
	}
	s.pending[session.ID] = plaintext
	return session, nil
}

// Get returns the member's live session, or nil.
func (s *DefaultService) Get(orgID, userID string) (*Session, error) {
	return s.repo.FindLiveSessionForUser(orgID, userID)
}

// Extend pushes a lease's deadlines out.
func (s *DefaultService) Extend(sessionID string, minutes int) (*Session, error) {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrNotFound
	}
	if !session.Live() {
		return nil, errors.New("this runner session has already ended")
	}
	if minutes <= 0 {
		minutes = DefaultSessionMinutes
	}
	now := time.Now()
	// The cap is measured from the original start, so extending repeatedly
	// cannot hold a pool node indefinitely.
	hardCap := session.StartedAt.Add(MaxSessionMinutes * time.Minute)
	expires := now.Add(time.Duration(minutes) * time.Minute)
	if expires.After(hardCap) {
		expires = hardCap
	}
	session.ExpiresAt = expires
	session.LastActivityAt = now
	if err := s.repo.UpdateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// End terminates a lease and revokes its credential.
func (s *DefaultService) End(sessionID, reason string) (*Session, error) {
	session, err := s.repo.FindSessionByID(sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrNotFound
	}
	if !session.Live() {
		return session, nil
	}
	return s.end(session, reason)
}

// end is the shared teardown: revoke first (so the runner loses the API the
// moment the lease is over, even if the node never wipes), then mark the
// session ending and the node draining.
func (s *DefaultService) end(session *Session, reason string) (*Session, error) {
	delete(s.pending, session.ID)
	if session.WorkerKeyID != nil {
		if err := s.minter.RevokeSessionKey(session.OrgID, *session.WorkerKeyID); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	session.Status = StatusEnded
	session.EndedAt = &now
	session.EndReason = reason
	if err := s.repo.UpdateSession(session); err != nil {
		return nil, err
	}
	// The node sees the assignment disappear on its next heartbeat, wipes,
	// and reports itself released.
	if session.NodeID != "" {
		id := session.ID
		if err := s.repo.SetNodeStatus(session.NodeID, NodeDraining, &id); err != nil {
			return nil, err
		}
	}
	return session, nil
}

// Touch records activity against a live lease.
func (s *DefaultService) Touch(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return s.repo.TouchSession(sessionID, time.Now())
}

// ListLive returns a workspace's current leases.
func (s *DefaultService) ListLive(orgID string) ([]*Session, error) {
	return s.repo.ListLiveSessions(orgID)
}

// Sweep ends every lapsed lease and every session whose node went away.
func (s *DefaultService) Sweep(now time.Time) ([]*Session, error) {
	var ended []*Session

	lapsed, err := s.repo.ListLapsedSessions(now)
	if err != nil {
		return nil, err
	}
	for _, session := range lapsed {
		reason := EndReasonIdle
		switch {
		case !now.Before(session.ExpiresAt):
			reason = EndReasonExpired
		case session.Status == StatusStarting:
			reason = EndReasonNodeLost
		}
		done, err := s.end(session, reason)
		if err != nil {
			return ended, err
		}
		ended = append(ended, done)
	}

	stale, err := s.repo.MarkStaleNodesOffline(now.Add(-NodeOfflineAfter))
	if err != nil {
		return ended, err
	}
	for _, node := range stale {
		if node.SessionID == nil {
			continue
		}
		session, err := s.repo.FindSessionByID(*node.SessionID)
		if err != nil {
			return ended, err
		}
		if session == nil || !session.Live() {
			continue
		}
		done, err := s.end(session, EndReasonNodeLost)
		if err != nil {
			return ended, err
		}
		ended = append(ended, done)
	}
	return ended, nil
}

// Counts summarizes a pool's live nodes.
func (s *DefaultService) Counts(pool string) (PoolCounts, error) {
	nodes, err := s.repo.ListNodes(normalizePool(pool))
	if err != nil {
		return PoolCounts{}, err
	}
	now := time.Now()
	var counts PoolCounts
	for _, n := range nodes {
		if !n.Online(now) {
			continue
		}
		counts.Total++
		switch n.Status {
		case NodeIdle:
			counts.Idle++
		case NodeLeased, NodeDraining:
			counts.Leased++
		}
	}
	return counts, nil
}
