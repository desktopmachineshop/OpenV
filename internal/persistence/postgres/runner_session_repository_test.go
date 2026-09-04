package postgres

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/runnersessions"
)

// runnerPoolFixture is a database with one workspace and one member, ready to
// register pool nodes and lease them.
type runnerPoolFixture struct {
	db     *sql.DB
	repo   *RunnerSessionRepository
	orgID  string
	userID string
}

func newRunnerPoolFixture(t *testing.T) *runnerPoolFixture {
	t.Helper()
	db := testDB(t)
	initTestSchema(t, db)

	f := &runnerPoolFixture{
		db:     db,
		repo:   NewRunnerSessionRepository(db),
		orgID:  uuid.New().String(),
		userID: uuid.New().String(),
	}
	if _, err := db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Test Org', 'test-org')`, f.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, email, name, password_hash) VALUES ($1, 'member@example.com', 'Member', 'x')`, f.userID); err != nil {
		t.Fatal(err)
	}
	return f
}

// registerNode inserts an idle node heartbeating now.
func (f *runnerPoolFixture) registerNode(t *testing.T, name string) *runnersessions.Node {
	t.Helper()
	node := &runnersessions.Node{
		ID:         uuid.New().String(),
		Pool:       runnersessions.DefaultPool,
		Name:       name,
		Providers:  []string{"claude-code", "codex-cli"},
		Status:     runnersessions.NodeIdle,
		LastSeenAt: time.Now(),
		CreatedAt:  time.Now(),
	}
	if err := f.repo.SaveNode(node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	return node
}

// lease writes a session row for a node the caller already reserved.
func (f *runnerPoolFixture) lease(t *testing.T, sessionID, nodeID string, expiresAt time.Time, idleMinutes int) *runnersessions.Session {
	t.Helper()
	now := time.Now()
	session := &runnersessions.Session{
		ID:             sessionID,
		OrgID:          f.orgID,
		UserID:         f.userID,
		NodeID:         nodeID,
		Status:         runnersessions.StatusActive,
		StartedAt:      now,
		ExpiresAt:      expiresAt,
		LastActivityAt: now,
		IdleMinutes:    idleMinutes,
	}
	if err := f.repo.SaveSession(session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return session
}

// A node round-trips through the pool: registered, found by name, and its
// provider list survives the text encoding.
func TestRunnerPoolNodeRoundTrip(t *testing.T) {
	f := newRunnerPoolFixture(t)
	node := f.registerNode(t, "pool-1")

	byID, err := f.repo.FindNodeByID(node.ID)
	if err != nil || byID == nil {
		t.Fatalf("FindNodeByID = %v, %v", byID, err)
	}
	if len(byID.Providers) != 2 || byID.Providers[0] != "claude-code" {
		t.Errorf("providers = %v, want the two registered", byID.Providers)
	}

	byName, err := f.repo.FindNodeByName(runnersessions.DefaultPool, "pool-1")
	if err != nil || byName == nil || byName.ID != node.ID {
		t.Fatalf("FindNodeByName = %v, %v", byName, err)
	}
	if missing, err := f.repo.FindNodeByName(runnersessions.DefaultPool, "nope"); err != nil || missing != nil {
		t.Errorf("FindNodeByName for an unknown node = %v, %v; want nil, nil", missing, err)
	}
}

// The lease is a single atomic statement precisely so that two members
// clicking at the same moment take two different nodes — never the same one.
func TestLeaseIdleNodeIsAtomic(t *testing.T) {
	f := newRunnerPoolFixture(t)
	for i := 0; i < 4; i++ {
		f.registerNode(t, "pool-"+string(rune('a'+i)))
	}

	const contenders = 8
	var wg sync.WaitGroup
	results := make([]*runnersessions.Node, contenders)
	errs := make([]error, contenders)
	start := make(chan struct{})
	seenSince := time.Now().Add(-runnersessions.NodeOfflineAfter)

	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = f.repo.LeaseIdleNode(runnersessions.DefaultPool, uuid.New().String(), seenSince)
		}(i)
	}
	close(start)
	wg.Wait()

	claimed := map[string]int{}
	leased := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("LeaseIdleNode: %v", errs[i])
		}
		if results[i] == nil {
			continue
		}
		leased++
		claimed[results[i].ID]++
	}
	if leased != 4 {
		t.Errorf("leased %d nodes, want all 4 (and the other 4 callers to get nothing)", leased)
	}
	for id, times := range claimed {
		if times != 1 {
			t.Errorf("node %s was leased %d times; a node serves one member at a time", id, times)
		}
	}
}

