package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
)

// The characterization of internal/scheduler, refactor plan step S11
// (services-4; OpenV REQ-24, an automation launched on a cron schedule). It
// pins what the scheduler does today, as cmd/server wires it
// (scheduler.New(automationRepo, runService, teamService).Start(ctx)):
// catch-up at start, the claim it wins or loses against a peer replica, an
// invalid cron expression, the run it launches, and ResolveTarget, which the
// trigger matcher and run-now share. The scheduler reads the clock itself,
// so the times it stamps are pinned to the window of the call.

// TestSchedulerCatchUp pins the catch-up Start performs after downtime,
// synchronously, before it returns (cmd/server calls Start before the server
// listens, invariant I17). An automation whose next_run_at passed while the
// server was down, however many occurrences ago, gets exactly one run when
// it has catch_up, and none when it has not; either way its row is claimed,
// so next_run_at moves to the cron's next occurrence after now, but only the
// one that ran has last_run_at stamped, after its launch (bug 74 of issue
// #379: the claim stamped the skipped one too). One not yet due is not
// touched. Then nothing is due: a tick right after fires nothing.
func TestSchedulerCatchUp(t *testing.T) {
	logs := captureLog(t)
	cron := quietCron(t)
	now := time.Now()
	missed := now.Add(-3 * 365 * 24 * time.Hour) // three yearly occurrences went by
	later := now.Add(2 * time.Hour)
	catchUp := scheduled("au-catch", "Nightly digest", cron, missed)
	catchUp.CatchUp = true
	catchUp.PromptTemplate = "Digest for {{automation.name}}"
	repo := newSchedRepo(catchUp,
		scheduled("au-skip", "Sweep", cron, missed),
		scheduled("au-later", "Later", cron, later))
	runs := newSchedRuns()
	s := New(repo, runs, crewGraphs())
	if s.interval != 30*time.Second {
		t.Errorf("New's tick interval = %s, want 30s", s.interval)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	before := time.Now()
	s.Start(ctx)
	after := time.Now()

	// No waiting: the run is launched before Start returns.
	want := []string{wantLaunch("au-catch", "Digest for Nightly digest")}
	if got := launchLines(runs.requests()); !equalLines(got, want) {
		t.Fatalf("catch-up launched\n  %q\nwant exactly one run, for the automation with catch_up:\n  %q", got, want)
	}
	claims := repo.claimsMade()
	if len(claims) != 2 || claims[0].id != "au-catch" || claims[1].id != "au-skip" {
		t.Fatalf("catch-up claimed %v, want au-catch then au-skip (oldest next_run_at first, each once)", claims)
	}
	for _, c := range claims {
		if !c.won {
			t.Errorf("the claim of %s was lost, want won", c.id)
		}
		within(t, c.id+"'s claim time", c.at, before, after)
		if c.nextRun == nil || !c.nextRun.Equal(nextAfter(t, cron, c.at)) {
			t.Errorf("%s's claim advanced next_run_at to %s, want the cron's next occurrence after the claim, %s",
				c.id, timeOrDash(c.nextRun), nextAfter(t, cron, c.at).Format(time.RFC3339Nano))
		}
	}
	for _, id := range []string{"au-catch", "au-skip"} {
		if a := repo.stored(t, id); a.NextRunAt == nil || !a.NextRunAt.After(after) {
			t.Errorf("%s after catch-up: next_run_at %s, want a next run to come", id, timeOrDash(a.NextRunAt))
		}
	}
	stamps := repo.stampsMade()
	if len(stamps) != 1 || stamps[0].id != "au-catch" {
		t.Fatalf("stamped last_run_at %v, want au-catch's alone: au-skip ran nothing", stamps)
	}
	within(t, "au-catch's last_run_at", stamps[0].at, claims[0].at, after)
	if a := repo.stored(t, "au-skip"); a.LastRunAt != nil {
		t.Errorf("au-skip's last_run_at = %s, want unset: its missed run was skipped", timeOrDash(a.LastRunAt))
	}
	if a := repo.stored(t, "au-later"); !a.NextRunAt.Equal(later) || a.LastRunAt != nil {
		t.Errorf("the automation not yet due was changed: next_run_at %s, last_run_at %s",
			timeOrDash(a.NextRunAt), timeOrDash(a.LastRunAt))
	}

	s.tick()
	if n := len(runs.requests()); n != 1 {
		t.Errorf("a tick right after catch-up launched %d more runs, want none", n-1)
	}
	if n := len(repo.claimsMade()); n != 2 {
		t.Errorf("a tick right after catch-up made %d more claims, want none", n-2)
	}
	if n := len(repo.stampsMade()); n != 1 {
		t.Errorf("a tick right after catch-up made %d more stamps, want none", n-1)
	}
	if got := logs.lines(); len(got) != 0 {
		t.Errorf("catch-up logged %q, want nothing", got)
	}
}

// TestSchedulerTickFiresEveryDueAutomation pins a tick: every automation
// due fires once, in next_run_at order, catch_up or not (the flag matters
// only at start), each claim advancing its row so the next tick fires
// nothing. A failed due query logs and fires nothing, at start or on a tick.
func TestSchedulerTickFiresEveryDueAutomation(t *testing.T) {
	t.Run("every due automation fires once", func(t *testing.T) {
		logs := captureLog(t)
		cron := quietCron(t)
		now := time.Now()
		first := scheduled("au-1", "First", cron, now.Add(-time.Minute))
		second := scheduled("au-2", "Second", cron, now.Add(-time.Hour))
		second.CatchUp = true
		repo := newSchedRepo(first, second, scheduled("au-3", "Not yet", cron, now.Add(time.Hour)))
		runs := newSchedRuns()
		s := New(repo, runs, crewGraphs())

		s.tick()
		want := []string{wantLaunch("au-2", "Scheduled run of automation: Second"),
			wantLaunch("au-1", "Scheduled run of automation: First")}
		if got := launchLines(runs.requests()); !equalLines(got, want) {
			t.Fatalf("a tick launched\n  %q\nwant\n  %q", got, want)
		}
		s.tick()
		if n := len(runs.requests()); n != 2 {
			t.Errorf("the next tick launched %d more runs, want none", n-2)
		}
		if got := logs.lines(); len(got) != 0 {
			t.Errorf("the ticks logged %q, want nothing", got)
		}
	})

	t.Run("a failed due query fires nothing", func(t *testing.T) {
		logs := captureLog(t)
		repo := newSchedRepo(scheduled("au-1", "First", "*/5 * * * *", time.Now().Add(-time.Minute)))
		repo.listErr = errors.New("connection refused")
		runs := newSchedRuns()
		s := New(repo, runs, crewGraphs())
		s.catchUp()
		s.tick()
		if n := len(runs.requests()) + len(repo.claimsMade()); n != 0 {
			t.Errorf("a failed query still claimed or launched %d times", n)
		}
		want := []string{"scheduler: catch-up query failed: connection refused",
			"scheduler: due query failed: connection refused"}
		if got := logs.lines(); !equalLines(got, want) {
			t.Errorf("logged %q, want %q", got, want)
		}
	})
}

// TestSchedulerStartTicks pins Start's loop: catch-up first, synchronously,
// then a tick every interval on a goroutine of its own, which fires an
// automation that falls due after start (catch-up only claimed the row it
// skipped). The interval is shortened here; New sets 30 s
// (TestSchedulerCatchUp).
func TestSchedulerStartTicks(t *testing.T) {
	captureLog(t)
	cron := quietCron(t)
	repo := newSchedRepo(scheduled("au-skip", "Skipped", cron, time.Now().Add(-time.Hour)))
	runs := newSchedRuns()
	s := New(repo, runs, crewGraphs())
	s.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)
	if claims := repo.claimsMade(); len(claims) != 1 || !claims[0].won || len(runs.requests()) != 0 {
		t.Fatalf("Start returned with claims %v and %d runs, want catch-up's one claim and no run", claims,
			len(runs.requests()))
	}
	repo.add(scheduled("au-due", "Fell due", cron, time.Now().Add(-time.Second)))
	select {
	case req := <-runs.launches:
		if got, want := launchLine(req), wantLaunch("au-due", "Scheduled run of automation: Fell due"); got != want {
			t.Fatalf("a tick launched\n  %s\nwant\n  %s", got, want)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("no tick fired the automation that fell due after start (%d due queries)", repo.listCount())
	}
	if repo.listCount() < 2 {
		t.Errorf("due queries = %d, want catch-up's and at least one tick's", repo.listCount())
	}
}

// TestSchedulerPrompt pins the prompt a scheduled run gets: the template
// rendered with automation.name as its one variable (any other placeholder,
// event ones included, renders empty), or, when that renders to the empty
// string or only whitespace, "Scheduled run of automation: <name>", as the
// trigger matcher's and run-now's copies do (the regression test for bug 77
// of issue #379: this copy kept a render of only whitespace as the prompt).
func TestSchedulerPrompt(t *testing.T) {
	cases := []struct{ name, template, want string }{
		{"an empty template", "", "Scheduled run of automation: Weekly report"},
		{"the name", "Run {{automation.name}} now", "Run Weekly report now"},
		{"spaces inside the braces", "{{ automation.name }}", "Weekly report"},
		{"no other variable", "{{automation.name}} on {{event.type}} in {{project.id}}{{event.entity_id}}",
			"Weekly report on  in "},
		{"only unknown placeholders", "{{event.type}}{{project.id}}", "Scheduled run of automation: Weekly report"},
		{"unknown placeholders and a space", "{{event.type}} {{project.id}}",
			"Scheduled run of automation: Weekly report"},
		{"a template of whitespace", " \n\t", "Scheduled run of automation: Weekly report"},
		{"text inside whitespace, kept as it is", "  Summarise {{automation.name}}.\n", "  Summarise Weekly report.\n"},
		{"plain text", "Summarise the week.", "Summarise the week."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureLog(t)
			a := scheduled("au-1", "Weekly report", "0 9 * * 1", time.Now().Add(-time.Minute))
			a.PromptTemplate = tc.template
			runs := newSchedRuns()
			New(newSchedRepo(a), runs, crewGraphs()).tick()
			if got, want := launchLines(runs.requests()), []string{wantLaunch("au-1", tc.want)}; !equalLines(got, want) {
				t.Errorf("launched\n  %q\nwant\n  %q", got, want)
			}
		})
	}
}

