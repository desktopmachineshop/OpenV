package automation

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/events"
)

// The characterization of internal/automation's trigger matcher, refactor
// plan step S11 (services-4; OpenV REQ-24, an automation launched on a
// project event). It pins what the matcher does today, as cmd/server wires
// it (automation.NewTriggerMatcher(automationRepo, runService,
// teamService).Start(bus)): the event filter, the guards (the agent:
// self-trigger skip, the cooldown and the hourly cap, in that order), the
// prompt variables, and the run it launches and stamps. Workspace and
// project scope are TestTriggerMatcherKeepsWorkspacesApart's, beside it.
// The matcher reads the clock itself, so the times it passes on are pinned
// to the window of the call.

// TestMatcherEventFilters pins the event filter: every key must be in the
// event's payload, and its value must print (fmt %v) as the filter's does,
// so a filter compares strings. Filters come from jsonb, so a number is a
// float64, while publishers send Go values (string, int, bool and []string,
// S6's event_payload_types.txt): an integral filter number matches an int
// below a million, and from a million on it prints as 1e+06 and never
// matches; a number, a bool and their strings match each other; a null
// matches a nil value but not a missing key; and a list matches a []string
// with the same items. A filter that does not match consults no guard.
func TestMatcherEventFilters(t *testing.T) {
	cases := []struct {
		name    string
		filter  string
		payload map[string]interface{}
		fires   bool
	}{
		{"no filter", `{}`, map[string]interface{}{"artifact_type": "requirement"}, true},
		{"a string, equal", `{"artifact_type":"requirement"}`, map[string]interface{}{"artifact_type": "requirement"}, true},
		{"a string, in another case", `{"artifact_type":"requirement"}`, map[string]interface{}{"artifact_type": "Requirement"}, false},
		{"a string, key missing", `{"artifact_type":"requirement"}`, map[string]interface{}{"title": "requirement"}, false},
		{"an empty string, equal", `{"title":""}`, map[string]interface{}{"title": ""}, true},
		{"a string against an int", `{"version":"2"}`, map[string]interface{}{"version": 2}, true},
		{"a number against an equal int", `{"version":2}`, map[string]interface{}{"version": 2}, true},
		{"a number against another int", `{"version":2}`, map[string]interface{}{"version": 3}, false},
		{"2.0 against the int 2", `{"version":2.0}`, map[string]interface{}{"version": 2}, true},
		{"a fraction against an int", `{"version":2.5}`, map[string]interface{}{"version": 2}, false},
		{"a number against an equal float", `{"version":2}`, map[string]interface{}{"version": 2.0}, true},
		{"a number against its string", `{"version":2}`, map[string]interface{}{"version": "2"}, true},
		{"999999 against the int", `{"version":999999}`, map[string]interface{}{"version": 999999}, true},
		{"a million against the int", `{"version":1000000}`, map[string]interface{}{"version": 1000000}, false},
		{"a million against 1e+06", `{"version":1000000}`, map[string]interface{}{"version": "1e+06"}, true},
		{"true against true", `{"review_round":true}`, map[string]interface{}{"review_round": true}, true},
		{"true against false", `{"review_round":true}`, map[string]interface{}{"review_round": false}, false},
		{"false against false", `{"review_round":false}`, map[string]interface{}{"review_round": false}, true},
		{"false, key missing", `{"review_round":false}`, map[string]interface{}{}, false},
		{"the string true against true", `{"review_round":"true"}`, map[string]interface{}{"review_round": true}, true},
		{"true against the string true", `{"review_round":true}`, map[string]interface{}{"review_round": "true"}, true},
		{"two keys, both equal", `{"artifact_type":"requirement","review_round":true}`,
			map[string]interface{}{"artifact_type": "requirement", "review_round": true, "title": "T"}, true},
		{"two keys, one differs", `{"artifact_type":"requirement","review_round":true}`,
			map[string]interface{}{"artifact_type": "requirement", "review_round": false}, false},
		{"null against nil", `{"from":null}`, map[string]interface{}{"from": nil}, true},
		{"null, key missing", `{"from":null}`, map[string]interface{}{}, false},
		{"a list against a []string", `{"successors":["a","b"]}`, map[string]interface{}{"successors": []string{"a", "b"}}, true},
		{"a Stringer against its text", `{"stamp":"stamp 7"}`, map[string]interface{}{"stamp": stamp("7")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			a := onArtifacts("au-1", "Filtered")
			a.EventFilter = filterJSON(t, tc.filter)
			a.MaxRunsPerHour = 10
			runs := newMatcherRuns()
			NewTriggerMatcher(newMatcherRepo(a), runs, matcherTeams{}).handle(artifactEvent("user:u-1", tc.payload))
			if fired := len(runs.requests()) == 1; fired != tc.fires || len(runs.requests()) > 1 {
				t.Errorf("filter %s on payload %v: launched %d runs, want fired=%v", tc.filter, tc.payload,
					len(runs.requests()), tc.fires)
			}
			if asked := runs.countsAsked(); tc.fires != (len(asked) == 1) {
				t.Errorf("hourly-cap counts asked %v, want one only when the filter matched", asked)
			}
			if got := logs.lines(); len(got) != 0 {
				t.Errorf("logged %q, want nothing", got)
			}
		})
	}
}

