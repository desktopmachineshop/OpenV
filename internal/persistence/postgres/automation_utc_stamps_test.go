package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/automation"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/events"
	"github.com/openv/requirements-platform/internal/scheduler"
)

// The automations stamp the times they write into TIMESTAMP columns in UTC
// (#379 bug 133), as the services bug 93 fixed do (TestTheServicesStampTimesInUTC):
// the service's created_at, updated_at and next_run_at, the scheduler's
// due query, claim and switch-off (the "now" that decides what is due, and
// the next_run_at the claim advances to), the last_run_at the scheduler and
// the trigger matcher stamp, and the start of the hour the matcher counts
// runs from. A TIMESTAMP keeps the wall clock it is sent and drops the
// offset, so on a server outside UTC a time.Now() moved by the zone's offset
// on its way in. Two hours east of UTC, an automation showed a next run two
// hours late and a last run two hours in the future, a scheduled one fired
// two hours before the next_run_at it showed, one whose schedule no longer
// parses was switched off two hours before it was due, a triggered one's
// cooldown held it back for two hours after every run, and its hourly cap
// never held, since it counted the runs made after an instant an hour in the
// future. West of UTC, a scheduled automation fired late by the offset, as
// the due query asked for what was due hours ago. A schedule is now read in
// UTC on every server, as it already was on one in UTC. Every part is driven
// through the real repositories with time.Local east, then west, of UTC.
func TestTheAutomationsStampTimesInUTC(t *testing.T) {
	db := rtDB(t)
	for _, zone := range []*time.Location{rtCEST, time.FixedZone("EDT", -4*60*60)} {
		t.Run(zone.String(), func(t *testing.T) {
			prev := time.Local
			time.Local = zone
			t.Cleanup(func() { time.Local = prev })
			automationsStampInUTC(t, db)
		})
	}
}