// TestSchedulerTargets pins whom a scheduled run goes to, and what happens
// when there is no one: a crew's run goes to its entry node's agent with the
// crew and node named, and last_run_at is stamped after the launch; a crew
// with no entry node, one whose entry node is gone, one no row has and an
// automation with no target each launch nothing, log one line, and stay
// claimed (next_run_at advanced), so the occurrence is skipped, not retried,
// but leave last_run_at unset, since nothing ran (bug 74 of issue #379,
// decided under Q35: the claim stamped it before the target was resolved). A
// launch the run service refuses is logged and skipped the same way. A
// stamp the repository refuses is logged; the run stands.
func TestSchedulerTargets(t *testing.T) {
	cron := quietCron(t)
	crew := func(id string) *automations.Automation {
		a := scheduled("au-"+id, "Crew "+id, cron, time.Now().Add(-time.Minute))
		team := id
		a.AgentID, a.TeamID = nil, &team
		return a
	}
	none := scheduled("au-none", "Nobody", cron, time.Now().Add(-time.Minute))
	none.AgentID = nil
	emptyAgent := crew("crew-1")
	emptyAgent.ID = "au-empty-agent"
	blank := ""
	emptyAgent.AgentID = &blank

	project, crewID, entry, automation := "proj-1", "crew-1", "node-entry", ""
	crewLaunch := func(id string) string {
		automation = id
		return launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: "agent-entry", ProjectID: &project,
			AutomationID: &automation, TeamID: &crewID, TeamNodeID: &entry,
			Prompt: "Scheduled run of automation: Crew crew-1"})
	}
	cases := []struct {
		name   string
		a      *automations.Automation
		launch string
		log    string
	}{
		{name: "a crew goes to its entry node's agent", a: crew("crew-1"), launch: crewLaunch("au-crew-1")},
		{name: "an empty agent id counts as none", a: emptyAgent, launch: crewLaunch("au-empty-agent")},
		{name: "a crew with no entry node", a: crew("crew-headless"),
			log: "scheduler: automation Crew crew-headless (au-crew-headless) target unresolvable: team has no entry node"},
		{name: "a crew whose entry node is gone", a: crew("crew-stale"),
			log: "scheduler: automation Crew crew-stale (au-crew-stale) target unresolvable: team has no entry node"},
		{name: "a crew no row has", a: crew("crew-gone"),
			log: "scheduler: automation Crew crew-gone (au-crew-gone) target unresolvable: team crew-gone not found"},
		{name: "no agent and no crew", a: none,
			log: "scheduler: automation Nobody (au-none) target unresolvable: automation has neither agent nor team target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t)
			repo := newSchedRepo(tc.a)
			runs := newSchedRuns()
			New(repo, runs, crewGraphs()).tick()
			var want []string
			if tc.launch != "" {
				want = []string{tc.launch}
			}
			if got := launchLines(runs.requests()); !equalLines(got, want) {
				t.Errorf("launched\n  %q\nwant\n  %q", got, want)
			}
			var wantLog []string
			if tc.log != "" {
				wantLog = []string{tc.log}
			}
			if got := logs.lines(); !equalLines(got, wantLog) {
				t.Errorf("logged %q, want %q", got, wantLog)
			}
			a := repo.stored(t, tc.a.ID)
			if a.NextRunAt == nil || !a.NextRunAt.After(time.Now()) {
				t.Errorf("next_run_at = %s: want it advanced by the claim, which comes first, whether or not a run "+
					"follows", timeOrDash(a.NextRunAt))
			}
			if launched := tc.launch != ""; (a.LastRunAt != nil) != launched || len(repo.stampsMade()) > 1 {
				t.Errorf("last_run_at = %s after stamps %v: want it stamped only when a run was launched (%v)",
					timeOrDash(a.LastRunAt), repo.stampsMade(), launched)
			}
		})
	}

	t.Run("a refused launch is logged and skipped", func(t *testing.T) {
		logs := captureLog(t)
		repo := newSchedRepo(scheduled("au-1", "Refused", cron, time.Now().Add(-time.Minute)))
		runs := newSchedRuns()
		runs.launchErr = errors.New("this workspace has reached its monthly budget")
		s := New(repo, runs, crewGraphs())
		s.tick()
		s.tick()
		if n := len(runs.requests()); n != 1 {
			t.Errorf("launch attempts = %d, want 1 (the occurrence is skipped, not retried)", n)
		}
		if a := repo.stored(t, "au-1"); a.LastRunAt != nil || a.NextRunAt == nil || !a.NextRunAt.After(time.Now()) {
			t.Errorf("last_run_at = %s, next_run_at = %s: want unset, no run was launched, and advanced",
				timeOrDash(a.LastRunAt), timeOrDash(a.NextRunAt))
		}
		want := []string{"scheduler: failed to launch run for automation au-1: this workspace has reached its monthly budget"}
		if got := logs.lines(); !equalLines(got, want) {
			t.Errorf("logged %q, want %q", got, want)
		}
	})

	t.Run("a stamp refused is logged", func(t *testing.T) {
		logs := captureLog(t)
		repo := newSchedRepo(scheduled("au-1", "Unstamped", cron, time.Now().Add(-time.Minute)))
		repo.stampErr = errors.New("deadlock detected")
		runs := newSchedRuns()
		s := New(repo, runs, crewGraphs())
		s.tick()
		s.tick()
		want := []string{wantLaunch("au-1", "Scheduled run of automation: Unstamped")}
		if got := launchLines(runs.requests()); !equalLines(got, want) {
			t.Errorf("launched\n  %q\nwant the one run\n  %q", got, want)
		}
		if stamps := repo.stampsMade(); len(stamps) != 1 {
			t.Errorf("stamps = %v, want one attempt", stamps)
		}
		wantLog := []string{"scheduler: failed to stamp last_run_at for automation au-1: deadlock detected"}
		if got := logs.lines(); !equalLines(got, wantLog) {
			t.Errorf("logged %q, want %q", got, wantLog)
		}
	})
}

