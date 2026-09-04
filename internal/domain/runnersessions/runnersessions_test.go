package runnersessions

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeRepo is an in-memory Repository good enough to exercise the lease
// lifecycle without a database.
type fakeRepo struct {
	nodes    map[string]*Node
	sessions map[string]*Session
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{nodes: map[string]*Node{}, sessions: map[string]*Session{}}
}

func (r *fakeRepo) SaveNode(n *Node) error {
	copied := *n
	r.nodes[n.ID] = &copied
	return nil
}

func (r *fakeRepo) FindNodeByID(id string) (*Node, error) {
	n, ok := r.nodes[id]
	if !ok {
		return nil, nil
	}
	copied := *n
	return &copied, nil
}

func (r *fakeRepo) FindNodeByName(pool, name string) (*Node, error) {
	for _, n := range r.nodes {
		if n.Pool == pool && n.Name == name {
			copied := *n
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeRepo) TouchNode(id string, at time.Time) (*Node, error) {
	n, ok := r.nodes[id]
	if !ok {
		return nil, nil
	}
	n.LastSeenAt = at
	copied := *n
	return &copied, nil
}

func (r *fakeRepo) SetNodeStatus(id, status string, sessionID *string) error {
	n, ok := r.nodes[id]
	if !ok {
		return errors.New("no such node")
	}
	n.Status = status
	n.SessionID = sessionID
	return nil
}

func (r *fakeRepo) ListNodes(pool string) ([]*Node, error) {
	var out []*Node
	for _, n := range r.nodes {
		if n.Pool == pool {
			copied := *n
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeRepo) MarkStaleNodesOffline(before time.Time) ([]*Node, error) {
	var out []*Node
	for _, n := range r.nodes {
		if n.LastSeenAt.Before(before) && n.Status != NodeOffline {
			n.Status = NodeOffline
			copied := *n
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeRepo) SaveSession(s *Session) error {
	copied := *s
	r.sessions[s.ID] = &copied
	return nil
}

func (r *fakeRepo) UpdateSession(s *Session) error {
	copied := *s
	r.sessions[s.ID] = &copied
	return nil
}

func (r *fakeRepo) FindSessionByID(id string) (*Session, error) {
	s, ok := r.sessions[id]
	if !ok {
		return nil, nil
	}
	copied := *s
	return &copied, nil
}

func (r *fakeRepo) FindLiveSessionForUser(orgID, userID string) (*Session, error) {
	for _, s := range r.sessions {
		if s.OrgID == orgID && s.UserID == userID && s.Live() {
			copied := *s
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeRepo) LeaseIdleNode(pool, sessionID string, seenSince time.Time) (*Node, error) {
	for _, n := range r.nodes {
		if n.Pool == pool && n.Status == NodeIdle && n.SessionID == nil && n.LastSeenAt.After(seenSince) {
			id := sessionID
			n.Status = NodeLeased
			n.SessionID = &id
			copied := *n
			return &copied, nil
		}
	}
	return nil, nil
}

func (r *fakeRepo) TouchSession(sessionID string, at time.Time) error {
	if s, ok := r.sessions[sessionID]; ok && s.Live() {
		s.LastActivityAt = at
	}
	return nil
}

func (r *fakeRepo) ListLapsedSessions(now time.Time) ([]*Session, error) {
	var out []*Session
	for _, s := range r.sessions {
		if !s.Live() {
			continue
		}
		lapsed := !now.Before(s.ExpiresAt) ||
			!now.Before(s.IdleDeadline()) ||
			(s.Status == StatusStarting && !now.Before(s.StartedAt.Add(StartTimeout)))
		if lapsed {
			copied := *s
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListLiveSessions(orgID string) ([]*Session, error) {
	var out []*Session
	for _, s := range r.sessions {
		if s.OrgID == orgID && s.Live() {
			copied := *s
			out = append(out, &copied)
		}
	}
	return out, nil
}

// fakeMinter records the credentials handed out and revoked.
type fakeMinter struct {
	minted  map[string]string // keyID -> plaintext
	revoked map[string]bool
}

func newFakeMinter() *fakeMinter {
	return &fakeMinter{minted: map[string]string{}, revoked: map[string]bool{}}
}

func (m *fakeMinter) MintSessionKey(orgID, userID, sessionID, name string) (string, string, error) {
	keyID := uuid.New().String()
	plaintext := "key-" + sessionID
	m.minted[keyID] = plaintext
	return keyID, plaintext, nil
}

func (m *fakeMinter) RevokeSessionKey(orgID, keyID string) error {
	m.revoked[keyID] = true
	return nil
}

// idleNode registers a node that is heartbeating now.
func idleNode(t *testing.T, svc *DefaultService, name string) *Node {
	t.Helper()
	node, err := svc.RegisterNode("", name, []string{"claude-code"})
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	return node
}

func newService() (*DefaultService, *fakeRepo, *fakeMinter) {
	repo, minter := newFakeRepo(), newFakeMinter()
	return NewDefaultService(repo, minter), repo, minter
}

// A lease takes an idle node, mints a credential, and hands that credential
// to the node exactly once — on the heartbeat that picks the lease up.
func TestStartLeasesNodeAndHandsKeyOverOnce(t *testing.T) {
	svc, _, minter := newService()
	node := idleNode(t, svc, "node-a")

	session, err := svc.Start("org", "user", 60, 15)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if session.NodeID != node.ID {
		t.Fatalf("session took node %q, want %q", session.NodeID, node.ID)
	}
	if session.Status != StatusStarting {
		t.Fatalf("new session status = %q, want %q", session.Status, StatusStarting)
	}

	_, assignment, err := svc.Heartbeat(node.ID)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if assignment == nil || assignment.SessionID != session.ID {
		t.Fatalf("first heartbeat returned no assignment for the lease")
	}
	if assignment.WorkerKey != minter.minted[*session.WorkerKeyID] {
		t.Fatalf("assignment carried key %q, want the minted plaintext", assignment.WorkerKey)
	}

	_, second, err := svc.Heartbeat(node.ID)
	if err != nil {
		t.Fatalf("second Heartbeat: %v", err)
	}
	if second == nil {
		t.Fatal("the lease disappeared on the second heartbeat")
	}
	if second.WorkerKey != "" {
		t.Errorf("the credential was handed over twice (%q); it must be issued once", second.WorkerKey)
	}
}

// A member holds one lease at a time: clicking start again returns the lease
// they already have rather than taking a second node out of the pool.
func TestStartIsIdempotentPerMember(t *testing.T) {
	svc, _, _ := newService()
	idleNode(t, svc, "node-a")
	idleNode(t, svc, "node-b")

	first, err := svc.Start("org", "user", 60, 15)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	second, err := svc.Start("org", "user", 60, 15)
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("second Start leased a new node (%s vs %s)", second.ID, first.ID)
	}
	counts, _ := svc.Counts(DefaultPool)
	if counts.Idle != 1 {
		t.Errorf("idle nodes = %d, want 1 (only one node should be leased)", counts.Idle)
	}
}

// Two members leasing from a one-node pool: the second is told there is
// nothing free rather than sharing a node.
func TestStartWithNoFreeNodes(t *testing.T) {
	svc, _, _ := newService()
	idleNode(t, svc, "node-a")

	if _, err := svc.Start("org", "user-1", 60, 15); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := svc.Start("org", "user-2", 60, 15); !errors.Is(err, ErrNoNodes) {
		t.Fatalf("second Start error = %v, want ErrNoNodes", err)
	}
}

// Ending a lease revokes its credential and sends the node to draining —
// which is what makes the next session start with a fresh sign-in.
func TestEndRevokesCredentialAndDrainsNode(t *testing.T) {
	svc, repo, minter := newService()
	node := idleNode(t, svc, "node-a")
	session, _ := svc.Start("org", "user", 60, 15)

	ended, err := svc.End(session.ID, EndReasonUser)
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	if ended.Status != StatusEnded || ended.EndReason != EndReasonUser {
		t.Errorf("ended session = %q/%q, want ended/user", ended.Status, ended.EndReason)
	}
	if !minter.revoked[*session.WorkerKeyID] {
		t.Error("the session credential was not revoked")
	}
	if repo.nodes[node.ID].Status != NodeDraining {
		t.Errorf("node status = %q, want %q", repo.nodes[node.ID].Status, NodeDraining)
	}

	// The node learns the lease is over by seeing no assignment.
	_, assignment, err := svc.Heartbeat(node.ID)
	if err != nil {
		t.Fatalf("Heartbeat after End: %v", err)
	}
	if assignment != nil {
		t.Error("a heartbeat after the lease ended still returned an assignment")
	}

	if err := svc.ReleaseNode(node.ID, session.ID); err != nil {
		t.Fatalf("ReleaseNode: %v", err)
	}
	if repo.nodes[node.ID].Status != NodeIdle || repo.nodes[node.ID].SessionID != nil {
		t.Error("a released node did not go back to the idle pool")
	}
}

// A late release from a previous lease must not free a node that has since
// been leased to somebody else.
func TestReleaseNodeIgnoresStaleSession(t *testing.T) {
	svc, repo, _ := newService()
	node := idleNode(t, svc, "node-a")
	first, _ := svc.Start("org", "user-1", 60, 15)
	if _, err := svc.End(first.ID, EndReasonUser); err != nil {
		t.Fatalf("End: %v", err)
	}
	if err := svc.ReleaseNode(node.ID, first.ID); err != nil {
		t.Fatalf("ReleaseNode: %v", err)
	}
	second, err := svc.Start("org", "user-2", 60, 15)
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}

	// The old lease's release arrives late.
	if err := svc.ReleaseNode(node.ID, first.ID); err != nil {
		t.Fatalf("stale ReleaseNode: %v", err)
	}
	if repo.nodes[node.ID].Status != NodeLeased {
		t.Errorf("node status = %q, want leased: a stale release freed a live lease", repo.nodes[node.ID].Status)
	}
	if repo.nodes[node.ID].SessionID == nil || *repo.nodes[node.ID].SessionID != second.ID {
		t.Error("the stale release detached the node from its current lease")
	}
}

// The sweep is what "times out after use" means: a lease past its hard
// expiry, and one that has simply gone unused, both end — with reasons that
// tell them apart.
func TestSweepEndsExpiredAndIdleLeases(t *testing.T) {
	svc, repo, minter := newService()
	idleNode(t, svc, "node-a")
	idleNode(t, svc, "node-b")

	expired, _ := svc.Start("org", "user-1", 60, 15)
	idle, _ := svc.Start("org", "user-2", 60, 15)

	// Make one lease's hard expiry pass, and let the other go quiet.
	now := time.Now()
	repo.sessions[expired.ID].Status = StatusActive
	repo.sessions[expired.ID].ExpiresAt = now.Add(-time.Minute)
	repo.sessions[expired.ID].LastActivityAt = now
	repo.sessions[idle.ID].Status = StatusActive
	repo.sessions[idle.ID].LastActivityAt = now.Add(-16 * time.Minute)

	ended, err := svc.Sweep(now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(ended) != 2 {
		t.Fatalf("sweep ended %d leases, want 2", len(ended))
	}
	reasons := map[string]string{}
	for _, s := range ended {
		reasons[s.ID] = s.EndReason
	}
	if reasons[expired.ID] != EndReasonExpired {
		t.Errorf("expired lease reason = %q, want %q", reasons[expired.ID], EndReasonExpired)
	}
	if reasons[idle.ID] != EndReasonIdle {
		t.Errorf("idle lease reason = %q, want %q", reasons[idle.ID], EndReasonIdle)
	}
	for _, s := range []*Session{expired, idle} {
		if !minter.revoked[*s.WorkerKeyID] {
			t.Errorf("lease %s kept a live credential after the sweep", s.ID)
		}
	}
}

// Activity resets the idle clock: a member mid-task does not lose their
// runner just because the window elapsed since they started.
func TestTouchDefersIdleTimeout(t *testing.T) {
	svc, repo, _ := newService()
	idleNode(t, svc, "node-a")
	session, _ := svc.Start("org", "user", 60, 15)
	repo.sessions[session.ID].Status = StatusActive
	repo.sessions[session.ID].LastActivityAt = time.Now().Add(-16 * time.Minute)

	if err := svc.Touch(session.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	ended, err := svc.Sweep(time.Now())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(ended) != 0 {
		t.Errorf("sweep ended %d leases after activity, want 0", len(ended))
	}
}

// A node that stops heartbeating takes its lease with it: the member's
// credential is revoked rather than left live on a machine nobody can see.
func TestSweepEndsLeasesOnLostNodes(t *testing.T) {
	svc, repo, minter := newService()
	node := idleNode(t, svc, "node-a")
	session, _ := svc.Start("org", "user", 60, 15)
	repo.sessions[session.ID].Status = StatusActive
	repo.nodes[node.ID].LastSeenAt = time.Now().Add(-2 * NodeOfflineAfter)

	ended, err := svc.Sweep(time.Now())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(ended) != 1 || ended[0].EndReason != EndReasonNodeLost {
		t.Fatalf("sweep ended %d leases (reason %v), want 1 node_lost", len(ended), ended)
	}
	if !minter.revoked[*session.WorkerKeyID] {
		t.Error("a lost node's credential was left live")
	}
}

// A lease whose node never collects it must not pin that node forever.
func TestSweepEndsLeasesStuckStarting(t *testing.T) {
	svc, repo, _ := newService()
	idleNode(t, svc, "node-a")
	session, _ := svc.Start("org", "user", 60, 15)
	repo.sessions[session.ID].StartedAt = time.Now().Add(-2 * StartTimeout)

	ended, err := svc.Sweep(time.Now())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(ended) != 1 || ended[0].EndReason != EndReasonNodeLost {
		t.Fatalf("a lease stuck in starting was not reclaimed: %v", ended)
	}
}

// Extending is capped from the original start, so repeated extensions cannot
// hold a pool node indefinitely.
func TestExtendIsCappedFromStart(t *testing.T) {
	svc, repo, _ := newService()
	idleNode(t, svc, "node-a")
	session, _ := svc.Start("org", "user", 60, 15)
	repo.sessions[session.ID].StartedAt = time.Now().Add(-(MaxSessionMinutes - 10) * time.Minute)

	extended, err := svc.Extend(session.ID, 60)
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if remaining := time.Until(extended.ExpiresAt); remaining > 11*time.Minute {
		t.Errorf("extend granted %v, want no more than the cap allows (~10m)", remaining)
	}
}

// A restarted node reclaims its own row rather than leaving a phantom behind
// in the pool.
func TestRegisterNodeReclaimsItsRow(t *testing.T) {
	svc, repo, _ := newService()
	first := idleNode(t, svc, "node-a")
	svc.Start("org", "user", 60, 15)

	second, err := svc.RegisterNode("", "node-a", []string{"claude-code"})
	if err != nil {
		t.Fatalf("re-RegisterNode: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("restarted node registered as %s, want its existing row %s", second.ID, first.ID)
	}
	if len(repo.nodes) != 1 {
		t.Errorf("pool holds %d nodes, want 1", len(repo.nodes))
	}
	if repo.nodes[first.ID].Status != NodeIdle || repo.nodes[first.ID].SessionID != nil {
		t.Error("a restarted node did not come back to the pool free")
	}
}

// Deadline is the earlier of the two clocks — the moment the runner actually
// goes away, which is what the UI counts down to.
func TestDeadlineIsTheEarlierClock(t *testing.T) {
	now := time.Now()
	session := &Session{
		StartedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
		LastActivityAt: now,
		IdleMinutes:    15,
	}
	if got, want := session.Deadline(), now.Add(15*time.Minute); !got.Equal(want) {
		t.Errorf("Deadline = %v, want the idle deadline %v", got, want)
	}
	session.LastActivityAt = now.Add(50 * time.Minute)
	if got := session.Deadline(); !got.Equal(session.ExpiresAt) {
		t.Errorf("Deadline = %v, want the hard expiry %v", got, session.ExpiresAt)
	}
}
