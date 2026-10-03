package automation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// The stand-ins of refactor plan S11's trigger-matcher tests. Each embeds
// its interface, so a method the matcher newly reaches panics loudly instead
// of answering wrongly. They are safe for concurrent use, since the bus
// calls the matcher from a goroutine of its own.

// matcherRepo is the automations repository as the matcher reads and stamps
// it, modelled on internal/persistence/postgres's automation_repository.go:
// ListEnabledTriggered returns a snapshot of the enabled triggered rows of
// the event type, in creation order, from every workspace; MarkRun sets
// last_run_at and next_run_at.
type matcherRepo struct {
	automations.Repository

	mu    sync.Mutex
	rows  []*automations.Automation
	marks []matcherMark
	// listErr and markErr are the answers of ListEnabledTriggered and
	// MarkRun when set (a failed MarkRun changes nothing).
	listErr, markErr error
	// marked receives each MarkRun, never blocking.
	marked chan matcherMark
}

// matcherMark is one MarkRun call.
type matcherMark struct {
	id      string
	lastRun time.Time
	nextRun *time.Time
}

func (m matcherMark) String() string {
	next := "-"
	if m.nextRun != nil {
		next = m.nextRun.Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%s at %s, next_run_at %s", m.id, m.lastRun.Format(time.RFC3339Nano), next)
}

func newMatcherRepo(rows ...*automations.Automation) *matcherRepo {
	return &matcherRepo{rows: rows, marked: make(chan matcherMark, 1024)}
}

func (r *matcherRepo) ListEnabledTriggered(eventType string) ([]*automations.Automation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*automations.Automation
	for _, a := range r.rows {
		if a.Enabled && a.Kind == automations.KindTriggered && a.EventType == eventType {
			c := *a
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *matcherRepo) MarkRun(id string, lastRun time.Time, nextRun *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := matcherMark{id: id, lastRun: lastRun, nextRun: nextRun}
	r.marks = append(r.marks, m)
	select {
	case r.marked <- m:
	default:
	}
	if r.markErr != nil {
		return r.markErr
	}
	for _, a := range r.rows {
		if a.ID == id {
			last := lastRun
			a.LastRunAt, a.NextRunAt = &last, nextRun
		}
	}
	return nil
}

func (r *matcherRepo) marksMade() []matcherMark {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]matcherMark(nil), r.marks...)
}

// matcherRuns records the launches, answers Get from byID (and records each
// id asked for) and CountRunsSince from counts (and records each call).
type matcherRuns struct {
	agentruns.Service

	mu        sync.Mutex
	launched  []agentruns.LaunchRequest
	launchErr error
	byID      map[string]*agentruns.Run
	gets      []string
	counts    map[string]int
	countErr  error
	countArgs []countCall
	// launches receives each request, never blocking.
	launches chan agentruns.LaunchRequest
}

// countCall is one CountRunsSince call.
type countCall struct {
	automationID string
	since        time.Time
}

func (c countCall) String() string {
	return fmt.Sprintf("%s since %s", c.automationID, c.since.Format(time.RFC3339Nano))
}

func newMatcherRuns() *matcherRuns {
	return &matcherRuns{launches: make(chan agentruns.LaunchRequest, 1024)}
}

func (f *matcherRuns) Launch(req agentruns.LaunchRequest) (*agentruns.Run, string, error) {
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
	return &agentruns.Run{ID: "run-new", OrgID: req.OrgID, AgentID: req.AgentID, ProjectID: req.ProjectID,
		AutomationID: req.AutomationID, Status: agentruns.StatusQueued}, "run-token", nil
}

func (f *matcherRuns) Get(id string) (*agentruns.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, id)
	if run, ok := f.byID[id]; ok {
		return run, nil
	}
	return nil, agentruns.ErrNotFound
}

func (f *matcherRuns) CountRunsSince(automationID string, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countArgs = append(f.countArgs, countCall{automationID, since})
	return f.counts[automationID], f.countErr
}

func (f *matcherRuns) requests() []agentruns.LaunchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agentruns.LaunchRequest(nil), f.launched...)
}

func (f *matcherRuns) runsAsked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.gets...)
}

func (f *matcherRuns) countsAsked() []countCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]countCall(nil), f.countArgs...)
}

// matcherTeams serves crew-1, whose entry node node-entry is agent-entry's,
// and crew-headless, which has no entry node.
type matcherTeams struct{ teams.Service }

func (matcherTeams) GetTeam(id string) (*teams.TeamGraph, error) {
	entry := "node-entry"
	switch id {
	case "crew-1":
		return &teams.TeamGraph{Team: &teams.Team{ID: id, OrgID: "org-1", EntryNodeID: &entry},
			Nodes: []*teams.Node{{ID: entry, TeamID: id, NodeType: teams.NodeAgent, AgentID: "agent-entry"}}}, nil
	case "crew-headless":
		return &teams.TeamGraph{Team: &teams.Team{ID: id, OrgID: "org-1"}}, nil
	}
	return nil, fmt.Errorf("team %s not found", id)
}

// onArtifacts is an enabled triggered automation of workspace org-1 on
// artifact.created for the whole workspace, targeting the agent agent-<id>,
// with an empty filter and no cooldown or hourly cap, so that each test
// turns on only the guard it pins.
func onArtifacts(id, name string) *automations.Automation {
	agent := "agent-" + id
	return &automations.Automation{ID: id, OrgID: "org-1", Name: name, AgentID: &agent,
		Kind: automations.KindTriggered, Enabled: true, EventType: domainevents.ArtifactCreated,
		EventFilter: map[string]interface{}{}}
}

// artifactEvent is an artifact.created event in project proj-1 of org-1,
// on art-1, by the actor given.
func artifactEvent(actor string, payload map[string]interface{}) domainevents.Event {
	return domainevents.New(domainevents.ArtifactCreated, "proj-1", "art-1", actor, payload).WithOrg("org-1")
}

// filterJSON decodes an event filter as the repository does (jsonb into
// map[string]interface{}), so numbers are float64.
func filterJSON(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var f map[string]interface{}
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		t.Fatalf("filter %s: %v", s, err)
	}
	return f
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

func strp(s string) *string { return &s }

// matcherLog captures what the matcher logs through log/slog, as text
// without the time, one record per line.
type matcherLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *matcherLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *matcherLog) lines() []string {
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

// captureSlog points slog's default at a text handler that writes to the
// returned log, at every level. slog.SetDefault also points the log package
// at the new handler, and setting the old one back does not undo that, so
// both are restored.
func captureSlog(t *testing.T) *matcherLog {
	t.Helper()
	logs := &matcherLog{}
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return logs
}

// within fails unless at lies in [before, after]: the matcher reads the
// clock itself (time.Now), so a time it passes on is pinned to the window
// of the call.
func within(t *testing.T, what string, at, before, after time.Time) {
	t.Helper()
	if at.Before(before) || at.After(after) {
		t.Errorf("%s = %s, want within the call, [%s, %s]", what, at.Format(time.RFC3339Nano),
			before.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano))
	}
}

// stamp is a payload value of a type of its own that implements
// fmt.Stringer, which the matcher renders through String.
type stamp string

func (s stamp) String() string { return "stamp " + string(s) }