// A node that is offline, already leased, or too stale to trust is not
// leasable.
func TestLeaseIdleNodeSkipsUnavailableNodes(t *testing.T) {
	f := newRunnerPoolFixture(t)
	stale := f.registerNode(t, "stale")
	if _, err := f.db.Exec(`UPDATE runner_pool_nodes SET last_seen_at = $2 WHERE id = $1`,
		stale.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	busy := f.registerNode(t, "busy")
	other := uuid.New().String()
	if err := f.repo.SetNodeStatus(busy.ID, runnersessions.NodeLeased, &other); err != nil {
		t.Fatal(err)
	}

	got, err := f.repo.LeaseIdleNode(runnersessions.DefaultPool, uuid.New().String(), time.Now().Add(-runnersessions.NodeOfflineAfter))
	if err != nil {
		t.Fatalf("LeaseIdleNode: %v", err)
	}
	if got != nil {
		t.Fatalf("leased node %q; both nodes should have been skipped", got.Name)
	}
}

// A heartbeat from a node marked offline brings it straight back into the
// pool, but never steals it out of a live lease.
func TestTouchNodeRevivesOnlyFreeNodes(t *testing.T) {
	f := newRunnerPoolFixture(t)
	free := f.registerNode(t, "free")
	held := f.registerNode(t, "held")
	sessionID := uuid.New().String()
	if err := f.repo.SetNodeStatus(free.ID, runnersessions.NodeOffline, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SetNodeStatus(held.ID, runnersessions.NodeOffline, &sessionID); err != nil {
		t.Fatal(err)
	}

	revived, err := f.repo.TouchNode(free.ID, time.Now())
	if err != nil || revived == nil {
		t.Fatalf("TouchNode = %v, %v", revived, err)
	}
	if revived.Status != runnersessions.NodeIdle {
		t.Errorf("free node came back as %q, want idle", revived.Status)
	}

	stillHeld, err := f.repo.TouchNode(held.ID, time.Now())
	if err != nil || stillHeld == nil {
		t.Fatalf("TouchNode = %v, %v", stillHeld, err)
	}
	if stillHeld.Status != runnersessions.NodeOffline || stillHeld.SessionID == nil {
		t.Errorf("a node mid-lease came back as %q (session %v); its lease must survive the beat",
			stillHeld.Status, stillHeld.SessionID)
	}

	if missing, err := f.repo.TouchNode(uuid.New().String(), time.Now()); err != nil || missing != nil {
		t.Errorf("TouchNode for an unknown node = %v, %v; want nil, nil so the node re-registers", missing, err)
	}
}

// The sweep query is the one that has to be right: a lease past its hard
// expiry, one past its idle window, and one stuck waiting for its node all
// come back — and a healthy one does not.
func TestListLapsedSessions(t *testing.T) {
	f := newRunnerPoolFixture(t)
	now := time.Now()

	healthy := f.lease(t, uuid.New().String(), f.registerNode(t, "n-healthy").ID, now.Add(time.Hour), 15)

	expired := f.lease(t, uuid.New().String(), f.registerNode(t, "n-expired").ID, now.Add(-time.Minute), 15)

	idle := f.lease(t, uuid.New().String(), f.registerNode(t, "n-idle").ID, now.Add(time.Hour), 15)
	if _, err := f.db.Exec(`UPDATE runner_sessions SET last_activity_at = $2 WHERE id = $1`,
		idle.ID, now.Add(-16*time.Minute)); err != nil {
		t.Fatal(err)
	}

	stuck := f.lease(t, uuid.New().String(), f.registerNode(t, "n-stuck").ID, now.Add(time.Hour), 15)
	if _, err := f.db.Exec(`UPDATE runner_sessions SET status = 'starting', started_at = $2 WHERE id = $1`,
		stuck.ID, now.Add(-2*runnersessions.StartTimeout)); err != nil {
		t.Fatal(err)
	}

	lapsed, err := f.repo.ListLapsedSessions(now)
	if err != nil {
		t.Fatalf("ListLapsedSessions: %v", err)
	}
	got := map[string]bool{}
	for _, s := range lapsed {
		got[s.ID] = true
	}
	for _, want := range []*runnersessions.Session{expired, idle, stuck} {
		if !got[want.ID] {
			t.Errorf("lapsed set is missing session %s", want.ID)
		}
	}
	if got[healthy.ID] {
		t.Error("a healthy lease was swept")
	}
}

// Activity is recorded against live leases only: a lease that already ended
// cannot be revived by a late worker poll.
func TestTouchSessionOnlyLiveLeases(t *testing.T) {
	f := newRunnerPoolFixture(t)
	session := f.lease(t, uuid.New().String(), f.registerNode(t, "n1").ID, time.Now().Add(time.Hour), 15)
	stale := time.Now().Add(-time.Hour)
	if _, err := f.db.Exec(`UPDATE runner_sessions SET last_activity_at = $2 WHERE id = $1`, session.ID, stale); err != nil {
		t.Fatal(err)
	}

	if err := f.repo.TouchSession(session.ID, time.Now()); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	touched, _ := f.repo.FindSessionByID(session.ID)
	if !touched.LastActivityAt.After(stale) {
		t.Error("activity was not recorded on a live lease")
	}

	touched.Status = runnersessions.StatusEnded
	if err := f.repo.UpdateSession(touched); err != nil {
		t.Fatal(err)
	}
	before, _ := f.repo.FindSessionByID(session.ID)
	if err := f.repo.TouchSession(session.ID, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("TouchSession on an ended lease: %v", err)
	}
	after, _ := f.repo.FindSessionByID(session.ID)
	if !after.LastActivityAt.Equal(before.LastActivityAt) {
		t.Error("an ended lease still recorded activity")
	}
}

// A member's live lease is found by workspace and user, and disappears from
// that lookup once it ends.
func TestFindLiveSessionForUser(t *testing.T) {
	f := newRunnerPoolFixture(t)
	node := f.registerNode(t, "n1")
	session := f.lease(t, uuid.New().String(), node.ID, time.Now().Add(time.Hour), 15)

	live, err := f.repo.FindLiveSessionForUser(f.orgID, f.userID)
	if err != nil || live == nil {
		t.Fatalf("FindLiveSessionForUser = %v, %v", live, err)
	}
	if live.NodeName != "n1" {
		t.Errorf("node name = %q, want the joined name n1", live.NodeName)
	}

	live.Status = runnersessions.StatusEnded
	live.EndReason = runnersessions.EndReasonUser
	if err := f.repo.UpdateSession(live); err != nil {
		t.Fatal(err)
	}
	if gone, err := f.repo.FindLiveSessionForUser(f.orgID, f.userID); err != nil || gone != nil {
		t.Errorf("an ended lease is still the member's live session: %v, %v", gone, err)
	}
	if all, err := f.repo.ListLiveSessions(f.orgID); err != nil || len(all) != 0 {
		t.Errorf("ListLiveSessions = %v, %v; want none", all, err)
	}
	_ = session
}

// Nodes that stopped heartbeating are flagged and returned with the lease
// each was holding, so the sweep can end it.
func TestMarkStaleNodesOffline(t *testing.T) {
	f := newRunnerPoolFixture(t)
	fresh := f.registerNode(t, "fresh")
	lost := f.registerNode(t, "lost")
	sessionID := uuid.New().String()
	if err := f.repo.SetNodeStatus(lost.ID, runnersessions.NodeLeased, &sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE runner_pool_nodes SET last_seen_at = $2 WHERE id = $1`,
		lost.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	stale, err := f.repo.MarkStaleNodesOffline(time.Now().Add(-runnersessions.NodeOfflineAfter))
	if err != nil {
		t.Fatalf("MarkStaleNodesOffline: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != lost.ID {
		t.Fatalf("stale nodes = %v, want just the lost one", stale)
	}
	if stale[0].SessionID == nil || *stale[0].SessionID != sessionID {
		t.Error("the lost node came back without the lease it was holding")
	}
	if still, _ := f.repo.FindNodeByID(fresh.ID); still.Status != runnersessions.NodeIdle {
		t.Errorf("a heartbeating node was marked %q", still.Status)
	}
}

// A session key is a personal key that must not shadow the member's own
// connector key: FindPersonal keeps returning the connector key, while the
// online check counts either.
func TestSessionKeysDoNotShadowConnectorKeys(t *testing.T) {
	f := newRunnerPoolFixture(t)
	keys := NewWorkerKeyRepository(f.db)

	connectorID := uuid.New().String()
	if _, err := f.db.Exec(`
		INSERT INTO worker_keys (id, org_id, user_id, name, key_hash, created_at)
		VALUES ($1, $2, $3, 'connector', 'hash-connector', NOW() - INTERVAL '1 day')
	`, connectorID, f.orgID, f.userID); err != nil {
		t.Fatal(err)
	}
	sessionKeyID := uuid.New().String()
	if _, err := f.db.Exec(`
		INSERT INTO worker_keys (id, org_id, user_id, name, key_hash, created_at, session_id, last_used_at)
		VALUES ($1, $2, $3, 'cloud runner', 'hash-session', NOW(), $4, NOW())
	`, sessionKeyID, f.orgID, f.userID, uuid.New().String()); err != nil {
		t.Fatal(err)
	}

	personal, err := keys.FindPersonal(f.orgID, f.userID)
	if err != nil || personal == nil {
		t.Fatalf("FindPersonal = %v, %v", personal, err)
	}
	if personal.ID != connectorID {
		t.Errorf("FindPersonal returned %q; a cloud lease must not displace the connector key", personal.Name)
	}

	// Routing asks "does this member have a runner online?" — a leased cloud
	// runner counts.
	online, err := keys.HasOnlinePersonalKey(f.orgID, f.userID, time.Now().Add(-time.Minute))
	if err != nil || !online {
		t.Errorf("HasOnlinePersonalKey = %v, %v; a live cloud runner should count as the member's runner", online, err)
	}
}
