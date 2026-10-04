package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/scheduler"
)

// schedulerClaimRuns records the runs the schedulers launch, and refuses
// each with launchErr when it is set.
type schedulerClaimRuns struct {
	agentruns.Service
	mu        sync.Mutex
	launched  []agentruns.LaunchRequest
	launchErr error
}

func (f *schedulerClaimRuns) Launch(req agentruns.LaunchRequest) (*agentruns.Run, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launched = append(f.launched, req)
	if f.launchErr != nil {
		return nil, "", f.launchErr
	}
	return &agentruns.Run{ID: uuid.New().String(), OrgID: req.OrgID, AgentID: req.AgentID}, "token", nil
}

func (f *schedulerClaimRuns) requests() []agentruns.LaunchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agentruns.LaunchRequest(nil), f.launched...)
}

// racingRepo is the real repository, with its first two due queries held
// until both have read the due set, so that two schedulers each hold the
// same candidate before either claims it.
type racingRepo struct {
	*AutomationRepository
	mu      sync.Mutex
	held    int
	barrier sync.WaitGroup
}

func (r *racingRepo) ListDueScheduled(now time.Time) ([]*automations.Automation, error) {
	due, err := r.AutomationRepository.ListDueScheduled(now)
	r.mu.Lock()
	hold := r.held < 2
	r.held++
	r.mu.Unlock()
	if hold {
		r.barrier.Done()
		r.barrier.Wait()
	}
	return due, err
}