// TestMatcherSelfTriggerSkip pins the loop guard, the first guard: an
// automation does not fire on an event whose actor is agent:<run> when that
// run is the automation's own (its automation_id; run-now's runs carry it
// too). It looks the run up only for an agent: actor, and a run it cannot
// find, one of another automation or one of none does not hold it back. The
// skip is per automation: another automation fires on the same event. A
// skipped automation asks no further guard and is not stamped.
func TestMatcherSelfTriggerSkip(t *testing.T) {
	runsByID := map[string]*agentruns.Run{
		"run-own":     {ID: "run-own", OrgID: "org-1", AutomationID: strp("au-1")},
		"run-sibling": {ID: "run-sibling", OrgID: "org-1", AutomationID: strp("au-2")},
		"run-manual":  {ID: "run-manual", OrgID: "org-1"},
	}
	cases := []struct {
		actor string
		fires bool
		asks  []string // the runs looked up
	}{
		{"agent:run-own", false, []string{"run-own"}},
		{"agent:run-sibling", true, []string{"run-sibling"}},
		{"agent:run-manual", true, []string{"run-manual"}},
		{"agent:run-gone", true, []string{"run-gone"}},
		{"agent:", true, []string{""}},
		{"user:u-1", true, nil},
		{"system", true, nil},
		{"Agent:run-own", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.actor, func(t *testing.T) {
			captureSlog(t)
			a := onArtifacts("au-1", "Self")
			a.MaxRunsPerHour = 10
			repo := newMatcherRepo(a)
			runs := newMatcherRuns()
			runs.byID = runsByID
			NewTriggerMatcher(repo, runs, matcherTeams{}).handle(artifactEvent(tc.actor, nil))
			if fired := len(runs.requests()) == 1; fired != tc.fires {
				t.Errorf("launched %d runs, want fired=%v", len(runs.requests()), tc.fires)
			}
			if got := runs.runsAsked(); !equalLines(got, tc.asks) {
				t.Errorf("runs looked up %q, want %q", got, tc.asks)
			}
			if !tc.fires && (len(runs.countsAsked()) != 0 || len(repo.marksMade()) != 0) {
				t.Errorf("a skipped automation asked the hourly count %v and was stamped %v", runs.countsAsked(),
					repo.marksMade())
			}
		})
	}

	t.Run("the skip is per automation", func(t *testing.T) {
		captureSlog(t)
		runs := newMatcherRuns()
		runs.byID = runsByID
		NewTriggerMatcher(newMatcherRepo(onArtifacts("au-1", "Own"), onArtifacts("au-2", "Other")), runs,
			matcherTeams{}).handle(artifactEvent("agent:run-own", nil))
		if got := launchLines(runs.requests()); len(got) != 1 || *runs.requests()[0].AutomationID != "au-2" {
			t.Errorf("launched %q, want au-2's run alone", got)
		}
		if got := runs.runsAsked(); !equalLines(got, []string{"run-own", "run-own"}) {
			t.Errorf("runs looked up %q, want run-own once per automation", got)
		}
	})
}