// TestSchedulerClaims pins the claim that makes a multi-replica deployment
// fire a due automation once: each replica claims the row before it fires,
// and only the replica whose claim wins resolves the target and launches.
// Two schedulers that both read the automation as due race for it: one claim
// is won and one lost, and exactly one run is launched. A claim the
// repository answers with an error launches nothing, whether or not the
// answer also says won, and so does a switch-off of an automation whose cron
// does not parse (TestSchedulerSwitchesOffAnInvalidCron); the regression
// test for bug 73 (issue #379): the switch-off's path, which was a claim
// then, fired on an answer of won with an error, the other path never did.
func TestSchedulerClaims(t *testing.T) {
	t.Run("two schedulers racing for one due automation", func(t *testing.T) {
		logs := captureLog(t)
		a := scheduled("au-1", "Raced", quietCron(t), time.Now().Add(-time.Minute))
		a.CatchUp = true
		repo := newSchedRepo(a)
		repo.barrier = &sync.WaitGroup{}
		repo.barrier.Add(2)
		runsA, runsB := newSchedRuns(), newSchedRuns()
		replicaA, replicaB := New(repo, runsA, crewGraphs()), New(repo, runsB, crewGraphs())

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); replicaA.tick() }()
		go func() { defer wg.Done(); replicaB.tick() }()
		wg.Wait()
		repo.mu.Lock()
		repo.barrier = nil
		repo.mu.Unlock()

		claims := repo.claimsMade()
		if len(claims) != 2 || claims[0].id != "au-1" || claims[1].id != "au-1" {
			t.Fatalf("claims = %v, want one per replica", claims)
		}
		if !claims[0].won || claims[1].won {
			t.Errorf("claims won = %v then %v, want the first won and the second lost", claims[0].won, claims[1].won)
		}
		got := launchLines(append(runsA.requests(), runsB.requests()...))
		if want := []string{wantLaunch("au-1", "Scheduled run of automation: Raced")}; !equalLines(got, want) {
			t.Fatalf("the replicas launched\n  %q\nwant exactly one run\n  %q", got, want)
		}
		replicaA.tick()
		replicaB.tick()
		if n := len(runsA.requests()) + len(runsB.requests()); n != 1 {
			t.Errorf("the next ticks launched %d more runs, want none", n-1)
		}
		if got := logs.lines(); len(got) != 0 {
			t.Errorf("logged %q, want nothing (a lost claim is silent)", got)
		}
	})

	t.Run("a lost claim resolves no target and launches nothing", func(t *testing.T) {
		logs := captureLog(t)
		team := "crew-1"
		a := scheduled("au-1", "Stale", "*/5 * * * *", time.Now().Add(-time.Minute))
		a.AgentID, a.TeamID = nil, &team
		candidate := cloneAutomation(a)
		future := time.Now().Add(time.Hour)
		a.NextRunAt = &future // a peer replica claimed it and advanced it
		repo := newSchedRepo(a)
		runs, crews := newSchedRuns(), crewGraphs()
		New(repo, runs, crews).fire(candidate)
		if claims := repo.claimsMade(); len(claims) != 1 || claims[0].won {
			t.Fatalf("claims = %v, want one lost", claims)
		}
		if n, asked := len(runs.requests()), crews.crewsAsked(); n != 0 || len(asked) != 0 {
			t.Errorf("a lost claim launched %d runs and looked up crews %q, want neither", n, asked)
		}
		if a := repo.stored(t, "au-1"); !a.NextRunAt.Equal(future) {
			t.Errorf("a lost claim changed next_run_at to %s", timeOrDash(a.NextRunAt))
		}
		if got := logs.lines(); len(got) != 0 {
			t.Errorf("logged %q, want nothing", got)
		}
	})

	t.Run("a claim the repository answers with an error", func(t *testing.T) {
		boom := errors.New("connection reset")
		const claimFailed = "scheduler: failed to claim au-1: connection reset"
		const switchOffFailed = "scheduler: failed to switch off automation Erring (au-1): connection reset"
		cases := []struct {
			name      string
			cron      string
			answer    claimResult
			switchOff bool
			log       string
		}{
			{name: "valid cron, not won", cron: "*/5 * * * *", answer: claimResult{false, boom}, log: claimFailed},
			{name: "valid cron, won", cron: "*/5 * * * *", answer: claimResult{true, boom}, log: claimFailed},
			{name: "invalid cron, not switched off", cron: "@fortnightly", answer: claimResult{false, boom},
				switchOff: true, log: switchOffFailed},
			{name: "invalid cron, switched off", cron: "@fortnightly", answer: claimResult{true, boom},
				switchOff: true, log: switchOffFailed},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				logs := captureLog(t)
				repo := newSchedRepo(scheduled("au-1", "Erring", tc.cron, time.Now().Add(-time.Minute)))
				repo.claimFails = map[string]claimResult{"au-1": tc.answer}
				runs := newSchedRuns()
				New(repo, runs, crewGraphs()).tick()
				if n := len(runs.requests()); n != 0 {
					t.Errorf("launched %d runs, want none: an answer with an error never fires", n)
				}
				if claims, offs := len(repo.claimsMade()), len(repo.switchOffsMade()); (offs == 1) != tc.switchOff ||
					claims+offs != 1 {
					t.Errorf("claims %d and switch-offs %d, want one switch-off: %v, else one claim", claims, offs,
						tc.switchOff)
				}
				if got := logs.lines(); !equalLines(got, []string{tc.log}) {
					t.Errorf("logged %q, want %q", got, []string{tc.log})
				}
			})
		}
	})
}