func automationsStampInUTC(t *testing.T, db *sql.DB) {
	org, agentID := uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, org)
	rtSeed(t, db, `INSERT INTO agents (id, org_id, slug, name, provider) VALUES ($1, $2, 'clock', 'Clock', 'claude-code')`,
		agentID, org)
	repo := NewAutomationRepository(db)
	bus := events.NewBus(NewEventRepository(db), nil)
	runs := NewAgentRunRepository(db)
	runService := agentruns.NewDefaultService(runs, triggerAgents{repo: NewAgentRepository(db)}, bus)
	const hourly = "0 * * * *"

	var before time.Time
	start := func() { before = time.Now() }
	read := func(t *testing.T, id string) *automations.Automation {
		t.Helper()
		a, err := repo.FindByID(id)
		if err != nil || a == nil {
			t.Fatalf("FindByID(%s): %v, %v", id, a, err)
		}
		return a
	}
	// wantNext checks a next_run_at read back: the next hour after the
	// moment it was computed, in UTC.
	wantNext := func(t *testing.T, what string, stored *time.Time, answered *time.Time, before, after time.Time) {
		t.Helper()
		if stored == nil {
			t.Fatalf("%s: no next_run_at stored", what)
		}
		lo, _ := automations.NextAfter(hourly, before.UTC())
		hi, _ := automations.NextAfter(hourly, after.UTC())
		if !stored.Equal(lo) && !stored.Equal(hi) {
			t.Errorf("%s stored the wall clock %s, want the next hour in UTC, %s", what,
				stored.Format(time.RFC3339Nano), hi.Format(time.RFC3339))
		}
		if answered != nil && !answered.Equal(*stored) {
			t.Errorf("%s answered %s, but a later read answers %s", what, answered.Format(time.RFC3339Nano),
				stored.Format(time.RFC3339Nano))
		}
	}
	runsOf := func(t *testing.T, automationID string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM agent_runs WHERE automation_id = $1`, automationID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// save stores an automation as an older row would hold it, its times
	// UTC wall clocks.
	created := time.Now().UTC().Add(-time.Hour)
	save := func(t *testing.T, a *automations.Automation) string {
		t.Helper()
		created = created.Add(time.Second) // the matcher takes them in creation order
		a.ID, a.OrgID, a.AgentID, a.Enabled = uuid.New().String(), org, &agentID, true
		a.EventFilter, a.CreatedAt, a.UpdatedAt = map[string]interface{}{}, created, created
		if err := repo.Save(a); err != nil {
			t.Fatalf("save %s: %v", a.Name, err)
		}
		return a.ID
	}

	t.Run("the service", func(t *testing.T) {
		svc := automations.NewDefaultService(repo)
		start()
		a, err := svc.Create(automations.CreateAutomationRequest{OrgID: org, Name: "Hourly", AgentID: &agentID,
			Kind: automations.KindScheduled, CronExpr: hourly})
		if err != nil {
			t.Fatal(err)
		}
		after := time.Now()
		got := read(t, a.ID)
		rtStamp(t, "a new automation's created_at", got.CreatedAt, a.CreatedAt, before, after)
		rtStamp(t, "a new automation's updated_at", got.UpdatedAt, a.UpdatedAt, before, after)
		wantNext(t, "a new automation's next_run_at", got.NextRunAt, a.NextRunAt, before, after)

		if _, err := svc.Update(a.ID, automations.UpdateAutomationRequest{Enabled: boolPtr(false)}); err != nil {
			t.Fatal(err)
		}
		start()
		updated, err := svc.Update(a.ID, automations.UpdateAutomationRequest{Enabled: boolPtr(true)})
		if err != nil {
			t.Fatal(err)
		}
		after = time.Now()
		got = read(t, a.ID)
		rtStamp(t, "an updated automation's updated_at", got.UpdatedAt, updated.UpdatedAt, before, after)
		wantNext(t, "a switched-on automation's next_run_at", got.NextRunAt, updated.NextRunAt, before, after)
	})

	t.Run("the scheduler", func(t *testing.T) {
		now := time.Now().UTC()
		inAnHour, aMinuteAgo := now.Add(time.Hour), now.Add(-time.Minute)
		later := save(t, &automations.Automation{Name: "Due in an hour", Kind: automations.KindScheduled,
			CronExpr: hourly, CatchUp: true, NextRunAt: &inAnHour})
		due := save(t, &automations.Automation{Name: "Due a minute ago", Kind: automations.KindScheduled,
			CronExpr: hourly, CatchUp: true, NextRunAt: &aMinuteAgo})
		brokenLater := save(t, &automations.Automation{Name: "Unreadable, due in an hour",
			Kind: automations.KindScheduled, CronExpr: "nope", CatchUp: true, NextRunAt: &inAnHour})
		brokenDue := save(t, &automations.Automation{Name: "Unreadable, due a minute ago",
			Kind: automations.KindScheduled, CronExpr: "nope", CatchUp: true, NextRunAt: &aMinuteAgo})

		// Start's catch-up runs before it returns: catch_up fires each
		// automation due as of the claim's now, through the claim.
		ctx, cancel := context.WithCancel(context.Background())
		start()
		scheduler.New(repo, runService, nil).Start(ctx)
		after := time.Now()
		cancel()

		if n := runsOf(t, later); n != 0 {
			t.Errorf("the automation due in an hour launched %d runs, want none: it is not due yet", n)
		}
		if got := read(t, later); got.NextRunAt == nil || !got.NextRunAt.Equal(inAnHour.Round(time.Microsecond)) {
			t.Errorf("the automation due in an hour holds next_run_at %v, want %s, unclaimed", got.NextRunAt,
				inAnHour.Format(time.RFC3339Nano))
		}
		if n := runsOf(t, due); n != 1 {
			t.Errorf("the automation due a minute ago launched %d runs, want one", n)
		}
		got := read(t, due)
		wantNext(t, "a claimed automation's next_run_at", got.NextRunAt, nil, before, after)
		if got.LastRunAt == nil {
			t.Fatal("the automation due a minute ago has no last_run_at")
		}
		rtStamp(t, "a scheduled run's last_run_at", *got.LastRunAt, time.Time{}, before, after)
		if got := read(t, brokenLater); !got.Enabled || got.NextRunAt == nil {
			t.Errorf("the unreadable automation due in an hour is switched off (enabled %v, next_run_at %v), "+
				"want it left alone until it is due", got.Enabled, got.NextRunAt)
		}
		if got := read(t, brokenDue); got.Enabled || got.NextRunAt != nil {
			t.Errorf("the unreadable automation due a minute ago: enabled %v, next_run_at %v, want switched off",
				got.Enabled, got.NextRunAt)
		}
	})

	t.Run("the trigger matcher", func(t *testing.T) {
		const event = domainevents.ArtifactCreated
		// At its hourly cap: one run, launched ten minutes ago, of at most
		// one an hour. The run repository stores a run's created_at in UTC.
		capped := save(t, &automations.Automation{Name: "At its cap", Kind: automations.KindTriggered,
			EventType: event, MaxRunsPerHour: 1})
		cappedID := capped
		if err := runs.Save(&agentruns.Run{ID: uuid.New().String(), OrgID: org, AgentID: agentID,
			AutomationID: &cappedID, Status: agentruns.StatusSucceeded, Prompt: "Earlier",
			ArtifactsTouched: []map[string]interface{}{}, AttemptCount: 1, MaxAttempts: 1,
			CreatedAt: time.Now().Add(-10 * time.Minute)}); err != nil {
			t.Fatal(err)
		}
		fires := save(t, &automations.Automation{Name: "Fires", Kind: automations.KindTriggered,
			EventType: event, CooldownSeconds: 60, MaxRunsPerHour: 10})

		automation.NewTriggerMatcher(repo, runService, nil).Start(bus)
		start()
		bus.Publish(domainevents.New(event, "", uuid.New().String(), domainevents.ActorSystem,
			map[string]interface{}{"title": "Clock"}).WithOrg(org))

		// The bus dispatches on a goroutine of its own, and the matcher
		// takes the capped automation first.
		deadline := time.Now().Add(30 * time.Second)
		var got *automations.Automation
		for {
			if got = read(t, fires); got.LastRunAt != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the triggered automation was not stamped: it launched no run")
			}
			time.Sleep(20 * time.Millisecond)
		}
		rtStamp(t, "a triggered run's last_run_at", *got.LastRunAt, time.Time{}, before, time.Now())
		if n := runsOf(t, capped); n != 1 {
			t.Errorf("the automation at its hourly cap has %d runs, want its one earlier run: the cap held it back", n)
		}
	})
}

func boolPtr(b bool) *bool { return &b }