// TestMatcherCooldown pins the second guard: an automation with a cooldown
// above 0 and a last_run_at does not fire until the cooldown has passed
// since that time (time.Since, so a last_run_at in the future holds it back
// too); a cooldown of 0 or below, or no last_run_at, never holds it back. A
// firing stamps last_run_at, so with a cooldown the next event at once is
// held back. A held-back automation asks no hourly count.
func TestMatcherCooldown(t *testing.T) {
	ago := func(d time.Duration) *time.Time { at := time.Now().Add(-d); return &at }
	cases := []struct {
		name     string
		lastRun  *time.Time
		cooldown int
		fires    bool
	}{
		{"no last run", nil, 3600, true},
		{"within the cooldown", ago(30 * time.Minute), 3600, false},
		{"past the cooldown", ago(2 * time.Hour), 3600, true},
		{"a last run to come", ago(-time.Hour), 3600, false},
		{"no cooldown", ago(0), 0, true},
		{"a negative cooldown", ago(0), -60, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureSlog(t)
			a := onArtifacts("au-1", "Cooling")
			a.LastRunAt, a.CooldownSeconds, a.MaxRunsPerHour = tc.lastRun, tc.cooldown, 10
			runs := newMatcherRuns()
			NewTriggerMatcher(newMatcherRepo(a), runs, matcherTeams{}).handle(artifactEvent("user:u-1", nil))
			if fired := len(runs.requests()) == 1; fired != tc.fires {
				t.Errorf("launched %d runs, want fired=%v", len(runs.requests()), tc.fires)
			}
			if asked := runs.countsAsked(); tc.fires != (len(asked) == 1) {
				t.Errorf("hourly counts asked %v, want one only when the cooldown let it through", asked)
			}
		})
	}

	for _, tc := range []struct {
		cooldown int
		fires    int
	}{{3600, 1}, {0, 2}} {
		t.Run(fmt.Sprintf("two events at once, cooldown %d s", tc.cooldown), func(t *testing.T) {
			captureSlog(t)
			a := onArtifacts("au-1", "Cooling")
			a.CooldownSeconds = tc.cooldown
			repo := newMatcherRepo(a)
			runs := newMatcherRuns()
			m := NewTriggerMatcher(repo, runs, matcherTeams{})
			m.handle(artifactEvent("user:u-1", nil))
			m.handle(artifactEvent("user:u-1", nil))
			if n := len(runs.requests()); n != tc.fires {
				t.Errorf("cooldown %d s: launched %d runs, want %d (the first firing's stamp is what the cooldown reads)",
					tc.cooldown, n, tc.fires)
			}
			if n := len(repo.marksMade()); n != tc.fires {
				t.Errorf("stamped last_run_at %d times, want once per run", n)
			}
		})
	}
}