// TestSchedulerSwitchesOffAnInvalidCron is the regression test for bug 72
// (issue #379, decided under Q34): an automation whose cron expression does
// not parse (robfig/cron's ParseStandard) fired once, on a tick or at
// catch-up with catch_up, and its claim set next_run_at to NULL while the
// row stayed enabled, so it never ran again and an admin saw it switched on;
// without catch_up, catch-up claimed it the same way and it never ran at all.
// Now the scheduler fires no automation whose cron does not parse: it claims
// nothing, switches the automation off (enabled false, next_run_at NULL, in
// one atomic step under the claim's condition) and logs why, once; then it
// is never due again. The create and update paths refuse such an expression
// (S5d pins their text); the row can only hold one written another way. Of
// two replicas that both read it due, one switches it off and logs; the
// other's switch-off is lost, silently, as a lost claim is.
func TestSchedulerSwitchesOffAnInvalidCron(t *testing.T) {
	const cron = "every day at noon"
	const logLine = `scheduler: switched off automation Broken schedule (au-bad): ` +
		`invalid cron expression "every day at noon": expected exactly 5 fields, found 4: [every day at noon]`
	cases := []struct {
		name    string
		catchUp bool
		start   func(s *Scheduler)
	}{
		{name: "on a tick", start: func(s *Scheduler) { s.tick() }},
		{name: "at catch-up, with catch_up", catchUp: true, start: func(s *Scheduler) { s.catchUp() }},
		{name: "at catch-up, without catch_up", start: func(s *Scheduler) { s.catchUp() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLog(t)
			a := scheduled("au-bad", "Broken schedule", cron, time.Now().Add(-time.Minute))
			a.CatchUp = tc.catchUp
			repo := newSchedRepo(a)
			runs := newSchedRuns()
			s := New(repo, runs, crewGraphs())

			before := time.Now()
			tc.start(s)
			after := time.Now()
			if got := launchLines(runs.requests()); len(got) != 0 {
				t.Fatalf("launched %q, want nothing: an automation whose cron does not parse never fires", got)
			}
			if claims := repo.claimsMade(); len(claims) != 0 {
				t.Errorf("claims = %v, want none", claims)
			}
			offs := repo.switchOffsMade()
			if len(offs) != 1 || offs[0].id != "au-bad" || !offs[0].won {
				t.Fatalf("switch-offs = %v, want one of au-bad, won", offs)
			}
			within(t, "the switch-off's time", offs[0].at, before, after)
			stored := repo.stored(t, "au-bad")
			if stored.Enabled || stored.NextRunAt != nil || stored.LastRunAt != nil {
				t.Errorf("after the switch-off: enabled %v, next_run_at %s, last_run_at %s; want switched off, "+
					"NULL and never run", stored.Enabled, timeOrDash(stored.NextRunAt), timeOrDash(stored.LastRunAt))
			}
			if got := logs.lines(); !equalLines(got, []string{logLine}) {
				t.Errorf("logged %q, want %q", got, []string{logLine})
			}

			s.tick()
			s.tick()
			if n := len(runs.requests()) + len(repo.claimsMade()) + len(repo.switchOffsMade()); n != 1 {
				t.Errorf("two more ticks made %d more launches, claims or switch-offs, want none: it is never due again",
					n-1)
			}
		})
	}

	t.Run("two schedulers racing for it", func(t *testing.T) {
		logs := captureLog(t)
		repo := newSchedRepo(scheduled("au-bad", "Broken schedule", cron, time.Now().Add(-time.Minute)))
		repo.barrier = &sync.WaitGroup{}
		repo.barrier.Add(2)
		runsA, runsB := newSchedRuns(), newSchedRuns()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); New(repo, runsA, crewGraphs()).tick() }()
		go func() { defer wg.Done(); New(repo, runsB, crewGraphs()).tick() }()
		wg.Wait()
		offs := repo.switchOffsMade()
		if len(offs) != 2 || !offs[0].won || offs[1].won {
			t.Errorf("switch-offs = %v, want one won and then one lost", offs)
		}
		if n := len(runsA.requests()) + len(runsB.requests()) + len(repo.claimsMade()); n != 0 {
			t.Errorf("the replicas launched or claimed %d times, want none", n)
		}
		if got := logs.lines(); !equalLines(got, []string{logLine}) {
			t.Errorf("logged %q, want the winner's line alone, %q", got, []string{logLine})
		}
	})
}