// TestSchedulersShareTheRealClaim drives two real schedulers
// (internal/scheduler, which cmd/server starts once per API replica) against
// this package's AutomationRepository, refactor plan step S11 (OpenV
// REQ-24): the claim SQL TestClaimDueScheduledIsExactlyOnce pins, as the
// scheduler uses it. Two schedulers starting together, both reading the same
// due automation at catch-up, race for it: one claim is won, one lost, and
// exactly one run is launched; the row's next_run_at moves to the cron's
// next occurrence and its last_run_at is stamped. An occurrence that launches
// nothing, because the launch is refused or because catch-up skips an
// automation without catch_up, still advances next_run_at, so it is skipped,
// not retried, but leaves last_run_at as it was (bug 74 of issue #379,
// decided under Q35: the claim stamped it before anything launched). An
// automation whose cron expression does not parse is
// never fired: it is switched off (enabled false, next_run_at NULL; bug 72 of
// issue #379, which fired it once and left it enabled but never due), and
// stays so across a restart. internal/scheduler's own tests pin the rest
// against a stand-in that models this SQL.
func TestSchedulersShareTheRealClaim(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewAutomationRepository(db)
	agentID, orgID := uuid.New().String(), uuid.New().String()
	if _, err := db.Exec(`INSERT INTO agents (id, slug, name, provider) VALUES ($1, 'helper', 'Helper', 'claude-code')`,
		agentID); err != nil {
		t.Fatal(err)
	}
	// quiet is a cron with no occurrence in the next hour, so an advanced
	// row is not due again however close to an occurrence the test runs.
	quiet := "0 0 1 1 *"
	if next, _ := automations.NextAfter(quiet, time.Now()); time.Until(next) < time.Hour {
		quiet = "0 0 1 7 *"
	}
	seedWith := func(t *testing.T, name, cron string, catchUp bool) string {
		t.Helper()
		id := uuid.New().String()
		next := time.Now().Add(-time.Hour)
		a := &automations.Automation{ID: id, OrgID: orgID, Name: name, AgentID: &agentID,
			Kind: automations.KindScheduled, Enabled: true, CronExpr: cron, CatchUp: catchUp, NextRunAt: &next,
			EventFilter: map[string]interface{}{}, CooldownSeconds: 60, MaxRunsPerHour: 10,
			CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := repo.Save(a); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		return id
	}
	seed := func(t *testing.T, name, cron string) string {
		t.Helper()
		return seedWith(t, name, cron, true)
	}
	stored := func(t *testing.T, id string) *automations.Automation {
		t.Helper()
		a, err := repo.FindByID(id)
		if err != nil || a == nil {
			t.Fatalf("FindByID: %v, %v", a, err)
		}
		return a
	}
	start := func(t *testing.T, r automations.Repository, runs ...*schedulerClaimRuns) {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel() // ends each scheduler's ticks; catch-up is done when Start returns
		var wg sync.WaitGroup
		for _, rs := range runs {
			wg.Add(1)
			go func(rs *schedulerClaimRuns) {
				defer wg.Done()
				scheduler.New(r, rs, nil).Start(ctx)
			}(rs)
		}
		wg.Wait()
	}
	due := func(t *testing.T, id string, at time.Time) bool {
		t.Helper()
		list, err := repo.ListDueScheduled(at)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range list {
			if a.ID == id {
				return true
			}
		}
		return false
	}

	t.Run("two schedulers racing at catch-up", func(t *testing.T) {
		// The winner's advance leaves the row not due for the loser's claim.
		cron := quiet
		id := seed(t, "Raced", cron)
		racing := &racingRepo{AutomationRepository: repo}
		racing.barrier.Add(2)
		runsA, runsB := &schedulerClaimRuns{}, &schedulerClaimRuns{}
		start(t, racing, runsA, runsB)
		after := time.Now()

		launched := append(runsA.requests(), runsB.requests()...)
		if len(launched) != 1 {
			t.Fatalf("the two schedulers launched %d runs, want exactly one", len(launched))
		}
		if req := launched[0]; req.AutomationID == nil || *req.AutomationID != id || req.AgentID != agentID ||
			req.OrgID != orgID || req.Prompt != "Scheduled run of automation: Raced" {
			t.Errorf("launched %+v, want the raced automation's scheduled run", req)
		}
		a, err := repo.FindByID(id)
		if err != nil || a == nil {
			t.Fatalf("FindByID: %v, %v", a, err)
		}
		next, _ := automations.NextAfter(cron, after)
		if a.NextRunAt == nil || a.NextRunAt.Format("2006-01-02 15:04") != next.Format("2006-01-02 15:04") ||
			a.LastRunAt == nil || !a.Enabled {
			t.Errorf("after the race: next_run_at %v, last_run_at %v, enabled %v; want %s (wall clock), stamped, enabled",
				a.NextRunAt, a.LastRunAt, a.Enabled, next.Format("2006-01-02 15:04"))
		}
		if due(t, id, time.Now()) {
			t.Error("the raced automation is still due")
		}
	})

	for _, tc := range []struct {
		name    string
		catchUp bool
		refuse  bool
	}{
		{name: "a refused launch leaves last_run_at", catchUp: true, refuse: true},
		{name: "a missed run skipped at catch-up leaves last_run_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := seedWith(t, "Unlaunched", quiet, tc.catchUp)
			runs := &schedulerClaimRuns{}
			if tc.refuse {
				runs.launchErr = errors.New("this workspace has reached its monthly budget")
			}
			start(t, repo, runs)
			attempts := 0
			if tc.refuse {
				attempts = 1
			}
			if n := len(runs.requests()); n != attempts {
				t.Fatalf("launch attempts = %d, want %d", n, attempts)
			}
			a := stored(t, id)
			if a.NextRunAt == nil || !a.Enabled || due(t, id, time.Now()) {
				t.Errorf("next_run_at %v, enabled %v: want the occurrence claimed, next_run_at advanced", a.NextRunAt,
					a.Enabled)
			}
			if a.LastRunAt != nil {
				t.Errorf("last_run_at = %v, want NULL: no run was launched", a.LastRunAt)
			}
			start(t, repo, runs) // a restart: the skipped occurrence is not retried
			if n := len(runs.requests()); n != attempts {
				t.Errorf("a restart made %d more launch attempts, want none", n-attempts)
			}
		})
	}

	t.Run("an invalid cron is switched off", func(t *testing.T) {
		id := seed(t, "Broken", "every day at noon")
		runs := &schedulerClaimRuns{}
		start(t, repo, runs)
		if got := runs.requests(); len(got) != 0 {
			t.Fatalf("launched %+v, want nothing", got)
		}
		a, err := repo.FindByID(id)
		if err != nil || a == nil {
			t.Fatalf("FindByID: %v, %v", a, err)
		}
		if a.Enabled || a.NextRunAt != nil || a.LastRunAt != nil {
			t.Errorf("after catch-up: enabled %v, next_run_at %v, last_run_at %v; want switched off, NULL, never run",
				a.Enabled, a.NextRunAt, a.LastRunAt)
		}
		if due(t, id, time.Now().AddDate(10, 0, 0)) {
			t.Error("the broken automation is due again within ten years")
		}
		start(t, repo, runs) // a restart, which catches up again
		if n := len(runs.requests()); n != 0 {
			t.Errorf("a restart launched %d runs, want none", n)
		}
	})
}
