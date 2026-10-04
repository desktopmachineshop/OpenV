package scheduler

import (
	"bytes"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// The stand-ins of refactor plan S11's scheduler tests. Each embeds its
// interface, so a method the scheduler newly reaches panics loudly instead
// of answering wrongly. They are safe for concurrent use: two schedulers
// race on one repository, and Start's loop runs on its own goroutine.

// schedRepo is the automations repository as the scheduler reads and claims
// it, modelled on the SQL in internal/persistence/postgres's
// automation_repository.go: ListDueScheduled returns a snapshot of the
// enabled scheduled rows whose next_run_at is set and not after now, oldest
// first; ClaimDueScheduled is one atomic step that wins only while the row is
// still enabled, scheduled and due as of the claim's time, and then sets
// next_run_at to the time given, leaving last_run_at; SwitchOffScheduled is
// the same step under the same condition, which sets enabled to false and
// next_run_at to NULL instead; StampLastRun sets last_run_at alone.
// TestClaimDueScheduledIsExactlyOnce and its neighbours in that package pin
// the SQL side; TestSchedulersShareTheRealClaim there drives two real
// schedulers against it.
type schedRepo struct {
	automations.Repository

	mu         sync.Mutex
	rows       []*automations.Automation
	lists      int
	claims     []schedClaim
	switchOffs []schedClaim
	stamps     []schedStamp
	// listErr is ListDueScheduled's answer when set.
	listErr error
	// stampErr is StampLastRun's answer when set (changing nothing).
	stampErr error
	// claimFails answers an automation's claims and switch-offs with a
	// fixed result, changing nothing, as a statement that failed changes
	// nothing.
	claimFails map[string]claimResult
	// barrier, when set, holds each ListDueScheduled until that many calls
	// have read the due set, so that racing schedulers each hold the same
	// candidates before either claims one.
	barrier *sync.WaitGroup
	// listed receives one value per ListDueScheduled, never blocking.
	listed chan struct{}
}

type claimResult struct {
	won bool
	err error
}

// schedClaim is one ClaimDueScheduled or SwitchOffScheduled call and what it
// answered (a switch-off has no next_run_at).
type schedClaim struct {
	id      string
	at      time.Time
	nextRun *time.Time
	won     bool
}

func (c schedClaim) String() string {
	result := "lost"
	if c.won {
		result = "won"
	}
	return fmt.Sprintf("%s at %s, next_run_at %s: %s", c.id, c.at.Format(time.RFC3339Nano), timeOrDash(c.nextRun),
		result)
}

// schedStamp is one StampLastRun call.
type schedStamp struct {
	id string
	at time.Time
}

func newSchedRepo(rows ...*automations.Automation) *schedRepo {
	return &schedRepo{rows: rows, listed: make(chan struct{}, 1024)}
}

func (r *schedRepo) ListDueScheduled(now time.Time) ([]*automations.Automation, error) {
	r.mu.Lock()
	r.lists++
	var due []*automations.Automation
	if r.listErr == nil {
		for _, a := range r.rows {
			if a.Enabled && a.Kind == automations.KindScheduled && a.NextRunAt != nil && !a.NextRunAt.After(now) {
				due = append(due, cloneAutomation(a))
			}
		}
	}
	err := r.listErr
	barrier := r.barrier
	r.mu.Unlock()
	sort.SliceStable(due, func(i, j int) bool { return due[i].NextRunAt.Before(*due[j].NextRunAt) })
	if barrier != nil {
		barrier.Done()
		barrier.Wait()
	}
	select {
	case r.listed <- struct{}{}:
	default:
	}
	if err != nil {
		return nil, err
	}
	return due, nil
}

func (r *schedRepo) ClaimDueScheduled(id string, now time.Time, nextRun time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := nextRun
	if fail, ok := r.claimFails[id]; ok {
		r.claims = append(r.claims, schedClaim{id: id, at: now, nextRun: &next, won: fail.won})
		return fail.won, fail.err
	}
	row := r.row(id)
	won := row != nil && row.Enabled && row.Kind == automations.KindScheduled && row.NextRunAt != nil &&
		!row.NextRunAt.After(now)
	if won {
		stored := next
		row.NextRunAt = &stored
	}
	r.claims = append(r.claims, schedClaim{id: id, at: now, nextRun: &next, won: won})
	return won, nil
}

func (r *schedRepo) SwitchOffScheduled(id string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if fail, ok := r.claimFails[id]; ok {
		r.switchOffs = append(r.switchOffs, schedClaim{id: id, at: now, won: fail.won})
		return fail.won, fail.err
	}
	row := r.row(id)
	won := row != nil && row.Enabled && row.Kind == automations.KindScheduled && row.NextRunAt != nil &&
		!row.NextRunAt.After(now)
	if won {
		row.Enabled, row.NextRunAt = false, nil
	}
	r.switchOffs = append(r.switchOffs, schedClaim{id: id, at: now, won: won})
	return won, nil
}

func (r *schedRepo) StampLastRun(id string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stamps = append(r.stamps, schedStamp{id: id, at: at})
	if r.stampErr != nil {
		return r.stampErr
	}
	if row := r.row(id); row != nil {
		stamped := at
		row.LastRunAt = &stamped
	}
	return nil
}

// row is the stored automation with that id; the caller holds mu.
func (r *schedRepo) row(id string) *automations.Automation {
	for _, a := range r.rows {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// stored is a copy of the stored automation with that id.
func (r *schedRepo) stored(t *testing.T, id string) *automations.Automation {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.row(id)
	if a == nil {
		t.Fatalf("no automation %s is stored", id)
	}
	return cloneAutomation(a)
}

func (r *schedRepo) add(a *automations.Automation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, a)
}

// claimsMade is a copy of the claims so far, in order.
func (r *schedRepo) claimsMade() []schedClaim {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]schedClaim(nil), r.claims...)
}