// TestMatcherHourlyCap pins the third guard: an automation with
// max_runs_per_hour above 0 asks the run service how many runs it has had
// since an hour before now (agentruns' CountRunsSince, by automation id),
// and does not fire once that count reaches the cap, logging one line at
// Info; a cap of 0 or below asks nothing, and a count the service cannot
// give does not hold it back.
func TestMatcherHourlyCap(t *testing.T) {
	const capped = `level=INFO msg="triggers: automation hit max_runs_per_hour" automation_id=au-1 max_runs_per_hour=3`
	cases := []struct {
		name    string
		cap     int
		count   int
		err     error
		fires   bool
		counted bool
		logs    []string
	}{
		{name: "below the cap", cap: 3, count: 2, fires: true, counted: true},
		{name: "at the cap", cap: 3, count: 3, counted: true, logs: []string{capped}},
		{name: "over the cap", cap: 3, count: 7, counted: true, logs: []string{capped}},
		{name: "a count that fails", cap: 3, count: 9, err: errors.New("timeout"), fires: true, counted: true},
		{name: "no cap", cap: 0, count: 99, fires: true},
		{name: "a negative cap", cap: -1, count: 99, fires: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			a := onArtifacts("au-1", "Capped")
			a.MaxRunsPerHour = tc.cap
			runs := newMatcherRuns()
			runs.counts, runs.countErr = map[string]int{"au-1": tc.count}, tc.err
			before := time.Now()
			NewTriggerMatcher(newMatcherRepo(a), runs, matcherTeams{}).handle(artifactEvent("user:u-1", nil))
			after := time.Now()
			if fired := len(runs.requests()) == 1; fired != tc.fires {
				t.Errorf("launched %d runs, want fired=%v", len(runs.requests()), tc.fires)
			}
			asked := runs.countsAsked()
			if (len(asked) == 1) != tc.counted || len(asked) > 1 {
				t.Fatalf("counts asked %v, want one: %v", asked, tc.counted)
			}
			if tc.counted {
				if asked[0].automationID != "au-1" {
					t.Errorf("counted the runs of %q, want au-1's", asked[0].automationID)
				}
				within(t, "the count's start", asked[0].since, before.Add(-time.Hour), after.Add(-time.Hour))
			}
			if got := logs.lines(); !equalLines(got, tc.logs) {
				t.Errorf("logged %q, want %q", got, tc.logs)
			}
		})
	}
}

// TestMatcherPromptVariables pins the prompt a triggered run gets: the
// template rendered with automation.name, event.type, event.entity_id,
// event.actor and project.id (the event's project), and event.<key> for
// each payload value that is a string, a fmt.Stringer, a float64, an int,
// an int64 or a bool (fmt %v, so 1.5e+06); any other value (an int32, a
// []string, nil) and any other placeholder render empty. A payload key
// named type, entity_id or actor overrides the event's own variable. When
// the render is empty or only whitespace, the prompt is a fixed sentence
// naming the automation (%q), the event type and the entity.
func TestMatcherPromptVariables(t *testing.T) {
	payload := map[string]interface{}{
		"title": "Pump spec", "version": 3, "size": int64(42), "ratio": 0.5, "big": 1500000.0,
		"review_round": true, "stamp": stamp("7"), "count32": int32(7), "successors": []string{"a"}, "none": nil,
	}
	const everything = "{{automation.name}}|{{event.type}}|{{event.entity_id}}|{{event.actor}}|{{project.id}}|" +
		"{{event.title}}|{{event.version}}|{{event.size}}|{{event.ratio}}|{{event.big}}|{{event.review_round}}|" +
		"{{event.stamp}}|{{event.count32}}|{{event.successors}}|{{event.none}}|{{event.missing}}|{{org.id}}|" +
		"{{event.id}}|{{event.project_id}}|{{automation.id}}"
	const fallback = `Automation "Pump \"watch\"" fired on event artifact.created (entity art-1). ` +
		"Investigate via your OpenV tools and act per your instructions."
	cases := []struct {
		name     string
		template string
		payload  map[string]interface{}
		want     string
	}{
		{"every variable", everything, payload,
			`Pump "watch"|artifact.created|art-1|user:u-1|proj-1|Pump spec|3|42|0.5|1.5e+06|true|stamp 7||||||||`},
		{"payload keys named like the event's variables", "{{event.type}} {{event.entity_id}} {{event.actor}}",
			map[string]interface{}{"type": "requirement", "entity_id": "req-9", "actor": 5}, "requirement req-9 5"},
		{"an empty template", "", payload, fallback},
		{"a template of whitespace", " \n\t", payload, fallback},
		{"a render of whitespace", "{{event.missing}} {{org.id}}", payload, fallback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureSlog(t)
			a := onArtifacts("au-1", `Pump "watch"`)
			a.PromptTemplate = tc.template
			runs := newMatcherRuns()
			NewTriggerMatcher(newMatcherRepo(a), runs, matcherTeams{}).handle(artifactEvent("user:u-1", tc.payload))
			reqs := runs.requests()
			if len(reqs) != 1 {
				t.Fatalf("launched %q, want one run", launchLines(reqs))
			}
			if reqs[0].Prompt != tc.want {
				t.Errorf("prompt = %q\nwant     %q", reqs[0].Prompt, tc.want)
			}
		})
	}
}