// TestResolveTarget pins ResolveTarget, which the scheduler, the trigger
// matcher and run-now each call to find whom a run goes to: an agent target
// wins (a crew named beside it is ignored, and not returned); otherwise a
// crew's entry node's agent, with the crew's id and the node's; and the two
// sentinel errors, or the crew lookup's own error, when there is no one.
func TestResolveTarget(t *testing.T) {
	str := func(s string) *string { return &s }
	lookupErr := errors.New("team crew-gone not found")
	cases := []struct {
		name               string
		agent, team        *string
		wantAgent          string
		wantTeam, wantNode string
		wantErr            error
		wantErrText        string
		wantCrewLookup     bool
	}{
		{name: "an agent", agent: str("agent-1"), wantAgent: "agent-1"},
		{name: "an agent and a crew: the agent", agent: str("agent-1"), team: str("crew-1"), wantAgent: "agent-1"},
		{name: "an empty agent id and a crew: the crew", agent: str(""), team: str("crew-1"), wantAgent: "agent-entry",
			wantTeam: "crew-1", wantNode: "node-entry", wantCrewLookup: true},
		{name: "a crew", team: str("crew-1"), wantAgent: "agent-entry", wantTeam: "crew-1", wantNode: "node-entry",
			wantCrewLookup: true},
		{name: "a crew with no entry node", team: str("crew-headless"), wantErr: ErrNoEntryNode,
			wantErrText: "team has no entry node", wantCrewLookup: true},
		{name: "a crew whose entry node is gone", team: str("crew-stale"), wantErr: ErrNoEntryNode,
			wantErrText: "team has no entry node", wantCrewLookup: true},
		{name: "a crew no row has", team: str("crew-gone"), wantErrText: lookupErr.Error(), wantCrewLookup: true},
		{name: "neither", wantErr: ErrNoTarget, wantErrText: "automation has neither agent nor team target"},
		{name: "an empty crew id", team: str(""), wantErr: ErrNoTarget,
			wantErrText: "automation has neither agent nor team target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			crews := crewGraphs()
			a := &automations.Automation{ID: "au-1", OrgID: "org-1", AgentID: tc.agent, TeamID: tc.team}
			agent, team, node, err := ResolveTarget(a, crews)
			if tc.wantErrText != "" {
				if err == nil || err.Error() != tc.wantErrText || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
					t.Fatalf("err = %v, want %q", err, tc.wantErrText)
				}
				if agent != "" || team != nil || node != nil {
					t.Errorf("with the error it also answered %q, %v, %v, want nothing", agent, team, node)
				}
			} else if err != nil {
				t.Fatalf("err = %v", err)
			}
			if agent != tc.wantAgent || derefOr(team, "") != tc.wantTeam || derefOr(node, "") != tc.wantNode {
				t.Errorf("= %q, crew %q, node %q; want %q, %q, %q", agent, derefOr(team, ""), derefOr(node, ""),
					tc.wantAgent, tc.wantTeam, tc.wantNode)
			}
			if asked := crews.crewsAsked(); (len(asked) == 1) != tc.wantCrewLookup || len(asked) > 1 {
				t.Errorf("crews looked up %q, want a lookup: %v", asked, tc.wantCrewLookup)
			}
		})
	}
}