// stampsMade is a copy of the last_run_at stamps so far, in order.
func (r *schedRepo) stampsMade() []schedStamp {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]schedStamp(nil), r.stamps...)
}

// switchOffsMade is a copy of the switch-offs so far, in order.
func (r *schedRepo) switchOffsMade() []schedClaim {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]schedClaim(nil), r.switchOffs...)
}

func (r *schedRepo) listCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lists
}

// cloneAutomation copies an automation and the times it points to, as a
// database read hands out values the next write does not change.
func cloneAutomation(a *automations.Automation) *automations.Automation {
	c := *a
	if a.NextRunAt != nil {
		n := *a.NextRunAt
		c.NextRunAt = &n
	}
	if a.LastRunAt != nil {
		l := *a.LastRunAt
		c.LastRunAt = &l
	}
	return &c
}

// schedRuns records the runs the scheduler launches.
type schedRuns struct {
	agentruns.Service

	mu       sync.Mutex
	launched []agentruns.LaunchRequest
	// launchErr is Launch's answer, after recording the request, when set.
	launchErr error
	// launches receives each request, never blocking.
	launches chan agentruns.LaunchRequest
}

func newSchedRuns() *schedRuns {
	return &schedRuns{launches: make(chan agentruns.LaunchRequest, 1024)}
}

func (f *schedRuns) Launch(req agentruns.LaunchRequest) (*agentruns.Run, string, error) {
	f.mu.Lock()
	f.launched = append(f.launched, req)
	err := f.launchErr
	f.mu.Unlock()
	select {
	case f.launches <- req:
	default:
	}
	if err != nil {
		return nil, "", err
	}
	return &agentruns.Run{ID: "run-" + derefOr(req.AutomationID, "none"), OrgID: req.OrgID, AgentID: req.AgentID,
		ProjectID: req.ProjectID, AutomationID: req.AutomationID, Status: agentruns.StatusQueued}, "run-token", nil
}

func (f *schedRuns) requests() []agentruns.LaunchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agentruns.LaunchRequest(nil), f.launched...)
}

// schedTeams serves crew graphs and records the crews asked for.
type schedTeams struct {
	teams.Service

	mu     sync.Mutex
	graphs map[string]*teams.TeamGraph
	asked  []string
}

func (f *schedTeams) GetTeam(id string) (*teams.TeamGraph, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	if g, ok := f.graphs[id]; ok {
		return g, nil
	}
	return nil, fmt.Errorf("team %s not found", id)
}

func (f *schedTeams) crewsAsked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// crewGraphs is crew-1, whose entry node node-entry is agent-entry's (a
// second node, node-review, is agent-review's); crew-headless, which has no
// entry node; and crew-stale, whose entry node names a node the crew no
// longer has (as when the entry agent was deleted and its node cascaded
// away, refactor plan S5d).
func crewGraphs() *schedTeams {
	entry, stale := "node-entry", "node-gone"
	return &schedTeams{graphs: map[string]*teams.TeamGraph{
		"crew-1": {Team: &teams.Team{ID: "crew-1", OrgID: "org-1", Name: "Crew", EntryNodeID: &entry},
			Nodes: []*teams.Node{
				{ID: "node-review", TeamID: "crew-1", NodeType: teams.NodeAgent, AgentID: "agent-review"},
				{ID: entry, TeamID: "crew-1", NodeType: teams.NodeAgent, AgentID: "agent-entry"},
			}},
		"crew-headless": {Team: &teams.Team{ID: "crew-headless", OrgID: "org-1", Name: "Headless"},
			Nodes: []*teams.Node{{ID: "node-a", TeamID: "crew-headless", NodeType: teams.NodeAgent, AgentID: "agent-a"}}},
		"crew-stale": {Team: &teams.Team{ID: "crew-stale", OrgID: "org-1", Name: "Stale", EntryNodeID: &stale},
			Nodes: []*teams.Node{{ID: "node-b", TeamID: "crew-stale", NodeType: teams.NodeAgent, AgentID: "agent-b"}}},
	}}
}