// TestMatcherLaunch pins the run a firing launches and what follows: the
// automation's workspace, its target (a crew's entry node's agent with the
// crew and node), the automation's project or, for one of the whole
// workspace, the event's (none when the event has none), the automation,
// the event as trigger, and no launcher or parent; then MarkRun stamps
// last_run_at (now) and passes the automation's next_run_at back as it was
// read. Automations fire in the order the repository lists them. A target
// it cannot resolve, a launch refused, a stamp refused and a failed query
// each log one line; the first two launch or stamp nothing.
func TestMatcherLaunch(t *testing.T) {
	t.Run("the run and the stamp", func(t *testing.T) {
		logs := captureSlog(t)
		pinned := onArtifacts("au-pinned", "Pinned")
		pinned.ProjectID = strp("proj-1")
		next := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		pinned.NextRunAt = &next
		crew := onArtifacts("au-crew", "Crew")
		crew.AgentID, crew.TeamID = nil, strp("crew-1")
		repo := newMatcherRepo(crew, pinned, onArtifacts("au-all", "All"))
		runs := newMatcherRuns()
		e := artifactEvent("user:u-1", nil)
		before := time.Now()
		NewTriggerMatcher(repo, runs, matcherTeams{}).handle(e)
		after := time.Now()

		prompt := func(name string) string {
			return `Automation "` + name + `" fired on event artifact.created (entity art-1). ` +
				"Investigate via your OpenV tools and act per your instructions."
		}
		want := []string{
			launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: "agent-entry", ProjectID: strp("proj-1"),
				AutomationID: strp("au-crew"), TriggerEventID: &e.ID, TeamID: strp("crew-1"),
				TeamNodeID: strp("node-entry"), Prompt: prompt("Crew")}),
			launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: "agent-au-pinned", ProjectID: strp("proj-1"),
				AutomationID: strp("au-pinned"), TriggerEventID: &e.ID, Prompt: prompt("Pinned")}),
			launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: "agent-au-all", ProjectID: strp("proj-1"),
				AutomationID: strp("au-all"), TriggerEventID: &e.ID, Prompt: prompt("All")}),
		}
		if got := launchLines(runs.requests()); !equalLines(got, want) {
			t.Fatalf("launched\n  %q\nwant\n  %q", got, want)
		}
		marks := repo.marksMade()
		if len(marks) != 3 || marks[0].id != "au-crew" || marks[1].id != "au-pinned" || marks[2].id != "au-all" {
			t.Fatalf("stamped %v, want au-crew, au-pinned and au-all in turn", marks)
		}
		for _, m := range marks {
			within(t, m.id+"'s last_run_at", m.lastRun, before, after)
		}
		if marks[1].nextRun == nil || !marks[1].nextRun.Equal(next) || marks[0].nextRun != nil {
			t.Errorf("next_run_at passed to MarkRun: %v and %v, want the automation's own (%s, and none)",
				marks[1].nextRun, marks[0].nextRun, next)
		}
		if got := logs.lines(); len(got) != 0 {
			t.Errorf("logged %q, want nothing", got)
		}
	})

	t.Run("an event with no project", func(t *testing.T) {
		captureSlog(t)
		pinned := onArtifacts("au-pinned", "Pinned")
		pinned.ProjectID = strp("proj-1")
		runs := newMatcherRuns()
		e := domainevents.New(domainevents.ArtifactCreated, "", "art-1", "user:u-1", nil).WithOrg("org-1")
		NewTriggerMatcher(newMatcherRepo(pinned, onArtifacts("au-all", "All")), runs, matcherTeams{}).handle(e)
		want := []string{launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: "agent-au-all",
			AutomationID: strp("au-all"), TriggerEventID: &e.ID,
			Prompt: `Automation "All" fired on event artifact.created (entity art-1). ` +
				"Investigate via your OpenV tools and act per your instructions."})}
		if got := launchLines(runs.requests()); !equalLines(got, want) {
			t.Errorf("launched\n  %q\nwant only the workspace automation's, with no project:\n  %q", got, want)
		}
	})

	failures := []struct {
		name    string
		setup   func(repo *matcherRepo, runs *matcherRuns)
		crew    string
		launch  bool
		stamped bool
		logs    []string
	}{
		{name: "a crew with no entry node", crew: "crew-headless",
			logs: []string{`level=WARN msg="triggers: automation target unresolvable" automation_id=au-1 error="team has no entry node"`}},
		{name: "a crew no row has", crew: "crew-gone",
			logs: []string{`level=WARN msg="triggers: automation target unresolvable" automation_id=au-1 error="team crew-gone not found"`}},
		{name: "a launch refused", setup: func(_ *matcherRepo, runs *matcherRuns) { runs.launchErr = errors.New("over budget") },
			launch: true,
			logs:   []string{`level=ERROR msg="triggers: failed to launch run for automation" automation_id=au-1 error="over budget"`}},
		{name: "a stamp refused", setup: func(repo *matcherRepo, _ *matcherRuns) { repo.markErr = errors.New("deadlock") },
			launch: true, stamped: true,
			logs: []string{`level=WARN msg="triggers: failed to stamp last_run_at" automation_id=au-1 error=deadlock`}},
		{name: "a failed query", setup: func(repo *matcherRepo, _ *matcherRuns) { repo.listErr = errors.New("connection refused") },
			logs: []string{`level=ERROR msg="triggers: automation query failed" event_type=artifact.created error="connection refused"`}},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			a := onArtifacts("au-1", "Failing")
			if tc.crew != "" {
				a.AgentID, a.TeamID = nil, strp(tc.crew)
			}
			repo := newMatcherRepo(a)
			runs := newMatcherRuns()
			if tc.setup != nil {
				tc.setup(repo, runs)
			}
			NewTriggerMatcher(repo, runs, matcherTeams{}).handle(artifactEvent("user:u-1", nil))
			if launched := len(runs.requests()) == 1; launched != tc.launch {
				t.Errorf("launch attempts = %d, want one: %v", len(runs.requests()), tc.launch)
			}
			if stamped := len(repo.marksMade()) == 1; stamped != tc.stamped {
				t.Errorf("stamps = %d, want one: %v", len(repo.marksMade()), tc.stamped)
			}
			if got := logs.lines(); !equalLines(got, tc.logs) {
				t.Errorf("logged %q, want %q", got, tc.logs)
			}
		})
	}
}