// TestSchedulerHandsTheRepositoryUTC is #379 bug 133's unit pin (the
// database one is TestTheAutomationsStampTimesInUTC in
// internal/persistence/postgres): next_run_at and last_run_at are TIMESTAMP
// columns, which keep the wall clock they are sent, so every time the
// scheduler hands the repository is UTC whatever the server's zone. The due
// query of catch-up and of a tick, a claim's time and the next_run_at it
// advances to, a switch-off's time and a last_run_at stamp: on a server two
// hours east of UTC each was a local time, so a schedule was read in the
// server's zone and every stamp moved two hours on its way in.
func TestSchedulerHandsTheRepositoryUTC(t *testing.T) {
	captureLog(t)
	prev := time.Local
	time.Local = time.FixedZone("CEST", 2*60*60)
	t.Cleanup(func() { time.Local = prev })

	cron := quietCron(t)
	now := time.Now()
	atCatchUp := scheduled("au-1", "At catch-up", cron, now.Add(-time.Hour))
	atCatchUp.CatchUp = true
	repo := newSchedRepo(atCatchUp, scheduled("au-bad", "Broken", "nope", now.Add(-time.Minute)))
	runs := newSchedRuns()
	s := New(repo, runs, crewGraphs())
	s.catchUp()
	repo.add(scheduled("au-2", "On a tick", cron, time.Now().Add(-time.Second)))
	s.tick()

	utc := func(what string, at time.Time) {
		t.Helper()
		if at.Location() != time.UTC {
			t.Errorf("%s is %s, in %s; want UTC", what, at.Format(time.RFC3339Nano), at.Location())
		}
	}
	if lists := repo.listTimes(); len(lists) != 2 {
		t.Fatalf("due queries = %d, want catch-up's and the tick's", len(lists))
	} else {
		utc("catch-up's due query", lists[0])
		utc("a tick's due query", lists[1])
	}
	claims := repo.claimsMade()
	if len(claims) != 2 || !claims[0].won || !claims[1].won {
		t.Fatalf("claims = %v, want au-1's at catch-up and au-2's on the tick, both won", claims)
	}
	for _, c := range claims {
		utc(c.id+"'s claim", c.at)
		utc(c.id+"'s claimed next_run_at", *c.nextRun)
	}
	offs := repo.switchOffsMade()
	if len(offs) != 1 || !offs[0].won {
		t.Fatalf("switch-offs = %v, want au-bad's, won", offs)
	}
	utc("au-bad's switch-off", offs[0].at)
	stamps := repo.stampsMade()
	if len(stamps) != 2 {
		t.Fatalf("stamps = %v, want au-1's and au-2's", stamps)
	}
	for _, st := range stamps {
		utc(st.id+"'s last_run_at", st.at)
	}
}
