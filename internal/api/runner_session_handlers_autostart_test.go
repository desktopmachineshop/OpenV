package api

import (
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// autoLeases is a lease service that holds no lease until Start makes one.
type autoLeases struct {
	runnersessions.Service
	held    *runnersessions.Session
	started int
	err     error
}

func (f *autoLeases) Get(orgID, userID string) (*runnersessions.Session, error) { return f.held, nil }

func (f *autoLeases) MinutesUsed(orgID string, since time.Time) (int, error) { return 0, nil }

func (f *autoLeases) Start(orgID, userID string, sessionMinutes, idleMinutes int) (*runnersessions.Session, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	f.started++
	f.held = &runnersessions.Session{ID: "s1", OrgID: orgID, UserID: userID, Status: runnersessions.StatusStarting}
	return f.held, true, nil
}

type onlineKeys struct {
	workerkeys.Service
	keys []*workerkeys.Key
}

func (f *onlineKeys) List(orgID string) ([]*workerkeys.Key, error) { return f.keys, nil }

func TestAChatTurnWithNoRunnerStartsACloudRunner(t *testing.T) {
	now := time.Now().UTC()
	member, other := "user-1", "user-2"
	cases := []struct {
		name  string
		keys  []*workerkeys.Key
		held  bool
		err   error
		start bool
	}{
		{name: "nothing online", start: true},
		{name: "another member's personal runner does not take the turn", keys: []*workerkeys.Key{{UserID: &other, LastUsedAt: &now}}, start: true},
		{name: "a stale workspace runner does not count", keys: []*workerkeys.Key{{LastUsedAt: ptrTime(now.Add(-time.Hour))}}, start: true},
		{name: "the member's own runner is online", keys: []*workerkeys.Key{{UserID: &member, LastUsedAt: &now}}},
		{name: "a workspace runner is online", keys: []*workerkeys.Key{{LastUsedAt: &now}}},
		{name: "the member already holds a lease", held: true},
		{name: "the pool has nothing free", err: runnersessions.ErrNoNodes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			leases := &autoLeases{err: c.err}
			if c.held {
				leases.held = &runnersessions.Session{ID: "s0"}
			}
			h := newTestHandler(t, func(h *Handler) {
				h.RunnerSessionService = leases
				h.WorkerKeyService = &onlineKeys{keys: c.keys}
				h.OrgService = memberOrgService()
			})
			h.autoStartCloudRunner("org-1", &member)
			if got := leases.started == 1; got != c.start {
				t.Fatalf("started a lease = %v, want %v", got, c.start)
			}
		})
	}
}

func TestNoCloudRunnerWithoutAPoolOrALauncher(t *testing.T) {
	member := "user-1"
	if newTestHandler(t).autoStartCloudRunner("org-1", &member) {
		t.Fatal("a deployment with no pool reported a lease")
	}
	leases := &autoLeases{}
	h := newTestHandler(t, func(h *Handler) {
		h.RunnerSessionService = leases
		h.WorkerKeyService = &onlineKeys{}
		h.OrgService = memberOrgService()
	})
	h.autoStartCloudRunner("org-1", nil)
	if leases.started != 0 {
		t.Fatal("a turn nobody launched leased a runner")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