// TestMatcherSubscribesToTheBus pins Start as cmd/server calls it: the
// matcher subscribes to the event bus (internal/events' DefaultBus, which
// fills an event's workspace from its project and dispatches on a goroutine
// of its own), so an event published there fires a matching automation and
// stamps it.
func TestMatcherSubscribesToTheBus(t *testing.T) {
	captureSlog(t)
	a := onArtifacts("au-1", "On the bus")
	a.PromptTemplate = "Review {{event.title}} in {{project.id}}"
	repo := newMatcherRepo(a)
	runs := newMatcherRuns()
	bus := events.NewBus(nil, func(projectID string) string {
		if projectID == "proj-1" {
			return "org-1"
		}
		return ""
	})
	NewTriggerMatcher(repo, runs, matcherTeams{}).Start(bus)
	e := domainevents.New(domainevents.ArtifactCreated, "proj-1", "art-1", "user:u-1",
		map[string]interface{}{"title": "Pump spec"})
	bus.Publish(e)
	select {
	case req := <-runs.launches:
		want := launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: "agent-au-1", ProjectID: strp("proj-1"),
			AutomationID: strp("au-1"), TriggerEventID: &e.ID, Prompt: "Review Pump spec in proj-1"})
		if got := launchLine(req); got != want {
			t.Fatalf("launched\n  %s\nwant\n  %s", got, want)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the published event launched no run")
	}
	select {
	case m := <-repo.marked:
		if m.id != "au-1" {
			t.Errorf("stamped %s, want au-1", m.id)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the run launched was never stamped")
	}
}
