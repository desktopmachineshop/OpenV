package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/scheduler"
)

// TriggerMatcher subscribes to the event bus and launches runs for matching
// triggered automations.
type TriggerMatcher struct {
	repo        automations.Repository
	runService  agentruns.Service
	teamService teams.Service
}

// NewTriggerMatcher creates a matcher.
func NewTriggerMatcher(repo automations.Repository, runService agentruns.Service, teamService teams.Service) *TriggerMatcher {
	return &TriggerMatcher{repo: repo, runService: runService, teamService: teamService}
}

// Start subscribes to the bus.
func (m *TriggerMatcher) Start(bus domainevents.Bus) {
	bus.Subscribe(m.handle)
}

func (m *TriggerMatcher) handle(e domainevents.Event) {
	list, err := m.repo.ListEnabledTriggered(e.EventType)
	if err != nil {
		slog.Error("triggers: automation query failed",
			slog.String("event_type", e.EventType),
			slog.Any("error", err))
		return
	}
	for _, a := range list {
		if m.matches(a, e) && m.passesGuards(a, e) {
			m.fire(a, e)
		}
	}
}

func (m *TriggerMatcher) matches(a *automations.Automation, e domainevents.Event) bool {
	// Workspace scope: an automation sees only its own workspace's events
	// (the bus stamps OrgID from the event's project where the publisher did
	// not), so a workspace-wide automation's run, which takes the event's
	// project, stays in its workspace. An event with no workspace fires none.
	if a.OrgID == "" || e.OrgID != a.OrgID {
		return false
	}
	// Project scope.
	if a.ProjectID != nil && *a.ProjectID != "" && *a.ProjectID != e.ProjectID {
		return false
	}
	// Event filter: flat equality against payload values.
	for key, want := range a.EventFilter {
		got, ok := e.Payload[key]
		if !ok || !filterValueMatches(got, want) {
			return false
		}
	}
	return true
}

// filterValueMatches reports whether a payload value matches an event
// filter's. Two numbers match when they are equal as numbers: a filter's
// number comes from JSON as a float64 and a publisher's as a Go integer, so
// comparing their text would miss a filter's 1000000, which prints as
// 1e+06, against the int 1000000. Any other pair, and two numbers that are
// not equal as numbers, match when they print alike (fmt %v), so strings,
// bools, null and lists compare as text, a number matches its string, and a
// float32 matches the filter number it prints as.
func filterValueMatches(got, want interface{}) bool {
	if g, ok := asNumber(got); ok {
		if w, ok := asNumber(want); ok && g == w {
			return true
		}
	}
	return fmt.Sprintf("%v", got) == fmt.Sprintf("%v", want)
}

// asNumber is v as a float64, JSON's number, when v is a Go integer or float
// of any size (a named type too) or a json.Number.
func asNumber(v interface{}) (float64, bool) {
	if n, ok := v.(json.Number); ok {
		f, err := n.Float64()
		return f, err == nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// passesGuards applies the loop guard, the cooldown and the hourly cap, in
// that order. A guard whose lookup fails holds the automation back (fail
// closed) and logs why: it cannot tell whether firing would loop or run
// over the cap. A run no row has is no failure: it is not this
// automation's own.
func (m *TriggerMatcher) passesGuards(a *automations.Automation, e domainevents.Event) bool {
	// Self-trigger loop guard: skip events produced by this automation's own runs.
	if strings.HasPrefix(e.Actor, "agent:") {
		runID := strings.TrimPrefix(e.Actor, "agent:")
		run, err := m.runService.Get(runID)
		if err != nil && !errors.Is(err, agentruns.ErrNotFound) {
			slog.Warn("triggers: automation skipped: self-trigger check failed",
				slog.String("automation_id", a.ID),
				slog.String("run_id", runID),
				slog.Any("error", err))
			return false
		}
		if err == nil && run != nil && run.AutomationID != nil && *run.AutomationID == a.ID {
			return false
		}
	}

	// Cooldown.
	if a.LastRunAt != nil && a.CooldownSeconds > 0 {
		if time.Since(*a.LastRunAt) < time.Duration(a.CooldownSeconds)*time.Second {
			return false
		}
	}

	// Hourly rate cap.
	if a.MaxRunsPerHour > 0 {
		count, err := m.runService.CountRunsSince(a.ID, time.Now().UTC().Add(-time.Hour))
		if err != nil {
			slog.Warn("triggers: automation skipped: hourly run count failed",
				slog.String("automation_id", a.ID),
				slog.Any("error", err))
			return false
		}
		if count >= a.MaxRunsPerHour {
			slog.Info("triggers: automation hit max_runs_per_hour",
				slog.String("automation_id", a.ID),
				slog.Int("max_runs_per_hour", a.MaxRunsPerHour))
			return false
		}
	}
	return true
}

func (m *TriggerMatcher) fire(a *automations.Automation, e domainevents.Event) {
	agentID, teamID, teamNodeID, err := scheduler.ResolveTarget(a, m.teamService)
	if err != nil {
		slog.Warn("triggers: automation target unresolvable",
			slog.String("automation_id", a.ID),
			slog.Any("error", err))
		return
	}

	// Lean-context rendering: identifiers and event metadata only. The
	// payload's values go in first, so that the event's own variables,
	// set after them, win over a payload key named type, entity_id or actor.
	vars := map[string]string{}
	for key, value := range e.Payload {
		switch v := value.(type) {
		case string:
			vars["event."+key] = v
		case fmt.Stringer:
			vars["event."+key] = v.String()
		case float64, int, int64, bool:
			vars["event."+key] = fmt.Sprintf("%v", v)
		}
	}
	vars["automation.name"] = a.Name
	vars["event.type"] = e.EventType
	vars["event.entity_id"] = e.EntityID
	vars["event.actor"] = e.Actor
	vars["project.id"] = e.ProjectID

	prompt := automations.RenderPrompt(a.PromptTemplate, vars)
	if strings.TrimSpace(prompt) == "" {
		prompt = fmt.Sprintf("Automation %q fired on event %s (entity %s). Investigate via your OpenV tools and act per your instructions.", a.Name, e.EventType, e.EntityID)
	}

	automationID := a.ID
	eventID := e.ID
	projectID := a.ProjectID
	if projectID == nil && e.ProjectID != "" {
		p := e.ProjectID
		projectID = &p
	}
	req := agentruns.LaunchRequest{
		OrgID:          a.OrgID,
		AgentID:        agentID,
		ProjectID:      projectID,
		AutomationID:   &automationID,
		TriggerEventID: &eventID,
		TeamID:         teamID,
		TeamNodeID:     teamNodeID,
		Prompt:         prompt,
	}
	if _, _, err := m.runService.Launch(req); err != nil {
		slog.Error("triggers: failed to launch run for automation",
			slog.String("automation_id", a.ID),
			slog.Any("error", err))
		return
	}
	if err := m.repo.MarkRun(a.ID, time.Now().UTC(), a.NextRunAt); err != nil {
		slog.Warn("triggers: failed to stamp last_run_at",
			slog.String("automation_id", a.ID),
			slog.Any("error", err))
	}
}