// scheduled is an enabled scheduled automation of workspace org-1 pinned to
// proj-1, targeting the agent agent-<id>, with the cron expression and the
// next_run_at given, the defaults a create stores (60 s cooldown, 10 runs an
// hour, which the scheduler never reads) and catch_up off.
func scheduled(id, name, cron string, next time.Time) *automations.Automation {
	agent, project := "agent-"+id, "proj-1"
	return &automations.Automation{ID: id, OrgID: "org-1", Name: name, AgentID: &agent, ProjectID: &project,
		Kind: automations.KindScheduled, Enabled: true, CronExpr: cron, NextRunAt: &next,
		EventFilter: map[string]interface{}{}, CooldownSeconds: 60, MaxRunsPerHour: 10}
}

// launchLine prints every field of a launch request, pointers dereferenced
// and nil as -, so a test compares the whole request and a field added to
// agentruns.LaunchRequest shows up in every expectation.
func launchLine(req agentruns.LaunchRequest) string {
	v := reflect.ValueOf(req)
	var parts []string
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		s := "-"
		switch {
		case f.Kind() == reflect.Pointer && f.IsNil():
		case f.Kind() == reflect.Pointer:
			s = fmt.Sprintf("%q", fmt.Sprint(f.Elem().Interface()))
		case f.Kind() == reflect.String:
			s = fmt.Sprintf("%q", f.String())
		default:
			s = fmt.Sprint(f.Interface())
		}
		parts = append(parts, v.Type().Field(i).Name+"="+s)
	}
	return strings.Join(parts, " ")
}

func launchLines(reqs []agentruns.LaunchRequest) []string {
	var out []string
	for _, r := range reqs {
		out = append(out, launchLine(r))
	}
	return out
}

// wantLaunch is launchLine of the request a scheduled firing of an
// automation built by scheduled() makes, with the prompt given: the
// automation's workspace, agent, project and id, and nothing else (no
// trigger event, launcher or parent, priority 0).
func wantLaunch(id, prompt string) string {
	agent, project, automation := "agent-"+id, "proj-1", id
	return launchLine(agentruns.LaunchRequest{OrgID: "org-1", AgentID: agent, ProjectID: &project,
		AutomationID: &automation, Prompt: prompt})
}

func derefOr(s *string, none string) string {
	if s == nil {
		return none
	}
	return *s
}

// schedLog captures what the scheduler logs through the standard log
// package, without the timestamp, one entry per line.
type schedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *schedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *schedLog) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func captureLog(t *testing.T) *schedLog {
	t.Helper()
	logs := &schedLog{}
	prevOut, prevFlags, prevPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	})
	return logs
}

func equalLines(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// within fails unless at lies in [before, after]: the scheduler reads the
// clock itself (time.Now), so a time it stamps is pinned to the window of
// the call that stamped it.
func within(t *testing.T, what string, at, before, after time.Time) {
	t.Helper()
	if at.Before(before) || at.After(after) {
		t.Errorf("%s = %s, want within the call, [%s, %s]", what, at.Format(time.RFC3339Nano),
			before.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano))
	}
}

func nextAfter(t *testing.T, cron string, at time.Time) time.Time {
	t.Helper()
	next, err := automations.NextAfter(cron, at)
	if err != nil {
		t.Fatalf("NextAfter(%q): %v", cron, err)
	}
	return next
}

// quietCron is a cron expression with no occurrence in the next hour, so a
// row a claim advances with it cannot fall due again while a test runs,
// however close to an occurrence the test starts (with "*/5 * * * *", a
// claim a millisecond before an occurrence would leave the row due at the
// next tick). New Year's Day and the first of July are never both within an
// hour.
func quietCron(t *testing.T) string {
	t.Helper()
	for _, cron := range []string{"0 0 1 1 *", "0 0 1 7 *"} {
		if now := time.Now(); nextAfter(t, cron, now).Sub(now) > time.Hour {
			return cron
		}
	}
	t.Fatal("both quiet crons fall due within the hour")
	return ""
}

func timeOrDash(at *time.Time) string {
	if at == nil {
		return "-"
	}
	return at.Format(time.RFC3339Nano)
}
